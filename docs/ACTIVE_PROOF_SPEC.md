# CostMax Active Path Proof Spec

## Status: experimental

## Claim

When a model invokes the `costmax_run` MCP tool instead of running Bash directly, CostMax can reduce the model-visible output of verbose commands by returning a compact summary + artifact ID. Raw output is stored locally and retrievable on demand. The tool also returns a transparent recommendation (`reduce`, `passthrough`, `preserve_full`, or `artifact_required`).

## Non-claims

- CostMax does **not** replace Codex's built-in Bash tool output. Hook-based PostToolUse remains observe-only.
- CostMax does **not** guarantee intelligence retention. The compact summary may omit details the model needs. The model can request the full artifact via its ID.
- CostMax itself is local-only and sends no telemetry. However, commands executed
  by `costmax_run` are arbitrary shell invocations and may contact external
  services — CostMax cannot prevent that.
- CostMax does **not** automatically intercept calls. The model must opt in by calling `costmax_run`.
- A command's nonzero exit status is returned as evidence (`Exit: N`), not treated as an MCP transport error.

## Acceptance criteria

1. `costmax_run` executes a local command with optional `cwd`.
2. Raw stdout+stderr is stored as a content-addressed compressed artifact.
3. The classifier selects a deterministic reducer based on output content.
4. The model receives only the compact text + artifact ID.
5. The raw artifact is retrievable byte-for-byte by artifact ID.
6. Token estimates show measurable reduction (raw vs compact).

The MCP server performs one additional guard after rendering the complete
response envelope. If metadata plus the compact result is not smaller than the
raw output, the recommendation is downgraded to `passthrough` automatically.

## Security boundary

- `costmax_run` runs arbitrary shell commands with the same permissions as the
  CostMax process. It has full network, filesystem, and process access.
- There is no sandbox, no network block, and no allowlist.
- `cwd` must resolve within the filesystem; no symlink traversal protection yet.
- Secret redaction (API keys, tokens, JWTs) runs before storage, but the
  redactor is regex-based and may miss novel patterns.
- The tool is opt-in. Codex does not automatically route Bash calls through it.
  The model must explicitly call `costmax_run`.

## Baseline vs active metric

| Metric | Baseline (direct Bash) | Active (costmax_run) |
|--------|------------------------|----------------------|
| Model-visible output | Raw command stdout+stderr | Full MCP response text (includes recommendation, metadata, and compact or preserved output) |
| Raw evidence stored | No (unless redirected) | Yes (compressed, SHA-256) |
| Retrievable by ID | No | Yes, via `resources/read` on `cmx://artifact/{id}` |
| Token estimate (model-visible) | `len(raw_output) / 4` | `len(full_returned_text) / 4` (includes metadata + optional compact) |

## Measurable proof

For any command producing output:

```
raw_tokens = len(raw_output) / 4
full_mcp_tokens = len(full_mcp_response_text) / 4  # includes metadata header, compact text, artifact ID
reduction_pct = (raw_tokens - full_mcp_tokens) / raw_tokens * 100
```

The proof script must fail unless:
- artifact file exists on disk
- reduction record exists in SQLite
- `reduction_pct > 0`
- raw artifact content matches original command output byte-for-byte
