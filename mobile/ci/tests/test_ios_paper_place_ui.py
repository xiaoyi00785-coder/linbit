#!/usr/bin/env python3
"""Paper place form UI guards ≠ Swift compile."""
from __future__ import annotations
import sys
from pathlib import Path
ROOT = Path(__file__).resolve().parents[3]
fails=[]
def ok(c,m):
    print(("OK " if c else "FAIL ")+m)
    if not c: fails.append(m)
def read(r): return (ROOT/r).read_text()
ps=read("mobile/ios/Linbit/Features/Paper/PaperPlaceSheet.swift")
v=read("mobile/ios/Linbit/Features/Paper/PaperTradingView.swift")
api=read("mobile/ios/Linbit/Features/Paper/PaperTradingAPI.swift")
app=read("mobile/ios/Linbit/App/LinbitApp.swift")
risk=read("mobile/ci/screens-risk.txt")
ok("struct PaperPlaceSheet" in ps, "place sheet present")
ok("quoteOrderQty" in ps and "quantity" in ps, "buy quote / sell qty")
ok("clientOrderId" in ps and "isEmpty" in ps, "stable clientOrderId mint")
ok("INDEX" in ps and "/spot/orders" not in ps, "INDEX note; no spot path in UI")
ok("placeForTestOrUI" in ps, "uses session placeForTestOrUI")
ok("SESSION_STALE" in ps and "INDEX_UNAVAILABLE" in ps, "index/stale handling")
ok("INSUFFICIENT_BALANCE" in ps and "IDEMPOTENCY_CONFLICT" in ps or "409" in ps, "balance/conflict")
ok("reloadBalances" in ps and "reloadAccount" in ps, "success reloads balances")
ok("!PaperTradingFeature.isFlagOn" in ps, "flag re-check rejects write")
ok("busy" in ps, "anti double-submit")
ok("PaperPlaceSheet" in v and "模拟买入" in v, "view wires buy/sell")
ok("openPlace" in api and "placeForceFail" in api, "demo place ok/fail")
ok("place-fail" in api and "INDEX_UNAVAILABLE" in api[api.find("interceptPlace"):api.find("interceptPlace")+900], "fail mock in intercept")
for k in ["paper-place","paper-place-ok","paper-place-fail"]:
    ok(k in app, f"app routes {k}")
    ok(k in risk, f"risk lists {k}")
# pushdown: clientOrderId reuse
ok("if clientOrderId.isEmpty" in ps, "mint only when empty → retry stable")
if fails:
    print("FAILED",len(fails)); [print(" -",f) for f in fails]; sys.exit(1)
print("ALL PASSED (paper place UI ≠ Swift/XCTest)")
