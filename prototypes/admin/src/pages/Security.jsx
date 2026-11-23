/* 安全与风控：API Key 管理 / 风控规则（命中队列 + 规则配置）/ 角色与权限编辑。
   全部读写 admin/v1 真实接口；敏感写操作走 Confirm（Google 验证码 + 原因），后端写审计。 */
import { useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Empty, Kpi, Modal, PageHead, Sw, Tabs, Tag, useApp } from '../ui.jsx'
import { api, errText, ft, items, num, useApi } from '../api.js'
import { FreezeModal, UserDrawer, mapUser } from './Users.jsx'
import { allowed } from '../menu.js'

const F = ({ l, children }) => <div className="fr"><label>{l}</label>{children}</div>
const useCan = () => { const { me } = useApp(); return p => allowed(me && me.perms, p) }
const pre = { whiteSpace: 'pre-wrap', wordBreak: 'break-word', margin: 0 }

/* 按 UID 打开用户详情（复用用户列表的抽屉） */
function UserByUid({ uid, onClose, freeze }) {
  const q = useApi('users', { q: uid, size: 5 })
  const u = items(q.data).map(mapUser).find(x => String(x.uid) === String(uid))
  if (q.loading) return null
  if (!u) return <Modal title={'用户 ' + uid} onClose={onClose}><Empty text="未找到该用户" /></Modal>
  return freeze ? <FreezeModal user={u} onClose={onClose} onDone={onClose} /> : <UserDrawer u={u} onClose={onClose} />
}

/* ===================== API Key 管理 ===================== */
export function ApiKeys() {
  const can = useCan(); const [f, setF] = useState({ uid: '', status: 'active', noIp: '' }); const [page, setPage] = useState(1)
  const q = useApi('api-keys', { ...f, uid: f.uid.trim(), page, size: 50 }); const st = (q.data && q.data.stats) || {}
  const [cur, setCur] = useState(null); const [rv, setRv] = useState(null); const [uq, setUq] = useState(null)
  const v = k => e => { setF({ ...f, [k]: e.target.value }); setPage(1) }; const total = q.data ? q.data.total : 0
  return <><PageHead title="API Key 管理" desc="用户 API Key 一览（只显示脱敏标识，不读取密钥）· 吊销立即生效（网关每次请求实时校验）· 需 Google 验证码并写入审计" />
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="有效 Key" v={st.active ?? '-'} d={`${st.users ?? '-'} 个用户持有`} /><Kpi l="24h 内使用" v={st.used24h ?? '-'} />
      <Kpi l="未绑定 IP" v={st.noIp ?? '-'} up={!(+st.noIp > 0)} d="建议提醒用户设置白名单" /><Kpi l="已吊销" v={st.revoked ?? '-'} /></div>
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pageSize={50} filters={<>
      <input className="in" placeholder="UID" value={f.uid} onChange={v('uid')} />
      <select className="sel" value={f.status} onChange={v('status')}><option value="">全部状态</option><option value="active">有效</option><option value="revoked">已吊销</option></select>
      <select className="sel" value={f.noIp} onChange={v('noIp')}><option value="">全部</option><option value="1">仅未绑定 IP</option></select>
      <span className="muted" style={{ alignSelf: 'center' }}>第 {page} 页 · 共 {total} 条</span><button className="btn" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><button className="btn" disabled={page * 50 >= total} onClick={() => setPage(page + 1)}>下一页</button></>}
      cols={[{ t: 'ID', k: 'id' }, { t: 'UID', render: r => <a onClick={e => { e.stopPropagation(); setUq(r.uid) }}>{r.uid}</a> }, { t: 'Key', render: r => <code>{r.keyMasked}</code> }, { t: '备注', render: r => r.label || <span className="muted">-</span> },
        { t: '权限', render: r => <Tag>{r.permissions === 'read' ? '只读' : r.permissions}</Tag> }, { t: 'IP 白名单', render: r => r.ipAllowlist ? <span>{r.ipAllowlist}</span> : <Tag k="wn">未绑定</Tag> },
        { t: '创建', render: r => ft(r.createdAt) }, { t: '最近使用', render: r => ft(r.lastUsedAt) },
        { t: '状态', render: r => r.revokedAt ? <Tag k="bad">已吊销</Tag> : <Tag k="ok">有效</Tag> },
        { t: '操作', render: r => !r.revokedAt && can('user:edit') ? <a className="btn g" onClick={e => { e.stopPropagation(); setRv(r) }}>吊销</a> : null }]} /></ApiState>
    {cur && <Drawer title={`API Key #${cur.id}`} onClose={() => setCur(null)} footer={!cur.revokedAt && can('user:edit') ? <button className="btn d" onClick={() => setRv(cur)}>吊销此 Key</button> : null}>
      <dl className="kv"><dt>UID</dt><dd>{cur.uid}{cur.isSub ? '（子账户）' : ''}</dd><dt>Key 标识</dt><dd><code>{cur.keyMasked}</code></dd><dt>备注</dt><dd>{cur.label || '-'}</dd><dt>权限</dt><dd>{cur.permissions === 'read' ? '只读（当前版本不支持交易/提现权限）' : cur.permissions}</dd>
        <dt>IP 白名单</dt><dd>{cur.ipAllowlist || '未绑定'}</dd><dt>创建时间</dt><dd>{ft(cur.createdAt)}</dd><dt>最近使用</dt><dd>{ft(cur.lastUsedAt)}</dd><dt>吊销时间</dt><dd>{ft(cur.revokedAt)}</dd></dl></Drawer>}
    {rv && <Confirm danger title={`吊销 API Key #${rv.id}`} okText="吊销" rows={[['UID', rv.uid], ['Key', rv.keyMasked], ['备注', rv.label || '-']]} note="吊销后该 Key 立即失效且不可恢复，用户需重新创建。" onClose={() => setRv(null)}
      onOk={async ({ totpCode, reason }) => { await api(`api-keys/${rv.id}/revoke`, { method: 'POST', body: { totpCode, reason } }); setRv(null); setCur(null); q.reload(); return '已吊销' }} />}
    {uq && <UserByUid uid={uq} onClose={() => setUq(null)} />}</>
}

/* ===================== 风控规则 ===================== */
const SEV = { high: ['高', 'bad'], medium: ['中', 'wn'], low: ['低', ''] }
const sevTag = s => <Tag k={(SEV[s] || [])[1]}>{(SEV[s] || [s])[0]}</Tag>
const HIT_ST = { open: ['待处理', 'wn'], ack: ['已处理', 'ok'], ignored: ['已忽略', ''] }
const paramText = r => (r.specs || []).filter(s => s.key !== 'excludeUids' || (r.params || {}).excludeUids).map(s => `${s.label} ${(r.params || {})[s.key] ?? '-'}`).join(' · ')

export function RiskRules() {
  const [t, setT] = useState('hits')
  return <><PageHead title="风控规则" desc="定时（约每 10 分钟）对提现、充值、持仓、强平、API Key 运行检测规则；命中进入队列由风控人工处理。规则只告警，不会自动冻结或拦截资金" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['hits', '命中队列'], ['rules', '规则配置']]} /></div>
    {t === 'hits' ? <RiskHits /> : <RiskRuleList />}</>
}

function RiskHits() {
  const can = useCan(); const { say } = useApp()
  const [f, setF] = useState({ status: 'open', rule: '', severity: '', uid: '' }); const [page, setPage] = useState(1)
  const q = useApi('risk/hits', { ...f, uid: f.uid.trim(), page, size: 50 }); const rules = useApi('risk/rules'); const st = (q.data && q.data.stats) || {}
  const [cur, setCur] = useState(null); const [h, setH] = useState(null); const [uq, setUq] = useState(null); const [fz, setFz] = useState(null); const [running, setRunning] = useState(false)
  const v = k => e => { setF({ ...f, [k]: e.target.value }); setPage(1) }; const total = q.data ? q.data.total : 0
  const run = async () => { setRunning(true); try { const r = await api('risk/run', { method: 'POST', body: {} }); say(`检测完成：${r.rules} 条规则，${r.findings} 个命中，新增 ${r.newHits}` + (r.errors.length ? `，${r.errors.length} 个错误` : '')); q.reload(); rules.reload() } catch (e) { say('失败：' + errText(e)) } finally { setRunning(false) } }
  return <>
    <div className="grid g4" style={{ margin: '12px 0' }}><Kpi l="待处理命中" v={st.open ?? '-'} up={!(+st.open > 0)} /><Kpi l="其中高风险" v={st.openHigh ?? '-'} up={!(+st.openHigh > 0)} />
      <Kpi l="24h 新增" v={st.new24h ?? '-'} /><Kpi l="24h 已处理" v={st.handled24h ?? '-'} /></div>
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pageSize={50} filters={<>
      <select className="sel" value={f.status} onChange={v('status')}><option value="open">待处理</option><option value="ack">已处理</option><option value="ignored">已忽略</option><option value="all">全部</option></select>
      <select className="sel" value={f.rule} onChange={v('rule')}><option value="">全部规则</option>{items(rules.data).map(r => <option key={r.code} value={r.code}>{r.name}</option>)}</select>
      <select className="sel" value={f.severity} onChange={v('severity')}><option value="">全部级别</option><option value="high">高</option><option value="medium">中</option><option value="low">低</option></select>
      <input className="in" placeholder="UID" value={f.uid} onChange={v('uid')} />
      <span className="muted" style={{ alignSelf: 'center' }}>共 {total} 条</span><button className="btn" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><button className="btn" disabled={page * 50 >= total} onClick={() => setPage(page + 1)}>下一页</button>
      {can('risk:edit') && <button className="btn p" style={{ marginLeft: 'auto' }} disabled={running} onClick={run}>{running ? '检测中…' : '立即检测'}</button>}</>}
      cols={[{ t: '级别', render: r => sevTag(r.severity) }, { t: '规则', k: 'ruleName' }, { t: 'UID', render: r => <a onClick={e => { e.stopPropagation(); setUq(r.uid) }}>{r.uid}</a> }, { t: '内容', k: 'summary' },
        { t: '次数', r: 1, render: r => r.hitCount }, { t: '首次', render: r => ft(r.firstSeen) }, { t: '最近', render: r => ft(r.lastSeen) },
        { t: '状态', render: r => { const s = HIT_ST[r.status] || [r.status]; return <Tag k={s[1]}>{s[0]}</Tag> } },
        { t: '操作', render: r => r.status === 'open' && can('risk:edit') ? <><a className="btn g" onClick={e => { e.stopPropagation(); setH({ hit: r, status: 'ack' }) }}>已处理</a><a className="btn g" onClick={e => { e.stopPropagation(); setH({ hit: r, status: 'ignored' }) }}>忽略</a></> : <span className="muted">{r.handledBy || ''}</span> }]} /></ApiState>
    {cur && <Drawer title={`命中 #${cur.id} · ${cur.ruleName}`} onClose={() => setCur(null)} footer={<>
      <button className="btn" onClick={() => setUq(cur.uid)}>用户详情</button>
      {can('user:edit') && <button className="btn d" onClick={() => setFz(cur.uid)}>冻结/限制…</button>}
      {cur.status === 'open' && can('risk:edit') && <><button className="btn" onClick={() => setH({ hit: cur, status: 'ignored' })}>忽略</button><button className="btn p" onClick={() => setH({ hit: cur, status: 'ack' })}>标记已处理</button></>}</>}>
      <dl className="kv"><dt>级别</dt><dd>{sevTag(cur.severity)}</dd><dt>UID</dt><dd>{cur.uid}</dd><dt>内容</dt><dd>{cur.summary}</dd><dt>对象</dt><dd><code>{cur.subject}</code></dd>
        <dt>命中次数</dt><dd>{cur.hitCount}（首次 {ft(cur.firstSeen)}，最近 {ft(cur.lastSeen)}）</dd><dt>状态</dt><dd>{(HIT_ST[cur.status] || [cur.status])[0]}{cur.handledBy ? ` · ${cur.handledBy} · ${ft(cur.handledAt)}` : ''}</dd>
        {cur.note && <><dt>处理说明</dt><dd style={pre}>{cur.note}</dd></>}</dl>
      <div className="sec">明细</div><pre className="card pad" style={pre}>{JSON.stringify(cur.detail, null, 2)}</pre>
      <div className="muted" style={{ marginTop: 8 }}>规则只做提示。如需冻结或限制提现，请通过「冻结/限制」走现有审核流程（需 2FA，写审计）。</div></Drawer>}
    {h && <Confirm noCode title={(h.status === 'ack' ? '标记已处理' : '忽略命中') + ' · #' + h.hit.id} reasonLabel="处理说明" rows={[['规则', h.hit.ruleName], ['UID', h.hit.uid], ['内容', h.hit.summary]]}
      note="处理后，同一对象在规则的静默时间内不会再次生成命中。" onClose={() => setH(null)}
      onOk={async ({ reason }) => { await api(`risk/hits/${h.hit.id}`, { method: 'POST', body: { status: h.status, reason } }); setH(null); setCur(null); q.reload(); return '已更新' }} />}
    {uq && <UserByUid uid={uq} onClose={() => setUq(null)} />}
    {fz && <UserByUid freeze uid={fz} onClose={() => { setFz(null); q.reload() }} />}</>
}

function RiskRuleList() {
  const can = useCan(); const q = useApi('risk/rules'); const [ed, setEd] = useState(null); const [cf, setCf] = useState(null)
  const open = r => setEd({ rule: r, enabled: r.enabled, notify: r.notify, severity: r.severity, params: Object.fromEntries((r.specs || []).map(s => [s.key, String((r.params || {})[s.key] ?? '')])) })
  const diff = ed ? (() => { const r = ed.rule; const ch = []; if (ed.enabled !== r.enabled) ch.push(['启用', ed.enabled ? '开' : '关']); if (ed.notify !== r.notify) ch.push(['Telegram 告警', ed.notify ? '开' : '关']); if (ed.severity !== r.severity) ch.push(['级别', (SEV[ed.severity] || [])[0]])
    for (const s of r.specs || []) if (String((r.params || {})[s.key] ?? '') !== ed.params[s.key]) ch.push([s.label, `${(r.params || {})[s.key] ?? '-'} → ${ed.params[s.key] || '(空)'}`]); return ch })() : []
  const body = () => { const r = ed.rule; const p = {}; for (const s of r.specs || []) if (String((r.params || {})[s.key] ?? '') !== ed.params[s.key]) p[s.key] = s.type === 'num' ? Number(ed.params[s.key]) : ed.params[s.key]
    return { enabled: ed.enabled, notify: ed.notify, severity: ed.severity, ...(Object.keys(p).length ? { params: p } : {}) } }
  return <ApiState q={q}><div style={{ marginTop: 12 }}><DataTable rowKey="code" rows={items(q.data)} onRow={r => can('risk:edit') && open(r)} pageSize={20} cols={[
    { t: '规则', render: r => <><b>{r.name}</b><div className="muted" style={{ fontSize: 12 }}>{r.description}</div></> }, { t: '参数', render: r => <span style={{ fontSize: 12 }}>{paramText(r)}</span> },
    { t: '级别', render: r => sevTag(r.severity) }, { t: '启用', render: r => r.enabled ? <Tag k="ok">启用</Tag> : <Tag>停用</Tag> }, { t: 'TG 告警', render: r => r.notify ? <Tag k="ok">开</Tag> : <span className="muted">关</span> },
    { t: '待处理', r: 1, render: r => r.openHits }, { t: '上次运行', render: r => <>{ft(r.lastRunAt)}{r.lastError ? <div className="down" style={{ fontSize: 12 }}>{r.lastError}</div> : <div className="muted" style={{ fontSize: 12 }}>命中 {r.lastHits}</div>}</> },
    { t: '更新', render: r => <span className="muted" style={{ fontSize: 12 }}>{r.updatedBy || '系统'} · {ft(r.updatedAt)}</span> }]} /></div>
    {ed && <Drawer title={'编辑规则 · ' + ed.rule.name} onClose={() => setEd(null)} footer={<button className="btn p" disabled={!diff.length} onClick={() => setCf(true)}>保存（{diff.length} 项变更）</button>}>
      <div className="muted" style={{ marginBottom: 12 }}>{ed.rule.description}。规则只生成命中与告警，不改变任何资金或冻结流程。</div>
      <F l="启用"><Sw on={ed.enabled} onClick={() => setEd({ ...ed, enabled: !ed.enabled })} /></F>
      <F l="Telegram 告警"><Sw on={ed.notify} onClick={() => setEd({ ...ed, notify: !ed.notify })} /></F>
      <F l="级别"><select className="sel" value={ed.severity} onChange={e => setEd({ ...ed, severity: e.target.value })}><option value="high">高</option><option value="medium">中</option><option value="low">低</option></select></F>
      {(ed.rule.specs || []).map(s => <F key={s.key} l={s.label}><input className="in" inputMode={s.type === 'num' ? 'decimal' : undefined} value={ed.params[s.key]} onChange={e => setEd({ ...ed, params: { ...ed.params, [s.key]: e.target.value } })}
        placeholder={s.type === 'num' ? `${s.min} ~ ${s.max}` : ''} /></F>)}</Drawer>}
    {ed && cf && <Confirm title={'保存规则 · ' + ed.rule.name} rows={diff} onClose={() => setCf(null)}
      onOk={async ({ totpCode, reason }) => { await api('risk/rules/' + ed.rule.code, { method: 'PUT', body: { ...body(), totpCode, reason } }); setCf(null); setEd(null); q.reload(); return '已保存' }} />}</ApiState>
}

/* ===================== 角色与权限 ===================== */
export const PERM_GROUPS = [
  ['看板', [['dash:view', '数据看板']]],
  ['用户', [['user:view', '查看用户'], ['user:pii', '查看完整个人信息'], ['user:edit', '冻结/解冻/吊销 Key'], ['kyc:view', '查看 KYC'], ['kyc:approve', 'KYC 审核']]],
  ['资金', [['withdraw:view', '查看提现'], ['withdraw:first', '提现初审'], ['withdraw:second', '提现复审'], ['ledger:view', '查看流水/充值/持仓'], ['fund:adjust', '发起人工调账'], ['fund:adjust:approve', '复核人工调账']]],
  ['产品', [['symbol:view', '查看交易对/合约'], ['symbol:edit', '编辑交易对/合约'], ['fee:view', '查看费率'], ['fee:edit', '提交费率变更'], ['fee:approve', '审批费率变更']]],
  ['运营', [['reward:view', '查看活动/奖励'], ['reward:edit', '创建活动/奖励'], ['reward:approve', '审批奖励'], ['coupon:approve', '审批卡券'], ['content:edit', '内容/公告/聊天/PoR'], ['earn:view', '查看理财'], ['earn:edit', '编辑理财'], ['vip:view', '查看 VIP'], ['vip:review', 'VIP 审核']]],
  ['社区', [['copy:view', '查看跟单'], ['copy:review', '跟单审核'], ['partner:view', '查看合伙人'], ['partner:review', '合伙人审核']]],
  ['客服', [['support:view', '查看工单'], ['support:reply', '回复/领取工单']]],
  ['风控', [['risk:view', '查看风控命中/规则'], ['risk:edit', '处理命中/编辑规则']]],
  ['做市', [['mm:view', '查看做市/偿付'], ['mm:edit', '编辑做市/立即对账']]],
  ['系统', [['audit:view', '审计日志'], ['admin:manage', '管理员与角色']]],
]
const PERM_CN = Object.fromEntries(PERM_GROUPS.flatMap(([, ps]) => ps))

export function RolesPanel({ q }) {
  const [ed, setEd] = useState(null); const [cf, setCf] = useState(null)
  const d = q.data || {}; const locked = new Set(d.lockedPerms || []); const all = d.allPerms || []
  const groups = PERM_GROUPS.map(([g, ps]) => [g, ps.filter(([p]) => all.includes(p))]).concat([['其他', all.filter(p => !PERM_CN[p]).map(p => [p, p])]]).filter(([, ps]) => ps.length)
  const tog = p => setEd({ ...ed, perms: ed.perms.includes(p) ? ed.perms.filter(x => x !== p) : [...ed.perms, p] })
  const added = ed ? ed.perms.filter(p => !(ed.role ? ed.role.perms : []).includes(p)) : []; const removed = ed && ed.role ? ed.role.perms.filter(p => !ed.perms.includes(p)) : []
  return <ApiState q={q}>
    <div className="card pad" style={{ marginTop: 12 }}><div className="banner">资金与冻结类权限点（🔒）锁定为系统默认，不能通过角色编辑授予或移除：{[...locked].map(p => PERM_CN[p] || p).join('、')}。超级管理员不可编辑。</div></div>
    <DataTable rowKey="code" rows={items(d)} onRow={r => r.editable && setEd({ role: r, name: r.name, perms: [...r.perms] })} pageSize={30}
      filters={<span className="muted">修改后立即对该角色的全部管理员生效（前端菜单在其下次刷新页面时更新）</span>} toolbar={<button className="btn p" onClick={() => setEd({ role: null, code: '', name: '', perms: [] })}>+ 新建自定义角色</button>}
      cols={[{ t: '角色', render: r => <><b>{r.name}</b> <span className="muted">{r.code}</span></> }, { t: '类型', render: r => r.builtin ? (r.overridden ? <Tag k="wn">内置 · 已调整</Tag> : <Tag>内置</Tag>) : <Tag k="ok">自定义</Tag> },
        { t: '权限点', render: r => <span style={{ fontSize: 12 }}>{r.code === 'super_admin' ? '全部' : r.perms.map(p => (locked.has(p) ? '🔒' : '') + (PERM_CN[p] || p)).join('、')}</span> },
        { t: '成员', r: 1, render: r => r.members }, { t: '更新', render: r => r.updatedBy ? <span className="muted" style={{ fontSize: 12 }}>{r.updatedBy} · {ft(r.updatedAt)}</span> : <span className="muted">默认</span> },
        { t: '', render: r => r.editable ? <a className="btn g">编辑</a> : <span className="muted">不可编辑</span> }]} />
    {ed && <Drawer width={640} title={ed.role ? `编辑角色 · ${ed.role.name}` : '新建自定义角色'} onClose={() => setEd(null)} footer={<>
      {ed.role && (ed.role.builtin ? ed.role.overridden && <button className="btn" onClick={() => setCf('reset')}>恢复默认</button> : <button className="btn d" onClick={() => setCf('delete')}>删除角色</button>)}
      <button className="btn p" disabled={ed.role ? !added.length && !removed.length && ed.name === ed.role.name : !ed.code || !ed.name || !ed.perms.length} onClick={() => setCf('save')}>{ed.role ? '保存' : '创建'}</button></>}>
      {!ed.role && <F l="角色代码"><input className="in" placeholder="如 cs_lead（小写字母/数字/下划线）" value={ed.code} onChange={e => setEd({ ...ed, code: e.target.value.toLowerCase() })} /></F>}
      <F l="角色名称"><input className="in" value={ed.name} onChange={e => setEd({ ...ed, name: e.target.value })} /></F>
      {ed.role && ed.role.builtin && <div className="muted" style={{ margin: '0 0 10px' }}>内置默认：{ed.role.defaultPerms.map(p => PERM_CN[p] || p).join('、')}</div>}
      {groups.map(([g, ps]) => <div key={g} style={{ marginBottom: 10 }}><div className="sec">{g}</div><div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 16px' }}>
        {ps.map(([p, n]) => <label key={p} title={p} style={{ opacity: locked.has(p) ? .55 : 1 }}><input type="checkbox" disabled={locked.has(p)} checked={ed.perms.includes(p)} onChange={() => tog(p)} /> {locked.has(p) ? '🔒 ' : ''}{n}</label>)}</div></div>)}
    </Drawer>}
    {ed && cf === 'save' && <Confirm title={(ed.role ? '保存角色 · ' : '新建角色 · ') + (ed.name || ed.code)} rows={ed.role ? [['新增权限', added.map(p => PERM_CN[p] || p).join('、') || '-'], ['移除权限', removed.map(p => PERM_CN[p] || p).join('、') || '-'], ['影响管理员', ed.role.members + ' 人（立即生效）']] : [['代码', ed.code], ['权限', ed.perms.map(p => PERM_CN[p] || p).join('、')]]}
      onClose={() => setCf(null)} onOk={async ({ totpCode, reason }) => { if (ed.role) await api('roles/' + ed.role.code, { method: 'PUT', body: { name: ed.name, perms: ed.perms, totpCode, reason } }); else await api('roles', { method: 'POST', body: { code: ed.code, name: ed.name, perms: ed.perms, totpCode, reason } }); setCf(null); setEd(null); q.reload(); return '已保存' }} />}
    {ed && ed.role && (cf === 'reset' || cf === 'delete') && <Confirm danger title={(cf === 'reset' ? '恢复默认 · ' : '删除角色 · ') + ed.role.name} rows={[['影响管理员', ed.role.members + ' 人']]} note={cf === 'delete' ? '仍有管理员持有该角色时无法删除。' : '恢复为代码内置的默认权限。'}
      onClose={() => setCf(null)} onOk={async ({ totpCode, reason }) => { await api('roles/' + ed.role.code, { method: 'DELETE', body: { totpCode, reason } }); setCf(null); setEd(null); q.reload(); return '已完成' }} />}
  </ApiState>
}
