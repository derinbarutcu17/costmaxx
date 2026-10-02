# Capability Matrix (experimental)

| Capability | Codex (current) |
|------------|----------------|
| Observe supported local tool results | ✓ (PostToolUse, Bash) |
| Replace large supported hook tool results | ✗ (Codex hook limitation) |
| Inject context at SessionStart | ✓ (returns additionalContext) |
| Restore state after compaction | ✗ (Codex limitation; state persists and loads at SessionStart resume instead) |
| Persist state across subprocesses | ✓ (session_id-keyed SQLite) |
| Store raw tool output as evidence | ✓ (zstd-compressed, SHA-256 addressed, SQLite metadata) |
| Cross-process artifact retrieval | ✓ (artifact_id → digest → file) |
| Reduce test/build/diff/search output | ✓ (deterministic MCP path; hooks observe only) |
| Persist reduction records | ✓ (SQLite reduction_records table) |
| Immutable per-call ledger | ✓ (SQLite call_ledger, one row per accepted call, idempotency-keyed) |
| Persist per-session metrics | ✓ (SQLite session_metrics, compatibility view only; reporting derives from the ledger) |
| Savings/report by event timestamp | ✓ (`costmaxx savings` / `report`, all-history and `--since` windows) |
| Numbered DB migrations | ✓ (v1→v2→v3→v4→v5→v6, handles old task_id schema, idempotent; v6 normalizes ledger timestamps to a monotonic fixed-width UTC layout) |
| Lifecycle hook coverage | SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PreCompact, Stop, SessionEnd |
| Hermes adapter | ✗ (no bespoke adapter; Hermes is MCP client support via `costmaxx install --target hermes`, tool exposed as `mcp__costmaxx__costmax_run` / `costmaxx:costmax_run`) |
| opencode adapter | ✗ (no bespoke adapter; opencode is supported via the MCP server, not an adapter; no auto-compression plugin ships in this repo) |
| Hermes MCP `mcp_servers` registration | ✓ (`costmaxx install --target hermes`, text-preserving merge under `mcp_servers:`; informational doctor check) |
| MCP `costmax_run` execution and evidence retrieval | ✓ (opt-in active path; any spec-compliant MCP client: Codex, opencode, Hermes, Gemini, custom) |
| Active hook output replacement | ✗ (Codex hook limitation; MCP path is separate) |
| Real active-path verifier | ✓ (`scripts/verify-active-path.py`, positive + negative directions) |
| Transcript-backed benchmark runner | ✓ (bounded 20-case Codex evaluator; organic-adoption arm separate) |
