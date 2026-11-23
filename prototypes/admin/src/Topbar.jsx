/* 顶栏：全局搜索 / 待办 / 通知（数据来自 admin/v1/todos、admin/v1/inbox，均按当前管理员权限过滤） */
import { useEffect, useRef, useState } from 'react'
import { allowed } from './menu.js'
import { api, ft } from './api.js'

const go = (route, q) => { location.hash = '#/' + route + (q ? '?' + new URLSearchParams(q).toString() : '') }

/** 根据输入猜测类型，给出可跳转的目标（只列有权限的） */
function targets(raw, perms) {
  const v = raw.trim(); if (!v) return []
  const isUid = /^\d{6,9}$/.test(v), isLongNum = /^\d{10,}$/.test(v), isTx = /^(0x)?[0-9a-fA-F]{40,}$/.test(v) || /^[1-9A-HJ-NP-Za-km-z]{60,90}$/.test(v), isMail = v.includes('@')
  const all = [
    { k: 'user', label: `用户：${v}`, perm: 'user:view', route: 'users', q: { q: v }, w: isUid || isMail ? 3 : 1 },
    { k: 'spot', label: `现货订单号：${v}`, perm: 'ledger:view', route: 'spot-orders', q: { orderId: v, status: '' }, w: isLongNum ? 3 : 0 },
    { k: 'spotu', label: `UID ${v} 的现货订单`, perm: 'ledger:view', route: 'spot-orders', q: { uid: v, status: '' }, w: isUid ? 2 : 0 },
    { k: 'futu', label: `UID ${v} 的合约委托`, perm: 'ledger:view', route: 'fut-orders', q: { uid: v }, w: isUid ? 2 : 0 },
    { k: 'dep', label: `充值 TxHash：${v.length > 24 ? v.slice(0, 12) + '…' + v.slice(-8) : v}`, perm: 'ledger:view', route: 'deposit', q: { txHash: v }, w: isTx ? 3 : 0 },
    { k: 'ledger', label: `UID ${v} 的资产流水`, perm: 'ledger:view', route: 'ledger', q: { uid: v }, w: isUid ? 1 : 0 },
  ]
  return all.filter(t => allowed(perms, t.perm) && (t.w > 0 || (!isUid && !isLongNum && !isTx && !isMail && t.k === 'user'))).sort((a, b) => b.w - a.w)
}

function usePop() {
  const [open, setOpen] = useState(false); const ref = useRef(null)
  useEffect(() => { if (!open) return; const f = e => { if (ref.current && !ref.current.contains(e.target)) setOpen(false) }; const h = () => setOpen(false)
    addEventListener('mousedown', f); addEventListener('hashchange', h); return () => { removeEventListener('mousedown', f); removeEventListener('hashchange', h) } }, [open])
  return [open, setOpen, ref]
}

export function TopSearch({ perms }) {
  const [v, setV] = useState(''); const [open, setOpen, ref] = usePop(); const [i, setI] = useState(0)
  const ts = targets(v, perms)
  const pick = t => { if (!t) return; go(t.route, t.q); setOpen(false); setV('') }
  const onKey = e => { if (e.key === 'Enter') pick(ts[i] || ts[0]); else if (e.key === 'ArrowDown') { e.preventDefault(); setI(Math.min(i + 1, ts.length - 1)) } else if (e.key === 'ArrowUp') { e.preventDefault(); setI(Math.max(i - 1, 0)) } else if (e.key === 'Escape') setOpen(false) }
  return <div className="search" ref={ref} style={{ position: 'relative' }}>⌕&nbsp;<input className="sin" placeholder="搜索 UID / 邮箱 / 订单号 / TxHash" value={v} onFocus={() => setOpen(true)}
      onChange={e => { setV(e.target.value); setI(0); setOpen(true) }} onKeyDown={onKey} />
    {open && v.trim() && <div className="pop" style={{ left: 0, right: 0 }}>{ts.length ? ts.map((t, j) => <div key={t.k} className={'pi' + (j === i ? ' on' : '')} onMouseEnter={() => setI(j)} onMouseDown={() => pick(t)}>{t.label}</div>)
      : <div className="pi muted">没有可搜索的目标（权限不足）</div>}<div className="pf muted">回车跳转第一项 · ↑↓ 选择</div></div>}</div>
}

export function TodoBell({ perms }) {
  const [open, setOpen, ref] = usePop(); const [d, setD] = useState(null)
  const load = () => api('todos').then(setD, () => {})
  useEffect(() => { load(); const t = setInterval(load, 60000); return () => clearInterval(t) }, []) // eslint-disable-line
  const list = (d && d.items) || []; const total = d ? d.total : 0
  return <div className="ibtn" title="待办" ref={ref} onClick={() => { setOpen(!open); if (!open) load() }}>☑{total > 0 && <span className="cnt">{total > 99 ? '99+' : total}</span>}
    {open && <div className="pop" style={{ right: 0, width: 280 }} onClick={e => e.stopPropagation()}><div className="ph">待办 <span className="muted">（每分钟刷新）</span></div>
      {list.filter(x => x.count > 0).map(x => <div key={x.key} className="pi" onClick={() => { go(x.route); setOpen(false) }}>{x.label}<b className="pc">{x.count >= 100 ? '99+' : x.count}</b></div>)}
      {!list.some(x => x.count > 0) && <div className="pi muted">暂无待办 🎉</div>}
      {list.length > 0 && <div className="pf muted">统计 {ft(d.cachedAt)}；仅显示你有权处理的事项</div>}</div>}</div>
}

const KIND = { risk: '⚑ 风控', mine: '✓ 我的申请', solvency: '⚖ 偿付', export: '⇩ 导出' }
export function InboxBell({ user }) {
  const [open, setOpen, ref] = usePop(); const [d, setD] = useState(null); const key = 'lb_inbox_seen_' + user
  const [seen, setSeen] = useState(() => localStorage.getItem(key) || '')
  const load = () => api('inbox').then(setD, () => {})
  useEffect(() => { load(); const t = setInterval(load, 120000); return () => clearInterval(t) }, []) // eslint-disable-line
  const list = (d && d.items) || []; const unread = list.filter(x => (x.ts || '') > seen).length
  const toggle = () => { if (!open) { load(); const now = new Date().toISOString(); localStorage.setItem(key, now); setTimeout(() => setSeen(now), 1500) } setOpen(!open) }
  return <div className="ibtn" title="通知" ref={ref} onClick={toggle}>🔔{unread > 0 && <span className="dot" />}
    {open && <div className="pop" style={{ right: 0, width: 360 }} onClick={e => e.stopPropagation()}><div className="ph">通知 <span className="muted">（近 7 天）</span></div>
      {list.map(x => <div key={x.id} className={'pi col' + ((x.ts || '') > seen ? ' new' : '')} onClick={() => { go(x.route); setOpen(false) }}>
        <span className="muted" style={{ fontSize: 11 }}>{KIND[x.kind] || x.kind} · {ft(x.ts)}{x.uid ? ' · UID ' + x.uid : ''}</span><span>{x.title}</span></div>)}
      {!list.length && <div className="pi muted">暂无通知</div>}</div>}</div>
}
