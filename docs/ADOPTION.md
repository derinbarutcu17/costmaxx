# Organic-Adoption Protocol

This is the **separate experiment** for measuring whether a model naturally
chooses CostMax's `costmax_run` tool, distinct from the bounded 20×3 fixture
evaluation in `docs/PROOF_PLAN.md`.

## Why separate

The controlled evaluation **forces** `costmax_run` (the active-arm prompt says
"You MUST use the MCP tool costmax_run"). That proves the active path works —
it proves nothing about whether a model would reach for it on its own.

Adoption is about defaults, not capability. A reduction tool nobody reaches
for saves nothing. This protocol records the default.

## Protocol

For each fixture case, run the same task in a fresh repo with the CostMax MCP
server available but **never mentioned**:

```bash
python3 scripts/run-codex-eval.py --adoption --yes \
  --binary /tmp/costmaxx --results-dir results/adoption-<stamp>
```

The prompt is just the fixture question plus "Run the command you need and
answer the question." No tool guidance, no routing, no costmax_run mention.
The model may pick `costmax_run`, a direct command, both, or neither.

## Reported metrics

`adoption.json` records one row per case, and `adoption.md` summarizes:

- **eligible calls** — every fixture is an eligible command-execution task;
- **CostMax-routed calls** — the model called `costmax_run` at least once;
- **direct/bypassed calls** — the model used a direct command and never
  `costmax_run`;
- **reduction-applied calls** — a routed call whose envelope actually carried
  a reduce/artifact_required recommendation;
- **rehydrated calls** — the model read a `cmx://artifact` resource back;
- **quality outcomes and failures** — answer-signal passes and any
  harness/exit errors.

## Honest framing

- Adoption numbers are model- and context-dependent, one run at a time.
- A routed call is not automatically a saving; only `reduction-applied`
  calls are.
- Failures (no tool used, codex exit, setup) are reported, never hidden.
- No billed-dollar savings and no intelligence claim are derived from this
  experiment.

## Fail-closed startup

The runner verifies the toolchain (Codex CLI and `git` on PATH, plus the
`--binary` path) before creating any evidence directory. A missing tool aborts
with a concise actionable `ERROR:` message and exit 1 — no traceback and no
partially-created `results/` directory. The same pre-check guards the
controlled `--live` run.

The controlled `--live` run shares the exit semantics documented in
`docs/EVALUATION_PROTOCOL.md`: it exits 0 when the active arm and harness
invariants pass, recording any baseline answer mismatch on a valid baseline
transcript as an explicit control miss/warning instead of failing. Missing
baseline transcripts, baseline subprocess/command errors, active errors, and
MCP bypasses still fail closed.

## Status

The `--adoption` runner and its transcript classifier are implemented and
self-tested (`python3 scripts/run-codex-eval.py --self-test`). A live adoption
run spends Codex API usage and requires the Codex CLI and credentials; no live
adoption run has been executed from this checkout. Until one is, adoption
metrics are **unmeasured**, not zero and not claimed.

## Hook replacement remains off

Nothing in this protocol activates hook-based replacement of tool output.
Hooks stay observe-only; the active reduction route is the explicit
`costmax_run` MCP tool. Hook replacement would require a real Codex hook
protocol demo plus a running CLI smoke test of the response semantics before
it could even be considered — that evidence does not exist today.