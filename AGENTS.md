# AGENTS.md — CostMax (for coding agents)

Safe, concise instructions for Codex, Hermes, and OpenCode agents working in
this repository.

## What CostMax is

A local-first stdio MCP server that, when asked, runs a command and returns a
compact, digest-addressable summary instead of the full raw output. The full
output stays on disk and is retrievable via `cmx://artifact/<id>`.

- MCP use is **opt-in**: the model must call the configured `costmax_run` tool
  (exposed as `costmaxx_costmax_run` in OpenCode and
  `mcp__costmaxx__costmax_run` in Hermes; Hermes' config/tools list calls it
  `costmaxx:costmax_run`).
- Codex lifecycle hooks are **observe-only**: they record evidence and state
  but never replace tool output. CostMax does not automatically compress
  anything and never claims a saving it did not deliver.

## Using the `costmax_run` tool

- **Prefer `costmax_run` for non-interactive, verbose commands** — long test
  runs, diffs, build logs, listings — where you only need the salient signal
  (pass/fail counts, failing tests, one error line) plus the option to
  retrieve full evidence later.
- **Never wrap interactive, destructive, or secrets-sensitive commands in
  `costmax_run`** without explicit need. It executes with process permissions
  and is not a sandbox; piping secrets through it is not warranted. Use the
  normal shell for `rm -rf`, `git push`, credential handling, and anything that
  prompts.
- **Retrieve evidence when exact output is needed.** If a later step depends
  on the precise bytes (a specific line, an error string, a diff), call
  `artifact retrieve <id>` / read the `cmx://artifact/<id>` resource rather
  than relying on the summary.
- **Act on the receipt, not the summary.** Every compressed result carries a
  `Receipt:` line (kept/dropped lines, failing test IDs, `replay: costmaxx
  replay <id>`). Re-run a stored command with `costmaxx replay <id>` instead of
  retyping it.

## What NOT to claim

- Never claim **automatic savings** or **hook replacement**: hooks are
  observe-only and nothing compresses without an explicit `costmax_run` call.
- Never claim **billed-dollar savings**: token figures are `len(text)/4`
  estimates, not provider invoices.
- Never overstate **intelligence retention**: it is proven on deterministic
  fixtures, not arbitrary production repositories.

## Repository conventions

- Go (`go build -buildvcs=false ./...`, `go test -buildvcs=false ./...`,
  `gofmt` clean). `pnpm`/`bun` for the JS/TS bits, `python3` for scripts.
- Do not touch user-home configs (`~/.codex`, `~/.config/opencode`,
  `~/.hermes`, `~/.costmax`) when working in this repo; tests use `HOME`/
  `HERMES_HOME`/`XDG_CONFIG_HOME`/`CODEX_HOME` isolation instead.
- MCP install/uninstall targets: `codex`, `opencode`, `hermes`
  (`costmaxx install --target <t>` / `costmaxx uninstall --target <t>`).
