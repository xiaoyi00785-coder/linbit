#!/usr/bin/env python3
"""由 mobile/tokens/tokens.json 生成 Android 设计令牌 ui/theme/Tokens.kt（对应 iOS DesignSystem/Tokens.swift）。

用法：python3 mobile/tools/gen_android_tokens.py
Android 与 iOS 共用同一份 tokens.json；仅有的平台差异写在下面的 DARK_OVERRIDES：
  - 夜间背景使用品牌指定的 #070808（tokens.json 中的 #0B0B0C 是按日间层级推导、"待设计确认"的占位）。
"""
import json, os, re

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = os.path.join(HERE, "..", "tokens", "tokens.json")
OUT = os.path.join(HERE, "..", "android", "app", "src", "main", "kotlin", "click", "cowa", "linbit", "ui", "theme", "Tokens.kt")
DARK_OVERRIDES = {"bg": "#070808", "textOnPrimary": "#070808"}


def color(v):
    h = v.lstrip("#")
    if len(h) == 6: h = "FF" + h
    else: h = h[6:] + h[:6]  # tokens 用 #RRGGBBAA，Compose 用 0xAARRGGBB
    return "Color(0x%s)" % h.upper()


def main():
    t = json.load(open(SRC, encoding="utf-8"))
    names = list(t["color"]["light"].keys())
    L = ["// 由 mobile/tools/gen_android_tokens.py 从 mobile/tokens/tokens.json 生成，请勿手改。",
         "package click.cowa.linbit.ui.theme", "",
         "import androidx.compose.ui.graphics.Color", "import androidx.compose.ui.unit.dp", "import androidx.compose.ui.unit.sp", "",
         "/** 颜色令牌（与 iOS Palette 一一对应）。 */", "data class Palette("]
    L += ["    val %s: Color," % n for n in names]
    L += ["    val isDark: Boolean,", ")", ""]
    for mode in ("light", "dark"):
        vals = dict(t["color"][mode])
        if mode == "dark": vals.update(DARK_OVERRIDES)
        L.append("val %sPalette = Palette(" % mode.capitalize())
        L += ["    %s = %s," % (n, color(vals[n])) for n in names]
        L += ["    isDark = %s," % ("true" if mode == "dark" else "false"), ")", ""]
    L.append("object FontSize {")
    for k, v in t["font"]["size"].items(): L.append("    val %s = %d.sp" % (k, v))
    L += ["}", "", "object Spacing {"]
    for k, v in t["spacing"].items(): L.append("    val %s = %d.dp" % (k, v))
    L += ["}", "", "object Radius {"]
    for k, v in t["radius"].items(): L.append("    val %s = %d.dp" % (k, v))
    L += ["}", "", "object Metrics {"]
    s = t["size"]
    for k, v in s.items():
        if isinstance(v, dict):
            for k2, v2 in v.items():
                L.append("    val %s%s = %d.dp" % (k, k2[0].upper() + k2[1:], v2))
        else:
            L.append("    val %s = %d.dp" % (k, v))
    L += ["}", ""]
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    open(OUT, "w", encoding="utf-8").write("\n".join(L))
    print("done")


if __name__ == "__main__":
    main()
