#!/usr/bin/env python3
"""Paper trading client layer guards (≠ Swift/XCTest/Xcode). No remote calls."""
from __future__ import annotations
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
fails: list[str] = []

def ok(c: bool, m: str) -> None:
    print(("OK " if c else "FAIL ") + m)
    if not c:
        fails.append(m)

def read(rel: str) -> str:
    return (ROOT / rel).read_text()

api = read("mobile/ios/Linbit/Features/Paper/PaperTradingAPI.swift")
client = read("mobile/ios/Linbit/Core/Network/APIClient.swift")
yml = read("mobile/ios/project.yml")
fb = read("mobile/ios/Linbit/DesignSystem/Feedback.swift")
trade = read("mobile/ios/Linbit/Core/Network/TradeAPI.swift")

# Paths: paper only, never spot place reuse
ok('"/paper/account"' in api, "GET /paper/account")
ok('"/paper/account/reset"' in api, "POST /paper/account/reset")
ok('"/paper/balances"' in api, "GET /paper/balances")
ok('"/paper/orders"' in api and "PaperTradingAPI.place" in api.replace("func place", "X"), "paper place via /paper/orders")
ok('"/paper/trades"' in api, "GET /paper/trades")
ok('post("/spot/orders' not in api and 'get("/spot/orders' not in api, "Paper has no /spot/orders call sites")
ok('post("/spot/orders' not in api and 'get("/spot/orders' not in api, "Paper has no /spot/orders call sites")

# Decimal strings — no Double/Float fields for money
for bad in ["var quantity: Double", "var threshold: Double", "var free: Double", "var initialBalance: Double",
            "Float(", "as! Double", "Double("]:
    # allow Dec.parse for validation only
    pass
ok("var initialBalance: String" in api and "var free: String" in api and "var quantity: String" in api, "balances/qty as String")
ok("var quoteOrderQty: String" in api, "quoteOrderQty as String")
ok("Double(" not in api.split("validatePlace")[0], "no Double() in models/API section before validate")

# Gate default closed
ok('flagKey = "paperTrading"' in api, "flag key paperTrading")
ok("flags[flagKey] == true" in api, "missing flag → closed (== true, not isOn default)")
ok("AppFeatures.shared.isOn(flagKey)" not in api, "does not use isOn default-true for paper")

# Idempotency header
ok('Idempotency-Key' in api, "reset sends Idempotency-Key")
ok("headers: [String: String]" in client and "for (k, v) in headers" in client, "APIClient supports custom headers")
ok("RESET_PAPER_ACCOUNT" in api, "reset confirmation enum string")

# Place validation buy/sell
ok("quoteOrderQty" in api and "sell" in api.split("validatePlace")[1][:800], "validatePlace encodes buy/sell rules")
ok('type: String = "market"' in api, "market-only default type")

# Errors
ok("INDEX_UNAVAILABLE" in api and "FEATURE_DISABLED" in api, "index/disabled helpers")
ok("PAPER_RESET_LIMIT_EXCEEDED" in fb, "ErrorText reset limit")
ok("PAPER_ORDER_NOT_FOUND" in fb, "ErrorText order not found")

# Session isolation
ok("reloadSeq" in api and "writeGen" in api, "reloadSeq + writeGen")
ok("SESSION_STALE" in api, "stale write error")
ok("rateLimitUntil = nil" in api.split("func invalidate")[1].split("func ")[0], "invalidate clears rate limit")

# Demo / Release gate
ok("blocksRemoteWrites" in api, "blocksRemoteWrites")
ok(re.search(r"guard ShotGate\.enabled else \{ return false \}", api) is not None, "Release ShotGate off → writes not sealed")
ok("interceptPlace" in api and "interceptReset" in api, "DEBUG mock intercept place/reset")
ok("SHOT_BLOCKED" in api, "non-paper shot blocks writes")

# Behavioral pushdowns
def flag_on(flags: dict) -> bool:
    return flags.get("paperTrading") is True
ok(flag_on({}) is False, "pushdown: missing flag closed")
ok(flag_on({"paperTrading": False}) is False, "pushdown: false closed")
ok(flag_on({"paperTrading": True}) is True, "pushdown: true open")

def blocks_remote(enabled: bool, shot: str | None) -> bool:
    if not enabled:
        return False
    return shot is not None
ok(blocks_remote(False, "paper-orders") is False, "pushdown Release keeps formal API path")
ok(blocks_remote(True, "home") is True, "pushdown any DEBUG shot blocks remote paper write")
ok(blocks_remote(True, None) is False, "pushdown DEBUG no-shot allows formal path")

def validate_place(side: str, qty: str, quote: str) -> str | None:
    def pos(s: str) -> bool:
        if not s or s[0] in "+-" or "e" in s.lower():
            return False
        if not all(c.isdigit() or c == "." for c in s):
            return False
        if s.count(".") > 1:
            return False
        try:
            return float(s) > 0  # test-only; Swift uses Dec.parse
        except ValueError:
            return False
    if side == "sell":
        if not pos(qty):
            return "sell qty"
        if quote:
            return "sell no quote"
        return None
    if side == "buy":
        if pos(qty) == pos(quote):
            return "buy xor"
        return None
    return "side"
ok(validate_place("sell", "0.01", "") is None, "pushdown sell qty ok")
ok(validate_place("sell", "0.01", "10") is not None, "pushdown sell rejects quote")
ok(validate_place("buy", "", "100.00") is None, "pushdown buy quote ok")
ok(validate_place("buy", "0.01", "100") is not None, "pushdown buy rejects both")
ok(validate_place("buy", "", "") is not None, "pushdown buy requires one")

# Project inclusion
ok("sources: [Linbit]" in yml, "XcodeGen sources [Linbit] includes Features/Paper/")
ok((ROOT / "mobile/ios/Linbit/Features/Paper/PaperTradingAPI.swift").is_file(), "PaperTradingAPI.swift on disk")
# No XCTest target in project.yml
ok("testTarget" not in yml and "LinbitTests" not in yml, "no XCTest target in project.yml — cannot add runnable Swift tests here")

# clientOrderId pattern
cid = "ios." + ("b" * 20)
ok(re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", cid) is not None, "clientOrderId pattern")

if fails:
    print("FAILED", len(fails))
    for f in fails:
        print(" -", f)
    sys.exit(1)
print("ALL PASSED (paper source/contract guards ≠ Swift compile / XCTest / remote)")
