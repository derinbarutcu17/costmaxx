package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// GC must remove files AND their metadata rows together, and must handle
// content-addressed files shared by multiple rows without leaving dangling
// references.
func TestGCRemovesFilesAndMetadataConsistently(t *testing.T) {
	home := newIsolatedHome(t)
	add := func(command string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, "artifact", "add", "--command", command, "--exit-code", "0")
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader("shared-content\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("artifact add failed: %v", err)
		}
		return extractArtifactID(t, string(out))
	}
	id1 := add("echo one")
	id2 := add("echo two")

	dbPath := filepath.Join(home, ".costmax", "costmax.db")
	if n := rowsInTable(t, dbPath, "artifacts"); n != 2 {
		t.Fatalf("expected 2 artifact rows, got %d", n)
	}
	files := countZst(t, home)
	if files != 1 {
		t.Fatalf("shared content should produce 1 file, got %d", files)
	}

	gc := exec.Command(costmaxBinary, "gc", "--older-than=0")
	gc.Env = append(os.Environ(), "HOME="+home)
	out, err := gc.CombinedOutput()
	if err != nil {
		t.Fatalf("gc failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Cleaned 2") {
		t.Errorf("expected 2 cleaned, got: %s", out)
	}
	if n := rowsInTable(t, dbPath, "artifacts"); n != 0 {
		t.Errorf("gc left %d metadata rows behind", n)
	}
	if files := countZst(t, home); files != 0 {
		t.Errorf("gc left %d files behind", files)
	}

	// Retrieval of a collected artifact must be a clean not-found error.
	for _, id := range []string{id1, id2} {
		cmd := exec.Command(costmaxBinary, "artifact", "retrieve", id)
		cmd.Env = append(os.Environ(), "HOME="+home)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("retrieve of gc'd artifact %s should fail", id)
		}
		if strings.Contains(string(out), "Usage:") {
			t.Errorf("retrieve not-found must not dump usage:\n%s", out)
		}
	}
}

// A fresh artifact sharing a digest with an old one must survive gc: the
// file stays, only the old row goes.
func TestGCKeepsFileWhileFreshRowReferencesIt(t *testing.T) {
	home := newIsolatedHome(t)
	add := func(command string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, "artifact", "add", "--command", command, "--exit-code", "0")
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader("same-payload\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("artifact add failed: %v", err)
		}
		return extractArtifactID(t, string(out))
	}
	oldID := add("echo old")
	freshID := add("echo fresh")

	// Backdate the old row below the cutoff, keep the fresh one as-is.
	// (sqlite3 CLI does no ?-binding; embed the literal id.)
	dbPath := filepath.Join(home, ".costmax", "costmax.db")
	stmt := "UPDATE artifacts SET created_at = '2000-01-01T00:00:00Z' WHERE artifact_id = '" + oldID + "';"
	if out, err := exec.Command("sqlite3", dbPath, stmt).CombinedOutput(); err != nil {
		t.Fatalf("backdate failed: %v\n%s", err, out)
	}

	gc := exec.Command(costmaxBinary, "gc", "--older-than=1h")
	gc.Env = append(os.Environ(), "HOME="+home)
	if out, err := gc.CombinedOutput(); err != nil {
		t.Fatalf("gc failed: %v\n%s", err, out)
	}

	if n := rowsInTable(t, dbPath, "artifacts"); n != 1 {
		t.Errorf("expected 1 surviving row, got %d", n)
	}
	if files := countZst(t, home); files != 1 {
		t.Errorf("shared file should survive for the fresh row, got %d files", files)
	}

	cmd := exec.Command(costmaxBinary, "artifact", "retrieve", freshID)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fresh artifact must still be retrievable: %v", err)
	}
	if !strings.Contains(string(out), "same-payload") {
		t.Errorf("retrieved content mismatch: %q", out)
	}
}

func rowsInTable(t *testing.T, dbPath, table string) int {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, "SELECT COUNT(*) FROM "+table+";").Output()
	if err != nil {
		t.Fatalf("sqlite3 query failed: %v", err)
	}
	var n int
	for _, c := range strings.TrimSpace(string(out)) {
		n = n*10 + int(c-'0')
	}
	return n
}

func countZst(t *testing.T, home string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".costmax", "artifacts", "sha256", "*", "*", "*.zst"))
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}

// Regression: concurrent processes racing a FRESH database must all succeed.
// Before the DSN pragma fix (mattn-style params ignored by modernc) and the
// idempotent v4 migration, this reliably produced SQLITE_BUSY and
// "duplicate column name: cwd" failures.
func TestConcurrentColdStartAllSucceed(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrency regression")
	}
	home := newIsolatedHome(t)
	const n = 12
	errs := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(costmaxBinary, "artifact", "add", "--command", "echo hi", "--exit-code", "0")
			cmd.Env = append(os.Environ(), "HOME="+home)
			cmd.Stdin = strings.NewReader("payload\n")
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- string(out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent cold-start failure: %s", e)
	}
	if got := rowsInTable(t, filepath.Join(home, ".costmax", "costmax.db"), "artifacts"); got != n {
		t.Errorf("expected %d artifact rows, got %d", n, got)
	}
}
