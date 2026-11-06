#!/usr/bin/env python3
"""Reproducible LOCAL simulations of Passkey/Kline/Withdraw/Calculator/Newcomer runtime risks.

IMPORTANT: This is Python state-machine + Decimal math. It is NOT Swift XCTest,
NOT ASAuthorizationController, NOT URLSession, NOT Xcode compile.
Fund / reward claim paths are MOCKED — no real createWithdrawal / enroll / claim network calls.
"""
from __future__ import annotations
from decimal import Decimal, getcontext
import sys
from pathlib import Path

getcontext().prec = 50
ROOT = Path(__file__).resolve().parents[3]
failures: list[str] = []

def ok(cond: bool, msg: str):
    if not cond: failures.append(msg)
    else: print('OK', msg)

def read(rel: str) -> str:
    return (ROOT / rel).read_text()

# ---------- 1) K-line / market loadSeq stale discard ----------
class ChartLoader:
    def __init__(self):
        self.seq = 0
        self.symbol = 'BTCUSDT'
        self.interval = '15m'
        self.candles = None

    def start(self, symbol=None, interval=None):
        if symbol: self.symbol = symbol
        if interval: self.interval = interval
        self.seq += 1
        seq, sym, iv = self.seq, self.symbol, self.interval
        self.candles = ('loading', None)
        return seq, sym, iv

    def apply(self, seq, sym, iv, result):
        if seq != self.seq or sym != self.symbol or iv != self.interval:
            return False
        self.candles = result
        return True

loader = ChartLoader()
s1, a1, i1 = loader.start('BTCUSDT', '15m')
s2, a2, i2 = loader.start('ETHUSDT', '1h')
ok(loader.apply(s1, a1, i1, ('loaded', ['btc'])) is False, 'stale BTC response discarded after ETH switch')
ok(loader.apply(s2, a2, i2, ('loaded', ['eth'])) is True, 'fresh ETH response applied')
ok(loader.candles == ('loaded', ['eth']), 'UI holds ETH candles only')

# ---------- 2) Passkey resumeOnce + concurrent cancel-prev ----------
class PasskeyCont:
    def __init__(self):
        self.cont = None
        self.resumed = []

    def resume_once(self, result):
        if self.cont is None:
            return False
        c = self.cont
        self.cont = None
        self.resumed.append(result)
        return True

    def begin(self, label):
        if self.cont is not None:
            prev = self.cont
            self.cont = None
            self.resumed.append(('cancelled', prev))
        self.cont = label
        return label

pk = PasskeyCont()
pk.begin('A')
pk.begin('B')
ok(any(r[0] == 'cancelled' and r[1] == 'A' for r in pk.resumed), 'concurrent passkey cancels previous continuation')
ok(pk.resume_once(('ok', 'B')) is True, 'B resumes once')
ok(pk.resume_once(('ok', 'B2')) is False, 'double resume ignored (resumeOnce)')
ok(pk.cont is None, 'cont cleared after resume')

flow = read('mobile/ios/Linbit/Features/Auth/AuthFlow.swift')
ok('catch PasskeyError.cancelled' in flow and 'passkeyBusy' in flow, 'Swift: cancel silent + busy disable (source)')
sec = read('mobile/ios/Linbit/Features/Security/SecurityAPI.swift')
ok('if let prev = cont' in sec and 'PasskeyError.cancelled' in sec, 'Swift: overlapping assert/register cancels prev cont (source)')

# ---------- 3) Withdraw reloadSeq + submitting gate (NO real fund API) ----------
class WithdrawForm:
    def __init__(self):
        self.asset = 'USDT'
        self.reload_seq = 0
        self.networks = []
        self.submitting = False
        self.submit_count = 0

    def reload(self, asset):
        self.asset = asset
        self.reload_seq += 1
        return self.reload_seq, asset

    def apply_networks(self, seq, asset, nets):
        if seq != self.reload_seq or asset != self.asset:
            return False
        self.networks = nets
        return True

    def submit(self):
        if self.submitting:
            return False
        self.submitting = True
        try:
            self.submit_count += 1
            # would call createWithdrawal — NOT invoked in this test
            return True
        finally:
            self.submitting = False

w = WithdrawForm()
r1 = w.reload('USDT')
r2 = w.reload('BTC')
ok(w.apply_networks(*r1, ['TRC20']) is False, 'stale USDT networks discarded after BTC switch')
ok(w.apply_networks(*r2, ['BTC']) is True, 'BTC networks applied')
ok(w.submit() and w.submit() and w.submit_count == 2, 'submit serializes via submitting flag model (2 sequential OK)')
w.submitting = True
ok(w.submit() is False, 'overlapping submit rejected while submitting')
w.submitting = False

wd = read('mobile/ios/Linbit/Features/Assets/WithdrawView.swift')
ok('reloadSeq' in wd and 'createWithdrawal' in wd, 'Swift withdraw has reloadSeq + live createWithdrawal path')
ok('shotForceTotp' in wd and 'useTotp = true' not in wd, 'fixture does not assign get-only useTotp')
ok('guard !submitting else' in wd, 'Swift withdraw has explicit anti-double guard')

# ---------- 4) Calculator formula + invalid inputs ----------
def d(s): return Decimal(s)
def cost(qty, price, lev):
    if price <= 0 or lev <= 0 or qty == 0: return d(0)
    return abs(qty) * price / d(lev)
def liq(entry, lev, long, mmr):
    if entry <= 0 or lev <= 0: return d(0)
    inv = d(1)/d(lev)
    return entry*(d(1)-inv+mmr) if long else entry*(d(1)+inv-mmr)
def pnl(entry, exitp, qty, long):
    q=abs(qty)
    return (exitp-entry)*q if long else (entry-exitp)*q
def target(entry, qty, tp, long):
    if qty == 0: return d(0)
    delta = tp/abs(qty)
    return entry+delta if long else entry-delta
def parse_input(s):
    cleaned = s.replace(',', '')
    try:
        return Decimal(cleaned)
    except Exception:
        return d(0)

ok(cost(d('0.1'), d('80000'), 20) == d('400'), 'calc cost')
ok(liq(d('80000'), 20, True, d('0.004')) == d('76320'), 'calc liq long')
ok(liq(d('80000'), 20, False, d('0.004')) == d('80000')*(d(1)+d(1)/d(20)-d('0.004')), 'calc liq short')
ok(pnl(d('80000'), d('82000'), d('0.1'), True) == d('200'), 'calc pnl long')
ok(pnl(d('80000'), d('78000'), d('0.1'), False) == d('200'), 'calc pnl short')
ok(target(d('80000'), d('0.1'), d('200'), True) == d('82000'), 'calc target long')
ok(parse_input('81,133.25') == d('81133.25'), 'parse strips grouping commas')
ok(cost(d(0), d('80000'), 20) == 0 and cost(d('1'), d(0), 20) == 0, 'calc rejects zero qty/price')
ok(liq(d(0), 20, True, d('0.004')) == 0, 'calc liq rejects zero entry')

dv = read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift')
ok('candles = .loading' in dv and 'loadSeq' in dv, 'Swift kline forces loading (no stale chart flash)')
ok('base: String' in dv and 'parseInput' in dv, 'Swift calculator has base unit + parseInput')

# ---------- 5) Newcomer enroll/claim MOCK (no network) ----------
class MockNewcomerAPI:
    def __init__(self):
        self.enroll_calls = 0
        self.claim_calls = []
        self.fail_next = None

    async_not = True  # marker: sync mock

    def enroll(self):
        self.enroll_calls += 1
        if self.fail_next == 'enroll':
            self.fail_next = None
            raise RuntimeError('network')
        return True

    def claim(self, key):
        self.claim_calls.append(key)
        if self.fail_next == 'claim':
            self.fail_next = None
            raise RuntimeError('network')
        return {'amount': '10', 'title': '体验金'}

class NewcomerUI:
    def __init__(self, api: MockNewcomerAPI):
        self.api = api
        self.action_busy = False
        self.logged_in = True
        self.toasts = []
        # OpenAPI enum: locked|in_progress|claimable|claimed|expired（无 completed/done）
        self.tasks = [
            {'key': 't1', 'status': 'claimed'},
            {'key': 't2', 'status': 'claimable'},
            {'key': 't3', 'status': 'in_progress'},
            {'key': 't4', 'status': 'locked'},
            {'key': 't5', 'status': 'expired'},
        ]

    def claimable_key(self):
        for t in self.tasks:
            if t['status'] == 'claimable' and t.get('key'):
                return t['key']
        return None

    def run(self, cta):
        if self.action_busy:
            return 'busy'
        if cta == 'claim':
            if not self.logged_in:
                self.toasts.append('login'); return 'login'
            key = self.claimable_key()
            if not key:
                self.toasts.append('暂无可领取任务'); return 'none'
            self.action_busy = True
            try:
                if not self.logged_in:
                    self.toasts.append('login'); return 'login'
                r = self.api.claim(key)
                self.toasts.append(f"已领取 {r['amount']}")
                return 'ok'
            except Exception as e:
                self.toasts.append(f'err:{e}'); return 'err'
            finally:
                self.action_busy = False
        if cta == 'enroll':
            if not self.logged_in:
                self.toasts.append('login'); return 'login'
            self.action_busy = True
            try:
                self.api.enroll()
                self.toasts.append('报名成功')
                return 'ok'
            except Exception as e:
                self.toasts.append(f'err:{e}'); return 'err'
            finally:
                self.action_busy = False
        return 'noop'

api = MockNewcomerAPI()
ui = NewcomerUI(api)
ok(ui.claimable_key() == 't2', 'mock picks claimable not claimed/in_progress/locked/expired')
ok(ui.run('claim') == 'ok' and api.claim_calls == ['t2'], 'mock claim once on claimable key')
ui.action_busy = True
ok(ui.run('claim') == 'busy' and api.claim_calls == ['t2'], 'actionBusy blocks second claim (no double reward)')
ui.action_busy = False
ui.logged_in = False
ok(ui.run('enroll') == 'login' and api.enroll_calls == 0, 'logged-out enroll does not call API')
ui.logged_in = True
ok(ui.run('enroll') == 'ok' and api.enroll_calls == 1, 'mock enroll once')
ui.tasks = [{'key': 'x', 'status': 'in_progress'}, {'key': 'y', 'status': 'claimed'}, {'key': 'z', 'status': 'locked'}, {'key': 'w', 'status': 'expired'}]
ok(ui.claimable_key() is None and ui.run('claim') == 'none', 'non-claimable statuses rejected')
# session switch mid-busy
ui.tasks = [{'key': 's1', 'status': 'claimable'}]
ui.action_busy = False
ui.logged_in = True
ui.action_busy = True  # simulate in-flight
ui.logged_in = False
ok(ui.run('claim') == 'busy', 'busy blocks even after logout race')
ui.action_busy = False
ok(ui.run('claim') == 'login' and 's1' not in api.claim_calls, 'logged-out after busy clear does not claim')
ui.logged_in = True
api.fail_next = 'claim'
ui.tasks = [{'key': 'z', 'status': 'claimable'}]
ok(ui.run('claim') == 'err' and any(t.startswith('err:') for t in ui.toasts), 'claim error surfaces toast (mock)')

nv = read('mobile/ios/Linbit/Features/Home/NewcomerView.swift')
api_src = read('mobile/ios/Linbit/Core/Network/Phase3API.swift')
ok('enrollNewcomer' in api_src and 'claimNewcomerTask' in api_src, 'HomeAPI enroll/claim present')
ok('enrollNewcomer' in nv and 'claimNewcomerTask' in nv, 'NewcomerView calls enroll/claim')
ok('奖励领取即将开放' not in nv, 'false claim toast removed')
ok('actionBusy' in nv and 'guard !actionBusy' in nv, 'NewcomerView actionBusy anti-double')
ok('$0.status == "claimable"' in nv or 'status == "claimable"' in nv, 'claim status filter claimable-only')
ok('completed' not in nv.split('case "claim"')[1].split('default:')[0], 'claim block does not treat completed as claimable')
ok('locked|in_progress|claimable|claimed|expired' in nv, 'OpenAPI status enum documented in comment')
ok('session.isLoggedIn' in nv, 'session guard on reward actions')

# ---------- 6) Futures beginner guide: static source mapping ----------
guide = read('mobile/ios/Linbit/Features/Futures/FuturesBeginnerGuideView.swift')
pagedata = read('apps/web/legacy/pagedata.ts')
ok('apps/web/legacy/pagedata.ts' in guide, 'guide documents pagedata source')
ok('仓位档位' in pagedata and '标记价格' in guide, 'guide mirrors mark-price topic')
ok('fundingIntervalHours' in guide and '不保证所有品种均为固定 8 小时' in guide, 'funding interval not absolutized')
ok('合约指南' in guide and '不是' in guide.split('合约指南')[0]+guide.split('合约指南')[1][:80], 'page labeled 合约指南 not full academy')
ok('FeatureShell(title: "合约指南")' in guide, 'shell title 合约指南')
ok('全部保证金损失' in guide and '全部保证金损失' in pagedata, 'risk disclaimer from 服务条款')
ok('futuresGuide' in read('mobile/ios/Linbit/Features/Common/P3Components.swift'), 'FeaturePage.futuresGuide')
ok('Router.shared.openFeature(.futuresGuide)' in read('mobile/ios/Linbit/Features/Futures/FuturesSheets.swift'), 'tools 新手教学 wired')
ok('case "futures-guide"' in read('mobile/ios/Linbit/App/LinbitApp.swift'), 'shot route futures-guide')
ok('futures-guide' in read('mobile/ci/screens-pr.txt'), 'screens-pr includes futures-guide')
ok('intervalHours' in read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift') and 'fundingIntervalHours' in read('mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift'), 'funding history shows per-symbol interval')
ac = read('mobile/ios/Linbit/Features/Home/AcademyHomeSection.swift')
ok('linbit 学院' in ac and '合约指南' in ac and 'AcademyHomeSection()' in read('mobile/ios/Linbit/Features/Home/HomeView.swift'), 'home 学院目录 from pagedata')
ok('学院即将上线' not in read('mobile/ios/Linbit/Features/Home/HomeView.swift'), 'academy placeholder removed')


# ---------- 7) Dynamic type cap + fixed-frame mitigations (source) ----------
theme = read('mobile/ios/Linbit/DesignSystem/Theme.swift')
ok('dynamicCap: CGFloat = 1.35' in theme, 'UIFontMetrics cap remains 1.35 (not expanded)')
comp = read('mobile/ios/Linbit/DesignSystem/Components.swift')
ok('frame(minHeight: Metrics.chipH)' in comp, 'ChipBar uses minHeight not fixed height')
ok('frame(minHeight: Metrics.tabBarH)' in comp, 'FloatingTabBar outer minHeight')
home = read('mobile/ios/Linbit/Features/Home/HomeView.swift')
ok('frame(minHeight: 14)' in home or 'minHeight: 14' in home, 'home grid label minHeight')
ok('ScrollView(.horizontal' in home and '涨幅榜' in home, 'home rank tabs horizontally scrollable')
spot = read('mobile/ios/Linbit/Features/Spot/SpotTradeView.swift')
ok('ScrollView(.horizontal' in spot.split('private var topBar')[1].split('private var pairHeader')[0], 'spot top tabs scrollable')


# ---------- 8) Withdraw shot fixtures (no real fund) ----------
wd = read("mobile/ios/Linbit/Features/Assets/WithdrawView.swift")
ok("assets-withdraw-ok" in wd and "shotAvail" in wd, "withdraw-ok fixture injects shotAvail")
ok("截图夹具不提交提现" in wd, "ShotGate withdraw never createWithdrawal")
ok("shot.hasPrefix(\"assets-withdraw\")" in wd or 'shot.hasPrefix("assets-withdraw")' in wd, "all withdraw-* shots use fixture path")
app = read("mobile/ios/Linbit/App/LinbitApp.swift")
ok("assets-withdraw-ok" in app and "assets-withdraw-paused" in app, "shot routes for withdraw ok/paused")
risk = read("mobile/ci/screens-risk.txt")
ok("assets-withdraw-ok" in risk and "kyc-form" in risk and "copy-detail" in risk, "screens-risk covers withdraw+KYC+copy")

# screens-risk 破坏性动作在 SecShot/KycShot/LoanDemo/CopyDemo 下不得打真实 API（源文本守卫 ≠ XCTest）
kyc = (ROOT / "mobile/ios/Linbit/Features/Security/KycView.swift").read_text()
ok("截图夹具不提交认证" in kyc and "KycShot.state != nil" in kyc, "KYC submit blocked under KycShot")
ok("shot-" in kyc and "KycAPI.upload" in kyc, "KYC upload shot-local only under KycShot")
sec = (ROOT / "mobile/ios/Linbit/Features/Security/SecurityCenter.swift").read_text()
ok("截图夹具不删除通行密钥" in sec and "截图夹具不移除设备" in sec, "passkey/device delete blocked under SecShot")
ok("截图夹具不关闭身份验证器" in sec and "截图夹具不添加通行密钥" in sec, "2FA disable + passkey add blocked under SecShot")
loan = (ROOT / "mobile/ios/Linbit/Features/Growth/LoansView.swift").read_text()
ok("if LoanDemo.state != nil { return }" in loan and "LoanAPI.borrow" in loan, "loan borrow blocked under LoanDemo")
copy = (ROOT / "mobile/ios/Linbit/Features/Growth/CopyTradingView.swift").read_text()
ok("if CopyDemo.state != nil { return }" in copy and "CopyAPI.follow" in copy, "copy follow blocked under CopyDemo")



# Passkey cancel + newcomer claimable-only already covered; assert OpenAPI status enum once more
nv = read("mobile/ios/Linbit/Features/Home/NewcomerView.swift")
ok("locked|in_progress|claimable|claimed|expired" in nv, "newcomer OpenAPI status comment present")
ok("guard !actionBusy" in nv, "newcomer anti-double present")

# MarketDemo：DEBUG/ShotGate 行情 example fixture（API shape）；Release 不可启用
md = (ROOT / "mobile/ios/Linbit/Features/Common/MarketDemo.swift").read_text()
ok("enum MarketDemo" in md and "ShotGate.enabled" in md, "MarketDemo gated on ShotGate.enabled")
ok("APIDepth" in md and "APIFuturesTicker" in md and "APIKline" in md, "MarketDemo uses real API shapes")
ok("不是" in md or "example" in md.lower() or "非" in md, "MarketDemo documents non-production")
fm = (ROOT / "mobile/ios/Linbit/Features/Futures/FuturesModel.swift").read_text()
ok("if MarketDemo.on || shotDemo" in fm and "refreshMarket" in fm, "FuturesModel.refreshMarket uses MarketDemo")
ok("截图夹具不下单" in fm, "Futures place blocked under MarketDemo")
sm = (ROOT / "mobile/ios/Linbit/Features/Spot/SpotModel.swift").read_text()
ok("if MarketDemo.on" in sm and "loadBook" in sm, "SpotModel.loadBook uses MarketDemo")
ok("截图夹具不下单" in sm, "Spot place blocked under MarketDemo")
dv = (ROOT / "mobile/ios/Linbit/Features/Futures/FuturesDataViews.swift").read_text()
ok(dv.count("if MarketDemo.on") >= 2, "Futures+Spot chart panels use MarketDemo")
sg = (ROOT / "mobile/ios/Linbit/Features/Common/ShotGate.swift").read_text()
ok("#if DEBUG" in sg and "false" in sg, "ShotGate Release compile-time false")

if failures:
    print('FAIL', len(failures))
    for f in failures: print(' -', f)
    sys.exit(1)
print('ALL PASSED (Python simulation ≠ Swift XCTest; no real fund/reward network)')
