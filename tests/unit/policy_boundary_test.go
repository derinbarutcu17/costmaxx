package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/mcp"
)

func TestRecommendBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		category     events.OutputCategory
		raw, compact int
		hasReducer   bool
		want         mcp.Recommendation
	}{
		{"raw at 2x overhead", events.OutputTest, 160, 20, true, mcp.RecommendationPassthrough},
		{"raw just above 2x overhead", events.OutputTest, 161, 20, true, mcp.RecommendationReduce},
		{"compact+overhead == raw", events.OutputTest, 200, 120, true, mcp.RecommendationPassthrough},
		{"compact+overhead one below raw", events.OutputTest, 200, 119, true, mcp.RecommendationReduce},
		{"binary with reducer", events.OutputBinary, 10000, 50, true, mcp.RecommendationPreserveFull},
		{"no reducer huge", events.OutputTest, 10000, 10000, false, mcp.RecommendationPreserveFull},
		{"terminal category", events.OutputTerminal, 1000, 50, true, mcp.RecommendationArtifactRequired},
		{"generic category", events.OutputGeneric, 1000, 50, true, mcp.RecommendationArtifactRequired},
		{"test category reduces", events.OutputTest, 1000, 50, true, mcp.RecommendationReduce},
	}
	for _, c := range cases {
		if got := mcp.Recommend(c.category, c.raw, c.compact, c.hasReducer); got != c.want {
			t.Errorf("%s: Recommend = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGuardDowngradesWhenRenderedNotShorter(t *testing.T) {
	// Rendered response (includes command text and envelope) >= raw: must
	// downgrade a reduction to passthrough.
	got := mcp.GuardRecommendation(mcp.RecommendationReduce, 100, 150)
	if got != mcp.RecommendationPassthrough {
		t.Errorf("GuardRecommendation = %q, want passthrough", got)
	}
}

func TestGuardKeepsGenuineSaving(t *testing.T) {
	got := mcp.GuardRecommendation(mcp.RecommendationReduce, 1000, 150)
	if got != mcp.RecommendationReduce {
		t.Errorf("GuardRecommendation = %q, want reduce", got)
	}
}

func TestGuardIsIdentityForNonReduction(t *testing.T) {
	for _, r := range []mcp.Recommendation{mcp.RecommendationPassthrough, mcp.RecommendationPreserveFull} {
		if got := mcp.GuardRecommendation(r, 100, 1000); got != r {
			t.Errorf("GuardRecommendation(%q) = %q, want identity", r, got)
		}
	}
}

// The guard exists because a long command string inside the envelope can
// consume the saving. Reproduce the exact failure mode at the policy level.
func TestGuardLongCommandPushesOverRaw(t *testing.T) {
	rawTokens := 200
	// 4KB command = 1024 estimated tokens, more than the raw output alone.
	longCmd := "echo " + strings.Repeat("x", 4096)
	envelope := len(longCmd)/4 + mcp.WrapperOverheadTokens
	got := mcp.GuardRecommendation(mcp.RecommendationReduce, rawTokens, envelope)
	if got != mcp.RecommendationPassthrough {
		t.Errorf("long command envelope (%d) >= raw (%d): want passthrough, got %q", envelope, rawTokens, got)
	}
}
