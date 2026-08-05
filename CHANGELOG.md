# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
