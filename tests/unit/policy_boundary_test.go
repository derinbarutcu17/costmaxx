package unit

import (
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/policy"
)

func TestRecommendBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		category     events.OutputCategory
		raw, compact int
		hasReducer   bool
		want         policy.Recommendation
	}{
		{"raw at 2x overhead", events.OutputTest, 160, 20, true, policy.RecommendationPassthrough},
		{"raw just above 2x overhead", events.OutputTest, 161, 20, true, policy.RecommendationReduce},
		{"compact+overhead == raw", events.OutputTest, 200, 120, true, policy.RecommendationPassthrough},
		{"compact+overhead one below raw", events.OutputTest, 200, 119, true, policy.RecommendationReduce},
		{"binary with reducer", events.OutputBinary, 10000, 50, true, policy.RecommendationPreserveFull},
		{"no reducer huge", events.OutputTest, 10000, 10000, false, policy.RecommendationPreserveFull},
		{"terminal category", events.OutputTerminal, 1000, 50, true, policy.RecommendationArtifactRequired},
		{"generic category", events.OutputGeneric, 1000, 50, true, policy.RecommendationArtifactRequired},
		{"test category reduces", events.OutputTest, 1000, 50, true, policy.RecommendationReduce},
	}
	for _, c := range cases {
		if got := policy.Recommend(c.category, c.raw, c.compact, c.hasReducer); got != c.want {
			t.Errorf("%s: Recommend = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGuardDowngradesWhenRenderedNotShorter(t *testing.T) {
	// Rendered response (includes command text and envelope) >= raw: must
	// downgrade a reduction to passthrough.
	got := policy.GuardRecommendation(policy.RecommendationReduce, 100, 150)
	if got != policy.RecommendationPassthrough {
		t.Errorf("GuardRecommendation = %q, want passthrough", got)
	}
}

func TestGuardKeepsGenuineSaving(t *testing.T) {
	got := policy.GuardRecommendation(policy.RecommendationReduce, 1000, 150)
	if got != policy.RecommendationReduce {
		t.Errorf("GuardRecommendation = %q, want reduce", got)
	}
}

func TestGuardIsIdentityForNonReduction(t *testing.T) {
	for _, r := range []policy.Recommendation{policy.RecommendationPassthrough, policy.RecommendationPreserveFull} {
		if got := policy.GuardRecommendation(r, 100, 1000); got != r {
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
	envelope := len(longCmd)/4 + policy.WrapperOverheadTokens
	got := policy.GuardRecommendation(policy.RecommendationReduce, rawTokens, envelope)
	if got != policy.RecommendationPassthrough {
		t.Errorf("long command envelope (%d) >= raw (%d): want passthrough, got %q", envelope, rawTokens, got)
	}
}
