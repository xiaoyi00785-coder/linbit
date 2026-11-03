import { BarChart, Donut, Kpi, LineChart, PageHead, Tabs, useApp } from '../ui.jsx'
import { fmt, withdraws } from '../mock.js'
import { useState } from 'react'
const days = Array.from({ length: 30 }, (_, i) => `10/${String(i + 3).padStart(2, '0')}`.replace('10/32', '11/01').replace('10/33', '11/02'))
const wave = (b, a, n = 30, k = 1) => Array.from({ length: n }, (_, i) => Math.round(b + a * Math.sin(i / 3 * k) + i * b * .01 + (i * 7919 % 13) * a * .05))
export default function Dashboard() {
  const { go } = useApp(); const [t, setT] = useState('vol')
  return <>
    <PageHead title="数据看板" desc="数据截至 2026-11-02 09:00（UTC-5）· 每 5 分钟刷新"><select className="sel"><option>近 30 天</option><option>今日</option><option>近 7 天</option></select><button className="btn">导出报表</button></PageHead>
    <div className="grid g6" style={{ marginBottom: 12 }}>
      <Kpi l="24h 总成交额" v="$1.284B" d="▲ 8.42% 较昨日" up /><Kpi l="合约成交额" v="$1.136B" d="▲ 9.10%" up /><Kpi l="现货成交额" v="$148.2M" d="▼ 2.31%" up={false} />
      <Kpi l="24h 手续费收入" v="$418,920" d="▲ 6.05%" up /><Kpi l="新注册 / 新 KYC" v="3,214 / 1,102" d="转化率 34.3%" /><Kpi l="日活用户 DAU" v="86,410" d="▲ 3.2%" up />
    </div>
    <div className="grid g6" style={{ marginBottom: 12 }}>
      <Kpi l="24h 净流入" v="+$12.48M" d="充 $38.1M / 提 $25.6M" up /><Kpi l="平台总资产" v="$2.913B" d="热钱包占比 6.4%" /><Kpi l="合约未平仓 OI" v="$684.5M" d="多空比 1.12" />
      <Kpi l="24h 强平金额" v="$9.82M" d="风险准备金 $41.2M" up={false} /><Kpi l="跟单 AUM" v="$58.3M" d="带单员 1,284" /><Kpi l="活动奖励支出" v="$62,410" d="预算消耗 41%" />
    </div>
    <div className="grid g21" style={{ marginBottom: 12 }}>
      <div className="card"><Tabs value={t} onChange={setT} items={[['vol', '成交额趋势'], ['fee', '手续费收入'], ['user', '用户增长']]} />
        <div style={{ padding: '8px 14px' }}><div className="legend">{t === 'vol' ? <><span><i style={{ background: 'var(--lb-primary)' }} />合约</span><span><i style={{ background: 'var(--lb-info)' }} />现货</span></> : t === 'fee' ? <span><i style={{ background: 'var(--lb-gold)' }} />手续费（千 USD）</span> : <><span><i style={{ background: 'var(--lb-primary)' }} />注册</span><span><i style={{ background: 'var(--lb-warn)' }} />KYC 通过</span></>}</div>
          {t === 'vol' && <LineChart labels={days} series={[{ data: wave(1000, 180), color: 'var(--lb-primary)' }, { data: wave(150, 30, 30, 1.6), color: 'var(--lb-info)' }]} h={230} />}
          {t === 'fee' && <BarChart labels={days} data={[wave(400, 60)]} colors={['var(--lb-gold)']} h={230} />}
          {t === 'user' && <BarChart labels={days} data={[wave(3000, 500), wave(1000, 200)]} colors={['var(--lb-primary)', 'var(--lb-warn)']} h={230} />}</div></div>
      <div className="card pad"><h3>待办事项<small>需要你处理</small></h3>
        {[['KYC 待审核', 14, 'kyc', 'wn'], ['提现待初审', 9, 'withdraw', 'wn'], ['提现待复核（高风险）', 7, 'withdraw', 'bad'], ['充值待人工确认', 3, 'deposit', ''], ['带单员入驻申请', 4, 'copy', ''], ['内容待审核', 1, 'content', '']]
          .map(([l, n, k, c]) => <div className="todo" key={l}><span className={'tag ' + c}>●</span>{l}<b className="num">{n}</b><a className="btn sm" onClick={() => go(k)}>处理</a></div>)}
        <div className="banner bad" style={{ marginTop: 10, marginBottom: 0 }}>⚠ ERC20 热钱包余额低于阈值（剩余 182,400 USDT / 阈值 300,000）</div></div>
    </div>
    <div className="grid g3">
      <div className="card pad"><h3>成交额构成（24h）</h3><div style={{ marginTop: 10 }}><Donut parts={[{ l: '加密合约', v: 962, color: 'var(--lb-primary)' }, { l: '黄金合约', v: 88, color: 'var(--lb-gold)' }, { l: '美股合约', v: 86, color: 'var(--lb-info)' }, { l: '现货', v: 148, color: 'var(--lb-text-3)' }]} /></div></div>
      <div className="card pad"><h3>热门合约 Top 5<small>24h 成交额</small></h3>
        {[['BTCUSDT', 512], ['ETHUSDT', 288], ['SOLUSDT', 96], ['XAUUSDT', 88], ['NVDAUSDT', 41]].map(([s, v]) => <div key={s} style={{ marginTop: 10 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}><span>{s}</span><span className="num t2">${v}M</span></div><div className="bar"><i style={{ width: v / 5.2 + '%' }} /></div></div>)}</div>
      <div className="card pad"><h3>最新大额提现<small><a className="pri" onClick={() => go('withdraw')}>全部 ›</a></small></h3>
        <table className="t" style={{ marginTop: 6 }}><tbody>{withdraws.filter(w => w.coin === 'USDT').slice(0, 6).map(w => <tr key={w.id}><td className="muted">{w.uid}</td><td className="num r">{fmt(w.amt)} USDT</td>
          <td><span className={'tag ' + (w.level === '高' ? 'bad' : w.level === '中' ? 'wn' : 'ok')}>风险{w.level}</span></td></tr>)}</tbody></table></div>
    </div></>
}
