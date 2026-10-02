package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
)

var clf = events.NewClassifier()

func TestFindWithoutNameIsNotSearch(t *testing.T) {
	// `find` is a search command regardless of the -name flag; token matching
	// fixes the old operator-precedence false negative.
	if got := clf.Classify("Bash", "find . -type f", "", 0, 100); got != events.OutputSearch {
		t.Errorf("find without -name = %q, want search", got)
	}
}

func TestFindWithNameIsSearch(t *testing.T) {
	if got := clf.Classify("Bash", "find . -name '*.go'", "", 0, 100); got != events.OutputSearch {
		t.Errorf("find -name = %q, want search", got)
	}
}

// Token matching: a path containing "test" or "build" must NOT trip the
// test/build classifiers (the old substring matcher did).
func TestPathWithTestSubstringIsFalsePositive(t *testing.T) {
	if got := clf.Classify("Bash", "cd /tmp/test-data && ls", "", 0, 2000); got == events.OutputTest {
		t.Errorf("path with test-data classified as test (false positive)")
	}
	if got := clf.Classify("Bash", "cat /var/log/test-output.log", "", 0, 2000); got == events.OutputTest {
		t.Errorf("path with test-output classified as test (false positive)")
	}
}

func TestPathWithBuildSubstringIsFalsePositive(t *testing.T) {
	if got := clf.Classify("Bash", "cat ~/build-notes.md", "", 0, 2000); got == events.OutputBuild {
		t.Errorf("path with build-notes classified as build (false positive)")
	}
	if got := clf.Classify("Bash", "ls /opt/build-system/tmp", "", 0, 2000); got == events.OutputBuild {
		t.Errorf("path with build-system classified as build (false positive)")
	}
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
	// Leading whitespace must not defeat the JSON signature; the classifier
	// trims before deciding the shape.
	got := clf.Classify("Bash", "curl api", "  {\"a\":1}\n", 0, 20)
	if got != events.OutputJSON {
		t.Errorf("whitespace-prefixed JSON = %q, want json", got)
	}
	if got := clf.Classify("Bash", "curl api", "\n[\n1,\n2\n]\n", 0, 20); got != events.OutputJSON {
		t.Errorf("whitespace-prefixed array = %q, want json", got)
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

// A malformed PostToolUse (or direct caller) can pass an empty or
// whitespace-only tool name. The classifier must not panic on
// strings.Fields(tool)[0]; it falls back to the command/output signature and
// must still return the category the command actually produced.
func TestEmptyToolNameDoesNotPanic(t *testing.T) {
	for _, tool := range []string{"", "   ", "\t\n "} {
		got := clf.Classify(tool, "go test ./...", strings.Repeat("=== RUN   TestX\n--- FAIL: TestX\nFAIL\n", 20), 1, 2000)
		if got != events.OutputTest {
			t.Errorf("empty tool %q with test command = %q, want test", tool, got)
		}
	}
}

func TestWhitespaceToolNameStillClassifiesFromSignature(t *testing.T) {
	// No command hint (empty command): the signature alone must decide, and
	// an empty tool name must not corrupt it.
	jsonOut := "{\"name\":\"x\",\"value\":1}\n"
	got := clf.Classify("  ", "", jsonOut, 0, int64(len(jsonOut)))
	if got != events.OutputJSON {
		t.Errorf("whitespace tool with JSON output = %q, want json", got)
	}

	// A non-empty command still routes through the token matcher even when the
	// tool name is whitespace-only.
	got = clf.Classify("   ", "git diff HEAD~1", "", 0, 1000)
	if got != events.OutputDiff {
		t.Errorf("whitespace tool with diff command = %q, want diff", got)
	}
}

func TestToolNameWithPathBase(t *testing.T) {
	// The tool name is normalized through filepath.Base before keyword checks,
	// so a full path must classify from the binary name, not the directory.
	if got := clf.Classify("/usr/local/bin/go", "go test ./...", strings.Repeat("ok\n", 300), 0, 1200); got != events.OutputTest {
		t.Errorf("path tool name with test command = %q, want test", got)
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
	// Bare binary names were a trailing-space false negative before token
	// matching; they must now classify as test.
	for _, cmd := range []string{"jest", "mocha", "rspec", "vitest"} {
		if got := clf.Classify("Bash", cmd, strings.Repeat("x", 500), 0, 500); got != events.OutputTest {
			t.Errorf("bare %q = %q, want test", cmd, got)
		}
	}
}
