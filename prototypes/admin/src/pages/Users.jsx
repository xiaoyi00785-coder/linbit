import { useState } from 'react'
import { DataTable, Drawer, Modal, PageHead, Tabs, Tag, eq, has, useApp, useFilter } from '../ui.jsx'
import { Confirm } from '../ui.jsx'
import { fmt, kycs, ledger, positions, users } from '../mock.js'
const stTag = s => <Tag k={s === '正常' ? 'ok' : s === '冻结' ? 'bad' : 'wn'}>{s}</Tag>
const riskTag = r => <Tag k={r === '高' ? 'bad' : r === '中' ? 'wn' : 'ok'}>{r}</Tag>

export function FreezeModal({ user, onClose }) {
  const { say } = useApp(); const [scope, setScope] = useState(['提现'])
  const opts = ['登录', '交易', '提现', '划转', '充值入账', '跟单/带单']
  return <Modal title={`冻结 / 限制账户 · UID ${user.uid}`} onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button><button className="btn d" onClick={() => { say('已提交冻结，等待风控主管确认'); onClose() }}>提交冻结</button></>}>
    <div className="banner">高危操作：需填写原因并经第二人（风控主管）确认后生效，全程写入审计日志。</div>
    <div className="fr"><label>冻结范围</label><div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>{opts.map(o => <label key={o}><input type="checkbox" checked={scope.includes(o)} onChange={() => setScope(s => s.includes(o) ? s.filter(x => x !== o) : [...s, o])} /> {o}</label>)}</div></div>
    <div className="fr"><label>原因分类</label><select className="sel"><option>疑似盗号</option><option>司法协查</option><option>AML 命中</option><option>羊毛党/刷量</option><option>用户自助申请</option></select></div>
    <div className="fr"><label>期限</label><select className="sel"><option>永久（手动解冻）</option><option>24 小时</option><option>7 天</option></select></div>
    <div className="fr"><label>工单号</label><input className="in" placeholder="如 CS-20261102-0012" /></div>
    <div className="fr" style={{ alignItems: 'start' }}><label>备注</label><textarea className="in" rows="3" placeholder="内部可见，用户不可见" /></div>
    <div className="fr"><label>通知用户</label><label><input type="checkbox" defaultChecked /> 发送站内信 + 邮件（模板：账户安全冻结）</label></div>
  </Modal>
}

export function UserDrawer({ u, onClose }) {
  const [adj, setAdj] = useState(false)
  const [tab, setTab] = useState('base'); const [fz, setFz] = useState(false)
  return <Drawer width={760} title={<>{u.nick} <span className="muted" style={{ fontWeight: 400 }}>UID {u.uid}</span></>} extra={<>{stTag(u.status)}<Tag k="gd">{u.identity}</Tag></>} onClose={onClose}
    footer={<><button className="btn">重置 2FA</button><button className="btn">设为合伙人</button><button className="btn" onClick={() => setAdj(true)}>人工调账</button><button className="btn d" onClick={() => setFz(true)}>冻结 / 限制</button></>}>
    <div className="grid g4" style={{ marginBottom: 12 }}>{[['总资产估值', '$' + fmt(u.asset)], ['30 日交易量', '$1,284,320'], ['累计手续费', '$642.16'], ['邀请人数', '12']].map(([l, v]) => <div key={l} className="card kpi"><div className="l">{l}</div><div className="v num" style={{ fontSize: 16 }}>{v}</div></div>)}</div>
    <div className="card"><Tabs value={tab} onChange={setTab} items={[['base', '基本信息'], ['asset', '资产'], ['pos', '持仓'], ['log', '登录/设备'], ['note', '备注与工单']]} />
      <div style={{ padding: 14 }}>
        {tab === 'base' && <dl className="kv"><dt>昵称</dt><dd>{u.nick}</dd><dt>邮箱</dt><dd>{u.email} <Tag k="ok">已验证</Tag></dd><dt>手机</dt><dd>{u.phone}</dd><dt>国家/地区</dt><dd>{u.country}</dd>
          <dt>KYC</dt><dd>{u.kyc}</dd><dt>2FA</dt><dd>{u.tfa ? <Tag k="ok">Google 验证器</Tag> : <Tag k="wn">未开启</Tag>}</dd><dt>邀请码</dt><dd>{u.uid === '80000001' ? 'LB8DEMO' : 'LB' + u.uid.slice(-4)}</dd>
          <dt>上级邀请人</dt><dd>{u.inviter}</dd><dt>代理商</dt><dd>{u.agent ? '是' : '否'}</dd><dt>风险等级</dt><dd>{riskTag(u.risk)}</dd><dt>安全冷却</dt><dd className="warn">修改密码后 24h 内禁止提现（剩余 0h）</dd><dt>注册时间</dt><dd>{u.reg}</dd><dt>最近登录</dt><dd>{u.last}</dd></dl>}
        {tab === 'asset' && <table className="t"><thead><tr><th>账户</th><th>币种</th><th className="r">可用</th><th className="r">冻结</th><th className="r">估值 USD</th></tr></thead><tbody>
          {[['现货', 'USDT', 8210.22, 120], ['现货', 'BTC', .0821, 0], ['合约', 'USDT', 15200, 3200], ['跟单', 'USDT', 2000, 0], ['体验金', 'USDT', 50, 0]].map((r, i) => <tr key={i}><td>{r[0]}</td><td>{r[1]}</td><td className="r num">{fmt(r[2], 4)}</td><td className="r num">{fmt(r[3], 4)}</td><td className="r num">{fmt(r[1] === 'BTC' ? r[2] * 68420 : r[2])}</td></tr>)}</tbody></table>}
        {tab === 'pos' && <table className="t"><thead><tr><th>合约</th><th>方向</th><th>杠杆</th><th className="r">仓位</th><th className="r">未实现盈亏</th></tr></thead><tbody>
          {positions.slice(0, 5).map(p => <tr key={p.id}><td>{p.sym}</td><td className={p.side === '多' ? 'up' : 'down'}>{p.side}</td><td>{p.lev}x</td><td className="r num">{fmt(p.size, 0)}</td><td className={'r num ' + (p.upnl > 0 ? 'up' : 'down')}>{fmt(p.upnl)}</td></tr>)}</tbody></table>}
        {tab === 'log' && <table className="t"><thead><tr><th>时间</th><th>IP</th><th>设备</th><th>结果</th></tr></thead><tbody>
          {[0, 1, 2, 3, 4].map(i => <tr key={i}><td>{ledger[i].time}</td><td>203.0.113.{20 + i}</td><td>{['iOS App 6.2', 'Chrome / macOS', 'Android App 6.1'][i % 3]}</td><td>{i === 3 ? <Tag k="bad">密码错误</Tag> : <Tag k="ok">成功</Tag>}</td></tr>)}</tbody></table>}
        {tab === 'note' && <><div className="tl"><div><b>cs_he</b> · 2026-10-30 · 用户反馈无法提现，已指导完成 2FA 绑定</div><div><b>risk_sun</b> · 2026-10-12 · 新设备登录 + 改密，触发 24h 提现冷却</div></div><textarea className="in" rows="3" placeholder="添加内部备注…" /></>}
      </div></div>
    {fz && <FreezeModal user={u} onClose={() => setFz(false)} />}
    {adj && <Confirm danger title={`人工调账 · UID ${u.uid}`} okText="提交调账单（待复核）" onClose={() => setAdj(false)} rows={[['账户', '现货账户 · USDT'], ['方向', '贷（增加）'], ['金额', '50.00 USDT'], ['关联工单', 'TK-20261102-118']]}
      note="调账单需财务复审后入账，借贷双边分录同步写入资产流水。" />}
  </Drawer>
}

export function Users() {
  const { q } = useApp()
  const [rows, set, f] = useFilter(users, (r, f) => (has(r.uid, f.k) || has(r.nick, f.k) || has(r.email, f.k)) && eq(r.status, f.s) && eq(r.kyc, f.kyc) && eq(r.identity, f.idt))
  const [cur, setCur] = useState(q.open ? users[0] : null)
  return <><PageHead title="用户列表" desc="共 86 名演示用户 · 支持 UID / 昵称 / 邮箱检索"><button className="btn">导出 CSV</button></PageHead>
    <DataTable rows={rows} select onRow={setCur} cols={[
      { t: 'UID', k: 'uid', render: r => <span className="pri">{r.uid}</span> }, { t: '昵称', k: 'nick' }, { t: '邮箱', k: 'email' }, { t: '国家', k: 'country' },
      { t: 'KYC', k: 'kyc', render: r => <Tag k={r.kyc.startsWith('L2') ? 'ok' : r.kyc === '审核中' ? 'in' : ''}>{r.kyc}</Tag> }, { t: '身份', k: 'identity' },
      { t: '总资产(USD)', k: 'asset', r: 1, num: 1, render: r => fmt(r.asset) }, { t: '风险', k: 'risk', render: r => riskTag(r.risk) }, { t: '状态', k: 'status', render: r => stTag(r.status) },
      { t: '注册时间', k: 'reg' }, { t: '操作', render: r => <a className="btn g">详情</a> }]}
      filters={<><input className="in" style={{ width: 220 }} placeholder="UID / 昵称 / 邮箱" value={f.k || ''} onChange={set('k')} />
        <select className="sel" onChange={set('s')}><option>全部</option><option>正常</option><option>冻结</option><option>限制提现</option></select>
        <select className="sel" onChange={set('kyc')}><option>全部</option><option>未认证</option><option>L1 基础</option><option>L2 高级</option><option>审核中</option></select>
        <select className="sel" onChange={set('idt')}><option>全部</option>{['普通用户', '合伙人·普通', '合伙人·银牌', '带单员'].map(v => <option key={v}>{v}</option>)}</select><input className="in" type="date" /></>}
      toolbar={(sel) => <><button className="btn" disabled={!sel.length}>批量打标签</button><button className="btn d" disabled={!sel.length}>批量冻结</button></>} />
    {cur && <UserDrawer u={cur} onClose={() => setCur(null)} />}</>
}

export function Kyc() {
  const { say } = useApp(); const [tab, setTab] = useState('待审核'); const [cur, setCur] = useState(null); const [rej, setRej] = useState(false)
  const rows = kycs.filter(k => tab === '全部' || k.status === tab)
  return <><PageHead title="KYC 审核" desc="L1：证件 OCR + 人脸比对；L2：地址证明 + 人工复核 · SLA 2 小时"><button className="btn">分配规则</button></PageHead>
    <div className="card" style={{ marginBottom: 0 }}><Tabs value={tab} onChange={setTab} items={[['待审核', '待审核', 14], ['复审中', '复审中'], ['已通过', '已通过'], ['已驳回', '已驳回'], ['全部', '全部']]} /></div>
    <DataTable rows={rows} onRow={setCur} filters={<><input className="in" placeholder="UID / 申请单号" /><select className="sel"><option>全部等级</option><option>L1 基础</option><option>L2 高级</option></select><select className="sel"><option>全部国家</option></select></>}
      cols={[{ t: '申请单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '等级', k: 'level' }, { t: '证件', k: 'doc' }, { t: '国家', k: 'country' },
        { t: '人脸相似度', k: 'face', render: r => <span className={r.face < 80 ? 'down' : 'up'}>{r.face}%</span> }, { t: 'OCR 校验', k: 'ocr', render: r => <Tag k={r.ocr === '一致' ? 'ok' : 'bad'}>{r.ocr}</Tag> },
        { t: 'AML 筛查', k: 'aml', render: r => <Tag k={r.aml === '无命中' ? 'ok' : 'bad'}>{r.aml}</Tag> }, { t: '证件重复', k: 'dup', render: r => r.dup ? <Tag k="bad">重复</Tag> : <span className="muted">-</span> },
        { t: '提交时间', k: 'submit' }, { t: '状态', k: 'status', render: r => <Tag k={r.status === '已通过' ? 'ok' : r.status === '已驳回' ? 'bad' : 'wn'}>{r.status}</Tag> }, { t: '审核人', k: 'reviewer' }]} />
    {cur && <Drawer width={720} title={`KYC 审核 · ${cur.id}`} extra={<Tag k="wn">{cur.status}</Tag>} onClose={() => setCur(null)}
      footer={<><button className="btn">转复审</button><button className="btn d" onClick={() => setRej(true)}>驳回</button><button className="btn p" onClick={() => { say('已通过，用户升级为 ' + cur.level); setCur(null) }}>通过</button></>}>
      <div className="sec">证件材料</div><div className="grid g3"><div className="idimg">证件正面（演示占位）</div><div className="idimg">证件反面（演示占位）</div><div className="idimg">活体自拍（演示占位）</div></div>
      <div className="sec">OCR 识别 vs 用户填写</div><table className="t"><thead><tr><th>字段</th><th>OCR</th><th>用户填写</th><th>结果</th></tr></thead><tbody>
        {[['姓名', cur.name, cur.name], ['证件号', 'E1234****', 'E1234****'], ['出生日期', '1994-06-12', cur.ocr === '一致' ? '1994-06-12' : '1994-06-21'], ['国籍', cur.country, cur.country]].map(([a, b, c]) => <tr key={a}><td>{a}</td><td>{b}</td><td>{c}</td><td>{b === c ? <Tag k="ok">一致</Tag> : <Tag k="bad">不一致</Tag>}</td></tr>)}</tbody></table>
      <div className="sec">自动检查</div>
      {[['人脸相似度 ≥ 80%', cur.face >= 80, cur.face + '%'], ['活体检测', true, '通过'], ['证件有效期', true, '2031-03'], ['证件未被其他账户使用', !cur.dup, cur.dup ? '与 UID 80000037 重复' : '无'], ['制裁/PEP 名单', cur.aml === '无命中', cur.aml], ['限制国家/地区', true, '不在名单']].map(([l, ok, v]) =>
        <div className="chk" key={l}><span className={ok ? 'up' : 'down'}>{ok ? '✓' : '✕'}</span>{l}<span className="s muted">{v}</span></div>)}
      {rej && <Modal title="驳回原因" onClose={() => setRej(false)} footer={<><button className="btn" onClick={() => setRej(false)}>取消</button><button className="btn d" onClick={() => { say('已驳回并通知用户'); setRej(false); setCur(null) }}>确认驳回</button></>}>
        {['证件照片模糊/反光', '证件信息与填写不一致', '人脸比对未通过', '证件已过期', '不支持的国家/地区', '其他（自定义）'].map(x => <label key={x} className="chk"><input type="radio" name="rj" /> {x}</label>)}
        <textarea className="in" rows="3" placeholder="对用户展示的说明（支持多语言模板）" /></Modal>}
    </Drawer>}</>
}

export function RiskList() {
  const rows = users.filter(u => u.risk !== '低' || u.status !== '正常')
  return <><PageHead title="风险名单" desc="由风控引擎打标（多账户关联、刷量、短线套利返佣、异常登录）+ 人工维护"><button className="btn p">+ 加入名单</button></PageHead>
    <DataTable rows={rows} filters={<><input className="in" placeholder="UID" /><select className="sel"><option>全部标签</option><option>多账户关联</option><option>对敲刷量</option><option>短线返佣套利</option><option>异常登录</option></select></>}
      cols={[{ t: 'UID', k: 'uid' }, { t: '昵称', k: 'nick' }, { t: '风险', render: r => riskTag(r.risk) }, { t: '命中规则', render: r => ['同设备 6 账户', '对敲成交 32 笔', '持仓 < 60s 占比 82%', '异地登录'][+r.uid % 4] },
        { t: '处置', render: r => stTag(r.status) }, { t: '最近活动', k: 'last' }, { t: '操作', render: () => <><a className="btn g">查看</a><a className="btn g">移出</a></> }]} /></>
}
