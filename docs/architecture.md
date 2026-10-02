# CostMax Architecture (experimental)

## Core Principle

Separate observation, storage, reduction, state projection, and delivery.

## Components

1. **CLI and Configuration Layer** — `cmd/costmax/main.go`, `internal/config/`
2. **Codex Adapter** — `internal/adapters/codex/` (stdin JSON lifecycle hooks)
3. **Adapter Protocol / Capability Set** — `internal/adapters/protocol/` (Adapter interface, CapabilitySet)
4. **MCP Transport** — `internal/mcp/` (opt-in `costmax_run` active path)
5. **Artifact Store** — `internal/artifacts/` (content-addressed, zstd-compressed)
6. **Ingest Pipeline** — `internal/pipeline/` (shared ingestion for hooks and MCP)
7. **Output Classifier** — `internal/events/classifier.go`
8. **Deterministic Reducer Pipeline** — `internal/reducers/`
9. **Recommendation / Format Policy** — `internal/policy/`
10. **Task-State Projector** — `internal/state/`
11. **Metrics Engine** — `internal/metrics/`
12. **Privacy Layer** — `internal/privacy/`
13. **SQLite Storage** — `internal/store/` (events, task state, artifact metadata, reduction records, session metrics)

## Hook Data Flow (observe-only)

```
Codex lifecycle hook (stdin JSON) → Parse hook event → Store raw evidence →
Classify output → Apply deterministic reducer → Update task state →
Record reduction metrics → Return hook response (observe-only)
```

## MCP Active Path (opt-in)

```
Any MCP client (Codex, opencode, Hermes, Gemini, custom) calls costmax_run →
execute local command → store raw artifact →
classify/reduce or apply safe recommendation → return model-visible result →
resources/read retrieves the original artifact when needed
```

MCP use is opt-in: a client only compresses when the model calls `costmax_run`
(Codex: `costmax_run`; opencode: `costmaxx_costmax_run`; Hermes:
`mcp__costmaxx__costmax_run` / `costmaxx:costmax_run`). Codex hooks
remain observe-only and never replace tool output.

## Evidence Retrieval Flow

```
artifact_id → SQLite metadata lookup → content_digest → SHA-256 addressed file → decompressed original
```

## Storage

- Content-addressed files for raw evidence (SHA-256, zstd-compressed)
- SQLite for metadata, events, task state, artifact metadata, reduction
  records, session metrics, and the immutable per-call ledger (`call_ledger`).
  The ledger is the source of truth for savings/report windows;
  `session_metrics` is a compatibility view only.
- Default: `~/.costmax/` with user-only permissions

## Status

- **observe mode**: working (records and measures, never replaces output)
- **session state restored at SessionStart resume**, not at PostCompact (Codex limitation)
- **MCP active path**: working and transcript-verified; it is opt-in and does
  not replace Codex's built-in Bash output
- **Adapters**: Codex is the only bespoke adapter; Claude/Hermes adapters were
  removed. opencode and Hermes are MCP client support
  (`costmaxx install --target opencode|hermes`, tool exposed as
  `costmaxx_costmax_run` for opencode or `mcp__costmaxx__costmax_run` for
  Hermes), not adapters. MCP use is opt-in and no
  auto-compression plugin ships in this repository.
