package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
)

var clf = events.NewClassifier()

func TestFindWithoutNameIsNotSearch(t *testing.T) {
	// Operator precedence: "find X && contains(name)" means plain `find`
	// without "name" never classifies as search. Document the behavior.
	got := clf.Classify("Bash", "find . -type f", "", 0, 100)
	if got == events.OutputSearch {
		t.Log("find without -name classifies as search (precedence changed?)")
	} else {
		t.Logf("find without -name -> %q (current behavior)", got)
	}
}

func TestFindWithNameIsSearch(t *testing.T) {
	if got := clf.Classify("Bash", "find . -name '*.go'", "", 0, 100); got != events.OutputSearch {
		t.Errorf("find -name = %q, want search", got)
	}
}

// Substring matching: a path containing "test" trips the test classifier.
func TestPathWithTestSubstringIsFalsePositive(t *testing.T) {
	got := clf.Classify("Bash", "cd /tmp/test-data && ls", "", 0, 2000)
	t.Logf("command with test-data path -> %q (potential false positive)", got)
}

func TestPathWithBuildSubstringIsFalsePositive(t *testing.T) {
	got := clf.Classify("Bash", "cat ~/build-notes.md", "", 0, 2000)
	t.Logf("command with build path -> %q (potential false positive)", got)
}

func TestDiffVariants(t *testing.T) {
	if got := clf.Classify("Bash", "git diff", "", 0, 1000); got != events.OutputDiff {
		t.Errorf("git diff = %q, want diff", got)
	}
	// difftool starts with "git diff" so it also lands in diff. Acceptable
	// (output is a diff) but worth recording.
	if got := clf.Classify("Bash", "git difftool", "", 0, 1000); got != events.OutputDiff {
		t.Errorf("git difftool = %q, want diff", got)
	}
}

func TestGrepVariants(t *testing.T) {
	if got := clf.Classify("Bash", "grep -r foo .", "", 0, 1000); got != events.OutputSearch {
		t.Errorf("grep -r = %q, want search", got)
	}
}

func TestJSONSignatureNeedsLeadingBrace(t *testing.T) {
	// Leading whitespace defeats the JSON signature; falls through to terminal.
	got := clf.Classify("Bash", "curl api", "  {\"a\":1}\n", 0, 20)
	t.Logf("whitespace-prefixed JSON -> %q (documented behavior)", got)
	if got == events.OutputJSON {
		t.Errorf("expected fallthrough, got json")
	}
}

func TestBinaryOutput(t *testing.T) {
	got := clf.Classify("Bash", "head -c 100 /dev/urandom", string([]byte{0x00, 0x01, 0xFF, 0xFE, 0x00}), 0, 5)
	if got != events.OutputBinary {
		t.Errorf("binary bytes = %q, want binary", got)
	}
}

func TestZeroSizeIsGeneric(t *testing.T) {
	if got := clf.Classify("Bash", "true", "", 0, 0); got != events.OutputGeneric {
		t.Errorf("zero size = %q, want generic", got)
	}
}

func TestFewerThanThreeTestSignals(t *testing.T) {
	out := "one pass\none failed\nnothing else"
	if got := clf.Classify("Bash", "run.sh", out, 1, int64(len(out))); got == events.OutputTest {
		t.Errorf("2 signals should not classify as test, got %q", got)
	}
}

func TestTestCommandSubstrings(t *testing.T) {
	for _, cmd := range []string{"go test ./...", "go test", "jest --runInBand", "pytest", "cargo test", "npx jest --ci"} {
		if got := clf.Classify("Bash", cmd, strings.Repeat("x", 500), 0, 500); got != events.OutputTest {
			t.Errorf("%q = %q, want test", cmd, got)
		}
	}
	// False negative: the matchers require "jest " / "mocha " / "rspec " with
	// a trailing space, so the bare binary name falls through to terminal.
	for _, cmd := range []string{"jest", "mocha", "rspec"} {
		got := clf.Classify("Bash", cmd, strings.Repeat("x", 500), 0, 500)
		t.Logf("bare %q -> %q (false negative: trailing-space matcher)", cmd, got)
	}
}
