#!/usr/bin/env python3
"""从 mobile/icons/logo.svg 渲染 1024×1024 AppIcon.png（仅标准库，CI 构建前运行）。

仓库不允许提交二进制文件，AppIcon.png 已加入 .gitignore，由本脚本在构建前生成。
logo.svg 本身就是满版（full-bleed）App 图标：viewBox 直接映射到 1024×1024，
脚本不额外加背景、留白或圆角（圆角由 iOS 系统遮罩处理）。
支持的 SVG 子集：<rect>（无圆角）与 <path>；路径命令 M/L/H/V/C/S/Q/T/Z 及其相对形式，
曲线细分为折线，按非零环绕规则填充，4× 垂直子采样 + 水平覆盖率抗锯齿。
用法：python3 mobile/tools/gen_app_icon.py [输出路径]
"""
import os, re, struct, sys, zlib

HERE = os.path.dirname(os.path.abspath(__file__))
SVG = os.path.join(HERE, "..", "icons", "logo.svg")
OUT = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
    HERE, "..", "ios", "Linbit", "Resources", "Assets.xcassets", "AppIcon.appiconset", "AppIcon.png")
SIZE, SUB, CURVE_STEPS = 1024, 4, 24
DEFAULT_BG, DEFAULT_FG = (0xFF, 0xFF, 0xFF), (0x00, 0x00, 0x00)


def hexrgb(s):
    s = s.lstrip("#")
    if len(s) == 3: s = "".join(c * 2 for c in s)
    return tuple(int(s[i:i + 2], 16) for i in (0, 2, 4))


def attr(attrs, name):
    m = re.search(r'(?:^|\s)%s="([^"]*)"' % name, attrs)
    return m.group(1) if m else None


def fill_of(attrs, default):
    f = attr(attrs, "fill")
    return hexrgb(f) if f and f.startswith("#") else default


def path_polys(d):
    toks = re.findall(r"[MmLlHhVvCcSsQqTtZz]|-?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?", d)
    polys, cur = [], []
    x = y = sx = sy = 0.0
    cpx = cpy = None; prev = ""
    i, cmd = 0, None

    def num():
        nonlocal i
        v = float(toks[i]); i += 1; return v

    def bez(pts):
        n = len(pts) - 1
        for k in range(1, CURVE_STEPS + 1):
            t = k / CURVE_STEPS; q = list(pts)
            for r in range(n):
                q = [(a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t) for a, b in zip(q, q[1:])]
            cur.append(q[0])

    while i < len(toks):
        if re.match(r"[A-Za-z]", toks[i]):
            cmd = toks[i]; i += 1
        rel, c = cmd.islower(), cmd.upper()
        ox, oy = (x, y) if rel else (0.0, 0.0)
        if c == "Z":
            if cur: polys.append(cur)
            cur, x, y = [], sx, sy; prev = c; continue
        if c == "M":
            if cur: polys.append(cur)
            x, y = ox + num(), oy + num(); sx, sy = x, y; cur = [(x, y)]
            cmd = "l" if rel else "L"; prev = c; continue
        if not cur: cur = [(x, y)]
        if c == "L": x, y = ox + num(), oy + num(); cur.append((x, y))
        elif c == "H": x = ox + num(); cur.append((x, y))
        elif c == "V": y = (y if rel else 0.0) + num(); cur.append((x, y))
        elif c in "CS":
            if c == "C": x1, y1 = ox + num(), oy + num()
            else: x1, y1 = (2 * x - cpx, 2 * y - cpy) if prev in "CS" else (x, y)
            x2, y2, ex, ey = ox + num(), oy + num(), ox + num(), oy + num()
            bez([(x, y), (x1, y1), (x2, y2), (ex, ey)]); cpx, cpy, x, y = x2, y2, ex, ey
        elif c in "QT":
            if c == "Q": x1, y1 = ox + num(), oy + num()
            else: x1, y1 = (2 * x - cpx, 2 * y - cpy) if prev in "QT" else (x, y)
            ex, ey = ox + num(), oy + num()
            bez([(x, y), (x1, y1), (ex, ey)]); cpx, cpy, x, y = x1, y1, ex, ey
        else:
            raise SystemExit(f"unsupported path command {cmd}")
        prev = c
    if cur: polys.append(cur)
    return polys


def parse(svg):
    vb = [float(v) for v in re.search(r'viewBox="([^"]+)"', svg).group(1).replace(",", " ").split()]
    root = re.search(r"<svg\b([^>]*)>", svg).group(1)
    root_fill = fill_of(root, DEFAULT_FG)
    shapes = []
    for tag, attrs in re.findall(r"<(rect|path)\b([^>]*?)/?>", svg):
        color = fill_of(attrs, root_fill)
        if tag == "rect":
            rx = float(attr(attrs, "x") or 0); ry = float(attr(attrs, "y") or 0)
            w = float(attr(attrs, "width").rstrip("%") if attr(attrs, "width") else vb[2])
            h = float(attr(attrs, "height").rstrip("%") if attr(attrs, "height") else vb[3])
            if "%" in (attr(attrs, "width") or ""): w, h = vb[2] * w / 100, vb[3] * h / 100
            shapes.append((color, [[(rx, ry), (rx + w, ry), (rx + w, ry + h), (rx, ry + h)]]))
        else:
            shapes.append((color, path_polys(attr(attrs, "d"))))
    return vb, shapes


def render():
    vb, shapes = parse(open(SVG).read())
    # 满版：viewBox 等比铺满画布（logo.svg 为正方形），不留白
    scale = SIZE / max(vb[2], vb[3])
    ox = (SIZE - vb[2] * scale) / 2 - vb[0] * scale
    oy = (SIZE - vb[3] * scale) / 2 - vb[1] * scale
    img = [[float(c) for c in DEFAULT_BG] * SIZE for _ in range(SIZE)]
    for color, polys in shapes:
        edges = []
        for poly in polys:
            pts = [(ox + px * scale, oy + py * scale) for px, py in poly]
            for (x0, y0), (x1, y1) in zip(pts, pts[1:] + pts[:1]):
                if y0 != y1: edges.append((x0, y0, x1, y1, min(y0, y1), max(y0, y1)))
        if not edges: continue
        ymin = max(0, int(min(e[4] for e in edges))); ymax = min(SIZE, int(max(e[5] for e in edges)) + 1)
        for row in range(ymin, ymax):
            cov = {}
            for s in range(SUB):
                cy = row + (s + 0.5) / SUB; xs = []
                for x0, y0, x1, y1, lo, hi in edges:
                    if lo <= cy < hi:
                        xs.append((x0 + (cy - y0) * (x1 - x0) / (y1 - y0), 1 if y1 > y0 else -1))
                xs.sort(); wind = 0; start = 0.0
                for xv, w in xs:
                    p = wind; wind += w
                    if p == 0 and wind != 0: start = xv
                    elif p != 0 and wind == 0:
                        a, b = max(0.0, start), min(float(SIZE), xv)
                        if b <= a: continue
                        ia, ib = int(a), int(b)
                        if ia == ib:
                            cov[ia] = cov.get(ia, 0) + (b - a) / SUB; continue
                        cov[ia] = cov.get(ia, 0) + (ia + 1 - a) / SUB
                        for px in range(ia + 1, min(ib, SIZE)): cov[px] = cov.get(px, 0) + 1 / SUB
                        if ib < SIZE: cov[ib] = cov.get(ib, 0) + (b - ib) / SUB
            r = img[row]
            for px, a in cov.items():
                a = min(1.0, a); k = px * 3
                for ch in range(3): r[k + ch] += (color[ch] - r[k + ch]) * a
    raw = b"".join(b"\x00" + bytes(min(255, max(0, int(v + 0.5))) for v in r) for r in img)
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 2, 0, 0, 0)) \
        + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")
    os.makedirs(os.path.dirname(os.path.abspath(OUT)), exist_ok=True)
    open(OUT, "wb").write(png)
    print(f"AppIcon.png {SIZE}x{SIZE} -> {OUT}")


if __name__ == "__main__":
    render()
