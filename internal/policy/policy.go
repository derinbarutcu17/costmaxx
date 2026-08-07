// Package policy implements CostMax's output-recommendation policy: deciding
// whether raw command output should be passed through, reduced, or stored as
// an artifact, and rendering the model-visible envelope that carries the
// recommendation.
package policy

import (
	"fmt"

	"github.com/derinbarutcu17/costmaxx/internal/events"
)

type Recommendation string

const (
	RecommendationReduce           Recommendation = "reduce"
	RecommendationPassthrough      Recommendation = "passthrough"
	RecommendationPreserveFull     Recommendation = "preserve_full"
	RecommendationArtifactRequired Recommendation = "artifact_required"
)

// WrapperOverheadTokens is a conservative estimate for the fixed portion of
// CostMax's result envelope (exit, token counts, and artifact reference).
// The pipeline performs a final full-response check as well because command
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
// fully rendered response (including its metadata envelope) is not actually
// smaller than the raw command output.
func GuardRecommendation(recommendation Recommendation, rawTokens, fullResponseTokens int) Recommendation {
	if (recommendation == RecommendationReduce || recommendation == RecommendationArtifactRequired) &&
		fullResponseTokens >= rawTokens {
		return RecommendationPassthrough
	}
	return recommendation
}

// FormatToolOutput renders the model-visible result envelope. The layout is
// part of CostMax's observable contract: MCP tool results and the CLI
// artifact add command emit the same envelope bytes.
func FormatToolOutput(recommendation Recommendation, command string, exitCode, rawTokens, modelTokens int, artifactID, modelText string) string {
	return fmt.Sprintf("[costmax_run] Recommendation: %s\nOutput mode: %s\nCommand: %s\nExit: %d\nRaw tokens: %d\nModel-visible tokens: %d\nArtifact ID: %s\nArtifact URI: cmx://artifact/%s\n---\n%s",
		recommendation, outputMode(recommendation), command, exitCode, rawTokens, modelTokens, artifactID, artifactID, modelText)
}

func outputMode(recommendation Recommendation) string {
	if recommendation == RecommendationReduce || recommendation == RecommendationArtifactRequired {
		return "compact summary"
	}
	return "unmodified output"
}
