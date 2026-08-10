// Package policy implements CostMax's output-recommendation policy: deciding
// whether raw command output should be passed through, reduced, or stored as
// an artifact, and rendering the model-visible envelope that carries the
// recommendation.
package policy

import (
	"fmt"
	"strings"

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
// CostMax's result envelope (exit, token counts, artifact reference, and the
// replay receipt line). The pipeline performs a final full-response check as
// well because command names and artifact IDs make the envelope variable.
const WrapperOverheadTokens = 110

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
// artifact add command emit the same envelope bytes. The receipt line (see
// FormatReceipt) is emitted on its own line between the artifact URI and the
// separator; an empty receipt emits no line.
func FormatToolOutput(recommendation Recommendation, command string, exitCode, rawTokens, modelTokens int, artifactID, modelText, receipt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[costmax_run] Recommendation: %s\nOutput mode: %s\nCommand: %s\nExit: %d\nRaw tokens: %d\nModel-visible tokens: %d\nArtifact ID: %s\nArtifact URI: cmx://artifact/%s\n",
		recommendation, outputMode(recommendation), command, exitCode, rawTokens, modelTokens, artifactID, artifactID)
	if receipt != "" {
		b.WriteString(receipt)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.WriteString(modelText)
	return b.String()
}

// FormatReceipt renders the machine-parseable receipt line so agents can act
// without retrieving the artifact. When reduced, the line always carries the
// kept/dropped line counts and dropped bytes, plus failing test names when
// known (capped at five, then "+N more"). When not reduced, only the replay
// hint is emitted.
func FormatReceipt(keptLines, rawLines, droppedBytes int, failedTests []string, artifactID string, reduced bool) string {
	if !reduced {
		return fmt.Sprintf("Receipt: replay: costmaxx replay %s", artifactID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Receipt: kept %d/%d lines | dropped %d B", keptLines, rawLines, droppedBytes)
	if len(failedTests) > 0 {
		// Dedupe: logs repeat failure lines (retries, dual reporters), and a
		// duplicated name would read as two different failures.
		seen := make(map[string]struct{}, len(failedTests))
		unique := failedTests[:0]
		for _, t := range failedTests {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			unique = append(unique, t)
		}
		b.WriteString(" | tests failed: ")
		if len(unique) <= 5 {
			b.WriteString(strings.Join(unique, ", "))
		} else {
			b.WriteString(strings.Join(unique[:5], ", "))
			fmt.Fprintf(&b, ", +%d more", len(unique)-5)
		}
	}
	fmt.Fprintf(&b, " | replay: costmaxx replay %s", artifactID)
	return b.String()
}

func outputMode(recommendation Recommendation) string {
	if recommendation == RecommendationReduce || recommendation == RecommendationArtifactRequired {
		return "compact summary"
	}
	return "unmodified output"
}
