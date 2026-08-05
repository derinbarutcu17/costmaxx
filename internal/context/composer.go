package context

import (
	"fmt"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/state"
)

type Composer struct {
	TokenBudget int
}

func NewComposer(budget int) *Composer {
	return &Composer{TokenBudget: budget}
}

func (c *Composer) Compose(ts *state.TaskState) string {
	if ts == nil {
		return ""
	}

	var parts []string

	if ts.Objective != "" {
		parts = append(parts, fmt.Sprintf("## Objective\n%s\n", ts.Objective))
	}

	if len(ts.AcceptanceCriteria) > 0 {
		parts = append(parts, fmt.Sprintf("## Acceptance Criteria\n%s\n", bullet(ts.AcceptanceCriteria)))
	}

	if len(ts.Constraints) > 0 {
		parts = append(parts, fmt.Sprintf("## Constraints\n%s\n", bullet(ts.Constraints)))
	}

	if len(ts.UnresolvedIssues) > 0 {
		parts = append(parts, fmt.Sprintf("## Current Unresolved Issues\n%s\n", bullet(ts.UnresolvedIssues)))
	}

	if len(ts.RelevantLocations) > 0 {
		parts = append(parts, fmt.Sprintf("## Relevant Files\n%s\n", bullet(ts.RelevantLocations)))
	}

	if len(ts.Decisions) > 0 {
		var ds []string
		for _, d := range ts.Decisions {
			ds = append(ds, d.Value)
		}
		parts = append(parts, fmt.Sprintf("## Decisions\n%s\n", bullet(ds)))
	}

	if len(ts.TestRuns) > 0 {
		last := ts.TestRuns[len(ts.TestRuns)-1]
		parts = append(parts, fmt.Sprintf("## Latest Test Status\nCommand: %s | Exit: %d | %d passed, %d failed\n",
			last.Command, last.ExitCode, last.Passed, last.Failed))
		if len(last.FailingTestIDs) > 0 {
			parts = append(parts, fmt.Sprintf("Failing: %s\n", strings.Join(last.FailingTestIDs, ", ")))
		}
	}

	if ts.NextAction != "" {
		parts = append(parts, fmt.Sprintf("## Next Action\n%s\n", ts.NextAction))
	}

	block := strings.Join(parts, "\n")
	tokens := len(block) / 4

	if tokens > c.TokenBudget {
		lines := strings.Split(block, "\n")
		var trimmed []string
		used := 0
		for _, line := range lines {
			lineTokens := len(line)/4 + 1
			if used+lineTokens > c.TokenBudget {
				break
			}
			trimmed = append(trimmed, line)
			used += lineTokens
		}
		block = strings.Join(trimmed, "\n") + fmt.Sprintf("\n[context truncated to ~%d tokens]\n", c.TokenBudget)
	}

	return block
}

func bullet(items []string) string {
	var b []string
	for _, item := range items {
		b = append(b, fmt.Sprintf("- %s", item))
	}
	return strings.Join(b, "\n")
}
