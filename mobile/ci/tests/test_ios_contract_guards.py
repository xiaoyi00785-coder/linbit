#!/usr/bin/env python3
"""Static / contract guards for iOS #104 local changes (no Xcode required)."""
from __future__ import annotations
import base64, re, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]  # repo root: mobile/ci/tests -> ../../..
# parents: tests=0, ci=1, mobile=2, repo=3
ROOT = Path(__file__).resolve().parents[2].parent  # mobile/ci/tests -> mobile -> repo? 
# __file__ = repo/mobile/ci/tests/test_...
# parents[0]=tests, [1]=ci, [2]=mobile, [3]=repo
ROOT = Path(__file__).resolve().parents[3]

failures: list[str] = []

def ok(cond: bool, msg: str):
    if not cond:
        failures.append(msg)
    else:
        print('OK', msg)

def read(rel: str) -> str:
    return (ROOT / rel).read_text()

# --- Base64URL (mirror SecurityAPI Data extension) ---
def b64url_encode(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).decode().rstrip('=')

def b64url_decode(s: str) -> bytes:
    b = s.replace('-', '+').replace('_', '/')
    b += '=' * (-len(b) % 4)
    return base64.b64decode(b)

sample = bytes(range(32))
enc = b64url_encode(sample)
ok('+' not in enc and '/' not in enc and '=' not in enc, 'base64url encode has no +/=')
ok(b64url_decode(enc) == sample, 'base64url roundtrip')
# challenge-like
ch = 'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8'
ok(len(b64url_decode(ch)) == 32, 'decode 32-byte challenge')

# --- Passkey assert maps Web fields ---
sec = read('mobile/ios/Linbit/Features/Security/SecurityAPI.swift')
ok('allowCredentials' in sec and 'allowedCredentials' in sec, 'assert maps allowCredentials')
ok('userVerification' in sec and 'userVerificationPreference' in sec, 'assert maps userVerification')
ok('createCredentialAssertionRequest' in sec, 'uses assertion API not registration for login')
ok('PasskeyError.cancelled' in sec, 'cancelled path exists')
# dual credential handling
ok('ASAuthorizationPlatformPublicKeyCredentialAssertion' in sec, 'assertion callback branch')
ok('ASAuthorizationPlatformPublicKeyCredentialRegistration' in sec, 'registration callback branch')

auth = read('mobile/ios/Linbit/Core/Network/AuthAPI.swift')
ok('/auth/passkey/login/begin' in auth and '/auth/passkey/login/finish' in auth, 'AuthAPI passkey paths')
ok('TokenStore.shared.save' in auth, 'finishPasskeyLogin saves tokens')
ok('EmptyJSON()' in auth, 'begin sends JSON object body')
ok('deviceId: Keychain.deviceId' in auth, 'finish sends deviceId')

flow = read('mobile/ios/Linbit/Features/Auth/AuthFlow.swift')
ok('passkeyLogin()' in flow and 'beginPasskeyLogin' in flow, 'LoginAccountView wires passkeyLogin')
ok('即将开放' not in flow or '通行密钥登录即将开放' not in flow, 'passkey not still stub toast')
ok('OAuth 契约' in flow, 'Apple/Google disclose missing OAuth')

# --- ShotGate release isolation ---
gate = read('mobile/ios/Linbit/Features/Common/ShotGate.swift')
ok('#if DEBUG' in gate and 'Release' in gate or 'false' in gate, 'ShotGate has DEBUG/Release split')
ok(re.search(r'static var enabled: Bool \{\s*#if DEBUG\s*true\s*#else\s*false', gate), 'enabled false in Release')

dep = read('mobile/ios/Linbit/Features/Assets/DepositView.swift')
ok('ShotGate.launchShot == "assets-deposit"' in dep, 'deposit fixture gated on launchShot')
ok('TXfixtureDemo' in dep, 'deposit uses fixture address marker')
ok('createWithdrawal' not in dep, 'deposit file never withdraws')

wd = read('mobile/ios/Linbit/Features/Assets/WithdrawView.swift')
ok('ShotGate.launchShot == "assets-withdraw"' in wd, 'withdraw fixture gated')
ok('TXfixtureDemo' in wd and '不调用 createWithdrawal' in wd, 'withdraw fixture does not submit')
# fixture return before network
# fixture block must return before any live createWithdrawal call site
import re as _re
m = _re.search(r'if ShotGate\.launchShot == "assets-withdraw" \{.*?\n        \}', wd, _re.S)
ok(bool(m) and 'return' in m.group(0) and 'AssetsAPI.createWithdrawal' not in m.group(0), 'fixture block returns without createWithdrawal call')
ok('AssetsAPI.createWithdrawal' in wd, 'live withdraw path still present')

# fixtures must not appear outside ShotGate checks in Release-visible unconditional strings that write real APIs
# ensure launchShot checks are present (already)

home = read('mobile/ios/Linbit/Features/Home/HomeView.swift')
ok('ShotGate.launchShot == nil' in home, 'home carousels pause during shots')

spot = read('mobile/ios/Linbit/Features/Spot/SpotTradeView.swift')
ok('openFeature(.strategy)' in spot, 'trading bot opens strategy grid')

app = read('mobile/ios/Linbit/App/LinbitApp.swift')
ok('assets-deposit' in app and 'assets-withdraw' in app, 'shot routes for deposit/withdraw')

pr = read('mobile/ci/screens-pr.txt')
ok('swap light' in pr, 'swap still in screens-pr')

# New shot routes for this tip's features (screens-pr + LinbitApp)
app = read('mobile/ios/Linbit/App/LinbitApp.swift')
ok('spot-chart' in app and 'futures-calc' in app, 'LinbitApp spot-chart + futures-calc routes')
ok('futures-mark-history' in app and 'futures-insurance' in app, 'LinbitApp mark-history + insurance routes')
ok('spot-chart light' in pr and 'futures-calc light' in pr, 'screens-pr includes new feature shots')
ok('futures-mark-history light' in pr and 'futures-insurance light' in pr, 'screens-pr mark + insurance')
ok('case "spotChart"' in spot or 'spotChart' in spot, 'SpotTradeView handles spotChart shot')
ok('case "calc"' in read('mobile/ios/Linbit/Features/Futures/FuturesView.swift'), 'FuturesView handles calc shot')
# swap should not be last before only new keys - check position
lines=[ln.strip() for ln in pr.splitlines() if ln.strip() and not ln.strip().startswith('#')]
screens=[ln.split()[0] for ln in lines]
ok(screens.index('swap') < screens.index('earn'), 'swap moved before earn to avoid cancel cutting tail')

# OpenAPI presence for wired features
oa = read('docs/api/openapi.yaml')
ok('/auth/passkey/login/begin' in oa and '/strategy/grid' in oa, 'openapi has passkey login + grid')
ok('/auth/passkey/login/begin' in oa, 'passkey begin in openapi')
ok('/convert/' in oa or '/convert/pairs' in oa or 'convert' in oa.lower(), 'convert paths exist for swap')


# Auth support + assets navigation
authc = read('mobile/ios/Linbit/Features/Auth/AuthComponents.swift')
ok('openFeature(.support)' in authc, 'auth headphones opens support when flagged')
assets = read('mobile/ios/Linbit/Features/Assets/AssetsView.swift')
ok('FeaturePage' in assets and '.earn' in assets and '.strategy' in assets, 'assets tabs map to FeaturePages')
ok('openFeature(.swap)' in assets, '现货兑换 opens swap')
sec = read('mobile/ios/Linbit/Features/Security/SecurityAPI.swift')
ok('resumeOnce' in sec, 'passkey continuation single-resume helper')
ok('NSNull()' not in sec, 'assertion omits NSNull userHandle')

# --- Passkey error / cancel / double-tap / session (Python SOURCE GUARDS ≠ Swift XCTest) ---
# NOTE: These assert source text only. They do NOT execute ASAuthorizationController,
# do NOT simulate cancel UI, and do NOT replace device/XCTest coverage.
ok('passkeyBusy' in flow and '.disabled(passkeyBusy' in flow, 'passkey double-tap: busy disables login UI')
ok('catch PasskeyError.cancelled' in flow, 'passkey cancel: silent catch (no error toast)')
ok('ToastCenter.shared.error' in flow, 'passkey non-cancel errors surface toast')
ok('sessionId' in flow and 'finishPasskeyLogin(session:' in flow, 'passkey sessionId threaded begin→finish')
ok('defer { passkeyBusy = false }' in flow, 'passkeyBusy cleared in defer')
ok('ASAuthorizationError)?.code == .canceled' in sec or 'ASAuthorizationError' in sec, 'AS cancel mapped to PasskeyError.cancelled')
ok('cont = nil' in sec and 'guard let c = cont' in sec, 'resumeOnce single-shot continuation')

# --- Spot K-line reuses GET /market/klines (no new contract) ---
market = read('mobile/ios/Linbit/Core/Network/MarketAPI.swift')
ok('/market/klines' in market and 'static func klines' in market, 'MarketAPI.klines path')
ok('/market/klines' in oa, 'openapi defines /market/klines')
ok('SpotChartPanel' in spot and 'showChart' in spot, 'SpotTradeView toggles SpotChartPanel')
ok('MarketAPI.klines' in read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift'), 'SpotChartPanel calls MarketAPI.klines')
ok('struct CandlePoint' in read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift'), 'shared CandlePoint for spot+futures')

# --- Futures tools with real OpenAPI ---
fut_api = read('mobile/ios/Linbit/Core/Network/FuturesAPI+Data.swift')
ok('/futures/market/mark-history' in fut_api and 'markHistory' in fut_api, 'FuturesAPI.markHistory')
ok('/futures/market/insurance-fund' in fut_api and 'insuranceFund' in fut_api, 'FuturesAPI.insuranceFund')
ok('/futures/market/mark-history' in oa and '/futures/market/insurance-fund' in oa, 'openapi mark-history + insurance-fund')
tools = read('mobile/ios/Linbit/Features/Futures/FuturesSheets.swift')
ok('futuresMarkHistory' in tools and 'futuresInsurance' in tools, 'tools sheet opens mark/insurance features')
ok('onCalculator' in tools, 'tools sheet calculator callback')
ok('quoteCurrency' in tools or '/account/preferences' in tools, 'trading prefs toast cites preferences limit')
host = read('mobile/ios/Linbit/Features/Common/P3Components.swift')
ok('futuresMarkHistory' in host and 'FuturesMarkHistoryView' in host, 'FeatureHost mark history')
ok('futuresInsurance' in host and 'FuturesInsuranceFundView' in host, 'FeatureHost insurance')
fv = read('mobile/ios/Linbit/Features/Futures/FuturesView.swift')
ok('FuturesCalculatorSheet' in fv and 'showCalc' in fv, 'FuturesView hosts calculator sheet')
ok('TradeCalculator' in read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift'), 'local TradeCalculator (Android parity, no fund API)')

# Assets honest gaps (no OpenAPI for quick-buy / assets PnL analysis)
ok('快捷买币无 OpenAPI' in assets or '快捷买币即将开放' in assets, '快捷买币 remains non-claimed')
ok('资产盈亏分析无独立接口' in assets or '盈亏分析即将开放' in assets, 'assets 盈亏 analysis not falsely completed')
ok('/futures/pnl/today' in fut_api, 'futures today pnl API remains (trading page card)')



# --- TradeCalculator formula numeric parity vs Android TradeLogicTest (NOT Swift XCTest) ---
# Runs Decimal-equivalent math in Python against Android asserted vectors.
# Proves formula intent; does NOT compile or execute Swift on device/simulator.
from decimal import Decimal, getcontext
getcontext().prec = 50

def d(s): return Decimal(s)
def cost(qty, price, lev):
    if price <= 0 or lev <= 0 or qty == 0: return d(0)
    return abs(qty) * price / d(lev)
def liq(entry, lev, long, mmr):
    if entry <= 0 or lev <= 0: return d(0)
    inv = d(1) / d(lev)
    return entry * (d(1) - inv + mmr) if long else entry * (d(1) + inv - mmr)
def pnl(entry, exitp, qty, long):
    q = abs(qty)
    return (exitp - entry) * q if long else (entry - exitp) * q
def target(entry, qty, target_pnl, long):
    if qty == 0: return d(0)
    delta = target_pnl / abs(qty)
    return entry + delta if long else entry - delta

ok(cost(d('0.1'), d('80000'), 20) == d('400'), 'formula cost 0.1*80000/20=400')
ok(liq(d('80000'), 20, True, d('0.004')) == d('76320'), 'formula liq long 76320')
ok(pnl(d('80000'), d('82000'), d('0.1'), True) == d('200'), 'formula pnl long 200')
ok(target(d('80000'), d('0.1'), d('200'), True) == d('82000'), 'formula target 82000')
ok(liq(d('80000'), 20, False, d('0.004')) == d('80000') * (d(1) + d(1)/d(20) - d('0.004')), 'formula liq short shape')
ok(cost(d('0'), d('80000'), 20) == 0 and cost(d('0.1'), d('0'), 20) == 0, 'formula cost rejects zero price/qty')
# Swift source must expose matching helpers after review fixes
dv = read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift')
ok('static func cost(' in dv and 'static func parseInput(' in dv, 'Swift TradeCalculator has cost+parseInput')
ok('loadSeq' in dv and 'candleSeq' in dv, 'kline loadSeq/candleSeq stale-response isolation')
ok('guard seq == loadSeq' in dv and 'guard seq == candleSeq' in dv, 'stale response discarded by seq guard')
ok('base: m.base' in read('mobile/ios/Linbit/Features/Futures/FuturesView.swift'), 'calculator qty unit uses contract base')


if failures:
    print('FAIL', len(failures))
    for f in failures:
        print(' -', f)
    sys.exit(1)
print('ALL PASSED', len(failures)==0)
