package integration

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/codex"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/state"
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

// TestHookDuplicatePostToolUseNotDoubleCounted proves the hook path is
// atomic and idempotent: the same logical PostToolUse (same session, same
// tool_use_id) delivered twice records exactly one ledger/artifact/reduction
// row, the artifact/reduction metadata is not partially persisted, and BOTH
// the persisted session_metrics and the in-process metrics Snapshot are
// unchanged by the duplicate. This guards the invariant that in-memory
// metrics increment only after a new ledger row is inserted.
func TestHookDuplicatePostToolUseNotDoubleCounted(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_dup","cwd":"/repo"}`))

	esc := strings.ReplaceAll(strings.Repeat("test line output\n", 200), "\n", "\\n")
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_dup","tool_name":"Bash","tool_use_id":"tool-1","tool_input":{"command":"npm test"},"tool_response":{"output":"` + esc + `","exit_code":1}}`
	if resp := runHook(t, a, payload); !resp.Continue {
		t.Fatal("first PostToolUse expected continue=true")
	}
	snap := a.Metrics().Snapshot()
	if snap.ToolCalls != 1 {
		t.Fatalf("first call in-process tool_calls = %d, want 1", snap.ToolCalls)
	}
	if snap.ArtifactsReduced != 1 {
		t.Fatalf("first call in-process artifacts_reduced = %d, want 1", snap.ArtifactsReduced)
	}
	if resp := runHook(t, a, payload); !resp.Continue {
		t.Fatal("duplicate PostToolUse expected continue=true")
	}

	if n, err := a.CountLedgerRows(); err != nil || n != 1 {
		t.Fatalf("ledger rows = %d (err=%v), want exactly 1", n, err)
	}
	if n, err := a.CountArtifacts(); err != nil || n != 1 {
		t.Fatalf("artifact rows = %d (err=%v), want exactly 1", n, err)
	}
	if n, err := a.CountReductions(); err != nil || n != 1 {
		t.Fatalf("reduction rows = %d (err=%v), want exactly 1", n, err)
	}

	rt, ct, ar, tc, err := a.SessionMetrics("sess_dup")
	if err != nil {
		t.Fatal(err)
	}
	if tc != 1 || ar != 1 {
		t.Errorf("duplicate PostToolUse inflated persisted metrics: tool_calls=%d artifacts_reduced=%d, want 1/1", tc, ar)
	}
	_ = rt
	_ = ct

	// The in-process snapshot must be byte-for-byte unchanged by the
	// duplicate: metrics increment only after a new ledger row is inserted.
	snap2 := a.Metrics().Snapshot()
	if snap2.ToolCalls != snap.ToolCalls {
		t.Errorf("duplicate PostToolUse inflated in-process tool_calls: %d -> %d", snap.ToolCalls, snap2.ToolCalls)
	}
	if snap2.ArtifactsReduced != snap.ArtifactsReduced {
		t.Errorf("duplicate PostToolUse inflated in-process artifacts_reduced: %d -> %d", snap.ArtifactsReduced, snap2.ArtifactsReduced)
	}
	if snap2.RawTokens != snap.RawTokens || snap2.CompactTokens != snap.CompactTokens {
		t.Errorf("duplicate PostToolUse inflated in-process token counters: raw %d->%d compact %d->%d",
			snap.RawTokens, snap2.RawTokens, snap.CompactTokens, snap2.CompactTokens)
	}

	// The ledger row must record the real working directory (from the session
	// repository when PostToolUse carries none) and the redacted command.
	rows, err := a.LedgerRows()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Cwd != "/repo" {
		t.Errorf("ledger cwd = %q, want /repo", rows[0].Cwd)
	}
	if rows[0].Command != "npm test" {
		t.Errorf("ledger command = %q, want npm test", rows[0].Command)
	}
}

// TestHookEmptyOutputRecordedAsObservableCall proves every accepted
// PostToolUse gets a deliberate treatment: a command that produced no stdout
// is still a real call. It must record an observable ledger row (zero bytes,
// no invented saving) and increment the call metric exactly once per new
// call, while a duplicate empty-output delivery with the same tool_use_id
// adds no second row and no second metric increment.
func TestHookEmptyOutputRecordedAsObservableCall(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_empty","cwd":"/repo"}`))

	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_empty","tool_name":"Bash","tool_use_id":"tool-empty","tool_input":{"command":"make clean"},"tool_response":{"output":"","exit_code":0}}`
	if resp := runHook(t, a, payload); !resp.Continue {
		t.Fatal("empty-output PostToolUse expected continue=true")
	}
	if resp := runHook(t, a, payload); !resp.Continue {
		t.Fatal("duplicate empty-output PostToolUse expected continue=true")
	}

	// Exactly one ledger row for the one logical call, and no artifacts
	// (nothing was stored for an empty output).
	if n, err := a.CountLedgerRows(); err != nil || n != 1 {
		t.Fatalf("empty-output ledger rows = %d (err=%v), want exactly 1", n, err)
	}
	if n, err := a.CountArtifacts(); err != nil || n != 0 {
		t.Fatalf("empty-output must not store artifacts, got %d (err=%v)", n, err)
	}
	if n, err := a.CountReductions(); err != nil || n != 0 {
		t.Fatalf("empty-output must not store reductions, got %d (err=%v)", n, err)
	}

	// The ledger row records the call honestly: zero bytes, zero tokens, no
	// claimed saving, passthrough outcome, hook_status observe.
	rows, err := a.LedgerRows()
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.RawBytes != 0 || row.ModelVisibleBytes != 0 {
		t.Errorf("empty-output row must claim zero bytes, got raw=%d visible=%d", row.RawBytes, row.ModelVisibleBytes)
	}
	if row.RawTokenEst != 0 || row.ModelVisibleTokenEst != 0 {
		t.Errorf("empty-output row must claim zero tokens, got raw=%d visible=%d", row.RawTokenEst, row.ModelVisibleTokenEst)
	}
	if row.HookStatus != "observe" {
		t.Errorf("empty-output hook_status = %q, want observe", row.HookStatus)
	}
	if row.Outcome != "passthrough" {
		t.Errorf("empty-output outcome = %q, want passthrough", row.Outcome)
	}
	if row.Command != "make clean" {
		t.Errorf("empty-output command = %q, want make clean", row.Command)
	}

	// Metrics: exactly one tool call recorded, and the in-process snapshot is
	// unchanged by the duplicate.
	_, _, ar, tc, err := a.SessionMetrics("sess_empty")
	if err != nil {
		t.Fatal(err)
	}
	if tc != 1 {
		t.Errorf("empty-output persisted tool_calls = %d, want 1", tc)
	}
	if ar != 0 {
		t.Errorf("empty-output persisted artifacts_reduced = %d, want 0 (no invented saving)", ar)
	}
	snap := a.Metrics().Snapshot()
	if snap.ToolCalls != 1 {
		t.Errorf("empty-output in-process tool_calls = %d, want 1", snap.ToolCalls)
	}
	if snap.ArtifactsReduced != 0 {
		t.Errorf("empty-output in-process artifacts_reduced = %d, want 0", snap.ArtifactsReduced)
	}
}

// TestHookSessionsAreIsolated proves a long-lived adapter serving two sessions
// never leaks objective/repository/state between them. A PostToolUse for a
// session with no persisted state must fail open (noop) — never reuse the other
// session's in-memory state — and each session's PostToolUse must be recorded
// under its own session with its own working directory and objective.
func TestHookSessionsAreIsolated(t *testing.T) {
	a, dir, cleanup := newTestAdapter(t)
	defer cleanup()

	// Session A starts, records its repository, and sets an objective.
	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_a","cwd":"/repo-a"}`))
	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"UserPromptSubmit","session_id":"sess_a","prompt":"Objective for session A"}`))

	// PostToolUse A: no cwd in the payload, so the ledger must fall back to
	// session A's repository (/repo-a).
	if resp := runHook(t, a, `{"hook_event_name":"PostToolUse","session_id":"sess_a","tool_name":"Bash","tool_use_id":"tool-a","tool_input":{"command":"ls"},"tool_response":{"output":"output from A\n"}}`); !resp.Continue {
		t.Fatal("PostToolUse A expected continue=true")
	}

	// A PostToolUse for session B arrives WITHOUT a SessionStart B. It must
	// NOT inherit session A's repository/objective: session B has no state, so
	// it fails open and records nothing.
	if resp := runHook(t, a, `{"hook_event_name":"PostToolUse","session_id":"sess_b","tool_name":"Bash","tool_use_id":"tool-b-pre","tool_input":{"command":"pwd"},"tool_response":{"output":"pre-B\n"}}`); !resp.Continue {
		t.Fatal("PostToolUse B (no state) expected continue=true (fail open)")
	}

	// Session B starts with its own repository and objective.
	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_b","cwd":"/repo-b"}`))
	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"UserPromptSubmit","session_id":"sess_b","prompt":"Objective for session B"}`))
	if resp := runHook(t, a, `{"hook_event_name":"PostToolUse","session_id":"sess_b","tool_name":"Bash","tool_use_id":"tool-b","tool_input":{"command":"pwd"},"tool_response":{"output":"output from B\n"}}`); !resp.Continue {
		t.Fatal("PostToolUse B expected continue=true")
	}

	// Back to session A: a fresh call must be resolved from A's persisted
	// state (cwd fallback /repo-a), never the in-memory session B state.
	if resp := runHook(t, a, `{"hook_event_name":"PostToolUse","session_id":"sess_a","tool_name":"Bash","tool_use_id":"tool-a2","tool_input":{"command":"git status"},"tool_response":{"output":"A again\n"}}`); !resp.Continue {
		t.Fatal("PostToolUse A (second) expected continue=true")
	}

	rows, err := a.LedgerRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("ledger rows = %d, want 3 (A, B, A again); the no-state B call must not record", len(rows))
	}
	for _, r := range rows {
		wantCwd := map[string]string{"ls": "/repo-a", "pwd": "/repo-b", "git status": "/repo-a"}[r.Command]
		if wantCwd == "" {
			t.Errorf("unexpected ledger row command %q", r.Command)
			continue
		}
		if r.Cwd != wantCwd {
			t.Errorf("row %q cwd = %q, want %q (session state leaked across sessions)", r.Command, r.Cwd, wantCwd)
		}
		wantSession := map[string]string{"ls": "sess_a", "pwd": "sess_b", "git status": "sess_a"}[r.Command]
		if r.SessionID != wantSession {
			t.Errorf("row %q session = %q, want %q", r.Command, r.SessionID, wantSession)
		}
	}

	// Objectives must not cross: session A keeps its own objective, session B
	// its own, and the no-state B call must not have stamped A's state onto B.
	stateA := loadHookState(t, dir, "sess_a")
	if stateA == nil || stateA.Objective != "Objective for session A" {
		t.Errorf("session A objective = %+v, want 'Objective for session A'", stateA)
	}
	if stateA == nil || stateA.Repository != "/repo-a" {
		t.Errorf("session A repository = %+v, want /repo-a", stateA)
	}
	stateB := loadHookState(t, dir, "sess_b")
	if stateB == nil || stateB.Objective != "Objective for session B" {
		t.Errorf("session B objective = %+v, want 'Objective for session B' (A's objective leaked)", stateB)
	}
	if stateB == nil || stateB.Repository != "/repo-b" {
		t.Errorf("session B repository = %+v, want /repo-b", stateB)
	}
}

// persistedHookState is the JSON shape of task_state.data used to assert
// cross-session isolation without reaching into adapter internals.
type persistedHookState struct {
	Objective  string `json:"objective"`
	Repository string `json:"repository"`
}

func loadHookState(t *testing.T, dir, sessionID string) *persistedHookState {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var data string
	err = db.QueryRow(`SELECT data FROM task_state WHERE session_id = ?`, sessionID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var ps persistedHookState
	if err := json.Unmarshal([]byte(data), &ps); err != nil {
		t.Fatalf("unmarshal task_state for %s: %v", sessionID, err)
	}
	return &ps
}

// TestHookEmptyNoIDCallsAreDistinctSequential proves two accepted no-id empty
// PostToolUse calls are two ledger rows with distinct call IDs and idempotency
// keys (never collapsed under a shared empty/content-derived key), each
// carrying zero bytes and zero claimed saving, and no artifacts or reductions.
func TestHookEmptyNoIDCallsAreDistinctSequential(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_empty_noid","cwd":"/repo"}`))

	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_empty_noid","tool_name":"Bash","tool_input":{"command":"make clean"},"tool_response":{"output":"","exit_code":0}}`
	for i := 0; i < 2; i++ {
		if resp := runHook(t, a, payload); !resp.Continue {
			t.Fatal("empty no-id PostToolUse expected continue=true")
		}
	}

	rows, err := a.LedgerRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %d, want 2 (two independent no-id empty calls)", len(rows))
	}
	if rows[0].CallID == rows[1].CallID {
		t.Errorf("two no-id empty calls share a call id %q", rows[0].CallID)
	}
	if rows[0].IdempotencyKey == rows[1].IdempotencyKey {
		t.Errorf("two no-id empty calls share an idempotency key %q", rows[0].IdempotencyKey)
	}
	for _, r := range rows {
		if r.RawBytes != 0 || r.ModelVisibleBytes != 0 || r.RawTokenEst != 0 || r.ModelVisibleTokenEst != 0 {
			t.Errorf("no-id empty call claimed bytes/tokens: %+v", r)
		}
		if r.Outcome != "passthrough" || r.HookStatus != "observe" {
			t.Errorf("no-id empty call wrong status: outcome=%q status=%q", r.Outcome, r.HookStatus)
		}
	}
	if n, err := a.CountArtifacts(); err != nil || n != 0 {
		t.Fatalf("no-id empty calls must not store artifacts, got %d (err=%v)", n, err)
	}
	if n, err := a.CountReductions(); err != nil || n != 0 {
		t.Fatalf("no-id empty calls must not store reductions, got %d (err=%v)", n, err)
	}
	_, _, ar, tc, err := a.SessionMetrics("sess_empty_noid")
	if err != nil {
		t.Fatal(err)
	}
	if tc != 2 || ar != 0 {
		t.Errorf("metrics wrong: tool_calls=%d artifacts_reduced=%d, want 2/0", tc, ar)
	}
}

// TestHookEmptyNoIDCallsAreDistinctUnderConcurrency proves N accepted no-id
// empty PostToolUse calls are always N distinct ledger rows: the per-call
// identity is a collision-resistant UUID, never a nanosecond timestamp that
// could tie two concurrent calls into one idempotency key. Each call must
// record zero bytes/tokens, store no artifact or reduction, and never fabricate
// an error row. Each goroutine gets its own adapter (no shared mutable adapter
// state) but all share one store so the ledger is a single source of truth.
func TestHookEmptyNoIDCallsAreDistinctUnderConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrency regression")
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Core.DataDir = dir
	cfg.Store.DBPath = filepath.Join(dir, "test.db")
	cfg.Store.ArtifactDir = filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(cfg.Store.ArtifactDir, 0700); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(cfg.Store.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	artStore, err := artifacts.NewStore(cfg.Store.ArtifactDir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	// Pre-seed the session's task state so a fresh per-goroutine adapter can
	// resolve it through loadOrSkip without a SessionStart.
	if err := db.SaveTaskState("sess_noid_conc", &state.TaskState{
		SchemaVersion: 1, StateVersion: 1, TaskID: "seed", SessionIDs: []string{"sess_noid_conc"},
	}); err != nil {
		t.Fatal(err)
	}

	const n = 8
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_noid_conc","tool_name":"Bash","tool_input":{"command":"make clean"},"tool_response":{"output":"","exit_code":0}}`
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			adapter := codex.New(cfg, artStore, db)
			resp := adapter.HandleHook(strings.NewReader(payload))
			if resp == nil || !resp.Continue {
				t.Error("concurrent empty no-id PostToolUse expected continue=true")
			}
		}()
	}
	wg.Wait()

	rows, err := db.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != n {
		t.Fatalf("ledger rows = %d (err=%v), want %d distinct no-id empty calls", len(rows), err, n)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Outcome == "error" {
			t.Errorf("no-id concurrency fabricated an error row: %+v", r)
		}
		if r.RawBytes != 0 || r.ModelVisibleBytes != 0 || r.RawTokenEst != 0 || r.ModelVisibleTokenEst != 0 {
			t.Errorf("no-id empty call claimed bytes/tokens: %+v", r)
		}
		if seen[r.CallID] {
			t.Errorf("duplicate call id under concurrency: %q", r.CallID)
		}
		seen[r.CallID] = true
	}
	if c, err := db.ArtifactCount(); err != nil || c != 0 {
		t.Fatalf("no-id empty calls must not store artifacts, got %d (err=%v)", c, err)
	}
	if c, err := db.ReductionCount(); err != nil || c != 0 {
		t.Fatalf("no-id empty calls must not store reductions, got %d (err=%v)", c, err)
	}
	_, _, ar, tc, err := db.GetSessionMetrics("sess_noid_conc")
	if err != nil {
		t.Fatal(err)
	}
	if tc != n || ar != 0 {
		t.Errorf("metrics wrong: tool_calls=%d artifacts_reduced=%d, want %d/0", tc, ar, n)
	}
}

// TestHookPostToolUseRecordsRealCwdAndRedactsCommand proves the hook ledger
// persists the input working directory (not an empty string) and that a
// secret inside the command string is redacted before it lands in the ledger
// and artifact metadata.
func TestHookPostToolUseRecordsRealCwdAndRedactsCommand(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_cwd","cwd":"/home/user/repo"}`))

	esc := strings.ReplaceAll(strings.Repeat("build output line\n", 150), "\n", "\\n")
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_cwd","cwd":"/var/tmp/run","tool_name":"Bash","tool_use_id":"tool-cwd","tool_input":{"command":"curl -H 'Authorization: Bearer hook-secret-token-999' https://api.example"},"tool_response":{"output":"` + esc + `","exit_code":0}}`
	if resp := runHook(t, a, payload); !resp.Continue {
		t.Fatal("PostToolUse expected continue=true")
	}

	rows, err := a.LedgerRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(rows))
	}
	if rows[0].Cwd != "/var/tmp/run" {
		t.Errorf("ledger cwd = %q, want the PostToolUse cwd /var/tmp/run", rows[0].Cwd)
	}
	if strings.Contains(rows[0].Command, "hook-secret-token-999") {
		t.Errorf("ledger persisted the raw secret command: %q", rows[0].Command)
	}
	meta, err := a.GetArtifact(rows[0].ArtifactID)
	if err != nil || meta == nil {
		t.Fatalf("artifact lookup: err=%v meta=%v", err, meta)
	}
	if strings.Contains(meta.Command, "hook-secret-token-999") {
		t.Errorf("artifact metadata persisted the raw secret command: %q", meta.Command)
	}
}

func TestHookDuplicatePostToolUseCrossProcess(t *testing.T) {
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

	run(`{"hook_event_name":"SessionStart","session_id":"sess_dup_x","cwd":"/repo"}`)
	output := strings.Repeat("verbose cross-process output\n", 200)
	esc := strings.ReplaceAll(strings.ReplaceAll(output, "\n", "\\n"), "\"", "\\\"")
	payload := `{"hook_event_name":"PostToolUse","session_id":"sess_dup_x","tool_name":"Bash","tool_use_id":"tool-dup","tool_input":{"command":"npm test"},"tool_response":{"output":"` + esc + `","exit_code":1}}`
	run(payload)
	// Same logical PostToolUse in a fresh subprocess: must dedupe.
	run(payload)

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "costmax.db"))
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
		t.Fatalf("duplicate cross-process PostToolUse double-counted: ledger=%d artifacts=%d reductions=%d, want 1/1/1", ledger, artifacts, reductions)
	}

	var toolCalls, artifactsReduced int
	if err := db.QueryRow(`SELECT tool_calls, artifacts_reduced FROM session_metrics WHERE session_id = 'sess_dup_x'`).Scan(&toolCalls, &artifactsReduced); err != nil {
		t.Fatal(err)
	}
	if toolCalls != 1 || artifactsReduced != 1 {
		t.Errorf("duplicate cross-process PostToolUse inflated metrics: tool_calls=%d artifacts_reduced=%d, want 1/1", toolCalls, artifactsReduced)
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
