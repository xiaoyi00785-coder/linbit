#!/usr/bin/env python3
"""把 mobile/coin-icons 的 CC0 SVG 转为 Android VectorDrawable / iOS imageset。

默认只更新 Android；--ios 额外生成 iOS 资源与 CoinSymbols.swift（iOS 组件另行接入）。
仅需 Python 标准库。源文件与许可说明见 mobile/coin-icons/NOTICE.md。
"""
from pathlib import Path
import json
import re
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
SOURCES = HERE.parent / "coin-icons"
ANDROID = HERE.parent / "android/app/src/main/res/drawable"
IOS = HERE.parent / "ios/Linbit"
NS = "{http://www.w3.org/2000/svg}"


def number(value):
    text = f"{float(value):.5f}".rstrip("0").rstrip(".")
    return "0" if text in ("-0", "") else text


def color(value):
    value = (value or "#000000").strip()
    if value.startswith("#") and len(value) == 4:
        value = "#" + "".join(c * 2 for c in value[1:])
    if not re.fullmatch(r"#[0-9a-fA-F]{6}", value):
        raise ValueError(f"不支持的 SVG 颜色：{value}")
    return value.upper()


def circle_path(cx, cy, radius):
    cx, cy, radius = map(float, (cx, cy, radius))
    return (f"M{number(cx-radius)},{number(cy)}"
            f"a{number(radius)},{number(radius)} 0 1 0 {number(2*radius)},0"
            f"a{number(radius)},{number(radius)} 0 1 0 {number(-2*radius)},0z")


def convert(source, target):
    root = ET.parse(source).getroot()
    viewport = [float(v) for v in root.attrib["viewBox"].replace(",", " ").split()]
    if len(viewport) != 4:
        raise ValueError(f"{source} 缺少有效 viewBox")
    lines = [
        '<?xml version="1.0" encoding="utf-8"?>',
        '<!-- 由 mobile/tools/gen_coin_icons.py 从 CC0 SVG 生成，请勿手改 -->',
        '<vector xmlns:android="http://schemas.android.com/apk/res/android"',
        '    android:width="32dp" android:height="32dp"',
        f'    android:viewportWidth="{number(viewport[2])}" android:viewportHeight="{number(viewport[3])}">',
    ]
    def walk(element, inherited):
        nonlocal lines
        attrs = dict(inherited)
        attrs.update(element.attrib)
        tag = element.tag.replace(NS, "")
        if tag == "path":
            data = attrs.get("d")
        elif tag == "circle":
            data = circle_path(attrs["cx"], attrs["cy"], attrs["r"])
        elif tag == "rect":
            x, y = float(attrs.get("x", 0)), float(attrs.get("y", 0))
            w, h = float(attrs["width"]), float(attrs["height"])
            data = f"M{number(x)},{number(y)}h{number(w)}v{number(h)}h{number(-w)}z"
        elif tag in ("svg", "g"):
            if "transform" in attrs:
                raise ValueError(f"{source} 含需转换的 transform，请先展开路径")
            for child in element:
                walk(child, attrs)
            return
        else:
            return
        opacity = float(attrs.get("opacity", attrs.get("fill-opacity", 1)))
        fill = color(attrs.get("fill", "#000000"))
        if fill == "none":
            return
        if opacity != 1:
            fill = f"#{round(opacity*255):02X}{fill[1:]}"
        lines.extend([
            "    <path",
            f'        android:fillColor="{fill}"',
            f'        android:pathData="{data}" />',
        ])
    walk(root, {})
    lines.append("</vector>")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("\n".join(lines) + "\n", encoding="utf-8")


def ios_imageset(source, directory):
    directory.mkdir(parents=True, exist_ok=True)
    filename = directory.name.removesuffix(".imageset") + ".svg"
    (directory / filename).write_text(source.read_text(encoding="utf-8"), encoding="utf-8")
    (directory / "Contents.json").write_text(json.dumps({
        "images": [{"idiom": "universal", "filename": filename}],
        "info": {"author": "xcode", "version": 1},
        "properties": {"preserves-vector-representation": True},
    }, indent=2) + "\n", encoding="utf-8")


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ios", action="store_true", help="同时生成 iOS SVG imageset")
    args = parser.parse_args()
    sources = sorted(SOURCES.glob("*.svg"))
    for source in sources:
        symbol = source.stem.lower()
        convert(source, ANDROID / f"coin_{symbol}.xml")
        if args.ios:
            ios_imageset(source, IOS / f"Resources/Assets.xcassets/coin-{symbol}.imageset")
    if args.ios:
        body = "\n".join(f'        "{s.stem.upper()}": "coin-{s.stem}",' for s in sources)
        (IOS / "DesignSystem/CoinSymbols.swift").write_text(
            "import Foundation\n\n/// 由 mobile/tools/gen_coin_icons.py 生成；仅本地已打包图标。\n"
            f"enum CoinSymbols {{\n    static let bundled: [String: String] = [\n{body}\n    ]\n}}\n",
            encoding="utf-8")
    print(f"已生成 {len(sources)} 个 Android VectorDrawable" + ("及 iOS imageset" if args.ios else ""))


if __name__ == "__main__":
    main()
