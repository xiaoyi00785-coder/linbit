#!/usr/bin/env python3
"""静态核对：Release 演示入口须经 ShotGate / #if DEBUG 隔离。

本机无 Xcode 时作为契约检查（非 Swift 编译）。真实 Release 编译交 CI。
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "ios" / "Linbit"
errors: list[str] = []


def read(rel: str) -> str:
    return (ROOT / rel).read_text(encoding="utf-8")


def main() -> int:
    gate = read("Features/Common/ShotGate.swift")
    if "enum ShotGate" not in gate or "enum ShotRaw" not in gate:
        errors.append("ShotGate.swift 缺少 ShotGate / ShotRaw")
    if "#if DEBUG" not in gate or "launchShot" not in gate:
        errors.append("ShotGate 须 DEBUG 闸与 launchShot")

    app = read("App/LinbitApp.swift")
    if "ShotGate.launchShot" not in app and "ShotGate.enabled" not in app:
        # applyScreenshotRoute uses ShotGate
        errors.append("LinbitApp 未使用 ShotGate")
    # onAppear must be wrapped
    if not re.search(r"\.onAppear\s*\{[^}]*#if DEBUG[^}]*applyScreenshotRoute", app, re.S):
        errors.append("applyScreenshotRoute 调用未包在 #if DEBUG")
    if "#if DEBUG" not in app or "private func applyScreenshotRoute" not in app:
        errors.append("applyScreenshotRoute 定义须在 #if DEBUG 内")
    # Must not read UserDefaults lb.shot directly in Release path of applyScreenshotRoute
    # Outside DEBUG block, no UserDefaults lb.shot in RootTabView function - splash handled separately
    if re.search(r"UserDefaults\.standard\.string\(forKey:\s*\"lb\.shot\"\)", app):
        errors.append("LinbitApp 仍直接读 UserDefaults lb.shot（应走 ShotGate.launchShot）")

    splash = read("App/SplashView.swift")
    if "ShotGate.launchShot" not in splash and "ShotGate.enabled" not in splash:
        errors.append("SplashState 未经 ShotGate 读取 lb.shot")
    if re.search(r"UserDefaults\.standard\.string\(forKey:\s*\"lb\.shot\"\)", splash):
        errors.append("SplashView 仍直接读 UserDefaults lb.shot")

    sec = read("Features/Security/SecurityCenter.swift")
    if "ShotRaw.value" not in sec:
        errors.append("SecShot.raw 未接到 ShotRaw")
    if "ShotGate.enabled && raw" not in sec and "ShotGate.enabled" not in sec:
        errors.append("SecShot.on 未结合 ShotGate.enabled")

    # 直接写 SecShot.raw = 只应出现在 #if DEBUG 的 applyScreenshotRoute 内（已整体包住）
    for path in ROOT.rglob("*.swift"):
        text = path.read_text(encoding="utf-8")
        rel = path.relative_to(ROOT)
        if rel.as_posix() in ("App/LinbitApp.swift", "Features/Common/ShotGate.swift"):
            continue
        if 'UserDefaults.standard.string(forKey: "lb.shot")' in text:
            errors.append(f"{rel} 直接读取 lb.shot（应使用 ShotGate.launchShot）")

    if errors:
        print("FAIL ios_shot_gate_check:")
        for e in errors:
            print(" -", e)
        return 1
    print("OK ios_shot_gate_check: Release 演示入口静态隔离通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
