# Honest numbers

CostMax's credibility is its best feature, so this page is the boring,
uncomfortable version of its claims. Read this before quoting any number.

## What the numbers mean

- **They are `len(text)/4` estimates, not bills.** Model-visible tokens are
  computed as characters divided by four. Real tokenization is
  provider- and model-specific. The estimate is directionally honest — a
  60.6% reduction in estimated input tokens is a real reduction in what
  gets sent to the model — but it is not an invoice, and it is not what you
  will see on a provider billing page.
- **They are input-side only.** CostMax shrinks what the agent *reads*
  (tool output fed into the context window). It does nothing to the tokens
  the agent *writes* — your completion cost is untouched by design.
- **They are one user's stack.** The "Live savings" numbers in the README
  (392,748 → 51,820 tokens, 86.8% cut, 1.04 MB, 53 calls over 7 days) are
  measured on the owner's machine: opencode + codex with
  deepseek-v4-flash. Your workload, model, and agent will differ. Same
  direction, different magnitude.
- **The benchmark numbers are audited; the live numbers are
  self-measured.** The 60.6% figure comes from 20 deterministic fixtures ×
  3 repetitions, re-parsed from raw Codex transcripts by an independent
  script (`scripts/verify-live-results.py`) that fails on any bypass,
  direct command, or rehydration. The live numbers come from `costmaxx
  savings` on the owner's stack. Both are honest; they are not the same
  kind of evidence.

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

1. **`costmaxx savings`** — aggregate savings across sessions (raw tokens,
   model-visible tokens, calls, bytes kept out of context). This is the
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
