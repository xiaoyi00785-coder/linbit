#!/usr/bin/env python3
"""把 mobile/icons/*.svg 转成 Android VectorDrawable（仓库不放 PNG），并由 mobile/android/brand/*.svg（最终版 Linbit Logo）生成自适应启动图标 / 启动页 / 应用内 Logo。

输出到 mobile/android/app/src/main/res：
  drawable/ic_<name>.xml            单色图标（currentColor→黑色，Compose 里用 tint 着色）
  drawable/ic_tab_<name>_light.xml  Tab 图标日间版（currentColor=#111111，白色镂空=#FFFFFF）
  drawable/ic_tab_<name>_dark.xml   Tab 图标夜间版（currentColor=#F5F5F5，镂空=#1C1C1E）
  drawable/ic_mark.xml / ic_mark_lime.xml / ic_wordmark.xml / ic_wordmark_dark.xml   品牌 Logo（mobile/android/brand）
  drawable/ic_launcher_foreground.xml / ic_launcher_monochrome.xml / ic_splash_icon.xml
  mipmap-anydpi-v26/ic_launcher.xml、ic_launcher_round.xml（背景 #ABE127，前景为 Logo 符号，另含 monochrome 主题图标层）
注意：不读取 mobile/icons/logo.svg（iOS 旧 Logo，由 iOS 的 PR 单独更换）。
仅依赖标准库，支持 path / rect / circle，属性 fill / stroke / stroke-width / opacity / linecap / linejoin。
用法：python3 mobile/tools/gen_android_vectors.py
"""
import os, re, xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
ICONS = os.path.join(HERE, "..", "icons")
ANDROID_ICONS = os.path.join(HERE, "..", "android", "icons")  # Android 专用补充图标
BRAND = os.path.join(HERE, "..", "android", "brand")  # 最终版 Logo（单一来源）
RES = os.path.join(HERE, "..", "android", "app", "src", "main", "res")
NS = "{http://www.w3.org/2000/svg}"


def argb(c, alpha=1.0):
    c = c.lstrip("#")
    if len(c) == 3: c = "".join(ch * 2 for ch in c)
    return "#%02X%s" % (round(alpha * 255), c.upper())


def num(v): return ("%.4f" % v).rstrip("0").rstrip(".")


def rect_path(x, y, w, h, rx):
    rx = min(rx, w / 2, h / 2)
    if rx <= 0:
        return f"M{num(x)},{num(y)}h{num(w)}v{num(h)}h{num(-w)}z"
    r = num(rx)
    return (f"M{num(x + rx)},{num(y)}H{num(x + w - rx)}A{r},{r} 0 0 1 {num(x + w)},{num(y + rx)}"
            f"V{num(y + h - rx)}A{r},{r} 0 0 1 {num(x + w - rx)},{num(y + h)}H{num(x + rx)}"
            f"A{r},{r} 0 0 1 {num(x)},{num(y + h - rx)}V{num(y + rx)}A{r},{r} 0 0 1 {num(x + rx)},{num(y)}z")


def circle_path(cx, cy, r):
    return f"M{num(cx - r)},{num(cy)}a{num(r)},{num(r)} 0 1 0 {num(2 * r)},0a{num(r)},{num(r)} 0 1 0 {num(-2 * r)},0z"


def elements(svg_text):
    root = ET.fromstring(svg_text)
    vb = [float(v) for v in root.get("viewBox").split()]
    base = {k: root.get(k) for k in ("fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin")}
    out = []
    for el in root.iter():
        tag = el.tag.replace(NS, "")
        if tag not in ("path", "rect", "circle"): continue
        a = dict(base); a.update({k: v for k, v in el.attrib.items()})
        if tag == "path": d = a["d"]
        elif tag == "rect":
            d = rect_path(float(a.get("x", 0)), float(a.get("y", 0)), float(a["width"]), float(a["height"]), float(a.get("rx", 0)))
        else: d = circle_path(float(a["cx"]), float(a["cy"]), float(a["r"]))
        out.append(dict(tag=tag, d=d, fill=a.get("fill", "#000000"), stroke=a.get("stroke"), sw=a.get("stroke-width"),
                        cap=a.get("stroke-linecap"), join=a.get("stroke-linejoin"), opacity=a.get("opacity")))
    return vb, out


def color(c, cur, white):
    if c is None or c == "none": return None
    if c == "currentColor": return cur
    if c.lower() in ("#fff", "#ffffff"): return white
    return c


def vector(vb, els, size_dp, cur="#000000", white="#FFFFFF", extra_group=None, height_dp=None):
    lines = ['<?xml version="1.0" encoding="utf-8"?>',
             '<!-- 由 mobile/tools/gen_android_vectors.py 从 mobile/icons/*.svg 生成，请勿手改 -->',
             f'<vector xmlns:android="http://schemas.android.com/apk/res/android"',
             f'    android:width="{size_dp}dp" android:height="{height_dp or size_dp}dp"',
             f'    android:viewportWidth="{num(vb[2])}" android:viewportHeight="{num(vb[3])}">']
    ind = "    "
    if extra_group: lines.append(f"{ind}<group {extra_group}>"); ind += "    "
    for e in els:
        fill = color(e["fill"], cur, white)
        stroke = color(e["stroke"], cur, white)
        al = float(e["opacity"]) if e["opacity"] else 1.0
        attrs = [f'android:pathData="{e["d"]}"']
        if fill: attrs.append(f'android:fillColor="{argb(fill, al)}"')
        if stroke:
            attrs.append(f'android:strokeColor="{argb(stroke, al)}"')
            attrs.append(f'android:strokeWidth="{e["sw"] or 1}"')
            if e["cap"]: attrs.append(f'android:strokeLineCap="{e["cap"]}"')
            if e["join"]: attrs.append(f'android:strokeLineJoin="{e["join"]}"')
        lines.append(ind + "<path " + "\n".join(ind + "    " + x if i else x for i, x in enumerate(attrs)).replace("\n", "\n") + "/>")
    if extra_group: lines.append("    </group>")
    lines.append("</vector>")
    return "\n".join(lines) + "\n"


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "w", encoding="utf-8").write(text)


def main():
    drawable = os.path.join(RES, "drawable")
    sources = [(ICONS, f) for f in sorted(os.listdir(ICONS))] + [(ANDROID_ICONS, f) for f in sorted(os.listdir(ANDROID_ICONS))]
    for src, f in sources:
        if not f.endswith(".svg") or (src == ICONS and f == "logo.svg"): continue
        name = f[:-4].replace("-", "_")
        vb, els = elements(open(os.path.join(src, f), encoding="utf-8").read())
        if f.startswith("duo-"):
            # 灰色实心 + 镂空（清除按钮）：不随 tint 变色，日夜各一版
            write(os.path.join(drawable, f"ic_{name}_light.xml"), vector(vb, els, 24, "#A3A3A3", "#FFFFFF"))
            write(os.path.join(drawable, f"ic_{name}_dark.xml"), vector(vb, els, 24, "#5E5E62", "#161618"))
        elif f.startswith("mc-"):
            # 多色（深色块 + 白色镂空字形）：日夜各一版，不随 tint 变色
            write(os.path.join(drawable, f"ic_{name}_light.xml"), vector(vb, els, 24, "#111111", "#FFFFFF"))
            write(os.path.join(drawable, f"ic_{name}_dark.xml"), vector(vb, els, 24, "#F5F5F5", "#161618"))
        elif f.startswith("tab-"):
            write(os.path.join(drawable, f"ic_{name}_light.xml"), vector(vb, els, 24, "#111111", "#FFFFFF"))
            write(os.path.join(drawable, f"ic_{name}_dark.xml"), vector(vb, els, 24, "#F5F5F5", "#1C1C1E"))
        elif f.startswith("g-") and f != "g-redpacket.svg":
            # 首页宫格图标：currentColor 部分日夜不同（品牌绿描边不变），放在图标块（fill 色）上
            write(os.path.join(drawable, f"ic_{name}_light.xml"), vector(vb, els, 24, "#111111", "#F3F3F3"))
            write(os.path.join(drawable, f"ic_{name}_dark.xml"), vector(vb, els, 24, "#F5F5F5", "#1E1E20"))
        else:
            write(os.path.join(drawable, f"ic_{name}.xml"), vector(vb, els, 24))
    brand()
    adaptive = ('<?xml version="1.0" encoding="utf-8"?>\n'
                '<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n'
                '    <background android:drawable="@color/ic_launcher_background" />\n'
                '    <foreground android:drawable="@drawable/ic_launcher_foreground" />\n'
                '    <monochrome android:drawable="@drawable/ic_launcher_monochrome" />\n'
                '</adaptive-icon>\n')
    for n in ("ic_launcher", "ic_launcher_round"):
        write(os.path.join(RES, "mipmap-anydpi-v26", n + ".xml"), adaptive)
    print("done")


def brand():
    drawable = os.path.join(RES, "drawable")
    def load(name):
        return elements(open(os.path.join(BRAND, name), encoding="utf-8").read())
    vb, els = load("mark.svg")
    write(os.path.join(drawable, "ic_mark.xml"), vector(vb, els, 24))
    vb, els = load("mark-lime.svg")
    write(os.path.join(drawable, "ic_mark_lime.xml"), vector(vb, els, 24))
    for src, out in (("wordmark.svg", "ic_wordmark.xml"), ("wordmark-dark.svg", "ic_wordmark_dark.xml")):
        vb, els = load(src)
        w = 120
        write(os.path.join(drawable, out), vector(vb, els, w, height_dp=round(w * vb[3] / vb[2], 2)))
    # 应用图标：logo.svg = 全幅 #abe127 底 + 居中符号（已按自适应图标 66/108 安全区缩放）。只取符号路径作前景。
    vb, els = load("logo.svg")
    bg = [e for e in els if e["tag"] == "rect"][0]["fill"]
    fg = [e for e in els if e["tag"] == "path"]
    write(os.path.join(drawable, "ic_launcher_foreground.xml"), vector(vb, fg, 108))
    write(os.path.join(drawable, "ic_launcher_monochrome.xml"), vector(vb, [dict(e, fill="#000000") for e in fg], 108))
    # Android 12+ 系统启动页图标：288dp 画布、192dp 圆形安全区；符号再缩 0.85 确保落在圆内
    big = [0, 0, vb[2] / 0.85, vb[3] / 0.85]
    off = (big[2] - vb[2]) / 2
    write(os.path.join(drawable, "ic_splash_icon.xml"),
          vector(big, fg, 288, extra_group='android:translateX="%s" android:translateY="%s"' % (num(off), num(off))))
    write(os.path.join(RES, "values", "ic_launcher_background.xml"),
          '<?xml version="1.0" encoding="utf-8"?>\n<resources>\n    <color name="ic_launcher_background">%s</color>\n</resources>\n' % bg.upper())


if __name__ == "__main__":
    main()
