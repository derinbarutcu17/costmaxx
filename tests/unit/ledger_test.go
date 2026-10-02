package unit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/pipeline"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

func newLedgerTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("open ledger db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ledgerFixture(id string, outcome store.LedgerOutcome, attempted, applied int, rawTok, modelTok int64) *store.LedgerEntry {
	attemptedB := attempted == 1
	appliedB := applied == 1
	finalDecision := "passthrough"
	recommendation := "passthrough"
	switch outcome {
	case store.LedgerOutcomeReductionApplied:
		finalDecision = "reduce"
		recommendation = "reduce"
	case store.LedgerOutcomeGuarded:
		finalDecision = "passthrough"
		recommendation = "reduce"
	case store.LedgerOutcomePreserved:
		finalDecision = "preserve_full"
		recommendation = "preserve_full"
	}
	return &store.LedgerEntry{
		CallID:               "call-" + id,
		IdempotencyKey:       store.LedgerIdempotencyKey("test", "sess", id),
		EventTimestamp:       time.Now(),
		SessionID:            "sess",
		Harness:              "test",
		Command:              "command " + id,
		Category:             "terminal",
		ReducerName:          "terminal",
		ReducerVersion:       "test",
		RawBytes:             rawTok * 4,
		ModelVisibleBytes:    modelTok * 4,
		RawTokenEst:          rawTok,
		ModelVisibleTokenEst: modelTok,
		Recommendation:       recommendation,
		FinalDecision:        finalDecision,
		ReductionAttempted:   attemptedB,
		ReductionApplied:     appliedB,
		ArtifactID:           "art-" + id,
		ReductionID:          "red-" + id,
		ExitCode:             0,
		Outcome:              outcome,
	}
}

func TestLedgerInsertIdempotent(t *testing.T) {
	db := newLedgerTestDB(t)

	e := ledgerFixture("dup", store.LedgerOutcomeReductionApplied, 1, 1, 1000, 100)
	inserted, err := db.InsertLedger(e)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first insert must be counted")
	}
	inserted, err = db.InsertLedger(e)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("duplicate idempotency key must not double count")
	}

	rows, err := db.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 ledger row after duplicate, got %d", len(rows))
	}
}

func TestRecordCallAtomicAndIdempotent(t *testing.T) {
	db := newLedgerTestDB(t)

	art := &artifacts.EvidenceArtifact{
		ArtifactID:      "art-atomic",
		ContentDigest:   "digest-atomic",
		MediaType:       "text/plain",
		Encoding:        "utf-8",
		OriginalBytes:   4000,
		CompressedBytes: 400,
		EstimatedTokens: 1000,
		StoragePath:     "/tmp/atomic.zst",
		Command:         "seq 1 1000",
		ExitCode:        0,
		CreatedAt:       time.Now(),
	}
	red := &artifacts.ReductionRecord{
		ReductionID:      "red-atomic",
		ArtifactID:       art.ArtifactID,
		ReducerName:      "terminal",
		ReducerVersion:   "test",
		CompactContent:   "kept",
		OriginalBytes:    4000,
		CompactBytes:     400,
		OriginalTokenEst: 1000,
		CompactTokenEst:  100,
		CreatedAt:        time.Now(),
	}
	e := ledgerFixture("atomic", store.LedgerOutcomeReductionApplied, 1, 1, 1000, 100)

	inserted, err := db.RecordCall(art, red, e)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first RecordCall must insert")
	}

	// Duplicate retry: same idempotency key, nothing re-inserted.
	inserted, err = db.RecordCall(art, red, e)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("duplicate RecordCall must not re-insert")
	}

	if n, err := db.ArtifactCount(); err != nil || n != 1 {
		t.Fatalf("artifact count = %d, want 1 (err=%v)", n, err)
	}
	if n, err := db.ReductionCount(); err != nil || n != 1 {
		t.Fatalf("reduction count = %d, want 1 (err=%v)", n, err)
	}
	rows, err := db.LedgerRows(time.Time{}, time.Time{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1 (err=%v)", len(rows), err)
	}
	row := rows[0]
	if row.ArtifactID != art.ArtifactID || row.ReductionID != red.ReductionID {
		t.Errorf("ledger row not linked to artifact/reduction: %+v", row)
	}
	if !row.ReductionAttempted || !row.ReductionApplied {
		t.Errorf("ledger row flags wrong: %+v", row)
	}
}

func TestMarkLedgerRehydrated(t *testing.T) {
	db := newLedgerTestDB(t)
	e := ledgerFixture("rehydrate", store.LedgerOutcomeReductionApplied, 1, 1, 1000, 100)
	if _, err := db.InsertLedger(e); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkLedgerRehydrated(e.ArtifactID); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.LedgerRows(time.Time{}, time.Time{})
	if len(rows) != 1 || !rows[0].Rehydrated {
		t.Fatalf("expected rehydrated flag set, got %+v", rows)
	}
}

func TestLedgerReportFixture(t *testing.T) {
	db := newLedgerTestDB(t)
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)

	// Deterministic report fixture: a real reduction, a guard downgrade, a
	// policy passthrough, a preserve_full, an equal/longer compact (reduction
	// attempted but not applied), a duplicate retry, and a rehydration.
	cases := []struct {
		id        string
		outcome   store.LedgerOutcome
		attempted int
		applied   int
		rawTok    int64
		modelTok  int64
		at        time.Time
	}{
		{"a-reduction", store.LedgerOutcomeReductionApplied, 1, 1, 1000, 100, now},       // real reduction
		{"b-guard", store.LedgerOutcomeGuarded, 1, 0, 1000, 1000, now},                   // guard downgrade
		{"c-passthrough", store.LedgerOutcomePassthrough, 0, 0, 200, 200, now},           // policy passthrough
		{"d-preserve", store.LedgerOutcomePreserved, 0, 0, 300, 300, now},                // preserve_full
		{"e-compact-longer", store.LedgerOutcomeReductionAttempted, 1, 0, 500, 500, now}, // equal/longer compact
		{"f-old", store.LedgerOutcomeReductionApplied, 1, 1, 99999, 1, old},              // outside window
	}
	for _, c := range cases {
		e := ledgerFixture(c.id, c.outcome, c.attempted, c.applied, c.rawTok, c.modelTok)
		e.EventTimestamp = c.at
		if _, err := db.InsertLedger(e); err != nil {
			t.Fatal(err)
		}
	}
	// Duplicate retry of the real reduction (ignored).
	e := ledgerFixture("a-reduction", store.LedgerOutcomeReductionApplied, 1, 1, 1000, 100)
	if _, err := db.InsertLedger(e); err != nil {
		t.Fatal(err)
	}
	// Rehydration of the real reduction's artifact.
	if err := db.MarkLedgerRehydrated("art-a-reduction"); err != nil {
		t.Fatal(err)
	}

	sum, err := db.LedgerSummary(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// The duplicate retry must not double count; the old row is outside the
	// window, so the fixture has exactly 5 in-window calls.
	if sum.CallsProcessed != 5 {
		t.Errorf("calls_processed = %d, want 5", sum.CallsProcessed)
	}
	if sum.ArtifactsStored != 5 {
		t.Errorf("artifacts_stored = %d, want 5", sum.ArtifactsStored)
	}
	if sum.ReductionsAttempted != 3 {
		t.Errorf("reductions_attempted = %d, want 3", sum.ReductionsAttempted)
	}
	if sum.ReductionsApplied != 1 {
		t.Errorf("reductions_applied = %d, want 1", sum.ReductionsApplied)
	}
	if sum.Passthroughs != 1 {
		t.Errorf("passthroughs = %d, want 1", sum.Passthroughs)
	}
	if sum.GuardDowngrades != 1 {
		t.Errorf("guard_downgrades = %d, want 1", sum.GuardDowngrades)
	}
	if sum.Preserved != 1 {
		t.Errorf("preserved = %d, want 1", sum.Preserved)
	}
	if sum.Rehydrations != 1 {
		t.Errorf("rehydrations = %d, want 1", sum.Rehydrations)
	}
	if sum.Errors != 0 {
		t.Errorf("errors = %d, want 0", sum.Errors)
	}
	if sum.RawTokens != 3000 || sum.ModelVisibleTokens != 2100 {
		t.Errorf("token totals wrong: raw=%d model=%d, want 3000/2100", sum.RawTokens, sum.ModelVisibleTokens)
	}
	if sum.SavedTokens() != 900 {
		t.Errorf("saved = %d, want 900", sum.SavedTokens())
	}
	if sum.NoSavingCalls() != 4 {
		t.Errorf("no_saving_calls = %d, want 4", sum.NoSavingCalls())
	}

	// All-history window must include the old row.
	all, err := db.LedgerSummary(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if all.CallsProcessed != 6 || all.ReductionsApplied != 2 {
		t.Errorf("all-history wrong: calls=%d applied=%d, want 6/2", all.CallsProcessed, all.ReductionsApplied)
	}

	// LedgerRows with an until bound must exclude the fixture rows placed at
	// "now" when until is just before them.
	rows, err := db.LedgerRows(time.Time{}, now.Add(-1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("until-bounded rows = %d, want 1 (old row)", len(rows))
	}
}

// TestLedgerSummarySubSecondBoundary proves `--since` windows slice by actual
// event time, not by RFC3339Nano string order. A call 50ms after a boundary at
// .5s used to be dropped because its old form ("…:00.55Z") string-sorted
// BEFORE the boundary ("…:00.5Z"); the fixed-width ledger layout keeps
// lexicographic order chronologically monotonic.
func TestLedgerSummarySubSecondBoundary(t *testing.T) {
	db := newLedgerTestDB(t)
	boundary := time.Date(2026, 9, 2, 10, 0, 0, 500000000, time.UTC)
	inWindow := time.Date(2026, 9, 2, 10, 0, 0, 550000000, time.UTC)
	outOfWindow := time.Date(2026, 9, 2, 10, 0, 0, 450000000, time.UTC)

	for _, c := range []struct {
		id string
		at time.Time
	}{{"later", inWindow}, {"earlier", outOfWindow}} {
		e := ledgerFixture(c.id, store.LedgerOutcomePassthrough, 0, 0, 10, 10)
		e.EventTimestamp = c.at
		if inserted, err := db.InsertLedger(e); err != nil || !inserted {
			t.Fatalf("insert %s: inserted=%v err=%v", c.id, inserted, err)
		}
	}

	sum, err := db.LedgerSummary(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if sum.CallsProcessed != 1 {
		t.Fatalf("calls_processed = %d, want 1 (the .55s call was dropped by string-ordered slicing)", sum.CallsProcessed)
	}
	if sum.RawTokens != 10 {
		t.Errorf("raw tokens = %d, want 10 (only the in-window call)", sum.RawTokens)
	}

	rows, err := db.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(rows))
	}
	if rows[0].CallID != "call-earlier" || rows[1].CallID != "call-later" {
		t.Errorf("ORDER BY event_timestamp is not chronological: %s, %s", rows[0].CallID, rows[1].CallID)
	}
}

func TestLedgerConcurrentInserts(t *testing.T) {
	db := newLedgerTestDB(t)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := ledgerFixture(fmt.Sprintf("conc-%d", i), store.LedgerOutcomePassthrough, 0, 0, 10, 10)
			_, errs[i] = db.InsertLedger(e)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent insert %d: %v", i, err)
		}
	}
	rows, err := db.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != n {
		t.Errorf("ledger rows = %d, want %d", len(rows), n)
	}
}

// TestPipelineDuplicateCallIsIdempotentAndLeavesNoOrphan proves the shared
// pipeline's deterministic-caller path: invoking the same logical call twice
// (same harness/session/call-ref) yields exactly one ledger, artifact, and
// reduction row, byte-identical envelopes with the SAME artifact id (no
// dangling reference), no inflated session metrics, and no unreferenced
// artifact file on disk.
func TestPipelineDuplicateCallIsIdempotentAndLeavesNoOrphan(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.Harness = "test"
	deps.CallRef = "deterministic-retry-ref"

	command := "for i in $(seq 1 80); do echo line; done"
	var output strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&output, "line %d: this is verbose test output that should be reduced by the terminal reducer\n", i)
	}

	first, err := pipeline.Process(deps, output.String(), command, "/tmp/work", 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (first): %v", err)
	}
	second, err := pipeline.Process(deps, output.String(), command, "/tmp/work", 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (retry): %v", err)
	}

	// A duplicate retry must return the byte-identical stored result, artifact
	// id included — never a fresh id the model could not resolve.
	if first != second {
		t.Fatalf("duplicate retry returned a different envelope:\n--- first ---\n%s\n--- retry ---\n%s", first, second)
	}

	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want exactly 1", len(rows))
	}
	if n, err := deps.DB.ArtifactCount(); err != nil || n != 1 {
		t.Fatalf("artifact rows = %d (err=%v), want exactly 1", n, err)
	}
	if n, err := deps.DB.ReductionCount(); err != nil || n != 1 {
		t.Fatalf("reduction rows = %d (err=%v), want exactly 1", n, err)
	}

	// session_metrics must count the call exactly once.
	_, _, reduced, calls, err := deps.DB.GetSessionMetrics(deps.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || reduced != 1 {
		t.Errorf("metrics inflated by retry: calls=%d artifacts_reduced=%d, want 1/1", calls, reduced)
	}

	// Exactly one artifact file on disk, and it must be referenced by the
	// single artifact row.
	var onDisk []string
	if err := filepath.Walk(deps.Store.BaseDir(), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			onDisk = append(onDisk, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 1 {
		t.Fatalf("on-disk artifact files = %d, want exactly 1: %v", len(onDisk), onDisk)
	}
	meta, err := deps.DB.GetArtifact(rows[0].ArtifactID)
	if err != nil || meta == nil {
		t.Fatalf("artifact lookup for ledger row: err=%v meta=%v", err, meta)
	}
	if !strings.Contains(onDisk[0], meta.ContentDigest) {
		t.Errorf("on-disk file %q is not referenced by the stored artifact digest %s", onDisk[0], meta.ContentDigest)
	}
}

// TestPipelineDuplicateCallReusesStoredEvidence proves the pre-check answers a
// same-ref retry from the stored evidence even when the current input differs:
// the retry returns the FIRST envelope, nothing new is stored, and no orphan
// file or extra row appears.
func TestPipelineDuplicateCallReusesStoredEvidence(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.Harness = "test"
	deps.CallRef = "dup-ref-different-input"

	firstOut := strings.Repeat("=== RUN   TestFoo\n--- FAIL: TestFoo (0.00s)\n    a_test.go:9: boom\nFAIL\n", 25)
	secondOut := strings.Repeat("completely different payload that never reaches the store\n", 40)

	first, err := pipeline.Process(deps, firstOut, "go test ./...", "/tmp/a", 1, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (first): %v", err)
	}
	second, err := pipeline.Process(deps, secondOut, "something-else", "/tmp/b", 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (retry): %v", err)
	}
	if first != second {
		t.Fatalf("same-ref retry must return the stored envelope, got a different one:\n--- first ---\n%s\n--- retry ---\n%s", first, second)
	}

	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(rows))
	}
	if n, err := deps.DB.ArtifactCount(); err != nil || n != 1 {
		t.Fatalf("artifact rows = %d (err=%v), want 1", n, err)
	}
	if n, err := deps.DB.ReductionCount(); err != nil || n != 1 {
		t.Fatalf("reduction rows = %d (err=%v), want 1", n, err)
	}

	// The stored envelope must carry a real reduction (the first input
	// reduces) so we prove the STORED compact text was served, not the new
	// input.
	if !strings.Contains(first, "Recommendation: reduce") {
		t.Errorf("expected the stored reduction envelope, got:\n%s", first)
	}
}

// TestPipelineNoRefCallsCountSeparatelyAndLeaveNoOrphan proves the
// caller-less path (empty CallRef) treats each invocation as an independent
// call: two identical Process calls record two ledger and two artifact rows
// (never undercounted through a content-derived key), the content-addressed
// store keeps one shared file, and no unreferenced file is left behind.
func TestPipelineNoRefCallsCountSeparatelyAndLeaveNoOrphan(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.Harness = "test"

	output := strings.Repeat("some deterministic fallback output line\n", 300)
	if _, err := pipeline.Process(deps, output, "cmd", "", 0, "mcp_costmax_run"); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Process(deps, output, "cmd", "", 0, "mcp_costmax_run"); err != nil {
		t.Fatal(err)
	}

	if n, err := deps.DB.ArtifactCount(); err != nil || n != 2 {
		t.Fatalf("artifact rows = %d (err=%v), want 2 (independent calls)", n, err)
	}
	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %d, want 2 (independent calls)", len(rows))
	}

	// No orphan files: every on-disk file is referenced by a stored row.
	known := map[string]bool{}
	for _, a := range mustListArtifacts(t, deps) {
		known[a.ContentDigest] = true
	}
	orphans := deps.Store.OrphanDigests(known)
	if len(orphans) != 0 {
		t.Fatalf("orphan artifact files remain after independent calls: %v", orphans)
	}
}

// TestPipelineSameRefReplaysStoredAndLeavesNoOrphan proves a same-ref retry
// answers from the stored evidence before writing anything: one canonical
// ledger/artifact/reduction row and one shared file, with no orphan.
func TestPipelineSameRefReplaysStoredAndLeavesNoOrphan(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.Harness = "test"
	deps.CallRef = "deterministic-ref-1"

	output := strings.Repeat("some deterministic ref output line\n", 300)
	if _, err := pipeline.Process(deps, output, "cmd", "", 0, "mcp_costmax_run"); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Process(deps, output, "cmd", "", 0, "mcp_costmax_run"); err != nil {
		t.Fatal(err)
	}

	if n, err := deps.DB.ArtifactCount(); err != nil || n != 1 {
		t.Fatalf("artifact rows = %d (err=%v), want 1 (same-ref retry)", n, err)
	}
	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1 (same-ref retry)", len(rows))
	}

	known := map[string]bool{}
	for _, a := range mustListArtifacts(t, deps) {
		known[a.ContentDigest] = true
	}
	orphans := deps.Store.OrphanDigests(known)
	if len(orphans) != 0 {
		t.Fatalf("orphan artifact files remain after same-ref retry: %v", orphans)
	}
}

// TestRenderStoredGuardedClaimsNoSaving proves re-serving a stored guarded row
// renders a passthrough envelope with raw == model tokens (no saving claim)
// and a replay-only receipt.
func TestRenderStoredGuardedClaimsNoSaving(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.Harness = "test"
	deps.CallRef = "guard-ref"

	// A long command embeds the command in the compact summary, making the
	// fully rendered envelope larger than the raw output and forcing the
	// post-render guard to downgrade to passthrough (outcome=guarded).
	command := "seq 1 500 # " + strings.Repeat("x", 1000)
	var output strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&output, "%d\n", i)
	}
	env, err := pipeline.Process(deps, output.String(), command, "", 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(env, "Recommendation: passthrough") {
		t.Fatalf("guard did not downgrade long command:\n%s", env)
	}

	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Outcome != store.LedgerOutcomeGuarded {
		t.Errorf("outcome = %q, want guarded", row.Outcome)
	}
	if row.RawTokenEst != row.ModelVisibleTokenEst {
		t.Errorf("guarded call claimed a token saving: raw=%d model=%d", row.RawTokenEst, row.ModelVisibleTokenEst)
	}

	// Re-serve the stored row: the reconstructed envelope must still claim no
	// saving and carry the replay-only receipt.
	again, err := pipeline.RenderStored(deps.DB, deps.Store, &row)
	if err != nil {
		t.Fatalf("RenderStored: %v", err)
	}
	if !strings.Contains(again, "Recommendation: passthrough") {
		t.Errorf("reconstructed guarded envelope changed decision:\n%s", again)
	}
	if strings.Contains(again, "dropped ") {
		t.Errorf("guarded reconstruction claims dropped bytes:\n%s", again)
	}
	if !strings.Contains(again, "Receipt: replay: costmaxx replay "+row.ArtifactID) {
		t.Errorf("guarded reconstruction missing replay-only receipt:\n%s", again)
	}
}

// TestRenderStoredFidelityProvesByteIdenticalReplay loops over one input per
// meaningful final outcome and proves pipeline.RenderStored re-serves the exact
// bytes pipeline.Process originally returned: same recommendation, same token
// counts, same artifact id, same receipt, same compact text. It also proves
// re-serving never mutates the store (no new ledger/artifact/reduction rows).
func TestRenderStoredFidelityProvesByteIdenticalReplay(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		exitCode int
		output   string
		wantRec  string
		wantOut  store.LedgerOutcome
	}{
		{
			// A verbose failing test run reduces; the compact text is the
			// model-visible response (reduction_applied).
			name:     "reduction-applied",
			command:  "go test ./...",
			exitCode: 1,
			output:   strings.Repeat("=== RUN   TestThing\n--- FAIL: TestThing (0.00s)\nFAIL\nok  example/m 0.5s\n", 40),
			wantRec:  "reduce",
			wantOut:  store.LedgerOutcomeReductionApplied,
		},
		{
			// A terminal output that a reducer handles but cannot shrink (the
			// compact echoes the full input): the policy passes through and the
			// ledger records reduction_attempted (attempted, not applied).
			name:     "reduction-attempted-passthrough",
			command:  "run-workflow.sh",
			exitCode: 0,
			output: func() string {
				var b strings.Builder
				for i := 0; i < 22; i++ {
					fmt.Fprintf(&b, "some distinct terminal output line with unique words %03d\n", i)
				}
				return b.String()
			}(),
			wantRec: "passthrough",
			wantOut: store.LedgerOutcomeReductionAttempted,
		},
		{
			// Binary output has no reducer and must be preserved verbatim
			// (preserve_full).
			name:     "preserve-full",
			command:  "cat b.bin",
			exitCode: 0,
			output:   string([]byte{0x00, 0x01, 0xff, 0xfe}) + strings.Repeat("\x00binary\xff\n", 40),
			wantRec:  "preserve_full",
			wantOut:  store.LedgerOutcomePreserved,
		},
		{
			// A long command embeds in the compact summary, making the fully
			// rendered envelope larger than the raw output: the guard downgrades
			// reduce -> passthrough (guarded).
			name:     "guard-downgrade",
			command:  "seq 1 500 # " + strings.Repeat("x", 1000),
			exitCode: 0,
			output: func() string {
				var b strings.Builder
				for i := 1; i <= 500; i++ {
					fmt.Fprintf(&b, "%d\n", i)
				}
				return b.String()
			}(),
			wantRec: "passthrough",
			wantOut: store.LedgerOutcomeGuarded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := newPipelineDeps(t)
			deps.Harness = "test"
			deps.CallRef = "fidelity-" + tc.name

			first, err := pipeline.Process(deps, tc.output, tc.command, "/tmp/work", tc.exitCode, "mcp_costmax_run")
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if !strings.Contains(first, "Recommendation: "+tc.wantRec) {
				t.Fatalf("fixture produced recommendation %q, want %q:\n%s", tc.wantRec, tc.wantRec, first)
			}

			rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("ledger rows = %d, want 1", len(rows))
			}
			if rows[0].Outcome != tc.wantOut {
				t.Fatalf("fixture outcome = %q, want %q", rows[0].Outcome, tc.wantOut)
			}

			// Re-serve the stored row: byte-identical envelope, counts untouched.
			again, err := pipeline.RenderStored(deps.DB, deps.Store, &rows[0])
			if err != nil {
				t.Fatalf("RenderStored: %v", err)
			}
			if again != first {
				t.Fatalf("RenderStored is not byte-identical to Process:\n--- Process ---\n%s\n--- RenderStored ---\n%s", first, again)
			}

			if n, err := deps.DB.LedgerRows(time.Time{}, time.Time{}); err != nil || len(n) != 1 {
				t.Fatalf("RenderStored added ledger rows: %d (err=%v)", len(n), err)
			}
			if n, err := deps.DB.ArtifactCount(); err != nil || n != 1 {
				t.Fatalf("RenderStored changed artifact count: %d (err=%v), want 1", n, err)
			}
			if n, err := deps.DB.ReductionCount(); err != nil {
				t.Fatalf("RenderStored changed reduction count: %d (err=%v)", n, err)
			} else if want := map[store.LedgerOutcome]int{
				store.LedgerOutcomeReductionApplied:   1,
				store.LedgerOutcomeReductionAttempted: 1,
				store.LedgerOutcomeGuarded:            1,
				store.LedgerOutcomePreserved:          0,
				store.LedgerOutcomePassthrough:        0,
			}[tc.wantOut]; n != want {
				t.Fatalf("RenderStored changed reduction count: %d, want %d", n, want)
			}
		})
	}
}

// TestRenderStoredFidelitySameRefDifferentInput proves a same-ref retry with a
// different payload re-serves the canonical first envelope (via RenderStored)
// and never changes the ledger/artifact/reduction counts.
func TestRenderStoredFidelitySameRefDifferentInput(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.Harness = "test"
	deps.CallRef = "fidelity-same-ref"

	firstOut := strings.Repeat("=== RUN   TestFoo\n--- FAIL: TestFoo (0.00s)\n    a_test.go:9: boom\nFAIL\n", 25)
	first, err := pipeline.Process(deps, firstOut, "go test ./...", "/tmp/a", 1, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (first): %v", err)
	}

	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(rows))
	}

	// RenderStored directly (the same path the idempotency pre-check uses for
	// a same-ref retry) must reproduce the original envelope byte for byte.
	replayed, err := pipeline.RenderStored(deps.DB, deps.Store, &rows[0])
	if err != nil {
		t.Fatalf("RenderStored: %v", err)
	}
	if replayed != first {
		t.Fatalf("same-ref replay is not byte-identical:\n--- first ---\n%s\n--- replay ---\n%s", first, replayed)
	}

	// The Process retry with a different payload answers from stored evidence
	// and must not add rows or change counts.
	secondOut := strings.Repeat("completely different payload that never reaches the store\n", 40)
	second, err := pipeline.Process(deps, secondOut, "something-else", "/tmp/b", 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (retry): %v", err)
	}
	if second != first {
		t.Fatalf("same-ref retry returned a different envelope:\n--- first ---\n%s\n--- retry ---\n%s", first, second)
	}
	if n, err := deps.DB.LedgerRows(time.Time{}, time.Time{}); err != nil || len(n) != 1 {
		t.Fatalf("same-ref retry added ledger rows: %d (err=%v), want 1", len(n), err)
	}
	if n, err := deps.DB.ArtifactCount(); err != nil || n != 1 {
		t.Fatalf("same-ref retry added artifacts: %d (err=%v), want 1", n, err)
	}
	if n, err := deps.DB.ReductionCount(); err != nil || n != 1 {
		t.Fatalf("same-ref retry changed reductions: %d (err=%v), want 1", n, err)
	}
}

// mustListArtifacts returns all stored artifact rows, failing the test on a
// DB error.
func mustListArtifacts(t *testing.T, deps pipeline.Deps) []*artifacts.EvidenceArtifact {
	t.Helper()
	all, err := deps.DB.ListArtifacts()
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	return all
}

// TestPipelineWritesLedger proves the shared ingestion chain records one
// ledger row per accepted call, with honest reduction-applied semantics and a
// harness tag, so MCP and CLI calls are attributable.
func TestPipelineWritesLedger(t *testing.T) {
	deps := newPipelineDeps(t)

	var output strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&output, "line %d: this is verbose test output that should be reduced by the terminal reducer\n", i)
	}
	command := "for i in $(seq 1 80); do echo line; done"

	if _, err := pipeline.Process(deps, output.String(), command, "/tmp/work", 0, "mcp_costmax_run"); err != nil {
		t.Fatalf("Process: %v", err)
	}

	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 ledger row, got %d", len(rows))
	}
	row := rows[0]
	if row.Harness != "mcp_costmax_run" {
		t.Errorf("harness = %q, want mcp_costmax_run", row.Harness)
	}
	if row.Outcome != store.LedgerOutcomeReductionApplied {
		t.Errorf("outcome = %q, want reduction_applied", row.Outcome)
	}
	if !row.ReductionAttempted || !row.ReductionApplied {
		t.Errorf("reduction flags wrong: attempted=%v applied=%v", row.ReductionAttempted, row.ReductionApplied)
	}
	if row.ModelVisibleTokenEst >= row.RawTokenEst {
		t.Errorf("no saving claimed on reduced call: raw=%d model=%d", row.RawTokenEst, row.ModelVisibleTokenEst)
	}
	if row.Cwd != "/tmp/work" || row.Command != command {
		t.Errorf("command/cwd not recorded: %q / %q", row.Command, row.Cwd)
	}
	if row.ArtifactID == "" {
		t.Error("ledger row missing artifact link")
	}
}

// TestPipelineRedactsCommandBeforePersistence proves a secret inside a command
// string is removed from the artifact metadata and the ledger row, matching
// the redaction policy applied to output.
func TestPipelineRedactsCommandBeforePersistence(t *testing.T) {
	deps := newPipelineDeps(t)

	secretCmd := "curl -H 'Authorization: Bearer super-secret-token-123' https://api.example"
	output := strings.Repeat("ok\n", 1200)
	envelope, err := pipeline.Process(deps, output, secretCmd, "/tmp/sec", 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if strings.Contains(envelope, "super-secret-token-123") {
		t.Error("envelope leaks the secret from the command string")
	}

	rows, err := deps.DB.LedgerRows(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 ledger row, got %d", len(rows))
	}
	if strings.Contains(rows[0].Command, "super-secret-token-123") {
		t.Errorf("ledger persisted the raw secret command: %q", rows[0].Command)
	}

	meta, err := deps.DB.GetArtifact(rows[0].ArtifactID)
	if err != nil || meta == nil {
		t.Fatalf("artifact lookup: %v", err)
	}
	if strings.Contains(meta.Command, "super-secret-token-123") {
		t.Errorf("artifact metadata persisted the raw secret command: %q", meta.Command)
	}
}
