# CostMax benchmarks

Methodology: [benchmark-methodology.md](benchmark-methodology.md) ·
Retained run: [RESULTS.md](RESULTS.md) · Audit write-up: [VERIFICATION.md](VERIFICATION.md)

## Benchmark summary

| Benchmark | What it measures | Result |
|---|---|---|
| 20 deterministic fixtures × 3 reps (60 records) | Model-visible tool output, `len(text)/4` estimate | **60.6% less** (36,645 → 14,436) |
| Fixture answer signals (test/build/diff/search/lint/json/terminal) | Quality retention through reduction | **60/60 correct** |
| MCP discipline (per-case preflight + active) | Bypasses / direct commands / rehydrations | **0 / 0 / 0** |
| Baseline control arm (no CostMax) | Model baseline without the tool | 59/60 (1 count miss on case-005-search; active arm passed it 3/3) |

## Fixture coverage

| Category | Cases |
|---|---|
| terminal | verbose-tail, npm-install, deploy-log |
| test | failing-tests, jest-stack, pytest-collection, go-test |
| build | build, typescript-build, rust-compile, docker-build |
| diff | diff, git-rename-multi |
| search | search, ripgrep-many-files |
| lint | lint, eslint-warnings |
| json | json-summary, json-api-groups, json-absent-value |

## How to reproduce

```bash
python3 scripts/run-codex-eval.py --self-test          # parser/fixture validation
python3 scripts/run-codex-eval.py --fixture-smoke      # local command exit-status check
go build -buildvcs=false -o /tmp/costmaxx ./cmd/costmax/
python3 scripts/run-codex-eval.py --live --yes --binary /tmp/costmaxx \
  --preflight-runs 3 --repetitions 3 \
  --results-dir results/<utc-timestamp>-authoritative
python3 scripts/verify-live-results.py results/<dir> \
  --expected-cases 20 --expected-repetitions 3 --forbid-rehydration
```

The live run spends Codex API usage. Always audit the result directory with
`verify-live-results.py`; a directory without a passing audit is not
evidence.

## Benchmark your own workload

The harness accepts new fixtures: add a `case-NNN-*.json` to
`benchmarks/eval-cases/` (prompt, files, expected command, expected answer
signals, exit code) and run the same commands above. If your workload beats
the fixture set, share it in
[Discussions](https://github.com/derinbarutcu17/costmaxx/discussions).

## What these numbers do not mean

- Not billed-dollar savings (estimates use `len(text)/4`).
- Not a universal intelligence-retention claim (deterministic fixtures
  only, Codex CLI 0.146.0).
- Not automatic adoption (the model must call `costmax_run`; hooks are
  observe-only).
