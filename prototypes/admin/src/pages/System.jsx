import { useState } from 'react'
import { DataTable, Drawer, PageHead, Sw, Tabs, Tag } from '../ui.jsx'
import { admins, audits } from '../mock.js'
export const ROLES = ['超级管理员', '运营', 'KYC 审核员', '财务初审', '财务复审', '风控专员', '客服', '市场', '审计（只读）']
const MODS = [['数据看板', 'dash'], ['用户', 'user'], ['KYC', 'kyc'], ['提现审核', 'wd'], ['充值/流水', 'fund'], ['订单/持仓', 'trade'], ['币种/交易对', 'coin'], ['合约参数', 'fut'], ['手续费', 'fee'], ['钱包', 'wallet'], ['强平风控', 'risk'], ['活动/奖励', 'mkt'], ['跟单', 'copy'], ['内容', 'cms'], ['RBAC', 'rbac'], ['审计日志', 'audit'], ['系统设置', 'sys']]
// V 查看 E 编辑 A 审批 X 导出；- 无
const M = { '超级管理员': 'VEAX'.repeat(1), 运营: { dash: 'V', user: 'VE', kyc: 'V', trade: 'V', coin: 'V', mkt: 'VE', copy: 'VE', cms: 'VE' }, 'KYC 审核员': { user: 'V', kyc: 'VEA' },
  财务初审: { dash: 'V', user: 'V', wd: 'VA', fund: 'VX' }, 财务复审: { dash: 'V', user: 'V', wd: 'VA', fund: 'VX', mkt: 'A' }, 风控专员: { dash: 'V', user: 'VE', kyc: 'V', wd: 'V', fund: 'V', trade: 'VE', fut: 'VE', copy: 'V' },
  客服: { user: 'V', kyc: 'V', fund: 'V', trade: 'V' }, 市场: { dash: 'V', mkt: 'VE', cms: 'VE', copy: 'V' }, '审计（只读）': Object.fromEntries(MODS.map(m => [m[1], 'VX'])) }
const cell = (r, m) => r === '超级管理员' ? 'VEAX' : (M[r][m] || '')
function AdminsDemo() {
  const [t, setT] = useState('admin'); const [role, setRole] = useState('风控专员'); const [cur, setCur] = useState(null)
  return <><PageHead title="管理员与角色" desc="RBAC：用户 → 角色 → 权限点（菜单/按钮/数据范围）· 强制 2FA + IP 白名单 · 职责分离"><button className="btn p">{t === 'admin' ? '+ 新增管理员' : '+ 新增角色'}</button></PageHead>
    <div className="card"><Tabs value={t} onChange={setT} items={[['admin', '管理员', admins.length], ['role', '角色权限矩阵'], ['sod', '职责分离规则']]} /></div>
    {t === 'admin' && <DataTable rows={admins} onRow={setCur} cols={[{ t: '账号', k: 'user', render: r => <b>{r.user}</b> }, { t: '角色', k: 'role', render: r => <Tag k={r.role === '超级管理员' ? 'gd' : ''}>{r.role}</Tag> }, { t: '部门', k: 'dept' },
      { t: '2FA', render: () => <Tag k="ok">已绑定</Tag> }, { t: '登录 IP', k: 'ip' }, { t: '最近登录', k: 'last' }, { t: '状态', render: r => <Tag k={r.status === '启用' ? 'ok' : 'bad'}>{r.status}</Tag> }, { t: '操作', render: () => <><a className="btn g">编辑</a><a className="btn g">重置密码</a></> }]} />}
    {t === 'role' && <div className="card" style={{ marginTop: 12 }}><div className="filters"><span className="muted">V 查看 · E 编辑 · A 审批 · X 导出</span><span style={{ marginLeft: 'auto' }} className="muted">点击角色列头编辑</span></div><div className="tw"><table className="t mx"><thead><tr><th>模块 \ 角色</th>{ROLES.map(r => <th key={r} style={{ cursor: 'pointer', color: r === role ? 'var(--lb-primary)' : null }} onClick={() => setRole(r)}>{r}</th>)}</tr></thead>
      <tbody>{MODS.map(([n, k]) => <tr key={k}><td>{n}</td>{ROLES.map(r => { const v = cell(r, k); return <td key={r} className={v.includes('A') ? 'warn' : v ? 'pri' : 'muted'}>{v || '—'}</td> })}</tr>)}</tbody></table></div></div>}
    {t === 'sod' && <div className="card pad" style={{ marginTop: 12 }}>{[['提现：初审人 ≠ 复核人', '强制'], ['参数变更：提交人 ≠ 审批人', '强制'], ['冻结/解冻、强制减仓：需第二人确认', '强制'], ['人工调账：财务提交 + 财务复审 + 风控知会', '强制'], ['管理员不得审批与自己 UID 绑定的用户', '强制'], ['超级管理员操作同样写审计，且每日推送摘要给内审', '强制']].map(([a, b]) => <div className="chk" key={a}>{a}<span className="s"><Tag k="ok">{b}</Tag></span></div>)}</div>}
    {cur && <Drawer title={`管理员 · ${cur.user}`} onClose={() => setCur(null)} footer={<button className="btn p" onClick={() => setCur(null)}>保存</button>}>
      <div className="fr"><label>角色</label><select className="sel" defaultValue={cur.role}>{ROLES.map(r => <option key={r}>{r}</option>)}</select></div><div className="fr"><label>数据范围</label><select className="sel"><option>全部用户</option><option>指定国家/地区</option><option>指定代理线</option></select></div>
      <div className="fr"><label>IP 白名单</label><input className="in" defaultValue={cur.ip + '/32'} /></div><div className="fr"><label>敏感字段脱敏</label><Sw on /></div><div className="fr"><label>会话超时</label><select className="sel"><option>30 分钟</option></select></div></Drawer>}</>
}
function AuditDemo() {
  const [cur, setCur] = useState(null)
  return <><PageHead title="审计日志" desc="所有后台操作（含查看敏感信息、导出）不可篡改留存 ≥ 5 年 · 前后值对比"><button className="btn">导出</button></PageHead>
    <DataTable rows={audits} onRow={setCur} filters={<><input className="in" placeholder="管理员 / 对象" /><select className="sel"><option>全部模块</option></select><select className="sel"><option>全部级别</option><option>敏感</option><option>高危</option></select><input className="in" type="date" /></>}
      cols={[{ t: '时间', k: 'time' }, { t: '管理员', k: 'who' }, { t: '角色', k: 'role' }, { t: '模块', k: 'mod' }, { t: '操作', k: 'act' }, { t: '对象', k: 'target' }, { t: 'IP', k: 'ip' },
        { t: '级别', render: r => <Tag k={r.risk === '高危' ? 'bad' : r.risk === '敏感' ? 'wn' : ''}>{r.risk}</Tag> }, { t: '结果', render: r => <span className={r.result === '成功' ? 'up' : 'down'}>{r.result}</span> }]} />
    {cur && <Drawer title={`审计详情 · ${cur.id}`} onClose={() => setCur(null)}><dl className="kv"><dt>管理员</dt><dd>{cur.who}（{cur.role}）</dd><dt>操作</dt><dd>{cur.mod} · {cur.act}</dd><dt>对象</dt><dd>{cur.target}</dd><dt>IP / UA</dt><dd>{cur.ip} · Chrome 130</dd><dt>请求 ID</dt><dd>req_7f3a9c21</dd></dl>
      <div className="sec">变更对比</div><div className="grid g2"><pre className="card pad down" style={{ margin: 0 }}>{'{\n  "maxLeverage": 125,\n  "status": "交易中"\n}'}</pre><pre className="card pad up" style={{ margin: 0 }}>{'{\n  "maxLeverage": 100,\n  "status": "仅减仓"\n}'}</pre></div></Drawer>}</>
}
export function Settings() {
  const [t, setT] = useState('sec')
  const rows = { sec: [['后台强制 2FA', true], ['IP 白名单登录', true], ['新设备登录二次验证', true], ['敏感数据默认脱敏', true]],
    user: [['改密后禁止提现 24 小时', true], ['解绑 2FA 后禁止提现 24 小时', true], ['绑定邮箱/手机后禁止提现 6 小时', true], ['未 KYC 用户可提现', false]],
    fund: [['低风险小额提现自动放行（≤ 1,000 USDT 且风险分 ≤ 20）', true], ['热钱包低余额告警', true], ['每日 09:00 链上对账', true]],
    sys: [['全站维护模式', false], ['暂停全部提现（紧急开关）', false], ['暂停新用户注册', false], ['仅减仓模式（全合约）', false]] }
  return <><PageHead title="系统设置" desc="全局安全、用户安全策略、资金策略、紧急开关；所有开关变更需审批" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['sec', '后台安全'], ['user', '用户安全'], ['fund', '资金策略'], ['sys', '紧急开关'], ['notify', '通知模板']]} />
      <div style={{ padding: 14 }}>{t === 'sys' && <div className="banner bad">紧急开关影响全站用户，需超级管理员 + 风控负责人双人确认。</div>}
        {(rows[t] || [['邮件模板：提现成功 / 冻结通知 / KYC 结果 …', true], ['短信通道：主备切换', true]]).map(([l, on]) => <div className="chk" key={l}>{l}<span className="s"><Sw on={on} /></span></div>)}</div></div></>
}

// 已接入真实接口的版本见 Live.jsx；上面的 *Demo 保留为原型参考
export { Admins, Audit } from './Live.jsx'
