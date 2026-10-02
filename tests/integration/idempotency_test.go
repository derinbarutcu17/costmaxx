package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// TestArtifactAddIdempotentAcrossSubprocesses proves the CLI's explicit
// call-reference contract: re-running `costmaxx artifact add --call-ref X`
// with identical inputs (same command, cwd, exit code, stdin, and ref) in a
// fresh subprocess dedupes to exactly one ledger/artifact/reduction row,
// returns the byte-identical envelope (same artifact id), and leaves no
// unreferenced artifact file behind.
func TestArtifactAddIdempotentAcrossSubprocesses(t *testing.T) {
	home := newIsolatedHome(t)
	dbPath := filepath.Join(home, ".costmax", "costmax.db")

	payload := strings.Repeat("line of terminal output for the idempotency test\n", 200)
	run := func() string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, "artifact", "add",
			"--command", "cat input.txt", "--exit-code", "0", "--cwd", "/tmp/work",
			"--call-ref", "cli-idem-test")
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(payload)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("artifact add failed: %v\n%s", err, out)
		}
		return string(out)
	}

	first := run()
	second := run()
	if first != second {
		t.Fatalf("idempotent CLI retry returned a different envelope:\n--- first ---\n%s\n--- retry ---\n%s", first, second)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var artifacts, ledger, reductions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM call_ledger`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reduction_records`).Scan(&reductions); err != nil {
		t.Fatal(err)
	}
	if artifacts != 1 || ledger != 1 || reductions != 1 {
		t.Fatalf("duplicate CLI call double-counted: artifacts=%d ledger=%d reductions=%d, want 1/1/1", artifacts, ledger, reductions)
	}

	// session_metrics must count the call once.
	var toolCalls int
	if err := db.QueryRow(`SELECT tool_calls FROM session_metrics WHERE session_id = 'cli'`).Scan(&toolCalls); err != nil {
		t.Fatal(err)
	}
	if toolCalls != 1 {
		t.Errorf("session_metrics tool_calls = %d, want 1", toolCalls)
	}

	// Exactly one on-disk artifact file, referenced by the stored row.
	var onDisk []string
	root := filepath.Join(home, ".costmax", "artifacts")
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			onDisk = append(onDisk, path)
		}
		return nil
	})
	if len(onDisk) != 1 {
		t.Fatalf("on-disk artifact files = %d, want exactly 1: %v", len(onDisk), onDisk)
	}
	var digest string
	if err := db.QueryRow(`SELECT content_digest FROM artifacts LIMIT 1`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(onDisk[0], digest) {
		t.Errorf("on-disk file %q not referenced by stored digest %s", onDisk[0], digest)
	}

	// Deterministic replay semantics: the explicit ref identifies the logical
	// call, so a retry with different output under the SAME ref is still that
	// same call — the pipeline replays the canonical first envelope (byte
	// identical, same artifact id) instead of recording a second call or
	// storing a second artifact.
	other := strings.Repeat("different logical call output\n", 200)
	cmd := exec.Command(costmaxBinary, "artifact", "add", "--command", "cat input.txt", "--exit-code", "0", "--cwd", "/tmp/work", "--call-ref", "cli-idem-test")
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(other)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("artifact add (different content, same ref) failed: %v\n%s", err, out)
	}
	if string(out) != first {
		t.Errorf("same-ref replay did not return the canonical envelope:\n--- canonical ---\n%s\n--- replay ---\n%s", first, out)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM call_ledger`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 {
		t.Errorf("same-ref different-content retry created a second call: ledger rows = %d, want 1 (deterministic replay)", ledger)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if artifacts != 1 {
		t.Errorf("same-ref different-content retry stored a second artifact: artifacts = %d, want 1", artifacts)
	}
}

// TestConcurrentSameRefDifferentOutputIsCanonical races several processes with
// the SAME explicit call ref but DIFFERENT output content. The idempotency key
// identifies one logical call, so exactly one canonical ledger/artifact/
// reduction row may exist: whichever process commits first wins, and every
// other process must either replay that canonical envelope or write a file
// that is purged as unreferenced. No orphan files, no false savings, and the
// ledger is deterministic (a single row referencing the winner's artifact).
func TestConcurrentSameRefDifferentOutputIsCanonical(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrency regression")
	}
	home := newIsolatedHome(t)
	const n = 10
	errs := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each process pipes a distinct payload but the same logical ref.
			payload := strings.Repeat(fmt.Sprintf("candidate %d unique payload line\n", i), 50)
			cmd := exec.Command(costmaxBinary, "artifact", "add",
				"--command", "printf candidate", "--exit-code", "0",
				"--call-ref", "conc-diff-output")
			cmd.Env = append(os.Environ(), "HOME="+home)
			cmd.Stdin = strings.NewReader(payload)
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- string(out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent same-ref-different-output failure: %s", e)
	}

	db, err := sql.Open("sqlite", filepath.Join(home, ".costmax", "costmax.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var ledger, artifacts, reductions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM call_ledger`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reduction_records`).Scan(&reductions); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 || artifacts != 1 || reductions != 1 {
		t.Fatalf("same-ref different-output race double-counted: ledger=%d artifacts=%d reductions=%d, want 1/1/1", ledger, artifacts, reductions)
	}

	// No orphan files on disk: exactly one stored file, referenced by the
	// single artifact row.
	var onDisk []string
	root := filepath.Join(home, ".costmax", "artifacts")
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			onDisk = append(onDisk, path)
		}
		return nil
	})
	if len(onDisk) != 1 {
		t.Fatalf("orphan artifact files after same-ref race: %d files: %v", len(onDisk), onDisk)
	}
	var digest string
	if err := db.QueryRow(`SELECT content_digest FROM artifacts LIMIT 1`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(onDisk[0], digest) {
		t.Errorf("canonical file %q not referenced by stored digest %s", onDisk[0], digest)
	}

	// Exactly one ledger row, no false token saving fabricated by the losers.
	var rawTokens, modelTokens int64
	if err := db.QueryRow(`SELECT raw_token_est, model_visible_token_est FROM call_ledger LIMIT 1`).Scan(&rawTokens, &modelTokens); err != nil {
		t.Fatal(err)
	}
	if modelTokens > rawTokens {
		t.Errorf("canonical row claims model-visible tokens > raw tokens: %d > %d", modelTokens, rawTokens)
	}
}

// TestArtifactAddIndependentInvocationsCountSeparately proves the default CLI
// contract: two identical `artifact add` invocations WITHOUT an explicit
// call ref are two independent real calls and must each be recorded, never
// collapsed into one row by a content-derived key. This guards against
// undercounting genuine usage.
func TestArtifactAddIndependentInvocationsCountSeparately(t *testing.T) {
	home := newIsolatedHome(t)
	dbPath := filepath.Join(home, ".costmax", "costmax.db")

	payload := strings.Repeat("identical independent invocation output\n", 200)
	run := func() string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, "artifact", "add",
			"--command", "cat input.txt", "--exit-code", "0", "--cwd", "/tmp/work")
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(payload)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("artifact add failed: %v\n%s", err, out)
		}
		return string(out)
	}

	first := run()
	second := run()

	// Distinct invocations produce distinct artifact IDs even for identical
	// content, so the second call is a real new call, not a replayed retry.
	if first == second {
		t.Fatalf("independent identical invocations returned the same envelope (conflated):\n%s", first)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var artifacts, ledger int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM call_ledger`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if artifacts != 2 || ledger != 2 {
		t.Fatalf("independent identical invocations were undercounted: artifacts=%d ledger=%d, want 2/2", artifacts, ledger)
	}

	// session_metrics must count both calls.
	var toolCalls int
	if err := db.QueryRow(`SELECT tool_calls FROM session_metrics WHERE session_id = 'cli'`).Scan(&toolCalls); err != nil {
		t.Fatal(err)
	}
	if toolCalls != 2 {
		t.Errorf("session_metrics tool_calls = %d, want 2", toolCalls)
	}
}

// TestMCPIdempotencyWithinProcessAndBoundaryAcrossProcesses proves the exact
// idempotency boundary of the MCP tool: a same-id retry inside one server
// process (one client session) is answered from stored evidence — one ledger
// row, byte-identical response — while the same id in a new subprocess (a
// distinct session) is a genuinely different call and records its own row.
func TestMCPIdempotencyWithinProcessAndBoundaryAcrossProcesses(t *testing.T) {
	home := newIsolatedHome(t)
	dbPath := filepath.Join(home, ".costmax", "costmax.db")

	request := `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"costmax_run","arguments":{"command":"printf mcp-idem"}}}`
	resps, _ := runMCP(t, home, request, request)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	text := func(r rpcResponse) string {
		var res struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if r.Error != nil {
			t.Fatalf("call error: %v", r.Error)
		}
		if err := json.Unmarshal(r.Result, &res); err != nil {
			t.Fatal(err)
		}
		return res.Content[0].Text
	}
	a, b := text(resps[0]), text(resps[1])
	if a != b {
		t.Fatalf("same-id retry returned a different envelope:\n--- first ---\n%s\n--- retry ---\n%s", a, b)
	}

	count := func(table string) int {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("call_ledger"); n != 1 {
		t.Fatalf("same-process retry double-counted: ledger rows = %d, want 1", n)
	}
	if n := count("artifacts"); n != 1 {
		t.Fatalf("same-process retry stored extra artifacts: rows = %d, want 1", n)
	}

	// A new subprocess is a new session: the same request id is a different
	// call and must be recorded separately (honest boundary, not a false
	// dedup across sessions).
	resps, _ = runMCP(t, home, request)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response from fresh subprocess, got %d", len(resps))
	}
	if n := count("call_ledger"); n != 2 {
		t.Fatalf("cross-process same-id call was deduped: ledger rows = %d, want 2", n)
	}
}
