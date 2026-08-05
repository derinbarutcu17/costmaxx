package integration

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

func TestMigrationFromV1ToV2(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create a v1 database with old schema (task_id column)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (1, '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS task_state (
		task_id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		version INTEGER NOT NULL DEFAULT 1,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO task_state (task_id, data, version, updated_at) VALUES ('old-task-1', '{"task_id":"old-task-1","objective":"old objective"}', 1, '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		session_id TEXT PRIMARY KEY,
		task_id TEXT,
		repository TEXT,
		branch TEXT,
		mode TEXT,
		harness TEXT,
		harness_version TEXT,
		model TEXT,
		started_at TEXT,
		ended_at TEXT,
		task_result TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO sessions (session_id, task_id) VALUES ('sess-from-old', 'old-task-1')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Open with store (runs migrations)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Verify old data is accessible via session_id
	ts, err := s.LoadTaskState("sess-from-old")
	if err != nil {
		t.Fatal(err)
	}
	if ts == nil {
		t.Fatal("expected migrated task state for session_id 'sess-from-old'")
	}
	if ts.Objective != "old objective" {
		t.Errorf("expected 'old objective', got %q", ts.Objective)
	}

	// Verify schema version is now 2
	var version int
	err = s.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Errorf("expected schema version >= 2, got %d", version)
	}

	// Verify old task_id table is gone
	var tableName string
	err = s.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='task_state'`).Scan(&tableName)
	if err != nil {
		t.Fatal("task_state table should exist after migration")
	}
	// Verify it has session_id column, not task_id
	var cid int
	var name, ctype string
	var notnull int
	var dflt sql.NullString
	var pk int
	err = s.QueryRow(`PRAGMA table_info(task_state)`).Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
	if err != nil {
		t.Fatal(err)
	}
	if name != "session_id" {
		t.Errorf("expected first column to be session_id, got %s", name)
	}
}

func TestFreshDBNeedsOneOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Immediately after one Open, schema_version must be >= 3
	var version int
	err = s.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version < 3 {
		t.Fatalf("fresh DB should reach v3 in one Open, got v%d", version)
	}

	// InsertArtifact must succeed immediately
	art := &artifacts.EvidenceArtifact{
		ArtifactID:    "test-artifact-1",
		ContentDigest: "abc123",
		StoragePath:   "/tmp/test",
		CreatedAt:     time.Now(),
	}
	if err := s.InsertArtifact(art); err != nil {
		t.Fatalf("InsertArtifact on fresh DB: %v", err)
	}

	// InsertSessionMetrics must succeed immediately
	if err := s.InsertSessionMetrics("sess_metrics_test", 100, 50, 2, 5); err != nil {
		t.Fatalf("InsertSessionMetrics on fresh DB: %v", err)
	}

	// Verify the session metrics row
	rt, ct, ar, tc, err := s.GetSessionMetrics("sess_metrics_test")
	if err != nil {
		t.Fatal(err)
	}
	if rt != 100 || ct != 50 || ar != 2 || tc != 5 {
		t.Errorf("metrics mismatch: got %d/%d/%d/%d, want 100/50/2/5", rt, ct, ar, tc)
	}
}
