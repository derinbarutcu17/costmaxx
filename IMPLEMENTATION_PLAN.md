# CostMax: Proof-First Usability Upgrade

Status: execution plan for the OpenCode build loop
Owner: Codex orchestrator
Coding workhorse: OpenCode `build` agent
Target model: `opencode-go/deepseek-v4-flash`

## Mission

Turn CostMax from a promising, opt-in output compressor into a trustworthy,
usable project whose savings, safety, and adoption claims are backed by
reproducible evidence.

The project must improve work per dollar without pretending that a character
estimate is a bill, that a fixture suite is general intelligence, or that an
observe-only hook is an automatic replacement mechanism.

## Ground truth at the start of this plan

The following facts were read directly from the repository and the local
CostMax store on 2026-09-01. Treat them as a baseline, not as acceptance claims
for the finished work.

- Local SQLite store: `/Users/derin/.costmax/costmax.db`.
- Persisted metrics: 13 metric rows, 66 recorded calls, 408,992 raw estimated
  tokens, 53,445 model-visible estimated tokens, 355,547 estimated tokens saved
  (86.9%).
- The `cli-` aggregate row contains 54 calls and 400,635 → 52,899 estimated
  tokens (86.8%). This is an attribution convention, not a proven harness
  identity: `session_metrics` has no harness column.
- Retained evidence: 2 artifact rows and 2 reduction rows. Six task-state rows
  exist, but `events` and `sessions` are empty.
- One retained reduction is useful (31,548 → 4,318 bytes). One small output
  expands (1,284 → 1,355 bytes) and was not applied to the model-visible
  response.
- The repository documents a 20-fixture × 3-repetition Codex proof run with
  60/60 active answer checks and 36,645 → 14,436 estimated visible tokens
  (60.6%). The referenced raw `results/` directory is absent from this
  checkout, so that run is not locally re-auditable yet.
- Passing baseline checks already observed: `go test -count=1 ./...`,
  `go vet ./...`, evaluator self-test, 20-fixture smoke test, five-platform
  release build, and SVG overflow checks.
- Known blocker: `scripts/verify-active-path.sh` invokes the missing
  `scripts/verify-active-path.py`.
- Known product gaps documented in `docs/VERIFICATION.md`: cumulative metrics
  distort time windows, `artifacts_reduced` is not a true successful-reduction
  count, redaction misses AWS/Bearer/spaced assignment forms, classifier
  false-positive/false-negative cases exist, and `COSTMAX_DISABLE` is stale.
- Product boundary: hooks are observe-only; the active reduction route is the
  explicit `costmax_run` MCP tool.

## Non-negotiable truth boundaries

1. Never claim billed-dollar savings without provider-token and price data.
2. Keep `len(text)/4` numbers labeled as estimates and input-side only.
3. Never call a documented historical result locally verified unless the raw
   transcript/evidence is available and the verifier passes.
4. Never imply that Codex hooks replace tool output when the hook contract only
   permits observe/inject behavior in the supported configuration.
5. Never weaken a test, verifier, redactor, policy guard, or safety boundary to
   make a run green.
6. Preserve unrelated user changes, especially the existing uncommitted
   `README.md` edit, unless a narrowly required documentation correction is
   made and explicitly reported.

## Workstreams and implementation requirements

### 1. Replace cumulative rollups with an immutable call ledger

Add a numbered SQLite migration after the current schema. The new table should
persist exactly one row per accepted command/tool call and include, at minimum:

- immutable call ID and event timestamp;
- session ID and explicit harness/source (`codex_hook`, `mcp`, `cli`, etc.);
- command and working directory with the same redaction policy as output;
- classifier category and reducer name/version;
- raw and model-visible bytes plus raw and model-visible token estimates;
- policy recommendation and final guarded decision;
- `reduction_applied` boolean distinct from `reduction_attempted`;
- artifact ID and reduction ID when present;
- exit code, rehydration flag, hook/error status, and a compact outcome enum.

Write the row atomically with the artifact/reduction metadata for each call.
Use an idempotency key so a retry or duplicate subprocess cannot double count a
call. Keep `session_metrics` only as a compatibility/read model, or derive it
from the ledger; do not let it remain the source of truth for time windows.

Add indexes for timestamp, harness, session, category, decision, and artifact.
Ensure concurrent processes on one data directory remain safe.

### 2. Correct metric semantics and reporting

Separate these concepts everywhere:

- calls observed/processed;
- artifacts stored;
- reduction attempted;
- reduction applied to model-visible output;
- passthrough/no-saving decisions;
- guard downgrades;
- rehydrations;
- errors/fail-open events.

Update `savings`, `report`, and any JSON output to aggregate ledger rows by
actual event timestamp. Support all-history and true `--since` windows. Expose
raw, visible, saved, reduction rate, passthrough rate, artifact count,
rehydration count, and error count with clear denominators. Do not label a
processed artifact as “reduced” if the final response was passthrough.

Add a deterministic report fixture covering: a real reduction, an equal/longer
compact result, a policy passthrough, a guard downgrade, a duplicate retry, and
a rehydration. Verify totals and time-window boundaries exactly.

### 3. Restore a real active-path verifier

Repair or replace `scripts/verify-active-path.sh` so it points to an existing,
tested verifier. It must execute the real built binary and verify, at minimum:

- command execution and exit-code propagation;
- artifact metadata and digest integrity;
- byte-for-byte retrieval;
- reduction record persistence;
- final policy/guard behavior;
- metrics ledger row creation;
- no false positive saving on a tiny or expanded compact result.

The verifier must fail closed on missing files, malformed JSON, missing rows,
wrong digests, wrong decisions, or silent bypasses. Add both positive and
negative fixtures. The negative direction must intentionally corrupt or remove
one expected invariant and demonstrate a non-zero failure before restoring the
fixture.

### 4. Make proof evidence reproducible from the checkout

Choose one honest path and document it:

- restore the authoritative raw transcripts under a size-safe, immutable
  evidence location; or
- make a fresh run and retain its manifest, hashes, transcripts, and report in
  a documented artifact location that can be audited from a clean checkout.

Update `docs/RESULTS.md`, `docs/PROOF_PLAN.md`, and `docs/VERIFICATION.md` so
paths and commands actually exist. The result verifier must check expected
case/repetition counts, exactly one MCP call, zero active direct commands, the
rehydration policy, answer signals, and binary hash. Never replace missing
evidence with hand-written summary numbers.

### 5. Separate organic-adoption proof from forced benchmark proof

Keep the bounded 20×3 fixture evaluation as a controlled reduction/quality
test. Add a separate adoption protocol that records whether a model naturally
chooses `costmax_run` or bypasses it for verbose commands. Report:

- eligible calls;
- CostMax-routed calls;
- direct/bypassed calls;
- reduction-applied calls;
- rehydrated calls;
- quality outcomes and failures.

Do not activate hook replacement unless the real Codex hook protocol and a
running CLI smoke test prove the response semantics. Observe-only behavior is
the safe default.

### 6. Close the documented safety and quality gaps

Add regression tests and narrow fixes for:

- AWS secret key/value forms;
- `Authorization: Bearer ...` forms;
- spaced/quoted secret assignments;
- command-string redaction before persistence;
- classifier token boundaries and known command aliases;
- JSON with leading whitespace;
- `find`/search precedence;
- stale `COSTMAX_DISABLE` behavior or remove the claim completely.

Preserve fail-open behavior for malformed hook payloads and never log secrets.
Use adversarial fixtures, not only the examples from existing tests.

### 7. Reconcile product surface and documentation

Audit README, plugin manifest, hooks, install/uninstall commands, capability
matrix, architecture docs, and skill text against the actual code. Resolve
contradictions about Codex, opencode, Hermes, Claude, active mode, installers,
and MCP. Every user-facing command must either work in a clean home directory
or be removed/documented as unavailable.

Keep claims scoped to what the evidence proves. Update the “honest numbers”
page with the new ledger definitions and an explicit distinction between
personal usage, controlled fixtures, and unavailable evidence.

## Required test matrix

Every implementation pass must run the applicable checks below and record the
actual command and exit status:

1. `gofmt -w` on changed Go files, then `go test -buildvcs=false -count=1 ./...`.
2. `go vet ./...`.
3. `go test -race -buildvcs=false -count=1 ./...` when storage/concurrency code
   changes.
4. `python3 scripts/run-codex-eval.py --self-test`.
5. `python3 scripts/run-codex-eval.py --fixture-smoke`.
6. Build a fresh binary with `-buildvcs=false` and print its SHA-256.
7. `bash scripts/verify-active-path.sh <fresh-binary>`.
8. Run the strict live-result verifier only when raw live evidence exists;
   otherwise report the external/evidence blocker instead of fabricating a
   result.
9. `bash scripts/build-release.sh <version> <temporary-output>` and confirm
   five binaries plus five checksums.
10. Run relevant CLI clean-home, MCP stdio, hook, migration, artifact, privacy,
    and concurrency tests.

## Proof loop (repeat until the exit criteria hold)

OpenCode must work one bounded phase at a time. After each phase:

1. Inspect the current diff and the affected call paths.
2. Run the smallest targeted tests.
3. Run the full deterministic matrix above.
4. Run the active-path verifier and inspect its output, not just its exit code.
5. Run the negative direction: revert or sabotage only the production behavior
   under test while keeping the new test/verifier; confirm it fails for the
   intended reason; restore the production change.
6. Classify every failure as product bug, test bug, fixture bug, model/control
   variance, environment issue, or missing external evidence.
7. Feed the exact failure, command, output, and file/line context back to
   OpenCode. Do not ask for a vague “please fix.”
8. Repeat the phase until positive and negative directions both pass.
9. Only then continue to the next phase.

If OpenCode stops early, loops, requests a permission it does not need, or
claims completion without executable evidence, the orchestrator must interrupt
or follow up with a precise corrective prompt and continue.

## Definition of done

The project is complete only when all of these are true:

- immutable per-call ledger exists, migrates from a clean database, and is
  concurrency/idempotency tested;
- `savings` and `report` use event timestamps and correct reduction semantics;
- a fresh binary passes a real active-path verifier whose negative direction has
  also been demonstrated;
- proof evidence is present and independently auditable from the checkout, or
  the exact external blocker is recorded and no proof claim is made;
- controlled fixture quality/savings and organic adoption are reported as
  separate experiments;
- documented privacy/classifier/CLI gaps in scope are fixed with adversarial
  regression tests;
- hooks remain honest and observe-only unless real Codex smoke evidence proves
  otherwise;
- docs, plugin metadata, commands, and capability claims match the code;
- full tests, vet/race where relevant, fixture checks, verifier, and release
  build pass;
- final report includes files changed, migrations, commands/results, positive
  and negative proof, remaining risks, and any external blockers.

## OpenCode operating instructions

Read this file and the repository docs before editing. Work directly in the
CostMax checkout. Do not push, create a PR, delete unrelated user work, or
rewrite the README wholesale. Use normal reversible edits and keep commits out
of scope unless explicitly requested.

Use the existing architecture unless a test-backed root-cause change requires
otherwise. Prefer small, reviewable patches. Do not invent benchmark results or
claim a live integration was run when credentials/evidence are absent.

At the end of every pass, print:

- phase completed;
- files changed;
- tests/checks run and exact results;
- negative-direction result;
- remaining risks/blockers;
- the next bounded phase.

The orchestrator will inspect that output, run independent checks, and send the
next corrective prompt. Continue until the Definition of Done is met or a
genuine external blocker remains after all independent work is complete.
