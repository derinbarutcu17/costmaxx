# Honest numbers

CostMax's credibility is its best feature, so this page is the boring,
uncomfortable version of its claims. Read this before quoting any number.

## Three kinds of evidence — never mix them

- **Personal usage.** Whatever `costmaxx savings` reports on one machine
  measures that user's own stack (agent, model, workload). It is real
  measurement of a single sample, not a benchmark and not a promise.
- **Controlled fixtures.** The 20×3 evaluator run is a bounded, deterministic
  quality/savings test on synthetic outputs. It is audited by re-parsing raw
  transcripts, but it is not your workload and not general intelligence.
- **Unavailable evidence.** Anything whose raw evidence is not present and
  whose run cannot be reproduced is not a claim. The retained 2026-08-05 live
  run is in this category from a clean checkout until its transcripts are
  restored or a fresh run is produced (see docs/RESULTS.md). Do not quote it
  as locally verified.

Organic adoption (does a model *naturally* pick `costmax_run`?) is a separate
experiment from the forced fixture evaluation and is reported separately.

## What the numbers mean

- **They are `len(text)/4` estimates, not bills.** Model-visible tokens are
  computed as characters divided by four. Real tokenization is
  provider- and model-specific. The estimate is directionally honest — a
  reduction in estimated input tokens is a real reduction in what
  gets sent to the model — but it is not an invoice, and it is not what you
  will see on a provider billing page.
- **They are input-side only.** CostMax shrinks what the agent *reads*
  (tool output fed into the context window). It does nothing to the tokens
  the agent *writes* — your completion cost is untouched by design.
- **They are one user's stack.** The "Live savings" numbers in the README
  are measured on the owner's machine: opencode + codex with
  deepseek-v4-flash. Your workload, model, and agent will differ. Same
  direction, different magnitude.
- **The ledger defines the words.** Every accepted call gets one immutable
  row in `call_ledger` with an explicit outcome. "Reductions applied" means
  the compact text was actually the final model-visible response — never a
  stored artifact that the model never saw. "Attempted" and "applied" are
  separate counters; so are passthroughs, guard downgrades, rehydrations, and
  errors. `session_metrics` is a compatibility view, not the source of truth.
- **The benchmark numbers are audited; the live numbers are
  self-measured.** The controlled-fixture figure comes from 20 deterministic
  fixtures × 3 repetitions, re-parsed from raw transcripts by an independent
  script (`scripts/verify-live-results.py`) that fails on any bypass,
  direct command, or rehydration, and now also on a binary-hash mismatch.
  The live numbers come from `costmaxx savings` on the owner's stack. Both
  are honest; they are not the same kind of evidence.

## What CostMax does NOT save

- **Output tokens.** Completion/prose tokens are untouched. Pair with
  [Caveman](https://github.com/JuliusBrussee/caveman)-style skills for
  that axis.
- **Code written.** CostMax will not make your agent write less code or
  simpler solutions. Pair with
  [ponytail](https://github.com/DietrichGebert/ponytail)-style skills for
  that axis.
- **Reasoning/thinking tokens.** Anything the model spends computing is
  outside CostMax's reach.
- **Context you must keep.** Compressed output that the model genuinely
  needs verbatim (e.g. a full diff it is editing) should not go through
  aggressive reduction. The receipt tells you when nothing was cut.

## When it can go net-negative

CostMax only claims savings the rendered response actually delivers — the
post-render guard refuses to count otherwise. But a *negative* claim is
still possible; know the cases:

- **Tiny outputs.** A 30-character command result costs more to envelope
  (metadata, receipt, artifact write) than it saves.
- **Terse workloads.** If your agent's tool calls already return short,
  dense output, there is little to compress and real overhead to pay.
- **Passthrough decisions.** When the policy decides nothing is worth
  cutting, you get the raw output plus envelope overhead — a small net
  loss by design, in exchange for never losing evidence.
- **Rehydration.** Every `artifact retrieve` costs a round-trip. Rare
  retrieval pays for itself many times over; habitual retrieval does not.

The honest framing: CostMax is a win on output-heavy tool calls (test
runs, build logs, diffs, file trees). On everything else, expect a wash.

## How to measure on your own stack

Don't trust the README; the tool is built to report on itself.

1. **`costmaxx savings`** — aggregate savings across calls by actual event
   timestamp (`--since=0` for all history; the default window is 7 days).
   It separates calls processed, artifacts stored, reductions attempted,
   reductions applied, passthroughs, guard downgrades, rehydrations, and
   errors, with token estimates and bytes kept out of context. This is the
   scoreboard.
2. **Daily snapshots** — the maintenance launchd job in
   [docs/INSTALL.md](INSTALL.md) appends
   `costmaxx savings --since 168h` to `~/.costmax/logs/snapshots.log`.
   Watch the trend over weeks, not hours.
3. **The eval harness** — `benchmarks/` ships the deterministic fixtures
   and scorers. Run the full protocol on your own binary:
   [docs/EVALUATION_PROTOCOL.md](EVALUATION_PROTOCOL.md) and
   [docs/benchmarks.md](benchmarks.md). The three-arm design (baseline /
   preflight / active) isolates CostMax's effect from model noise.
4. **Check the receipts.** Every envelope's `Receipt:` line says what was
   kept, dropped, and what failed. If you see `passthrough` receipts on
   calls you expected compressed, your threshold
   (`~/.costmax/config.toml` → `[reduce] threshold`) is set too high for
   your workload.

## The guard

The reason the numbers stay honest is mechanical, not rhetorical:

- A **post-render guard** refuses to record a saving when the full
  rendered response (including envelope metadata) is not actually shorter
  than the raw output.
- `scripts/verify-live-results.py` **fails the entire run** if any active
  case bypassed MCP, called a command directly, rehydrated evidence, or
  missed its answer signal.
- The one control-arm miss in the benchmark (9/10 files counted) is
  published in the README rather than hidden.

If a number ever disagrees with your receipts, trust your receipts — and
file an issue.
