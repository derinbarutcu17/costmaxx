package unit

import (
	"path/filepath"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/store"
)

func TestSessionMetricsAccumulateAcrossCalls(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sid := "session-1"
	for i := 0; i < 3; i++ {
		if err := db.InsertSessionMetrics(sid, 100, 40, 1, 1); err != nil {
			t.Fatal(err)
		}
	}
	raw, compact, reduced, calls, err := db.GetSessionMetrics(sid)
	if err != nil {
		t.Fatal(err)
	}
	if raw != 300 || compact != 120 || reduced != 3 || calls != 3 {
		t.Errorf("accumulation wrong: raw=%d compact=%d reduced=%d calls=%d", raw, compact, reduced, calls)
	}
}

func TestSessionMetricsSeparateSessions(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.InsertSessionMetrics("a", 10, 5, 1, 1)
	db.InsertSessionMetrics("b", 20, 10, 2, 2)
	raw, _, _, _, _ := db.GetSessionMetrics("a")
	if raw != 10 {
		t.Errorf("session a raw = %d, want 10 (sessions must not bleed)", raw)
	}
}

func TestSessionMetricsMissingSession(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	raw, compact, reduced, calls, err := db.GetSessionMetrics("ghost")
	if err != nil {
		t.Fatalf("missing session should not error, got %v", err)
	}
	if raw != 0 || compact != 0 || reduced != 0 || calls != 0 {
		t.Errorf("missing session should return zeros, got %d/%d/%d/%d", raw, compact, reduced, calls)
	}
}
