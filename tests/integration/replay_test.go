package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func addArtifact(t *testing.T, home string, args ...string) string {
	t.Helper()
	base := []string{"artifact", "add", "--command", "echo replay-works", "--exit-code", "0"}
	base = append(base, args...)
	cmd := exec.Command(costmaxBinary, base...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader("payload\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("artifact add failed: %v\n%s", err, out)
	}
	return extractArtifactID(t, string(out))
}

func TestReplayRunsStoredCommand(t *testing.T) {
	home := newIsolatedHome(t)
	id := addArtifact(t, home)
	cmd := exec.Command(costmaxBinary, "replay", id)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("replay failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "replay-works") {
		t.Errorf("replay output missing echoed text:\n%s", out)
	}
}

func TestReplayPropagatesExitCode(t *testing.T) {
	home := newIsolatedHome(t)
	cmd := exec.Command(costmaxBinary, "artifact", "add", "--command", "exit 3", "--exit-code", "3")
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader("payload\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("artifact add failed: %v", err)
	}
	id := extractArtifactID(t, string(out))

	replay := exec.Command(costmaxBinary, "replay", id)
	replay.Env = append(os.Environ(), "HOME="+home)
	if err := replay.Run(); err == nil {
		t.Fatal("replay of exit-3 command should exit nonzero")
	} else if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
		t.Fatalf("expected exit 3, got %v", err)
	}
}

func TestReplayUsesStoredCwd(t *testing.T) {
	home := newIsolatedHome(t)
	dir := t.TempDir()
	id := addArtifact(t, home, "--cwd", dir, "--command", "pwd")
	cmd := exec.Command(costmaxBinary, "replay", id)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if !strings.Contains(string(out), dir) {
		t.Errorf("replay did not run in stored cwd %s:\n%s", dir, out)
	}
}

func TestArtifactPathPrintsExistingFile(t *testing.T) {
	home := newIsolatedHome(t)
	id := addArtifact(t, home)
	cmd := exec.Command(costmaxBinary, "artifact", "path", id)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("artifact path failed: %v", err)
	}
	path := strings.TrimSpace(string(out))
	if !strings.HasSuffix(path, ".zst") {
		t.Errorf("expected .zst path, got %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("artifact file does not exist at %q: %v", path, err)
	}
	// Not the isolated home's artifacts dir unless HOME pointed there; the
	// store lives under the isolated home so the path must be under it.
	if !strings.HasPrefix(path, filepath.Join(home, ".costmax")) {
		t.Errorf("artifact path outside isolated store: %q", path)
	}
}

func TestReplayUnknownArtifact(t *testing.T) {
	home := newIsolatedHome(t)
	cmd := exec.Command(costmaxBinary, "replay", "does-not-exist")
	cmd.Env = append(os.Environ(), "HOME="+home)
	if err := cmd.Run(); err == nil {
		t.Fatal("replay of unknown artifact should error")
	}
}
