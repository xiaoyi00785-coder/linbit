import { useState } from 'react'
import { DataTable, Drawer, Kpi, Modal, PageHead, Sw, Tabs, Tag, useApp } from '../ui.jsx'
import { campaigns, contents, fmt, pointsItems, rewards, traders } from '../mock.js'
const cst = s => <Tag k={s === '进行中' ? 'ok' : s === '待开始' ? 'in' : s === '草稿' ? '' : 'wn'}>{s}</Tag>
export function Campaigns() {
  const [cur, setCur] = useState(null)
  return <><PageHead title="活动管理" desc="新人任务、交易赛、邀请返佣、体验金、手续费优惠、签到积分"><button className="btn p" onClick={() => setCur(campaigns[0])}>+ 创建活动</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="进行中活动" v="4" /><Kpi l="本月预算" v="$300,000" d="已用 $123,910（41%）" /><Kpi l="参与人数" v="73,370" /><Kpi l="风控拦截奖励" v="1,284 笔" d="短持仓 / 对敲 / 多账户" up={false} /></div>
    <DataTable rows={campaigns} onRow={setCur} cols={[{ t: 'ID', k: 'id' }, { t: '活动名称', k: 'name', render: r => <b>{r.name}</b> }, { t: '类型', k: 'type' }, { t: '预算 USDT', r: 1, render: r => r.budget ? fmt(r.budget, 0) : '不限' },
      { t: '已发放', r: 1, num: 1, render: r => fmt(r.used, 0) }, { t: '消耗', render: r => r.budget ? <div className="bar" style={{ width: 80 }}><i style={{ width: Math.min(100, r.used / r.budget / 10) + '%' }} /></div> : '-' },
      { t: '参与人数', r: 1, render: r => fmt(r.part, 0) }, { t: '开始', k: 'start' }, { t: '结束', k: 'end' }, { t: '状态', render: r => cst(r.status) }, { t: '操作', render: () => <><a className="btn g">编辑</a><a className="btn g">数据</a></> }]} />
    {cur && <Drawer width={720} title={`活动配置 · ${cur.name}`} onClose={() => setCur(null)} footer={<><button className="btn">保存草稿</button><button className="btn">预览</button><button className="btn p" onClick={() => setCur(null)}>提交审批</button></>}>
      <div className="sec">基础</div><div className="fr"><label>活动名称（多语言）</label><input className="in" defaultValue={cur.name} /></div><div className="fr"><label>活动时间</label><input className="in" defaultValue="2026-11-01 00:00 ~ 2026-11-30 23:59" /></div>
      <div className="fr"><label>参与人群</label><select className="sel"><option>注册 30 天内新用户</option><option>全部 KYC 用户</option><option>合伙人下级用户</option><option>上传 UID 名单</option></select></div><div className="fr"><label>总预算</label><input className="in" defaultValue={cur.budget} /></div>
      <div className="sec">任务与奖励</div><table className="t"><thead><tr><th>任务</th><th>条件</th><th>奖励</th><th>有效期</th></tr></thead><tbody>
        {[['完成注册', '-', '5 USDT 体验金', '7 天'], ['完成 L1 KYC', '-', '10 USDT 手续费抵扣券', '14 天'], ['首次充值', '≥ 100 USDT', '20 USDT 体验金', '7 天'], ['首笔合约交易', '≥ 500 USDT 名义价值', '30 USDT 抵扣券', '14 天'], ['累计合约交易', '≥ 100,000 USDT', '150 USDT 抵扣券', '30 天']].map(r => <tr key={r[0]}>{r.map((c, i) => <td key={i}>{c}</td>)}</tr>)}</tbody></table>
      <div className="muted" style={{ marginTop: 6 }}>合计 215 USDT（体验金 + 手续费抵扣券）· 体验金盈利可提，本金到期回收</div>
      <div className="sec">反作弊</div>{[['同设备 / 同 IP 仅一个账户可领取', true], ['持仓时间 < 60 秒的成交不计入', true], ['对敲成交不计入', true], ['奖励发放前进入风控复核', false]].map(([l, on]) => <div className="chk" key={l}>{l}<span className="s"><Sw on={on} /></span></div>)}</Drawer>}</>
}
function RewardsDemo() {
  const [m, setM] = useState(false); const { say } = useApp()
  return <><PageHead title="奖励发放" desc="活动自动发放 + 人工补发；体验金/抵扣券/现金券/积分；支持回收"><button className="btn p" onClick={() => setM(true)}>+ 人工补发</button></PageHead>
    <DataTable rows={rewards} select filters={<><input className="in" placeholder="UID" /><select className="sel"><option>全部类型</option><option>体验金</option><option>手续费抵扣券</option><option>现金券</option></select><select className="sel"><option>全部状态</option><option>风控拦截</option></select></>}
      toolbar={sel => <button className="btn d" disabled={!sel.length}>批量回收</button>}
      cols={[{ t: '发放单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '类型', k: 'kind' }, { t: '面额', r: 1, render: r => r.amt + (r.kind === '积分' ? ' 分' : ' USDT') }, { t: '来源', k: 'src' },
        { t: '状态', render: r => <Tag k={r.status === '已发放' || r.status === '已使用' ? 'ok' : r.status === '风控拦截' ? 'bad' : ''}>{r.status}</Tag> }, { t: '时间', k: 'time' }]} />
    {m && <Modal title="人工补发奖励" onClose={() => setM(false)} footer={<><button className="btn" onClick={() => setM(false)}>取消</button><button className="btn p" onClick={() => { say('补发申请已提交，待运营主管审批'); setM(false) }}>提交审批</button></>}>
      <div className="fr"><label>UID（可批量）</label><textarea className="in" rows="2" defaultValue="80000001" /></div><div className="fr"><label>奖励类型</label><select className="sel"><option>手续费抵扣券</option><option>体验金</option><option>积分</option></select></div>
      <div className="fr"><label>面额</label><input className="in" defaultValue="10" /></div><div className="fr"><label>关联工单</label><input className="in" /></div><div className="banner" style={{ marginBottom: 0 }}>单次补发合计 &gt; 1,000 USDT 需财务复核。</div></Modal>}</>
}
export function Points() {
  const [t, setT] = useState('goods')
  return <><PageHead title="积分商城" desc="仅虚拟奖励（抵扣金 / 体验金），不设实物奖品 · 每档每人限兑 1 次 · 积分仅在积分活动期间有效，活动结束清零 · 任务每日 UTC 0 点重置"><button className="btn p">+ 新增兑换档位</button></PageHead>
    <div className="card"><Tabs value={t} onChange={setT} items={[['goods', '兑换档位', pointsItems.length], ['rule', '获取规则'], ['log', '兑换记录']]} />
      {t === 'goods' && <table className="t"><thead><tr><th className="r">所需积分</th><th>兑换奖励</th><th>类型</th><th className="r">已兑换</th><th>每人限兑</th><th>上架</th><th>操作</th></tr></thead><tbody>
        {pointsItems.map(p => <tr key={p.id}><td className="r num pri">{p.cost}</td><td>{p.name}</td><td><Tag k={p.name.includes('体验金') ? 'gd' : 'in'}>{p.name.includes('体验金') ? '体验金' : '抵扣金'}</Tag></td><td className="r num">{p.sold}</td><td>1 次</td><td><Sw on={p.on} /></td><td><a className="btn g">编辑</a></td></tr>)}</tbody></table>}
      {t === 'rule' && <table className="t"><thead><tr><th>任务</th><th>积分</th><th>每日上限</th><th>启用</th></tr></thead><tbody>
        {[['当日合约交易量 2千 / 1万 / 10万 / 20万 / 30万 / 50万 / 100万 / 300万 / 1000万 USDT（9 档）', '+1 / 1 / 3 / 3 / 5 / 8 / 12 / 35 / 100', '168 分'], ['入金：每 100 USDT', '+1', '10 分'], ['邀请有效用户（累计手续费 ≥ 100 USDT）', '+8 / 人', '80 分']].map(r => <tr key={r[0]}><td>{r[0]}</td><td>{r[1]}</td><td>{r[2]}</td><td><Sw on /></td></tr>)}</tbody></table>}
      {t === 'log' && <div className="empty">兑换记录（同奖励发放列表结构）</div>}</div></>
}
export function Copy() {
  const [t, setT] = useState('全部'); const [cur, setCur] = useState(null)
  return <><PageHead title="跟单交易" desc="仅合约跟单 · 带单员分润 0 ~ 50%（默认 10%）· 每日结算 · 分润不计入返佣 · 每用户最多跟随 10 位带单员 · 跟单 1 ~ 100x · 最低 10 USDT"><button className="btn">全局参数</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="带单员" v="1,284" d="申请中 4" /><Kpi l="跟单人数" v="38,210" /><Kpi l="跟单 AUM" v="$58.3M" /><Kpi l="昨日分润结算" v="$41,280" d="09:00 已完成" up /></div>
    <div className="card"><Tabs value={t} onChange={setT} items={[['全部', '全部'], ['申请中', '入驻申请', 4], ['带单中', '带单中'], ['暂停带单', '暂停'], ['已下架', '已下架']]} /></div>
    <DataTable rows={traders.filter(x => t === '全部' || x.status === t)} onRow={setCur} cols={[{ t: '带单员', render: r => <><b>{r.nick}</b> <span className="muted">{r.uid}</span></> }, { t: '收益率', r: 1, render: r => <span className={r.roi > 0 ? 'up' : 'down'}>{r.roi}%</span> },
      { t: '累计收益', r: 1, num: 1, render: r => fmt(r.pnl) }, { t: '最大回撤', r: 1, render: r => r.mdd + '%' }, { t: '跟随资金', r: 1, num: 1, render: r => '$' + fmt(r.aum, 0) }, { t: '跟随人数', r: 1, render: r => r.followers + ' / ' + r.maxF },
      { t: '分润比例', r: 1, render: r => r.share + '%' }, { t: '带单天数', r: 1, k: 'days' }, { t: '状态', render: r => <Tag k={r.status === '带单中' ? 'ok' : r.status === '申请中' ? 'in' : r.status === '已下架' ? 'bad' : 'wn'}>{r.status}</Tag> }, { t: '操作', render: r => <a className="btn g">{r.status === '申请中' ? '审核' : '管理'}</a> }]} />
    {cur && <Drawer title={`带单员 · ${cur.nick}`} onClose={() => setCur(null)} footer={cur.status === '申请中' ? <><button className="btn d">拒绝</button><button className="btn p" onClick={() => setCur(null)}>通过入驻</button></> : <><button className="btn">暂停带单</button><button className="btn d">下架并结算</button></>}>
      <dl className="kv"><dt>UID</dt><dd>{cur.uid}</dd><dt>入驻条件</dt><dd>KYC L2 ✓ · 合约账户 ≥ 500 USDT ✓ · 近 30 日交易 ≥ 10 笔 ✓</dd><dt>分润比例</dt><dd><input className="in" defaultValue={cur.share} style={{ width: 80, minWidth: 0 }} /> %（0 ~ 50，每日结算，不计入返佣）</dd>
        <dt>最大跟随人数</dt><dd><input className="in" defaultValue={cur.maxF} style={{ width: 100, minWidth: 0 }} /></dd><dt>可带单合约</dt><dd>BTCUSDT, ETHUSDT, SOLUSDT …</dd><dt>风控</dt><dd>回撤 &gt; 50% 自动暂停带单</dd></dl></Drawer>}</>
}
function ContentDemo() {
  const [cur, setCur] = useState(null)
  return <><PageHead title="内容与公告" desc="公告、上新、活动页、帮助中心、Banner、弹窗 · 多语言 · 定时发布 · 发布需审核"><button className="btn p" onClick={() => setCur({ title: '', cat: '公告' })}>+ 新建内容</button></PageHead>
    <DataTable rows={contents} onRow={setCur} filters={<><input className="in" placeholder="标题" /><select className="sel"><option>全部分类</option><option>公告</option><option>上新</option><option>活动</option><option>帮助中心</option><option>Banner</option></select></>}
      cols={[{ t: 'ID', k: 'id' }, { t: '标题', k: 'title', render: r => <>{r.top && <Tag k="gd">置顶</Tag>} {r.title}</> }, { t: '分类', k: 'cat' }, { t: '语言', k: 'lang' }, { t: '作者', k: 'author' }, { t: '浏览', r: 1, render: r => fmt(r.pv, 0) },
        { t: '状态', render: r => <Tag k={r.status === '已发布' ? 'ok' : r.status === '草稿' ? '' : 'in'}>{r.status}</Tag> }, { t: '更新时间', k: 'time' }]} />
    {cur && <Drawer width={760} title={cur.title ? '编辑内容' : '新建内容'} onClose={() => setCur(null)} footer={<><button className="btn">保存草稿</button><button className="btn">预览</button><button className="btn p" onClick={() => setCur(null)}>提交审核</button></>}>
      <div className="fr"><label>分类</label><select className="sel" defaultValue={cur.cat}><option>公告</option><option>上新</option><option>活动</option><option>帮助中心</option><option>Banner</option></select></div>
      <div className="tabs" style={{ padding: 0, marginBottom: 10 }}><span className="on">简体中文</span><span>English</span><span>日本語</span><span>한국어</span><span>Tiếng Việt</span></div>
      <div className="fr"><label>标题</label><input className="in" defaultValue={cur.title} /></div><div className="fr" style={{ alignItems: 'start' }}><label>正文</label><textarea className="in" rows="10" placeholder="富文本编辑器（原型占位）" /></div>
      <div className="fr"><label>推送渠道</label><span><label><input type="checkbox" defaultChecked /> 站内信</label> <label><input type="checkbox" /> App Push</label> <label><input type="checkbox" /> 邮件</label></span></div>
      <div className="fr"><label>发布时间</label><select className="sel"><option>审核通过后立即</option><option>定时</option></select></div><div className="fr"><label>置顶</label><Sw /></div></Drawer>}</>
}

const TIERS = [['普通合伙人', '审核通过', 30, 20], ['白银合伙人', '直推 30 日合约量 ≥ 5M USDT', 40, 25], ['黄金合伙人', '直推 30 日合约量 ≥ 20M USDT', 45, 30], ['钻石合伙人', '直推 30 日合约量 ≥ 50M USDT', 50, 35]]
export function Rebate() {
  return <><PageHead title="返佣配置" desc="合伙人等级 · 二级 Override · 普通邀请返佣 · 每日结算至资金账户 · 推荐关系终身绑定"><button className="btn p">提交变更单</button></PageHead>
    <div className="card"><h3>合伙人等级（按直推用户近 30 日合约交易量）</h3>
      <table className="tbl"><thead><tr><th>等级</th><th>升级条件</th><th className="r">合约返佣</th><th className="r">现货返佣</th></tr></thead>
        <tbody>{TIERS.map(r => <tr key={r[0]}><td><b>{r[0]}</b></td><td>{r[1]}</td><td className="r num"><input className="in" defaultValue={r[2]} style={{ width: 64, minWidth: 0 }} /> %</td><td className="r num"><input className="in" defaultValue={r[3]} style={{ width: 64, minWidth: 0 }} /> %</td></tr>)}</tbody></table></div>
    <div className="grid g2" style={{ marginTop: 12 }}>
      <div className="card"><h3>Override（上级合伙人）</h3><dl className="kv"><dt>比例</dt><dd>下级合伙人佣金的 10%（平台出资，不扣下级）</dd><dt>最多层级</dt><dd>2 级</dd><dt>结算</dt><dd>每日结算至资金账户</dd></dl></div>
      <div className="card"><h3>普通邀请返佣</h3><dl className="kv"><dt>比例</dt><dd>被邀请人手续费的 10%</dd><dt>有效期</dt><dd>注册起 1 年</dd><dt>层级</dt><dd>单级</dd><dt>叠加</dt><dd>与合伙人返佣不叠加（取合伙人）</dd><dt>排除</dt><dd>跟单分润不计入返佣基数</dd></dl></div></div></>
}

// 已接入真实接口的版本见 Live.jsx；上面的 *Demo 保留为原型参考
export { Rewards, Content } from './Live.jsx'
