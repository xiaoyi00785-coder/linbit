import { useState } from 'react'
import { DataTable, Drawer, Kpi, Modal, PageHead, Skeleton, Sw, Tabs, Tag, useApp, useLoading } from '../ui.jsx'
import { NETFEE, NETNAME, coins, feeChanges, feeRates, fmt, futures, pairs, riskTiers } from '../mock.js'
const Change = ({ onClose, title, children }) => { const { say } = useApp()
  return <Modal title={title} onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button><button className="btn p" onClick={() => { say('变更单已提交，待审批人确认后生效'); onClose() }}>提交变更单</button></>}>
    {children}<div className="sec">变更说明</div><textarea className="in" rows="2" placeholder="必填：变更原因" /><div className="fr" style={{ marginTop: 10 }}><label>生效方式</label><select className="sel"><option>审批通过后立即生效</option><option>定时生效</option></select></div>
    <div className="banner" style={{ marginBottom: 0 }}>参数变更走「提交 → 审批 → 生效」流程，前后值写入审计日志，可一键回滚。</div></Modal> }

export function Coins() {
  const [cur, setCur] = useState(null); const [add, setAdd] = useState(false)
  return <><PageHead title="币种与网络" desc="币种上下架、充提开关、链上 Gas 估算（平台提现手续费 0）、最小/最大提现、确认数"><button className="btn p" onClick={() => setAdd(true)}>+ 新增币种</button></PageHead>
    <DataTable rows={coins} onRow={setCur} filters={<><input className="in" placeholder="币种" /><select className="sel"><option>全部状态</option><option>已上线</option><option>待上线</option></select></>}
      cols={[{ t: '排序', k: 'sort' }, { t: '币种', k: 'coin', render: r => <b>{r.coin}</b> }, { t: '支持网络', render: r => r.nets.map(n => <Tag key={n}>{n}</Tag>) }, { t: '充值', render: r => <Sw on={r.dep} /> }, { t: '提现', render: r => <Sw on={r.wd} /> },
        { t: '最小提现', r: 1, k: 'min' }, { t: '单笔最大', r: 1, render: r => fmt(r.max, 0) }, { t: '确认数', k: 'conf' }, { t: '状态', render: r => <Tag k={r.status === '已上线' ? 'ok' : 'wn'}>{r.status}</Tag> }, { t: '操作', render: () => <a className="btn g">配置</a> }]} />
    {cur && <Drawer title={`币种配置 · ${cur.coin}`} onClose={() => setCur(null)} footer={<><button className="btn" onClick={() => setCur(null)}>取消</button><button className="btn p" onClick={() => setCur(null)}>提交变更单</button></>}>
      <div className="sec">网络配置</div><table className="t"><thead><tr><th>网络</th><th>充值</th><th>提现</th><th>平台费</th><th>Gas 估算</th><th>最小提现</th><th>确认数</th></tr></thead><tbody>
        {cur.nets.map(n => <tr key={n}><td>{n}</td><td><Sw on /></td><td><Sw on={n !== 'ERC20'} /></td><td>0</td><td className="muted">≈ {NETFEE[n]} USDT · 实时</td><td><input className="in" style={{ minWidth: 0, width: 70 }} defaultValue={cur.min} /></td><td>{cur.conf}</td></tr>)}</tbody></table>
      <div className="sec">限额</div><div className="fr"><label>单笔最大提现</label><input className="in" defaultValue={cur.max} /></div><div className="fr"><label>自动放行</label><span>≤ 1,000 USDT 且风险分 ≤ 20</span></div><div className="fr"><label>强制复核</label><span>≥ 5,000 USDT 或风险分 &gt; 40</span></div><div className="fr"><label>维护公告</label><input className="in" placeholder="关闭充提时向用户展示" /></div></Drawer>}
    {add && <Change title="新增币种（上币流程）" onClose={() => setAdd(false)}><div className="steps"><div className="done">基础信息</div><div className="cur">网络与合约地址</div><div>钱包接入测试</div><div>上线审批</div></div>
      <div className="fr"><label>币种代码</label><input className="in" placeholder="如 NEWT" /></div><div className="fr"><label>合约地址</label><input className="in" placeholder="0x…" /></div><div className="fr"><label>精度</label><input className="in" defaultValue="8" /></div></Change>}</>
}
export function Pairs() {
  const [m, setM] = useState(false)
  return <><PageHead title="现货交易对" desc="交易对上下架、价格/数量精度、最小下单额、分区；仅现货撮合，无杠杆"><button className="btn p" onClick={() => setM(true)}>+ 新增交易对</button></PageHead>
    <DataTable rows={pairs} select filters={<><input className="in" placeholder="交易对" /><select className="sel"><option>全部分区</option><option>主板</option><option>创新区</option><option>Meme</option></select><select className="sel"><option>全部状态</option><option>交易中</option><option>暂停交易</option><option>待上线</option></select></>}
      toolbar={sel => <><button className="btn" disabled={!sel.length}>批量暂停</button><button className="btn" disabled={!sel.length}>批量恢复</button></>}
      cols={[{ t: '交易对', k: 'sym', render: r => <b>{r.sym}</b> }, { t: '分区', k: 'zone' }, { t: '价格精度', k: 'tick' }, { t: '数量精度', k: 'lot' }, { t: '最小下单额', render: r => r.minNotional + ' USDT' }, { t: 'Maker/Taker', render: r => r.maker + ' / ' + r.taker },
        { t: '24h 成交额', r: 1, num: 1, render: r => '$' + fmt(r.vol, 0) }, { t: '状态', render: r => <Tag k={r.status === '交易中' ? 'ok' : r.status === '暂停交易' ? 'bad' : 'wn'}>{r.status}</Tag> }, { t: '操作', render: () => <><a className="btn g">编辑</a><a className="btn g">暂停</a></> }]} />
    {m && <Change title="新增交易对" onClose={() => setM(false)}><div className="fr"><label>基础币 / 计价币</label><input className="in" placeholder="NEWT / USDT" /></div><div className="fr"><label>价格精度</label><input className="in" defaultValue="0.0001" /></div>
      <div className="fr"><label>开盘方式</label><select className="sel"><option>定时开盘（先开放挂单 30 分钟）</option><option>立即开盘</option></select></div><div className="fr"><label>开盘时间</label><input className="in" type="datetime-local" /></div></Change>}</>
}
export function Futures() {
  const [cur, setCur] = useState(null); const [cls, setCls] = useState('全部')
  return <><PageHead title="合约参数" desc="USDT 本位永续 · 加密/黄金/美股 · Maker/Taker 统一 0.05%（费率在「手续费配置」统一管理）· 阶梯维持保证金" ><button className="btn p">+ 新增合约</button></PageHead>
    <div className="card"><Tabs value={cls} onChange={setCls} items={[['全部', '全部'], ['加密', '加密'], ['黄金', '黄金'], ['美股', '美股']]} /></div>
    <DataTable rows={futures.filter(f => cls === '全部' || f.cls === cls)} onRow={setCur} cols={[{ t: '合约', k: 'sym', render: r => <b>{r.sym}</b> }, { t: '类别', k: 'cls' }, { t: '最大杠杆', render: r => r.maxLev + 'x' }, { t: 'Maker', k: 'maker' }, { t: 'Taker', k: 'taker' },
      { t: '资金费周期', k: 'fundInt' }, { t: '费率上下限', k: 'fundCap' }, { t: '平台持仓上限', k: 'oiCap' }, { t: '状态', render: r => <Tag k={r.status === '交易中' ? 'ok' : 'wn'}>{r.status}</Tag> }, { t: '操作', render: () => <a className="btn g">风险参数</a> }]} />
    {cur && <Drawer width={720} title={`合约风险参数 · ${cur.sym}`} onClose={() => setCur(null)} footer={<><button className="btn">恢复上一版本</button><button className="btn" onClick={() => setCur(null)}>取消</button><button className="btn p" onClick={() => setCur(null)}>提交变更单</button></>}>
      <div className="grid g2"><div><div className="fr"><label>最大杠杆</label><input className="in" defaultValue={cur.maxLev} /></div><div className="fr"><label>最小下单量</label><input className="in" defaultValue="0.001" /></div><div className="fr"><label>价格偏离保护</label><input className="in" defaultValue="5%" /></div></div>
        <div><div className="fr"><label>资金费上限</label><input className="in" defaultValue="0.75%" /></div><div className="fr"><label>标记价格来源</label><select className="sel"><option>3 家指数加权</option></select></div><div className="fr"><label>交易状态</label><select className="sel"><option>交易中</option><option>仅减仓</option><option>暂停</option></select></div></div></div>
      <div className="sec">阶梯保证金（风险限额）</div><table className="t"><thead><tr><th>档位</th><th className="r">仓位价值上限 (USDT)</th><th className="r">最大杠杆</th><th className="r">维持保证金率</th><th className="r">初始保证金率</th></tr></thead><tbody>
        {riskTiers.map(t => <tr key={t.id}><td>{t.tier}</td><td className="r num">{fmt(t.cap, 0)}</td><td className="r">{Math.min(t.lev, cur.maxLev)}x</td><td className="r">{t.mmr}%</td><td className="r">{t.imr}%</td></tr>)}</tbody></table></Drawer>}</>
}
export function Fees() {
  const [m, setM] = useState(null); const [t, setT] = useState('rate'); const loading = useLoading()
  return <><PageHead title="手续费配置" desc="全站统一费率，不设 VIP 等级：现货 Maker/Taker 0.1% · 合约 Maker/Taker 0.05%（按仓位价值，开仓、平仓各收一次）· 提现平台手续费 0，仅收链上 Gas">
    <button className="btn" onClick={() => setT('log')}>变更记录</button><button className="btn p" onClick={() => setM(feeRates[0])}>发起费率变更</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="现货 Maker / Taker" v="0.10% / 0.10%" d="按成交额 · 收取到手币种" /><Kpi l="合约 Maker / Taker" v="0.05% / 0.05%" d="开仓 + 平仓各一次" /><Kpi l="提现平台手续费" v="0" d="仅扣链上 Gas（实时估算）" up /><Kpi l="待审批变更单" v="0" d="当前生效版本 v3" /></div>
    <div className="card"><Tabs value={t} onChange={setT} items={[['rate', '交易费率'], ['wd', '提现费用'], ['rule', '计费规则'], ['log', '变更记录', feeChanges.length]]} /></div>
    {loading ? <Skeleton /> : <>
    {t === 'rate' && <DataTable rows={feeRates} cols={[{ t: '市场', k: 'mkt', render: r => <b>{r.mkt}</b> }, { t: '方向', k: 'side' }, { t: '费率', r: 1, render: r => <span className="pri num" style={{ fontWeight: 600 }}>{r.rate}</span> },
      { t: '计费基数', k: 'base' }, { t: '收取币种', k: 'coin' }, { t: '版本', k: 'ver' }, { t: '状态', render: () => <Tag k="ok">生效中</Tag> }, { t: '操作', render: r => <a className="btn g" onClick={e => { e.stopPropagation(); setM(r) }}>变更</a> }]} />}
    {t === 'wd' && <div className="grid g21"><DataTable rows={Object.keys(NETFEE).map(n => ({ id: n, n }))} cols={[{ t: '网络', render: r => <b>{NETNAME[r.n]} <span className="muted">({r.n})</span></b> }, { t: '平台手续费', r: 1, render: () => '0' },
        { t: '链上 Gas 估算 (USDT)', r: 1, render: r => <span className="num">≈ {NETFEE[r.n]}</span> }, { t: 'Gas 来源', render: () => '节点实时估算 · 每 30s 刷新' }, { t: 'USDT 最小 / 单笔最大', r: 1, render: () => '12 / 20,000' }]} />
      <div className="card pad"><h3>提现计费口径</h3><div className="rule">用户到账 = 提现数量 − 链上 Gas</div><div className="rule">平台手续费固定为 <b>0</b>，前台显示「网络费（链上 Gas）≈ X · 平台手续费 0」</div>
        <div className="rule">Gas 波动超过估算 50% 时，暂停该网络提现并告警</div><div className="rule">USDT 单笔 <b>12 ~ 20,000</b>，日累计受风控限额约束</div></div></div>}
    {t === 'rule' && <div className="grid g2"><div className="card pad"><h3>交易手续费</h3><div className="rule">现货：成交才收费，按成交额 × <b>0.1%</b>，从收到的币种中扣除</div><div className="rule">合约：仓位价值 × <b>0.05%</b>，开仓、平仓各收一次</div>
        <div className="rule">强平单按 Taker 0.05% 计；资金费另计，不属于手续费</div><div className="rule">不设 VIP 分级；做市商特殊费率需单独审批，当前 0 个</div></div>
      <div className="card pad"><h3>与返佣 / 卡券的关系</h3><div className="rule">合伙人返佣与普通邀请返佣以手续费为基数，<b>不叠加</b></div><div className="rule">跟单分润不是手续费，不计入返佣基数</div>
        <div className="rule">手续费抵扣券仅抵扣合约手续费，抵扣部分不计返佣</div></div></div>}
    {t === 'log' && <DataTable rows={feeChanges} cols={[{ t: '变更单', k: 'id', render: r => <span className="pri">{r.id}</span> }, { t: '项目', k: 'item' }, { t: '变更前', k: 'from' }, { t: '变更后', k: 'to' },
      { t: '发起人', k: 'by' }, { t: '审批人', k: 'ap' }, { t: '状态', render: r => <Tag k={r.st === '已生效' ? 'ok' : 'bad'}>{r.st}</Tag> }, { t: '时间', k: 'time' }, { t: '操作', render: r => r.st === '已生效' ? <a className="btn g">回滚</a> : null }]} />}</>}
    {m && <Change title={`费率变更 · ${m.mkt} ${m.side}`} onClose={() => setM(null)}><div className="banner bad">费率直接影响用户资金，变更须超级管理员 + 财务复审双人审批，生效前 24h 在前台公告。</div>
      <div className="fr"><label>当前费率</label><span className="num">{m.rate}</span></div><div className="fr"><label>新费率</label><input className="in" defaultValue={m.rate.replace('%', '')} style={{ width: 100, minWidth: 0 }} /> %</div>
      <div className="fr"><label>同步修改</label><select className="sel"><option>Maker 与 Taker 同时修改</option><option>仅修改 {m.side}</option></select></div></Change>}</>
}
