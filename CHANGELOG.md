# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Cross-agent setup.** `costmaxx install --target hermes` now merges a
  backup-protected `mcp_servers.costmaxx` entry into Hermes' YAML config;
  `costmaxx doctor` reports Codex, opencode, and Hermes MCP registration plus
  the local handshake. Shared `AGENTS.md` guidance and the validated Codex
  plugin manifest teach agents to use the opt-in MCP path safely.

### Fixed

- **Idempotency retries never double-count or orphan evidence.** The shared
  pipeline pre-checks the immutable ledger before writing an artifact file
  (deterministic callers), so a duplicate logical call produces exactly one
  ledger/artifact/reduction row, returns the byte-identical stored envelope
  (same artifact id), and leaves no unreferenced file. A post-hoc orphan
  purge guards the remaining window.
- **CLI call identity is honest by default.** `costmaxx artifact add` assigns
  a fresh unique call reference per invocation, so independent identical
  invocations are recorded as separate calls (no content-derived undercount).
  `--call-ref`/`--idempotency-key` opts into retry deduplication: the same
  explicit ref replays the canonical stored envelope (deterministic replay,
  one ledger/artifact/reduction row) even when the retried input differs.
- **Codex hooks persist atomically and count only real calls.** `PostToolUse`
  now writes the artifact, reduction record, and ledger row in one
  `RecordCall` transaction; the in-process metrics and the compatibility
  `session_metrics` row are updated only for a newly inserted call with
  per-call deltas (no more cumulative-snapshot or pre-insert inflation on
  duplicate retries), and DB errors fail open into an observable `error`
  ledger row instead of being ignored. An empty-output PostToolUse records an
  observable zero-byte ledger row with no invented saving. The ledger records
  the real working directory and redacted command.
- **MCP idempotency boundary is documented and tested.** A same-id retry
  inside one server process is answered from stored evidence (no second
  execution); a new subprocess is a distinct session and records separately.
- **The active-path verifier's `--negative` direction now fails with a real
  non-zero exit** when evidence is tampered, instead of reporting a swallowed
  internal PASS.
- **Ledger time windows slice by real event time.** Migration v6 normalizes
  `call_ledger` timestamps to a fixed-width UTC layout (previously variable
  RFC3339Nano, whose string order inverts sub-second values like `.5Z` vs
  `.55Z` and could drop in-window calls from `--since` and `report` windows).
- **The eval runner fails closed when the Codex CLI or git is missing.**
  `scripts/run-codex-eval.py --live`/`--adoption` now verify the toolchain
  before creating any evidence directory (concise actionable error, exit 1,
  no traceback, no partial results dir); `--adoption` no longer crashes on an
  undefined `cases`; a missing `codex` binary returns a PATH hint; and the
  preflight timeout retry actually retries (the old timeout branch was dead
  code because `run_codex` reports timeouts as rc=-1, not an exception).
- **The eval runner no longer fails on a baseline control miss.** A baseline
  answer mismatch on a valid baseline transcript is recorded as an explicit
  control miss/warning — `baseline.control_miss` in `report.json`, a `Control
  miss` column + aggregate + detail section in `report.md`, and a console
  `WARN` line — and the run still exits 0 when the active arm and the harness
  invariants pass (60/60 active quality is the product claim). Missing
  baseline transcripts, baseline subprocess/command errors, active
  errors/answer mismatches, MCP bypasses, and rehydration-policy violations
  remain fail-closed; `verify-live-results.py --require-baseline` stays strict
  and fails on the control miss. Deterministic self-tests cover the
  classification, aggregation, and exit semantics.

### Docs

- opencode auto-compression plugin is explicitly documented as **not shipped**
  in this repository (no plugin file under `packages/` or the repo root);
  `costmaxx install` scope and safety (one file, backup, refuse-overwrite)
  are documented.

## [0.1.2] - 2026-08-05

### Fixed

- `costmaxx --version` via plain `go install` now reports the module version
  (build-info fallback; the previous attempt bound the version before build
  info was read)

## [0.1.1] - 2026-08-05

### Fixed

- `costmaxx --version` now reports the module version when installed via
  plain `go install` (build-info fallback instead of "dev")

## [0.1.0] - 2026-08-05

First public release.

### Added

- `costmax_run` MCP tool: executes a local command, stores full output as a
  zstd-compressed, SHA-256 content-addressed artifact, returns a compact
  summary with artifact reference
- Deterministic reducers for test, build, diff, search, lint, JSON,
  terminal, and generic output
- Recommendation policy with conservative overhead and post-render guard
  (no saving claimed unless the rendered response is actually shorter)
- Observe-only Codex lifecycle hooks with session-keyed task state
- SQLite storage: events, artifacts, reduction records, session metrics
  (schema migrations through v3)
- CLI: `install`, `uninstall`, `doctor`, `status`, `state`, `report`, `gc`,
  `hook`, `mcp`
- Evaluation harness: 20 deterministic fixtures, baseline/preflight/active
  arms, strict raw-transcript audit (`verify-live-results.py`)

### Fixed

- Signal-killed commands now report `128+signal` exit codes instead of `-1`
- `ReadRange` no longer panics on inverted ranges
- Concurrent process startup on a shared data dir retries `SQLITE_BUSY`
  during migration instead of failing

### Verified

- Live evaluation: 60/60 active quality passes, 0 bypasses, 0 rehydrations,
  60.6% lower model-visible tokens (36,645 → 14,436, `len(text)/4`
  estimates), 59/60 baseline control (single count miss, reported honestly)
