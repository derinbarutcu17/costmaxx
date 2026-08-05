package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/derinbarutcu17/costmaxx/internal/events"
)

type Projector struct{}

func NewProjector() *Projector { return &Projector{} }

func (p *Projector) StartTask() *TaskState {
	return &TaskState{
		SchemaVersion: 1,
		TaskID:        uuid.New().String(),
		SessionIDs:    []string{},
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		StateVersion:  1,
	}
}

func (p *Projector) ApplyEvent(ts *TaskState, evt *events.HarnessEvent) *TaskState {
	if ts == nil {
		ts = p.StartTask()
	}

	ts.UpdatedAt = time.Now()

	switch evt.EventType {
	case events.EventSessionStart:
		if !contains(ts.SessionIDs, evt.SessionID) {
			ts.SessionIDs = append(ts.SessionIDs, evt.SessionID)
		}
		if evt.Repository != "" {
			ts.Repository = evt.Repository
		}

	case events.EventUserPromptSubmit:
		s := evt.ToolOutput
		if s != "" && ts.Objective == "" {
			ts.Objective = truncate(s, 200)
		}

	case events.EventPostToolUse:
		if evt.ToolName != "" {
			ts.Commands = append(ts.Commands, fmt.Sprintf("%s: %s (exit %d)",
				evt.ToolName, truncate(evt.ToolOutput, 80), getExitCode(evt)))
		}

		if strings.Contains(evt.ToolOutput, "error") || strings.Contains(evt.ToolOutput, "failed") || strings.Contains(evt.ToolOutput, "FAIL") {
			lines := strings.Split(evt.ToolOutput, "\n")
			for _, line := range lines {
				if strings.Contains(strings.ToLower(line), "error") || strings.Contains(strings.ToLower(line), "fail") {
					issue := truncate(strings.TrimSpace(line), 150)
					if !contains(ts.UnresolvedIssues, issue) {
						ts.UnresolvedIssues = append(ts.UnresolvedIssues, issue)
					}
				}
			}
		}

	case events.EventStop:
		ts.NextAction = "Task completed. Review results."

	case events.EventSessionEnd:
		ts.NextAction = "Session ended."
	}

	return ts
}

func (p *Projector) AddDecision(ts *TaskState, value string, source FactSource) {
	ts.Decisions = append(ts.Decisions, StateFact{
		Value:      value,
		Source:     source,
		Confidence: 1.0,
		CreatedAt:  time.Now(),
	})
}

func (p *Projector) AddCompletedWork(ts *TaskState, work string) {
	ts.CompletedWork = append(ts.CompletedWork, work)
	ts.UpdatedAt = time.Now()
}

func (p *Projector) MarkTestRun(ts *TaskState, run TestRun) {
	ts.TestRuns = append(ts.TestRuns, run)
	if run.Failed > 0 {
		for _, id := range run.FailingTestIDs {
			issue := fmt.Sprintf("Test failure: %s", id)
			if !contains(ts.UnresolvedIssues, issue) {
				ts.UnresolvedIssues = append(ts.UnresolvedIssues, issue)
			}
		}
	} else {
		ts.UnresolvedIssues = nil
	}
	ts.UpdatedAt = time.Now()
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func getExitCode(evt *events.HarnessEvent) int {
	if code, ok := evt.ExecutionMetadata["exit_code"]; ok {
		var ec int
		fmt.Sscanf(code, "%d", &ec)
		return ec
	}
	return 0
}
