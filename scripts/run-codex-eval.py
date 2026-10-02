#!/usr/bin/env python3
"""CostMax evaluation runner.

Usage:
  python3 scripts/run-codex-eval.py --self-test       # parser/fixture validation (default)
  python3 scripts/run-codex-eval.py --live --binary /path/to/costmaxx  # real Codex calls
  python3 scripts/run-codex-eval.py --live --case case-001 --binary /path/to/costmaxx

Exit semantics: the run exits 0 only when every active arm and the harness
invariants pass. A baseline answer mismatch on a valid baseline transcript is
recorded as an explicit control miss/warning (report.json baseline.control_miss,
report.md Control-miss column + aggregate, console WARN) and does not fail the
run. Missing baseline transcripts, baseline subprocess/command errors, active
errors/answer mismatches, MCP bypasses, and rehydration policy violations stay
fail-closed.
"""

import argparse, glob, hashlib, json, os, re, shutil, signal, subprocess, sys, tempfile
from datetime import datetime, timezone

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CASES_DIR = os.path.join(PROJECT_ROOT, "benchmarks", "eval-cases")


# ---------------------------------------------------------------------------
# Fixture loading / validation
# ---------------------------------------------------------------------------

def load_cases():
    cases = []
    for path in sorted(glob.glob(os.path.join(CASES_DIR, "*.json"))):
        with open(path) as f:
            cases.append(json.load(f))
    return cases


def validate_fixtures(cases):
    errors = []
    for c in cases:
        required = ["id", "description", "files", "prompt", "expected_answer", "expected_command", "expected_exit_code", "reducer_target"]
        for key in required:
            if key not in c:
                errors.append(f"{c.get('id', '?')}: missing {key}")
        if not isinstance(c.get("expected_answer"), list):
            errors.append(f"{c.get('id', '?')}: expected_answer must be a list")
        if not isinstance(c.get("files"), dict) or len(c.get("files", {})) == 0:
            errors.append(f"{c.get('id', '?')}: files must be a non-empty dict")
        if not isinstance(c.get("expected_exit_code"), int):
            errors.append(f"{c.get('id', '?')}: expected_exit_code must be an integer")
        if not isinstance(c.get("reducer_target"), str) or not c.get("reducer_target"):
            errors.append(f"{c.get('id', '?')}: reducer_target must be a non-empty string")
        if "initial_files" in c and not isinstance(c["initial_files"], dict):
            errors.append(f"{c.get('id', '?')}: initial_files must be an object")
        if "deleted_files" in c and not isinstance(c["deleted_files"], list):
            errors.append(f"{c.get('id', '?')}: deleted_files must be a list")
    return errors


# ---------------------------------------------------------------------------
# Deterministic project creation
# ---------------------------------------------------------------------------

def create_project_repo(case, tmpdir, label):
    """Create a disposable git project for baseline or active."""
    repo = os.path.join(tmpdir, f"repo-{label}")
    os.makedirs(repo, exist_ok=True)
    subprocess.check_call(["git", "init", "-q"], cwd=repo)
    subprocess.check_call(["git", "config", "user.email", "eval@costmaxx"], cwd=repo)
    subprocess.check_call(["git", "config", "user.name", "eval"], cwd=repo)
    for path, content in case.get("initial_files", {}).items():
        full = os.path.join(repo, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)
    if case.get("initial_files"):
        subprocess.check_call(["git", "add", "."], cwd=repo)
        subprocess.check_call(["git", "commit", "-qm", "fixture baseline"], cwd=repo)
    for path, content in case["files"].items():
        full = os.path.join(repo, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)
    for path in case.get("deleted_files", []):
        os.remove(os.path.join(repo, path))
    # Keep newly-created paths visible to commands such as `git diff` without
    # staging their contents. Intent-to-add gives Git an index entry while
    # preserving the working-tree diff (and lets rename detection see a real
    # old->new path in fixtures).
    if case.get("initial_files"):
        for path in case["files"]:
            if path not in case.get("initial_files", {}):
                full = os.path.join(repo, path)
                if os.path.exists(full):
                    subprocess.check_call(["git", "add", "-N", "--", path], cwd=repo)
    return repo


def install_mcp_config(repo, costmax_binary, costmax_home):
    """Add .codex/config.toml with MCP server wrapper for a repo."""
    bin_dir = os.path.join(repo, ".costmax-bin")
    os.makedirs(bin_dir, exist_ok=True)
    wrapper = os.path.join(bin_dir, "costmaxx-wrapper.sh")
    with open(wrapper, "w") as f:
        f.write(f"""#!/bin/bash
export HOME="{costmax_home}"
exec "{costmax_binary}" mcp
""")
    os.chmod(wrapper, 0o755)
    codex_dir = os.path.join(repo, ".codex")
    os.makedirs(codex_dir, exist_ok=True)
    with open(os.path.join(codex_dir, "config.toml"), "w") as f:
        f.write(f"""[mcp_servers]
  [mcp_servers.costmaxx]
    command = "{wrapper}"
    type = "stdio"
""")
    return wrapper


def verify_mcp_setup(costmax_binary, wrapper):
    """Verify the costmax binary exists and the MCP wrapper is executable.
    Returns (ok, reason)."""
    if not os.path.isfile(costmax_binary):
        return False, f"binary not found: {costmax_binary}"
    if not os.access(costmax_binary, os.X_OK):
        return False, f"binary not executable: {costmax_binary}"
    if not os.path.isfile(wrapper):
        return False, f"MCP wrapper not found: {wrapper}"
    if not os.access(wrapper, os.X_OK):
        return False, f"MCP wrapper not executable: {wrapper}"
    return True, "ok"


def check_toolchain():
    """Verify external commands before any evidence directory is created.

    Returns (error, info); error is None when ready. The evaluator depends on
    the Codex CLI and git, so a missing tool aborts with a concise actionable
    message instead of a traceback and an empty results directory.
    """
    for cmd in ("codex", "git"):
        if not shutil.which(cmd):
            return f"{cmd} CLI not found on PATH; install it and retry", None
    info = {}
    try:
        info["codex_version"] = subprocess.check_output(["codex", "--version"], text=True).strip()
        info["git_version"] = subprocess.check_output(["git", "--version"], text=True).strip()
    except Exception as e:
        return f"toolchain probe failed: {e}", None
    return None, info


# ---------------------------------------------------------------------------
# Codex execution
# ---------------------------------------------------------------------------

def run_codex(repo, prompt, jsonl_path, mcp_wrapper=None, timeout=300):
    """Run codex exec --json, save transcript. Returns exit code + stderr."""
    cmd = [
        "codex", "exec", "--json", "--ephemeral", "--ignore-user-config",
        "--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust",
        "--cd", repo, prompt,
    ]
    if mcp_wrapper:
        # Explicit overrides are deterministic; project config is loaded only for trusted repos.
        cmd[2:2] = [
            "-c", f"mcp_servers.costmaxx.command={json.dumps(mcp_wrapper)}",
            "-c", "mcp_servers.costmaxx.required=true",
            "-c", 'mcp_servers.costmaxx.default_tools_approval_mode="approve"',
        ]
    # Codex may leave helper processes holding stderr open. Run it in its own
    # process group so a timeout cannot strand the evaluator in communicate().
    try:
        with open(jsonl_path, "w") as out:
            process = subprocess.Popen(
                cmd,
                env=os.environ.copy(),
                stdout=out,
                stderr=subprocess.PIPE,
                text=True,
                start_new_session=True,
            )
            try:
                stderr = process.communicate(timeout=timeout)[1]
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                stderr = process.communicate()[1]
                return -1, f"timeout after {timeout}s\n{stderr}"
    except FileNotFoundError:
        return -1, "codex CLI not found on PATH; install it and retry"
    return process.returncode, stderr


# ---------------------------------------------------------------------------
# JSONL parsing
# ---------------------------------------------------------------------------

def parse_jsonl(path):
    events = []
    if not os.path.exists(path):
        return events
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    return events


def find_command_event(events, command_substring):
    """Find a completed or failed command event for the expected command."""
    for ev in events:
        if ev.get("type") != "item.completed":
            continue
        item = ev.get("item", {})
        if item.get("type") != "command_execution" or item.get("status") not in ("completed", "failed"):
            continue
        cmd = item.get("command", "")
        if isinstance(cmd, str) and command_substring in cmd:
            return item
        # Also handle dict-shaped command (synthetic tests)
        if isinstance(cmd, dict):
            c = cmd.get("command", "")
            if command_substring in c:
                return item
    return None


def command_output(item):
    if not item:
        return ""
    agg = item.get("aggregated_output", "")
    if agg:
        return agg
    result = item.get("result") or {}
    if isinstance(result, dict):
        return result.get("aggregated_output") or result.get("output") or result.get("text") or ""
    return str(result)


def find_command_output(events, command_substring):
    return command_output(find_command_event(events, command_substring))


def find_mcp_tool_result(events, tool_name):
    """Find completed mcp_tool_call, return its text content."""
    for ev in events:
        if ev.get("type") != "item.completed":
            continue
        item = ev.get("item", {})
        if item.get("type") != "mcp_tool_call":
            continue
        if item.get("tool") != tool_name:
            continue
        result = item.get("result") or {}
        for c in (result.get("content") or []):
            if c.get("type") == "text":
                return c["text"]
    return None


def count_tool_calls(events, tool_pattern):
    """Count completed command_execution or mcp_tool_call matching pattern."""
    n = 0
    for ev in events:
        if ev.get("type") != "item.completed":
            continue
        item = ev.get("item", {})
        itype = item.get("type", "")
        if itype == "mcp_tool_call" and tool_pattern in item.get("tool", ""):
            n += 1
        elif itype == "command_execution":
            cmd = item.get("command", "")
            if isinstance(cmd, str) and tool_pattern in cmd:
                n += 1
            elif isinstance(cmd, dict) and tool_pattern in cmd.get("command", ""):
                n += 1
    return n


def count_direct_bash_for(events, expected_command):
    """Count command_execution completed events matching the expected command (direct Bash fallback)."""
    n = 0
    for ev in events:
        if ev.get("type") != "item.completed":
            continue
        item = ev.get("item", {})
        if item.get("type") != "command_execution":
            continue
        cmd = item.get("command", "")
        if isinstance(cmd, str) and expected_command in cmd:
            n += 1
        elif isinstance(cmd, dict) and expected_command in cmd.get("command", ""):
            n += 1
    return n


def count_bash_calls(events):
    # Codex JSONL records every Bash tool invocation as command_execution;
    # the shell command itself does not contain the literal word "Bash".
    return sum(1 for ev in events
               if ev.get("type") == "item.completed"
               and ev.get("item", {}).get("type") == "command_execution")


def extract_model_answer(events):
    texts = []
    for ev in events:
        if ev.get("type") != "item.completed":
            continue
        item = ev.get("item", {})
        if item.get("type") == "agent_message":
            texts.append(item.get("text", ""))
    return "\n".join(texts)


def check_answer(text, patterns):
    return [(p, bool(re.search(p, text, re.IGNORECASE))) for p in patterns]


def estimate_tokens(text):
    return len(text) // 4


def run_preflight(repo, wrapper, jsonl_path):
    """Prove the explicitly configured MCP tool is available before a paid case run."""
    prompt = ('Call costmax_run exactly once with arguments '
              '{"command":"printf costmax-preflight-ok"}. '
              'Do not use Bash. Reply only with the returned text.')
    result = {"ok": False, "costmax_calls": 0, "bash_calls": 0, "error": None}
    try:
        # A transient Codex subprocess stall must not turn an otherwise valid
        # MCP setup into a false product failure. Retry only timeouts (codex
        # exec reports them as rc=-1 with a "timeout after" stderr marker);
        # all protocol/model violations remain fail-closed.
        for attempt in range(1, 3):
            attempt_path = jsonl_path if attempt == 1 else jsonl_path + f".attempt{attempt}"
            rc, err = run_codex(repo, prompt, attempt_path, wrapper, timeout=180)
            if rc == -1 and err.startswith("timeout after"):
                result["error"] = f"preflight timed out after 180s (attempt {attempt})"
                if attempt == 1:
                    continue
                return result
            events = parse_jsonl(attempt_path)
            text = find_mcp_tool_result(events, "costmax_run") or ""
            result["costmax_calls"] = count_tool_calls(events, "costmax_run")
            result["bash_calls"] = count_bash_calls(events)
            if rc != 0:
                result["error"] = f"codex exit={rc}: {err.strip()}"
            elif result["costmax_calls"] != 1:
                result["error"] = f"costmax_run calls={result['costmax_calls']} (expected 1)"
            elif result["bash_calls"] != 0:
                result["error"] = f"Bash calls={result['bash_calls']} (expected 0)"
            elif "costmax-preflight-ok" not in text:
                result["error"] = "costmax_run result missing preflight marker"
            else:
                if attempt != 1:
                    shutil.copyfile(attempt_path, jsonl_path)
                result["ok"] = True
                result["error"] = None
                return result
            return result
    except Exception as e:
        result["error"] = str(e)
    return result


# ---------------------------------------------------------------------------
# Evaluation runner
# ---------------------------------------------------------------------------

def classify_case_report(report):
    """Pure classifier over one paired-case report (no live Codex needed).

    Sets ``baseline.control_miss`` and the final ``outcome``. Fail-closed
    conditions stay hard failures: missing baseline/active output, baseline
    subprocess/command errors, active errors, active answer mismatches, and
    MCP bypasses (missing/duplicate ``costmax_run`` or a direct Bash call).
    A baseline answer mismatch on a valid baseline transcript is a control
    miss/warning: it is recorded on the report and does not, by itself, fail
    the run.
    """
    fail = bool(report["baseline"]["error"]) or bool(report["active"]["error"])
    if not report["baseline"]["output_text"]:
        report["baseline"]["error"] = (report["baseline"]["error"] or "") + " [FAIL] no baseline output"
        fail = True
    if not report["active"]["output_text"]:
        report["active"]["error"] = (report["active"]["error"] or "") + " [FAIL] no active output"
        fail = True
    if report["active"]["costmax_calls"] != 1:
        report["active"]["error"] = (report["active"]["error"] or "") + " [FAIL] expected exactly one costmax_run"
        fail = True
    if report["active"]["bash_calls"] > 0:
        report["active"]["error"] = (report["active"]["error"] or "") + " [FAIL] active used Bash"
        fail = True
    report["baseline"]["control_miss"] = False
    if not report["baseline"]["all_match"]:
        if report["baseline"]["error"]:
            fail = True
        else:
            report["baseline"]["control_miss"] = True
    if not report["active"]["all_match"]:
        report["active"]["error"] = (report["active"]["error"] or "") + " [FAIL] answer mismatch"
        fail = True

    if fail:
        report["saving"] = "error"
        report["outcome"] = "quality_failure" if not report["active"]["all_match"] else "harness_failure"
    elif report["saving"] == "yes":
        report["outcome"] = "quality_and_saving"
    elif report["saving"] == "partial":
        report["outcome"] = "quality_with_rehydration"
    else:
        report["outcome"] = "quality_no_saving"
    return report


def evaluate_case(case, results_dir, costmax_binary, live=False):
    case_dir = os.path.join(results_dir, case["id"])
    os.makedirs(case_dir, exist_ok=True)
    report = {
        "case_id": case["id"],
        "description": case["description"],
        "expected_exit_code": case["expected_exit_code"],
        "transcripts": {},
        "preflight": None,
        "baseline": {"output_text": "", "chars": 0, "tokens": 0, "answer": "", "answer_matches": [], "all_match": False,
                     "control_miss": False, "error": None},
        "active": {"output_text": "", "chars": 0, "tokens": 0, "answer": "", "answer_matches": [], "all_match": False,
                    "costmax_calls": 0, "resource_calls": 0, "bash_calls": 0, "rehydrated": False, "error": None},
        "saving": "no",
        "outcome": "harness_failure",
    }

    cmd = case["expected_command"]

    # Baseline
    full_prompt = f"{case['prompt']}\n\nIMPORTANT: Use Bash exactly once to run '{cmd}'. Do NOT use costmax_run or any other tool."
    repo_b = create_project_repo(case, case_dir, "base")
    jsonl_b = os.path.join(case_dir, "baseline.jsonl")
    report["transcripts"]["baseline"] = jsonl_b
    try:
        rc, err = run_codex(repo_b, full_prompt, jsonl_b)
        evts = parse_jsonl(jsonl_b)
        command_event = find_command_event(evts, cmd)
        out = command_output(command_event)
        ans = extract_model_answer(evts)
        report["baseline"]["output_text"] = out
        report["baseline"]["chars"] = len(out)
        report["baseline"]["tokens"] = estimate_tokens(out)
        report["baseline"]["answer"] = ans
        report["baseline"]["answer_matches"] = check_answer(ans, case["expected_answer"])
        report["baseline"]["all_match"] = all(m for _, m in report["baseline"]["answer_matches"])
        if not out:
            report["baseline"]["error"] = "expected_command output not found in transcript"
        elif command_event.get("exit_code") != case["expected_exit_code"]:
            report["baseline"]["error"] = f"command exit={command_event.get('exit_code')} (expected {case['expected_exit_code']})"
        if rc != 0:
            e = err or ""
            report["baseline"]["error"] = (report["baseline"]["error"] or "") + f" codex exit={rc}"
    except Exception as e:
        report["baseline"]["error"] = str(e)

    # Active
    repo_a = create_project_repo(case, case_dir, "active")
    costmax_home = os.path.join(case_dir, "costmax-home")
    os.makedirs(costmax_home, exist_ok=True)
    wrapper = install_mcp_config(repo_a, costmax_binary, costmax_home)
    ok, reason = verify_mcp_setup(costmax_binary, wrapper)
    if not ok:
        report["active"]["error"] = f"MCP setup failed: {reason}"
        report["saving"] = "error"
        return report
    jsonl_preflight = os.path.join(case_dir, "preflight.jsonl")
    report["transcripts"]["preflight"] = jsonl_preflight
    report["preflight"] = run_preflight(repo_a, wrapper, jsonl_preflight)
    if not report["preflight"]["ok"]:
        report["active"]["error"] = "MCP preflight failed: " + report["preflight"]["error"]
        return report

    jsonl_a = os.path.join(case_dir, "active.jsonl")
    report["transcripts"]["active"] = jsonl_a
    full_prompt_a = (
        f"{case['prompt']}\n\n"
        f"You MUST use the MCP tool `costmax_run` with the JSON argument `{{\"command\":{json.dumps(cmd)}}}`. "
        f"Call costmax_run exactly once. "
        f"Do NOT use the Bash tool. Do NOT call resources/read unless necessary. "
        f"Answer based on the compact output returned by costmax_run."
    )
    try:
        rc, err = run_codex(repo_a, full_prompt_a, jsonl_a, wrapper)
        evts = parse_jsonl(jsonl_a)
        out = find_mcp_tool_result(evts, "costmax_run") or ""
        ans = extract_model_answer(evts)
        report["active"]["output_text"] = out
        report["active"]["chars"] = len(out)
        report["active"]["tokens"] = estimate_tokens(out)
        report["active"]["answer"] = ans
        report["active"]["answer_matches"] = check_answer(ans, case["expected_answer"])
        report["active"]["all_match"] = all(m for _, m in report["active"]["answer_matches"])
        report["active"]["costmax_calls"] = count_tool_calls(evts, "costmax_run")
        report["active"]["resource_calls"] = count_tool_calls(evts, "read_mcp_resource")
        report["active"]["bash_calls"] = count_bash_calls(evts)
        report["active"]["rehydrated"] = report["active"]["resource_calls"] > 0
        if not out:
            report["active"]["error"] = "costmax_run result not found in transcript"
        if report["active"]["costmax_calls"] != 1:
            report["active"]["error"] = (report["active"]["error"] or "") + f" costmax_run calls={report['active']['costmax_calls']} (expected 1)"
        if report["active"]["bash_calls"] > 0:
            e = f"model used Bash despite instruction ({report['active']['bash_calls']} call(s))"
            report["active"]["error"] = (report["active"]["error"] or "") + " " + e
        if rc != 0:
            e = err or ""
            report["active"]["error"] = (report["active"]["error"] or "") + f" codex exit={rc}"
    except Exception as e:
        report["active"]["error"] = str(e)

    # Saving determination
    bt = report["baseline"]["tokens"]
    ct = report["active"]["tokens"]
    if report["active"].get("rehydrated"):
        report["saving"] = "partial"
    elif bt > 0 and ct > 0 and ct < bt:
        report["saving"] = "yes"
    elif bt > 0 and ct > 0:
        report["saving"] = "none"

    # Fail closed on hard failures; a baseline answer miss on a valid baseline
    # transcript is a control miss/warning, not a run failure.
    classify_case_report(report)
    return report


# ---------------------------------------------------------------------------
# Organic adoption (separate experiment from the forced fixture evaluation)
# ---------------------------------------------------------------------------

def classify_adoption(evts, expected_answer):
    """Classify a transcript into adoption outcomes. Pure function over parsed
    events so the self-test can exercise it without a live Codex run."""
    routed = count_tool_calls(evts, "costmax_run")
    direct = count_bash_calls(evts)
    resource_reads = count_tool_calls(evts, "read_mcp_resource")
    result = find_mcp_tool_result(evts, "costmax_run") or ""
    ans = extract_model_answer(evts)
    return {
        "routed": routed > 0,
        "bypassed": routed == 0 and direct > 0,
        "direct_command_calls": direct,
        "rehydrated": resource_reads > 0,
        "reduction_applied": (
            "Recommendation: reduce" in result or "Recommendation: artifact_required" in result
        ),
        "quality_pass": all(m for _, m in check_answer(ans, expected_answer)),
        "used_nothing": routed == 0 and direct == 0,
    }


def evaluate_adoption(case, case_dir, costmax_binary):
    """Run one fixture WITHOUT forcing a tool choice.

    The model may freely pick `costmax_run` or a direct command; the record
    captures what it naturally did. This is the organic-adoption arm, kept
    separate from the bounded 20x3 reduction/quality test.
    """
    cmd = case["expected_command"]
    repo = create_project_repo(case, case_dir, "adopt")
    costmax_home = os.path.join(case_dir, "costmax-home")
    os.makedirs(costmax_home, exist_ok=True)
    wrapper = install_mcp_config(repo, costmax_binary, costmax_home)
    ok, reason = verify_mcp_setup(costmax_binary, wrapper)
    record = {
        "case_id": case["id"],
        "eligible": True,
        "routed": False,          # model chose costmax_run at least once
        "bypassed": False,        # model chose a direct command instead
        "direct_command_calls": 0,
        "reduction_applied": False,
        "rehydrated": False,
        "quality_pass": False,
        "error": None,
        "transcript": os.path.join(case_dir, "adoption.jsonl"),
    }
    if not ok:
        record["error"] = f"MCP setup failed: {reason}"
        return record

    # Neutral prompt: no mention of costmax_run, no forced routing.
    prompt = f"{case['prompt']}\n\nRun the command you need and answer the question."
    try:
        rc, err = run_codex(repo, prompt, record["transcript"], wrapper)
    except Exception as e:
        record["error"] = str(e)
        return record

    evts = parse_jsonl(record["transcript"])
    c = classify_adoption(evts, case["expected_answer"])
    record["routed"] = c["routed"]
    record["bypassed"] = c["bypassed"]
    record["direct_command_calls"] = c["direct_command_calls"]
    record["rehydrated"] = c["rehydrated"]
    record["reduction_applied"] = c["reduction_applied"]
    record["quality_pass"] = c["quality_pass"]
    if rc != 0:
        record["error"] = (record["error"] or "") + f" codex exit={rc}"
    if c["used_nothing"]:
        record["error"] = (record["error"] or "") + " model used neither costmax_run nor a direct command"
    return record


def write_adoption_report(records):
    """Render the adoption summary: eligible / routed / bypassed / applied /
    rehydrated / quality / failures. No billed-dollar or intelligence claim."""
    n = len(records)
    routed = sum(1 for r in records if r["routed"])
    bypassed = sum(1 for r in records if r["bypassed"])
    applied = sum(1 for r in records if r["reduction_applied"])
    rehydrated = sum(1 for r in records if r["rehydrated"])
    quality = sum(1 for r in records if r["quality_pass"])
    failures = sum(1 for r in records if r["error"])
    lines = [
        "# CostMax Organic-Adoption Report",
        "",
        "Separate experiment: models were NOT told to use costmax_run.",
        "",
        f"- Eligible calls:       {n}",
        f"- CostMax-routed calls: {routed}",
        f"- Direct/bypassed:      {bypassed}",
        f"- Reduction applied:    {applied}",
        f"- Rehydrated calls:     {rehydrated}",
        f"- Quality passes:       {quality}/{n}",
        f"- Failures:             {failures}",
        "",
        "| Case | Routed | Bypassed | Reduction applied | Rehydrated | Quality | Error |",
        "|------|--------|----------|-------------------|------------|---------|-------|",
    ]
    for r in records:
        q = "✓" if r["quality_pass"] else "✗"
        lines.append(
            f"| {r['case_id']} | {r['routed']} | {r['bypassed']} | {r['reduction_applied']} | "
            f"{r['rehydrated']} | {q} | {r['error'] or ''} |"
        )
    return "\n".join(lines), {
        "eligible": n, "routed": routed, "bypassed": bypassed,
        "reduction_applied": applied, "rehydrated": rehydrated,
        "quality_passes": quality, "failures": failures,
    }


# ---------------------------------------------------------------------------
# Reports
# ---------------------------------------------------------------------------

def generate_markdown(reports):
    lines = [
        "# CostMax Evaluation Report",
        "",
        "| Case | Outcome | Baseline tokens | Active tokens | Savings | Baseline pass | Active pass | Rehydrated | Control miss |",
        "|------|---------|----------------|---------------|---------|--------------|-------------|------------|--------------|",
    ]
    for r in reports:
        bp = "✓" if r["baseline"]["all_match"] and not r["baseline"]["error"] else "✗"
        ap = "✓" if r["active"]["all_match"] and not r["active"]["error"] else "✗"
        rh = "✓" if r["active"]["rehydrated"] else ""
        cm = "✓" if r["baseline"].get("control_miss") else ""
        lines.append(f"| {r['case_id']} | {r['outcome']} | {r['baseline']['tokens']} | {r['active']['tokens']} | {r['saving']} | {bp} | {ap} | {rh} | {cm} |")

    # Aggregate section — model-visible tool-output estimates, not billed cost or intelligence
    n = len(reports)
    baseline_pass = sum(1 for r in reports if r["baseline"]["all_match"] and not r["baseline"]["error"])
    active_pass = sum(1 for r in reports if r["active"]["all_match"] and not r["active"]["error"])
    saving_yes = sum(1 for r in reports if r["saving"] == "yes")
    rehydrated = sum(1 for r in reports if r["active"]["rehydrated"])
    no_saving = sum(1 for r in reports if r["outcome"] == "quality_no_saving")
    harness_failures = sum(1 for r in reports if r["outcome"] == "harness_failure")
    quality_failures = sum(1 for r in reports if r["outcome"] == "quality_failure")
    control_misses = sum(1 for r in reports if r["baseline"].get("control_miss"))
    total_base_tokens = sum(r["baseline"]["tokens"] for r in reports)
    total_active_tokens = sum(r["active"]["tokens"] for r in reports)
    delta = total_active_tokens - total_base_tokens

    lines.append("")
    lines.append("## Aggregate (model-visible tool-output estimates only)")
    lines.append("")
    lines.append(f"| Metric | Value |")
    lines.append(f"|--------|-------|")
    lines.append(f"| Total cases | {n} |")
    lines.append(f"| Baseline quality passes | {baseline_pass}/{n} |")
    lines.append(f"| Baseline control misses | {control_misses} |")
    lines.append(f"| Active quality passes | {active_pass}/{n} |")
    lines.append(f"| Non-rehydrated savings cases | {saving_yes} |")
    lines.append(f"| Correct but no-saving cases | {no_saving} |")
    lines.append(f"| Rehydrated cases | {rehydrated} |")
    lines.append(f"| Harness failures | {harness_failures} |")
    lines.append(f"| Quality failures | {quality_failures} |")
    lines.append(f"| Total baseline tool-output token estimate | {total_base_tokens} |")
    lines.append(f"| Total active tool-output token estimate | {total_active_tokens} |")
    lines.append(f"| Overall token change | {delta:+d} |")

    missed = [r for r in reports if r["baseline"].get("control_miss")]
    if missed:
        lines.append("")
        lines.append("## Baseline control misses")
        lines.append("")
        lines.append("A control miss means the baseline (no-CostMax) transcript is valid and the")
        lines.append("command ran correctly, but the model's answer did not match every expected")
        lines.append("pattern. It is reported here honestly and does not fail the run: the active")
        lines.append("arm and harness invariants determine the exit code.")
        lines.append("")
        for r in missed:
            lines.append(f"- {r['case_id']} run {r.get('run', '?')}: matches={r['baseline']['answer_matches']}")

    for r in reports:
        errs = []
        if r["baseline"]["error"]:
            errs.append(f"- Baseline: {r['baseline']['error']}")
        if r["active"]["error"]:
            errs.append(f"- Active: {r['active']['error']}")
        if errs:
            lines.append(f"\n## {r['case_id']} errors\n" + "\n".join(errs))
    return "\n".join(lines)


def evaluate_exit(reports):
    """Compute the run exit semantics. Pure over report dicts.

    Returns ``(exit_code, failures, control_misses)`` where ``failures`` is a
    list of printable FAIL lines and ``control_misses`` a list of
    ``(case_id, run)`` tuples.

    Fail-closed: any active error or active answer mismatch, any baseline
    subprocess/command/transcript error, an MCP bypass (missing/duplicate
    ``costmax_run`` or a direct Bash call) fails the run. A baseline answer
    mismatch on a valid baseline transcript is a control miss: recorded and
    reported as a warning, and the run still exits 0 when the active arm and
    harness invariants pass.
    """
    failures = []
    control_misses = []
    for r in reports:
        if r["baseline"]["error"]:
            failures.append(f"baseline {r['case_id']} run {r.get('run', '?')}: {r['baseline']['error']}")
        if r["active"]["error"]:
            failures.append(f"active {r['case_id']} run {r.get('run', '?')}: {r['active']['error']}")
        if r["baseline"].get("control_miss"):
            control_misses.append((r["case_id"], r.get("run", "?")))
    return (0 if not failures else 1), failures, control_misses


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

SELF_TEST_JSONL = """\
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc 'cat data.txt'","aggregated_output":"line 1\\nline 2\\n...\\nline 100","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc 'cat data.txt'","aggregated_output":"line 1\\nline 100\\nTotal: 100","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_3","type":"mcp_tool_call","tool":"costmax_run","result":{"content":[{"type":"text","text":"compact output"}]}}}
{"type":"item.completed","item":{"id":"item_4","type":"mcp_tool_call","tool":"read_mcp_resource","result":{"content":[{"type":"text","text":"full content"}]}}}
{"type":"item.completed","item":{"id":"item_5","type":"agent_message","text":"Last line: line 100, Total: 100"}}
"""


def run_self_test():
    errors = []
    cases = load_cases()
    errors.extend(validate_fixtures(cases))

    # Write synthetic JSONL to a temp file for parser testing
    import tempfile as tf
    tmp = tf.NamedTemporaryFile(mode="w", suffix=".jsonl", delete=False)
    tmp.write(SELF_TEST_JSONL)
    tmp.close()
    evts = parse_jsonl(tmp.name)
    os.unlink(tmp.name)
    # command_execution parsing
    out = find_command_output(evts, "cat data.txt")
    if not out:
        errors.append("find_command_output returned None")
    elif "line 100" not in out:
        errors.append("find_command_output content mismatch")
    # mcp_tool_call parsing
    mcp = find_mcp_tool_result(evts, "costmax_run")
    if not mcp:
        errors.append("find_mcp_tool_result returned None")
    # resource read detection
    rc = count_tool_calls(evts, "read_mcp_resource")
    if rc != 1:
        errors.append(f"count_tool_calls resource expected 1 got {rc}")
    # bash detection via expected command (0 in synthetic data)
    bc = count_direct_bash_for(evts, "cat data.txt")
    if bc != 2:
        errors.append(f"count_direct_bash_for 'cat data.txt' expected 2 got {bc}")
    # command_execution count matching expected command
    cc = count_tool_calls(evts, "cat data.txt")
    if cc != 2:
        errors.append(f"count_tool_calls 'cat data.txt' expected 2 got {cc}")

    # costmax_run count (single call is correct)
    cm = count_tool_calls(evts, "costmax_run")
    if cm != 1:
        errors.append(f"count_tool_calls costmax_run expected 1 got {cm}")
    # answer matching
    ans = extract_model_answer(evts)
    if "100" not in ans:
        errors.append("extract_model_answer failed")
    matches = check_answer(ans, ["100", "line 100"])
    if not all(m for _, m in matches):
        errors.append("check_answer failed")

    # Test that duplicate costmax_run calls are detected
    dup_events = parse_jsonl(write_tmp_jsonl("""\
{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"costmax_run","status":"completed","result":{"content":[{"type":"text","text":"first"}]}}}
{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"costmax_run","status":"completed","result":{"content":[{"type":"text","text":"second"}]}}}
"""))
    cm2 = count_tool_calls(dup_events, "costmax_run")
    if cm2 != 2:
        errors.append(f"count duplicate costmax_run expected 2 got {cm2}")
    # This scenario should be a failure in the eval (costmax_calls != 1)
    if cm2 == 1:
        errors.append("duplicate costmax_run test: should have detected 2 calls but got 1")

    failed_command = parse_jsonl(write_tmp_jsonl("""\
{"type":"item.completed","item":{"type":"command_execution","command":"git diff --no-index before after","aggregated_output":"diff output","exit_code":1,"status":"failed"}}
"""))
    item = find_command_event(failed_command, "git diff --no-index")
    if not item or item.get("exit_code") != 1 or command_output(item) != "diff output":
        errors.append("nonzero command event parsing failed")

    unavailable = parse_jsonl(write_tmp_jsonl("""\
{"type":"item.completed","item":{"type":"agent_message","text":"costmax_run is not available"}}
"""))
    if find_mcp_tool_result(unavailable, "costmax_run") is not None or count_tool_calls(unavailable, "costmax_run") != 0:
        errors.append("unavailable MCP tool parsing failed")

    bash_events = parse_jsonl(write_tmp_jsonl("""\
{"type":"item.completed","item":{"type":"command_execution","command":"cat data.txt","aggregated_output":"raw","status":"completed"}}
"""))
    if count_bash_calls(bash_events) != 1:
        errors.append("Bash fallback parsing failed")

    # Adoption classification: routed through costmax_run with a reduce
    # recommendation, quality answer present.
    routed_adoption = parse_jsonl(write_tmp_jsonl("""\
{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"costmax_run","server":"costmaxx","status":"completed","result":{"content":[{"type":"text","text":"[costmax_run] Recommendation: reduce\\nModel-visible tokens: 10\\nArtifact ID: a1\\n---\\ncompact"}]}}}
{"type":"item.completed","item":{"type":"agent_message","text":"The answer is 100"}}
"""))
    c = classify_adoption(routed_adoption, ["100"])
    if not c["routed"] or c["bypassed"] or not c["reduction_applied"] or not c["quality_pass"]:
        errors.append(f"adoption routed classification wrong: {c}")
    # Adoption classification: bypassed via a direct command.
    bypass_adoption = parse_jsonl(write_tmp_jsonl("""\
{"type":"item.completed","item":{"type":"command_execution","command":"cat data.txt","aggregated_output":"line 1\\nline 100","status":"completed"}}
{"type":"item.completed","item":{"type":"agent_message","text":"The answer is 100"}}
"""))
    c2 = classify_adoption(bypass_adoption, ["100"])
    if c2["routed"] or not c2["bypassed"] or c2["reduction_applied"] or not c2["quality_pass"]:
        errors.append(f"adoption bypass classification wrong: {c2}")
    # Adoption summary aggregation is exact.
    md, agg = write_adoption_report([
        {"case_id": "a", "routed": True, "bypassed": False, "reduction_applied": True,
         "rehydrated": False, "quality_pass": True, "error": None},
        {"case_id": "b", "routed": False, "bypassed": True, "reduction_applied": False,
         "rehydrated": False, "quality_pass": True, "error": None},
        {"case_id": "c", "routed": False, "bypassed": False, "reduction_applied": False,
         "rehydrated": True, "quality_pass": False, "error": "harness"},
    ])
    if agg != {"eligible": 3, "routed": 1, "bypassed": 1, "reduction_applied": 1,
               "rehydrated": 1, "quality_passes": 2, "failures": 1}:
        errors.append(f"adoption summary aggregation wrong: {agg}")
    if "Organic-Adoption" not in md:
        errors.append("adoption markdown header missing")

    # --- Baseline control-miss classification / exit semantics --------------
    # A baseline answer mismatch on a valid baseline transcript is a control
    # miss/warning, not a run failure; everything else stays fail-closed.
    from copy import deepcopy

    def make_report(**over):
        r = {
            "case_id": "case-x", "run": 1, "outcome": "pending", "saving": "yes",
            "baseline": {"output_text": "out", "chars": 4, "tokens": 1, "answer": "nine",
                         "answer_matches": [("10", False)], "all_match": False,
                         "control_miss": False, "error": None},
            "active": {"output_text": "out", "chars": 2, "tokens": 0, "answer": "ten",
                       "answer_matches": [("10", True)], "all_match": True,
                       "costmax_calls": 1, "resource_calls": 0, "bash_calls": 0,
                       "rehydrated": False, "error": None},
        }
        r.update(over)
        return r

    # Control miss: baseline transcript valid, answer mismatch. The run exits
    # success and records the miss explicitly.
    cm_report = make_report()
    classify_case_report(cm_report)
    if not cm_report["baseline"]["control_miss"] or cm_report["baseline"]["error"]:
        errors.append(f"control miss must be flagged and not a baseline error: {cm_report['baseline']}")
    if cm_report["outcome"] != "quality_and_saving":
        errors.append(f"control-miss outcome wrong: {cm_report['outcome']}")
    ec, failures, misses = evaluate_exit([cm_report])
    if ec != 0 or failures or misses != [("case-x", 1)]:
        errors.append(f"control miss must not fail the run: exit={ec} failures={failures} misses={misses}")

    # Baseline subprocess/command error stays fail-closed.
    sub_err = make_report()
    sub_err["baseline"]["error"] = "command exit=2 (expected 0)"
    classify_case_report(sub_err)
    if not sub_err["baseline"]["error"] or sub_err["baseline"]["control_miss"]:
        errors.append("baseline command error must remain a hard failure, not a control miss")
    ec, failures, _ = evaluate_exit([sub_err])
    if ec != 1 or not failures or "baseline case-x" not in failures[0]:
        errors.append(f"baseline command error must fail the run: exit={ec} failures={failures}")

    # Missing baseline transcript/output stays fail-closed.
    missing_base = make_report()
    missing_base["baseline"]["output_text"] = ""
    missing_base["baseline"]["error"] = "expected_command output not found in transcript"
    classify_case_report(missing_base)
    ec, failures, _ = evaluate_exit([missing_base])
    if ec != 1 or not failures:
        errors.append(f"missing baseline output must fail the run: exit={ec} failures={failures}")

    # Active answer mismatch stays fail-closed (quality_failure).
    active_miss = make_report()
    active_miss["active"]["all_match"] = False
    classify_case_report(active_miss)
    if not active_miss["active"]["error"] or active_miss["outcome"] != "quality_failure":
        errors.append(f"active answer mismatch must be a quality failure: {active_miss}")
    ec, failures, _ = evaluate_exit([active_miss])
    if ec != 1 or not failures:
        errors.append(f"active answer mismatch must fail the run: exit={ec} failures={failures}")

    # MCP bypass (active used Bash) stays fail-closed (harness_failure).
    bypass = make_report()
    bypass["active"]["bash_calls"] = 1
    classify_case_report(bypass)
    if not bypass["active"]["error"] or bypass["outcome"] != "harness_failure":
        errors.append(f"MCP bypass must be a harness failure: {bypass}")
    ec, failures, _ = evaluate_exit([bypass])
    if ec != 1 or not failures:
        errors.append(f"MCP bypass must fail the run: exit={ec} failures={failures}")

    # Markdown must surface the control miss and exclude it from baseline passes.
    md2 = generate_markdown([cm_report])
    if "| Baseline control misses | 1 |" not in md2:
        errors.append("markdown aggregate must report the baseline control miss")
    if "| Baseline quality passes | 0/1 |" not in md2:
        errors.append("markdown must exclude the control miss from baseline passes")
    if "## Baseline control misses" not in md2:
        errors.append("markdown must include a control-miss detail section")
    if "| case-x | quality_and_saving |" not in md2 or "| ✓ |" not in md2.split("| case-x |")[1]:
        errors.append("markdown case table must show the outcome and control-miss column")

    # --- Fail-closed startup paths -----------------------------------------
    # A missing codex CLI must abort with a concise actionable error and no
    # traceback, and it must not leave a partially-created evidence directory.
    saved_path = os.environ.get("PATH", "")
    try:
        os.environ["PATH"] = ""
        err, info = check_toolchain()
        if not err or "codex" not in err or info is not None:
            errors.append(f"check_toolchain must flag missing codex: err={err!r} info={info!r}")
        rc, err2 = run_codex("/tmp/costmax-missing-codex", "hello", write_tmp_jsonl(""))
        if rc != -1 or "codex" not in err2 or "PATH" not in err2:
            errors.append(f"run_codex missing codex must fail closed: rc={rc} err={err2!r}")
    finally:
        os.environ["PATH"] = saved_path

    # Preflight retries a transient timeout exactly once, then succeeds; a
    # non-timeout failure is NOT retried.
    real_run_codex = run_codex
    preflight_ok_jsonl = (
        '{"type":"item.completed","item":{"id":"i1","type":"mcp_tool_call","tool":"costmax_run",'
        '"server":"costmaxx","status":"completed","result":{"content":[{"type":"text",'
        '"text":"[costmax_run] costmax-preflight-ok"}]}}}\n'
    )
    try:
        timeout_calls = []

        def timeout_then_ok(repo, prompt, jsonl_path, mcp_wrapper=None, timeout=300):
            timeout_calls.append(1)
            if len(timeout_calls) == 1:
                return -1, "timeout after 180s\nstalled"
            with open(jsonl_path, "w") as f:
                f.write(preflight_ok_jsonl)
            return 0, ""

        globals()["run_codex"] = timeout_then_ok
        res = run_preflight("/tmp/preflight-repo", "/tmp/preflight-wrapper", write_tmp_jsonl(""))
        if not res["ok"] or len(timeout_calls) != 2 or res["error"] is not None:
            errors.append(f"preflight timeout retry must succeed on attempt 2: calls={timeout_calls} res={res}")

        fail_calls = []

        def always_fail(repo, prompt, jsonl_path, mcp_wrapper=None, timeout=300):
            fail_calls.append(1)
            return 1, "boom"

        globals()["run_codex"] = always_fail
        res2 = run_preflight("/tmp/preflight-repo", "/tmp/preflight-wrapper", write_tmp_jsonl(""))
        if len(fail_calls) != 1 or res2["ok"] or "codex exit=1" not in (res2["error"] or ""):
            errors.append(f"preflight must not retry non-timeout failures: calls={fail_calls} res={res2}")
    finally:
        globals()["run_codex"] = real_run_codex

    # End-to-end: --adoption with a missing codex CLI must exit 1 with an
    # actionable error and MUST NOT create the requested results directory.
    with tempfile.TemporaryDirectory(prefix="costmax-eval-cli-") as tmp:
        fake_binary = os.path.join(tmp, "costmaxx")
        with open(fake_binary, "w") as f:
            f.write("#!/bin/sh\n")
        os.chmod(fake_binary, 0o755)
        ev_dir = os.path.join(tmp, "evidence")
        proc = subprocess.run(
            [sys.executable, os.path.abspath(__file__), "--adoption", "--binary", fake_binary,
             "--results-dir", ev_dir],
            capture_output=True, text=True,
            env={**os.environ, "PATH": ""},
        )
        if proc.returncode != 1:
            errors.append(f"missing-codex --adoption must exit 1, got {proc.returncode}: {proc.stdout}{proc.stderr}")
        if "codex CLI not found" not in (proc.stdout + proc.stderr):
            errors.append(f"missing-codex --adoption must print actionable error, got: {proc.stdout!r} {proc.stderr!r}")
        if os.path.exists(ev_dir):
            errors.append("missing-codex --adoption must not create a partial evidence directory")

    if errors:
        for e in errors:
            print(f"  FAIL: {e}")
        return False
    print(f"  {len(cases)} fixtures validated, all parsers OK")
    return True


def write_tmp_jsonl(content):
    import tempfile as tf
    tmp = tf.NamedTemporaryFile(mode="w", suffix=".jsonl", delete=False)
    tmp.write(content)
    tmp.close()
    return tmp.name


def run_fixture_smoke():
    """Execute every fixture command locally and verify its declared exit code."""
    failures = []
    with tempfile.TemporaryDirectory(prefix="costmax-fixture-smoke-") as tmpdir:
        for case in load_cases():
            repo = create_project_repo(case, tmpdir, case["id"])
            result = subprocess.run(["sh", "-c", case["expected_command"]], cwd=repo,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            if result.returncode != case["expected_exit_code"]:
                failures.append(f"{case['id']}: exit={result.returncode}, expected={case['expected_exit_code']}")
    if failures:
        for failure in failures:
            print("  FAIL: " + failure)
        return False
    print(f"  {len(load_cases())} fixture commands matched their declared exit status")
    return True


def main():
    ap = argparse.ArgumentParser(description="CostMax evaluation runner")
    ap.add_argument("--self-test", action="store_true", help="parser/fixture validation only")
    ap.add_argument("--fixture-smoke", action="store_true", help="run fixture commands locally; no Codex API calls")
    ap.add_argument("--live", action="store_true", help="run real Codex calls (spends API usage)")
    ap.add_argument("--case", help="single case ID")
    ap.add_argument("--binary", help="path to costmaxx binary (required for --live)")
    ap.add_argument("--repetitions", type=int, default=1, help="paired runs per case (default: 1)")
    ap.add_argument("--preflight-runs", type=int, default=0, help="run MCP-only availability checks before evaluations")
    ap.add_argument("--preflight-only", action="store_true", help="run only MCP availability checks")
    ap.add_argument("--results-dir", help="directory for immutable reports/transcripts")
    ap.add_argument("--adoption", action="store_true",
                    help="run the organic-adoption arm (model chooses freely, no forced costmax_run)")
    ap.add_argument("--yes", action="store_true", help="acknowledge live API usage without a prompt")
    args = ap.parse_args()

    is_self = args.self_test or args.fixture_smoke or not (args.live or args.adoption)

    if is_self:
        ok = run_self_test()
        if args.fixture_smoke:
            ok = run_fixture_smoke() and ok
        sys.exit(0 if ok else 1)

    # Live mode
    if not args.binary:
        print("ERROR: --binary <path> is required with --live/--adoption")
        sys.exit(1)
    if not os.path.isfile(args.binary):
        print(f"ERROR: binary not found: {args.binary}")
        sys.exit(1)
    if args.repetitions < 1 or args.preflight_runs < 0:
        ap.error("--repetitions must be >= 1 and --preflight-runs must be >= 0")

    # Fail closed before creating any evidence: a missing Codex CLI or git must
    # abort with a concise actionable error and no partial results directory.
    toolchain_error, toolchain = check_toolchain()
    if toolchain_error:
        print(f"ERROR: {toolchain_error}")
        sys.exit(1)
    codex_version = toolchain["codex_version"]

    cases = load_cases()
    if args.case:
        cases = [c for c in cases if c["id"] == args.case]
        if not cases:
            print(f"Case '{args.case}' not found")
            sys.exit(1)

    # Organic-adoption arm: separate experiment. The model chooses freely;
    # nothing forces costmax_run. Results are written to adoption.json.
    if args.adoption:
        stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        results_dir = os.path.abspath(args.results_dir or os.path.join(PROJECT_ROOT, "results", f"adoption-{stamp}"))
        try:
            os.makedirs(results_dir, exist_ok=False)
        except FileExistsError:
            print(f"ERROR: evidence directory already exists: {results_dir}")
            sys.exit(1)
        with open(args.binary, "rb") as f:
            binary_sha256 = hashlib.sha256(f.read()).hexdigest()
        manifest = {"created_at": stamp, "mode": "adoption",
                    "costmax_binary_sha256": binary_sha256, "codex_version": codex_version}
        with open(os.path.join(results_dir, "manifest.json"), "w") as f:
            json.dump(manifest, f, indent=2)

        records = []
        for case in cases:
            print(f"  Adoption {case['id']}...")
            r = evaluate_adoption(case, os.path.join(results_dir, f"adopt-{case['id']}"), args.binary)
            records.append(r)
        with open(os.path.join(results_dir, "adoption.json"), "w") as f:
            json.dump(records, f, indent=2)
        md, agg = write_adoption_report(records)
        with open(os.path.join(results_dir, "adoption.md"), "w") as f:
            f.write(md + "\n")
        print("\n".join(md.splitlines()[:10]))
        print(f"\nAdoption evidence: {results_dir}")
        print(f"Adoption JSON: {os.path.join(results_dir, 'adoption.json')}")
        return 0

    if not args.yes:
        print("WARNING: Live mode incurs API usage costs.")
        confirm = input("Type 'yes' to continue: ")
        if confirm != "yes":
            print("Aborted.")
            sys.exit(1)

    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    results_dir = os.path.abspath(args.results_dir or os.path.join(PROJECT_ROOT, "results", stamp))
    try:
        os.makedirs(results_dir, exist_ok=False)
    except FileExistsError:
        print(f"ERROR: evidence directory already exists: {results_dir}")
        sys.exit(1)
    with open(args.binary, "rb") as f:
        binary_sha256 = hashlib.sha256(f.read()).hexdigest()
    fixture_hashes = {os.path.basename(p): hashlib.sha256(open(p, "rb").read()).hexdigest()
                      for p in sorted(glob.glob(os.path.join(CASES_DIR, "*.json")))}
    manifest = {"created_at": stamp, "costmax_binary": os.path.abspath(args.binary),
                "costmax_binary_sha256": binary_sha256, "codex_version": codex_version,
                "fixture_sha256": fixture_hashes, "repetitions": args.repetitions,
                "preflight_runs": args.preflight_runs}
    with open(os.path.join(results_dir, "manifest.json"), "w") as f:
        json.dump(manifest, f, indent=2)

    preflights = []
    for i in range(args.preflight_runs):
        preflight_dir = os.path.join(results_dir, "preflight", f"run-{i + 1:02d}")
        os.makedirs(preflight_dir, exist_ok=True)
        repo = create_project_repo({"files": {"README.md": "CostMax preflight\n"}}, preflight_dir, "repo")
        wrapper = install_mcp_config(repo, args.binary, os.path.join(preflight_dir, "costmax-home"))
        ok, reason = verify_mcp_setup(args.binary, wrapper)
        result = {"run": i + 1, "ok": False, "error": reason, "transcript": os.path.join(preflight_dir, "preflight.jsonl")}
        if ok:
            result.update(run_preflight(repo, wrapper, result["transcript"]))
        preflights.append(result)
        print(f"  Preflight {i + 1}/{args.preflight_runs}: {'PASS' if result['ok'] else 'FAIL'}")
    if preflights:
        with open(os.path.join(results_dir, "preflights.json"), "w") as f:
            json.dump(preflights, f, indent=2)
        if not all(p["ok"] for p in preflights):
            print(f"Preflight failed. Evidence: {results_dir}")
            return 1
    if args.preflight_only:
        if args.preflight_runs == 0:
            ap.error("--preflight-only requires --preflight-runs >= 1")
        print(f"Preflight evidence: {results_dir}")
        return 0

    reports = []
    for case in cases:
        for i in range(args.repetitions):
            print(f"  Evaluating {case['id']} ({i + 1}/{args.repetitions})...")
            r = evaluate_case(case, os.path.join(results_dir, f"run-{i + 1:02d}"), args.binary, live=True)
            r["run"] = i + 1
            reports.append(r)

    rj = os.path.join(results_dir, "report.json")
    with open(rj, "w") as f:
        json.dump(reports, f, indent=2)
    rm = os.path.join(results_dir, "report.md")
    with open(rm, "w") as f:
        f.write(generate_markdown(reports))

    print(f"\nReport JSON: {rj}")
    print(f"Report MD:  {rm}")
    print(f"Evidence directory: {results_dir}")

    exit_code, failures, control_misses = evaluate_exit(reports)
    for failure in failures:
        print(f"  FAIL {failure}")
    for case_id, run in control_misses:
        print(f"  WARN baseline {case_id} run {run}: control miss — baseline transcript is valid but the model answer did not match all expected patterns")
    if failures:
        print("OVERALL: FAIL")
        return exit_code
    if control_misses:
        print(f"OVERALL: PASS ({len(control_misses)} baseline control miss(es) recorded; active arm and harness invariants passed)")
    else:
        print("OVERALL: PASS")
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
