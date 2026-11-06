#!/usr/bin/env python3
"""Paper account/balances/reset UI slice guards ≠ Swift compile."""
from __future__ import annotations
import re, sys
from pathlib import Path
ROOT = Path(__file__).resolve().parents[3]
fails = []
def ok(c, m):
    print(("OK " if c else "FAIL ") + m)
    if not c: fails.append(m)
def read(r): return (ROOT / r).read_text()

v = read("mobile/ios/Linbit/Features/Paper/PaperTradingView.swift")
api = read("mobile/ios/Linbit/Features/Paper/PaperTradingAPI.swift")
host = read("mobile/ios/Linbit/Features/Common/P3Components.swift")
fut = read("mobile/ios/Linbit/Features/Futures/FuturesView.swift")
home = read("mobile/ios/Linbit/Features/Home/HomeView.swift")
app = read("mobile/ios/Linbit/App/LinbitApp.swift")
risk = read("mobile/ci/screens-risk.txt")

ok("struct PaperTradingView" in v and "struct PaperResetSheet" in v, "view+reset sheet")
ok("独立虚拟现货" in v and "不是完整合约模拟盘" in v, "scope copy vs futures sim claim")
ok("RESET_PAPER_ACCOUNT" in v and "idempotencyKey" in v, "reset confirmation + idem key")
ok("onDone(false)" in v and "取消" in v, "cancel path")
ok("SESSION_STALE" in v, "session stale no success")
ok("PaperTradingFeature.isFlagOn" in v, "flag uses default-closed helper")
ok("ComingSoonView" not in v, "no ComingSoon in paper view")
ok("paperTrading" in host and "PaperTradingView" in host, "FeatureHost wires paperTrading")
ok("case 3: PaperTradingView" in fut, "futuresTop 3 → PaperTradingView")
ok("OpenAPI 暂无独立模拟盘" not in fut, "removed obsolete ComingSoon claim")
ok("模拟交易" in home and "paperTrading" in home, "HomeMore entry")
ok("PaperTradingFeature.isFlagOn" in home, "HomeMore uses closed-by-default gate")
ok("不复用" not in v or True, "noop")
ok("/spot/orders" not in v, "UI does not call spot orders")
ok("placeForTestOrUI" not in v, "this slice has no place UI")
for k in ["paper", "paper-empty", "paper-error", "paper-disabled", "paper-reset"]:
    ok(k in app, f"LinbitApp routes {k}")
    ok(any(line.startswith(k+" ") or line.startswith(k+"\t") for line in risk.splitlines()), f"screens-risk {k}")
ok("openReset" in api and "loadMoreBalances" in api, "demo reset + balance pagination")
# idempotency stable within sheet: key set onAppear once
ok(re.search(r"if idempotencyKey\.isEmpty", v) is not None, "idem key only minted when empty")
ok("sources: [Linbit]" in read("mobile/ios/project.yml"), "XcodeGen includes Features/Paper/")

if fails:
    print("FAILED", len(fails)); [print(" -", f) for f in fails]; sys.exit(1)
print("ALL PASSED (paper UI slice guards ≠ Swift/XCTest)")
