import { useEffect, useState } from 'react'
import { FLAT, allowed, menuFor } from './menu.js'
import { Ctx } from './ui.jsx'
import { api, getToken, roleCn, setToken, setUnauthorizedHandler } from './api.js'
import Login from './pages/Login.jsx'
import Dashboard from './pages/Dashboard.jsx'
import * as U from './pages/Users.jsx'
import * as F from './pages/Funds.jsx'
import * as T from './pages/Trade.jsx'
import * as P from './pages/Product.jsx'
import * as O from './pages/Ops.jsx'
import * as S from './pages/System.jsx'
import * as X from './pages/Extra.jsx'
import { Adjust } from './pages/Adjust.jsx'
import { Support } from './pages/Support.jsx'
import Marketmaker from './pages/Marketmaker.jsx'
import * as C from './pages/Compliance.jsx'
import * as SEC from './pages/Security.jsx'
import * as P1 from './pages/P1.jsx'
import * as B6 from './pages/Batch6.jsx'
import { Skeleton } from './ui.jsx'
import { InboxBell, TodoBell, TopSearch } from './Topbar.jsx'

const ROUTES = { dashboard: Dashboard, users: U.Users, kyc: U.Kyc, 'risk-users': U.RiskList, withdraw: F.Withdraw, deposit: F.Deposit, ledger: F.Ledger, adjust: Adjust, support: Support,
  'spot-orders': T.SpotOrders, positions: T.Positions, grid: T.Grid, marketmaker: Marketmaker, coins: P.Coins, pairs: P.Pairs, futures: P.Futures, fees: P.Fees,
  campaigns: O.Campaigns, rewards: O.Rewards, points: O.Points, copy: O.Copy, rebate: O.Rebate, content: O.Content, admins: S.Admins, audit: S.Audit, settings: S.Settings,
  wallets: X.Wallets, liquidation: X.Liquidation, partners: X.Partners, coupons: X.Coupons, notify: X.Notify,
  cs: C.CsWorkbench, 'risk-rules': SEC.RiskRules, apikeys: SEC.ApiKeys, chat: C.ChatModeration, por: C.ProofOfReserves, solvency: C.Solvency,
  'spot-trades': P1.SpotTrades, 'fut-orders': P1.FutOrders, 'fut-trades': P1.FutTrades, 'fut-risk': P1.FutRisk, 'login-history': P1.LoginHistory, 'report-exports': P1.ReportExports,
  earn: P1.EarnLoans, convert: P1.Convert, event: P1.EventContracts, 'fee-overrides': P1.FeeOverrides, 'sub-accounts': P1.SubAccounts, vip: P1.VipApplications,
  referrals: P1.Referrals, 'internal-transfers': P1.InternalTransfers, 'user-coupons': P1.UserCoupons,
  'fund-changes': B6.FundChanges, launchpad: B6.Launchpad, staking: B6.Staking, nft: B6.NFT, options: B6.Options, web3wallet: B6.Web3Wallet, paper: B6.Paper, dca: B6.DCA,
  'im-groups': B6.IMGroups, 'app-versions': B6.AppVersions, 'content-docs': B6.ContentDocs, 'app-push': B6.AppPush }

const parse = () => { const [k, q] = (location.hash.slice(2) || 'dashboard').split('?'); return { key: ROUTES[k] ? k : 'dashboard', q: Object.fromEntries(new URLSearchParams(q || '')) } }

export default function App() {
  const [r, setR] = useState(parse)
  const [toast, setToast] = useState(null)
  useEffect(() => { const f = () => setR(parse()); addEventListener('hashchange', f); return () => removeEventListener('hashchange', f) }, [])
  const go = (k) => { location.hash = '#/' + k }
  const say = (t) => { setToast(t); setTimeout(() => setToast(null), 1800) }
  const [me, setMe] = useState(null); const [authed, setAuthed] = useState(!!getToken())
  useEffect(() => { setUnauthorizedHandler(() => { setAuthed(false); setMe(null) }) }, [])
  useEffect(() => { if (authed) api('me').then(setMe, () => {}) }, [authed])
  const logout = () => { api('auth/logout', { method: 'POST' }).catch(() => {}).finally(() => { setToken(null); setAuthed(false); setMe(null) }) }
  const meV = { id: me ? me.id : 0, user: me ? me.username : '…', role: me ? roleCn(me.roles) : '', perms: me ? me.permissions : [] }
  const menu = menuFor(meV.perms)
  const cur = FLAT.find(f => f.key === r.key)
  const ok = !!me && allowed(meV.perms, cur.perm)
  // 无权限的路由（含默认 dashboard）：跳到第一个有权限的菜单
  const first = menu.length ? menu[0].items[0][0] : null
  useEffect(() => { if (authed && me && !ok && first && r.key === 'dashboard') go(first) }, [authed, me, ok, first, r.key]) // eslint-disable-line
  if (!authed) return <Login onDone={() => setAuthed(true)} />
  const Page = ok ? ROUTES[r.key] : null
  return <Ctx.Provider value={{ go, say, q: r.q, me: meV }}>
    <div className="app">
      <aside className="side"><div className="brand"><i />linbit<em>Admin</em></div>
        {menu.map(m => <div key={m.g}><div className="grp">{m.g}</div>
          {m.items.map(([k, n, ic, , b]) => <a key={k} className={'mi' + (k === r.key ? ' on' : '')} href={'#/' + k}><span className="ic">{ic}</span>{n}{b && <span className="bd">{b}</span>}</a>)}</div>)}
      </aside>
      <div className="main">
        <header className="top"><span className="crumb">{cur.group} / <b>{cur.name}</b></span><span className="env">{import.meta.env.VITE_ADMIN_ENV_LABEL || '后台'}</span>
          <TopSearch perms={meV.perms} />
          {me && <TodoBell perms={meV.perms} />}{me && <InboxBell user={meV.user} />}
          <div className="me"><div className="av">{meV.user.slice(0, 2).toUpperCase()}</div><div style={{ lineHeight: 1.2 }}>{meV.user}<div className="muted" style={{ fontSize: 11 }}>{meV.role}</div></div>
            <a className="btn g" style={{ marginLeft: 8 }} onClick={logout}>退出</a></div></header>
        <main className="content" key={r.key + JSON.stringify(r.q)}>{!me ? <Skeleton /> : Page ? <Page /> : <div className="card pad"><div className="banner bad">无权限访问「{cur.name}」</div>
          <div className="muted">当前角色：{meV.role || '-'}。如需开通请联系超级管理员在「管理员与角色」中调整角色。</div>{first && <button className="btn" style={{ marginTop: 10 }} onClick={() => go(first)}>返回首个可用页面</button>}</div>}</main>
      </div>
    </div>{toast && <div className="toast">✓ {toast}</div>}
  </Ctx.Provider>
}
