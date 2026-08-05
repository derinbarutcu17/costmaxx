#!/usr/bin/env python3
"""Generate minimal README proof graphics from the authoritative live-run report.

Outputs assets/savings-chart.svg and assets/outcomes-bar.svg. Design: neutral
dark, text hierarchy via weight/size, color for accents only.
"""
import json, os

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPORT = os.path.join(REPO, "results", "20260805T114713Z-authoritative", "report.json")
OUT = os.path.join(REPO, "assets")

BG = "#0a0a0b"
PANEL = "#131316"
STROKE = "#26262a"
ACCENT = "#22d3ee"
GRAY_BAR = "#3f3f46"
T1 = "#f4f4f5"   # primary text
T2 = "#a1a1aa"   # secondary
T3 = "#71717a"   # tertiary
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


def load():
    recs = json.load(open(REPORT))
    agg = {}
    for rec in recs:
        cid = rec["case_id"]
        d = agg.setdefault(cid, {"base": [], "act": []})
        d["base"].append(rec["baseline"]["tokens"])
        d["act"].append(rec["active"]["tokens"])
    rows = [(cid, sum(d["base"]) // 3, sum(d["act"]) // 3) for cid, d in agg.items()]
    totals = (sum(r["baseline"]["tokens"] for r in recs), sum(r["active"]["tokens"] for r in recs))
    outcomes = {}
    for r in recs:
        outcomes[r["outcome"]] = outcomes.get(r["outcome"], 0) + 1
    return rows, totals, outcomes


def savings_chart(rows, totals):
    W, H = 960, 680
    label_x, plot_x, plot_w = 24, 210, 726
    maxv = max(max(b, a) for _, b, a in rows)
    scale = plot_w / (maxv * 1.06)
    row_h = 26
    top = 86
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="{MONO}">']
    parts.append(f'<rect width="{W}" height="{H}" fill="{BG}"/>')
    parts.append(f'<text x="24" y="34" font-size="16" font-weight="700" fill="{T1}">Model-visible tokens per fixture</text>')
    parts.append(f'<text x="24" y="54" font-size="11" fill="{T2}">20 fixtures x 3 reps · Codex CLI 0.146.0 · fresh binary · 5 of 20 cases: policy chose passthrough (output already compact)</text>')
    # legend
    parts.append(f'<rect x="{W-220}" y="24" width="10" height="10" fill="{GRAY_BAR}"/><text x="{W-202}" y="33" font-size="11" fill="{T2}">baseline</text>')
    parts.append(f'<rect x="{W-120}" y="24" width="10" height="10" fill="{ACCENT}"/><text x="{W-102}" y="33" font-size="11" fill="{T2}">CostMax</text>')
    # axis ticks
    for i in range(0, 5):
        x = plot_x + plot_w * i / 4
        val = maxv * i / 4
        parts.append(f'<line x1="{x:.0f}" y1="{top-12}" x2="{x:.0f}" y2="{top+len(rows)*row_h+4}" stroke="{STROKE}" stroke-width="0.75"/>')
        parts.append(f'<text x="{x:.0f}" y="{top+len(rows)*row_h+18}" font-size="9" fill="{T3}" text-anchor="middle">{val:.0f}</text>')
    for i, (cid, base, act) in enumerate(rows):
        y = top + i * row_h + 12
        parts.append(f'<text x="{label_x}" y="{y+3}" font-size="10" fill="{T2}">{SHORT[cid]}</text>')
        bw = base * scale
        parts.append(f'<rect x="{plot_x}" y="{y-8}" width="{bw:.0f}" height="9" rx="1.5" fill="{GRAY_BAR}"/>')
        aw = act * scale
        parts.append(f'<rect x="{plot_x}" y="{y+3}" width="{aw:.0f}" height="9" rx="1.5" fill="{ACCENT}"/>')
    # totals row
    ty = top + len(rows) * row_h + 38
    tb, ta = totals
    parts.append(f'<line x1="24" y1="{ty-12}" x2="{W-24}" y2="{ty-12}" stroke="{STROKE}" stroke-width="1"/>')
    parts.append(f'<text x="24" y="{ty+10}" font-size="13" font-weight="700" fill="{T1}">Total {tb:,} baseline</text>')
    parts.append(f'<text x="250" y="{ty+10}" font-size="13" font-weight="700" fill="{ACCENT}">{ta:,} with CostMax</text>')
    parts.append(f'<text x="470" y="{ty+10}" font-size="13" font-weight="700" fill="{T1}">{100*(tb-ta)/tb:.1f}% less</text>')
    parts.append("</svg>")
    return "".join(parts)


def outcomes_bar(outcomes):
    W, H = 480, 300
    saving = outcomes.get("quality_and_saving", 0)
    nosave = outcomes.get("quality_no_saving", 0)
    miss = outcomes.get("quality_failure", 0)
    total = saving + nosave + miss
    bar_x, bar_y, bar_w, bar_h = 24, 96, 432, 16
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="{MONO}">']
    parts.append(f'<rect width="{W}" height="{H}" fill="{BG}"/>')
    parts.append(f'<text x="24" y="34" font-size="16" font-weight="700" fill="{T1}">What the run proved</text>')
    parts.append(f'<text x="24" y="54" font-size="11" fill="{T2}">60 records: 20 fixtures x 3 repetitions</text>')
    segs = [(saving / total, ACCENT), (nosave / total, GRAY_BAR), (miss / total, "#26262a")]
    x = bar_x
    for frac, color in segs:
        w = bar_w * frac
        parts.append(f'<rect x="{x:.1f}" y="{bar_y}" width="{max(w, 1):.1f}" height="{bar_h}" rx="2" fill="{color}"/>')
        x += w
    rows = [(saving, ACCENT, "saving cases"), (nosave, GRAY_BAR, "no-saving (policy passthrough)"), (miss, "#3f3f46", "control-arm baseline miss")]
    ly = 150
    for count, color, label in rows:
        parts.append(f'<rect x="24" y="{ly-9}" width="10" height="10" rx="2" fill="{color}"/>')
        parts.append(f'<text x="44" y="{ly}" font-size="12" fill="{T1}">{count} {label}</text>')
        parts.append(f'<text x="{W-24}" y="{ly}" font-size="12" fill="{T2}" text-anchor="end">{count}/{total}</text>')
        ly += 34
    parts.append(f'<text x="24" y="{ly+14}" font-size="11" fill="{T3}">control miss: baseline counted 9 vs 10 · active arm passed 3/3</text>')
    parts.append("</svg>")
    return "".join(parts)


os.makedirs(OUT, exist_ok=True)
rows, totals, outcomes = load()
with open(os.path.join(OUT, "savings-chart.svg"), "w") as f:
    f.write(savings_chart(rows, totals))
with open(os.path.join(OUT, "outcomes-bar.svg"), "w") as f:
    f.write(outcomes_bar(outcomes))
# old donut replaced by the minimal bar
old = os.path.join(OUT, "outcomes-donut.svg")
if os.path.exists(old):
    os.remove(old)
print("wrote:", sorted(os.listdir(OUT)))
