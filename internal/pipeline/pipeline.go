// Package pipeline implements the shared command-output ingestion chain used
// by both the MCP costmax_run tool and the CLI artifact add command: redact,
// store evidence, classify, reduce, apply the recommendation policy, and
// persist session metrics. Both callers emit byte-identical envelopes for
// identical inputs.
package pipeline

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/policy"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

// Deps carries the concrete dependencies of the ingestion chain. Callers keep
// ownership of the shared instances (the MCP server reuses its cached
// classifier/registry/redactor; the CLI news up fresh ones per invocation).
type Deps struct {
	Store      *artifacts.Store
	DB         *store.DB
	Classifier *events.Classifier
	Registry   *reducers.Registry
	Redactor   *privacy.Redactor
	SessionID  string
}

// Process ingests raw command output and returns the rendered envelope string
// (the output of policy.FormatToolOutput). The toolTag is recorded in the
// classifier call ("mcp_costmax_run" for the MCP tool, "cli_artifact_add" for
// the CLI) and is the only input that varies the classification.
func Process(d Deps, output, command, cwd string, exitCode int, toolTag string) (string, error) {
	if d.Redactor.ContainsSecrets(output) {
		output = d.Redactor.RedactOutput(output)
	}

	// Store raw evidence
	artifact, storeErr := d.Store.Store([]byte(output), uuid.New().String(), command, cwd, exitCode)
	if storeErr != nil {
		return "", fmt.Errorf("store artifact: %w", storeErr)
	}

	if err := d.DB.InsertArtifact(artifact); err != nil {
		return "", fmt.Errorf("insert artifact metadata: %w", err)
	}

	// Classify and reduce
	category := d.Classifier.Classify(toolTag, command, output, exitCode, int64(len(output)))
	reducer := d.Registry.Select(category, command, exitCode, int64(len(output)))

	compactText := output
	compactTokens := len(output) / 4
	reduced := false
	var reduction *artifacts.ReductionRecord

	if reducer != nil {
		red, redErr := reducer.Reduce(output, artifacts.ReducerMetadata{
			Command:  command,
			ExitCode: exitCode,
			Category: string(category),
			ToolName: artifact.ArtifactID,
			Size:     int64(len(output)),
		})
		if redErr == nil {
			// Reducers use deterministic IDs for unit-test readability. A live
			// artifact may be reduced more than once with the same byte length,
			// so persist a per-artifact ID to avoid silently colliding on the
			// reduction_records primary key.
			reduction = red
			red.ReductionID = "red-" + artifact.ArtifactID
			red.ArtifactID = artifact.ArtifactID
			if err := d.DB.InsertReduction(red); err != nil {
				return "", fmt.Errorf("insert reduction metadata: %w", err)
			}
			compactText = red.CompactContent
			compactTokens = red.CompactTokenEst
			reduced = true
		}
	}

	rawTokens := len(output) / 4
	recommendation := policy.Recommend(category, rawTokens, compactTokens, reduced)
	modelText := compactText
	modelTokens := compactTokens
	if recommendation == policy.RecommendationPassthrough || recommendation == policy.RecommendationPreserveFull {
		modelText = output
		modelTokens = rawTokens
	}

	// The receipt summarizes the final model-visible state so the model can
	// act without fetching the artifact. It is computed from the final text
	// and re-computed only if the guard downgrades the recommendation below.
	var failedTests []string
	if reduction != nil {
		failedTests = reduction.StructuredFacts
	}
	receipt := policy.FormatReceipt(lineCount(modelText), lineCount(output), len(output)-len(modelText), failedTests, artifact.ArtifactID, modelText != output)
	responseText := policy.FormatToolOutput(recommendation, command, exitCode, rawTokens, modelTokens, artifact.ArtifactID, modelText, receipt)
	// The policy uses a conservative envelope estimate, but the command text
	// itself is variable. Re-check the fully rendered response before returning
	// a reduction recommendation so a long command can never create a false
	// saving.
	guarded := policy.GuardRecommendation(recommendation, rawTokens, len(responseText)/4)
	if guarded != recommendation {
		recommendation = guarded
		modelText = output
		modelTokens = rawTokens
		receipt = policy.FormatReceipt(0, 0, 0, nil, artifact.ArtifactID, false)
		responseText = policy.FormatToolOutput(recommendation, command, exitCode, rawTokens, modelTokens, artifact.ArtifactID, modelText, receipt)
	}

	// Persist session metrics
	if err := d.DB.InsertSessionMetrics(d.SessionID, rawTokens, modelTokens, 1, 1); err != nil {
		return "", fmt.Errorf("insert session metrics: %w", err)
	}

	return responseText, nil
}

// lineCount returns the number of lines in s, matching the receipt's
// "kept N/M lines" semantics (a trailing newline does not add a line).
func lineCount(s string) int {
	return strings.Count(s, "\n") + 1
}
