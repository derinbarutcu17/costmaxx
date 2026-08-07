package policy

import "testing"

func TestGuardRecommendationDowngradesFalseSavings(t *testing.T) {
	tests := []struct {
		name string
		rec  Recommendation
		raw  int
		full int
		want Recommendation
	}{
		{"reduce response is smaller", RecommendationReduce, 300, 200, RecommendationReduce},
		{"reduce response is not smaller", RecommendationReduce, 300, 300, RecommendationPassthrough},
		{"artifact response is not smaller", RecommendationArtifactRequired, 300, 301, RecommendationPassthrough},
		{"preserve full remains preserve full", RecommendationPreserveFull, 100, 100, RecommendationPreserveFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GuardRecommendation(tt.rec, tt.raw, tt.full); got != tt.want {
				t.Fatalf("GuardRecommendation() = %q, want %q", got, tt.want)
			}
		})
	}
}
