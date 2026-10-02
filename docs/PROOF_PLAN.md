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
   records, retrieval, digest integrity, exit-code propagation, ledger-row
   creation, and guard behavior. Its `--negative` direction corrupts a stored
   digest and proves the verifier fails for that reason.
4. Run the live paired evaluator with fixed fixtures, three global preflights,
   and three repetitions.
5. Run `scripts/verify-live-results.py` against the new evidence directory.
   It audits raw JSONL and fails on missing evidence, a missing/duplicate MCP
   call, any active direct command execution, (for the bounded saving pass)
   any artifact rehydration, or a `manifest.json` binary-hash mismatch when
   `--expect-binary-sha` is given.
6. If a case fails, classify it as product, fixture, model-control, or harness
   failure; fix the root cause; discard that run as proof; and restart the
   complete loop.
7. Only publish a result when active quality and transcript invariants pass for
   every repetition. Record baseline control misses separately instead of
   hiding them.

## Evidence status

The retained historical run (`results/20260805T114713Z-authoritative/`) is
**not present in this checkout**. A fresh local run is now retained at
`results/20260902T-live-proof/` (ignored, compact raw-transcript bundle):
20 fixtures × 3 repetitions, 60/60 active answer passes, exactly one
`costmax_run` per active case, zero direct commands, zero rehydrations, and
36,645 → 16,326 model-visible token estimates (55.4% lower). Its binary hash
is recorded in `manifest.json`; the strict audit command is in
`docs/RESULTS.md`. The one baseline answer miss is a control-model miss and
is intentionally visible; `--require-baseline` remains available for a strict
all-arms gate.

The fresh evidence is local and ignored rather than committed, so a clean
clone must reproduce it or copy the bundle before treating it as portable
repository evidence. Controlled checks (self-test, fixture smoke, the real
active-path verifier, and the independent live-transcript audit) run from a
clean checkout today.

## Current exit criteria

- 20 deterministic fixtures × 3 repetitions.
- 60/60 active answer passes.
- 0 active harness failures.
- Exactly one `costmax_run` and zero direct command executions per active arm.
- All per-case and global MCP preflights pass.
- Artifact metadata, reduction records, byte-for-byte retrieval, and digest
  checks pass (verified by `scripts/verify-active-path.py` against the real
  binary, positive and negative directions).
- The recorded binary hash in `manifest.json` matches the audited binary.
- Savings are reported only as model-visible token estimates (`len(text)/4`),
  never as billed-dollar savings or a general intelligence claim.

### Exit semantics

The run exits `0` (and prints `OVERALL: PASS`) when the active arm and the
harness invariants pass for every repetition — even when a baseline control
answer misses. A baseline answer mismatch on a valid baseline transcript is a
**control miss**: recorded in `report.json` (`baseline.control_miss`), shown
in `report.md` and on the console as a `WARN`, and excluded from baseline-pass
counts, but it does not fail the run. Fail-closed conditions still exit `1`:
missing baseline transcripts, baseline subprocess/command errors, active
errors, active answer mismatches, MCP bypasses, and rehydration-policy
violations (`--forbid-rehydration` in the audit). Baseline answer checking is
never skipped; a miss is always reported, and `--require-baseline` re-asserts
the strict all-or-nothing stance.

The completed run is recorded in [`RESULTS.md`](RESULTS.md), with immutable
transcripts under `results/20260805T114713Z-authoritative/` (to be restored or
re-produced before its numbers are quoted).
