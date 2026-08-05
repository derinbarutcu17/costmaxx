package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/context"
	"github.com/derinbarutcu17/costmaxx/internal/state"
)

func TestComposerProducesOutput(t *testing.T) {
	ts := &state.TaskState{
		Objective:        "Fix auth tests",
		UnresolvedIssues: []string{"auth failure"},
		Decisions: []state.StateFact{
			{Value: "Use env vars", Source: state.FactAgentReported},
		},
		NextAction: "Update config",
	}
	c := context.NewComposer(4000)
	out := c.Compose(ts)
	if !strings.Contains(out, "Fix auth tests") {
		t.Error("expected objective in output")
	}
	if !strings.Contains(out, "Use env vars") {
		t.Error("expected decision in output")
	}
}

func TestComposerRespectsBudget(t *testing.T) {
	ts := &state.TaskState{
		Objective:        "X",
		UnresolvedIssues: make([]string, 1000),
		NextAction:       "Y",
	}
	for i := 0; i < 1000; i++ {
		ts.UnresolvedIssues[i] = "very long issue description that should consume token budget rapidly"
	}
	c := context.NewComposer(100)
	out := c.Compose(ts)
	tokens := len(out) / 4
	if tokens > 150 {
		t.Errorf("expected budget-limited output, got ~%d tokens", tokens)
	}
}
