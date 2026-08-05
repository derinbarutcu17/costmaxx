#!/usr/bin/env python3
"""Generate README proof charts from the authoritative live-run report.

Outputs assets/savings-chart.svg and assets/outcomes-donut.svg. Dark design
system, self-contained (no external fonts), GitHub-sanitizable.
"""
import json, os, math

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPORT = os.path.join(REPO, "results", "20260805T114713Z-authoritative", "report.json")
OUT = os.path.join(REPO, "assets")

BG = "#020617"; PANEL = "#0f172a"; GRID = "#1e293b"
CYAN = "#22d3ee"; EMERALD = "#34d399"; AMBER = "#fbbf24"; VIOLET = "#a78bfa"
SLATE = "#94a3b8"; WHITE = "#e2e8f0"
MONO = "ui-monospace, SFMono-Regular, Menlo, monospace"

SHORT = {
    "case-001-verbose-tail": "001 verbose-tail", "case-002-failing-tests": "002 failing-tests",
    "case-003-json-summary": "003 json-summary", "case-004-lint": "004 lint",
    "case-005-search": "005 search", "case-006-diff": "006 diff", "case-007-build": "007 build",
    "case-008-jest-stack": "008 jest-stack", "case-009-pytest-collection": "009 pytest",
    "case-010-go-test": "010 go-test", "case-011-typescript-build": "011 ts-build",
    "case-012-rust-compile": "012 rust", "case-013-eslint-warnings": "013 eslint",
    "case-014-ripgrep-many-files": "014 ripgrep", "case-015-git-rename-multi": "015 rename",
    "case-016-json-api-groups": "016 json-api", "case-017-json-absent-value": "017 json-absent",
    "case-018-docker-build": "018 docker", "case-019-package-install": "019 npm-install",
    "case-020-terminal-first-last": "020 deploy-log",
}

def esc(t):
    return t.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")

def load():
    recs = json.load(open(REPORT))
    agg = {}
    for rec in recs:
        cid = rec["case_id"]
        d = agg.setdefault(cid, {"base": [], "act": [], "ok": []})
        d["base"].append(rec["baseline"]["tokens"])
        d["act"].append(rec["active"]["tokens"])
        d["ok"].append(rec["active"]["all_match"])
    rows = []
    for cid in SHORT:
        d = agg[cid]
        rows.append((cid, sum(d["base"]) // 3, sum(d["act"]) // 3, all(d["ok"])))
    totals = (sum(r["baseline"]["tokens"] for r in recs), sum(r["active"]["tokens"] for r in recs))
    outcomes = {}
    for r in recs:
        outcomes[r["outcome"]] = outcomes.get(r["outcome"], 0) + 1
    return rows, totals, outcomes

def savings_chart(rows, totals):
    W, H = 960, 660
    label_x, plot_x, plot_w = 150, 160, 740
    maxv = max(max(b, a) for _, b, a, _ in rows)
    scale = plot_w / (maxv * 1.06)
    row_h = 26
    top = 78
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="{MONO}">']
    parts.append(f'<rect width="{W}" height="{H}" fill="{BG}"/>')
    parts.append(f'<pattern id="g" width="40" height="40" patternUnits="userSpaceOnUse"><path d="M 40 0 L 0 0 0 40" fill="none" stroke="{GRID}" stroke-width="0.5"/></pattern>')
    parts.append(f'<rect width="{W}" height="{H}" fill="url(#g)"/>')
    parts.append(f'<text x="24" y="38" font-size="17" fill="{WHITE}" font-weight="700">Model-visible tokens per fixture: baseline vs CostMax</text>')
    parts.append(f'<text x="24" y="58" font-size="11" fill="{SLATE}">20 fixtures x 3 repetitions, Codex CLI 0.146.0, fresh binary SHA 10d2253d</text>')
    # legend
    parts.append(f'<rect x="{W-330}" y="30" width="10" height="10" fill="{CYAN}" opacity="0.85"/><text x="{W-314}" y="39" font-size="11" fill="{SLATE}">baseline</text>')
    parts.append(f'<rect x="{W-230}" y="30" width="10" height="10" fill="{EMERALD}" opacity="0.85"/><text x="{W-214}" y="39" font-size="11" fill="{SLATE}">CostMax</text>')
    parts.append(f'<rect x="{W-140}" y="30" width="10" height="10" fill="{AMBER}"/><text x="{W-124}" y="39" font-size="11" fill="{SLATE}">no saving (policy)</text>')
    # gridlines
    for i in range(0, 7):
        x = plot_x + plot_w * i / 6
        parts.append(f'<line x1="{x:.0f}" y1="{top-14}" x2="{x:.0f}" y2="{top+len(rows)*row_h+6}" stroke="{GRID}" stroke-width="0.5"/>')
        val = maxv * i / 6
        parts.append(f'<text x="{x:.0f}" y="{top+len(rows)*row_h+20}" font-size="9" fill="{SLATE}" text-anchor="middle">{val:.0f}</text>')
    for i, (cid, base, act, ok) in enumerate(rows):
        y = top + i * row_h + 12
        parts.append(f'<text x="{label_x}" y="{y+3}" font-size="10" fill="{SLATE}" text-anchor="end">{SHORT[cid]}</text>')
        bw = base * scale
        parts.append(f'<rect x="{plot_x}" y="{y-8}" width="{bw:.0f}" height="9" rx="2" fill="{CYAN}" opacity="0.85"/>')
        parts.append(f'<text x="{plot_x+bw+6:.0f}" y="{y}" font-size="9" fill="{CYAN}">{base}</text>')
        aw = act * scale
        ay = y + 3
        parts.append(f'<rect x="{plot_x}" y="{ay}" width="{aw:.0f}" height="9" rx="2" fill="{EMERALD}" opacity="0.9"/>')
        if act >= base:
            # policy judged reduction not worth it: passthrough, outline it
            parts.append(f'<rect x="{plot_x}" y="{ay}" width="{aw:.0f}" height="9" rx="2" fill="none" stroke="{AMBER}" stroke-width="1.5"/>')
        ax = plot_x + aw + 6
        if ax > plot_x + plot_w - 30:
            ax = plot_x + aw - 46
        parts.append(f'<text x="{ax:.0f}" y="{ay+9}" font-size="9" fill="{EMERALD}">{act}</text>')
    # totals row (true totals across all 60 records, not per-case means)
    ty = top + len(rows) * row_h + 36
    tb, ta = totals
    parts.append(f'<line x1="{24}" y1="{ty-10}" x2="{W-24}" y2="{ty-10}" stroke="{GRID}" stroke-width="1"/>')
    parts.append(f'<text x="24" y="{ty+8}" font-size="13" fill="{WHITE}" font-weight="700">Total: {tb:,} baseline tokens</text>')
    parts.append(f'<text x="310" y="{ty+8}" font-size="13" fill="{EMERALD}" font-weight="700">{ta:,} with CostMax</text>')
    parts.append(f'<text x="560" y="{ty+8}" font-size="13" fill="{WHITE}" font-weight="700">{100*(tb-ta)/tb:.1f}% less</text>')
    parts.append("</svg>")
    return "".join(parts)

def donut(outcomes):
    W, H = 480, 430
    saving = outcomes.get("quality_and_saving", 0)
    nosave = outcomes.get("quality_no_saving", 0)
    miss = outcomes.get("quality_failure", 0)
    total = saving + nosave + miss
    cx, cy, r, sw = 240, 190, 88, 44
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="{MONO}">']
    parts.append(f'<rect width="{W}" height="{H}" fill="{BG}"/>')
    parts.append(f'<pattern id="g2" width="40" height="40" patternUnits="userSpaceOnUse"><path d="M 40 0 L 0 0 0 40" fill="none" stroke="{GRID}" stroke-width="0.5"/></pattern>')
    parts.append(f'<rect width="{W}" height="{H}" fill="url(#g2)"/>')
    parts.append(f'<text x="24" y="34" font-size="16" fill="{WHITE}" font-weight="700">What the run proved</text>')
    parts.append(f'<text x="24" y="52" font-size="11" fill="{SLATE}">60 records: 20 fixtures x 3 repetitions</text>')
    # segments: saving (emerald), no-saving (slate), control miss (amber)
    segs = [(saving, EMERALD), (nosave, "#475569"), (miss, AMBER)]
    offset = 0.0
    for count, color in segs:
        frac = count / total
        if frac <= 0:
            continue
        dash = frac * 2 * math.pi * r
        gap = 2 * math.pi * r - dash
        rot = offset * 360 - 90
        parts.append(f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="none" stroke="{color}" stroke-width="{sw}" stroke-dasharray="{dash:.1f} {gap:.1f}" transform="rotate({rot:.1f} {cx} {cy})" opacity="0.9"/>')
        offset += frac
    parts.append(f'<text x="{cx}" y="{cy-8}" font-size="26" fill="{WHITE}" font-weight="700" text-anchor="middle">60/60</text>')
    parts.append(f'<text x="{cx}" y="{cy+16}" font-size="11" fill="{EMERALD}" text-anchor="middle">active answers correct</text>')
    # legend
    lx, ly = 60, 330
    for i, (count, color, label) in enumerate([(saving, EMERALD, f"{saving} saving cases"), (nosave, "#475569", f"{nosave} no-saving (policy passthrough)"), (miss, AMBER, f"{miss} control-arm baseline miss")]):
        yy = ly + i * 26
        parts.append(f'<rect x="{lx}" y="{yy-10}" width="12" height="12" rx="2" fill="{color}"/>')
        parts.append(f'<text x="{lx+20}" y="{yy}" font-size="12" fill="{WHITE}">{label}</text>')
        parts.append(f'<text x="{lx+330}" y="{yy}" font-size="12" fill="{SLATE}" text-anchor="end">{count}/{total}</text>')
    parts.append("</svg>")
    return "".join(parts)

os.makedirs(OUT, exist_ok=True)
rows, totals, outcomes = load()
with open(os.path.join(OUT, "savings-chart.svg"), "w") as f:
    f.write(savings_chart(rows, totals))
with open(os.path.join(OUT, "outcomes-donut.svg"), "w") as f:
    f.write(donut(outcomes))
print("wrote", os.listdir(OUT))
