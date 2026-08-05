# CostMax Proof Run

This is the retained result of the authoritative live evaluation completed on
2026-08-05:

`results/20260805T114713Z-authoritative/`

It used the built binary recorded in `manifest.json` (SHA-256
`10d2253d640f7acb43d43b45ce11ee1fd3d1cacfbc51f08f1ad15513558bbc24`), a fixed
set of 20 deterministic fixtures, three repetitions, and three global MCP
preflights. Each case also ran its own transcript-backed preflight before the
active arm.

The recorded environment was Codex CLI `codex-cli 0.146.0`. The earlier
complete run from 2026-07-31 (`results/20260731T151500Z/`) is retained as
historical evidence; the 2026-08-05 run reproduces its outcomes and token
estimates on a fresh binary.

## Exact fixture set

| Fixture | Reducer | Command |
|---|---|---|
| `case-001-verbose-tail` | `terminal` | `cat data.txt` |
| `case-002-failing-tests` | `test` | `cat test_output.txt` |
| `case-003-json-summary` | `json` | `cat users.json` |
| `case-004-lint` | `lint` | `sh eslint --fake` |
| `case-005-search` | `search` | `rg "TODO" src` |
| `case-006-diff` | `diff` | `git diff --no-index before.txt after.txt` |
| `case-007-build` | `build` | `sh build --fake` |
| `case-008-jest-stack` | `test` | `sh jest --fake` |
| `case-009-pytest-collection` | `test` | `sh pytest --collect-only` |
| `case-010-go-test` | `test` | `go test -v ./...` |
| `case-011-typescript-build` | `build` | `sh tsc --noEmit` |
| `case-012-rust-compile` | `build` | `sh cargo-build --fake` |
| `case-013-eslint-warnings` | `lint` | `sh eslint-warnings --fake` |
| `case-014-ripgrep-many-files` | `search` | `rg "FIXME" src` |
| `case-015-git-rename-multi` | `diff` | `git diff --find-renames` |
| `case-016-json-api-groups` | `json` | `sh api --fake` |
| `case-017-json-absent-value` | `json` | `sh api --fake` |
| `case-018-docker-build` | `build` | `sh docker-build --fake` |
| `case-019-package-install` | `terminal` | `sh npm-install --fake` |
| `case-020-terminal-first-last` | `terminal` | `sh deploy-log --fake` |

An independent 10-run loading stress check is retained in
`results/20260731T151000Z-preflight10/` (v3 binary): all 10 preflights exposed
the named `costmaxx` MCP server, called `costmax_run` exactly once, and made
zero direct command or resource calls. It is audited with:

```bash
python3 scripts/verify-live-results.py \
  results/20260731T151000Z-preflight10 \
  --preflight-only --expected-preflights 10
```

## Result

| Check | Result |
|---|---:|
| Paired records | 60 (20 fixtures × 3 repetitions) |
| Active quality passes | 60/60 |
| Active harness failures | 0 |
| Active `costmax_run` calls | 60/60 exactly once |
| Active direct command calls | 0 |
| Per-case preflight passes | 60/60 |
| Global preflight passes | 3/3 |
| Active rehydrations | 0 |
| Baseline control quality passes | 59/60 |
| Non-rehydrated token-saving cases | 44/60 |
| Correct but no-saving cases | 15/60 |

The one baseline control miss was `case-005-search` in run 1: the baseline
model counted nine matching files instead of the fixture's ten. The active
route passed that same case in all three repetitions. This is a control
model-answer failure, not an active CostMax failure. The retained 2026-07-31
run had the same single baseline miss on the same fixture.

The evaluator's model-visible output estimates were:

```text
baseline: 36,645 tokens
active:   14,436 tokens
change:  -22,209 tokens (60.6% lower)
```

These are `len(text) / 4` estimates of tool-result text, not billed-token
measurements. They include CostMax's recommendation and artifact metadata.

## Independent audit

Run the strict second-pass audit against the retained evidence:

```bash
python3 scripts/verify-live-results.py \
  results/20260805T114713Z-authoritative \
  --expected-cases 20 \
  --expected-repetitions 3 \
  --forbid-rehydration
```

The audit parses every raw Codex JSONL transcript. It fails if an active or
preflight transcript is missing, has anything other than exactly one completed
`costmax_run` from the `costmaxx` server, uses the fixture's exact command,
contains a direct `command_execution`, or disagrees with the recorded quality
result. It treats baseline as a control arm and therefore does not require
every baseline answer to pass unless `--require-baseline` is provided.

## Reproduce

The local and live checks used for this result are:

```bash
go test -buildvcs=false -count=1 ./...
python3 scripts/run-codex-eval.py --self-test
python3 scripts/run-codex-eval.py --fixture-smoke
go build -buildvcs=false -o /tmp/costmaxx ./cmd/costmax/
bash scripts/verify-active-path.sh /tmp/costmaxx
python3 scripts/run-codex-eval.py --live --yes \
  --binary /tmp/costmaxx --preflight-runs 3 --repetitions 3
```

Live evaluation spends Codex API usage and writes a new immutable evidence
directory. Always run `verify-live-results.py` on the resulting directory.

## What this proves—and what it does not

This run proves that, for these 20 fixture commands, CostMax's opt-in Codex
MCP path reliably executed the requested command, reduced the visible output
in the saving cases, retained evidence, and preserved the fixture-specific
answer signals without bypassing the MCP route.

It does not prove general intelligence retention, billed-dollar savings, or
that Codex will choose `costmax_run` without prompting. Codex hooks remain
observe-only; the active reduction path is the explicit `costmax_run` MCP tool.
The current proof is Codex-only and uses deterministic fixtures rather than
arbitrary production repositories.
