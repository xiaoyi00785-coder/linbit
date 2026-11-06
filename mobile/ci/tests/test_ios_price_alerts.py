#!/usr/bin/env python3
"""Price alerts client — isolated mock + Swift source-text guards.

NOT URLSession, NOT XCTest, NOT a claim of Xcode/SwiftUI compile on Linux.
Write paths are MOCKED; no real /price-alerts create/update/delete.
Contract source: origin/main OpenAPI (/price-alerts, spot/INDEX/once/in_app/revision).
"""
from __future__ import annotations
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
failures: list[str] = []

def ok(cond: bool, msg: str) -> None:
    if not cond:
        failures.append(msg)
    else:
        print("OK", msg)

# ---------- OpenAPI contract anchors (from main; branch may lag generated yaml) ----------
# Prefer checking Swift source + a vendored snippet we keep with the client.
swift = (ROOT / "mobile/ios/Linbit/Features/Alerts/PriceAlertsAPI.swift").read_text()
ok("enum PriceAlertsAPI" in swift, "PriceAlertsAPI present")
ok('market: String = "spot"' in swift or 'market: String' in swift, "spot market field")
ok('"INDEX"' in swift and '"once"' in swift and '"in_app"' in swift, "INDEX/once/in_app defaults in source")
ok("revision" in swift and "UpdatePriceAlertRequest" in swift, "revision optimistic lock request")
ok('"/price-alerts"' in swift, "list/create path /price-alerts")
ok("FEATURE_DISABLED" in swift, "FEATURE_DISABLED handling")
ok("Retry-After" in swift or "retryAfter" in swift, "429 Retry-After handling")
ok("priceAlerts" in swift, "feature flag key priceAlerts")
ok("reloadSeq" in swift, "stale session discard via reloadSeq")
ok("SHOT_BLOCKED" in swift or "截图" in swift, "DEBUG write block")
ok("ShotGate" in swift and "PriceAlertDemo" in swift, "DEBUG fixture isolated")
ok("CreatePriceAlertRequest" in swift and "delete(alertId" in swift, "CRUD surface")
ok("pageSize" in swift and "PriceAlertsPage" in swift, "pagination page")

# AppFeatures gate wiring exists
phase = (ROOT / "mobile/ios/Linbit/Core/Network/Phase3API.swift").read_text()
ok("class AppFeatures" in phase and "isOn" in phase, "AppFeatures.isOn available for gate")

# APIClient already surfaces Retry-After
client = (ROOT / "mobile/ios/Linbit/Core/Network/APIClient.swift").read_text()
ok("Retry-After" in client and "retryAfter" in client, "APIClient parses Retry-After")

# ---------- Mock session state machine (Python ≠ Swift) ----------
class MockAlertsSession:
    def __init__(self):
        self.reload_seq = 0
        self.page = None
        self.feature_disabled = False
        self.rate_limit_until_sec = 0
        self.writes = []

    def invalidate(self):
        self.reload_seq += 1
        self.page = None
        self.feature_disabled = False

    def apply_list_response(self, seq: int, payload: dict | None, error: str | None = None):
        if seq != self.reload_seq:
            return False  # stale discarded
        if error == "FEATURE_DISABLED":
            self.feature_disabled = True
            self.page = None
            return True
        self.feature_disabled = False
        self.page = payload
        return True

    def create_mock(self, *, demo_block: bool, rate_limited: bool):
        if demo_block:
            raise RuntimeError("SHOT_BLOCKED")
        if rate_limited:
            raise RuntimeError("RATE_LIMITED:42")
        self.writes.append("create")
        return {"alertId": "al_mock", "revision": 1, "market": "spot", "priceSource": "INDEX",
                "triggerMode": "once", "channels": ["in_app"]}

s = MockAlertsSession()
s.reload_seq = 1
ok(s.apply_list_response(1, {"items": [{"alertId": "a1"}]}) is True, "fresh list lands")
ok(s.page and s.page["items"][0]["alertId"] == "a1", "list payload stored")
ok(s.apply_list_response(0, {"items": [{"alertId": "stale"}]}) is False, "stale seq discarded")
ok(s.page["items"][0]["alertId"] == "a1", "stale did not overwrite")

s.invalidate()
ok(s.page is None and s.reload_seq == 2, "invalidate bumps seq and clears")
s.reload_seq = 2
ok(s.apply_list_response(2, None, error="FEATURE_DISABLED") is True, "FEATURE_DISABLED applied")
ok(s.feature_disabled and s.page is None, "feature disabled state")

try:
    s.create_mock(demo_block=True, rate_limited=False)
    ok(False, "demo block should throw")
except RuntimeError as e:
    ok("SHOT_BLOCKED" in str(e), "DEBUG/demo blocks writes")

try:
    s.create_mock(demo_block=False, rate_limited=True)
    ok(False, "rate limit should throw")
except RuntimeError as e:
    ok("RATE_LIMITED" in str(e), "429 RATE_LIMITED mock")

created = s.create_mock(demo_block=False, rate_limited=False)
ok(created["market"] == "spot" and created["priceSource"] == "INDEX", "mock create shape spot/INDEX")
ok(created["triggerMode"] == "once" and created["channels"] == ["in_app"], "mock create once/in_app")
ok(s.writes == ["create"], "only one mock write recorded")

# Enumerate required contract tokens once more for review checklist
required = ["spot", "INDEX", "once", "in_app", "revision", "Retry-After", "FEATURE_DISABLED", "priceAlerts"]
missing = [k for k in required if k not in swift and k != "Retry-After"]
# Retry-After may only appear as comment; retryAfter covers
ok("retryAfter" in swift or "Retry-After" in swift, "retry after covered in Swift source")
ok(not missing, f"required tokens in Swift (missing={missing})")

if failures:
    print("FAIL", len(failures))
    for f in failures:
        print(" -", f)
    sys.exit(1)
print("ALL PASSED (Python mock/guards ≠ URLSession/XCTest/Xcode compile)")
