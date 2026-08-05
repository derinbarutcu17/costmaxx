package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type hookOutput struct {
	Continue           bool   `json:"continue"`
	SystemMessage      string `json:"systemMessage"`
	SuppressOutput     bool   `json:"suppressOutput"`
	Decision           string `json:"decision"`
	HookSpecificOutput *struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func runHookCLI(t *testing.T, home, input string) (hookOutput, int) {
	t.Helper()
	cmd := exec.Command(costmaxBinary, "hook")
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = bytes.NewBufferString(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("hook run failed: %v", err)
		}
	}
	var out hookOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unparseable hook output %q: %v (stderr: %s)", stdout.String(), err, stderr.String())
	}
	return out, exit
}

// Every event type except SessionStart must be a pure noop: continue only,
// no context injection, no output substitution, no suppression.
func TestHookEventsAreObserveOnly(t *testing.T) {
	home := newIsolatedHome(t)
	events := []struct {
		name  string
		input string
	}{
		{"UserPromptSubmit", `{"session_id":"s1","hook_event_name":"UserPromptSubmit","prompt":"do the thing"}`},
		{"PreToolUse", `{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hi"}}`},
		{"PostToolUse", `{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"echo hi"},"tool_response":{"output":"hi\n","exit_code":0}}`},
		{"PreCompact", `{"session_id":"s1","hook_event_name":"PreCompact","trigger":"token_limit"}`},
		{"PostCompact", `{"session_id":"s1","hook_event_name":"PostCompact","trigger":"token_limit"}`},
		{"Stop", `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":"done"}`},
		{"SessionEnd", `{"session_id":"s1","hook_event_name":"SessionEnd","reason":"done"}`},
		{"UnknownEvent", `{"session_id":"s1","hook_event_name":"TotallyUnknown"}`},
	}
	for _, e := range events {
		out, exit := runHookCLI(t, home, e.input)
		if exit != 0 {
			t.Errorf("%s: non-zero exit %d", e.name, exit)
		}
		if !out.Continue {
			t.Errorf("%s: continue = false", e.name)
		}
		if out.HookSpecificOutput != nil {
			t.Errorf("%s: injected context (observe-only violation): %+v", e.name, out.HookSpecificOutput)
		}
		if out.SystemMessage != "" || out.SuppressOutput || out.Decision != "" {
			t.Errorf("%s: non-noop output: %+v", e.name, out)
		}
	}
}

// SessionStart is the one documented capability that injects context.
func TestHookSessionStartInjectsContext(t *testing.T) {
	home := newIsolatedHome(t)
	out, exit := runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"SessionStart","cwd":"/tmp"}`)
	if exit != 0 || !out.Continue {
		t.Fatalf("SessionStart failed: exit %d continue %v", exit, out.Continue)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("SessionStart must inject hookSpecificOutput")
	}
	if out.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q", out.HookSpecificOutput.HookEventName)
	}
	if out.HookSpecificOutput.AdditionalContext == "" {
		t.Error("additionalContext is empty")
	}
}

func TestHookMalformedJSONIsNoop(t *testing.T) {
	home := newIsolatedHome(t)
	out, exit := runHookCLI(t, home, `{not json`)
	if exit != 0 {
		t.Errorf("malformed JSON should exit 0, got %d", exit)
	}
	if !out.Continue {
		t.Error("malformed JSON should be a noop continue")
	}
	if out.HookSpecificOutput != nil {
		t.Error("malformed JSON must not inject context")
	}
}

// PostToolUse with a large output must persist an artifact locally but never
// return the output to the harness.
func TestHookPostToolUseStoresArtifactWithoutReturningOutput(t *testing.T) {
	home := newIsolatedHome(t)
	// Real Codex flow: SessionStart precedes PostToolUse (separate hook
	// invocations, state recovered from the DB).
	runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"SessionStart","cwd":"/tmp"}`)
	big := `{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash",` +
		`"tool_input":{"command":"make test"},"tool_response":{"output":"` +
		`test line one\n` + `test line two\n` + `1000 passed, 0 failed\n",` +
		`"exit_code":0}}`
	out, exit := runHookCLI(t, home, big)
	if exit != 0 || !out.Continue {
		t.Fatalf("PostToolUse failed: exit %d continue %v", exit, out.Continue)
	}
	// The output must NOT be echoed back: no systemMessage, no context.
	if out.SystemMessage != "" || out.HookSpecificOutput != nil {
		t.Errorf("PostToolUse returned content to harness: %+v", out)
	}
	// But it must be stored locally as evidence.
	artifactFiles := 0
	root := filepath.Join(home, ".costmax", "artifacts", "sha256")
	filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			artifactFiles++
		}
		return nil
	})
	if artifactFiles == 0 {
		t.Error("PostToolUse did not store an artifact")
	}
}

// Stop persists NextAction; a fresh process SessionStart on the same HOME
// resumes the task state. Cross-process recovery is a claimed capability.
func TestHookCrossProcessStateResume(t *testing.T) {
	home := newIsolatedHome(t)
	runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"SessionStart","cwd":"/tmp"}`)
	runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":"next: fix the tests"}`)

	out, _ := runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"SessionStart","cwd":"/tmp"}`)
	if out.HookSpecificOutput == nil {
		t.Fatal("resumed SessionStart injected no context")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	t.Logf("resumed context: %q", ctx)
}

func TestHookStateCommandShowsResumedState(t *testing.T) {
	home := newIsolatedHome(t)
	runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"SessionStart","cwd":"/tmp"}`)
	runHookCLI(t, home, `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":"next: fix the tests"}`)

	cmd := exec.Command(costmaxBinary, "state", "s1")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("state command: %v", err)
	}
	if !bytes.Contains(out, []byte("next: fix the tests")) {
		t.Errorf("state output missing NextAction:\n%s", out)
	}
	fmt.Println(string(out))
}
