package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/tokens"
)

func TestEstimateASCII(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"single char", "a", 0},
		{"short", "hello", 1},
		{"1000 ascii", strings.Repeat("a", 1000), 250},
	}
	for _, c := range cases {
		if got := tokens.Estimate(c.in); got != c.want {
			t.Errorf("%s: Estimate(%q) = %d, want %d", c.name, c.in, got, c.want)
		}
	}
}

// CJK text is 3 bytes per char in UTF-8. len/4 yields 0.75 tokens per char,
// which undercounts real tokenizers (roughly 1-2 tokens per CJK char). This
// test documents the behavior so the policy consequence is explicit: CJK-heavy
// output can be declared "reduced" when the token saving is smaller than
// claimed, because both sides of the comparison use the same estimator.
func TestEstimateCJKUndercounts(t *testing.T) {
	in := strings.Repeat("测试中文", 200) // 800 chars, 2400 bytes
	got := tokens.Estimate(in)
	if got != 600 {
		t.Fatalf("Estimate(CJK 2400 bytes) = %d, want 600 (len/4)", got)
	}
	if len(in)/4 != got {
		t.Fatalf("estimator is exactly len/4, expected %d", len(in)/4)
	}
}

func TestEstimateEmojiAndANSI(t *testing.T) {
	// 4-byte emoji: len/4 = 1 token per emoji, overcounts real tokenizers.
	if got := tokens.Estimate("🚀🚀🚀🚀"); got != 4 {
		t.Errorf("emoji estimate = %d, want 4", got)
	}
	// ANSI escapes inflate byte count; estimator counts escape bytes as tokens.
	ansi := "\x1b[31mred\x1b[0m"
	if got := tokens.Estimate(ansi); got != len(ansi)/4 {
		t.Errorf("ANSI estimate = %d, want %d", got, len(ansi)/4)
	}
}

func TestEstimateDeterministic(t *testing.T) {
	in := strings.Repeat("deterministic input line\n", 50)
	if tokens.Estimate(in) != tokens.Estimate(in) {
		t.Error("Estimate is not deterministic")
	}
}

func TestEstimateBytesMatchesText(t *testing.T) {
	in := strings.Repeat("bytes", 100)
	if tokens.Estimate(in) != tokens.EstimateBytes([]byte(in)) {
		t.Error("Estimate and EstimateBytes disagree")
	}
}

func TestBudgetOK(t *testing.T) {
	if !tokens.BudgetOK("short", 100) {
		t.Error("short text should fit a 100 token budget")
	}
	if tokens.BudgetOK(strings.Repeat("x", 4000), 100) {
		t.Error("1000 char text should not fit a 100 token budget")
	}
}
