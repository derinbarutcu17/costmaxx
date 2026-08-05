# CostMax Proof Plan

This is the implementation loop for making a CostMax change trustworthy before
calling it usable. It is intentionally repetitive: a single green test run is
not evidence against flaky model routing or transcript mistakes.

## Loop

1. Make the smallest root-cause change.
2. Run deterministic checks: `gofmt`, `go test -count=1 ./...`, evaluator
   `--self-test`, and `--fixture-smoke`.
3. Build a fresh binary with `-buildvcs=false` and run
   `scripts/verify-active-path.sh` to prove artifact storage, reduction
   records, retrieval, and digest integrity.
4. Run the live paired evaluator with fixed fixtures, three global preflights,
   and three repetitions.
5. Run `scripts/verify-live-results.py` against the new evidence directory.
   It audits raw JSONL and fails on missing evidence, a missing/duplicate MCP
   call, any active direct command execution, or (for the bounded saving pass)
   any artifact rehydration.
6. If a case fails, classify it as product, fixture, model-control, or harness
   failure; fix the root cause; discard that run as proof; and restart the
   complete loop.
7. Only publish a result when active quality and transcript invariants pass for
   every repetition. Record baseline control misses separately instead of
   hiding them.

## Current exit criteria

- 20 deterministic fixtures × 3 repetitions.
- 60/60 active answer passes.
- 0 active harness failures.
- Exactly one `costmax_run` and zero direct command executions per active arm.
- All per-case and global MCP preflights pass.
- Artifact metadata, reduction records, byte-for-byte retrieval, and digest
  checks pass.
- Savings are reported only as model-visible token estimates (`len(text)/4`),
  never as billed-dollar savings or a general intelligence claim.

The completed run is recorded in [`RESULTS.md`](RESULTS.md), with immutable
transcripts under `results/20260805T114713Z-authoritative/`.
