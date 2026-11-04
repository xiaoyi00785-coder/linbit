import { createContext, useContext, useEffect, useMemo, useState } from 'react'

export const Ctx = createContext(null)
export const useApp = () => useContext(Ctx)

export const Tag = ({ k = '', children }) => <span className={'tag ' + k}>{children}</span>
export const Sw = ({ on, onClick }) => <span className={'sw' + (on ? ' on' : '')} onClick={onClick} />

export function PageHead({ title, desc, children }) {
  return <div className="ph"><div><h1>{title}</h1>{desc && <p>{desc}</p>}</div><div className="acts">{children}</div></div>
}

export function Kpi({ l, v, d, up, hint }) {
  return <div className="card kpi"><div className="l"><span>{l}</span>{hint && <span>{hint}</span>}</div>
    <div className="v num">{v}</div>{d && <div className={'d ' + (up === undefined ? 'muted' : up ? 'up' : 'down')}>{d}</div>}</div>
}

export function Drawer({ title, extra, onClose, footer, children, width }) {
  return <><div className="mask" onClick={onClose} /><div className="drawer" style={width ? { width } : null}>
    <div className="dh">{title}{extra}<span className="x" onClick={onClose}>✕</span></div>
    <div className="db">{children}</div>{footer && <div className="df">{footer}</div>}</div></>
}

export function Modal({ title, onClose, footer, children, width }) {
  return <><div className="mask" onClick={onClose} /><div className="modal" style={width ? { width } : null}>
    <div className="dh">{title}<span className="x" onClick={onClose}>✕</span></div>
    <div className="db">{children}</div>{footer && <div className="df">{footer}</div>}</div></>
}

export function Tabs({ items, value, onChange }) {
  return <div className="tabs">{items.map(([k, l, n]) =>
    <span key={k} className={value === k ? 'on' : ''} onClick={() => onChange(k)}>{l}{n !== undefined && <em>{n}</em>}</span>)}</div>
}

/** 通用表格：cols=[{k,t,r,w,render}]，支持关键字过滤、勾选、分页 */
export function DataTable({ cols, rows, filters, pageSize = 12, select, onRow, toolbar, rowKey = 'id' }) {
  const [page, setPage] = useState(1)
  const [sel, setSel] = useState([])
  const pages = Math.max(1, Math.ceil(rows.length / pageSize))
  const p = Math.min(page, pages)
  const view = useMemo(() => rows.slice((p - 1) * pageSize, p * pageSize), [rows, p, pageSize])
  const all = view.length > 0 && view.every(r => sel.includes(r[rowKey]))
  return <div className="card">
    {filters && <div className="filters">{filters}{toolbar && <div style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
      {select && sel.length > 0 && <span className="muted" style={{ alignSelf: 'center' }}>已选 {sel.length} 项</span>}
      {typeof toolbar === 'function' ? toolbar(sel, () => setSel([])) : toolbar}</div>}</div>}
    <div className="tw"><table className="t"><thead><tr>
      {select && <th style={{ width: 30 }}><input type="checkbox" checked={all} onChange={() => setSel(all ? [] : view.map(r => r[rowKey]))} /></th>}
      {cols.map(c => <th key={c.k || c.t} className={c.r ? 'r' : ''} style={c.w ? { width: c.w } : null}>{c.t}</th>)}</tr></thead>
      <tbody>{view.map(r => <tr key={r[rowKey]} style={onRow ? { cursor: 'pointer' } : null} onClick={() => onRow && onRow(r)}>
        {select && <td onClick={e => e.stopPropagation()}><input type="checkbox" checked={sel.includes(r[rowKey])}
          onChange={() => setSel(s => s.includes(r[rowKey]) ? s.filter(x => x !== r[rowKey]) : [...s, r[rowKey]])} /></td>}
        {cols.map(c => <td key={c.k || c.t} className={(c.r ? 'r ' : '') + (c.num ? 'num' : '')}>{c.render ? c.render(r) : r[c.k]}</td>)}</tr>)}</tbody></table>
      {rows.length === 0 && <Empty text="没有符合条件的记录，试试调整筛选条件" />}</div>
    <div className="pager">共 {rows.length} 条<span className="sp" />
      <b onClick={() => setPage(Math.max(1, p - 1))}>‹</b>
      {Array.from({ length: Math.min(pages, 6) }, (_, i) => i + 1).map(i => <b key={i} className={i === p ? 'on' : ''} onClick={() => setPage(i)}>{i}</b>)}
      {pages > 6 && <span>… {pages}</span>}
      <b onClick={() => setPage(Math.min(pages, p + 1))}>›</b><select className="sel" style={{ minWidth: 0, height: 26 }}><option>{pageSize} 条/页</option><option>50 条/页</option></select></div>
  </div>
}

export const useFilter = (rows, fn) => { const [q, setQ] = useState({}); const set = (k) => (e) => setQ({ ...q, [k]: e.target.value }); return [rows.filter(r => fn(r, q)), set, q] }
export const has = (v, s) => !s || String(v).toLowerCase().includes(String(s).toLowerCase())
export const eq = (v, s) => !s || s === '全部' || v === s

/* ===== SVG 图表（无第三方依赖） ===== */
export function LineChart({ series, labels, h = 200, area = true }) {
  const W = 600, H = h, P = 28
  const all = series.flatMap(s => s.data); const max = Math.max(...all) * 1.1, min = 0
  const x = i => P + (i * (W - P * 2)) / (labels.length - 1), y = v => H - 22 - ((v - min) / (max - min)) * (H - 40)
  return <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: h }}>
    {[0, 1, 2, 3].map(i => { const yy = 18 + i * ((H - 40) / 3); return <line key={i} x1={P} x2={W - P} y1={yy} y2={yy} style={{ stroke: 'var(--lb-grid)' }} /> })}
    {labels.map((l, i) => i % Math.ceil(labels.length / 8) === 0 && <text key={i} x={x(i)} y={H - 4} textAnchor="middle" style={{ fill: 'var(--lb-text-3)', fontSize: 10 }}>{l}</text>)}
    {series.map((s, si) => { const d = s.data.map((v, i) => `${i ? 'L' : 'M'}${x(i)},${y(v)}`).join(' ')
      return <g key={si}>{area && si === 0 && <path d={`${d} L${x(s.data.length - 1)},${H - 22} L${x(0)},${H - 22}Z`} style={{ fill: s.color, opacity: .12 }} />}
        <path d={d} style={{ fill: 'none', stroke: s.color, strokeWidth: 2 }} /></g> })}
  </svg>
}
export function BarChart({ data, labels, h = 200, colors }) {
  const W = 600, H = h, P = 28, max = Math.max(...data.flat()) * 1.1, n = labels.length, bw = (W - P * 2) / n
  return <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: h }}>
    {labels.map((l, i) => <g key={i}>{data.map((ser, si) => { const v = ser[i], hh = (v / max) * (H - 40), w = (bw * .6) / data.length
      return <rect key={si} x={P + i * bw + bw * .2 + si * w} y={H - 22 - hh} width={w - 1} height={hh} rx="2" style={{ fill: colors[si] }} /> })}
      {i % Math.ceil(n / 10) === 0 && <text x={P + i * bw + bw / 2} y={H - 6} textAnchor="middle" style={{ fill: 'var(--lb-text-3)', fontSize: 10 }}>{l}</text>}</g>)}
  </svg>
}
export function Donut({ parts, size = 150 }) {
  const tot = parts.reduce((a, p) => a + p.v, 0); let acc = 0; const r = 52, c = 2 * Math.PI * r
  return <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}><svg width={size} height={size} viewBox="0 0 140 140">
    {parts.map((p, i) => { const len = (p.v / tot) * c, el = <circle key={i} cx="70" cy="70" r={r} style={{ fill: 'none', stroke: p.color, strokeWidth: 18 }}
      strokeDasharray={`${len} ${c - len}`} strokeDashoffset={-acc} transform="rotate(-90 70 70)" />; acc += len; return el })}
    <text x="70" y="66" textAnchor="middle" style={{ fill: 'var(--lb-text-3)', fontSize: 10 }}>合计</text>
    <text x="70" y="82" textAnchor="middle" style={{ fill: 'var(--lb-text)', fontSize: 14, fontWeight: 600 }}>{tot.toLocaleString()}</text></svg>
    <div style={{ display: 'grid', gap: 6, fontSize: 12 }}>{parts.map(p => <div key={p.l}><i style={{ display: 'inline-block', width: 8, height: 8, borderRadius: 2, background: p.color, marginRight: 6 }} />{p.l}<span className="muted" style={{ marginLeft: 8 }}>{((p.v / tot) * 100).toFixed(1)}%</span></div>)}</div></div>
}

/** 资金类二次确认：展示变更摘要 + 2FA + 原因，确认后 say() */
export function Confirm({ title, danger, rows = [], note, onClose, onOk, okText = '确认', reasonLabel = '操作原因', children, noCode, noReason }) {
  const { say } = useApp(); const [code, setCode] = useState(''); const [reason, setReason] = useState(''); const [err, setErr] = useState(''); const [busy, setBusy] = useState(false)
  const ok = async () => { if (!onOk) { say('已提交'); onClose(); return } setBusy(true); setErr('')
    try { const m = await onOk({ totpCode: code, reason, comment: reason }); say(m || '已提交'); onClose() } catch (e) { setErr((e && e.message) || String(e)) } finally { setBusy(false) } }
  return <Modal title={title} onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button>
    <button className={'btn ' + (danger ? 'd' : 'p')} disabled={(!noCode && code.length < 6) || (!noReason && !reason.trim()) || busy} onClick={ok}>{busy ? '提交中…' : okText}</button></>}>
    <div className={'banner' + (danger ? ' bad' : '')}>操作将写入审计日志{noCode ? '' : '，需 Google 验证码'}。</div>
    {err && <div className="banner bad">{err}</div>}
    {rows.length > 0 && <dl className="kv">{rows.flatMap(([a, b]) => [<dt key={a}>{a}</dt>, <dd key={a + 'v'}>{b}</dd>])}</dl>}
    {children}
    {note && <div className="muted" style={{ margin: '8px 0' }}>{note}</div>}
    {!noCode && <div className="fr"><label>Google 验证码</label><input className="in" placeholder="6 位动态码" maxLength={6} value={code} onChange={e => setCode(e.target.value.replace(/\D/g, ''))} /></div>}
    {!noReason && <div className="fr"><label>{reasonLabel}</label><input className="in" placeholder="必填" value={reason} onChange={e => setReason(e.target.value)} /></div>}</Modal>
}
/** 接口状态：加载 / 错误（含重试） */
export function ApiState({ q, children }) {
  if (q.loading && !q.data) return <Skeleton />
  if (q.error) return <div className="card pad"><div className="banner bad">加载失败：{q.error.message}{q.error.code ? ` (${q.error.code})` : ''}</div><button className="btn" onClick={q.reload}>重试</button></div>
  return children
}
/** 加载骨架屏 */
export const Skeleton = ({ rows = 6 }) => <div className="card pad">{Array.from({ length: rows }, (_, i) => <div key={i} className="skel" style={{ width: (60 + (i * 13) % 40) + '%' }} />)}</div>
/** 空状态 */
export const Empty = ({ text = '暂无数据', action }) => <div className="empty"><div style={{ fontSize: 28, opacity: .4 }}>∅</div><div>{text}</div>{action && <div style={{ marginTop: 10 }}>{action}</div>}</div>
/** 首次进入模拟 400ms 加载 */
export const useLoading = (ms = 400) => { const [l, setL] = useState(true); useEffect(() => { const t = setTimeout(() => setL(false), ms); return () => clearTimeout(t) }, []); return l }
