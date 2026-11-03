/* 已接入 admin/v1 真实接口的页面（沿用原型的布局与组件） */
import { useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Kpi, Modal, PageHead, Tabs, Tag, has, useApp, useFilter } from '../ui.jsx'
import { api, ft, items, num, roleCn, ROLE_CN, useApi } from '../api.js'

const ST = { active: '生效中', pending_finance: '待财务审批', rejected: '已驳回', executed: '已发放', failed: '失败', trading: '交易中', halted: '暂停交易', pending: '待审批', approved: '已通过', applied: '已生效', disabled: '已停用', canceled: '已取消' }
const st = s => <Tag k={['active', 'executed', 'trading', 'approved', 'applied'].includes(s) ? 'ok' : ['rejected', 'failed', 'halted', 'disabled'].includes(s) ? 'bad' : 'in'}>{ST[s] || s}</Tag>
const pct = v => v == null || v === '' ? '-' : (Number(v) * 100).toFixed(4).replace(/\.?0+$/, '') + '%'
const F = ({ l, children }) => <div className="fr"><label>{l}</label>{children}</div>

/* 现货交易对 */
export function Pairs() {
  const q = useApi('symbols'); const [m, setM] = useState(null); const [f, setF] = useState({})
  const [rows, set] = useFilter(items(q.data), (r, x) => has(r.symbol, x.k) && (!x.s || r.status === x.s))
  const v = k => e => setF({ ...f, [k]: e.target.value })
  return <><PageHead title="现货交易对" desc="交易对上下架、价格/数量精度、最小下单额；仅现货撮合，无杠杆"><button className="btn p" onClick={() => { setF({ quote: 'USDT', pricePrecision: '2', quantityPrecision: '4', minNotional: '5' }); setM({ add: 1 }) }}>+ 新增交易对</button></PageHead>
    <ApiState q={q}><DataTable rows={rows} rowKey="symbol" filters={<><input className="in" placeholder="交易对" onChange={set('k')} /><select className="sel" onChange={set('s')}><option value="">全部状态</option><option value="trading">交易中</option><option value="halted">暂停交易</option></select></>}
      cols={[{ t: '交易对', render: r => <b>{r.symbol}</b> }, { t: '基础 / 计价', render: r => r.base + ' / ' + r.quote }, { t: '价格精度', k: 'pricePrecision' }, { t: '数量精度', k: 'quantityPrecision' }, { t: '最小数量', k: 'minQuantity' }, { t: '最小下单额', render: r => r.minNotional + ' ' + r.quote },
        { t: '状态', render: r => st(r.status) }, { t: '操作', render: r => <a className="btn g" onClick={e => { e.stopPropagation(); setM(r) }}>{r.status === 'trading' ? '暂停' : '恢复'}</a> }]} /></ApiState>
    {m && !m.add && <Confirm danger={m.status === 'trading'} title={(m.status === 'trading' ? '暂停交易 · ' : '恢复交易 · ') + m.symbol} rows={[['交易对', m.symbol], ['当前状态', ST[m.status] || m.status]]} noCode onClose={() => setM(null)}
      onOk={async ({ reason }) => { await api('symbols/' + m.symbol, { method: 'PATCH', body: { status: m.status === 'trading' ? 'halted' : 'trading', reason } }); q.reload(); return '已提交' }} />}
    {m && m.add && <Confirm title="新增交易对" noCode noReason note="新交易对默认为「暂停交易」状态，确认参数后再恢复交易。" onClose={() => setM(null)} okText="提交"
      onOk={async () => { await api('symbols', { method: 'POST', body: { symbol: (f.base || '') + (f.quote || ''), base: f.base, quote: f.quote, pricePrecision: +f.pricePrecision, quantityPrecision: +f.quantityPrecision, minQuantity: f.minQuantity, minNotional: f.minNotional } }); q.reload(); return '交易对已创建' }}>
      <F l="基础币"><input className="in" value={f.base || ''} onChange={v('base')} placeholder="NEWT" /></F><F l="计价币"><input className="in" value={f.quote || ''} onChange={v('quote')} /></F>
      <F l="价格精度"><input className="in" value={f.pricePrecision} onChange={v('pricePrecision')} /></F><F l="数量精度"><input className="in" value={f.quantityPrecision} onChange={v('quantityPrecision')} /></F>
      <F l="最小数量"><input className="in" value={f.minQuantity || ''} onChange={v('minQuantity')} /></F><F l="最小下单额"><input className="in" value={f.minNotional} onChange={v('minNotional')} /></F></Confirm>}
    </>
}

/* 手续费：当前费率 + 变更单（双人审批、公告期） */
export function Fees() {
  const fees = useApi('fees'); const ch = useApi('fee-changes'); const { me } = useApp(); const [t, setT] = useState('rate'); const [m, setM] = useState(null); const [f, setF] = useState({})
  const cur = (fees.data && fees.data.current) || {}; const list = items(ch.data); const pend = list.filter(c => c.status === 'pending')
  const desc = o => o ? Object.entries(o).map(([k, v]) => (k === 'spotMaker' ? 'Maker ' : k === 'spotTaker' ? 'Taker ' : k + ' ') + pct(v)).join(' · ') : '-'
  const reload = () => { fees.reload(); ch.reload() }
  return <><PageHead title="手续费配置" desc={`全站统一费率，不设 VIP 等级；变更须双人审批，生效前 ${fees.data ? fees.data.noticeHours : 24}h 前台公告`}>
    <button className="btn" onClick={() => setT('log')}>变更记录</button><button className="btn p" onClick={() => { setF({ spotMaker: cur.spotMaker, spotTaker: cur.spotTaker }); setM({ submit: 1 }) }}>发起费率变更</button></PageHead>
    <ApiState q={fees}><div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="现货 Maker" v={pct(cur.spotMaker)} d="按成交额 · 收取到手币种" /><Kpi l="现货 Taker" v={pct(cur.spotTaker)} /><Kpi l="公告期" v={(fees.data && fees.data.noticeHours) + 'h'} /><Kpi l="待审批变更单" v={pend.length} /></div></ApiState>
    <div className="card"><Tabs value={t} onChange={setT} items={[['rate', '交易费率'], ['log', '变更记录', list.length]]} /></div>
    {t === 'rate' && <DataTable rows={[{ id: 'm', side: 'Maker', k: 'spotMaker' }, { id: 't', side: 'Taker', k: 'spotTaker' }]} cols={[{ t: '市场', render: () => <b>现货</b> }, { t: '方向', k: 'side' }, { t: '费率', r: 1, render: r => <span className="pri num" style={{ fontWeight: 600 }}>{pct(cur[r.k])}</span> }, { t: '计费基数', render: () => '成交额' }, { t: '状态', render: () => <Tag k="ok">生效中</Tag> }]} />}
    {t === 'log' && <ApiState q={ch}><DataTable rows={list} cols={[{ t: '变更单', render: r => <span className="pri">#{r.id}</span> }, { t: '变更前', render: r => desc(r.before) }, { t: '变更后', render: r => desc(r.payload) }, { t: '原因', k: 'reason' }, { t: '发起人', render: r => '#' + r.submittedBy },
      { t: '审批', render: r => (r.approvals || []).map(a => `${a.admin}:${a.decision === 'approve' ? '通过' : '驳回'}`).join('，') || '-' }, { t: '状态', render: r => st(r.status) }, { t: '生效时间', render: r => ft(r.effectiveAt) }, { t: '时间', render: r => ft(r.createdAt) },
      { t: '操作', render: r => r.status === 'pending' ? <><a className="btn g" onClick={() => setM({ dec: 'approve', c: r })}>通过</a><a className="btn g" onClick={() => setM({ dec: 'reject', c: r })}>驳回</a></> : r.status === 'applied' && !r.rollbackOf ? <a className="btn g" onClick={() => setM({ dec: 'rollback', c: r })}>回滚</a> : null }]} /></ApiState>}
    {m && m.submit && <Confirm danger noCode title="费率变更" okText="提交变更单" note="费率直接影响用户资金：提交后需另一名有权限的管理员审批，并在公告期后生效。" onClose={() => setM(null)}
      onOk={async ({ reason }) => { await api('fee-changes', { method: 'POST', body: { spotMaker: f.spotMaker, spotTaker: f.spotTaker, reason } }); reload(); return '变更单已提交，待审批人确认后生效' }}>
      <F l="Maker（小数）"><input className="in" value={f.spotMaker || ''} onChange={e => setF({ ...f, spotMaker: e.target.value })} /> {pct(f.spotMaker)}</F><F l="Taker（小数）"><input className="in" value={f.spotTaker || ''} onChange={e => setF({ ...f, spotTaker: e.target.value })} /> {pct(f.spotTaker)}</F></Confirm>}
    {m && m.dec && <Confirm danger={m.dec !== 'approve'} title={{ approve: '通过变更单', reject: '驳回变更单', rollback: '回滚变更' }[m.dec] + ' #' + m.c.id} rows={[['变更前', desc(m.c.before)], ['变更后', desc(m.c.payload)]]} note={`职责分离：发起人不能审批自己的变更单（当前：${me.user}）。`} onClose={() => setM(null)}
      onOk={async ({ reason, totpCode }) => { await api(`fee-changes/${m.c.id}/${m.dec}`, { method: 'POST', body: { reason, comment: reason, totpCode } }); reload(); return '已提交' }} />}</>
}

/* 奖励发放（人工补发 + 财务审批） */
export function Rewards() {
  const q = useApi('rewards'); const [m, setM] = useState(null); const [f, setF] = useState({ asset: 'USDT' })
  const [rows, set] = useFilter(items(q.data), (r, x) => has(r.userId, x.k) && (!x.s || r.status === x.s))
  return <><PageHead title="奖励发放" desc="人工补发：超过阈值的补发需财务审批后执行"><button className="btn p" onClick={() => setM({ add: 1 })}>+ 人工补发</button></PageHead>
    <ApiState q={q}><DataTable rows={rows} filters={<><input className="in" placeholder="UID" onChange={set('k')} /><select className="sel" onChange={set('s')}><option value="">全部状态</option><option value="pending_finance">待财务审批</option><option value="executed">已发放</option><option value="rejected">已驳回</option><option value="failed">失败</option></select></>}
      cols={[{ t: '发放单号', k: 'id' }, { t: 'UID', k: 'userId' }, { t: '数量', r: 1, render: r => r.amount + ' ' + r.asset }, { t: '折合 USDT', r: 1, k: 'usdtValue' }, { t: '原因', k: 'reason' }, { t: '发起人', render: r => '#' + r.createdBy },
        { t: '状态', render: r => st(r.status) }, { t: '时间', render: r => ft(r.createdAt) }, { t: '操作', render: r => r.status === 'pending_finance' ? <><a className="btn g" onClick={() => setM({ dec: 'approve', r })}>通过</a><a className="btn g" onClick={() => setM({ dec: 'reject', r })}>驳回</a></> : null }]} /></ApiState>
    {m && m.add && <Confirm title="人工补发奖励" noCode okText="提交" onClose={() => setM(null)} onOk={async ({ reason }) => { const r = await api('rewards', { method: 'POST', body: { userId: f.userId, asset: f.asset, amount: f.amount, reason } }); q.reload(); return r && r.status === 'pending_finance' ? '补发申请已提交，待财务审批' : '已发放' }}>
      <F l="UID"><input className="in" value={f.userId || ''} onChange={e => setF({ ...f, userId: e.target.value })} /></F><F l="币种"><input className="in" value={f.asset} onChange={e => setF({ ...f, asset: e.target.value })} /></F><F l="数量"><input className="in" value={f.amount || ''} onChange={e => setF({ ...f, amount: e.target.value })} /></F></Confirm>}
    {m && m.dec && <Confirm danger={m.dec === 'reject'} title={(m.dec === 'approve' ? '通过补发 #' : '驳回补发 #') + m.r.id} rows={[['UID', m.r.userId], ['数量', m.r.amount + ' ' + m.r.asset], ['原因', m.r.reason]]} onClose={() => setM(null)}
      onOk={async ({ reason, totpCode }) => { await api(`rewards/${m.r.id}/${m.dec}`, { method: 'POST', body: { reason, comment: reason, totpCode } }); q.reload(); return '已处理' }} />}</>
}

/* 卡券模板 */
const CT = { cash: '现金券', fee_discount: '手续费抵扣券', trial: '合约体验金' }
export function Coupons() {
  const q = useApi('coupons'); const [m, setM] = useState(null); const [f, setF] = useState({}); const list = items(q.data)
  const v = k => e => setF({ ...f, [k]: e.target.value })
  return <><PageHead title="卡券管理" desc="手续费抵扣券 · 合约体验金 · 现金券；预算超阈值需财务审批后才能发放"><button className="btn p" onClick={() => { setF({ type: 'cash', faceValue: '10', quantity: '1000', minSpend: '0', validDays: '30', perUser: '1' }); setM({ add: 1 }) }}>+ 新建卡券</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="发放中模板" v={list.filter(c => c.status === 'active').length} /><Kpi l="待财务审批" v={list.filter(c => c.status === 'pending_finance').length} /><Kpi l="总预算 USDT" v={num(list.filter(c => c.status === 'active').reduce((s, c) => s + (+c.budget || 0), 0), 0)} /></div>
    <ApiState q={q}><DataTable rows={list} cols={[{ t: '编号', k: 'id' }, { t: '名称', render: r => <b>{r.name}</b> }, { t: '类型', render: r => <Tag k={r.type === 'trial' ? 'gd' : r.type === 'cash' ? 'ok' : 'in'}>{CT[r.type] || r.type}</Tag> }, { t: '面额 USDT', r: 1, k: 'faceValue' },
      { t: '总量', r: 1, k: 'quantity' }, { t: '预算', r: 1, k: 'budget' }, { t: '门槛', k: 'minSpend' }, { t: '有效期', render: r => '领取后 ' + r.validDays + ' 天' }, { t: '每人限领', k: 'perUser' }, { t: '状态', render: r => st(r.status) },
      { t: '操作', render: r => r.status === 'pending_finance' ? <><a className="btn g" onClick={() => setM({ dec: 'approve', r })}>通过</a><a className="btn g" onClick={() => setM({ dec: 'reject', r })}>驳回</a></> : null }]} /></ApiState>
    {m && m.add && <Confirm title="新建卡券" noCode noReason okText="提交" onClose={() => setM(null)} onOk={async () => { const r = await api('coupons', { method: 'POST', body: { name: f.name, type: f.type, faceValue: f.faceValue, quantity: +f.quantity, minSpend: f.minSpend, validDays: +f.validDays, perUser: +f.perUser } }); q.reload(); return r && r.status === 'pending_finance' ? '已提交，待财务审批' : '卡券已生效' }}>
      <F l="名称"><input className="in" value={f.name || ''} onChange={v('name')} /></F><F l="类型"><select className="sel" value={f.type} onChange={v('type')}>{Object.entries(CT).map(([k, n]) => <option key={k} value={k}>{n}</option>)}</select></F>
      <F l="面额 USDT"><input className="in" value={f.faceValue} onChange={v('faceValue')} /></F><F l="总量"><input className="in" value={f.quantity} onChange={v('quantity')} /></F><F l="使用门槛"><input className="in" value={f.minSpend} onChange={v('minSpend')} /></F>
      <F l="领取后有效天数"><input className="in" value={f.validDays} onChange={v('validDays')} /></F><F l="每人限领"><input className="in" value={f.perUser} onChange={v('perUser')} /></F></Confirm>}
    {m && m.dec && <Confirm danger={m.dec === 'reject'} title={(m.dec === 'approve' ? '通过卡券 #' : '驳回卡券 #') + m.r.id} rows={[['名称', m.r.name], ['预算', m.r.budget + ' USDT']]} onClose={() => setM(null)}
      onOk={async ({ reason, totpCode }) => { await api(`coupons/${m.r.id}/${m.dec}`, { method: 'POST', body: { reason, comment: reason, totpCode } }); q.reload(); return '已处理' }} />}</>
}

/* 公告 */
const CAT = { notice: '公告', listing: '上新', activity: '活动', fee: '费率', maintenance: '维护' }
export function Content() {
  const q = useApi('announcements'); const [cur, setCur] = useState(null); const [err, setErr] = useState(''); const { say } = useApp()
  const [rows, set] = useFilter(items(q.data), (r, x) => has(r.title, x.k))
  const save = async () => { setErr(''); try { await api('announcements', { method: 'POST', body: { category: cur.category, title: cur.title, body: cur.body, publishAt: cur.publishAt ? new Date(cur.publishAt).toISOString() : '' } }); say('公告已创建'); setCur(null); q.reload() } catch (e) { setErr(e.message) } }
  return <><PageHead title="内容与公告" desc="前台公告 · 支持定时发布（不填发布时间即立即发布）"><button className="btn p" onClick={() => setCur({ category: 'notice', title: '', body: '' })}>+ 新建公告</button></PageHead>
    <ApiState q={q}><DataTable rows={rows} onRow={r => setCur({ ...r, view: 1 })} filters={<input className="in" placeholder="标题" onChange={set('k')} />}
      cols={[{ t: 'ID', k: 'id' }, { t: '标题', k: 'title' }, { t: '分类', render: r => CAT[r.category] || r.category }, { t: '作者', k: 'createdBy' }, { t: '发布时间', render: r => ft(r.publishAt) }, { t: '状态', render: r => new Date(r.publishAt) > new Date() ? <Tag k="in">定时</Tag> : <Tag k="ok">已发布</Tag> }]} /></ApiState>
    {cur && <Drawer width={760} title={cur.view ? '公告详情' : '新建公告'} onClose={() => setCur(null)} footer={cur.view ? null : <button className="btn p" onClick={save}>发布</button>}>
      {err && <div className="banner bad">{err}</div>}
      <F l="分类"><select className="sel" disabled={cur.view} value={cur.category} onChange={e => setCur({ ...cur, category: e.target.value })}>{Object.entries(CAT).map(([k, n]) => <option key={k} value={k}>{n}</option>)}</select></F>
      <F l="标题"><input className="in" readOnly={cur.view} value={cur.title} onChange={e => setCur({ ...cur, title: e.target.value })} /></F>
      <div className="fr" style={{ alignItems: 'start' }}><label>正文</label><textarea className="in" rows="10" readOnly={cur.view} value={cur.body} onChange={e => setCur({ ...cur, body: e.target.value })} /></div>
      {!cur.view && <F l="定时发布"><input className="in" type="datetime-local" value={cur.publishAt || ''} onChange={e => setCur({ ...cur, publishAt: e.target.value })} /></F>}</Drawer>}</>
}

/* 合伙人入驻与状态 */
export function Partners() {
  const [t, setT] = useState(''); const [cur, setCur] = useState(null); const [m, setM] = useState(null)
  const q = useApi('partners', { status: t, size: 200 }); const [rows, set] = useFilter(items(q.data), (r, x) => has(r.uid, x.k) || has(r.contact, x.k))
  return <><PageHead title="合伙人管理" desc="入驻申请审核、停用；等级按直属下级近 30 日合约交易量评定" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['', '全部'], ['pending', '入驻申请'], ['approved', '已通过'], ['rejected', '已拒绝'], ['disabled', '已停用']]} /></div>
    <ApiState q={q}><DataTable rows={rows} rowKey="uid" onRow={setCur} filters={<input className="in" placeholder="UID / 联系方式" onChange={set('k')} />}
      cols={[{ t: 'UID', render: r => <b>{r.uid}</b> }, { t: '等级', render: r => r.tier ? <Tag k="gd">{r.tier}</Tag> : '-' }, { t: '联系方式', k: 'contact' }, { t: '渠道', k: 'channel' }, { t: '规模', k: 'scale' },
        { t: '直属 30 日合约量', r: 1, num: 1, render: r => '$' + num(r.volume30d, 0) }, { t: '申请时间', render: r => ft(r.appliedAt) }, { t: '状态', render: r => st(r.status) }]} /></ApiState>
    {cur && <Drawer width={620} title={`合伙人 · ${cur.uid}`} onClose={() => setCur(null)}
      footer={cur.status === 'pending' ? <><button className="btn d" onClick={() => setM('reject')}>拒绝</button><button className="btn p" onClick={() => setM('approve')}>通过入驻</button></> : cur.status === 'approved' ? <button className="btn d" onClick={() => setM('disable')}>停用</button> : null}>
      <dl className="kv"><dt>UID</dt><dd>{cur.uid}</dd><dt>等级</dt><dd>{cur.tier || '-'}</dd><dt>联系方式</dt><dd>{cur.contact}</dd><dt>渠道 / 规模</dt><dd>{cur.channel} · {cur.scale}</dd><dt>审核备注</dt><dd>{cur.reviewNote || '-'}</dd><dt>通过时间</dt><dd>{ft(cur.approvedAt)}</dd></dl>
      {m && <Confirm danger={m !== 'approve'} title={{ approve: '通过入驻', reject: '拒绝入驻', disable: '停用合伙人' }[m]} rows={[['合伙人', cur.uid]]} onClose={() => setM(null)}
        onOk={async ({ reason, totpCode }) => { await api(`partners/${cur.uid}/${m}`, { method: 'POST', body: { reason, totpCode } }); q.reload(); setCur(null); return '已处理' }} />}</Drawer>}</>
}

/* 站内信广播 */
export function Notify() {
  const [m, setM] = useState(false); const [f, setF] = useState({ kind: 'system', title: '', body: '' })
  return <><PageHead title="消息推送" desc="站内信全量广播；交易与安全类通知由系统自动触发，不在此发送"><button className="btn p" onClick={() => setM(true)}>+ 新建推送</button></PageHead>
    <div className="card pad"><h3>说明</h3><div className="rule">当前后端仅提供「站内信全量广播」接口；推送历史、人群包、邮件 / App Push 渠道、模板管理暂无接口。</div></div>
    {m && <Confirm danger title="全量站内信广播" okText="发送" note="将发送给全部用户，且不可撤回；禁止包含外部链接与收款地址。" onClose={() => setM(false)}
      onOk={async ({ totpCode }) => { await api('notifications/broadcast', { method: 'POST', body: { kind: f.kind, title: f.title, body: f.body, totpCode } }); return '已发送' }}>
      <F l="类型"><select className="sel" value={f.kind} onChange={e => setF({ ...f, kind: e.target.value })}><option value="system">系统</option><option value="activity">活动</option><option value="announcement">公告</option></select></F>
      <F l="标题"><input className="in" value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></F><textarea className="in" rows="4" placeholder="正文" value={f.body} onChange={e => setF({ ...f, body: e.target.value })} /></Confirm>}</>
}

/* 管理员 */
export function Admins() {
  const q = useApi('admins'); const [cur, setCur] = useState(null); const [m, setM] = useState(null); const [f, setF] = useState({ roles: [] }); const [secret, setSecret] = useState(null)
  const tog = r => setF({ ...f, roles: f.roles.includes(r) ? f.roles.filter(x => x !== r) : [...f.roles, r] })
  const RolePick = () => <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>{Object.entries(ROLE_CN).map(([k, n]) => <label key={k}><input type="checkbox" checked={f.roles.includes(k)} onChange={() => tog(k)} /> {n}</label>)}</div>
  return <><PageHead title="管理员与角色" desc="RBAC：管理员 → 角色 → 权限点 · 强制 2FA · 职责分离 · 账号只停用不删除"><button className="btn p" onClick={() => { setF({ username: '', password: '', roles: [] }); setM('add') }}>+ 新增管理员</button></PageHead>
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={a => { setCur(a); setF({ roles: a.roles || [] }) }} cols={[{ t: 'ID', k: 'id' }, { t: '账号', render: r => <b>{r.username}</b> }, { t: '角色', render: r => roleCn(r.roles) },
      { t: '2FA', render: r => r.totpEnabled ? <Tag k="ok">已绑定</Tag> : <Tag k="bad">未绑定</Tag> }, { t: '创建人', k: 'createdBy' }, { t: '创建时间', render: r => ft(r.createdAt) }, { t: '状态', render: r => r.status === 'active' ? <Tag k="ok">正常</Tag> : <Tag k="bad">{r.status === 'disabled' ? '已停用' : r.status}</Tag> }]} /></ApiState>
    {cur && <Drawer title={`管理员 · ${cur.username}`} onClose={() => setCur(null)} footer={<><button className="btn d" onClick={() => setM(cur.status === 'active' ? 'disable' : 'enable')}>{cur.status === 'active' ? '停用账号' : '恢复账号'}</button><button className="btn p" onClick={() => setM('roles')}>保存角色</button></>}>
      <div className="fr" style={{ alignItems: 'start' }}><label>角色</label><RolePick /></div></Drawer>}
    {m === 'add' && <Confirm title="新增管理员" okText="创建" onClose={() => setM(null)} note="创建后会一次性返回新管理员的 2FA 绑定密钥，请当面交付。"
      onOk={async ({ totpCode }) => { const r = await api('admins', { method: 'POST', body: { username: f.username, password: f.password, roles: f.roles, totpCode } }); q.reload(); setSecret(r); return '已创建' }}>
      <F l="账号"><input className="in" value={f.username} onChange={e => setF({ ...f, username: e.target.value })} /></F><F l="初始密码"><input className="in" type="password" autoComplete="new-password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} /></F>
      <div className="fr" style={{ alignItems: 'start' }}><label>角色</label><RolePick /></div></Confirm>}
    {cur && ['roles', 'disable', 'enable'].includes(m) && <Confirm danger={m === 'disable'} title={{ roles: '修改角色', disable: '停用管理员', enable: '恢复管理员' }[m] + ' · ' + cur.username} rows={m === 'roles' ? [['新角色', roleCn(f.roles)]] : []} onClose={() => setM(null)}
      onOk={async ({ reason, totpCode }) => { await api('admins/' + cur.id, { method: 'PATCH', body: m === 'roles' ? { roles: f.roles, reason, totpCode } : { status: m === 'disable' ? 'disabled' : 'active', reason, totpCode } }); q.reload(); setCur(null); return '已保存' }} />}
    {secret && <Modal title="2FA 绑定信息（仅显示一次）" onClose={() => setSecret(null)} footer={<button className="btn p" onClick={() => setSecret(null)}>我已妥善交付</button>}>
      <div className="banner bad">关闭后无法再次查看。</div><dl className="kv">{Object.entries(secret).filter(([k]) => /totp|secret|otpauth/i.test(k)).flatMap(([k, v]) => [<dt key={k}>{k}</dt>, <dd key={k + '_v'} style={{ wordBreak: 'break-all' }}>{String(v)}</dd>])}</dl></Modal>}</>
}

/* 审计日志（服务端分页 + 过滤） */
const LV = { normal: '普通', sensitive: '敏感', high: '高危', high_risk: '高危' }
export function Audit() {
  const [f, setF] = useState({ adminId: '', target: '', from: '', to: '' }); const [page, setPage] = useState(1); const [cur, setCur] = useState(null)
  const q = useApi('audit-logs', { ...f, from: f.from && new Date(f.from).toISOString(), to: f.to && new Date(f.to + 'T23:59:59').toISOString(), page, size: 100 })
  const v = k => e => { setF({ ...f, [k]: e.target.value }); setPage(1) }; const total = q.data ? q.data.total : 0
  return <><PageHead title="审计日志" desc="所有后台操作（含查看敏感信息）不可篡改留存 · 前后值对比" />
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pageSize={20} filters={<><input className="in" placeholder="管理员 ID" value={f.adminId} onChange={v('adminId')} /><input className="in" placeholder="对象，如 user:80000001" value={f.target} onChange={v('target')} />
      <input className="in" type="date" value={f.from} onChange={v('from')} /><input className="in" type="date" value={f.to} onChange={v('to')} />
      <span className="muted" style={{ alignSelf: 'center' }}>第 {page} 页 · 共 {total} 条</span><button className="btn" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><button className="btn" disabled={page * 100 >= total} onClick={() => setPage(page + 1)}>下一页</button></>}
      cols={[{ t: '时间', render: r => ft(r.at) }, { t: '管理员', k: 'admin' }, { t: '角色', render: r => roleCn(r.roles) }, { t: '模块', k: 'module' }, { t: '操作', k: 'action' }, { t: '对象', k: 'target' }, { t: 'IP', k: 'ip' },
        { t: '级别', render: r => <Tag k={/high/.test(r.level) ? 'bad' : r.level === 'sensitive' ? 'wn' : ''}>{LV[r.level] || r.level}</Tag> }, { t: '结果', render: r => <span className={r.result === 'ok' ? 'up' : 'down'}>{r.result === 'ok' ? '成功' : r.result}</span> }]} /></ApiState>
    {cur && <Drawer title={`审计详情 · ${cur.id}`} onClose={() => setCur(null)}><dl className="kv"><dt>管理员</dt><dd>{cur.admin}（{roleCn(cur.roles)}）</dd><dt>操作</dt><dd>{cur.module} · {cur.action}</dd><dt>对象</dt><dd>{cur.target || '-'}</dd><dt>IP / UA</dt><dd>{cur.ip} · {cur.userAgent}</dd><dt>请求 ID</dt><dd>{cur.requestId}</dd><dt>结果</dt><dd>{cur.result}</dd></dl>
      {(cur.before || cur.after) && <><div className="sec">变更对比</div><div className="grid g2"><pre className="card pad down" style={{ margin: 0 }}>{JSON.stringify(cur.before, null, 2)}</pre><pre className="card pad up" style={{ margin: 0 }}>{JSON.stringify(cur.after, null, 2)}</pre></div></>}</Drawer>}</>
}
