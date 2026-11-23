import { useState } from 'react'
import { Confirm, DataTable, Drawer, Modal, PageHead, Tabs, Tag, eq, has, useApp, useFilter } from '../ui.jsx'
import { allowed } from '../menu.js'
import { ApiState } from '../ui.jsx'
import { Profile30d } from './Common.jsx'
import { API_BASE, getToken, api, ft, items, KYC_LV, KYC_ST, num, USER_ST, useApi } from '../api.js'
export const mapUser = u => ({ ...u, id: u.uid, nick: u.nickname || '-', email: u.email || '-', phone: u.phone || '-', country: u.countryCode || '-', kyc: KYC_LV(u.kycLevel), status: USER_ST[u.status] || u.status, raw: u.status, tfa: u.totpEnabled, reg: ft(u.createdAt) })
const stTag = s => <Tag k={s === '正常' ? 'ok' : s === '冻结' ? 'bad' : 'wn'}>{s}</Tag>

export function FreezeModal({ user, onClose, onDone }) {
  const { say } = useApp(); const [reason, setReason] = useState(''); const [cat, setCat] = useState(''); const [ticket, setTicket] = useState(''); const [note, setNote] = useState(''); const [code, setCode] = useState(''); const [err, setErr] = useState('')
  const unfreeze = !!user.raw && user.raw !== 'active'
  // 后端只接收一个 reason 字段（写入用户状态说明 + 审计日志）；分类/工单号/备注拼进 reason 一并留痕
  const full = [cat && `[${cat}]`, reason.trim(), ticket.trim() && `工单 ${ticket.trim()}`, note.trim() && `备注：${note.trim()}`].filter(Boolean).join(' ')
  const submit = async () => { setErr(''); try { await api(`users/${user.uid}/${unfreeze ? 'unfreeze' : 'freeze'}`, { method: 'POST', body: { reason: full, totpCode: code } }); say(unfreeze ? '已解冻' : '已冻结'); onDone && onDone(); onClose() } catch (e) { setErr(e.message) } }
  return <Modal title={`${unfreeze ? '解冻账户' : '冻结账户'} · UID ${user.uid}`} onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button><button className="btn d" disabled={!reason.trim() || code.length < 6} onClick={submit}>{unfreeze ? '提交解冻' : '提交冻结'}</button></>}>
    <div className="banner"><div>高危操作：需填写原因 + Google 验证码，全程写入审计日志。<b>{unfreeze ? '解冻后账户恢复为正常状态。' : '当前仅支持整户冻结（状态 → 冻结），不支持按功能范围或期限冻结；到期不会自动解冻。'}</b>系统不会自动通知用户，如需告知请在「通知用户」中单独发送。</div></div>{err && <div className="banner bad">{err}</div>}
    <div className="fr"><label>原因分类</label><select className="sel" value={cat} onChange={e => setCat(e.target.value)}><option value="">（可选）</option>{(unfreeze ? ['核实无风险', '司法协查解除', '用户申诉通过', '误操作更正'] : ['疑似盗号', '司法协查', 'AML 命中', '羊毛党/刷量', '用户自助申请']).map(x => <option key={x}>{x}</option>)}</select></div>
    <div className="fr"><label>原因</label><input className="in" placeholder="必填，将记录为状态说明" value={reason} onChange={e => setReason(e.target.value)} /></div>
    <div className="fr"><label>工单号</label><input className="in" placeholder="（可选）如 CS-20261102-0012" value={ticket} onChange={e => setTicket(e.target.value)} /></div>
    <div className="fr" style={{ alignItems: 'start' }}><label>备注</label><textarea className="in" rows="2" placeholder="（可选）内部可见" value={note} onChange={e => setNote(e.target.value)} /></div>
    <div className="fr"><label>Google 验证码</label><input className="in" maxLength={6} placeholder="6 位动态码" value={code} onChange={e => setCode(e.target.value.replace(/\D/g, ''))} /></div>
    {full && <div className="muted" style={{ fontSize: 12 }}>将记录为：{full}</div>}
  </Modal>
}

export function NotifyModal({ uid, onClose }) {
  const { say } = useApp(); const [kind, setKind] = useState('system'); const [title, setTitle] = useState(''); const [body, setBody] = useState(''); const [email, setEmail] = useState(false); const [code, setCode] = useState(''); const [err, setErr] = useState(''); const [busy, setBusy] = useState(false)
  const send = async () => { setBusy(true); setErr(''); try { await api(`users/${uid}/notifications`, { method: 'POST', body: { kind, title: title.trim(), body: body.trim(), email, totpCode: code } }); say('已发送站内信'); onClose() } catch (e) { setErr(e.message) } finally { setBusy(false) } }
  return <Modal title={`通知用户 · UID ${uid}`} onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button><button className="btn p" disabled={!title.trim() || !body.trim() || code.length < 6 || busy} onClick={send}>{busy ? '发送中…' : '发送'}</button></>}>
    <div className="banner">发送到该用户的站内信（可选同时发邮件），需 Google 验证码，写入审计日志。内容用户可见，请勿包含内部信息。</div>{err && <div className="banner bad">{err}</div>}
    <div className="fr"><label>类型</label><select className="sel" value={kind} onChange={e => setKind(e.target.value)}><option value="system">系统通知</option><option value="security">安全通知</option><option value="activity">活动通知</option><option value="trade">交易通知</option></select></div>
    <div className="fr"><label>标题</label><input className="in" maxLength={60} value={title} onChange={e => setTitle(e.target.value)} placeholder="必填，≤ 60 字" /></div>
    <div className="fr" style={{ alignItems: 'start' }}><label>内容</label><textarea className="in" rows="4" maxLength={1000} value={body} onChange={e => setBody(e.target.value)} placeholder="必填" /></div>
    <div className="fr"><label>同时发邮件</label><label><input type="checkbox" checked={email} onChange={e => setEmail(e.target.checked)} /> 发送到用户注册邮箱</label></div>
    <div className="fr"><label>Google 验证码</label><input className="in" maxLength={6} placeholder="6 位动态码" value={code} onChange={e => setCode(e.target.value.replace(/\D/g, ''))} /></div>
  </Modal>
}

function SecurityRows({ uid }) {
  const sec = useApi('users/' + uid + '/security'); const d = sec.data
  if (!d) return sec.error ? <><dt>提现锁</dt><dd className="muted">读取失败</dd></> : null
  return <><dt>提现锁</dt><dd>{d.withdrawLocked ? <><Tag k="wn">锁定至 {ft(d.withdrawLockedUntil)}</Tag> <span className="muted">{d.withdrawLockReason || ''}</span></> : <span className="muted">未锁定</span>}</dd>
    <dt>资金密码</dt><dd>{d.fundPasswordSet ? '已设置' : '未设置'}{d.fundPasswordLockedUntil && new Date(d.fundPasswordLockedUntil) > new Date() ? <Tag k="wn">输错锁定至 {ft(d.fundPasswordLockedUntil)}</Tag> : ''}</dd></>
}

export function UserDrawer({ u, onClose, onChanged }) {
  const bal = useApi('users/' + u.uid + '/balances')
  const [tab, setTab] = useState('base'); const [fz, setFz] = useState(false); const [nt, setNt] = useState(false); const { me } = useApp(); const can = p => allowed(me && me.perms, p)
  const pos = useApi(tab === 'pos' ? 'futures/positions' : null, { uid: u.uid, size: 200 }); const dev = useApi(tab === 'log' ? `users/${u.uid}/devices` : null)
  return <Drawer width={760} title={<>{u.nick} <span className="muted" style={{ fontWeight: 400 }}>UID {u.uid}</span></>} extra={<>{stTag(u.status)}</>} onClose={onClose}
    footer={<>{can('content:edit') && <button className="btn" onClick={() => setNt(true)}>通知用户</button>}<button className="btn" onClick={() => { location.hash = '#/adjust?uid=' + encodeURIComponent(u.uid) }}>人工调账</button><button className="btn d" onClick={() => setFz(true)}>{u.raw && u.raw !== 'active' ? '解冻' : '冻结账户'}</button></>}>
    <div className="grid g4" style={{ marginBottom: 12 }}>{[['总资产估值', '$' + num(items(bal.data).reduce((a, b) => a + (+b.valuationUsd || 0), 0))]].map(([l, v]) => <div key={l} className="card kpi"><div className="l">{l}</div><div className="v num" style={{ fontSize: 16 }}>{v}</div></div>)}</div>
    <div className="sec" style={{ marginTop: 0 }}>近 30 日资金画像</div><div style={{ marginBottom: 12 }}><Profile30d uid={u.uid} /></div>
    <div className="card"><Tabs value={tab} onChange={setTab} items={[['base', '基本信息'], ['asset', '资产'], ['pos', '持仓'], ['log', '登录/设备']]} />
      <div style={{ padding: 14 }}>
        {tab === 'base' && <dl className="kv"><dt>昵称</dt><dd>{u.nick}</dd><dt>邮箱</dt><dd>{u.email} {u.emailVerified ? <Tag k="ok">已验证</Tag> : <Tag k="wn">未验证</Tag>}</dd><dt>手机</dt><dd>{u.phone}</dd><dt>国家/地区</dt><dd>{u.country}</dd>
          <dt>KYC</dt><dd>{u.kyc}</dd><dt>2FA</dt><dd>{u.tfa ? <Tag k="ok">Google 验证器</Tag> : <Tag k="wn">未开启</Tag>}</dd><dt>状态说明</dt><dd>{u.statusReason || '-'}</dd><SecurityRows uid={u.uid} /><dt>注册时间</dt><dd>{u.reg}</dd></dl>}
        {tab === 'asset' && <ApiState q={bal}><table className="t"><thead><tr><th>币种</th><th className="r">可用</th><th className="r">冻结</th><th className="r">合计</th><th className="r">估值 USD</th></tr></thead><tbody>
          {items(bal.data).map(r => <tr key={r.asset}><td>{r.asset}</td><td className="r num">{num(r.free, 4)}</td><td className="r num">{num(r.locked, 4)}</td><td className="r num">{num(r.total, 4)}</td><td className="r num">{num(r.valuationUsd)}</td></tr>)}</tbody></table></ApiState>}
        {tab === 'pos' && <ApiState q={pos}><table className="t"><thead><tr><th>合约</th><th>方向</th><th>杠杆</th><th className="r">数量</th><th className="r">开仓价</th><th className="r">未实现盈亏</th></tr></thead><tbody>
          {items(pos.data).map(p => <tr key={p.symbol + p.positionSide}><td>{p.symbol}</td><td className={+p.qty > 0 ? 'up' : 'down'}>{+p.qty > 0 ? '多' : '空'}</td><td>{p.leverage}x</td><td className="r num">{p.qty}</td><td className="r num">{num(p.entryPrice, 4)}</td><td className={'r num ' + (+p.unrealizedPnl >= 0 ? 'up' : 'down')}>{num(p.unrealizedPnl)}</td></tr>)}
          {!items(pos.data).length && <tr><td colSpan="6" className="muted">无持仓</td></tr>}</tbody></table></ApiState>}
        {tab === 'log' && <ApiState q={dev}><table className="t"><thead><tr><th>设备</th><th>IP</th><th>UA</th><th>首次</th><th>最近</th></tr></thead><tbody>
          {items(dev.data).map(d => <tr key={d.id}><td>{d.name || '-'}</td><td>{d.ip || '-'}</td><td style={{ maxWidth: 260, wordBreak: 'break-all' }}>{d.userAgent || '-'}</td><td>{ft(d.firstSeen)}</td><td>{ft(d.lastSeen)}</td></tr>)}
          {!items(dev.data).length && <tr><td colSpan="5" className="muted">无设备记录</td></tr>}</tbody></table></ApiState>}
      </div></div>
    {fz && <FreezeModal user={u} onClose={() => setFz(false)} onDone={onChanged} />}
    {nt && <NotifyModal uid={u.uid} onClose={() => setNt(false)} />}
  </Drawer>
}

const csvCell = v => { const t = v == null ? '' : String(v); return /[",\n]/.test(t) ? '"' + t.replace(/"/g, '""') + '"' : t }
export function downloadCsv(name, cols, rows) {
  const body = [cols.map(c => csvCell(c[0])).join(','), ...rows.map(r => cols.map(c => csvCell(typeof c[1] === 'function' ? c[1](r) : r[c[1]])).join(','))].join('\n')
  const a = document.createElement('a'); a.href = URL.createObjectURL(new Blob(['\ufeff' + body], { type: 'text/csv;charset=utf-8' })); a.download = name; a.click(); setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}
const USER_CSV = [['UID', 'uid'], ['昵称', 'nick'], ['邮箱(脱敏)', 'email'], ['手机(脱敏)', 'phone'], ['国家', 'country'], ['KYC', 'kyc'], ['2FA', r => r.tfa ? '已开启' : '未开启'], ['状态', 'status'], ['注册时间', 'reg']]
const PS = 50
export function Users() {
  const { q, say } = useApp(); const init = (q && (q.q || q.uid)) || ''
  const [kw, setKw] = useState(init); const [app, setApp] = useState(init); const [st, setSt] = useState(''); const [kyc, setKyc] = useState(''); const [page, setPage] = useState(1)
  const r = useApi('users', { q: app, status: st, page, size: PS }); const all = items(r.data).map(mapUser)
  const list = kyc === '' ? all : all.filter(u => String(u.kycLevel ?? '') === kyc)
  const total = r.data ? (r.data.total ?? all.length) : 0; const pages = Math.max(1, Math.ceil(total / PS))
  const [cur, setCur] = useState(null)
  const run = () => { setApp(kw.trim()); setPage(1) }
  const exp = (rows, tag) => { downloadCsv(`users-${tag}-${new Date().toISOString().slice(0, 10)}.csv`, USER_CSV, rows); say(`已导出 ${rows.length} 条（字段已脱敏）`) }
  const hasCountry = all.some(u => u.country && u.country !== '-')
  return <><PageHead title="用户列表" desc={`共 ${r.data ? total : '-'} 名用户 · 支持 UID / 昵称 / 邮箱检索（敏感字段已脱敏）`}><button className="btn" disabled={!list.length} onClick={() => exp(list, 'page' + page)}>导出本页 CSV</button></PageHead>
    <ApiState q={r}><DataTable rows={list} select rowKey="uid" onRow={setCur} pager={<><span>共 {total} 条 · 第 {page}/{pages} 页 · 每页 {PS} 条{kyc !== '' ? '（KYC 筛选仅作用于本页）' : ''}</span><span className="sp" />
        <b style={{ opacity: page > 1 ? 1 : .4 }} onClick={() => page > 1 && setPage(page - 1)}>‹ 上一页</b><b style={{ opacity: page < pages ? 1 : .4 }} onClick={() => page < pages && setPage(page + 1)}>下一页 ›</b></>}
      cols={[
      { t: 'UID', k: 'uid', render: r => <span className="pri">{r.uid}</span> }, { t: '昵称', k: 'nick' }, { t: '邮箱', k: 'email' }, ...(hasCountry ? [{ t: '国家', k: 'country' }] : []),
      { t: 'KYC', k: 'kyc', render: r => <Tag k={r.kyc.startsWith('L2') ? 'ok' : r.kyc.startsWith('L1') ? 'in' : ''}>{r.kyc}</Tag> }, { t: '2FA', render: r => r.tfa ? <Tag k="ok">已开启</Tag> : <Tag k="wn">未开启</Tag> }, { t: '状态', k: 'status', render: r => stTag(r.status) },
      { t: '注册时间', k: 'reg' }, { t: '操作', render: r => <a className="btn g">详情</a> }]}
      filters={<><input className="in" style={{ width: 220 }} placeholder="UID / 昵称 / 邮箱（回车查询）" value={kw} onChange={e => setKw(e.target.value)} onKeyDown={e => e.key === 'Enter' && run()} /><button className="btn" onClick={run}>查询</button>
        <select className="sel" value={st} onChange={e => { setSt(e.target.value); setPage(1) }}><option value="">全部状态</option><option value="active">正常</option><option value="frozen">冻结</option><option value="withdraw_restricted">限制提现</option></select>
        <select className="sel" value={kyc} onChange={e => setKyc(e.target.value)}><option value="">全部 KYC</option><option value="0">未认证</option><option value="1">L1 基础</option><option value="2">L2 高级</option></select></>}
      toolbar={(sel) => <button className="btn" disabled={!sel.length} onClick={() => exp(list.filter(u => sel.includes(u.uid)), 'selected')}>导出所选 CSV</button>} /></ApiState>
    {cur && <UserDrawer u={cur} onClose={() => setCur(null)} onChanged={() => { setCur(null); r.reload() }} />}</>
}

const mapKyc = k => ({ ...k, uid: k.userId, name: [k.firstName, k.lastName].filter(Boolean).join(' '), level: KYC_LV(k.level), doc: k.idType || '-', submit: ft(k.submittedAt), status: KYC_ST[k.status] || k.status })
function DocImg({ d }) {
  const [src, setSrc] = useState(null); const [err, setErr] = useState('')
  const load = async () => { try { const tok = getToken()
    const res = await fetch(API_BASE + 'kyc/documents/' + (d.url ? d.url.split('/').pop() : d.fileId), { headers: { Authorization: 'Bearer ' + tok } }); if (!res.ok) throw new Error('HTTP ' + res.status); setSrc(URL.createObjectURL(await res.blob())) } catch (e) { setErr(e.message) } }
  return <div className="idimg" onClick={src ? undefined : load} style={{ cursor: src ? 'default' : 'pointer', overflow: 'hidden' }}>{src ? <img src={src} style={{ maxWidth: '100%', maxHeight: '100%' }} /> : err ? '加载失败 ' + err : `${{ front: '证件正面', back: '证件反面', selfie: '活体自拍' }[d.side] || d.side || '证件'}（点击查看，写入审计）`}</div>
}
export function Kyc() {
  const { say } = useApp(); const [tab, setTab] = useState('pending'); const [cur, setCur] = useState(null); const [rej, setRej] = useState(false); const [reason, setReason] = useState(''); const [err, setErr] = useState('')
  const r = useApi('kyc/applications', { status: tab === 'all' ? '' : tab }); const rows = items(r.data).map(mapKyc)
  const det = useApi(cur ? 'kyc/applications/' + cur.id : null); const d = det.data || {}
  const decide = async (dec) => { setErr(''); try { await api(`kyc/applications/${cur.id}/${dec}`, { method: 'POST', body: { reason } }); say(dec === 'approve' ? '已通过，用户升级为 ' + cur.level : '已驳回并通知用户'); setRej(false); setCur(null); r.reload() } catch (e) { setErr(e.message) } }
  return <><PageHead title="KYC 审核" desc="L1：证件 OCR + 人脸比对；L2：地址证明 + 人工复核 · SLA 2 小时"><button className="btn" onClick={r.reload}>刷新</button></PageHead>
    <div className="card" style={{ marginBottom: 0 }}><Tabs value={tab} onChange={setTab} items={[['pending', '待审核', tab === 'pending' ? rows.length : undefined], ['approved', '已通过'], ['rejected', '已驳回'], ['all', '全部']]} /></div>
    <ApiState q={r}><DataTable rows={rows} onRow={c => { setErr(''); setCur(c) }}
      cols={[{ t: '申请单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '等级', k: 'level' }, { t: '证件', k: 'doc' }, { t: '国家', k: 'country' }, { t: '姓名', k: 'name' },
        { t: '提交时间', k: 'submit' }, { t: '状态', k: 'status', render: r => <Tag k={r.status === '已通过' ? 'ok' : r.status === '已驳回' ? 'bad' : 'wn'}>{r.status}</Tag> }, { t: '驳回原因', render: r => r.reason || '-' }]} /></ApiState>
    {cur && <Drawer width={720} title={`KYC 审核 · ${cur.id}`} extra={<Tag k="wn">{cur.status}</Tag>} onClose={() => setCur(null)}
      footer={cur.status === '待审核' ? <><button className="btn d" onClick={() => setRej(true)}>驳回</button><button className="btn p" onClick={() => decide('approve')}>通过</button></> : null}>
      {err && <div className="banner bad">{err}</div>}
      <div className="sec">证件材料</div><ApiState q={det}><div className="grid g3">{(d.documents || []).map(x => <DocImg key={x.fileId || x.side} d={x} />)}{!(d.documents || []).length && <span className="muted">无证件文件</span>}</div></ApiState>
      <div className="sec">用户填写</div><dl className="kv"><dt>姓名</dt><dd>{cur.name}</dd><dt>证件类型</dt><dd>{cur.idType}</dd><dt>证件号</dt><dd>{cur.idNumber}</dd><dt>出生日期</dt><dd>{cur.dateOfBirth}</dd><dt>国家</dt><dd>{cur.country}</dd><dt>审核时间</dt><dd>{ft(cur.reviewedAt)}</dd></dl>
      
      {rej && <Modal title="驳回原因" onClose={() => setRej(false)} footer={<><button className="btn" onClick={() => setRej(false)}>取消</button><button className="btn d" disabled={!reason.trim()} onClick={() => decide('reject')}>确认驳回</button></>}>
        {['证件照片模糊/反光', '证件信息与填写不一致', '人脸比对未通过', '证件已过期', '不支持的国家/地区'].map(x => <label key={x} className="chk"><input type="radio" name="rj" onChange={() => setReason(x)} /> {x}</label>)}
        <textarea className="in" rows="3" placeholder="对用户展示的说明（必填）" value={reason} onChange={e => setReason(e.target.value)} /></Modal>}
    </Drawer>}</>
}

export function RiskList() {
  const [uid, setUid] = useState(''); const q = useApi('risk-users', { uid: uid.trim(), size: 200 }); const [cur, setCur] = useState(null)
  return <><PageHead title="风险名单" desc="被冻结、限制提现或停用的账户（来自用户状态）；可在此解除" />
    <div className="card"><div className="filters"><input className="in" placeholder="UID" value={uid} onChange={e => setUid(e.target.value)} /></div></div>
    <ApiState q={q}><DataTable rowKey="uid" rows={items(q.data).map(mapUser)} cols={[{ t: 'UID', k: 'uid' }, { t: '昵称', k: 'nick' }, { t: '邮箱', k: 'email' }, { t: 'KYC', k: 'kyc' },
      { t: '状态', render: r => stTag(r.status) }, { t: '原因', render: r => r.reason || '-' }, { t: '变更时间', render: r => ft(r.changedAt) },
      { t: '操作', render: r => <a className="btn g" onClick={e => { e.stopPropagation(); setCur(r) }}>解除</a> }]} /></ApiState>
    {cur && <FreezeModal user={cur} onClose={() => setCur(null)} onDone={q.reload} />}</>
}
