import { useState } from 'react'
import { Confirm, DataTable, Drawer, Kpi, Modal, PageHead, Skeleton, Sw, Tabs, Tag, eq, has, useApp, useFilter, useLoading } from '../ui.jsx'
import { NETNAME, coupons, fmt, liqs, partners, pushes, sweeps, wallets } from '../mock.js'

/* 钱包管理：热/冷钱包余额、阈值告警、归集与补充（多签） */
export function Wallets() {
  const [t, setT] = useState('bal'); const [m, setM] = useState(null); const loading = useLoading()
  const hot = wallets.filter(w => w.kind === '热'), hotSum = hot.reduce((a, b) => a + b.bal, 0), coldSum = wallets.filter(w => w.kind === '冷').reduce((a, b) => a + b.bal, 0)
  return <><PageHead title="钱包管理" desc="热钱包按网络独立、低于阈值告警；冷钱包 3/5 多签离线；热→冷归集与冷→热补充均需多签确认"><button className="btn" onClick={() => setM('sweep')}>热→冷 归集</button><button className="btn p" onClick={() => setM('refill')}>冷→热 补充</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="热钱包合计" v={'$' + fmt(hotSum / 1e6) + 'M'} d={'占比 ' + (hotSum / (hotSum + coldSum) * 100).toFixed(2) + '%（目标 ≤ 2%）'} up /><Kpi l="冷钱包合计" v={'$' + fmt(coldSum / 1e9) + 'B'} d="多签 3/5" />
      <Kpi l="用户总负债" v="$1.84B" d="储备率 101.6%" up /><Kpi l="告警" v="1" d="Ethereum 热钱包低于阈值" up={false} /></div>
    <div className="card"><Tabs value={t} onChange={setT} items={[['bal', '钱包余额'], ['sw', '归集 / 补充记录', sweeps.length]]} /></div>
    {loading ? <Skeleton /> : t === 'bal' ? <DataTable rows={wallets} cols={[{ t: '钱包', k: 'name', render: r => <b>{r.name}</b> }, { t: '类型', render: r => <Tag k={r.kind === '热' ? 'wn' : 'in'}>{r.kind}钱包</Tag> }, { t: '网络', render: r => NETNAME[r.net] || '多链' },
      { t: '地址', k: 'addr' }, { t: '余额 (USDT 估值)', r: 1, num: 1, render: r => fmt(r.bal, 0) }, { t: '告警阈值', r: 1, num: 1, render: r => r.th ? fmt(r.th, 0) : '-' },
      { t: '水位', w: 140, render: r => r.th ? <div className={'bar' + (r.bal < r.th ? ' b' : '')}><i style={{ width: Math.min(100, r.bal / r.th / 3 * 100) + '%' }} /></div> : <span className="muted">-</span> },
      { t: '状态', render: r => <Tag k={r.st === '正常' ? 'ok' : 'bad'}>{r.st}</Tag> }]} />
      : <DataTable rows={sweeps} cols={[{ t: '单号', k: 'id', render: r => <span className="pri">{r.id}</span> }, { t: '类型', k: 'type' }, { t: '网络', render: r => NETNAME[r.net] }, { t: '金额 USDT', r: 1, num: 1, render: r => fmt(r.amt, 0) },
        { t: '签名进度', k: 'signers' }, { t: '发起人', k: 'by' }, { t: '状态', render: r => <Tag k={r.status === '已完成' ? 'ok' : r.status === '待签名' ? 'wn' : 'in'}>{r.status}</Tag> }, { t: '时间', k: 'time' }]} />}
    {m && <Confirm title={m === 'sweep' ? '发起热→冷归集' : '发起冷→热补充'} rows={[['网络', 'Ethereum (ERC20)'], ['金额', m === 'sweep' ? '500,000 USDT' : '1,000,000 USDT'], ['目标', m === 'sweep' ? '冷钱包 A（多签 3/5）' : 'Ethereum 热钱包'], ['签名', '需 3 / 5 签名人确认']]}
      okText="提交多签请求" onClose={() => setM(null)} />}</>
}

/* 风控监控：强平 / ADL / 风险保障基金 / 大户仓位 */
export function Liquidation() {
  const [cur, setCur] = useState(null)
  const [rows, set] = useFilter(liqs, (r, f) => has(r.uid, f.k) && eq(r.sym, f.s) && eq(r.type, f.t))
  return <><PageHead title="强平与风险" desc="强平记录、ADL 自动减仓、风险保障基金、平台净敞口；强平按标记价格触发，强平单手续费按 Taker 0.05% 计"><button className="btn">导出</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="风险保障基金" v="$8,420,310" d="▲ 近 24h +$12,410" up /><Kpi l="24h 强平笔数 / 金额" v="1,284 / $3.2M" d="多 62% · 空 38%" />
      <Kpi l="24h ADL 次数" v="0" d="保险基金充足" up /><Kpi l="维持保证金率 > 80% 账户" v="214" d="需关注" up={false} /></div>
    <div className="grid g21" style={{ marginBottom: 12 }}><div className="card pad"><h3>平台净敞口（Top 合约）</h3>
      {[['BTCUSDT', 62, '多'], ['ETHUSDT', 48, '空'], ['SOLUSDT', 31, '多'], ['XAUUSDT', 18, '空'], ['NVDAUSDT', 9, '多']].map(([s, v, d]) => <div className="rule" key={s}><b style={{ width: 90 }}>{s}</b><div className={'bar' + (v > 50 ? ' w' : '')} style={{ flex: 1 }}><i style={{ width: v + '%' }} /></div><span className={d === '多' ? 'up' : 'down'}>{d} {v}%</span><span className="muted">/ 持仓上限</span></div>)}</div>
      <div className="card pad"><h3>自动风控规则</h3><div className="rule">标记价格偏离指数 &gt; 5% → 暂停开仓</div><div className="rule">保险基金单日回撤 &gt; 10% → 告警</div><div className="rule">保险基金不足 → 触发 ADL</div><div className="rule">单用户持仓 &gt; 合约上限 20% → 仅减仓</div></div></div>
    <DataTable rows={rows} onRow={setCur} filters={<><input className="in" placeholder="UID" onChange={set('k')} /><select className="sel" onChange={set('s')}><option>全部</option>{['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'BNBUSDT'].map(x => <option key={x}>{x}</option>)}</select>
      <select className="sel" onChange={set('t')}><option>全部</option><option>强平</option><option>ADL 减仓</option></select></>}
      cols={[{ t: '单号', k: 'id', render: r => <span className="pri">{r.id}</span> }, { t: 'UID', k: 'uid' }, { t: '合约', k: 'sym' }, { t: '方向', render: r => <span className={r.side === '多' ? 'up' : 'down'}>{r.side}</span> }, { t: '杠杆', render: r => r.lev + 'x' },
        { t: '仓位价值', r: 1, num: 1, render: r => fmt(r.size, 0) }, { t: '强平价', r: 1, num: 1, render: r => fmt(r.price) }, { t: '用户损失', r: 1, num: 1, render: r => <span className="down">-{fmt(r.loss)}</span> },
        { t: '保险基金', r: 1, num: 1, render: r => <span className={r.ins >= 0 ? 'up' : 'down'}>{r.ins >= 0 ? '+' : ''}{fmt(r.ins)}</span> }, { t: '类型', render: r => <Tag k={r.type === '强平' ? 'bad' : 'wn'}>{r.type}</Tag> }, { t: '时间', k: 'time' }]} />
    {cur && <Drawer title={`强平详情 · ${cur.id}`} onClose={() => setCur(null)}><dl className="kv"><dt>UID</dt><dd>{cur.uid}</dd><dt>合约 / 方向</dt><dd>{cur.sym} · {cur.side} · {cur.lev}x</dd><dt>仓位价值</dt><dd>{fmt(cur.size, 0)} USDT</dd>
      <dt>强平价 / 标记价</dt><dd>{fmt(cur.price)} / {fmt(cur.price * 1.001)}</dd><dt>强平手续费</dt><dd>{fmt(cur.size * 0.0005)} USDT（0.05%）</dd><dt>保险基金变动</dt><dd>{fmt(cur.ins)} USDT</dd><dt>时间</dt><dd>{cur.time}</dd></dl>
      <div className="sec">触发链路</div><div className="tl"><div>维持保证金率达到 100%</div><div>撤销该用户全部挂单</div><div>强平引擎接管，按破产价挂单</div><div>成交完毕，剩余保证金注入保险基金</div></div></Drawer>}</>
}

/* 合伙人管理：4 档等级、上级 override、每日结算 */
const TIERS = [['普通', '-', 30, 20], ['银牌', '≥ 5M', 40, 25], ['金牌', '≥ 20M', 45, 30], ['钻石', '≥ 50M', 50, 35]]
export function Partners() {
  const [t, setT] = useState('全部'); const [cur, setCur] = useState(null); const [m, setM] = useState(null)
  const [rows, set] = useFilter(partners.filter(p => t === '全部' || p.status === t || p.tier === t), (r, f) => has(r.uid, f.k) || has(r.nick, f.k))
  return <><PageHead title="合伙人管理" desc="等级按直属下级近 30 日合约交易量自动评定 · 上级 override 10%（出自平台份额，最多 2 级）· 每日结算 · 终身有效 · 与普通邀请不叠加 · 跟单分润不计返佣"><a className="btn" href="#/rebate">返佣规则配置</a></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}>{TIERS.map(([n, v, f, s]) => <div className="card kpi" key={n}><div className="l"><span>{n} · 直属 30 日合约量 {v}</span></div><div className="v num">{f}% / {s}%</div><div className="d muted">合约 / 现货返佣 · {partners.filter(p => p.tier === n).length} 人</div></div>)}</div>
    <div className="card"><Tabs value={t} onChange={setT} items={[['全部', '全部'], ['申请中', '入驻申请', 3], ['普通', '普通'], ['银牌', '银牌'], ['金牌', '金牌'], ['钻石', '钻石'], ['已冻结', '已冻结']]} /></div>
    <DataTable rows={rows} onRow={setCur} filters={<><input className="in" placeholder="UID / 昵称" onChange={set('k')} /></>} toolbar={<button className="btn">导出结算单</button>}
      cols={[{ t: '合伙人', render: r => <><b>{r.nick}</b> <span className="muted">{r.uid}</span></> }, { t: '等级', render: r => <Tag k="gd">{r.tier}</Tag> }, { t: '合约 / 现货', render: r => r.fut + '% / ' + r.spot + '%' },
        { t: '直属 30 日合约量', r: 1, num: 1, render: r => '$' + fmt(r.vol, 0) }, { t: '直属人数', r: 1, k: 'invitees' }, { t: '上级', k: 'up' }, { t: '昨日返佣', r: 1, num: 1, render: r => fmt(r.today) }, { t: '累计返佣', r: 1, num: 1, render: r => fmt(r.total) },
        { t: '状态', render: r => <Tag k={r.status === '正常' ? 'ok' : r.status === '申请中' ? 'in' : 'bad'}>{r.status}</Tag> }]} />
    {cur && <Drawer width={680} title={`合伙人 · ${cur.nick}`} extra={<Tag k="gd">{cur.tier}</Tag>} onClose={() => setCur(null)}
      footer={cur.status === '申请中' ? <><button className="btn d">拒绝</button><button className="btn p" onClick={() => { setCur(null) }}>通过入驻</button></> : <><button className="btn" onClick={() => setM('tier')}>手动调整等级</button><button className="btn d" onClick={() => setM('freeze')}>冻结返佣</button></>}>
      <dl className="kv"><dt>UID</dt><dd>{cur.uid}</dd><dt>返佣比例</dt><dd>合约 {cur.fut}% · 现货 {cur.spot}%（以直属下级手续费为基数）</dd><dt>上级 override</dt><dd>{cur.up === '-' ? '无上级' : `上级 ${cur.up} 获得平台份额的 10%（最多 2 级）`}</dd>
        <dt>距下一档</dt><dd>{cur.tier === '钻石' ? '已最高档' : '还差 $' + fmt(([5e6, 2e7, 5e7][['普通', '银牌', '金牌'].indexOf(cur.tier)]) - cur.vol, 0)}</dd><dt>结算</dt><dd>每日 09:00（UTC+8）结算昨日，自动入现货账户</dd></dl>
      <div className="sec">近 7 日结算</div><table className="t"><thead><tr><th>日期</th><th className="r">下级手续费</th><th className="r">返佣</th><th>状态</th></tr></thead><tbody>
        {[1, 2, 3, 4, 5, 6, 7].map(d => <tr key={d}><td>{new Date(Date.UTC(2026, 10, 3 - d)).toISOString().slice(0, 10)}</td><td className="r num">{fmt(cur.today / cur.fut * 100 * (1 + d / 10))}</td><td className="r num">{fmt(cur.today * (1 + d / 10))}</td><td><Tag k="ok">已入账</Tag></td></tr>)}</tbody></table>
      {m && <Confirm danger={m === 'freeze'} title={m === 'tier' ? '手动调整等级' : '冻结返佣'} rows={m === 'tier' ? [['当前', cur.tier], ['调整为', '银牌（保底 30 天）']] : [['合伙人', cur.uid], ['影响', '冻结期间返佣挂起，不入账']]} onClose={() => setM(null)} />}</Drawer>}</>
}

/* 卡券管理 */
export function Coupons() {
  const [m, setM] = useState(false); const { say } = useApp()
  return <><PageHead title="卡券管理" desc="手续费抵扣券（仅合约手续费，抵扣部分不计返佣）· 合约体验金（本金到期回收，盈利可提）· 现金券"><button className="btn p" onClick={() => setM(true)}>+ 新建卡券</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="发放中模板" v="4" /><Kpi l="本月已发放" v="$58,210" d="预算 $120,000" /><Kpi l="核销率" v="61.4%" up d="▲ 4.1%" /><Kpi l="风控拦截" v="38" d="同设备批量领取" up={false} /></div>
    <DataTable rows={coupons} cols={[{ t: '编号', k: 'id' }, { t: '名称', k: 'name', render: r => <b>{r.name}</b> }, { t: '类型', render: r => <Tag k={r.kind === '体验金' ? 'gd' : r.kind === '现金券' ? 'ok' : 'in'}>{r.kind}</Tag> }, { t: '面额 USDT', r: 1, k: 'val' },
      { t: '已发 / 总量', r: 1, render: r => fmt(r.used, 0) + ' / ' + fmt(r.tot, 0) }, { t: '进度', w: 120, render: r => <div className="bar"><i style={{ width: r.used / r.tot * 100 + '%' }} /></div> }, { t: '有效期', k: 'exp' },
      { t: '状态', render: r => <Tag k={r.status === '发放中' ? 'ok' : ''}>{r.status}</Tag> }, { t: '操作', render: () => <><a className="btn g">编辑</a><a className="btn g">停发</a></> }]} />
    {m && <Modal title="新建卡券" onClose={() => setM(false)} footer={<><button className="btn" onClick={() => setM(false)}>取消</button><button className="btn p" onClick={() => { say('卡券模板已保存为草稿'); setM(false) }}>保存草稿</button></>}>
      <div className="fr"><label>类型</label><select className="sel"><option>手续费抵扣券</option><option>合约体验金</option><option>现金券</option></select></div><div className="fr"><label>面额 USDT</label><input className="in" defaultValue="10" /></div>
      <div className="fr"><label>总量</label><input className="in" defaultValue="1000" /></div><div className="fr"><label>使用门槛</label><input className="in" placeholder="如 合约交易额 ≥ 500 USDT" /></div><div className="fr"><label>有效期</label><select className="sel"><option>领取后 N 天</option><option>固定日期</option></select></div>
      <div className="fr"><label>每人限领</label><input className="in" defaultValue="1" /></div><div className="banner" style={{ marginBottom: 0 }}>预算 &gt; 10,000 USDT 的卡券需财务审批后才能发放。</div></Modal>}</>
}

/* 消息推送 */
export function Notify() {
  const [m, setM] = useState(false); const { say } = useApp()
  return <><PageHead title="消息推送" desc="站内信 / 邮件 / App 推送；支持人群包、定时发送、多语言；交易与安全类通知由系统自动触发，不在此发送"><button className="btn p" onClick={() => setM(true)}>+ 新建推送</button></PageHead>
    <div className="grid g2" style={{ marginBottom: 12 }}><div className="card pad"><h3>系统自动通知（模板）</h3>{['登录新设备', '提现申请 / 到账', '充值到账', '强平预警（保证金率 80%）', '强平完成', '返佣每日结算', '跟单分润结算', 'KYC 结果'].map(x => <div className="rule" key={x}><Sw on />{x}<span className="sp" /><a className="btn g" style={{ marginLeft: 'auto' }}>编辑模板</a></div>)}</div>
      <div className="card pad"><h3>发送概况（7 日）</h3><div className="grid g2" style={{ marginTop: 8 }}><Kpi l="站内信" v="1.82M" d="已读 41%" /><Kpi l="邮件" v="312K" d="送达 98.7%" up /><Kpi l="App 推送" v="904K" d="点击 6.2%" /><Kpi l="退订" v="1,204" /></div></div></div>
    <DataTable rows={pushes} cols={[{ t: '编号', k: 'id' }, { t: '标题', k: 'title', render: r => <b>{r.title}</b> }, { t: '人群', k: 'aud' }, { t: '渠道', k: 'ch' }, { t: '触达', r: 1, num: 1, render: r => fmt(r.n, 0) },
      { t: '状态', render: r => <Tag k={r.st === '已发送' ? 'ok' : r.st === '定时发送' ? 'in' : ''}>{r.st}</Tag> }, { t: '时间', k: 'time' }, { t: '操作', render: r => r.st === '已发送' ? <a className="btn g">数据</a> : <><a className="btn g">编辑</a><a className="btn g">撤回</a></> }]} />
    {m && <Modal width={620} title="新建推送" onClose={() => setM(false)} footer={<><button className="btn" onClick={() => setM(false)}>取消</button><button className="btn">发送测试</button><button className="btn p" onClick={() => { say('已提交审核，审核通过后按计划发送'); setM(false) }}>提交审核</button></>}>
      <div className="fr"><label>渠道</label><span><label><input type="checkbox" defaultChecked /> 站内信</label> <label><input type="checkbox" /> 邮件</label> <label><input type="checkbox" /> App 推送</label></span></div>
      <div className="fr"><label>人群</label><select className="sel"><option>全部用户</option><option>合约用户</option><option>合伙人</option><option>上传 UID 名单</option></select></div><div className="fr"><label>标题</label><input className="in" /></div>
      <textarea className="in" rows="4" placeholder="正文，支持 {nick} {uid} 变量" /><div className="fr" style={{ marginTop: 8 }}><label>发送时间</label><select className="sel"><option>立即</option><option>定时</option></select></div>
      <div className="banner" style={{ marginBottom: 0 }}>全量推送需运营负责人审核；禁止包含外部链接与收款地址。</div></Modal>}</>
}
