# CostMax Evaluation Protocol

## Status: experimental. One case is not a general intelligence conclusion.

## Claim boundary

The evaluation measures whether `costmax_run` can reduce model-visible output
tokens for commands the model chooses to route through it, while preserving
the model's ability to answer task-specific questions from the compact result.

It does **not** measure:
- General intelligence retention across all task types
- Whether the model will choose `costmax_run` over Bash (the model must opt in)
- Whether answers from compact output are equivalent to answers from full output
  in every case — that requires a separate study with many cases

## Threats to validity

- The model's prompt phrasing influences whether it uses `costmax_run` or Bash.
  The evaluation uses a fixed prompt, but results may vary with different
  phrasing.
- Token estimation uses `len(text) / 4`, not model-specific tokenizers.
- CostMax metadata (artifact ID, URI, token counts) is included in the
  active-route visible text, which inflates the active token count. The raw
  output has no such overhead.
- A single case passing does not predict performance on different tasks.

## Cost warning

Each Codex `exec --ephemeral` call spends API usage. The `--live` flag exists
to make this explicit; the default mode runs a parser self-test with no API
calls.

## Usage

```bash
# Self-test (no API calls, no binary needed)
python3 scripts/run-codex-eval.py --self-test

# Execute every fixture command locally (no API calls)
python3 scripts/run-codex-eval.py --fixture-smoke

# Live evaluation (requires --binary pointing to a built costmaxx)
go build -buildvcs=false -o /tmp/costmaxx ./cmd/costmax/
python3 scripts/run-codex-eval.py --live --yes --binary /tmp/costmaxx --case case-001

# MCP loading stress check (no paired task cases)
python3 scripts/run-codex-eval.py --live --yes --binary /tmp/costmaxx \
  --preflight-runs 10 --preflight-only
```

Live runs pass the MCP command explicitly through Codex's `--config` flags,
exclude unrelated user MCP servers, and require a transcript-backed preflight
before each active case. A preflight retries only a subprocess timeout once;
model or protocol violations fail closed. Every run is retained under a dated
`results/` folder and can be independently audited with:

```bash
python3 scripts/verify-live-results.py results/<timestamp> \
  --expected-cases 20 --expected-repetitions 3 --forbid-rehydration
```

The audit reads the raw JSONL, not just the summary report, and fails on a
missing transcript, a non-singleton `costmax_run`, or any direct active-arm
command execution. `--forbid-rehydration` is appropriate for the bounded
quality-and-saving pass; a separate run may omit it when testing recoverability.

## Case design

Each fixture is a JSON file with:

| Field | Purpose |
|-------|---------|
| `id` | Unique identifier |
| `description` | Human-readable summary |
| `files` | Project files to create (path → content) |
| `prompt` | The prompt given to Codex |
| `expected_answer` | List of regex patterns the final answer must match |
| `expected_command` | The shell command the model should run (for baseline verification) |
| `expected_exit_code` | Expected command exit status; nonzero may be valid (for example, `git diff --no-index`) |
| `reducer_target` | Reducer the fixture is designed to exercise |
| `expected_active_behaviour` | Description of what costmax_run should do |

## Metrics

| Metric | Definition |
|--------|------------|
| Baseline visible chars | `len(bash_tool_result_text)` for the expected command |
| Active visible chars | `len(full_costmax_run_result_text)` including all metadata |
| Baseline visible tokens | `len(bash_tool_result_text) / 4` |
| Active visible tokens | `len(full_costmax_run_result_text) / 4` |
| Rehydration flag | Whether `read_mcp_resource` was called for this task |
| Costmax_run calls | Count of `costmax_run` MCP tool invocations |
| Direct Bash calls | Count of all Codex `command_execution` events in the active arm |
| Answer match | Whether the final model answer matches all `expected_answer` regexes |
| Outcome | `quality_and_saving`, `quality_no_saving`, `quality_failure`, `quality_with_rehydration`, or `harness_failure` |

## Pass/fail rules

A case passes when:
1. Baseline: the model produces the expected command output and the answer
   matches all regexes.
2. Active: the model calls `costmax_run` **exactly once**, executes no direct
   command, and the answer matches all regexes.

A case fails when:
- No tool call matches the expected command (model took a different approach)
- Active mode calls `costmax_run` zero times or more than once
- Active mode executes any direct command
- Answer regexes do not match
- The runner itself errors (infrastructure failure, reported separately)

## Rehydration reporting

If the active mode calls `read_mcp_resource` to retrieve full evidence, the
case is marked "rehydrated" and the token savings column says "partial".
The model-visible tokens for the rehydrated case include the retrieved
content. Rehydrated cases do not count toward a savings claim.

## Evidence retention

Each live invocation stores fixture hashes, the CostMax binary SHA-256, Codex
version, preflight transcripts, paired-run JSONL transcripts, and JSON/Markdown
reports in a dated `results/<UTC timestamp>/` directory. A missing transcript
or invalid arm is a harness failure, never a pass.
