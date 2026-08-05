#!/usr/bin/env python3
"""Detect text overflow in the README SVGs before pushing.

Mono font advance width ~0.6 * font-size; use 0.62 to be safe.
Flags: text extending past the canvas or past its containing rect,
and same-line text pairs that overlap horizontally.
"""
import glob, re, os, sys

WIDTH_FACTOR = 0.62
SVGS = glob.glob(os.path.join(os.path.dirname(__file__), "..", "assets", "*.svg"))

def parse(path):
    src = open(path).read()
    texts = []
    # font-size can be inherited from an enclosing <g>
    group_fs = []
    for gm in re.finditer(r"<g\b([^>]*)>", src):
        gfs = re.search(r"font-size=\"([\d.]+)\"", gm.group(1))
        group_fs.append((gm.end(), float(gfs.group(1)) if gfs else None))
    # walk all <text> elements (handles tspans by flattening text)
    for m in re.finditer(r"<text\b([^>]*)>([\s\S]*?)</text>", src):
        attrs, body = m.group(1), m.group(2)
        x = float(re.search(r"x=\"([\d.-]+)\"", attrs).group(1))
        y = float(re.search(r"y=\"([\d.-]+)\"", attrs).group(1))
        fs = re.search(r"font-size=\"([\d.]+)\"", attrs)
        if fs:
            fs = float(fs.group(1))
        else:
            inherited = [g for (p, g) in group_fs if p < m.start() and g is not None]
            fs = inherited[-1] if inherited else 10
        anchor = re.search(r"text-anchor=\"(\w+)\"", attrs)
        anchor = anchor.group(1) if anchor else "start"
        text = re.sub(r"<[^>]+>", "", body).replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")
        texts.append({"x": x, "y": y, "fs": fs, "anchor": anchor, "text": text, "width": len(text) * fs * WIDTH_FACTOR})
    # canvas size
    wm = re.search(r'width="(\d+)"', src); hm = re.search(r'height="(\d+)"', src)
    W, H = int(wm.group(1)), int(hm.group(1))
    # rects with full geometry
    rects = []
    for m in re.finditer(r"<rect\b([^>]*?)/>", src):
        a = m.group(1)
        rx = re.search(r'x="([\d.-]+)"', a)
        rx = float(rx.group(1)) if rx else 0.0
        ry = re.search(r'y="([\d.-]+)"', a)
        ry = float(ry.group(1)) if ry else 0.0
        rw = float(re.search(r'width="([\d.-]+)"', a).group(1))
        rh = re.search(r'height="([\d.-]+)"', a)
        rh = float(rh.group(1)) if rh else 0.0
        rects.append((rx, rx + rw, ry, ry + rh))
    issues = []
    for t in texts:
        w = t["width"]
        # extent depends on anchor: start -> right, end -> left, middle -> both
        if t["anchor"] == "middle":
            right, left = t["x"] + w / 2, t["x"] - w / 2
        elif t["anchor"] == "end":
            right, left = t["x"], t["x"] - w
        else:
            right, left = t["x"] + w, t["x"]
        if right > W - 8 or left < 8:
            issues.append(f'{os.path.basename(path)}: "{t["text"][:40]}" at x={t["x"]} w={w:.0f} exceeds canvas bounds')
        if t["y"] + t["fs"] * 1.2 > H - 4:
            issues.append(f'{os.path.basename(path)}: "{t["text"][:40]}" at y={t["y"]} fs={t["fs"]} exceeds canvas bottom ({H})')
        # containing rect check: text vertically inside a rect but extending past its right edge
        for (rx, rr, ry, rb) in rects:
            if ry <= t["y"] <= rb and rx <= t["x"] <= rr and t["anchor"] == "start" and right > rr - 6:
                issues.append(f'{os.path.basename(path)}: "{t["text"][:40]}" at x={t["x"]} extends past rect right edge ({rr:.0f})')
                break
    # full 2D collision: text vs text (same line or lines close enough to touch)
    for i, a in enumerate(texts):
        for b in texts[i + 1:]:
            # vertical overlap: bottom of a (baseline + descent) vs top of b (baseline - ascent)
            if a["y"] <= b["y"]:
                if a["y"] + 0.2 * a["fs"] < b["y"] - 0.8 * b["fs"]:
                    continue
            else:
                if b["y"] + 0.2 * b["fs"] < a["y"] - 0.8 * a["fs"]:
                    continue
            def span(t):
                if t["anchor"] == "middle":
                    return (t["x"] - t["width"] / 2, t["x"] + t["width"] / 2)
                if t["anchor"] == "end":
                    return (t["x"] - t["width"], t["x"])
                return (t["x"], t["x"] + t["width"])
            al, ar = span(a)
            bl, br = span(b)
            if min(ar, br) - max(al, bl) > 2:
                issues.append(f'{os.path.basename(path)}: "{a["text"][:30]}" and "{b["text"][:30]}" overlap (y {a["y"]:.0f} vs {b["y"]:.0f})')
    return issues

all_issues = []
for svg in sorted(SVGS):
    try:
        all_issues += parse(svg)
    except Exception as e:
        import traceback
        all_issues.append(f"parse error {os.path.basename(svg)}: {e}\n{traceback.format_exc()}")
if all_issues:
    print("\n".join(all_issues))
    sys.exit(1)
print("CLEAN: no text overflow or same-line collisions in", len(SVGS), "SVGs")
