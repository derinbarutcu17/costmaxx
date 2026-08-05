package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
)

func TestCodexAdapterEndToEnd(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	// Session start
	resp := a.HandleHook(strings.NewReader(
		`{"hook_event_name":"SessionStart","session_id":"sess_e2e","cwd":"/repo"}`))
	if resp.HookSpecificOutput == nil {
		t.Fatal("SessionStart expected hookSpecificOutput")
	}

	// PostToolUse (observe mode)
	output := "Tests: 142 passed, 3 failed\n● auth/session.test.ts:88\n"
	input := `{"hook_event_name":"PostToolUse","session_id":"sess_e2e","tool_name":"Bash","tool_input":{"command":"npm test"},"tool_response":{"output":"` + jsonEsc(output) + `"}}`
	resp = a.HandleHook(strings.NewReader(input))
	if !resp.Continue {
		t.Error("expected continue=true")
	}

	// PostCompact — verifies state was persisted
	resp = a.HandleHook(strings.NewReader(
		`{"hook_event_name":"PostCompact","session_id":"sess_e2e","trigger":"auto"}`))
	if resp.HookSpecificOutput != nil {
		t.Fatal("PostCompact should not emit hookSpecificOutput")
	}
}

func TestArtifactStoreAndRetrieve(t *testing.T) {
	dir, _ := os.MkdirTemp("", "costmax-artifact-*")
	defer os.RemoveAll(dir)

	s, err := artifacts.NewStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("test output data for storage")
	artifact, err := s.Store(data, "test-session", "npm test", 0)
	if err != nil {
		t.Fatal(err)
	}

	if !s.Verify(artifact, data) {
		t.Error("verify failed for stored artifact")
	}

	retrieved, err := s.RetrieveByDigest(artifact.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}

	if string(retrieved) != string(data) {
		t.Errorf("round-trip failed: got %q, want %q", retrieved, data)
	}
}

func jsonEsc(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

var _ = os.DevNull
