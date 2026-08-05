package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
)

func TestClassifyTestOutput(t *testing.T) {
	c := events.NewClassifier()
	cat := c.Classify("jest", "npm test", "Tests: 1 passed, 1 failed\n● auth test", 1, 100)
	if cat != events.OutputTest {
		t.Errorf("expected test, got %s", cat)
	}
}

func TestClassifyBuildOutput(t *testing.T) {
	c := events.NewClassifier()
	cat := c.Classify("tsc", "npx tsc --noEmit", "error TS2322: Type 'X' is not assignable to type 'Y'", 2, 100)
	if cat != events.OutputBuild {
		t.Errorf("expected build, got %s", cat)
	}
}

func TestClassifyDiffOutput(t *testing.T) {
	c := events.NewClassifier()
	cat := c.Classify("git", "git diff", "diff --git a/src/main.ts b/src/main.ts", 0, 100)
	if cat != events.OutputDiff {
		t.Errorf("expected diff, got %s", cat)
	}
}

func TestClassifySearchOutput(t *testing.T) {
	c := events.NewClassifier()
	cat := c.Classify("rg", "rg TODO", "src/main.ts:42: // TODO", 0, 100)
	if cat != events.OutputSearch {
		t.Errorf("expected search, got %s", cat)
	}
}

func TestClassifyJSONOutput(t *testing.T) {
	c := events.NewClassifier()
	cat := c.Classify("curl", "curl API", `{"key": "value"}`, 0, 100)
	if cat != events.OutputJSON {
		t.Errorf("expected json, got %s", cat)
	}
}

func TestClassifyGenericOutput(t *testing.T) {
	c := events.NewClassifier()
	cat := c.Classify("echo", "echo hello", "hello world", 0, 50)
	if cat != events.OutputTerminal {
		t.Errorf("expected terminal, got %s", cat)
	}
}
