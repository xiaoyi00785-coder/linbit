/* 通用配置卡：GET 读取对象配置，逐字段编辑，PUT 提交（2FA + 原因，写审计） */
import { useEffect, useState } from 'react'
import { ApiState, Confirm, Empty, Sw } from '../ui.jsx'
import { api, num, useApi } from '../api.js'

const EMPTY = {}
const scalar = v => v === null || ['string', 'number', 'boolean'].includes(typeof v)

export function ConfigCard({ path, title, labels = {}, field }) {
  const q0 = useApi(path); const q = { ...q0, data: q0.data && field ? (q0.data[field] || EMPTY) : q0.data }; const [draft, setDraft] = useState(null); const [ok, setOk] = useState(false)
  useEffect(() => { if (q.data) setDraft(q.data) }, [q.data])
  const keys = draft ? Object.keys(draft).filter(k => scalar(draft[k])) : []
  const dirty = draft && q.data && keys.some(k => draft[k] !== q.data[k])
  const set = (k, v) => setDraft({ ...draft, [k]: v })
  return <div className="card pad" style={{ marginTop: 12 }}><div className="sec" style={{ marginTop: 0 }}>{title}</div>
    <ApiState q={q}>{!keys.length ? <Empty /> : keys.map(k => <div className="fr" key={k}><label title={k}>{labels[k] || k}</label>
      {typeof draft[k] === 'boolean' ? <Sw on={draft[k]} onClick={() => set(k, !draft[k])} />
        : <input className="in" value={draft[k] ?? ''} onChange={e => set(k, typeof q.data[k] === 'number' ? (e.target.value === '' ? '' : +e.target.value) : e.target.value)} />}</div>)}
      {keys.length > 0 && <div style={{ textAlign: 'right' }}><button className="btn" disabled={!dirty} onClick={() => setDraft(q.data)}>还原</button> <button className="btn p" disabled={!dirty} onClick={() => setOk(true)}>保存</button></div>}</ApiState>
    {ok && <Confirm title={'保存 · ' + title} rows={keys.filter(k => draft[k] !== q.data[k]).map(k => [labels[k] || k, `${q.data[k]} → ${draft[k]}`])} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { const body = {}; keys.forEach(k => { if (draft[k] !== q.data[k]) body[k] = draft[k] }); await api(path, { method: 'PUT', body: { ...(field ? { [field]: body } : body), totpCode, reason } }); q0.reload(); return '已保存' }} />}</div>
}

/** 用户近 30 日资金画像（充值/提现按币种、现货/合约成交量） */
export function Profile30d({ uid }) {
  const q = useApi(uid ? `users/${encodeURIComponent(uid)}/profile30d` : null); const d = q.data || {}
  const agg = l => (l || []).map(x => `${num(x.amount, 4)} ${x.asset}`).join(' · ') || '0'
  return <ApiState q={q}><div className="grid g4">{[['充值', agg(d.deposits)], ['提现', agg(d.withdrawals)], ['现货成交额', num((d.spot || {}).quoteVolume) + ` (${(d.spot || {}).trades || 0} 笔)`],
    ['合约成交额', num((d.futures || {}).volume) + ` (${(d.futures || {}).trades || 0} 笔)`]].map(([a, b]) => <div className="card kpi" key={a}><div className="l">{a}</div><div className="v num" style={{ fontSize: 14 }}>{b}</div></div>)}</div></ApiState>
}
