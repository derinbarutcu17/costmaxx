package unit

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

func TestSavingsSummaryAggregates(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)

	// One session with metrics.
	if err := db.InsertSessionMetrics("sess-1", 1000, 100, 2, 3); err != nil {
		t.Fatal(err)
	}
	// Two artifacts inside the window.
	for i := 0; i < 2; i++ {
		if err := db.InsertArtifact(&artifacts.EvidenceArtifact{
			ArtifactID:    fmt.Sprintf("art-%d", i),
			ContentDigest: fmt.Sprintf("digest-%d", i),
			StoragePath:   "unused",
			OriginalBytes: 100,
			CreatedAt:     now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A reduction record inside the window.
	if err := db.InsertReduction(&artifacts.ReductionRecord{
		ReductionID:    "red-1",
		ArtifactID:     "art-1",
		ReducerName:    "test",
		CompactContent: "kept",
		OriginalBytes:  4096,
		CompactBytes:   96,
		CreatedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	// An old reduction record outside the window.
	if err := db.InsertReduction(&artifacts.ReductionRecord{
		ReductionID:    "red-2",
		ArtifactID:     "art-2",
		ReducerName:    "test",
		CompactContent: "kept",
		OriginalBytes:  2048,
		CompactBytes:   64,
		CreatedAt:      old,
	}); err != nil {
		t.Fatal(err)
	}

	sum, err := db.SavingsSummary(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Sessions != 1 || sum.ToolCalls != 3 || sum.ArtifactsStored != 2 {
		t.Errorf("session metrics wrong: %+v", sum)
	}
	if sum.RawTokens != 1000 || sum.ModelVisible != 100 {
		t.Errorf("token totals wrong: %+v", sum)
	}
	if sum.ReductionsApplied != 1 {
		t.Errorf("expected 1 in-window reduction, got %d", sum.ReductionsApplied)
	}
	if sum.BytesDropped != 4000 {
		t.Errorf("expected 4000 bytes dropped, got %d", sum.BytesDropped)
	}
}
