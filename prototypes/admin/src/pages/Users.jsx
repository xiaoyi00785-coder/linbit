import { useState } from 'react'
import { DataTable, Drawer, Modal, PageHead, Tabs, Tag, eq, has, useApp, useFilter } from '../ui.jsx'
import { Confirm } from '../ui.jsx'
import { fmt, ledger, positions, users } from '../mock.js'
import { ApiState, DemoBadge } from '../ui.jsx'
import { API_BASE, getToken, api, ft, items, KYC_LV, KYC_ST, num, USER_ST, useApi } from '../api.js'
const mapUser = u => ({ ...u, id: u.uid, nick: u.nickname || '-', email: u.email || '-', phone: u.phone || '-', country: u.countryCode || '-', kyc: KYC_LV(u.kycLevel), status: USER_ST[u.status] || u.status, raw: u.status, tfa: u.totpEnabled, reg: ft(u.createdAt), identity: '-', risk: '-', inviter: '-', last: '-' })
const stTag = s => <Tag k={s === '正常' ? 'ok' : s === '冻结' ? 'bad' : 'wn'}>{s}</Tag>
const riskTag = r => <Tag k={r === '高' ? 'bad' : r === '中' ? 'wn' : 'ok'}>{r}</Tag>

export function FreezeModal({ user, onClose, onDone }) {
  const { say } = useApp(); const [scope, setScope] = useState(['提现']); const [reason, setReason] = useState(''); const [code, setCode] = useState(''); const [err, setErr] = useState('')
  const unfreeze = user.raw === 'frozen'
  const submit = async () => { setErr(''); try { await api(`users/${user.uid}/${unfreeze ? 'unfreeze' : 'freeze'}`, { method: 'POST', body: { reason, totpCode: code } }); say(unfreeze ? '已解冻' : '已冻结'); onDone && onDone(); onClose() } catch (e) { setErr(e.message) } }
  const opts = ['登录', '交易', '提现', '划转', '充值入账', '跟单/带单']
  return <Modal title={`冻结 / 限制账户 · UID ${user.uid}`} onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button><button className="btn d" disabled={!reason.trim() || code.length < 6} onClick={submit}>{unfreeze ? '提交解冻' : '提交冻结'}</button></>}>
    <div className="banner">高危操作：需填写原因 + Google 验证码，全程写入审计日志。当前接口为整户冻结，冻结范围/期限仅作记录。</div>{err && <div className="banner bad">{err}</div>}
    <div className="fr"><label>原因</label><input className="in" placeholder="必填" value={reason} onChange={e => setReason(e.target.value)} /></div>
    <div className="fr"><label>Google 验证码</label><input className="in" maxLength={6} placeholder="6 位动态码" value={code} onChange={e => setCode(e.target.value.replace(/\D/g, ''))} /></div>
    <div className="fr"><label>冻结范围</label><div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>{opts.map(o => <label key={o}><input type="checkbox" checked={scope.includes(o)} onChange={() => setScope(s => s.includes(o) ? s.filter(x => x !== o) : [...s, o])} /> {o}</label>)}</div></div>
    <div className="fr"><label>原因分类</label><select className="sel"><option>疑似盗号</option><option>司法协查</option><option>AML 命中</option><option>羊毛党/刷量</option><option>用户自助申请</option></select></div>
    <div className="fr"><label>期限</label><select className="sel"><option>永久（手动解冻）</option><option>24 小时</option><option>7 天</option></select></div>
    <div className="fr"><label>工单号</label><input className="in" placeholder="如 CS-20261102-0012" /></div>
    <div className="fr" style={{ alignItems: 'start' }}><label>备注</label><textarea className="in" rows="3" placeholder="内部可见，用户不可见" /></div>
    <div className="fr"><label>通知用户</label><label><input type="checkbox" defaultChecked /> 发送站内信 + 邮件（模板：账户安全冻结）</label></div>
  </Modal>
}

export function UserDrawer({ u, onClose, onChanged }) {
  const bal = useApi('users/' + u.uid + '/balances')
  const [adj, setAdj] = useState(false)
  const [tab, setTab] = useState('base'); const [fz, setFz] = useState(false)
  return <Drawer width={760} title={<>{u.nick} <span className="muted" style={{ fontWeight: 400 }}>UID {u.uid}</span></>} extra={<>{stTag(u.status)}<Tag k="gd">{u.identity}</Tag></>} onClose={onClose}
    footer={<><button className="btn">重置 2FA</button><button className="btn">设为合伙人</button><button className="btn" onClick={() => setAdj(true)}>人工调账</button><button className="btn d" onClick={() => setFz(true)}>{u.raw === 'frozen' ? '解冻' : '冻结 / 限制'}</button></>}>
    <div className="grid g4" style={{ marginBottom: 12 }}>{[['总资产估值', '$' + num(items(bal.data).reduce((a, b) => a + (+b.valuationUsd || 0), 0))], ['30 日交易量', '-'], ['累计手续费', '-'], ['邀请人数', '-']].map(([l, v]) => <div key={l} className="card kpi"><div className="l">{l}</div><div className="v num" style={{ fontSize: 16 }}>{v}</div></div>)}</div>
    <div className="card"><Tabs value={tab} onChange={setTab} items={[['base', '基本信息'], ['asset', '资产'], ['pos', '持仓'], ['log', '登录/设备'], ['note', '备注与工单']]} />
      <div style={{ padding: 14 }}>
        {tab === 'base' && <dl className="kv"><dt>昵称</dt><dd>{u.nick}</dd><dt>邮箱</dt><dd>{u.email} {u.emailVerified ? <Tag k="ok">已验证</Tag> : <Tag k="wn">未验证</Tag>}</dd><dt>手机</dt><dd>{u.phone}</dd><dt>国家/地区</dt><dd>{u.country}</dd>
          <dt>KYC</dt><dd>{u.kyc}</dd><dt>2FA</dt><dd>{u.tfa ? <Tag k="ok">Google 验证器</Tag> : <Tag k="wn">未开启</Tag>}</dd><dt>状态说明</dt><dd>{u.statusReason || '-'}</dd><dt>注册时间</dt><dd>{u.reg}</dd></dl>}
        {tab === 'asset' && <ApiState q={bal}><table className="t"><thead><tr><th>币种</th><th className="r">可用</th><th className="r">冻结</th><th className="r">合计</th><th className="r">估值 USD</th></tr></thead><tbody>
          {items(bal.data).map(r => <tr key={r.asset}><td>{r.asset}</td><td className="r num">{num(r.free, 4)}</td><td className="r num">{num(r.locked, 4)}</td><td className="r num">{num(r.total, 4)}</td><td className="r num">{num(r.valuationUsd)}</td></tr>)}</tbody></table></ApiState>}
        {tab !== 'base' && tab !== 'asset' && <div style={{ marginBottom: 8 }}><DemoBadge /></div>}
        {tab === 'pos' && <table className="t"><thead><tr><th>合约</th><th>方向</th><th>杠杆</th><th className="r">仓位</th><th className="r">未实现盈亏</th></tr></thead><tbody>
          {positions.slice(0, 5).map(p => <tr key={p.id}><td>{p.sym}</td><td className={p.side === '多' ? 'up' : 'down'}>{p.side}</td><td>{p.lev}x</td><td className="r num">{fmt(p.size, 0)}</td><td className={'r num ' + (p.upnl > 0 ? 'up' : 'down')}>{fmt(p.upnl)}</td></tr>)}</tbody></table>}
        {tab === 'log' && <table className="t"><thead><tr><th>时间</th><th>IP</th><th>设备</th><th>结果</th></tr></thead><tbody>
          {[0, 1, 2, 3, 4].map(i => <tr key={i}><td>{ledger[i].time}</td><td>203.0.113.{20 + i}</td><td>{['iOS App 6.2', 'Chrome / macOS', 'Android App 6.1'][i % 3]}</td><td>{i === 3 ? <Tag k="bad">密码错误</Tag> : <Tag k="ok">成功</Tag>}</td></tr>)}</tbody></table>}
        {tab === 'note' && <><div className="tl"><div><b>cs_he</b> · 2026-10-30 · 用户反馈无法提现，已指导完成 2FA 绑定</div><div><b>risk_sun</b> · 2026-10-12 · 新设备登录 + 改密，触发 24h 提现冷却</div></div><textarea className="in" rows="3" placeholder="添加内部备注…" /></>}
      </div></div>
    {fz && <FreezeModal user={u} onClose={() => setFz(false)} onDone={onChanged} />}
    {adj && <Confirm danger title={`人工调账 · UID ${u.uid}`} okText="提交调账单（待复核）" onClose={() => setAdj(false)} rows={[['账户', '现货账户 · USDT'], ['方向', '贷（增加）'], ['金额', '50.00 USDT'], ['关联工单', 'TK-20261102-118']]}
      note="调账单需财务复审后入账，借贷双边分录同步写入资产流水。" />}
  </Drawer>
}

export function Users() {
  const { q } = useApp(); const [kw, setKw] = useState(''); const [st, setSt] = useState('')
  const r = useApi('users', { q: kw, status: st, size: 200 }); const list = items(r.data).map(mapUser)
  const [rows, set, f] = useFilter(list, (r, f) => eq(r.kyc, f.kyc))
  const [cur, setCur] = useState(null)
  return <><PageHead title="用户列表" desc={`共 ${r.data ? r.data.total ?? list.length : '-'} 名用户 · 支持 UID / 昵称 / 邮箱检索（敏感字段已脱敏）`}><button className="btn">导出 CSV</button></PageHead>
    <ApiState q={r}><DataTable rows={rows} select onRow={setCur} cols={[
      { t: 'UID', k: 'uid', render: r => <span className="pri">{r.uid}</span> }, { t: '昵称', k: 'nick' }, { t: '邮箱', k: 'email' }, { t: '国家', k: 'country' },
      { t: 'KYC', k: 'kyc', render: r => <Tag k={r.kyc.startsWith('L2') ? 'ok' : r.kyc === '审核中' ? 'in' : ''}>{r.kyc}</Tag> }, { t: '2FA', render: r => r.tfa ? <Tag k="ok">已开启</Tag> : <Tag k="wn">未开启</Tag> }, { t: '状态', k: 'status', render: r => stTag(r.status) },
      { t: '注册时间', k: 'reg' }, { t: '操作', render: r => <a className="btn g">详情</a> }]}
      filters={<><input className="in" style={{ width: 220 }} placeholder="UID / 昵称 / 邮箱" value={kw} onChange={e => setKw(e.target.value)} />
        <select className="sel" onChange={e => setSt({ 正常: 'active', 冻结: 'frozen', 限制提现: 'withdraw_restricted' }[e.target.value] || '')}><option>全部</option><option>正常</option><option>冻结</option><option>限制提现</option></select>
        <select className="sel" onChange={set('kyc')}><option>全部</option><option>未认证</option><option>L1 基础</option><option>L2 高级</option><option>审核中</option></select></>}
      toolbar={(sel) => <><button className="btn" disabled={!sel.length}>批量打标签</button><button className="btn d" disabled={!sel.length}>批量冻结</button></>} /></ApiState>
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
      <div className="sec">自动检查 <DemoBadge /></div><div className="muted">OCR / 人脸比对 / AML 筛查服务尚未接入，当前为人工审核。</div>
      {rej && <Modal title="驳回原因" onClose={() => setRej(false)} footer={<><button className="btn" onClick={() => setRej(false)}>取消</button><button className="btn d" disabled={!reason.trim()} onClick={() => decide('reject')}>确认驳回</button></>}>
        {['证件照片模糊/反光', '证件信息与填写不一致', '人脸比对未通过', '证件已过期', '不支持的国家/地区'].map(x => <label key={x} className="chk"><input type="radio" name="rj" onChange={() => setReason(x)} /> {x}</label>)}
        <textarea className="in" rows="3" placeholder="对用户展示的说明（必填）" value={reason} onChange={e => setReason(e.target.value)} /></Modal>}
    </Drawer>}</>
}

export function RiskList() {
  // 演示数据：风控名单接口未提供
  const rows = users.filter(u => u.risk !== '低' || u.status !== '正常')
  return <><PageHead title="风险名单" desc="由风控引擎打标（多账户关联、刷量、短线套利返佣、异常登录）+ 人工维护"><DemoBadge /><button className="btn p">+ 加入名单</button></PageHead>
    <DataTable rows={rows} filters={<><input className="in" placeholder="UID" /><select className="sel"><option>全部标签</option><option>多账户关联</option><option>对敲刷量</option><option>短线返佣套利</option><option>异常登录</option></select></>}
      cols={[{ t: 'UID', k: 'uid' }, { t: '昵称', k: 'nick' }, { t: '风险', render: r => riskTag(r.risk) }, { t: '命中规则', render: r => ['同设备 6 账户', '对敲成交 32 笔', '持仓 < 60s 占比 82%', '异地登录'][+r.uid % 4] },
        { t: '处置', render: r => stTag(r.status) }, { t: '最近活动', k: 'last' }, { t: '操作', render: () => <><a className="btn g">查看</a><a className="btn g">移出</a></> }]} /></>
}
