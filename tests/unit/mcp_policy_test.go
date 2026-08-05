package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/mcp"
)

func TestMCPRecommendationPolicy(t *testing.T) {
	tests := []struct {
		name string
		cat  events.OutputCategory
		raw  int
		out  int
		has  bool
		want mcp.Recommendation
	}{
		{"short recognized output passes through", events.OutputTest, 100, 20, true, mcp.RecommendationPassthrough},
		{"long known output reduces", events.OutputTest, 600, 120, true, mcp.RecommendationReduce},
		{"unknown short output preserves full", events.OutputTerminal, 100, 100, false, mcp.RecommendationPreserveFull},
		{"lossy terminal summary requires artifact", events.OutputTerminal, 600, 120, true, mcp.RecommendationArtifactRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcp.Recommend(tt.cat, tt.raw, tt.out, tt.has); got != tt.want {
				t.Fatalf("Recommend() = %q, want %q", got, tt.want)
			}
		})
	}
}
