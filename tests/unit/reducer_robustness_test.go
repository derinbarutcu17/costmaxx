package unit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
)

func registry(t *testing.T) *reducers.Registry {
	t.Helper()
	return reducers.NewRegistry(config.Default())
}

type reducerCase struct {
	category string
	command  string
	exit     int
	input    string
}

// Each category through the real registry selection path with hostile inputs.
// Assert: no panic, no error, deterministic output.
func TestReducersRobustness(t *testing.T) {
	reg := registry(t)
	base := []reducerCase{
		{"test", "go test ./...", 1, "--- FAIL: TestThing\nFAIL\nok"},
		{"build", "go build ./...", 1, "# pkg\n./main.go:5:10: undefined: x"},
		{"diff", "git diff", 0, "diff --git a/f b/f\nindex 1..2\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-x\n+y"},
		{"search", "rg foo .", 0, "file1.go:10:foo\nfile2.go:20:bar foo baz"},
		{"lint", "eslint .", 1, "/a.js:1:1 error no-unused-vars\n1 problem"},
		{"json", "curl api", 0, `{"items":[{"id":1},{"id":2}],"total":2}`},
		{"terminal", "run.sh", 0, "line one\nline two\nline three\n"},
		{"generic", "cat file", 0, "some unstructured text\nmore text\n"},
	}

	inputs := map[string]string{
		"empty":      "",
		"whitespace": "\n\n  \n	\n",
		"ansi":       "\x1b[31mFAIL\x1b[0m \x1b[1mTestThing\x1b[0m\n",
		"unicode":    "测试 TestThing ✓ ✗ ünïcödé\n",
		"threshold":  strings.Repeat("line of output\n", 100), // ~1400 bytes, over the 1000 size gate
		"malformed":  "not what the regexes expect at all\n" + strings.Repeat("x", 500),
		"one line":   "just one line",
		"large":      strings.Repeat("some output line\n", 180), // ~3400 bytes, over every gate
	}

	for _, c := range base {
		for inName, in := range inputs {
			sel := reg.Select(events.OutputCategory(c.category), c.command, c.exit, int64(len(in)))
			if sel == nil {
				// terminal gates at >1000 bytes, generic at >2000: small
				// inputs with no reducer are by design (passthrough path).
				if len(in) >= 2000 {
					t.Errorf("%s/%s: no reducer selected for %d bytes", c.category, inName, len(in))
				}
				continue
			}
			meta := artifacts.ReducerMetadata{
				Command:  c.command,
				ExitCode: c.exit,
				Category: c.category,
				ToolName: "art-test",
				Size:     int64(len(in)),
			}
			r1, err1 := sel.Reduce(in, meta)
			r2, err2 := sel.Reduce(in, meta)
			if err1 != nil || err2 != nil {
				t.Errorf("%s/%s: Reduce error: %v / %v", c.category, inName, err1, err2)
				continue
			}
			if r1 == nil || r2 == nil {
				t.Errorf("%s/%s: nil reduction record", c.category, inName)
				continue
			}
			if !bytes.Equal([]byte(r1.CompactContent), []byte(r2.CompactContent)) {
				t.Errorf("%s/%s: non-deterministic output", c.category, inName)
			}
			// A reducer must never claim a saving it does not deliver at the
			// record level; the policy guard is the backstop, but a reducer
			// growing output for empty-ish input is a smell worth recording.
			if len(in) > 0 && len(r1.CompactContent) > len(in)*2 {
				t.Logf("%s/%s: compact (%d) much larger than input (%d), guard will handle", c.category, inName, len(r1.CompactContent), len(in))
			}
		}
	}
}
