import { useEffect, useState } from 'react'
import { FLAT, MENU } from './menu.js'
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

const ROUTES = { dashboard: Dashboard, users: U.Users, kyc: U.Kyc, 'risk-users': U.RiskList, withdraw: F.Withdraw, deposit: F.Deposit, ledger: F.Ledger, adjust: Adjust, support: Support,
  'spot-orders': T.SpotOrders, positions: T.Positions, grid: T.Grid, coins: P.Coins, pairs: P.Pairs, futures: P.Futures, fees: P.Fees,
  campaigns: O.Campaigns, rewards: O.Rewards, points: O.Points, copy: O.Copy, rebate: O.Rebate, content: O.Content, admins: S.Admins, audit: S.Audit, settings: S.Settings,
  wallets: X.Wallets, liquidation: X.Liquidation, partners: X.Partners, coupons: X.Coupons, notify: X.Notify }

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
  if (!authed) return <Login onDone={() => setAuthed(true)} />
  const meV = { user: me ? me.username : '…', role: me ? roleCn(me.roles) : '', perms: me ? me.permissions : [] }
  const cur = FLAT.find(f => f.key === r.key); const Page = ROUTES[r.key]
  return <Ctx.Provider value={{ go, say, q: r.q, me: meV }}>
    <div className="app">
      <aside className="side"><div className="brand"><i />linbit<em>Admin</em></div>
        {MENU.map(m => <div key={m.g}><div className="grp">{m.g}</div>
          {m.items.map(([k, n, ic, , b]) => <a key={k} className={'mi' + (k === r.key ? ' on' : '')} href={'#/' + k}><span className="ic">{ic}</span>{n}{b && <span className="bd">{b}</span>}</a>)}</div>)}
      </aside>
      <div className="main">
        <header className="top"><span className="crumb">{cur.group} / <b>{cur.name}</b></span><span className="env">{import.meta.env.VITE_ADMIN_ENV_LABEL || '后台'}</span>
          <div className="search">⌕&nbsp; 搜索 UID / 邮箱 / 订单号 / TxID</div>
          <div className="ibtn" title="待办">☑<span className="dot" /></div><div className="ibtn" title="通知">🔔</div>
          <div className="me"><div className="av">{meV.user.slice(0, 2).toUpperCase()}</div><div style={{ lineHeight: 1.2 }}>{meV.user}<div className="muted" style={{ fontSize: 11 }}>{meV.role}</div></div>
            <a className="btn g" style={{ marginLeft: 8 }} onClick={logout}>退出</a></div></header>
        <main className="content" key={r.key}><Page /></main>
      </div>
    </div>{toast && <div className="toast">✓ {toast}</div>}
  </Ctx.Provider>
}
