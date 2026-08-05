# CostMax Architecture (experimental)

## Core Principle

Separate observation, storage, reduction, state projection, and delivery.

## Components

1. **CLI and Configuration Layer** — `cmd/costmax/main.go`, `internal/config/`
2. **Codex Hook Adapter** — `internal/adapters/codex/` (stdin JSON lifecycle hooks)
3. **Artifact Store** — `internal/artifacts/` (content-addressed, zstd-compressed)
4. **Output Classifier** — `internal/events/classifier.go`
5. **Deterministic Reducer Pipeline** — `internal/reducers/`
6. **Task-State Projector** — `internal/state/`
7. **Context Composer** — `internal/context/`
8. **Metrics Engine** — `internal/metrics/`
9. **SQLite Storage** — `internal/store/` (events, task state, artifact metadata, reduction records, session metrics)

## Hook Data Flow (observe-only)

```
Codex lifecycle hook (stdin JSON) → Parse hook event → Store raw evidence →
Classify output → Apply deterministic reducer → Update task state →
Record reduction metrics → Return hook response (observe-only)
```

## MCP Active Path (opt-in)

```
Codex calls costmax_run → execute local command → store raw artifact →
classify/reduce or apply safe recommendation → return model-visible result →
resources/read retrieves the original artifact when needed
```

## Evidence Retrieval Flow

```
artifact_id → SQLite metadata lookup → content_digest → SHA-256 addressed file → decompressed original
```

## Storage

- Content-addressed files for raw evidence (SHA-256, zstd-compressed)
- SQLite for metadata, events, task state, artifact metadata, reduction records, session metrics
- Default: `~/.costmax/` with user-only permissions

## Status

- **observe mode**: working (records and measures, never replaces output)
- **session state restored at SessionStart resume**, not at PostCompact (Codex limitation)
- **MCP active path**: working and transcript-verified; it is opt-in and does
  not replace Codex's built-in Bash output
- **Hermes / Claude Code adapters**: future only
