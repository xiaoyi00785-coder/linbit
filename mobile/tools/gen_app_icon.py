#!/usr/bin/env python3
"""从 mobile/icons/logo.svg 渲染 1024×1024 AppIcon.png（仅标准库，CI 构建前运行）。

仓库不允许提交二进制文件，AppIcon.png 已加入 .gitignore，由本脚本在构建前生成。
支持 logo.svg 用到的路径子集：M/L/H/V/Z 及其相对形式（直线多边形），按非零环绕规则填充。
用法：python3 mobile/tools/gen_app_icon.py [输出路径]
"""
import os, re, struct, sys, zlib

HERE = os.path.dirname(os.path.abspath(__file__))
SVG = os.path.join(HERE, "..", "icons", "logo.svg")
OUT = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
    HERE, "..", "ios", "Linbit", "Resources", "Assets.xcassets", "AppIcon.appiconset", "AppIcon.png")
SIZE, UNIT, BG, FG = 1024, 32, (0x11, 0x11, 0x11), (0xF5, 0xF5, 0xF5)  # 24 单位 × 32 = 768，四周留 128


def hexrgb(s):
    s = s.lstrip("#"); return tuple(int(s[i:i + 2], 16) for i in (0, 2, 4))


def parse(svg):
    vb = [float(v) for v in re.search(r'viewBox="([^"]+)"', svg).group(1).split()]
    shapes = []
    for attrs in re.findall(r"<path\b([^>]*)/?>", svg):
        d = re.search(r'\sd="([^"]+)"', attrs).group(1)
        m = re.search(r'fill="([^"]+)"', attrs)
        color = hexrgb(m.group(1)) if m and m.group(1).startswith("#") else FG
        polys, cur, x, y, sx, sy = [], [], 0.0, 0.0, 0.0, 0.0
        for cmd, args in re.findall(r"([MmLlHhVvZz])([^MmLlHhVvZz]*)", d):
            n = [float(v) for v in re.findall(r"-?\d*\.?\d+(?:e-?\d+)?", args)]
            rel = cmd.islower(); c = cmd.upper()
            if c == "Z":
                if cur: polys.append(cur)
                cur, x, y = [], sx, sy; continue
            if c in "ML":
                for i in range(0, len(n), 2):
                    x, y = (x + n[i], y + n[i + 1]) if rel else (n[i], n[i + 1])
                    if c == "M" and i == 0:
                        if cur: polys.append(cur)
                        cur, sx, sy = [], x, y
                    cur.append((x, y))
            elif c == "H":
                for v in n: x = x + v if rel else v; cur.append((x, y))
            elif c == "V":
                for v in n: y = y + v if rel else v; cur.append((x, y))
        if cur: polys.append(cur)
        shapes.append((color, polys))
    return vb, shapes


def render():
    vb, shapes = parse(open(SVG).read())
    scale = (SIZE - 2 * 128) / max(vb[2], vb[3])
    ox = (SIZE - vb[2] * scale) / 2 - vb[0] * scale
    oy = (SIZE - vb[3] * scale) / 2 - vb[1] * scale
    rows = [bytearray(bytes(BG) * SIZE) for _ in range(SIZE)]
    for color, polys in shapes:
        edges = []
        for poly in polys:
            pts = [(ox + px * scale, oy + py * scale) for px, py in poly]
            for (x0, y0), (x1, y1) in zip(pts, pts[1:] + pts[:1]):
                if y0 != y1: edges.append((x0, y0, x1, y1))
        for row in range(SIZE):
            cy = row + 0.5; xs = []
            for x0, y0, x1, y1 in edges:
                if (y0 <= cy < y1) or (y1 <= cy < y0):
                    xs.append((x0 + (cy - y0) * (x1 - x0) / (y1 - y0), 1 if y1 > y0 else -1))
            xs.sort(); wind = 0
            for i, (xv, w) in enumerate(xs):
                prev = wind; wind += w
                if prev == 0 and wind != 0: start = xv
                elif prev != 0 and wind == 0:
                    a, b = max(0, int(round(start))), min(SIZE, int(round(xv)))
                    if b > a: rows[row][a * 3:b * 3] = bytes(color) * (b - a)
    raw = b"".join(b"\x00" + bytes(r) for r in rows)
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 2, 0, 0, 0)) \
        + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    open(OUT, "wb").write(png)
    print(f"AppIcon.png {SIZE}x{SIZE} -> {OUT}")


if __name__ == "__main__":
    render()
