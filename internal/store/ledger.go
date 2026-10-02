package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
)

// ledgerTimeLayout is the fixed-width UTC timestamp layout stored in
// call_ledger.event_timestamp. It always carries exactly nine fractional
// digits, so lexicographic TEXT ordering is chronologically monotonic. Plain
// time.RFC3339Nano trims trailing zeros, which inverts sub-second order for
// values like ".5Z" vs ".55Z" and silently mis-slices `--since` windows.
const ledgerTimeLayout = "2006-01-02T15:04:05.000000000Z"

// formatLedgerTime renders a timestamp in the fixed-width ledger layout.
func formatLedgerTime(t time.Time) string {
	return t.UTC().Format(ledgerTimeLayout)
}

// isNormalizedLedgerTime reports whether a stored value is already in the
// fixed-width layout (exactly nine fractional digits, UTC 'Z').
func isNormalizedLedgerTime(ts string) bool {
	return strings.HasSuffix(ts, "Z") && len(ts) == len(ledgerTimeLayout)
}

// LedgerOutcome is a compact enum describing what happened to one accepted
// call. It is the single source of truth for reporting semantics: the older
// session_metrics table is kept only as a compatibility/read model and must
// never be the source of truth for time windows.
type LedgerOutcome string

const (
	// LedgerOutcomeReductionApplied: a reducer produced compact text AND the
	// final decision put that compact text in the model-visible response.
	LedgerOutcomeReductionApplied LedgerOutcome = "reduction_applied"
	// LedgerOutcomeReductionAttempted: a reducer produced compact text but
	// the policy chose passthrough, so nothing was saved for the model.
	LedgerOutcomeReductionAttempted LedgerOutcome = "reduction_attempted"
	// LedgerOutcomeGuarded: a reduction was attempted but the final full
	// response was not actually smaller, so the guard downgraded to
	// passthrough. reduction_attempted is also set for these rows.
	LedgerOutcomeGuarded LedgerOutcome = "guarded"
	// LedgerOutcomePassthrough: no reduction was attempted and the final
	// decision was passthrough (below threshold or compact not smaller).
	LedgerOutcomePassthrough LedgerOutcome = "passthrough"
	// LedgerOutcomePreserved: the final decision was preserve_full (binary,
	// empty, or no reducer available).
	LedgerOutcomePreserved LedgerOutcome = "preserve_full"
	// LedgerOutcomeError: the call failed open (malformed payload, storage
	// error) and never produced a model-visible envelope.
	LedgerOutcomeError LedgerOutcome = "error"
)

// LedgerEntry is one immutable row per accepted command/tool call.
type LedgerEntry struct {
	CallID               string
	IdempotencyKey       string
	EventTimestamp       time.Time
	SessionID            string
	Harness              string
	Command              string
	Cwd                  string
	Category             string
	ReducerName          string
	ReducerVersion       string
	RawBytes             int64
	ModelVisibleBytes    int64
	RawTokenEst          int64
	ModelVisibleTokenEst int64
	Recommendation       string
	FinalDecision        string
	ReductionAttempted   bool
	ReductionApplied     bool
	ArtifactID           string
	ReductionID          string
	ExitCode             int
	Rehydrated           bool
	HookStatus           string
	ErrorStatus          string
	Outcome              LedgerOutcome
}

// LedgerIdempotencyKey derives a deterministic key from harness, session, and
// a caller-supplied call reference (JSON-RPC request id, hook tool_use_id,
// or fresh uuid). Replaying the same logical call re-derives the same key, so
// a retry or duplicate subprocess can never double count a call.
func LedgerIdempotencyKey(harness, sessionID, callRef string) string {
	sum := sha256.Sum256([]byte(harness + "\x00" + sessionID + "\x00" + callRef))
	return hex.EncodeToString(sum[:])
}

// InsertLedger persists one ledger row. It is idempotent on the idempotency
// key: a duplicate row is ignored and not counted twice. The returned bool
// reports whether the row was actually inserted.
func (s *DB) InsertLedger(e *LedgerEntry) (bool, error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO call_ledger
		(call_id, idempotency_key, event_timestamp, session_id, harness,
		 command, cwd, category, reducer_name, reducer_version,
		 raw_bytes, model_visible_bytes, raw_token_est, model_visible_token_est,
		 recommendation, final_decision, reduction_attempted, reduction_applied,
		 artifact_id, reduction_id, exit_code, rehydrated, hook_status,
		 error_status, outcome)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.CallID, e.IdempotencyKey, formatLedgerTime(e.EventTimestamp),
		e.SessionID, e.Harness, e.Command, e.Cwd, e.Category,
		e.ReducerName, e.ReducerVersion, e.RawBytes, e.ModelVisibleBytes,
		e.RawTokenEst, e.ModelVisibleTokenEst, e.Recommendation, e.FinalDecision,
		boolToInt(e.ReductionAttempted), boolToInt(e.ReductionApplied),
		e.ArtifactID, e.ReductionID, e.ExitCode, boolToInt(e.Rehydrated),
		e.HookStatus, e.ErrorStatus, string(e.Outcome),
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RecordCall persists an artifact, its optional reduction record, and the
// call ledger row in a single transaction so the metadata and the ledger row
// are written atomically for each accepted call. Idempotency is enforced on
// the ledger's idempotency key: when the row already exists (retry/duplicate
// subprocess), nothing is re-inserted and the returned bool is false.
func (s *DB) RecordCall(art *artifacts.EvidenceArtifact, red *artifacts.ReductionRecord, e *LedgerEntry) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO artifacts
		(artifact_id, content_digest, media_type, encoding, original_bytes,
		 compressed_bytes, estimated_tokens, storage_path, source_event_id,
		 command, cwd, exit_code, created_at, retention_class, redaction_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		art.ArtifactID, art.ContentDigest, art.MediaType, art.Encoding,
		art.OriginalBytes, art.CompressedBytes, art.EstimatedTokens, art.StoragePath,
		art.SourceEventID, art.Command, art.Cwd, art.ExitCode,
		art.CreatedAt.UTC().Format(time.RFC3339Nano),
		art.RetentionClass, art.RedactionStatus,
	); err != nil {
		return false, fmt.Errorf("tx insert artifact: %w", err)
	}

	if red != nil {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO reduction_records
			(reduction_id, artifact_id, reducer_name, reducer_version,
			 compact_content, structured_facts, preserved_anchors,
			 omitted_line_ranges, original_bytes, compact_bytes,
			 original_token_est, compact_token_est, replacement_applied, reason,
			 created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			red.ReductionID, red.ArtifactID, red.ReducerName, red.ReducerVersion,
			red.CompactContent, mustJSON(red.StructuredFacts), mustJSON(red.PreservedAnchors),
			mustJSON(red.OmittedLineRanges), red.OriginalBytes, red.CompactBytes,
			red.OriginalTokenEst, red.CompactTokenEst, boolToInt(red.ReplacementApplied),
			red.Reason, reductionTime(red.CreatedAt),
		); err != nil {
			return false, fmt.Errorf("tx insert reduction: %w", err)
		}
	}

	res, err := tx.Exec(
		`INSERT OR IGNORE INTO call_ledger
		(call_id, idempotency_key, event_timestamp, session_id, harness,
		 command, cwd, category, reducer_name, reducer_version,
		 raw_bytes, model_visible_bytes, raw_token_est, model_visible_token_est,
		 recommendation, final_decision, reduction_attempted, reduction_applied,
		 artifact_id, reduction_id, exit_code, rehydrated, hook_status,
		 error_status, outcome)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.CallID, e.IdempotencyKey, formatLedgerTime(e.EventTimestamp),
		e.SessionID, e.Harness, e.Command, e.Cwd, e.Category,
		e.ReducerName, e.ReducerVersion, e.RawBytes, e.ModelVisibleBytes,
		e.RawTokenEst, e.ModelVisibleTokenEst, e.Recommendation, e.FinalDecision,
		boolToInt(e.ReductionAttempted), boolToInt(e.ReductionApplied),
		e.ArtifactID, e.ReductionID, e.ExitCode, boolToInt(e.Rehydrated),
		e.HookStatus, e.ErrorStatus, string(e.Outcome),
	)
	if err != nil {
		return false, fmt.Errorf("tx insert ledger: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		// The ledger row already exists: this is a duplicate retry. Nothing
		// was written; do not double count.
		return false, nil
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

// MarkLedgerRehydrated records that the raw evidence for an artifact was
// rehydrated (served back to the model). The original call row stays
// immutable; only this status flag is flipped.
func (s *DB) MarkLedgerRehydrated(artifactID string) error {
	if _, err := s.db.Exec(
		`UPDATE call_ledger SET rehydrated = 1 WHERE artifact_id = ?`, artifactID,
	); err != nil {
		return err
	}
	return nil
}

// LedgerSummary aggregates ledger rows by actual event timestamp for a time
// window. A zero `since` means all history. Every count maps to the outcome
// enum partition; a processed artifact is never labelled "reduced" unless the
// final model-visible response was the compact text.
type LedgerSummary struct {
	CallsProcessed      int   `json:"calls_processed"`
	ArtifactsStored     int   `json:"artifacts_stored"`
	ReductionsAttempted int   `json:"reductions_attempted"`
	ReductionsApplied   int   `json:"reductions_applied"`
	Passthroughs        int   `json:"passthrough"`
	GuardDowngrades     int   `json:"guard_downgrades"`
	Preserved           int   `json:"preserve_full"`
	Rehydrations        int   `json:"rehydrations"`
	Errors              int   `json:"errors"`
	RawTokens           int64 `json:"raw_token_estimate"`
	ModelVisibleTokens  int64 `json:"model_visible_token_estimate"`
	RawBytes            int64 `json:"raw_bytes"`
	ModelVisibleBytes   int64 `json:"model_visible_bytes"`
}

// SavedTokens returns the estimated model-visible token saving for the window.
func (l *LedgerSummary) SavedTokens() int64 {
	return l.RawTokens - l.ModelVisibleTokens
}

// NoSavingCalls returns how many accepted calls produced no model-visible
// saving (everything except applied reductions).
func (l *LedgerSummary) NoSavingCalls() int {
	return l.CallsProcessed - l.ReductionsApplied
}

// ReductionRate returns the share of raw tokens not shown to the model,
// 0 when there is no raw input in the window.
func (l *LedgerSummary) ReductionRate() float64 {
	if l.RawTokens <= 0 {
		return 0
	}
	return float64(l.SavedTokens()) / float64(l.RawTokens)
}

// PassthroughRate returns the share of accepted calls that produced no saving
// (policy passthrough or guard downgrade), 0 when there are no calls.
func (l *LedgerSummary) PassthroughRate() float64 {
	if l.CallsProcessed <= 0 {
		return 0
	}
	return float64(l.Passthroughs+l.GuardDowngrades) / float64(l.CallsProcessed)
}

func (s *DB) LedgerSummary(since time.Time) (*LedgerSummary, error) {
	var sum LedgerSummary
	var args []any
	query := `
		SELECT
			COUNT(*) AS calls_processed,
			COALESCE(SUM(CASE WHEN artifact_id IS NOT NULL AND artifact_id != '' THEN 1 ELSE 0 END), 0) AS artifacts_stored,
			COALESCE(SUM(reduction_attempted), 0) AS reductions_attempted,
			COALESCE(SUM(reduction_applied), 0) AS reductions_applied,
			COALESCE(SUM(CASE WHEN outcome = 'passthrough' THEN 1 ELSE 0 END), 0) AS passthroughs,
			COALESCE(SUM(CASE WHEN outcome = 'guarded' THEN 1 ELSE 0 END), 0) AS guard_downgrades,
			COALESCE(SUM(CASE WHEN outcome = 'preserve_full' THEN 1 ELSE 0 END), 0) AS preserved,
			COALESCE(SUM(rehydrated), 0) AS rehydrations,
			COALESCE(SUM(CASE WHEN outcome = 'error' THEN 1 ELSE 0 END), 0) AS errors,
			COALESCE(SUM(raw_token_est), 0) AS raw_tokens,
			COALESCE(SUM(model_visible_token_est), 0) AS model_visible_tokens,
			COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
			COALESCE(SUM(model_visible_bytes), 0) AS model_visible_bytes
		FROM call_ledger`
	if !since.IsZero() {
		query += " WHERE event_timestamp >= ?"
		args = append(args, formatLedgerTime(since))
	}
	err := s.db.QueryRow(query, args...).Scan(
		&sum.CallsProcessed, &sum.ArtifactsStored, &sum.ReductionsAttempted,
		&sum.ReductionsApplied, &sum.Passthroughs, &sum.GuardDowngrades,
		&sum.Preserved, &sum.Rehydrations, &sum.Errors, &sum.RawTokens,
		&sum.ModelVisibleTokens, &sum.RawBytes, &sum.ModelVisibleBytes,
	)
	if err != nil {
		return nil, err
	}
	return &sum, nil
}

// ledgerSelect is the shared column list for reading ledger rows. Keep it in
// sync with the INSERT lists in InsertLedger/RecordCall.
const ledgerSelect = `
	SELECT call_id, idempotency_key, event_timestamp, session_id, harness,
	 command, cwd, category, reducer_name, reducer_version,
	 raw_bytes, model_visible_bytes, raw_token_est, model_visible_token_est,
	 recommendation, final_decision, reduction_attempted, reduction_applied,
	 artifact_id, reduction_id, exit_code, rehydrated, hook_status,
	 error_status, outcome
	FROM call_ledger`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLedgerRow(sc rowScanner) (*LedgerEntry, error) {
	var e LedgerEntry
	var ts, outcome string
	var attempt, applied, rehydrated int
	if err := sc.Scan(
		&e.CallID, &e.IdempotencyKey, &ts, &e.SessionID, &e.Harness,
		&e.Command, &e.Cwd, &e.Category, &e.ReducerName, &e.ReducerVersion,
		&e.RawBytes, &e.ModelVisibleBytes, &e.RawTokenEst, &e.ModelVisibleTokenEst,
		&e.Recommendation, &e.FinalDecision, &attempt, &applied,
		&e.ArtifactID, &e.ReductionID, &e.ExitCode, &rehydrated,
		&e.HookStatus, &e.ErrorStatus, &outcome,
	); err != nil {
		return nil, err
	}
	e.EventTimestamp = parseArtifactTime(ts)
	e.ReductionAttempted = attempt == 1
	e.ReductionApplied = applied == 1
	e.Rehydrated = rehydrated == 1
	e.Outcome = LedgerOutcome(outcome)
	return &e, nil
}

// GetLedgerByIdempotencyKey returns the stored ledger row for a key, or nil
// when no call with that key was recorded. Callers use it to pre-check
// idempotency before writing new evidence, so a duplicate retry can reuse the
// stored result instead of storing a second artifact.
func (s *DB) GetLedgerByIdempotencyKey(key string) (*LedgerEntry, error) {
	e, err := scanLedgerRow(s.db.QueryRow(ledgerSelect+" WHERE idempotency_key = ?", key))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// LedgerRows returns the ledger rows in a time window (all history when both
// bounds are zero), ordered by event timestamp.
func (s *DB) LedgerRows(since, until time.Time) ([]LedgerEntry, error) {
	var clauses []string
	var args []any
	if !since.IsZero() {
		clauses = append(clauses, "event_timestamp >= ?")
		args = append(args, formatLedgerTime(since))
	}
	if !until.IsZero() {
		clauses = append(clauses, "event_timestamp < ?")
		args = append(args, formatLedgerTime(until))
	}
	query := ledgerSelect
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY event_timestamp"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LedgerEntry
	for rows.Next() {
		e, err := scanLedgerRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}
