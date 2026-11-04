import { ApiState, BarChart, Empty, Kpi, LineChart, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { ft, num, useApi } from '../api.js'
import { useState } from 'react'

const usd = v => v === null || v === undefined ? '-' : '$' + num(v, 0)
const sumAmt = (rows, asset = 'USDT') => (rows || []).filter(r => r.asset === asset).reduce((a, r) => a + Number(r.amount || 0), 0)
const WD_ST = { pending_review: '待初审', approved: '待复审/执行', processing: '处理中', completed: '已完成', rejected: '已驳回', failed: '失败', canceled: '已取消' }

export default function Dashboard() {
  const { go } = useApp(); const [t, setT] = useState('vol')
  const q = useApi('dashboard/summary')
  const d = q.data || {}
  const daily = d.daily || []
  const labels = daily.map(x => x.day.slice(5).replace('-', '/'))
  const wd = Object.fromEntries((d.withdrawStatus || []).map(x => [x.status, x.count]))
  const u = d.users || {}, f = d.futures24h || {}, s = d.spot24h || {}, oi = d.openInterest || {}, lq = d.liquidations24h || {}
  const spotFeeUsdt = sumAmt(d.spotFees24h)
  const top = d.topContracts || []; const topMax = Math.max(1, ...top.map(x => Number(x.volume)))
  return <>
    <PageHead title="数据看板" desc={'数据截至 ' + (d.generatedAt ? ft(d.generatedAt) : '-') + '（本地时间）· 实时查询'}><button className="btn" onClick={q.reload}>刷新</button></PageHead>
    <ApiState q={q}>
      {d.errors && d.errors.length > 0 && <div className="banner bad">部分指标查询失败：{d.errors.join('、')}</div>}
      <div className="grid g6" style={{ marginBottom: 12 }}>
        <Kpi l="24h 合约成交额" v={usd(f.volume)} d={(f.trades ?? '-') + ' 笔'} /><Kpi l="24h 现货成交额" v={usd(s.quoteVolume)} d={(s.trades ?? '-') + ' 笔（计价币）'} />
        <Kpi l="24h 合约手续费" v={usd(f.fees)} /><Kpi l="24h 现货手续费 (USDT)" v={usd(spotFeeUsdt)} d={(d.spotFees24h || []).filter(x => x.asset !== 'USDT').map(x => num(x.amount, 4) + ' ' + x.asset).join(' · ') || ' '} />
        <Kpi l="用户总数 / 24h 新增" v={num(u.total, 0) + ' / ' + num(u.new24h, 0)} d={'KYC ' + num(u.kyc, 0) + ' · 冻结 ' + num(u.frozen, 0)} /><Kpi l="KYC 待审核" v={num(d.kyc && d.kyc.pending, 0)} />
      </div>
      <div className="grid g6" style={{ marginBottom: 12 }}>
        <Kpi l="24h 充值 (USDT)" v={usd(sumAmt(d.deposits24h))} d={(d.deposits24h || []).reduce((a, x) => a + x.count, 0) + ' 笔（全币种）'} />
        <Kpi l="24h 提现 (USDT)" v={usd(sumAmt(d.withdrawals24h))} d={(d.withdrawals24h || []).reduce((a, x) => a + x.count, 0) + ' 笔（全币种）'} />
        <Kpi l="合约未平仓 OI" v={usd(oi.notional)} d={num(oi.positions, 0) + ' 个持仓（按开仓价）'} />
        <Kpi l="24h 强平" v={usd(lq.notional)} d={num(lq.count, 0) + ' 笔 · 穿仓 ' + usd(lq.shortfall)} up={false} />
        <Kpi l="风险准备金" v={d.insurance && d.insurance.balance ? num(d.insurance.balance, 0) + ' ' + d.insurance.asset : '-'} d={d.insurance && d.insurance.ts ? '快照 ' + ft(d.insurance.ts) : '暂无快照'} />
        <Kpi l="提现待处理" v={num((wd.pending_review || 0) + (wd.approved || 0) + (wd.processing || 0), 0)} d={'待初审 ' + (wd.pending_review || 0) + ' · 处理中 ' + (wd.processing || 0)} />
      </div>
      <div className="grid g21" style={{ marginBottom: 12 }}>
        <div className="card"><Tabs value={t} onChange={setT} items={[['vol', '成交额趋势'], ['fee', '合约手续费'], ['user', '用户增长']]} />
          <div style={{ padding: '8px 14px' }}>{daily.length === 0 ? <Empty /> : <>
            <div className="legend">{t === 'vol' ? <><span><i style={{ background: 'var(--lb-primary)' }} />合约</span><span><i style={{ background: 'var(--lb-info)' }} />现货</span></> : t === 'fee' ? <span><i style={{ background: 'var(--lb-gold)' }} />合约手续费（USDT）</span> : <span><i style={{ background: 'var(--lb-primary)' }} />注册</span>}</div>
            {t === 'vol' && <LineChart labels={labels} series={[{ data: daily.map(x => Number(x.futuresVolume)), color: 'var(--lb-primary)' }, { data: daily.map(x => Number(x.spotVolume)), color: 'var(--lb-info)' }]} h={230} />}
            {t === 'fee' && <BarChart labels={labels} data={[daily.map(x => Number(x.futuresFees))]} colors={['var(--lb-gold)']} h={230} />}
            {t === 'user' && <BarChart labels={labels} data={[daily.map(x => Number(x.signups))]} colors={['var(--lb-primary)']} h={230} />}</>}</div></div>
        <div className="card pad"><h3>待办事项<small>实时统计</small></h3>
          {[['KYC 待审核', d.kyc && d.kyc.pending, 'kyc', 'wn'], ['提现待初审', wd.pending_review || 0, 'withdraw', 'wn'], ['提现待复审/执行', wd.approved || 0, 'withdraw', 'bad'], ['提现处理中', wd.processing || 0, 'withdraw', '']]
            .map(([l, n, k, c]) => <div className="todo" key={l}><span className={'tag ' + c}>●</span>{l}<b className="num">{n ?? '-'}</b><a className="btn sm" onClick={() => go(k)}>处理</a></div>)}</div>
      </div>
      <div className="grid g3">
        <div className="card pad"><h3>提现状态分布</h3>{(d.withdrawStatus || []).length === 0 ? <Empty /> : <table className="t" style={{ marginTop: 6 }}><tbody>{d.withdrawStatus.map(x => <tr key={x.status}><td>{WD_ST[x.status] || x.status}</td><td className="num r">{x.count}</td></tr>)}</tbody></table>}</div>
        <div className="card pad"><h3>热门合约<small>24h 成交额</small></h3>{top.length === 0 ? <Empty text="24h 内无合约成交" /> :
          top.map(x => <div key={x.symbol} style={{ marginTop: 10 }}><div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}><span>{x.symbol}</span><span className="num t2">{usd(x.volume)}</span></div><div className="bar"><i style={{ width: Number(x.volume) / topMax * 100 + '%' }} /></div></div>)}</div>
        <div className="card pad"><h3>最新提现<small><a className="pri" onClick={() => go('withdraw')}>全部 ›</a></small></h3>{(d.recentWithdrawals || []).length === 0 ? <Empty /> :
          <table className="t" style={{ marginTop: 6 }}><tbody>{d.recentWithdrawals.map(w => <tr key={w.id}><td className="muted">{w.uid}</td><td className="num r">{num(w.amount, 2)} {w.asset}</td>
            <td><Tag k={w.riskScore >= 70 ? 'bad' : w.riskScore >= 40 ? 'wn' : 'ok'}>风险 {w.riskScore}</Tag></td><td className="muted">{WD_ST[w.status] || w.status}</td></tr>)}</tbody></table>}</div>
      </div>
    </ApiState></>
}
