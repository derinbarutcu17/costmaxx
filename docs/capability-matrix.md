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
| Persist per-session metrics | ✓ (SQLite session_metrics table) |
| Numbered DB migrations | ✓ (v1→v2→v3, handles old task_id schema) |
| Lifecycle hook coverage | SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PreCompact, Stop, SessionEnd |
| Hermes adapter | ✗ (deleted; Codex is the only adapter) |
| Claude Code adapter | ✗ (deleted; opencode is supported via MCP + plugin, not an adapter) |
| MCP `costmax_run` execution and evidence retrieval | ✓ (opt-in active path) |
| Active hook output replacement | ✗ (Codex hook limitation; MCP path is separate) |
| Transcript-backed benchmark runner | ✓ (bounded 20-case Codex evaluator) |
