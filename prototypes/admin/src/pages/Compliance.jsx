/* 合规与客服：客服工作台 / 聊天审核 / 储备金证明 / 偿付能力。全部读取 admin/v1 真实接口；
   写操作统一走 Confirm（Google 验证码 + 原因），由后端写审计。用户内容一律纯文本渲染。 */
import { useEffect, useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Empty, Kpi, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { api, errText, ft, items, num, useApi } from '../api.js'
import { UserDrawer, mapUser } from './Users.jsx'
import { allowed } from '../menu.js'

const pre = { whiteSpace: 'pre-wrap', wordBreak: 'break-word' }
const F = ({ l, children }) => <div className="fr"><label>{l}</label>{children}</div>
const useCan = () => { const { me } = useApp(); return p => allowed(me && me.perms, p) }

/* ===================== 客服工作台 ===================== */
const TK_CAT = { account: '账户', deposit_withdraw: '充值与提现', trade: '交易', kyc: '身份认证', security: '账户安全', other: '其他' }
export function CsWorkbench() {
  const { go, say } = useApp(); const can = useCan()
  const open = useApi('support/tickets', { status: 'open', pageSize: 100 })
  const mine = useApi('support/tickets', { status: 'open', assigneeId: 'me', pageSize: 100 })
  const unas = useApi('support/tickets', { status: 'open', assigneeId: 'none', pageSize: 100 })
  const replied = useApi('support/tickets', { status: 'replied', pageSize: 100 })
  const me = useApi(can('support:reply') ? 'me' : null)
  const [kw, setKw] = useState(''); const [uq, setUq] = useState(''); const [busy, setBusy] = useState(0)
  const take = async id => { if (!me.data) return; setBusy(id); try { await api(`support/tickets/${id}`, { method: 'PATCH', body: { assigneeId: me.data.id } }); say('已领取工单 #' + id); mine.reload(); unas.reload() } catch (e) { say('失败：' + errText(e)) } finally { setBusy(0) } }
  const waitMin = t => t.lastReplyAt ? Math.round((Date.now() - new Date(t.lastReplyAt)) / 60000) : null
  const userWait = items(open.data).filter(t => t.lastReplyBy === 'user')
  const oldest = userWait.reduce((m, t) => Math.max(m, waitMin(t) || 0), 0)
  const n = q => q.data ? items(q.data).length : '-'
  const cols = (withTake) => [{ t: '单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '分类', render: x => TK_CAT[x.category] || x.category }, { t: '标题', render: x => <span style={pre}>{x.subject}</span> },
    { t: '用户等待', render: x => x.lastReplyBy === 'user' && waitMin(x) != null ? <Tag k={waitMin(x) > 60 ? 'bad' : 'wn'}>{waitMin(x)} 分钟</Tag> : '-' },
    { t: '处理人', render: x => x.assigneeName || '-' },
    { t: '操作', render: x => <span onClick={e => e.stopPropagation()}>{withTake && can('support:reply') && <button className="btn g" disabled={busy === x.id} onClick={() => take(x.id)}>领取</button>}
      <button className="btn g" onClick={() => go('support')}>去处理</button></span> }]
  return <><PageHead title="客服工作台" desc="待处理工单队列、领取与用户速查；回复/关闭在「客服工单」完成（均记审计）">
    <button className="btn" onClick={() => { open.reload(); mine.reload(); unas.reload(); replied.reload() }}>刷新</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}>
      <Kpi l="待处理工单" v={n(open)} d={`其中用户最后发言 ${open.data ? userWait.length : '-'} 单`} />
      <Kpi l="指派给我" v={n(mine)} /><Kpi l="未指派" v={n(unas)} d="可直接领取" />
      <Kpi l="最长用户等待" v={open.data ? oldest + ' 分钟' : '-'} d={`已回复待用户确认 ${n(replied)} 单`} up={open.data ? oldest <= 60 : undefined} /></div>
    <div className="card pad" style={{ marginBottom: 12 }}><div className="sec" style={{ marginTop: 0 }}>用户速查</div>
      <div style={{ display: 'flex', gap: 8 }}><input className="in" style={{ width: 260 }} placeholder="UID / 邮箱 / 昵称" value={kw} onChange={e => setKw(e.target.value)} onKeyDown={e => e.key === 'Enter' && setUq(kw.trim())} />
        <button className="btn p" disabled={!kw.trim()} onClick={() => setUq(kw.trim())}>查询</button></div>
      {uq && <UserLookup q={uq} />}</div>
    <div className="sec">我的待处理</div><ApiState q={mine}><DataTable rows={items(mine.data)} pageSize={8} onRow={() => go('support')} cols={cols(false)} /></ApiState>
    <div className="sec">未指派（先到先得）</div><ApiState q={unas}><DataTable rows={items(unas.data)} pageSize={8} cols={cols(true)} /></ApiState></>
}
function UserLookup({ q }) {
  const can = useCan(); const r = useApi('users', { q, size: 10 }); const [cur, setCur] = useState(null)
  const tk = useApi(/^\d+$/.test(q) ? 'support/tickets' : null, { uid: q, pageSize: 20 })
  return <div style={{ marginTop: 10 }}><ApiState q={r}>{!items(r.data).length ? <Empty text="未找到用户" /> : <table className="t"><thead><tr><th>UID</th><th>昵称</th><th>邮箱</th><th>KYC</th><th>状态</th><th>注册</th><th /></tr></thead>
    <tbody>{items(r.data).map(mapUser).map(u => <tr key={u.uid}><td className="pri">{u.uid}</td><td>{u.nick}</td><td>{u.email}</td><td>{u.kyc}</td><td>{u.status}</td><td>{u.reg}</td>
      <td>{can('ledger:view') && <a className="btn g" onClick={() => setCur(u)}>资产/详情</a>}</td></tr>)}</tbody></table>}</ApiState>
    {tk.data && <div className="muted" style={{ marginTop: 8 }}>该 UID 历史工单 {items(tk.data).length} 单{items(tk.data).slice(0, 5).map(t => ` · #${t.id} ${t.status}`).join('')}</div>}
    {cur && <UserDrawer u={cur} onClose={() => setCur(null)} onChanged={() => setCur(null)} />}</div>
}

/* ===================== 聊天审核 ===================== */
const MSG_ST = { visible: ['可见', 'ok'], deleted: ['已删除', 'bad'], hidden: ['已隐藏', 'wn'] }
const RP_ST = { open: ['待处理', 'wn'], resolved: ['已处理', 'ok'], dismissed: ['已驳回', ''] }
const tg = (m, s) => { const [l, k] = m[s] || [s, '']; return <Tag k={k}>{l}</Tag> }
const CFG = { minIntervalMs: '发言间隔 (ms)', maxLength: '单条最大长度', minKycLevel: '最低 KYC 等级', blockLinks: '屏蔽链接', blockContacts: '屏蔽联系方式',
  historyLimitMax: '历史条数上限', generalRoom: '综合聊天室', futuresRooms: '合约聊天室', reportsToHide: '举报达 N 次自动隐藏（0=不隐藏）', bannedWords: '屏蔽词（逗号分隔）' }

export function ChatModeration() {
  const [t, setT] = useState('reports'); const [act, setAct] = useState(null)
  return <><PageHead title="聊天审核" desc="交易聊天室：举报处理、消息检索与删除、禁言/封禁、聊天配置；所有操作需 Google 验证码与原因并写入审计" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['reports', '举报队列'], ['messages', '消息检索'], ['sanctions', '禁言/封禁名单'], ['config', '聊天配置']]} /></div>
    {t === 'reports' && <ChatReports setAct={setAct} />}
    {t === 'messages' && <ChatMessages setAct={setAct} />}
    {t === 'sanctions' && <ChatSanctions setAct={setAct} />}
    {t === 'config' && <ChatConfig />}
    {act && <ChatAction a={act} onClose={() => setAct(null)} />}</>
}
function ChatAction({ a, onClose }) {
  const [mins, setMins] = useState('60')
  const title = { del: '删除消息', mute: '禁言用户', unmute: '解除禁言', ban: '封禁用户（禁止发言）', unban: '解除封禁', resolve: '举报成立（标记已处理）', dismiss: '驳回举报' }[a.kind]
  const rows = a.msg ? [['UID', a.msg.uid], ['房间', a.msg.room], ['内容', <span style={pre}>{a.msg.text}</span>]] : [['UID', a.uid]]
  return <Confirm title={title} danger={['del', 'ban', 'mute'].includes(a.kind)} rows={rows} onClose={onClose} onOk={async ({ totpCode, reason }) => {
    const b = { totpCode, reason }
    if (a.kind === 'del') await api(`chat/messages/${a.msg.id}`, { method: 'DELETE', body: b })
    if (a.kind === 'mute') { const m = parseInt(mins, 10); if (!(m >= 1 && m <= 525600)) throw new Error('禁言时长须为 1–525600 分钟'); await api('chat/mutes', { method: 'POST', body: { ...b, uid: a.uid, durationMinutes: m } }) }
    if (a.kind === 'unmute') await api(`chat/mutes/${encodeURIComponent(a.uid)}`, { method: 'DELETE', body: b })
    if (a.kind === 'ban') await api('chat/bans', { method: 'POST', body: { ...b, uid: a.uid } })
    if (a.kind === 'unban') await api(`chat/bans/${encodeURIComponent(a.uid)}`, { method: 'DELETE', body: b })
    if (a.kind === 'resolve' || a.kind === 'dismiss') await api(`chat/reports/${a.report.id}/resolve`, { method: 'POST', body: { ...b, action: a.kind } })
    a.done && a.done(); return title + '：已完成' }}>
    {a.kind === 'mute' && <F l="禁言时长"><select className="sel" value={mins} onChange={e => setMins(e.target.value)}>{[['10', '10 分钟'], ['60', '1 小时'], ['1440', '1 天'], ['10080', '7 天'], ['43200', '30 天']].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></F>}
  </Confirm>
}
function ChatReports({ setAct }) {
  const [st, setSt] = useState('open'); const q = useApi('chat/reports', { status: st, limit: 200 })
  return <ApiState q={q}><DataTable rows={items(q.data)} filters={<select className="sel" value={st} onChange={e => setSt(e.target.value)}>
    {[['open', '待处理'], ['resolved', '已处理'], ['dismissed', '已驳回'], ['all', '全部']].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>}
    cols={[{ t: '举报#', k: 'id' }, { t: '时间', render: x => ft(x.createdAt) }, { t: '举报人', k: 'reporter' }, { t: '原因', render: x => <span style={pre}>{x.reason || '-'}</span> },
      { t: '被举报 UID', render: x => x.message.uid }, { t: '房间', render: x => x.message.room }, { t: '消息内容', render: x => <span style={pre}>{x.message.text}</span> },
      { t: '消息', render: x => tg(MSG_ST, x.message.status) }, { t: '状态', render: x => <>{tg(RP_ST, x.status)}{x.handledBy ? <span className="muted"> {x.handledBy}</span> : null}</> },
      { t: '操作', render: x => x.status !== 'open' ? '-' : <span style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
        {x.message.status !== 'deleted' && <button className="btn g" onClick={() => setAct({ kind: 'del', msg: { id: x.messageId, ...x.message }, done: q.reload })}>删除消息</button>}
        <button className="btn g" onClick={() => setAct({ kind: 'mute', uid: x.message.uid, done: q.reload })}>禁言</button>
        <button className="btn g" onClick={() => setAct({ kind: 'resolve', report: x, done: q.reload })}>成立</button>
        <button className="btn g" onClick={() => setAct({ kind: 'dismiss', report: x, done: q.reload })}>驳回</button></span> }]} /></ApiState>
}
function ChatMessages({ setAct }) {
  const [f, setF] = useState({ room: '', uid: '', status: '', q: '' }); const [qq, setQq] = useState(f); const v = k => e => setF({ ...f, [k]: e.target.value })
  const q = useApi('chat/messages', { ...qq, limit: 200 })
  return <ApiState q={q}><DataTable rows={items(q.data)} filters={<><input className="in" placeholder="房间" value={f.room} onChange={v('room')} style={{ width: 120 }} />
    <input className="in" placeholder="UID" value={f.uid} onChange={v('uid')} style={{ width: 120 }} /><input className="in" placeholder="内容关键词" value={f.q} onChange={v('q')} />
    <select className="sel" value={f.status} onChange={v('status')}><option value="">全部状态</option><option value="visible">可见</option><option value="deleted">已删除</option></select>
    <button className="btn p" onClick={() => setQq({ ...f })}>查询</button></>}
    cols={[{ t: 'ID', k: 'id' }, { t: '时间', render: x => ft(x.createdAt) }, { t: '房间', k: 'room' }, { t: 'UID', k: 'uid' }, { t: '内容', render: x => <span style={pre}>{x.text}</span> },
      { t: '举报', render: x => x.reports ? <Tag k="wn">{x.reports}</Tag> : '0' }, { t: '状态', render: x => <>{tg(MSG_ST, x.status)}{x.deletedBy ? <span className="muted"> {x.deletedBy}：{x.deletedReason}</span> : null}</> },
      { t: '操作', render: x => <span style={{ display: 'flex', gap: 4 }}>{x.status !== 'deleted' && <button className="btn g" onClick={() => setAct({ kind: 'del', msg: x, done: q.reload })}>删除</button>}
        <button className="btn g" onClick={() => setAct({ kind: 'mute', uid: x.uid })}>禁言</button><button className="btn g" onClick={() => setAct({ kind: 'ban', uid: x.uid })}>封禁</button></span> }]} /></ApiState>
}
function ChatSanctions({ setAct }) {
  const q = useApi('chat/sanctions'); const [uid, setUid] = useState('')
  return <ApiState q={q}><DataTable rows={items(q.data)} rowKey="uid" filters={<><input className="in" placeholder="UID" value={uid} onChange={e => setUid(e.target.value)} style={{ width: 160 }} />
    <button className="btn" disabled={!uid.trim()} onClick={() => setAct({ kind: 'mute', uid: uid.trim(), done: q.reload })}>禁言</button>
    <button className="btn d" disabled={!uid.trim()} onClick={() => setAct({ kind: 'ban', uid: uid.trim(), done: q.reload })}>封禁</button></>}
    cols={[{ t: 'UID', k: 'uid' }, { t: '禁言至', render: x => x.mutedUntil && new Date(x.mutedUntil) > new Date() ? ft(x.mutedUntil) : '-' }, { t: '封禁', render: x => x.banned ? <Tag k="bad">已封禁</Tag> : '-' },
      { t: '原因', render: x => <span style={pre}>{x.reason || '-'}</span> }, { t: '操作人', k: 'updatedBy' }, { t: '更新时间', render: x => ft(x.updatedAt) },
      { t: '操作', render: x => <span style={{ display: 'flex', gap: 4 }}>{x.mutedUntil && new Date(x.mutedUntil) > new Date() && <button className="btn g" onClick={() => setAct({ kind: 'unmute', uid: x.uid, done: q.reload })}>解除禁言</button>}
        {x.banned && <button className="btn g" onClick={() => setAct({ kind: 'unban', uid: x.uid, done: q.reload })}>解除封禁</button>}</span> }]} /></ApiState>
}
function ChatConfig() {
  const q = useApi('chat/config'); const [d, setD] = useState(null); const [ok, setOk] = useState(false)
  const cfg = q.data && q.data.config
  useEffect(() => { if (cfg) setD({ ...cfg, bannedWords: (cfg.bannedWords || []).join(', ') }) }, [q.data]) // eslint-disable-line
  const norm = x => ({ ...x, bannedWords: String(x.bannedWords || '').split(/[,，\n]/).map(s => s.trim()).filter(Boolean) })
  const changed = d && cfg ? Object.keys(CFG).filter(k => JSON.stringify(norm(d)[k]) !== JSON.stringify(cfg[k])) : []
  return <ApiState q={q}>{d && <div className="card pad" style={{ marginTop: 12 }}>
    {Object.keys(CFG).map(k => <F key={k} l={CFG[k]}>{typeof cfg[k] === 'boolean' ? <span className={'sw' + (d[k] ? ' on' : '')} onClick={() => setD({ ...d, [k]: !d[k] })} />
      : k === 'bannedWords' ? <textarea className="in" rows={3} style={{ width: 420 }} value={d[k]} onChange={e => setD({ ...d, [k]: e.target.value })} />
        : <input className="in" value={d[k]} onChange={e => setD({ ...d, [k]: e.target.value === '' ? '' : +e.target.value })} />}
      <span className="muted" style={{ marginLeft: 8 }}>默认 {String(Array.isArray(q.data.defaults[k]) ? q.data.defaults[k].join(',') || '无' : q.data.defaults[k])}</span></F>)}
    <div style={{ textAlign: 'right' }}><button className="btn" disabled={!changed.length} onClick={() => setD({ ...cfg, bannedWords: (cfg.bannedWords || []).join(', ') })}>还原</button> <button className="btn p" disabled={!changed.length} onClick={() => setOk(true)}>保存</button></div>
    {ok && <Confirm title="保存聊天配置" rows={changed.map(k => [CFG[k], `${JSON.stringify(cfg[k])} → ${JSON.stringify(norm(d)[k])}`])} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { const n = norm(d); const body = { totpCode, reason }; changed.forEach(k => { body[k] = n[k] }); await api('chat/config', { method: 'PUT', body }); q.reload(); return '已保存' }} />}
  </div>}</ApiState>
}

/* ===================== 储备金证明（PoR） ===================== */
const POR_ST = { draft: ['草稿', ''], pending_review: ['待复核', 'wn'], published: ['已发布', 'ok'], rejected: ['已驳回', 'bad'] }
const ratio = r => r == null || r === '' ? '-' : (Number(r) * 100).toFixed(2) + '%'
export function ProofOfReserves() {
  const { me } = useApp(); const q = useApi('proof-of-reserves', { limit: 50 }); const [cur, setCur] = useState(null); const [add, setAdd] = useState(false); const [act, setAct] = useState(null)
  const list = items(q.data); const live = list.find(s => s.status === 'published')
  return <><PageHead title="储备金证明" desc="人工录入储备快照（负债/储备）→ 提交 → 第二人复核后发布到前台；录入人与提交人不能复核；所有操作需 Google 验证码并写审计">
    <button className="btn p" onClick={() => setAdd(true)}>+ 新建快照</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="前台当前发布" v={live ? ft(live.snapshotAt) : '无'} d={live ? `${live.items.length} 个资产 · 复核 ${live.reviewedBy || '-'}` : '尚未发布任何快照'} />
      <Kpi l="待复核" v={list.filter(s => s.status === 'pending_review').length} /><Kpi l="草稿 / 驳回" v={list.filter(s => s.status === 'draft' || s.status === 'rejected').length} />
      <Kpi l="最低储备率（已发布）" v={live ? ratio(Math.min(...live.items.filter(i => i.ratio).map(i => +i.ratio))) : '-'} up={live ? live.items.every(i => !i.ratio || +i.ratio >= 1) : undefined} /></div>
    <ApiState q={q}><DataTable rows={list} onRow={setCur} cols={[{ t: '快照时间', render: x => <b>{ft(x.snapshotAt)}</b> }, { t: '资产', render: x => x.items.map(i => i.asset).join(', ') },
      { t: '储备率', render: x => x.items.map(i => `${i.asset} ${ratio(i.ratio)}`).join(' · ') }, { t: '录入', k: 'createdBy' }, { t: '提交', render: x => x.submittedBy || '-' }, { t: '复核', render: x => x.reviewedBy || '-' },
      { t: '状态', render: x => tg(POR_ST, x.status) }]} /></ApiState>
    {cur && <Drawer title={'储备快照 · ' + ft(cur.snapshotAt)} extra={tg(POR_ST, cur.status)} onClose={() => setCur(null)} footer={<>
      {(cur.status === 'draft' || cur.status === 'rejected') && <><button className="btn d" onClick={() => setAct({ kind: 'delete', s: cur })}>删除</button><button className="btn p" onClick={() => setAct({ kind: 'submit', s: cur })}>提交复核</button></>}
      {cur.status === 'pending_review' && <><button className="btn d" onClick={() => setAct({ kind: 'reject', s: cur })}>驳回</button>
        <button className="btn p" disabled={me.user === cur.createdBy || me.user === cur.submittedBy} title={me.user === cur.createdBy || me.user === cur.submittedBy ? '录入人/提交人不能复核' : ''} onClick={() => setAct({ kind: 'approve', s: cur })}>复核通过并发布</button></>}
      {cur.status === 'published' && <button className="btn d" onClick={() => setAct({ kind: 'delete', s: cur })}>撤下（删除）</button>}</>}>
      <dl className="kv"><dt>快照时间</dt><dd>{ft(cur.snapshotAt)}</dd><dt>备注</dt><dd style={pre}>{cur.note || '-'}</dd><dt>录入</dt><dd>{cur.createdBy} · {ft(cur.createdAt)}</dd><dt>提交</dt><dd>{cur.submittedBy || '-'}</dd><dt>复核</dt><dd>{cur.reviewedBy || '-'}{cur.reviewNote ? '：' + cur.reviewNote : ''}</dd></dl>
      <table className="t" style={{ marginTop: 12 }}><thead><tr><th>资产</th><th className="r">用户负债</th><th className="r">平台储备</th><th className="r">储备率</th></tr></thead>
        <tbody>{cur.items.map(i => <tr key={i.asset}><td>{i.asset}</td><td className="r num">{num(i.liabilities, 4)}</td><td className="r num">{num(i.reserves, 4)}</td><td className={'r num ' + (i.ratio && +i.ratio < 1 ? 'down' : 'up')}>{ratio(i.ratio)}</td></tr>)}</tbody></table></Drawer>}
    {act && <Confirm title={{ submit: '提交复核', approve: '复核通过并发布到前台', reject: '驳回快照', delete: '删除快照' }[act.kind]} danger={act.kind !== 'submit' && act.kind !== 'approve'}
      rows={[['快照时间', ft(act.s.snapshotAt)], ['资产', act.s.items.map(i => `${i.asset} ${ratio(i.ratio)}`).join(' · ')]]} onClose={() => setAct(null)}
      onOk={async ({ totpCode, reason }) => { const b = { totpCode, reason }
        if (act.kind === 'delete') await api(`proof-of-reserves/${act.s.id}`, { method: 'DELETE', body: b }); else await api(`proof-of-reserves/${act.s.id}/${act.kind}`, { method: 'POST', body: { ...b, note: reason } })
        setCur(null); q.reload(); return '已完成' }} />}
    {add && <PorCreate onClose={() => setAdd(false)} onDone={q.reload} />}</>
}
function PorCreate({ onClose, onDone }) {
  const [rows, setRows] = useState([{ asset: 'USDT', liabilities: '', reserves: '' }, { asset: 'BTC', liabilities: '', reserves: '' }]); const [at, setAt] = useState(''); const [note, setNote] = useState('')
  const set = (i, k) => e => setRows(rows.map((r, j) => j === i ? { ...r, [k]: e.target.value } : r))
  return <Confirm title="新建储备快照（草稿）" width={640} onClose={onClose} okText="保存草稿" onOk={async ({ totpCode, reason }) => {
    const its = rows.filter(r => r.asset.trim()).map(r => ({ asset: r.asset.trim().toUpperCase(), liabilities: r.liabilities.trim(), reserves: r.reserves.trim() }))
    if (!its.length || its.some(r => !/^\d+(\.\d+)?$/.test(r.liabilities) || !/^\d+(\.\d+)?$/.test(r.reserves))) throw new Error('每行需填写资产与非负数字的负债/储备')
    const body = { totpCode, reason, note: note.trim(), items: its }; if (at) body.snapshotAt = new Date(at).toISOString()
    await api('proof-of-reserves', { method: 'POST', body }); onDone(); return '草稿已保存，提交后需第二人复核' }}>
    <F l="快照时间"><input className="in" type="datetime-local" value={at} onChange={e => setAt(e.target.value)} /><span className="muted" style={{ marginLeft: 8 }}>留空 = 现在</span></F>
    <F l="备注"><input className="in" style={{ width: 360 }} value={note} onChange={e => setNote(e.target.value)} placeholder="数据来源，如：链上地址余额 + 托管对账单" /></F>
    <table className="t"><thead><tr><th>资产</th><th>用户负债</th><th>平台储备</th><th /></tr></thead><tbody>{rows.map((r, i) => <tr key={i}>
      <td><input className="in" style={{ width: 90 }} value={r.asset} onChange={set(i, 'asset')} /></td><td><input className="in" value={r.liabilities} onChange={set(i, 'liabilities')} /></td>
      <td><input className="in" value={r.reserves} onChange={set(i, 'reserves')} /></td><td><a className="btn g" onClick={() => setRows(rows.filter((_, j) => j !== i))}>删</a></td></tr>)}</tbody></table>
    <button className="btn g" onClick={() => setRows([...rows, { asset: '', liabilities: '', reserves: '' }])}>+ 添加资产</button>
  </Confirm>
}

/* ===================== 偿付能力 ===================== */
export function Solvency() {
  const can = useCan(); const q = useApi('solvency/latest'); const [run, setRun] = useState(false)
  const rep = q.data && q.data.report; const lines = (rep && rep.lines) || []
  const bad = lines.filter(l => !l.ok)
  return <><PageHead title="偿付能力" desc="每日自动对账（链上/托管持有 ≥ 用户负债 − 虚拟种子），短缺时 Telegram 告警；数据来自 wallet / asset / futures 服务">
    <button className="btn" onClick={q.reload}>刷新</button>{can('mm:edit') && <button className="btn p" onClick={() => setRun(true)}>立即对账</button>}</PageHead>
    <ApiState q={q}>{!rep ? <div className="card pad"><Empty text="尚无对账报告（每日自动运行；也可点「立即对账」）" /></div> : <>
      <div className="grid g4" style={{ marginBottom: 12 }}>
        <Kpi l="结论" v={bad.length ? '存在短缺' : (rep.errors || []).length ? '数据不完整' : '充足'} up={!bad.length && !(rep.errors || []).length} d={`${lines.length} 个资产 · 短缺 ${bad.length}`} />
        <Kpi l="对账时间" v={ft(rep.checkedAt || q.data.createdAt)} d={'触发：' + (q.data.trigger || '-')} />
        <Kpi l="钱包模式" v={rep.walletMode || '-'} /><Kpi l="数据源错误" v={(rep.errors || []).length} up={!(rep.errors || []).length} /></div>
      {(rep.errors || []).length > 0 && <div className="banner bad">部分数据源失败，结论可能不完整：{rep.errors.join('；')}</div>}
      <div className="card"><div className="tw"><table className="t"><thead><tr><th>资产</th><th className="r">持有（链上/托管）</th><th className="r">用户余额</th><th className="r">其中做市</th><th className="r">虚拟种子</th>
        <th className="r">闪兑净头寸</th><th className="r">人工调账净额</th><th className="r">应付</th><th className="r">盈余</th><th>状态</th></tr></thead>
        <tbody>{lines.map(l => <tr key={l.asset}><td><b>{l.asset}</b></td><td className="r num">{num(l.holdings, 4)}</td><td className="r num">{num(l.userBalances, 4)}</td><td className="r num">{num(l.marketMaker, 4)}</td>
          <td className="r num">{num(l.virtualSeed, 4)}</td><td className="r num">{num(l.netPosition, 4)}</td><td className="r num">{num(l.adminAdjustNet, 4)}{l.unbackedAdjust && <Tag k="wn">无链上背书</Tag>}</td>
          <td className="r num">{num(l.required, 4)}</td><td className={'r num ' + (+l.surplus < 0 ? 'down' : 'up')}>{num(l.surplus, 4)}</td><td>{l.ok ? <Tag k="ok">充足</Tag> : <Tag k="bad">短缺</Tag>}</td></tr>)}
          {!lines.length && <tr><td colSpan="10" className="muted">报告中没有资产行</td></tr>}</tbody></table></div></div></>}</ApiState>
    {run && <Confirm title="立即运行偿付对账" noCode note="读取钱包持有与各服务负债并保存一份新报告；如有短缺会发送 Telegram 告警。" onClose={() => setRun(false)}
      onOk={async () => { await api('solvency/run', { method: 'POST' }); q.reload(); return '对账完成' }} />}</>
}
