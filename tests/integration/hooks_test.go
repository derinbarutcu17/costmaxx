package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/codex"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

func newTestAdapter(t *testing.T) (*codex.Adapter, string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "costmax-hooks-*")
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Core.DataDir = dir
	cfg.Store.DBPath = filepath.Join(dir, "test.db")
	cfg.Store.ArtifactDir = filepath.Join(dir, "artifacts")

	os.MkdirAll(cfg.Store.ArtifactDir, 0700)

	db, err := store.Open(cfg.Store.DBPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}

	artStore, err := artifacts.NewStore(cfg.Store.ArtifactDir, 1<<20)
	if err != nil {
		db.Close()
		os.RemoveAll(dir)
		t.Fatal(err)
	}

	adapter := codex.New(cfg, artStore, db)
	return adapter, dir, func() {
		db.Close()
		os.RemoveAll(dir)
	}
}

func runHook(t *testing.T, a *codex.Adapter, input string) *codex.HookOutput {
	t.Helper()
	r := strings.NewReader(input)
	resp := a.HandleHook(r)
	if resp == nil {
		t.Fatal("HandleHook returned nil")
	}
	return resp
}

func TestHookSessionStart(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	input := `{"hook_event_name":"SessionStart","session_id":"sess_test123","cwd":"/home/user/repo","source":"startup"}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("expected SessionStart, got %s", resp.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "sess_test123") {
		t.Error("expected session ID in additionalContext")
	}
}

func TestHookSessionStartResumesState(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	// First session sets state
	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_resume","source":"startup"}`))

	// Second session with same ID resumes it
	resp := a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_resume","source":"resume"}`))

	if !resp.Continue {
		t.Error("expected continue=true")
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput on resume")
	}
}

func TestHookUserPromptSubmit(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test","cwd":"/repo"}`))

	input := `{"hook_event_name":"UserPromptSubmit","session_id":"sess_test","prompt":"Fix the auth tests"}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookPreToolUse(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	input := `{"hook_event_name":"PreToolUse","session_id":"sess_test","tool_name":"Bash","tool_input":{"command":"echo hello"}}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookPostToolUse(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test"}`))

	input := `{"hook_event_name":"PostToolUse","session_id":"sess_test","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"Tests: 10 passed, 2 failed\n● auth/test.ts:45\n"}}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookPreCompact(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test"}`))

	input := `{"hook_event_name":"PreCompact","session_id":"sess_test","trigger":"auto"}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookPostCompact(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test","cwd":"/repo"}`))
	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PreCompact","session_id":"sess_test","trigger":"auto"}`))

	// PostCompact should not emit context (Codex limitation)
	input := `{"hook_event_name":"PostCompact","session_id":"sess_test","trigger":"auto"}`
	resp := runHook(t, a, input)
	if !resp.Continue {
		t.Error("expected continue=true")
	}
	if resp.HookSpecificOutput != nil {
		t.Error("PostCompact should not emit hookSpecificOutput")
	}

	// State should still be loadable by SessionStart
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test","cwd":"/repo","source":"resume"}`))
	if resp.HookSpecificOutput == nil || resp.HookSpecificOutput.AdditionalContext == "" {
		t.Fatal("SessionStart should return context after PostCompact persisted state")
	}
}

func TestHookStop(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test"}`))

	input := `{"hook_event_name":"Stop","session_id":"sess_test","last_assistant_message":"All tests pass"}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookSessionEnd(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_test"}`))

	input := `{"hook_event_name":"SessionEnd","session_id":"sess_test","reason":"other"}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookMalformedJSON(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	input := `this is not json`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true on malformed input")
	}
}

func TestHookUnknownEvent(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	input := `{"hook_event_name":"UnknownEvent","session_id":"sess_test"}`
	resp := runHook(t, a, input)

	if !resp.Continue {
		t.Error("expected continue=true on unknown event")
	}
}

func TestHookSessionIdentityPreserved(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	// SessionStart creates state keyed by session ID
	resp := a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_preserve","cwd":"/repo"}`))
	if resp.HookSpecificOutput == nil || !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "sess_preserve") {
		t.Error("session ID not preserved in response")
	}

	// PostToolUse with same session should not panic
	resp2 := a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PostToolUse","session_id":"sess_preserve","tool_name":"echo","tool_response":{"output":"hello"}}`))
	if !resp2.Continue {
		t.Error("expected continue=true")
	}
}

func TestHookEndToEndFlow(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	// 1. SessionStart
	resp := a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_e2e","cwd":"/repo"}`))
	if resp.HookSpecificOutput == nil {
		t.Fatal("SessionStart expected hookSpecificOutput")
	}

	// 2. UserPromptSubmit
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"UserPromptSubmit","session_id":"sess_e2e","prompt":"Fix the auth tests"}`))
	if !resp.Continue {
		t.Fatal("UserPromptSubmit expected continue=true")
	}

	// 3. PostToolUse
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PostToolUse","session_id":"sess_e2e","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"Tests: 142 passed, 3 failed\n"}}`))
	if !resp.Continue {
		t.Fatal("PostToolUse expected continue=true")
	}

	// 4. PreCompact
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PreCompact","session_id":"sess_e2e","trigger":"auto"}`))
	if !resp.Continue {
		t.Fatal("PreCompact expected continue=true")
	}

	// 5. PostCompact (no-op for context, but persists state)
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PostCompact","session_id":"sess_e2e","trigger":"auto"}`))
	if !resp.Continue {
		t.Fatal("PostCompact expected continue=true")
	}
	if resp.HookSpecificOutput != nil {
		t.Error("PostCompact should not emit hookSpecificOutput")
	}

	// 6. Stop
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"Stop","session_id":"sess_e2e"}`))
	if !resp.Continue {
		t.Fatal("Stop expected continue=true")
	}

	// 7. SessionEnd
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionEnd","session_id":"sess_e2e"}`))
	if !resp.Continue {
		t.Fatal("SessionEnd expected continue=true")
	}
}

func TestPostToolUseBuildsTaskState(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_state","cwd":"/repo"}`))

	// Send a real-looking test failure output
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_state","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"Tests: 142 passed, 3 failed\n● auth/session.test.ts:88\n  Expected: 401\n  Received: 500\n● auth/refresh.test.ts:132\n  Expected token refresh, got timeout\n● middleware/auth.test.ts:47\n  Expected 401, got 500\n","exit_code":1}}`
	resp := a.HandleHook(strings.NewReader(payload))
	if !resp.Continue {
		t.Fatal("expected continue=true")
	}

	// State persists across compaction — SessionStart resume should include it
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PreCompact","session_id":"sess_state","trigger":"auto"}`))
	if !resp.Continue {
		t.Fatal("PreCompact expected continue=true")
	}
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PostCompact","session_id":"sess_state","trigger":"auto"}`))
	if !resp.Continue {
		t.Fatal("PostCompact expected continue=true")
	}
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_state","source":"resume"}`))
	if resp.HookSpecificOutput == nil {
		t.Fatal("SessionStart resume expected hookSpecificOutput")
	}
	ctx := resp.HookSpecificOutput.AdditionalContext
	if len(ctx) == 0 {
		t.Fatal("expected non-empty additionalContext from SessionStart resume")
	}
}

func TestLiveCodexBashStringResponse(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_bash_str","cwd":"/repo"}`))

	// Exact shape captured from real Codex: tool_response is a plain string
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_bash_str","tool_name":"Bash","tool_input":{"command":"wc -l test.txt"},"tool_response":"       1 test.txt"}`
	resp := a.HandleHook(strings.NewReader(payload))
	if !resp.Continue {
		t.Fatal("expected continue=true")
	}

	// Verify artifact metadata was persisted
	evts, err := a.DBEvents("sess_bash_str")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evts {
		if e.EventType == "post_tool_use" && e.ToolName == "Bash" {
			found = true
			if e.ToolOutput != "       1 test.txt" {
				t.Errorf("expected tool_output preserved, got %q", e.ToolOutput)
			}
			if e.ExecutionMetadata == nil || e.ExecutionMetadata["artifact_id"] == "" {
				t.Error("expected artifact_id in execution_metadata")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected post_tool_use event for Bash")
	}

	// Verify artifact record exists in DB
	artID := ""
	for _, e := range evts {
		if e.EventType == "post_tool_use" && e.ToolName == "Bash" && e.ExecutionMetadata != nil {
			artID = e.ExecutionMetadata["artifact_id"]
			break
		}
	}
	if artID == "" {
		t.Fatal("artifact ID not found in event metadata")
	}
	meta, err := a.GetArtifact(artID)
	if err != nil || meta == nil {
		t.Fatalf("artifact metadata not found: err=%v meta=%v", err, meta)
	}
	if meta.Command != "wc -l test.txt" {
		t.Errorf("expected command 'wc -l test.txt', got %q", meta.Command)
	}
	if meta.OriginalBytes == 0 {
		t.Error("expected non-zero original bytes")
	}

	// Verify artifact file on disk
	raw, err := a.GetArtifactStore().RetrieveByDigest(meta.ContentDigest)
	if err != nil {
		t.Fatalf("retrieve by digest: %v", err)
	}
	if string(raw) != "       1 test.txt" {
		t.Errorf("artifact content mismatch: got %q", string(raw))
	}

	// PostCompact should not emit context (Codex limitation)
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PostCompact","session_id":"sess_bash_str","trigger":"auto"}`))
	if resp.HookSpecificOutput != nil {
		t.Error("PostCompact should not emit hookSpecificOutput")
	}
	// State should still be loadable by SessionStart
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_bash_str","cwd":"/repo","source":"resume"}`))
	if resp.HookSpecificOutput == nil || resp.HookSpecificOutput.AdditionalContext == "" {
		t.Fatal("SessionStart should return context after PostCompact persisted state")
	}
}

func TestStoredArtifactIsFindable(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_artifact","cwd":"/repo"}`))

	// Send PostToolUse with test output so an artifact is stored
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_artifact","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"Tests: 50 passed, 1 failed\n● auth/test.ts:12\n","exit_code":1}}`
	resp := a.HandleHook(strings.NewReader(payload))
	if !resp.Continue {
		t.Fatal("expected continue=true")
	}

	// Load the event and verify it has tool output
	evts, err := a.DBEvents("sess_artifact")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evts {
		if e.EventType == "post_tool_use" {
			found = true
			if e.ToolName == "" {
				t.Error("expected tool_name to be set")
			}
			if e.ToolOutput == "" {
				t.Error("expected tool_output to be preserved")
			}
			break
		}
	}
	if !found {
		t.Error("expected post_tool_use event in DB")
	}
}

func TestCrossProcessArtifactRetrieval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test")
	}

	binary := costmaxBinary

	homeDir := t.TempDir()
	dataDir := filepath.Join(homeDir, ".costmax")
	os.MkdirAll(dataDir, 0700)

	run := func(stdin string) string {
		out, err := runCostmaxHook(binary, stdin, homeDir)
		if err != nil {
			t.Fatalf("subprocess failed: %v\nstdin: %s", err, stdin)
		}
		return out
	}

	// SessionStart
	run(`{"hook_event_name":"SessionStart","session_id":"sess_retrieve","cwd":"/repo","source":"startup"}`)

	// PostToolUse with test output
	testOutput := "package main\n\nfunc main() { println(\"hello\") }\n"
	escaped := strings.ReplaceAll(testOutput, "\n", "\\n")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_retrieve","tool_name":"Bash","tool_input":{"command":"cat main.go"},"tool_response":{"output":"` + escaped + `","exit_code":0}}`
	run(payload)

	// Open the DB in a new process to find the artifact
	db, err := store.Open(filepath.Join(dataDir, "costmax.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Find the post_tool_use event, get artifact ID from execution_metadata
	evts, err := db.GetSessionEvents("sess_retrieve")
	if err != nil {
		t.Fatal(err)
	}
	var artifactID string
	for _, e := range evts {
		if e.EventType == events.EventPostToolUse {
			if e.ExecutionMetadata != nil {
				artifactID = e.ExecutionMetadata["artifact_id"]
			}
			break
		}
	}
	if artifactID == "" {
		t.Fatal("no artifact ID found in stored event")
	}

	// Load artifact metadata from DB
	meta, err := db.GetArtifact(artifactID)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("artifact metadata not found in DB")
	}
	if meta.Command != "cat main.go" {
		t.Errorf("expected command 'cat main.go', got %q", meta.Command)
	}
	if meta.ExitCode != 0 {
		t.Errorf("expected exit_code 0, got %d", meta.ExitCode)
	}

	// Resolve to stored file and read content back
	artStore, err := artifacts.NewStore(filepath.Join(dataDir, "artifacts"), 50<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := artStore.RetrieveByDigest(meta.ContentDigest)
	if err != nil {
		t.Fatalf("retrieve by digest %s: %v", meta.ContentDigest, err)
	}
	if string(raw) != testOutput {
		t.Errorf("artifact content mismatch:\ngot:  %q\nwant: %q", string(raw), testOutput)
	}

	// Verify digest
	digest := sha256.Sum256(raw)
	gotDigest := hex.EncodeToString(digest[:])
	if gotDigest != meta.ContentDigest {
		t.Errorf("digest mismatch: got %s, want %s", gotDigest, meta.ContentDigest)
	}
}

func TestHookFailOpenOnMissingSession(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	// PostToolUse without prior SessionStart should not panic
	input := `{"hook_event_name":"PostToolUse","session_id":"sess_nosession","tool_name":"Bash","tool_response":{"output":"test"}}`
	resp := runHook(t, a, input)
	if !resp.Continue {
		t.Error("expected continue=true even without prior session")
	}
}

func TestSubprocessSessionPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test")
	}

	binary := costmaxBinary

	homeDir := t.TempDir()
	dataDir := filepath.Join(homeDir, ".costmax")
	os.MkdirAll(dataDir, 0700)

	run := func(stdin string) string {
		out, err := runCostmaxHook(binary, stdin, homeDir)
		if err != nil {
			t.Fatalf("subprocess failed: %v\nstdin: %s", err, stdin)
		}
		return out
	}

	// SessionStart — inject with session context
	out1 := run(`{"hook_event_name":"SessionStart","session_id":"sess_sub","cwd":"/repo","source":"startup"}`)
	if !strings.Contains(out1, "SessionStart") {
		t.Errorf("SessionStart output missing event name: %s", out1)
	}

	// UserPromptSubmit — record prompt, should passthrough
	out2 := run(`{"hook_event_name":"UserPromptSubmit","session_id":"sess_sub","prompt":"Fix the tests"}`)
	if !strings.Contains(out2, "true") {
		t.Errorf("UserPromptSubmit output unexpected: %s", out2)
	}

	// PostCompact — persists state, does not emit context
	out3 := run(`{"hook_event_name":"PostCompact","session_id":"sess_sub","trigger":"auto"}`)
	if strings.Contains(out3, "hookSpecificOutput") {
		t.Errorf("PostCompact should not emit context, got: %s", out3)
	}

	// SessionStart in new process should find state persisted by PostCompact
	out4 := run(`{"hook_event_name":"SessionStart","session_id":"sess_sub","source":"resume"}`)
	if !strings.Contains(out4, "SessionStart") {
		t.Errorf("SessionStart resume should return context from persisted state: %s", out4)
	}
}

func TestTwoProcessMetricsAccumulate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test")
	}

	binary := costmaxBinary
	homeDir := t.TempDir()
	dataDir := filepath.Join(homeDir, ".costmax")
	os.MkdirAll(dataDir, 0700)

	run := func(stdin string) string {
		out, err := runCostmaxHook(binary, stdin, homeDir)
		if err != nil {
			t.Fatalf("subprocess failed: %v\nstdin: %s", err, stdin)
		}
		return out
	}

	// SessionStart
	run(`{"hook_event_name":"SessionStart","session_id":"sess_metrics_acc","cwd":"/repo","source":"startup"}`)

	// Build large outputs to trigger reduction
	big1 := strings.Repeat("line of test output\n", 300)
	big2 := strings.Repeat("another line of build output\n", 300)
	esc1 := strings.ReplaceAll(strings.ReplaceAll(big1, "\n", "\\n"), "\"", "\\\"")
	esc2 := strings.ReplaceAll(strings.ReplaceAll(big2, "\n", "\\n"), "\"", "\\\"")

	// First PostToolUse (separate process)
	run(`{"hook_event_name":"PostToolUse","session_id":"sess_metrics_acc","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"` + esc1 + `","exit_code":0}}`)

	// Second PostToolUse (separate process)
	run(`{"hook_event_name":"PostToolUse","session_id":"sess_metrics_acc","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"` + esc2 + `","exit_code":0}}`)

	// Open DB and verify metrics accumulated across both processes
	db, err := store.Open(filepath.Join(dataDir, "costmax.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rt, ct, ar, tc, err := db.GetSessionMetrics("sess_metrics_acc")
	if err != nil {
		t.Fatal(err)
	}
	if tc < 2 {
		t.Errorf("expected tool_calls >= 2 across two processes, got %d", tc)
	}
	if ar < 2 {
		t.Errorf("expected artifacts_reduced >= 2 across two processes, got %d", ar)
	}
	_ = rt
	_ = ct
}

func runCostmaxHook(binary, stdin, homeDir string) (string, error) {
	cmd := exec.Command(binary, "hook")
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	cmd.Stdin = bytes.NewBufferString(stdin)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
