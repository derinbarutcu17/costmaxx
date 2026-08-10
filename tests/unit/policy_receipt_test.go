package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/policy"
)

func TestFormatReceiptReducedWithTests(t *testing.T) {
	got := policy.FormatReceipt(3, 120, 4096, []string{"TestAuthSession", "TestRefreshToken"}, "art-1", true)
	want := "Receipt: kept 3/120 lines | dropped 4096 B | tests failed: TestAuthSession, TestRefreshToken | replay: costmaxx replay art-1"
	if got != want {
		t.Errorf("FormatReceipt:\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatReceiptReducedWithTestsCapsAtFive(t *testing.T) {
	names := []string{"T1", "T2", "T3", "T4", "T5", "T6", "T7"}
	got := policy.FormatReceipt(1, 10, 100, names, "art-2", true)
	want := "Receipt: kept 1/10 lines | dropped 100 B | tests failed: T1, T2, T3, T4, T5, +2 more | replay: costmaxx replay art-2"
	if got != want {
		t.Errorf("FormatReceipt (cap):\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatReceiptReducedExactlyFiveTests(t *testing.T) {
	names := []string{"T1", "T2", "T3", "T4", "T5"}
	got := policy.FormatReceipt(1, 10, 100, names, "art-3", true)
	want := "Receipt: kept 1/10 lines | dropped 100 B | tests failed: T1, T2, T3, T4, T5 | replay: costmaxx replay art-3"
	if got != want {
		t.Errorf("FormatReceipt (no cap):\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatReceiptReducedWithoutTests(t *testing.T) {
	got := policy.FormatReceipt(5, 200, 2754, nil, "art-4", true)
	want := "Receipt: kept 5/200 lines | dropped 2754 B | replay: costmaxx replay art-4"
	if got != want {
		t.Errorf("FormatReceipt (no tests):\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatReceiptNotReduced(t *testing.T) {
	got := policy.FormatReceipt(0, 0, 0, nil, "art-5", false)
	want := "Receipt: replay: costmaxx replay art-5"
	if got != want {
		t.Errorf("FormatReceipt (not reduced):\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatToolOutputPlacesReceiptBeforeSeparator(t *testing.T) {
	got := policy.FormatToolOutput(policy.RecommendationReduce, "go test", 1, 100, 25, "art-6", "compact text", "Receipt: kept 1/2 lines | dropped 100 B | replay: costmaxx replay art-6")
	want := "[costmax_run] Recommendation: reduce\n" +
		"Output mode: compact summary\n" +
		"Command: go test\n" +
		"Exit: 1\n" +
		"Raw tokens: 100\n" +
		"Model-visible tokens: 25\n" +
		"Artifact ID: art-6\n" +
		"Artifact URI: cmx://artifact/art-6\n" +
		"Receipt: kept 1/2 lines | dropped 100 B | replay: costmaxx replay art-6\n" +
		"---\n" +
		"compact text"
	if got != want {
		t.Errorf("FormatToolOutput with receipt:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatToolOutputOmitsEmptyReceipt(t *testing.T) {
	got := policy.FormatToolOutput(policy.RecommendationPassthrough, "echo hi", 0, 10, 10, "art-7", "out", "")
	want := "[costmax_run] Recommendation: passthrough\n" +
		"Output mode: unmodified output\n" +
		"Command: echo hi\n" +
		"Exit: 0\n" +
		"Raw tokens: 10\n" +
		"Model-visible tokens: 10\n" +
		"Artifact ID: art-7\n" +
		"Artifact URI: cmx://artifact/art-7\n" +
		"---\n" +
		"out"
	if got != want {
		t.Errorf("FormatToolOutput without receipt:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatReceiptDedupesFailedTests(t *testing.T) {
	dup := []string{"TestA", "TestB", "TestA", "TestB", "TestC", "TestA"}
	r := policy.FormatReceipt(10, 100, 500, dup, "art-1", true)
	if strings.Count(r, "TestA") != 1 || strings.Count(r, "TestB") != 1 {
		t.Errorf("duplicate test names not deduped: %s", r)
	}
	if !strings.Contains(r, "tests failed: TestA, TestB, TestC") {
		t.Errorf("deduped list wrong: %s", r)
	}
	if strings.Contains(r, "+") {
		t.Errorf("dedup should drop count below cap, got: %s", r)
	}
}

func TestFormatReceiptDedupeThenCap(t *testing.T) {
	names := []string{"A", "B", "C", "D", "E", "F", "G", "A", "B"}
	r := policy.FormatReceipt(10, 100, 500, names, "art-1", true)
	if !strings.Contains(r, "A, B, C, D, E, +2 more") {
		t.Errorf("dedupe-then-cap wrong: %s", r)
	}
}
