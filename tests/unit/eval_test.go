package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type evalCase struct {
	ID                      string            `json:"id"`
	Description             string            `json:"description"`
	Files                   map[string]string `json:"files"`
	Prompt                  string            `json:"prompt"`
	ExpectedAnswer          []string          `json:"expected_answer"`
	ExpectedCommand         string            `json:"expected_command"`
	ExpectedExitCode        int               `json:"expected_exit_code"`
	ExpectedActiveBehaviour string            `json:"expected_active_behaviour"`
	ReducerTarget           string            `json:"reducer_target"`
}

func TestEvalFixturesValid(t *testing.T) {
	casesDir := filepath.Join("..", "..", "benchmarks", "eval-cases")
	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(casesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: read: %v", entry.Name(), err)
		}

		var c evalCase
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("%s: parse: %v", entry.Name(), err)
		}

		if c.ID == "" {
			t.Errorf("%s: missing id", entry.Name())
		}
		if len(c.Files) == 0 {
			t.Errorf("%s: missing files", entry.Name())
		}
		if c.Prompt == "" {
			t.Errorf("%s: missing prompt", entry.Name())
		}
		if len(c.ExpectedAnswer) == 0 {
			t.Errorf("%s: missing expected_answer", entry.Name())
		}
		if c.ExpectedCommand == "" {
			t.Errorf("%s: missing expected_command", entry.Name())
		}
		if c.ReducerTarget == "" {
			t.Errorf("%s: missing reducer_target", entry.Name())
		}
		if c.ExpectedActiveBehaviour == "" {
			t.Errorf("%s: missing expected_active_behaviour", entry.Name())
		}

		// Verify files content is non-empty
		for fpath, content := range c.Files {
			if len(content) == 0 {
				t.Errorf("%s: file %s is empty", entry.Name(), fpath)
			}
		}
	}
}
