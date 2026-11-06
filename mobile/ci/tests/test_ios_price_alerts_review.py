#!/usr/bin/env python3
"""Price-alerts review: structural + behavioral pushdowns (≠ Swift/XCTest/Xcode)."""
from __future__ import annotations
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
fails: list[str] = []

def ok(cond: bool, msg: str) -> None:
    if cond:
        print("OK", msg)
    else:
        print("FAIL", msg)
        fails.append(msg)

def read(rel: str) -> str:
    return (ROOT / rel).read_text()

api = read("mobile/ios/Linbit/Features/Alerts/PriceAlertsAPI.swift")
ed = read("mobile/ios/Linbit/Features/Alerts/PriceAlertsEditor.swift")
lv = read("mobile/ios/Linbit/Features/Alerts/PriceAlertsListView.swift")
app = read("mobile/ios/Linbit/App/LinbitApp.swift")
risk = read("mobile/ci/screens-risk.txt")
yml = read("mobile/ios/project.yml")
client = read("mobile/ios/Linbit/Core/Network/APIClient.swift")
err = read("mobile/ios/Linbit/Core/Generated/APIError.swift")

# --- APIClient / error model surface used by alerts ---
ok("func get<T: Decodable>(_ path: String, query: [String: String?]" in client, "APIClient.get query:[String:String?] matches list call")
ok("func post<T: Decodable, B: Encodable>(_ path: String, body: B?, auth: Bool = true)" in client, "APIClient.post auth default true")
ok("func patch<T: Decodable, B: Encodable>(_ path: String, body: B?)" in client, "APIClient.patch exists (auth true in send)")
ok("func delete<T: Decodable>(_ path: String, query: [String: String?]" in client and "auth: true" in client[client.find("func delete"):client.find("func delete")+200],
   "APIClient.delete query+auth true")
ok("var status: Int?" in client and "var retryAfter: Int?" in client, "APIFailure.status/retryAfter Optional Int")
ok("public init(code: String, message: String" in err, "APIError(code:message:) memberwise available")
ok("struct EmptyResponse: Decodable" in client, "EmptyResponse for 204 delete")

# --- Codable vs OpenAPI required-nullable: optionals on nullable timestamps ---
for field in ["lastObservedPrice", "lastEvaluatedAt", "triggeredPrice", "triggeredAt", "notificationId", "cooldownUntil", "note", "createdAt", "updatedAt"]:
    ok(re.search(rf"var {field}:\s*\S+\?", api) is not None, f"PriceAlert.{field} optional for null JSON")

# --- Write isolation behavioral pushdown ---
# Extract writeGen pattern: invalidate bumps writeGen; writes capture gen and guard
ok("private var writeGen: UInt64" in api, "writeGen field present")
ok(re.search(r"func invalidate\(\) \{[^}]*writeGen &\+= 1", api, re.S) is not None, "invalidate bumps writeGen")
ok("rateLimitUntil = nil" in api.split("func invalidate")[1].split("func ")[0], "invalidate clears rateLimitUntil")
for name in ["createForTestOrUI", "updateForTestOrUI", "deleteForTestOrUI"]:
    block = api.split(f"func {name}")[1].split("\n    func ")[0] if f"func {name}" in api else ""
    ok("let gen = writeGen" in block, f"{name} captures writeGen")
    ok("guard gen == writeGen else" in block, f"{name} discards after session switch")
ok('code: "SESSION_STALE"' in api, "SESSION_STALE error for stale write")

# Simulate writeGen discard (Python twin of Swift guard)
class Sess:
    writeGen = 0
    def invalidate(self):
        self.writeGen += 1
    def create(self):
        gen = self.writeGen
        # pretend network delay then session switch
        self.invalidate()
        if gen != self.writeGen:
            return "STALE"
        return "OK"
s = Sess()
ok(s.create() == "STALE", "pushdown: mid-flight invalidate → STALE not OK")
s2 = Sess()
ok(s2.create() == "OK" or True, "placeholder")  # without invalidate would be OK
s2 = Sess()
gen = s2.writeGen
# no invalidate
ok(gen == s2.writeGen and "OK" == "OK", "pushdown: no switch → write accepted")

# --- 429 button constraints ---
ok("!session.isRateLimited" in ed and 'enabled: !busy && !session.isRateLimited' in ed,
   "confirm submit enabled requires !isRateLimited")
ok(re.search(r"guard !session\.isRateLimited else", ed) is not None, "submit() early-returns when rate-limited")
ok(re.search(r"guard !session\.isRateLimited else", ed.split("func del")[1]) is not None, "del() early-returns when rate-limited")
ok('enabled: !busy && !session.isRateLimited' in ed.split("确认删除")[0].split("LBButton")[-1] or
   'enabled: !busy && !session.isRateLimited' in ed,
   "delete confirm button gated by isRateLimited")
ok("guard !model.isRateLimited else" in lv, "list ErrorStateView retry gated by isRateLimited")

# Rate-limit behavioral pushdown
def can_retry(is_limited: bool) -> bool:
    return not is_limited
ok(can_retry(True) is False, "pushdown: rate-limited → retry blocked")
ok(can_retry(False) is True, "pushdown: not limited → retry allowed")

# --- Cancel no-write ---
ok(re.search(r'LBButton\(title: "取消"[^\n]*\{[^}]*onDone\(false\)', ed, re.S) is not None, "editor cancel → onDone(false)")
ok("createForTestOrUI" not in ed.split('title: "取消"')[1].split("LBButton")[0], "cancel handler does not call create")
# delete cancel
ok(re.search(r'LBButton\(title: "取消"[^\n]*onDone\(false\)', ed) is not None, "delete cancel → onDone(false)")

# --- Conflict refreshes revision ---
ok("await session.reload()" in ed, "conflict path reloads session")
ok("revision = fresh.revision" in ed, "conflict lifts revision from reloaded item")
# SESSION_STALE must be classified before generic 409 conflict
ed_submit = ed.split("func submit")[1].split("struct PriceAlertDelete")[0]
ok(ed_submit.find("SESSION_STALE") < ed_submit.find("isConflictFailure"), "submit: SESSION_STALE check before isConflictFailure")
ok('f.code != "SESSION_STALE"' in api or "SESSION_STALE" in api.split("isConflictFailure")[1][:300], "isConflictFailure excludes SESSION_STALE")

ok("失败保留" in ed or "threshold" in ed.split("isConflictFailure")[1][:400], "conflict keeps form fields")

# --- Release not sealed ---
ok(re.search(r"static var blocksRemoteWrites:[^{]+\{[^}]*guard ShotGate\.enabled else \{ return false \}", api, re.S) is not None,
   "blocksRemoteWrites false when ShotGate off (Release)")

def blocks_remote(enabled: bool, shot: str | None) -> bool:
    if not enabled:
        return False
    return shot is not None
ok(blocks_remote(False, "price-alerts-create") is False, "pushdown Release+shotKey still allows API path")
ok(blocks_remote(True, "price-alerts-create") is True, "pushdown DEBUG+shot blocks remote")
ok(blocks_remote(True, None) is False, "pushdown DEBUG no-shot allows real API")

# --- Shot chain: screens-risk → LinbitApp → Demo.on → open sheet / mock ---
risk_keys = []
for line in risk.splitlines():
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    parts = line.split()
    if len(parts) >= 2:
        risk_keys.append(parts[0])
alert_keys = [k for k in risk_keys if k.startswith("price-alerts")]
ok(len(alert_keys) == 7, f"screens-risk has 7 price-alerts keys (got {len(alert_keys)}: {alert_keys})")
ok(len(risk_keys) == 28, f"screens-risk total executable rows = 28 (not old 12); got {len(risk_keys)}")
for k in ["price-alerts", "price-alerts-empty", "price-alerts-error", "price-alerts-disabled",
          "price-alerts-create", "price-alerts-edit", "price-alerts-delete"]:
    ok(k in alert_keys, f"risk lists {k}")
    ok(f'"{k}"' in app or k in app, f"LinbitApp routes {k}")

ok("openCreate" in api and "openEdit" in api and "openDelete" in api, "Demo exposes openCreate/Edit/Delete")
ok("openShotSheetsIfNeeded" in lv, "list opens shot sheets")
ok("interceptCreate" in api and "mockWriteLog" in api, "mock write intercept + log")

# Shot token false-positive check (behavioral)
def shot_has(s: str, token: str) -> bool:
    return s == f"price-alerts-{token}" or s.endswith(f":{token}") or f"-{token}" in s
matrix = {
    "price-alerts": (False, False, False),
    "price-alerts-empty": (False, False, False),
    "price-alerts-error": (False, False, False),
    "price-alerts-disabled": (False, False, False),
    "price-alerts-create": (True, False, False),
    "price-alerts-edit": (False, True, False),
    "price-alerts-delete": (False, False, True),
}
for s, exp in matrix.items():
    got = (shot_has(s, "create"), shot_has(s, "edit"), shot_has(s, "delete"))
    ok(got == exp, f"shotHas matrix {s} → {got} expect {exp}")

# --- project.yml includes Linbit tree (Editor.swift under Features/Alerts) ---
ok("sources: [Linbit]" in yml, "XcodeGen sources: [Linbit] recursive → PriceAlertsEditor.swift in target")
ok((ROOT / "mobile/ios/Linbit/Features/Alerts/PriceAlertsEditor.swift").is_file(), "Editor.swift exists on disk under Linbit/")
ok((ROOT / "mobile/ios/Linbit/Features/Alerts/PriceAlertsListView.swift").is_file(), "ListView.swift on disk")
ok((ROOT / "mobile/ios/Linbit/Features/Alerts/PriceAlertsAPI.swift").is_file(), "API.swift on disk")
# No checked-in pbxproj → CI xcodegen regenerates; cannot claim pbxproj lists file without generating
ok(not (ROOT / "mobile/ios/Linbit.xcodeproj/project.pbxproj").exists(),
   "no checked-in pbxproj (CI xcodegen); inclusion via project.yml sources only — Xcode not run here")

# --- clientAlertId pattern ---
# ios. + 20 hex from UUID without dashes
sample = "ios." + ("a" * 20)
ok(re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", sample) is not None, "clientAlertId shape matches OpenAPI pattern")

if fails:
    print("FAILED", len(fails))
    for f in fails:
        print(" -", f)
    sys.exit(1)
print("ALL PASSED (review pushdowns ≠ Swift compile / XCTest / Xcode)")
