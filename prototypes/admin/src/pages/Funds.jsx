import { useState } from 'react'
import { DataTable, Drawer, Kpi, Modal, PageHead, Tabs, Tag, eq, has, useApp, useFilter } from '../ui.jsx'
import { deposits, fmt, ledger, withdraws } from '../mock.js'
const wst = s => <Tag k={s === '已完成' ? 'ok' : s === '已驳回' ? 'bad' : s === '广播中' ? 'in' : 'wn'}>{s}</Tag>
const lv = l => <Tag k={l === '高' ? 'bad' : l === '中' ? 'wn' : 'ok'}>{l}</Tag>

function WdDrawer({ w, onClose }) {
  const { say, me } = useApp(); const [m, setM] = useState(null)
  const stage = w.status === '待初审' ? 1 : w.status === '待复核' || w.status === '待二审' ? 2 : 4
  const checks = [['账户状态正常、未冻结', true], ['24h 安全冷却（改密/解绑 2FA）', w.risk < 85, w.risk < 85 ? '无' : '改密后 3h'], ['提现地址在白名单', w.whitelist, w.whitelist ? '是' : '否'],
    ['首次使用的新地址', !w.newAddr, w.newAddr ? '新地址' : '历史地址'], ['链上地址风险（KYT）', w.risk < 70, w.risk < 70 ? '低风险' : '关联混币器 12%'], ['单笔/日累计限额（≤ 20,000 USDT）', true, '日累计 ' + fmt(w.amt) ],
    ['充提比 / 未交易直接提现', w.risk < 60, w.risk < 60 ? '正常' : '充值后 2h 内全额提现'], ['资产对账（账户余额 = 流水汇总）', true, '平衡'], ['同 IP / 设备关联账户', w.risk < 50, w.risk < 50 ? '0' : '3 个关联账户']]
  return <Drawer width={720} title={`提现审核 · ${w.id}`} extra={<>{wst(w.status)}<Tag k={w.level === '高' ? 'bad' : 'wn'}>风险分 {w.risk}</Tag></>} onClose={onClose}
    footer={stage < 4 ? <><button className="btn">挂起 / 联系用户</button><button className="btn d" onClick={() => setM('rej')}>驳回</button><button className="btn p" onClick={() => setM('ok')}>{stage === 1 ? '初审通过' : '复核通过并放行'}</button></> : null}>
    <div className="steps"><div className="done">用户提交</div><div className={stage > 1 ? 'done' : 'cur'}>风控检查 + 初审</div><div className={stage > 2 ? 'done' : stage === 2 ? 'cur' : ''}>复核（第二人）</div><div className={stage > 3 ? 'done' : ''}>签名广播</div><div className={w.status === '已完成' ? 'done' : ''}>链上确认</div></div>
    <dl className="kv"><dt>UID</dt><dd>{w.uid}</dd><dt>币种 / 网络</dt><dd>{w.coin} · {w.net}</dd><dt>提现数量</dt><dd className="num" style={{ fontSize: 16, fontWeight: 600 }}>{fmt(w.amt, w.coin === 'USDT' ? 2 : 4)} {w.coin}</dd>
      <dt>费用</dt><dd>链上 Gas ≈ {w.fee} USDT · 平台手续费 0 · 预计到账 {w.coin === 'USDT' ? fmt(w.amt - w.fee) + ' USDT' : '数量 − Gas'}</dd><dt>到账地址</dt><dd>{w.addr} <a className="btn g">区块浏览器 ↗</a></dd><dt>申请时间</dt><dd>{w.time}</dd><dt>初审人</dt><dd>{w.first}</dd><dt>复核人</dt><dd>{w.second}</dd></dl>
    <div className="sec">风控检查 <span className="muted" style={{ fontWeight: 400 }}>规则引擎自动执行 · 任一项失败即强制进入人工复核</span></div>
    {checks.map(([l, ok, v]) => <div className="chk" key={l}><span className={ok ? 'up' : 'down'}>{ok ? '✓' : '✕'}</span>{l}<span className="s muted">{v || '通过'}</span></div>)}
    <div className="sec">近 30 日资金画像</div><div className="grid g4">{[['充值', '$42,100'], ['提现', '$18,300'], ['交易量', '$1.28M'], ['盈亏', '+$2,410']].map(([a, b]) => <div className="card kpi" key={a}><div className="l">{a}</div><div className="v num" style={{ fontSize: 15 }}>{b}</div></div>)}</div>
    {m && <Modal title={m === 'ok' ? '确认审批' : '驳回提现'} onClose={() => setM(null)} footer={<><button className="btn" onClick={() => setM(null)}>取消</button>
      <button className={'btn ' + (m === 'ok' ? 'p' : 'd')} onClick={() => { say(m === 'ok' ? (stage === 1 && w.risk > 40 ? '初审通过，已流转至复核' : '已放行，进入签名队列') : '已驳回，资金已解冻退回'); setM(null); onClose() }}>确认</button></>}>
      {m === 'ok' && <><div className="banner">双人审批：初审与复核不能为同一管理员（当前：{me.user}）。≥ 5,000 USDT 或风险分 &gt; 40 必须复核。</div>
        <div className="fr"><label>Google 验证码</label><input className="in" placeholder="6 位动态码" /></div><div className="fr"><label>审批意见</label><input className="in" placeholder="选填" /></div></>}
      {m === 'rej' && <><div className="fr"><label>驳回原因</label><select className="sel"><option>地址风险</option><option>需补充资金来源</option><option>疑似盗号</option><option>其他</option></select></div>
        <div className="fr"><label>后续处置</label><select className="sel"><option>仅驳回，退回资金</option><option>驳回并冻结提现</option><option>驳回并冻结账户</option></select></div><textarea className="in" rows="3" placeholder="对用户说明" /></>}
    </Modal>}
  </Drawer>
}

export function Withdraw() {
  const [tab, setTab] = useState('待初审'); const [cur, setCur] = useState(null); const [rule, setRule] = useState(false)
  const [rows, set] = useFilter(withdraws.filter(w => tab === '全部' || w.status === tab), (r, f) => (has(r.uid, f.k) || has(r.id, f.k)) && eq(r.coin, f.c) && eq(r.net, f.n) && eq(r.level, f.l))
  return <><PageHead title="提现审核" desc="USDT 单笔 12 ~ 20,000 · 平台手续费 0，仅扣链上 Gas · ≤ 1,000 USDT 且风险分 ≤ 20 自动放行 · ≥ 5,000 USDT 或风险分 > 40 必须复核"><button className="btn" onClick={() => setRule(true)}>审批规则</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="待初审" v="9" d="最长等待 38 分钟" up={false} /><Kpi l="待复核" v="7" d="其中高风险 4" up={false} /><Kpi l="今日已放行" v="$2,184,320" d="1,284 笔 · 自动放行 92%" up /><Kpi l="热钱包余额" v="$11.82M" d="ERC20 低于阈值" up={false} /></div>
    <div className="card"><Tabs value={tab} onChange={setTab} items={[['待初审', '待初审', 9], ['待复核', '待复核', 7], ['广播中', '广播中'], ['已完成', '已完成'], ['已驳回', '已驳回'], ['全部', '全部']]} /></div>
    <DataTable rows={rows} select onRow={setCur} filters={<><input className="in" placeholder="UID / 提现单号 / 地址" onChange={set('k')} />
      <select className="sel" onChange={set('c')}><option>全部</option><option>USDT</option><option>BTC</option><option>ETH</option><option>SOL</option></select>
      <select className="sel" onChange={set('n')}><option>全部</option>{['TRC20', 'ERC20', 'BEP20', 'Arbitrum', 'Solana'].map(n => <option key={n}>{n}</option>)}</select>
      <select className="sel" onChange={set('l')}><option>全部</option><option>高</option><option>中</option><option>低</option></select></>}
      toolbar={sel => <button className="btn p" disabled={!sel.length}>批量初审通过（仅低风险）</button>}
      cols={[{ t: '单号', k: 'id', render: r => <span className="pri">{r.id}</span> }, { t: 'UID', k: 'uid' }, { t: '币种', k: 'coin' }, { t: '网络', k: 'net' }, { t: '数量', r: 1, num: 1, render: r => fmt(r.amt, r.coin === 'USDT' ? 2 : 4) },
        { t: '地址', k: 'addr', render: r => <>{r.addr} {r.newAddr && <Tag k="wn">新地址</Tag>}</> }, { t: '风险分', r: 1, render: r => <span className={r.risk > 70 ? 'down' : r.risk > 40 ? 'warn' : 'up'}>{r.risk}</span> }, { t: '等级', render: r => lv(r.level) },
        { t: '申请时间', k: 'time' }, { t: '状态', render: r => wst(r.status) }, { t: '操作', render: () => <a className="btn g">审核</a> }]} />
    {cur && <WdDrawer w={cur} onClose={() => setCur(null)} />}
    {rule && <Modal title="提现审批规则（当前生效 · 只读）" onClose={() => setRule(false)} footer={<button className="btn" onClick={() => setRule(false)}>关闭</button>}>
      <div className="rule"><Tag k="ok">自动放行</Tag>≤ 1,000 USDT 且风险分 ≤ 20，且所有风控检查通过</div>
      <div className="rule"><Tag k="wn">单人初审</Tag>1,000 ~ 5,000 USDT 且风险分 ≤ 40</div>
      <div className="rule"><Tag k="bad">初审 + 复核</Tag>≥ 5,000 USDT 或风险分 &gt; 40，复核人不可与初审人相同</div>
      <div className="rule"><Tag>限额</Tag>USDT 单笔 12 ~ 20,000；平台手续费 0，到账 = 数量 − 链上 Gas</div>
      <div className="muted" style={{ marginTop: 8 }}>阈值由业务确认，暂不开放修改；如需调整走「系统设置 → 变更单」。</div></Modal>}</>
}

export function Deposit() {
  const [rows, set] = useFilter(deposits, (r, f) => (has(r.uid, f.k) || has(r.txid, f.k)) && eq(r.status, f.s))
  return <><PageHead title="充值记录" desc="链上监听自动入账；确认数不足、未知币种、地址错误（需人工找回）进入待人工"><button className="btn">导出</button></PageHead>
    <DataTable rows={rows} filters={<><input className="in" placeholder="UID / TxID" onChange={set('k')} /><select className="sel" onChange={set('s')}><option>全部</option><option>已入账</option><option>确认中</option><option>待人工</option><option>已退回</option></select></>}
      cols={[{ t: '单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '币种', k: 'coin' }, { t: '网络', k: 'net' }, { t: '数量', r: 1, num: 1, render: r => fmt(r.amt) }, { t: '确认数', k: 'conf' }, { t: 'TxID', k: 'txid' },
        { t: '时间', k: 'time' }, { t: '状态', render: r => <Tag k={r.status === '已入账' ? 'ok' : r.status === '待人工' ? 'wn' : r.status === '已退回' ? 'bad' : 'in'}>{r.status}</Tag> },
        { t: '操作', render: r => r.status === '待人工' ? <a className="btn g">人工入账</a> : <a className="btn g">详情</a> }]} /></>
}

export function Ledger() {
  const [rows, set] = useFilter(ledger, (r, f) => has(r.uid, f.k) && eq(r.biz, f.b) && eq(r.acct, f.a))
  return <><PageHead title="资产流水" desc="复式记账：每笔业务至少一借一贷，借贷平衡；流水不可修改，冲正需新增反向分录"><button className="btn">对账报告</button><button className="btn">导出</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="今日分录数" v="4,812,330" /><Kpi l="借贷差额" v="0.00" d="✓ 平衡" up /><Kpi l="用户总负债 USDT" v="1,842,310,224" /><Kpi l="最近对账" v="09:00" d="链上 vs 账本 差异 0" up /></div>
    <DataTable rows={rows} filters={<><input className="in" placeholder="UID" onChange={set('k')} /><select className="sel" onChange={set('a')}><option>全部</option>{['现货账户', '合约账户', '跟单账户', '理财账户'].map(x => <option key={x}>{x}</option>)}</select>
      <select className="sel" onChange={set('b')}><option>全部</option>{['充值', '提现', '划转', '现货成交', '手续费', '资金费', '强平', '跟单分润', '活动奖励', '体验金回收'].map(x => <option key={x}>{x}</option>)}</select></>}
      cols={[{ t: '流水号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '账户', k: 'acct' }, { t: '币种', k: 'coin' }, { t: '业务类型', k: 'biz' }, { t: '借/贷', render: r => <span className={r.dir === '贷' ? 'up' : 'down'}>{r.dir}</span> },
        { t: '金额', r: 1, num: 1, render: r => (r.dir === '贷' ? '+' : '-') + fmt(r.amt) }, { t: '余额', r: 1, num: 1, render: r => fmt(r.bal) }, { t: '关联单号', k: 'ref' }, { t: '时间', k: 'time' }]} /></>
}
