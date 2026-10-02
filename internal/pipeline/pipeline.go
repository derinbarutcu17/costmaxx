// Package pipeline implements the shared command-output ingestion chain used
// by both the MCP costmax_run tool and the CLI artifact add command: redact,
// store evidence, classify, reduce, apply the recommendation policy, and
// persist session metrics. Both callers emit byte-identical envelopes for
// identical inputs.
package pipeline

import (
	"fmt"
	"strings"
	"time"

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
	// Harness names the call source for the immutable ledger: "mcp", "cli",
	// or "codex_hook". Empty falls back to the tool tag.
	Harness string
	// CallRef is the caller-supplied idempotency reference (JSON-RPC request
	// id, hook tool_use_id, or a fresh uuid). Retrying the same logical call
	// must re-derive the same ledger idempotency key.
	CallRef string
}

// Process ingests raw command output and returns the rendered envelope string
// (the output of policy.FormatToolOutput). The toolTag is recorded in the
// classifier call ("mcp_costmax_run" for the MCP tool, "cli_artifact_add" for
// the CLI) and is the only input that varies the classification.
func Process(d Deps, output, command, cwd string, exitCode int, toolTag string) (string, error) {
	if d.Harness == "" {
		d.Harness = toolTag
	}
	if d.Redactor.ContainsSecrets(output) {
		output = d.Redactor.RedactOutput(output)
	}
	// The command and working directory are persisted (artifact metadata,
	// ledger) and echoed in the envelope. Apply the same key/secret redaction
	// policy to them so a secret inside a command string never lands in the
	// store or the model-visible response.
	command = d.Redactor.Redact(command)
	cwd = d.Redactor.Redact(cwd)

	// Idempotency pre-check: callers with a deterministic call reference
	// (MCP JSON-RPC request id, CLI --call-ref/--idempotency-key, hook
	// tool_use_id) can answer a duplicate retry from the stored evidence
	// before writing a new artifact file. This is what keeps a retried call to
	// exactly one ledger/artifact/reduction row and never leaves an orphan
	// file. An empty CallRef cannot pre-check, so it is guarded below by a
	// post-hoc orphan purge instead; every independent invocation gets its own
	// fresh reference and its own row.
	if d.CallRef != "" {
		key := store.LedgerIdempotencyKey(d.Harness, d.SessionID, d.CallRef)
		prior, err := d.DB.GetLedgerByIdempotencyKey(key)
		if err != nil {
			return "", fmt.Errorf("ledger pre-check: %w", err)
		}
		if prior != nil && prior.ArtifactID != "" && prior.Outcome != store.LedgerOutcomeError {
			return RenderStored(d.DB, d.Store, prior)
		}
	}

	// Store raw evidence
	artifact, storeErr := d.Store.Store([]byte(output), uuid.New().String(), command, cwd, exitCode)
	if storeErr != nil {
		return "", fmt.Errorf("store artifact: %w", storeErr)
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
			compactText = red.CompactContent
			compactTokens = red.CompactTokenEst
			reduced = true
		}
	}

	rawTokens := len(output) / 4
	recommendation := policy.Recommend(category, rawTokens, compactTokens, reduced)
	preGuard := recommendation
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
	guarded := false
	if policy.GuardRecommendation(recommendation, rawTokens, len(responseText)/4) != recommendation {
		guarded = true
		recommendation = policy.RecommendationPassthrough
		modelText = output
		modelTokens = rawTokens
		receipt = policy.FormatReceipt(0, 0, 0, nil, artifact.ArtifactID, false)
		responseText = policy.FormatToolOutput(recommendation, command, exitCode, rawTokens, modelTokens, artifact.ArtifactID, modelText, receipt)
	}

	reductionApplied := reduced && (recommendation == policy.RecommendationReduce || recommendation == policy.RecommendationArtifactRequired)
	callRef := d.CallRef
	if callRef == "" {
		// No caller-supplied reference means this is an independent invocation:
		// it must count as its own call, never silently collapse with an
		// identical invocation through a content-derived key. A fresh ref keeps
		// the ledger honest; callers that want retry dedup pass an explicit
		// idempotency key instead.
		callRef = uuid.New().String()
	}

	var reducerName, reducerVersion string
	if reduction != nil {
		reducerName = reduction.ReducerName
		reducerVersion = reduction.ReducerVersion
	} else if reducer != nil {
		reducerName = reducer.Name()
		reducerVersion = reducer.Version()
	}

	outcome := store.LedgerOutcomePassthrough
	switch {
	case guarded:
		outcome = store.LedgerOutcomeGuarded
	case recommendation == policy.RecommendationPreserveFull:
		outcome = store.LedgerOutcomePreserved
	case reductionApplied:
		outcome = store.LedgerOutcomeReductionApplied
	case reduced:
		outcome = store.LedgerOutcomeReductionAttempted
	}

	entry := &store.LedgerEntry{
		CallID:               "call-" + artifact.ArtifactID,
		IdempotencyKey:       store.LedgerIdempotencyKey(d.Harness, d.SessionID, callRef),
		EventTimestamp:       time.Now(),
		SessionID:            d.SessionID,
		Harness:              d.Harness,
		Command:              command,
		Cwd:                  cwd,
		Category:             string(category),
		ReducerName:          reducerName,
		ReducerVersion:       reducerVersion,
		RawBytes:             int64(len(output)),
		ModelVisibleBytes:    int64(len(modelText)),
		RawTokenEst:          int64(rawTokens),
		ModelVisibleTokenEst: int64(modelTokens),
		Recommendation:       string(preGuard),
		FinalDecision:        string(recommendation),
		ReductionAttempted:   reduced,
		ReductionApplied:     reductionApplied,
		ArtifactID:           artifact.ArtifactID,
		ReductionID:          reductionID(reduction),
		ExitCode:             exitCode,
		HookStatus:           "",
		Outcome:              outcome,
	}

	// The artifact, reduction, and ledger rows are written in one transaction,
	// so a duplicate retry (same idempotency key) re-inserts nothing. The
	// session_metrics compat row is updated only for genuinely new calls so the
	// two read models never drift apart.
	inserted, err := d.DB.RecordCall(artifact, reduction, entry)
	if err != nil {
		return "", fmt.Errorf("record call: %w", err)
	}
	if inserted {
		// Per-call deltas, matching the reducer's actual outcome: a call that
		// produced no reduction record must not claim one in the compat row.
		if err := d.DB.InsertSessionMetrics(d.SessionID, rawTokens, modelTokens, btoi(reduced), 1); err != nil {
			return "", fmt.Errorf("insert session metrics: %w", err)
		}
	} else {
		// A concurrent process won the ledger insert, or the digest-fallback
		// retry collided. The file we just wrote may have no referencing row;
		// purge it only when it is genuinely unreferenced so evidence
		// integrity is preserved.
		if err := d.DB.PurgeOrphanArtifact(d.Store, artifact.ContentDigest); err != nil {
			return "", fmt.Errorf("purge orphan artifact: %w", err)
		}
	}

	return responseText, nil
}

// RenderStored rebuilds the envelope for an already-recorded ledger row from
// the stored evidence. It answers an idempotency retry without re-storing,
// re-classifying, or re-rendering the current input: the returned text is the
// byte-identical response the first call produced (same artifact id, same
// receipt, same compact text), so a retried logical call cannot double count
// and never leaves a second artifact file behind.
func RenderStored(db *store.DB, st *artifacts.Store, prior *store.LedgerEntry) (string, error) {
	if prior.ArtifactID == "" {
		return "", fmt.Errorf("ledger row %s has no artifact", prior.CallID)
	}
	meta, err := db.GetArtifact(prior.ArtifactID)
	if err != nil {
		return "", fmt.Errorf("load stored artifact: %w", err)
	}
	if meta == nil {
		return "", fmt.Errorf("ledger row references missing artifact %s", prior.ArtifactID)
	}
	raw, err := st.RetrieveByDigest(meta.ContentDigest)
	if err != nil {
		return "", fmt.Errorf("read stored artifact: %w", err)
	}
	rawText := string(raw)
	modelText := rawText
	var failedTests []string
	if prior.ReductionID != "" {
		red, err := db.GetReduction(prior.ReductionID)
		if err != nil {
			return "", fmt.Errorf("load stored reduction: %w", err)
		}
		if red != nil {
			failedTests = red.StructuredFacts
			// Only an applied reduction put compact text in the model-visible
			// response; attempted/guarded/passthrough rows showed the raw
			// output verbatim.
			if prior.Outcome == store.LedgerOutcomeReductionApplied {
				modelText = red.CompactContent
			}
		}
	}
	receipt := policy.FormatReceipt(lineCount(modelText), lineCount(rawText), len(rawText)-len(modelText), failedTests, prior.ArtifactID, modelText != rawText)
	return policy.FormatToolOutput(policy.Recommendation(prior.FinalDecision), prior.Command, prior.ExitCode, int(prior.RawTokenEst), int(prior.ModelVisibleTokenEst), prior.ArtifactID, modelText, receipt), nil
}

func reductionID(r *artifacts.ReductionRecord) string {
	if r == nil {
		return ""
	}
	return r.ReductionID
}

// btoi converts a bool to 0/1 for session_metrics column arguments.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// lineCount returns the number of lines in s, matching the receipt's
// "kept N/M lines" semantics (a trailing newline does not add a line).
func lineCount(s string) int {
	return strings.Count(s, "\n") + 1
}
