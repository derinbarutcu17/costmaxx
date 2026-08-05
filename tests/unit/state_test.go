package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/state"
)

func TestProjectorStartsTask(t *testing.T) {
	p := state.NewProjector()
	ts := p.StartTask()
	if ts.TaskID == "" {
		t.Error("expected non-empty task ID")
	}
	if ts.StateVersion != 1 {
		t.Error("expected state version 1")
	}
}

func TestProjectorAppliesSessionStart(t *testing.T) {
	p := state.NewProjector()
	ts := p.StartTask()
	evt := &events.HarnessEvent{
		EventID:    "test-event",
		EventType:  events.EventSessionStart,
		SessionID:  "session-1",
		Repository: "github.com/user/repo",
	}
	p.ApplyEvent(ts, evt)
	if len(ts.SessionIDs) != 1 {
		t.Error("expected 1 session ID")
	}
	if ts.Repository != "github.com/user/repo" {
		t.Error("expected repository to be set")
	}
}

func TestProjectorCapturesObjective(t *testing.T) {
	p := state.NewProjector()
	ts := p.StartTask()
	evt := &events.HarnessEvent{
		EventID:    "test-event",
		EventType:  events.EventUserPromptSubmit,
		ToolOutput: "Fix the authentication tests",
	}
	p.ApplyEvent(ts, evt)
	if ts.Objective != "Fix the authentication tests" {
		t.Errorf("expected objective to be set, got %q", ts.Objective)
	}
}

func TestProjectorRecordsUnresolved(t *testing.T) {
	p := state.NewProjector()
	ts := p.StartTask()
	evt := &events.HarnessEvent{
		EventID:    "test-event",
		EventType:  events.EventPostToolUse,
		ToolOutput: "Error: auth failed\nSome other line",
	}
	p.ApplyEvent(ts, evt)
	if len(ts.UnresolvedIssues) == 0 {
		t.Error("expected unresolved issues from error output")
	}
}

func TestProjectorAddsDecision(t *testing.T) {
	p := state.NewProjector()
	ts := p.StartTask()
	p.AddDecision(ts, "Use test cookies", state.FactAgentReported)
	if len(ts.Decisions) != 1 {
		t.Error("expected 1 decision")
	}
	if ts.Decisions[0].Value != "Use test cookies" {
		t.Error("expected decision value")
	}
}

func TestProjectorMarkTestRun(t *testing.T) {
	p := state.NewProjector()
	ts := p.StartTask()
	run := state.TestRun{
		Command:        "npm test",
		Passed:         10,
		Failed:         2,
		FailingTestIDs: []string{"auth.test.ts:12"},
		ExitCode:       1,
	}
	p.MarkTestRun(ts, run)
	if len(ts.TestRuns) != 1 {
		t.Error("expected 1 test run")
	}
	if len(ts.UnresolvedIssues) == 0 {
		t.Error("expected unresolved issues from failing tests")
	}
}
