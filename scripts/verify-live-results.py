#!/usr/bin/env python3
"""Strictly audit a CostMax live-evaluation evidence directory.

The evaluator already records per-case assertions. This second pass audits the
raw Codex JSONL transcripts so a report cannot claim an active pass when the
model bypassed MCP, called a command directly, or when evidence is missing.
"""

import argparse
import collections
import glob
import json
import pathlib
import sys

PROJECT_ROOT = pathlib.Path(__file__).resolve().parent.parent


def events(path):
    for line in path.read_text().splitlines():
        try:
            yield json.loads(line)
        except json.JSONDecodeError:
            continue


def completed_items(path):
    return [
        event.get("item", {})
        for event in events(path)
        if event.get("type") == "item.completed"
    ]


def transcript_counts(path):
    items = completed_items(path)
    mcp = [
        item
        for item in items
        if item.get("type") == "mcp_tool_call"
        and item.get("tool") == "costmax_run"
        and item.get("server") == "costmaxx"
        and item.get("status") == "completed"
    ]
    commands = [item for item in items if item.get("type") == "command_execution"]
    resources = [
        item
        for item in items
        if item.get("type") == "mcp_tool_call"
        and item.get("tool") == "read_mcp_resource"
        and item.get("status") == "completed"
    ]
    return len(mcp), len(commands), len(resources)


def completed_costmax_calls(path):
    return [
        item
        for item in completed_items(path)
        if item.get("type") == "mcp_tool_call"
        and item.get("tool") == "costmax_run"
        and item.get("server") == "costmaxx"
        and item.get("status") == "completed"
    ]


def command_argument(call):
    arguments = call.get("arguments", {})
    if isinstance(arguments, str):
        try:
            arguments = json.loads(arguments)
        except json.JSONDecodeError:
            return None
    return arguments.get("command") if isinstance(arguments, dict) else None


def fixture_commands():
    commands = {}
    for path in glob.glob(str(PROJECT_ROOT / "benchmarks" / "eval-cases" / "*.json")):
        data = json.loads(pathlib.Path(path).read_text())
        commands[data["id"]] = data["expected_command"]
    return commands


def evidence_path(root, path_text):
    """Resolve a report path and reject evidence outside the audited directory."""
    if not path_text:
        return None, "missing path"
    path = pathlib.Path(path_text)
    if not path.is_absolute():
        path = root / path
    try:
        path = path.resolve()
        path.relative_to(root)
    except ValueError:
        return None, "path points outside results directory"
    return path, None


def audit_preflight_only(root, expected):
    path = root / "preflights.json"
    if not path.is_file():
        print(f"FAIL: missing {path}")
        return 1
    try:
        records = json.loads(path.read_text())
    except json.JSONDecodeError as exc:
        print(f"FAIL: invalid {path}: {exc}")
        return 1
    failures = []
    for record in records:
        run = record.get("run", "?")
        if not record.get("ok"):
            failures.append(f"run {run}: report marked preflight failed")
        transcript, path_error = evidence_path(root, record.get("transcript"))
        if path_error:
            failures.append(f"run {run}: {path_error}")
            continue
        if not transcript.is_file():
            failures.append(f"run {run}: missing transcript")
            continue
        calls = completed_costmax_calls(transcript)
        _, commands, resources = transcript_counts(transcript)
        if len(calls) != 1:
            failures.append(f"run {run}: costmax_run calls={len(calls)}, expected 1")
        elif command_argument(calls[0]) != "printf costmax-preflight-ok":
            failures.append(f"run {run}: wrong command")
        if commands:
            failures.append(f"run {run}: direct command calls={commands}, expected 0")
        if resources:
            failures.append(f"run {run}: resource reads={resources}, expected 0")
    if expected and len(records) != expected:
        failures.append(f"expected {expected} preflights, found {len(records)}")
    print(f"Evidence: {root}")
    print(f"Preflights: {len(records)}")
    print(f"Exact MCP preflight calls: {sum(len(completed_costmax_calls(pathlib.Path(record['transcript']))) for record in records if record.get('transcript'))}")
    print(f"Failures: {len(failures)}")
    if failures:
        for failure in failures:
            print(f"  - {failure}")
        return 1
    print("PASS: all preflight transcripts verified")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("results_dir", type=pathlib.Path)
    parser.add_argument("--expected-cases", type=int, default=0)
    parser.add_argument("--expected-repetitions", type=int, default=0)
    parser.add_argument(
        "--require-baseline",
        action="store_true",
        help="also fail if any baseline control answer does not match",
    )
    parser.add_argument(
        "--forbid-rehydration",
        action="store_true",
        help="fail if an active transcript reads a full artifact",
    )
    parser.add_argument(
        "--preflight-only",
        action="store_true",
        help="audit preflights.json instead of a paired report.json",
    )
    parser.add_argument(
        "--expected-preflights",
        type=int,
        default=0,
        help="expected count for --preflight-only",
    )
    args = parser.parse_args()
    root = args.results_dir.resolve()
    if args.preflight_only:
        return audit_preflight_only(root, args.expected_preflights)
    report_path = root / "report.json"
    if not report_path.is_file():
        print(f"FAIL: missing {report_path}")
        return 1
    try:
        report = json.loads(report_path.read_text())
    except json.JSONDecodeError as exc:
        print(f"FAIL: invalid report JSON: {exc}")
        return 1
    if not isinstance(report, list) or not report:
        print("FAIL: report.json must contain a non-empty list")
        return 1

    failures = []
    outcomes = collections.Counter()
    transcript_totals = collections.Counter()
    expected_commands = fixture_commands()
    runs = collections.Counter()
    cases = set()
    rehydrated = 0
    for record in report:
        case_id = record.get("case_id", "?")
        run = record.get("run", "?")
        cases.add(case_id)
        runs[run] += 1
        outcome = record.get("outcome", "?")
        outcomes[outcome] += 1
        if record.get("active", {}).get("rehydrated"):
            rehydrated += 1

        if not record.get("active", {}).get("all_match") or record.get("active", {}).get("error"):
            failures.append(f"{case_id} run {run}: active report is not a clean quality pass")
        if args.require_baseline and (
            not record.get("baseline", {}).get("all_match")
            or record.get("baseline", {}).get("error")
        ):
            failures.append(f"{case_id} run {run}: baseline control failed")

        paths = record.get("transcripts", {})
        for phase in ("preflight", "active"):
            path_text = paths.get(phase)
            path, path_error = evidence_path(root, path_text)
            if path_error:
                failures.append(f"{case_id} run {run}: {phase} transcript {path_error}")
                continue
            if not path.is_file():
                failures.append(f"{case_id} run {run}: missing {phase} transcript")
                continue
            mcp, commands, resources = transcript_counts(path)
            transcript_totals[f"{phase}_mcp"] += mcp
            transcript_totals[f"{phase}_commands"] += commands
            transcript_totals[f"{phase}_resources"] += resources
            if mcp != 1:
                failures.append(f"{case_id} run {run}: {phase} costmax_run calls={mcp}, expected 1")
            if commands:
                failures.append(f"{case_id} run {run}: {phase} direct command calls={commands}, expected 0")
            if phase == "active":
                reported = bool(record.get("active", {}).get("rehydrated"))
                if bool(resources) != reported:
                    failures.append(
                        f"{case_id} run {run}: report rehydrated={reported} but transcript reads={resources}"
                    )
                if args.forbid_rehydration and resources:
                    failures.append(
                        f"{case_id} run {run}: active resource reads={resources}, expected 0"
                    )
            calls = completed_costmax_calls(path)
            if len(calls) == 1:
                expected = "printf costmax-preflight-ok" if phase == "preflight" else expected_commands.get(case_id)
                actual = command_argument(calls[0])
                if expected is None:
                    failures.append(f"{case_id} run {run}: no fixture command is defined")
                elif actual != expected:
                    failures.append(
                        f"{case_id} run {run}: {phase} command={actual!r}, expected {expected!r}"
                    )

        # Baseline is intentionally allowed to use one direct command. It is a
        # control arm, not part of the active-path safety assertion.
        baseline_path = paths.get("baseline")
        baseline_path, baseline_error = evidence_path(root, baseline_path)
        if baseline_error:
            failures.append(f"{case_id} run {run}: baseline transcript {baseline_error}")
        elif not baseline_path.is_file():
            failures.append(f"{case_id} run {run}: missing baseline transcript")

    if args.expected_cases and len(cases) != args.expected_cases:
        failures.append(f"expected {args.expected_cases} cases, found {len(cases)}")
    if args.expected_repetitions:
        bad_runs = {run: n for run, n in runs.items() if n != len(cases)}
        if len(runs) != args.expected_repetitions or bad_runs:
            failures.append(
                f"expected {args.expected_repetitions} complete repetitions, found {dict(runs)}"
            )

    global_preflights = root / "preflights.json"
    if global_preflights.is_file():
        try:
            preflights = json.loads(global_preflights.read_text())
            for item in preflights:
                if not item.get("ok"):
                    failures.append(f"global preflight {item.get('run', '?')} failed")
                path, path_error = evidence_path(root, item.get("transcript"))
                if path_error:
                    failures.append(f"global preflight {item.get('run', '?')}: {path_error}")
                    continue
                if not path.is_file():
                    failures.append(f"global preflight {item.get('run', '?')}: missing transcript")
                    continue
                calls = completed_costmax_calls(path)
                _, commands, _ = transcript_counts(path)
                if len(calls) != 1:
                    failures.append(
                        f"global preflight {item.get('run', '?')}: costmax_run calls={len(calls)}, expected 1"
                    )
                elif command_argument(calls[0]) != "printf costmax-preflight-ok":
                    failures.append(f"global preflight {item.get('run', '?')}: wrong command")
                if commands:
                    failures.append(
                        f"global preflight {item.get('run', '?')}: direct command calls={commands}, expected 0"
                    )
        except json.JSONDecodeError as exc:
            failures.append(f"invalid preflights.json: {exc}")

    baseline_tokens = sum(item.get("baseline", {}).get("tokens", 0) or 0 for item in report)
    active_tokens = sum(item.get("active", {}).get("tokens", 0) or 0 for item in report)
    reduction = ((baseline_tokens - active_tokens) / baseline_tokens * 100) if baseline_tokens else 0

    print(f"Evidence: {root}")
    print(f"Cases: {len(cases)} across {len(runs)} repetitions ({len(report)} records)")
    print(f"Active quality passes: {sum(bool(item.get('active', {}).get('all_match')) for item in report)}/{len(report)}")
    print(f"Outcomes: {dict(outcomes)}")
    print(f"Active/preflight costmax_run calls: {transcript_totals['active_mcp']}/{transcript_totals['preflight_mcp']}")
    print(f"Active/preflight direct command calls: {transcript_totals['active_commands']}/{transcript_totals['preflight_commands']}")
    print(f"Rehydrated active cases: {rehydrated}")
    print(f"Model-visible token estimates: {baseline_tokens} -> {active_tokens} ({reduction:.1f}% lower)")
    if failures:
        print(f"FAIL: {len(failures)} invariant(s) violated")
        for failure in failures:
            print(f"  - {failure}")
        return 1
    print("PASS: raw transcript and report invariants verified")
    return 0


if __name__ == "__main__":
    sys.exit(main())
