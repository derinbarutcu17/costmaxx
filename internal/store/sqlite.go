package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/state"
)

const schemaVersion int = 3

type DB struct {
	db *sql.DB
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &DB{db: db}
	// A second costmaxx process (e.g. a Codex hook) can hold the DB while
	// this one migrates. busy_timeout does not cover every SQLITE_BUSY path,
	// so retry the migration a few times before giving up.
	for attempt := 0; ; attempt++ {
		if err := s.migrate(); err == nil {
			return s, nil
		} else if attempt >= 4 || !isBusy(err) {
			db.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func isBusy(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}

func (s *DB) Close() error {
	return s.db.Close()
}

func (s *DB) migrate() error {
	version, err := s.currentVersion()
	if err != nil {
		return err
	}

	// Fresh database: apply latest schema directly
	if version == 0 {
		for v := 1; v <= schemaVersion; v++ {
			if err := s.applyMigration(v); err != nil {
				return err
			}
		}
		return nil
	}

	// Existing database: apply pending migrations sequentially
	for version < schemaVersion {
		version++
		if err := s.applyMigration(version); err != nil {
			return err
		}
	}
	return nil
}

func (s *DB) applyMigration(v int) error {
	// v2 migration from old task_id to session_id: check if needed
	if v == 2 {
		hasOldSchema, err := s.hasColumn("task_state", "task_id")
		if err != nil {
			return err
		}
		if hasOldSchema {
			if _, err := s.db.Exec(migrationFor(v)); err != nil {
				return fmt.Errorf("migration v%d: %w", v, err)
			}
		} else {
			// Fresh or already migrated — just ensure latest schema
			if _, err := s.db.Exec(migrationFor(1)); err != nil {
				return fmt.Errorf("schema create: %w", err)
			}
		}
	} else {
		if _, err := s.db.Exec(migrationFor(v)); err != nil {
			return fmt.Errorf("migration v%d: %w", v, err)
		}
	}

	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (?, ?)`,
		v, time.Now().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("record v%d: %w", v, err)
	}
	return nil
}

func (s *DB) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, nil
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, nil
}

func (s *DB) currentVersion() (int, error) {
	// schema_version may not exist yet (fresh DB)
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return 0, err
	}
	var v int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (s *DB) InsertArtifact(a *artifacts.EvidenceArtifact) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO artifacts
		(artifact_id, content_digest, media_type, encoding, original_bytes,
		 compressed_bytes, estimated_tokens, storage_path, source_event_id,
		 command, exit_code, created_at, retention_class, redaction_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ArtifactID, a.ContentDigest, a.MediaType, a.Encoding,
		a.OriginalBytes, a.CompressedBytes, a.EstimatedTokens, a.StoragePath,
		a.SourceEventID, a.Command, a.ExitCode, a.CreatedAt,
		a.RetentionClass, a.RedactionStatus,
	)
	return err
}

func (s *DB) GetArtifact(artifactID string) (*artifacts.EvidenceArtifact, error) {
	var a artifacts.EvidenceArtifact
	var ts string
	err := s.db.QueryRow(
		`SELECT artifact_id, content_digest, media_type, encoding, original_bytes,
		 compressed_bytes, estimated_tokens, storage_path, source_event_id,
		 command, exit_code, created_at, retention_class, redaction_status
		FROM artifacts WHERE artifact_id = ?`, artifactID,
	).Scan(&a.ArtifactID, &a.ContentDigest, &a.MediaType, &a.Encoding,
		&a.OriginalBytes, &a.CompressedBytes, &a.EstimatedTokens, &a.StoragePath,
		&a.SourceEventID, &a.Command, &a.ExitCode, &ts,
		&a.RetentionClass, &a.RedactionStatus,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &a, nil
}

func (s *DB) GetArtifactByDigest(digest string) (*artifacts.EvidenceArtifact, error) {
	var a artifacts.EvidenceArtifact
	var ts string
	err := s.db.QueryRow(
		`SELECT artifact_id, content_digest, media_type, encoding, original_bytes,
		 compressed_bytes, estimated_tokens, storage_path, source_event_id,
		 command, exit_code, created_at, retention_class, redaction_status
		FROM artifacts WHERE content_digest = ? LIMIT 1`, digest,
	).Scan(&a.ArtifactID, &a.ContentDigest, &a.MediaType, &a.Encoding,
		&a.OriginalBytes, &a.CompressedBytes, &a.EstimatedTokens, &a.StoragePath,
		&a.SourceEventID, &a.Command, &a.ExitCode, &ts,
		&a.RetentionClass, &a.RedactionStatus,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &a, nil
}

func (s *DB) InsertReduction(r *artifacts.ReductionRecord) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO reduction_records
		(reduction_id, artifact_id, reducer_name, reducer_version,
		 compact_content, structured_facts, preserved_anchors,
		 omitted_line_ranges, original_bytes, compact_bytes,
		 original_token_est, compact_token_est, replacement_applied, reason,
		 created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ReductionID, r.ArtifactID, r.ReducerName, r.ReducerVersion,
		r.CompactContent, mustJSON(r.StructuredFacts), mustJSON(r.PreservedAnchors),
		mustJSON(r.OmittedLineRanges), r.OriginalBytes, r.CompactBytes,
		r.OriginalTokenEst, r.CompactTokenEst, boolToInt(r.ReplacementApplied),
		r.Reason, time.Now(),
	)
	return err
}

func (s *DB) ReductionCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM reduction_records`).Scan(&count)
	return count, err
}

func (s *DB) InsertSessionMetrics(sessionID string, rawTokens, compactTokens, artifactsReduced, toolCalls int) error {
	_, err := s.db.Exec(
		`INSERT INTO session_metrics (session_id, raw_tokens, compact_tokens, artifacts_reduced, tool_calls, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			raw_tokens = raw_tokens + excluded.raw_tokens,
			compact_tokens = compact_tokens + excluded.compact_tokens,
			artifacts_reduced = artifacts_reduced + excluded.artifacts_reduced,
			tool_calls = tool_calls + excluded.tool_calls,
			updated_at = excluded.updated_at`,
		sessionID, rawTokens, compactTokens, artifactsReduced, toolCalls, time.Now(),
	)
	return err
}

func (s *DB) GetSessionMetrics(sessionID string) (rawTokens, compactTokens, artifactsReduced, toolCalls int, err error) {
	err = s.db.QueryRow(
		`SELECT raw_tokens, compact_tokens, artifacts_reduced, tool_calls
		FROM session_metrics WHERE session_id = ?`, sessionID,
	).Scan(&rawTokens, &compactTokens, &artifactsReduced, &toolCalls)
	if err == sql.ErrNoRows {
		return 0, 0, 0, 0, nil
	}
	return
}

func migrationFor(v int) string {
	switch v {
	case 1:
		return `
			CREATE TABLE IF NOT EXISTS events (
				event_id TEXT PRIMARY KEY,
				timestamp TEXT NOT NULL,
				harness TEXT NOT NULL,
				harness_version TEXT,
				adapter_version TEXT,
				session_id TEXT NOT NULL,
				repository TEXT,
				event_type TEXT NOT NULL,
				tool_name TEXT,
				tool_input TEXT,
				tool_output TEXT,
				execution_metadata TEXT,
				capability_flags TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
			CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
			CREATE TABLE IF NOT EXISTS sessions (
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
			);
			CREATE TABLE IF NOT EXISTS task_state (
				session_id TEXT PRIMARY KEY,
				data TEXT NOT NULL,
				version INTEGER NOT NULL DEFAULT 1,
				updated_at TEXT NOT NULL
			);
			CREATE TABLE IF NOT EXISTS reduction_records (
				reduction_id TEXT PRIMARY KEY,
				artifact_id TEXT NOT NULL,
				reducer_name TEXT NOT NULL,
				reducer_version TEXT,
				compact_content TEXT,
				structured_facts TEXT,
				preserved_anchors TEXT,
				omitted_line_ranges TEXT,
				original_bytes INTEGER,
				compact_bytes INTEGER,
				original_token_est INTEGER,
				compact_token_est INTEGER,
				replacement_applied INTEGER DEFAULT 0,
				reason TEXT,
				created_at TEXT NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_reduction_artifact ON reduction_records(artifact_id);
		`
	case 2:
		// Migration from old task_id schema to session_id schema
		return `
			CREATE TABLE IF NOT EXISTS task_state_v2 (
				session_id TEXT PRIMARY KEY,
				data TEXT NOT NULL,
				version INTEGER NOT NULL DEFAULT 1,
				updated_at TEXT NOT NULL
			);
			INSERT OR IGNORE INTO task_state_v2 (session_id, data, version, updated_at)
				SELECT COALESCE(s.session_id, 'migrated-' || ts.task_id), ts.data, ts.version, ts.updated_at
				FROM task_state ts
				LEFT JOIN sessions s ON s.task_id = ts.task_id;
			DROP TABLE IF EXISTS task_state;
			ALTER TABLE task_state_v2 RENAME TO task_state;
		`
	case 3:
		return `
			CREATE TABLE IF NOT EXISTS artifacts (
				artifact_id TEXT PRIMARY KEY,
				content_digest TEXT NOT NULL,
				media_type TEXT,
				encoding TEXT,
				original_bytes INTEGER,
				compressed_bytes INTEGER,
				estimated_tokens INTEGER,
				storage_path TEXT NOT NULL,
				source_event_id TEXT,
				command TEXT,
				exit_code INTEGER DEFAULT 0,
				created_at TEXT NOT NULL,
				retention_class TEXT DEFAULT 'session',
				redaction_status TEXT DEFAULT ''
			);
			CREATE INDEX IF NOT EXISTS idx_artifacts_digest ON artifacts(content_digest);
			CREATE TABLE IF NOT EXISTS session_metrics (
				session_id TEXT PRIMARY KEY,
				raw_tokens INTEGER DEFAULT 0,
				compact_tokens INTEGER DEFAULT 0,
				artifacts_reduced INTEGER DEFAULT 0,
				tool_calls INTEGER DEFAULT 0,
				updated_at TEXT NOT NULL
			);
		`
	default:
		return ""
	}
}

func (s *DB) InsertEvent(evt *events.HarnessEvent) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO events
		(event_id, timestamp, harness, harness_version, adapter_version,
		 session_id, repository, event_type, tool_name, tool_input,
		 tool_output, execution_metadata, capability_flags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.EventID, evt.Timestamp, evt.Harness, evt.HarnessVersion,
		evt.AdapterVersion, evt.SessionID, evt.Repository, evt.EventType,
		evt.ToolName, mapJSON(evt.ToolInput), evt.ToolOutput,
		mapJSON(evt.ExecutionMetadata), mapJSON(evt.CapabilityFlags),
	)
	return err
}

func (s *DB) GetSessionEvents(sessionID string) ([]events.HarnessEvent, error) {
	rows, err := s.db.Query(
		`SELECT event_id, timestamp, harness, harness_version, adapter_version,
		 session_id, repository, event_type, tool_name, tool_input,
		 tool_output, execution_metadata, capability_flags
		FROM events WHERE session_id = ? ORDER BY timestamp`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []events.HarnessEvent
	for rows.Next() {
		var e events.HarnessEvent
		var ts, ti, em, cf string
		err := rows.Scan(&e.EventID, &ts, &e.Harness, &e.HarnessVersion,
			&e.AdapterVersion, &e.SessionID, &e.Repository, &e.EventType,
			&e.ToolName, &ti, &e.ToolOutput, &em, &cf)
		if err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		if ti != "" {
			json.Unmarshal([]byte(ti), &e.ToolInput)
		}
		if em != "" {
			json.Unmarshal([]byte(em), &e.ExecutionMetadata)
		}
		if cf != "" {
			json.Unmarshal([]byte(cf), &e.CapabilityFlags)
		}
		result = append(result, e)
	}
	return result, nil
}

func (s *DB) SaveTaskState(sessionID string, ts *state.TaskState) error {
	ts.StateVersion++
	ts.UpdatedAt = time.Now()
	data := mustJSON(ts)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO task_state (session_id, data, version, updated_at)
		VALUES (?, ?, ?, ?)`,
		sessionID, data, ts.StateVersion, ts.UpdatedAt)
	return err
}

func (s *DB) QueryRow(query string, args ...any) *sql.Row {
	return s.db.QueryRow(query, args...)
}

func (s *DB) LoadTaskState(sessionID string) (*state.TaskState, error) {
	var data string
	err := s.db.QueryRow(
		`SELECT data FROM task_state WHERE session_id = ?`, sessionID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseJSONstate(data)
}
