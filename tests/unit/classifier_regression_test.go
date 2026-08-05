package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
)

func TestClassifierDoesNotTreatSingleTypeScriptWarningAsBuild(t *testing.T) {
	c := events.NewClassifier()
	got := c.Classify("mcp_costmax_run", "sh eslint-warnings --fake", "src/module.ts:1:1 warning generated lint output\n", 0, 64)
	if got == events.OutputBuild {
		t.Fatalf("single TypeScript warning was misclassified as build")
	}
}
