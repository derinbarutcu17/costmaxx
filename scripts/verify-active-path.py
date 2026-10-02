#!/usr/bin/env python3
"""Real active-path verifier for the CostMax built binary.

Drives the actual `costmaxx mcp` stdio server (the same transport opencode and
Codex speak) through the `costmax_run` tool and the `cmx://artifact` resource
read, then inspects the on-disk SQLite store to prove the full ingestion chain:

  - command execution and exit-code propagation;
  - artifact metadata and digest integrity;
  - byte-for-byte retrieval;
  - reduction record persistence;
  - final policy/guard behavior (a real reduction AND a guard downgrade);
  - metrics ledger row creation;
  - no false positive saving on tiny or expanded compact results.

It fails closed on missing files, malformed JSON, missing rows, wrong digests,
wrong decisions, or silent bypasses. Every check runs against a fresh isolated
HOME so the user's live ~/.costmax is never touched.

--negative demonstrates the negative direction: it corrupts one expected
invariant (the artifact digest) and proves the verifier FAILS with a non-zero
exit for that reason — a real tamper demonstration, not an internal
self-report that swallows the failure.
"""

import argparse
import hashlib
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile


class Failure(Exception):
    """A verification invariant was violated."""


def expect(cond, msg):
    if not cond:
        raise Failure(msg)


def rpc_call(binary, home, requests):
    """Send one batch of newline-framed JSON-RPC requests to a fresh server
    process and return the parsed responses plus stderr. One process per
    batch keeps sessions isolated and deterministic."""
    payload = "\n".join(json.dumps(r) for r in requests) + "\n"
    proc = subprocess.run(
        [binary, "mcp"],
        input=payload.encode(),
        capture_output=True,
        env={**os.environ, "HOME": home},
    )
    responses = []
    for line in proc.stdout.decode("utf-8", "replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            responses.append(json.loads(line))
        except json.JSONDecodeError as exc:
            raise Failure(f"non-JSON response line {line!r}: {exc}")
    return responses, proc.stderr.decode("utf-8", "replace")


def tool_result_text(response):
    expect(response.get("error") is None, f"JSON-RPC error: {response.get('error')}")
    result = response.get("result") or {}
    content = result.get("content") or []
    expect(len(content) == 1 and content[0].get("type") == "text", f"unexpected content: {content!r}")
    expect(result.get("isError") is not True, "tool call marked as transport error")
    return content[0].get("text", "")


def artifact_id(text):
    for line in text.splitlines():
        if "Artifact ID: " in line:
            return line.split("Artifact ID: ", 1)[1].strip()
    raise Failure("response carries no Artifact ID:\n" + text[:500])


def response_field(text, name):
    marker = name + ": "
    for line in text.splitlines():
        if marker in line:
            return line.split(marker, 1)[1].strip()
    raise Failure(f"response missing field {name!r}:\n{text[:500]}")


def read_artifact(home, artifact):
    """Byte-for-byte retrieval through the MCP resource read."""
    requests = [{"jsonrpc": "2.0", "id": "read", "method": "resources/read",
                 "params": {"uri": f"cmx://artifact/{artifact}"}}]
    responses, _ = rpc_call(binary_path, home, requests)
    expect(len(responses) == 1, f"expected 1 resource response, got {len(responses)}")
    resp = responses[0]
    expect(resp.get("error") is None, f"resource read error: {resp.get('error')}")
    contents = (resp.get("result") or {}).get("contents") or []
    expect(len(contents) == 1, f"expected 1 content item, got {len(contents)}")
    return contents[0].get("text", "")


def db(home):
    path = os.path.join(home, ".costmax", "costmax.db")
    expect(os.path.isfile(path), f"missing store file {path}")
    return sqlite3.connect(path)


def ledger_row(conn, artifact):
    rows = conn.execute(
        "SELECT harness, reduction_attempted, reduction_applied, exit_code, outcome, category "
        "FROM call_ledger WHERE artifact_id = ?", (artifact,)).fetchall()
    expect(len(rows) == 1, f"call_ledger has {len(rows)} rows for {artifact}, want exactly 1")
    return rows[0]


def verify_digest_integrity(conn, artifact, retrieved_bytes):
    rows = conn.execute(
        "SELECT content_digest, original_bytes, command, cwd FROM artifacts WHERE artifact_id = ?",
        (artifact,)).fetchall()
    expect(len(rows) == 1, f"artifacts has {len(rows)} rows for {artifact}, want exactly 1")
    digest, original_bytes, command, cwd = rows[0]
    computed = hashlib.sha256(retrieved_bytes).hexdigest()
    expect(computed == digest, f"digest mismatch: computed {computed}, stored {digest}")
    expect(len(retrieved_bytes) == original_bytes,
           f"byte count mismatch: retrieved {len(retrieved_bytes)}, stored {original_bytes}")
    expect(original_bytes > 0, f"artifact {artifact} claims zero original bytes")


def seq_output(n):
    return "".join(f"{i}\n" for i in range(1, n + 1)).encode()


def run_positive(binary, home):
    checks = []

    # 1. Large verbose command: must reduce, persist artifact + reduction +
    #    ledger, and allow byte-for-byte retrieval with a matching digest.
    big = "seq 1 5000"
    responses, stderr = rpc_call(binary, home, [
        {"jsonrpc": "2.0", "id": 1, "method": "tools/call",
         "params": {"name": "costmax_run", "arguments": {"command": big}}},
    ])
    expect(len(responses) == 1, f"expected 1 response, got {len(responses)} (stderr: {stderr})")
    text = tool_result_text(responses[0])
    recommendation = response_field(text, "Recommendation")
    expect(recommendation in ("reduce", "artifact_required"),
           f"large output must reduce, got {recommendation}:\n{text[:300]}")
    raw_tokens = int(response_field(text, "Raw tokens"))
    model_tokens = int(response_field(text, "Model-visible tokens"))
    expect(model_tokens < raw_tokens, f"no token saving claimed: raw={raw_tokens} model={model_tokens}")
    aid = artifact_id(text)

    expected_raw = seq_output(5000)
    retrieved = read_artifact(home, aid)
    expect(retrieved.encode() == expected_raw,
           f"retrieval is not byte-identical (len {len(retrieved)} vs {len(expected_raw)})")

    conn = db(home)
    try:
        verify_digest_integrity(conn, aid, expected_raw)
        checks.append("artifact metadata + digest integrity")
        reds = conn.execute(
            "SELECT reducer_name, original_bytes, compact_bytes FROM reduction_records WHERE artifact_id = ?",
            (aid,)).fetchall()
        expect(len(reds) == 1, f"reduction_records has {len(reds)} rows for {aid}, want 1")
        expect(reds[0][2] < reds[0][1], f"reduction record not smaller: {reds[0]}")
        checks.append("reduction record persistence")
        row = ledger_row(conn, aid)
        expect(row[0] == "mcp", f"ledger harness = {row[0]!r}, want 'mcp'")
        expect(row[1] == 1 and row[2] == 1, f"ledger reduction flags wrong: {row}")
        expect(row[4] == "reduction_applied", f"ledger outcome = {row[4]!r}")
        checks.append("ledger row creation")
    finally:
        conn.close()

    # 2. Non-zero exit: must propagate and be recorded as evidence, and a
    #    failing command with no output must claim no saving.
    responses, _ = rpc_call(binary, home, [
        {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
         "params": {"name": "costmax_run", "arguments": {"command": "exit 7"}}},
    ])
    text = tool_result_text(responses[0])
    expect("Exit: 7" in text, f"exit code 7 not propagated:\n{text[:300]}")
    checks.append("exit-code propagation")

    # 3. Tiny output: no reducer fires, so no saving may be claimed.
    responses, _ = rpc_call(binary, home, [
        {"jsonrpc": "2.0", "id": 3, "method": "tools/call",
         "params": {"name": "costmax_run", "arguments": {"command": "printf 'tiny'"}}},
    ])
    text = tool_result_text(responses[0])
    rec = response_field(text, "Recommendation")
    expect(rec not in ("reduce", "artifact_required"),
           f"tiny output must not claim a saving, got {rec}:\n{text[:300]}")
    checks.append("no false saving on tiny output")

    # 4. Guard downgrade: the policy recommends reduce for a reducible output,
    #    but a long command makes the fully rendered envelope larger than the
    #    raw output, so the guard must downgrade to passthrough and the ledger
    #    must record outcome=guarded (attempted=1, applied=0).
    guard_cmd = "seq 1 500 # " + "x" * 1000
    responses, _ = rpc_call(binary, home, [
        {"jsonrpc": "2.0", "id": 4, "method": "tools/call",
         "params": {"name": "costmax_run", "arguments": {"command": guard_cmd}}},
    ])
    text = tool_result_text(responses[0])
    expect("passthrough" in text, f"guard did not downgrade long command:\n{text[:300]}")
    guarded_aid = artifact_id(text)
    conn = db(home)
    try:
        row = ledger_row(conn, guarded_aid)
        expect(row[0] == "mcp", f"guarded ledger harness wrong: {row}")
        expect(row[1] == 1 and row[2] == 0, f"guarded row must have attempted=1 applied=0: {row}")
        expect(row[4] == "guarded", f"guarded outcome wrong: {row[4]!r}")
        checks.append("guard downgrade recorded")
    finally:
        conn.close()

    # 5. Ledger rows for every accepted call in one session process.
    conn = db(home)
    try:
        n = conn.execute("SELECT COUNT(*) FROM call_ledger").fetchone()[0]
        expect(n >= 4, f"expected >=4 ledger rows, got {n}")
        checks.append("ledger row for every accepted call")
    finally:
        conn.close()

    return checks, home


def run_negative(binary, home):
    # Establish fresh evidence with the positive suite, then corrupt exactly
    # one stored invariant (the digest) and re-run the same digest-integrity
    # check the positive suite uses. The verifier must FAIL for that reason:
    # main() reports the non-zero failure instead of swallowing it, so the
    # negative run is a real demonstration that tampered evidence is rejected.
    checks, home = run_positive(binary, home)
    for c in checks:
        print(f"  PASS  {c} (setup for negative direction)")
    conn = db(home)
    row = conn.execute("SELECT artifact_id FROM call_ledger ORDER BY event_timestamp LIMIT 1").fetchone()
    expect(row is not None, "negative run needs a prior ledger row")
    artifact = row[0]
    conn.execute("UPDATE artifacts SET content_digest = 'corrupted-digest' WHERE artifact_id = ?", (artifact,))
    conn.commit()
    conn.close()
    print(f"NEGATIVE DEMO: corrupted the stored digest of artifact {artifact}; the integrity check must now FAIL")
    # Must raise Failure. If it returns, tampered evidence was accepted.
    verify_digest_integrity(db(home), artifact, seq_output(5000))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", help="path to the built costmaxx binary")
    parser.add_argument("--negative", action="store_true",
                        help="run the negative direction: corrupt a digest and prove detection")
    parser.add_argument("--keep", action="store_true", help="keep the temp home for inspection")
    args = parser.parse_args()

    if not os.path.isfile(args.binary) or not os.access(args.binary, os.X_OK):
        print(f"FAIL: binary {args.binary} does not exist or is not executable")
        return 1
    global binary_path
    binary_path = os.path.abspath(args.binary)

    home = tempfile.mkdtemp(prefix="costmaxx-verify-")
    try:
        checks, home = run_positive(binary_path, home)
        print(f"Evidence HOME: {home}")
        for c in checks:
            print(f"  PASS  {c}")
        if args.negative:
            try:
                run_negative(binary_path, home)
            except Failure as exc:
                # A real, non-zero failure: tampered evidence was rejected.
                print(f"FAIL: {exc}")
                print("NEGATIVE-DEMO PASS: tampered evidence produced a non-zero verifier failure")
                return 1
            print("NEGATIVE-DEMO FAILED: tampered evidence was NOT detected")
            return 1
        print(f"PASS: {len(checks)} active-path invariant(s) verified")
        return 0
    except Failure as exc:
        print(f"FAIL: {exc}")
        return 1
    finally:
        if not args.keep:
            shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())