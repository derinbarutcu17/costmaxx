package mcp

import "github.com/derinbarutcu17/costmaxx/internal/events"

type Recommendation string

const (
	RecommendationReduce           Recommendation = "reduce"
	RecommendationPassthrough      Recommendation = "passthrough"
	RecommendationPreserveFull     Recommendation = "preserve_full"
	RecommendationArtifactRequired Recommendation = "artifact_required"
)

// WrapperOverheadTokens is a conservative estimate for the fixed portion of
// CostMax's result envelope (exit, token counts, and artifact reference).
// The server performs a final full-response check as well because command
// names and artifact IDs make the envelope variable.
const WrapperOverheadTokens = 80

func Recommend(category events.OutputCategory, rawTokens, compactTokens int, hasReducer bool) Recommendation {
	if category == events.OutputBinary || !hasReducer {
		return RecommendationPreserveFull
	}
	if rawTokens <= 2*WrapperOverheadTokens || compactTokens+WrapperOverheadTokens >= rawTokens {
		return RecommendationPassthrough
	}
	if category == events.OutputTerminal || category == events.OutputGeneric {
		return RecommendationArtifactRequired
	}
	return RecommendationReduce
}

// GuardRecommendation prevents the policy from claiming a saving when the
// fully rendered MCP response (including its metadata envelope) is not
// actually smaller than the raw command output.
func GuardRecommendation(recommendation Recommendation, rawTokens, fullResponseTokens int) Recommendation {
	if (recommendation == RecommendationReduce || recommendation == RecommendationArtifactRequired) &&
		fullResponseTokens >= rawTokens {
		return RecommendationPassthrough
	}
	return recommendation
}
