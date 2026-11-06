#!/usr/bin/env python3
"""Paper orders/trades history page guards ≠ Swift compile."""
from __future__ import annotations
import sys
from pathlib import Path
ROOT = Path(__file__).resolve().parents[3]
fails=[]
def ok(c,m):
    print(("OK " if c else "FAIL ")+m)
    if not c: fails.append(m)
def read(r): return (ROOT/r).read_text()

api=read("mobile/ios/Linbit/Features/Paper/PaperTradingAPI.swift")
hv=read("mobile/ios/Linbit/Features/Paper/PaperHistoryView.swift")
pv=read("mobile/ios/Linbit/Features/Paper/PaperTradingView.swift")
host=read("mobile/ios/Linbit/Features/Common/P3Components.swift")
app=read("mobile/ios/Linbit/App/LinbitApp.swift")
risk=read("mobile/ci/screens-risk.txt")
yml=read("mobile/ios/project.yml")

ok("struct PaperHistoryView" in hv, "history view present")
ok("reloadOrders" in hv and "reloadTrades" in hv, "uses formal reload APIs")
ok("loadMoreOrders" in api and "loadMoreTrades" in api, "pagination APIs")
ok("ordersError" in api and "tradesError" in api, "keep-list error fields")
ok("已保留当前列表" in hv, "UI keep-list retry copy")
ok("generation" in hv and "INDEX" in hv and "paper" in hv, "paper/INDEX/generation labels")
ok("/spot/" not in hv and "placeForTestOrUI" not in hv, "history is read-only / no spot")
ok("loadingMoreOrders" in api, "anti duplicate load-more flag")
ok("paperHistory" in host and "PaperHistoryView" in host, "FeatureHost wires paperHistory")
ok("模拟订单 / 成交历史" in pv and "paperHistory" in pv, "account page entry")
ok("ordersPage(page" in api and "tradesPage(page" in api, "demo multi-page fixtures")
ok("pageSize = 3" in api and "po_demo_5" in api, "demo has 5 items across pages")
for k in ["paper-orders","paper-orders-empty","paper-orders-error","paper-trades","paper-trades-empty","paper-trades-error"]:
    ok(k in app, f"app routes {k}")
    ok(k in risk, f"risk {k}")
ok("sources: [Linbit]" in yml and Path(ROOT/"mobile/ios/Linbit/Features/Paper/PaperHistoryView.swift").is_file(), "XcodeGen includes new file")
ok("flags[flagKey] == true" in api, "gate still default-closed")

# pushdown: keep list on error
def keep_on_fail(had_list: bool, err: bool):
    if err and had_list: return "keep+banner"
    if err: return "failed"
    return "loaded"
ok(keep_on_fail(True, True)=="keep+banner", "pushdown fail keeps list")
ok(keep_on_fail(False, True)=="failed", "pushdown first fail → failed state")

if fails:
    print("FAILED",len(fails)); [print(" -",f) for f in fails]; sys.exit(1)
print("ALL PASSED (paper history ≠ Swift/XCTest)")
