# Copilot instructions for CostMax

CostMax is a local-first efficiency layer for coding agents: a Go CLI +
MCP server that reduces large tool output for Codex while preserving full
evidence locally.

## Ground rules

- **No claim beyond the evidence.** The repo's credibility is its audit
  culture: token figures are `len(text)/4` estimates, not billed tokens;
  quality claims are bounded to the 20 deterministic fixtures; hooks are
  observe-only. Never weaken or soften these boundaries in code, docs, or
  replies.
- **Deterministic reducers.** Reducers are pure regex and must stay
  byte-identical on repeat runs. No LLM calls in the reduction path.
- **Small diffs.** Prefer the smallest correct change. Reuse existing
  helpers (`internal/reducers/shared`, `internal/tokens`) before adding new
  ones.
- **Tests accompany non-trivial logic.** Table-driven Go tests; the
  verification harness (`scripts/run-codex-eval.py --self-test`,
  `--fixture-smoke`) must keep passing.
- **Module path**: `github.com/derinbarutcu17/costmaxx`. Never rename the
  binary (`costmaxx`) or the MCP tool (`costmax_run`) — install paths and
  eval fixtures depend on them.
- **`results/` is gitignored** (multi-GB transcripts). Proof lives in
  `docs/` and is reproducible via scripts.
