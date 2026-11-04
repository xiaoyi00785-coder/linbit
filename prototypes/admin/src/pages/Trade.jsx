/* 交易监控：现货订单 / 合约持仓 / 网格策略（只读，数据来自 admin/v1） */
import { useState } from 'react'
import { ApiState, DataTable, Kpi, PageHead, Tag } from '../ui.jsx'
import { ft, items, num, useApi } from '../api.js'

const O_ST = { new: '挂单中', partially_filled: '部分成交', filled: '已成交', canceled: '已撤销', rejected: '已拒绝', expired: '已过期' }
const G_ST = { running: '运行中', stopped: '已停止', stopping: '停止中', failed: '失败' }
export const useQ = () => { const [f, setF] = useState({}); const v = k => e => setF({ ...f, [k]: e.target.value.trim() }); return [f, v] }
const total = d => d && d.total > (d.items || []).length ? <div className="muted" style={{ margin: '0 0 8px' }}>共 {d.total} 条，显示最新 {d.items.length} 条，请用筛选缩小范围</div> : null

export function SpotOrders() {
  const [f, v] = useQ(); const q = useApi('spot/orders', { ...f, size: 200 })
  return <><PageHead title="现货订单" desc="全站现货订单查询（只读）。强制撤单暂无后台接口，请通知用户自行撤单或由运维处理" />
    <div className="card"><div className="filters"><input className="in" placeholder="UID" onChange={v('uid')} /><input className="in" placeholder="交易对，如 BTCUSDT" onChange={v('symbol')} />
      <select className="sel" onChange={v('side')}><option value="">全部方向</option><option value="buy">买入</option><option value="sell">卖出</option></select>
      <select className="sel" onChange={v('status')}><option value="">全部状态</option><option value="open">挂单中/部分成交</option><option value="filled">已成交</option><option value="canceled">已撤销</option></select></div></div>
    <ApiState q={q}>{total(q.data)}<DataTable rows={items(q.data)} rowKey="orderId"
      cols={[{ t: '订单号', k: 'orderId' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' }, { t: '方向', render: r => <span className={r.side === 'buy' ? 'up' : 'down'}>{r.side === 'buy' ? '买入' : '卖出'}</span> }, { t: '类型', k: 'type' },
        { t: '价格', r: 1, num: 1, render: r => r.price }, { t: '数量', r: 1, num: 1, render: r => r.quantity }, { t: '已成交', r: 1, num: 1, render: r => r.filled },
        { t: '状态', render: r => <Tag k={r.status === 'filled' ? 'ok' : r.status === 'canceled' ? '' : 'in'}>{O_ST[r.status] || r.status}</Tag> }, { t: '时间', render: r => ft(r.createdAt) }]} /></ApiState></>
}

export function Positions() {
  const [f, v] = useQ(); const q = useApi('futures/positions', { ...f, size: 200 }); const rows = items(q.data)
  const long = rows.filter(r => +r.qty > 0).reduce((a, r) => a + +r.notional, 0), short = rows.filter(r => +r.qty < 0).reduce((a, r) => a + +r.notional, 0)
  return <><PageHead title="合约持仓" desc="USDT 本位永续当前持仓（只读）· 标记价取最新标记价快照；强制减仓暂无后台接口" />
    <ApiState q={q}><div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="持仓数" v={q.data ? q.data.total : '-'} /><Kpi l="多头价值（当前页）" v={'$' + num(long, 0)} /><Kpi l="空头价值（当前页）" v={'$' + num(short, 0)} /><Kpi l="未实现盈亏（当前页）" v={num(rows.reduce((a, r) => a + +r.unrealizedPnl, 0))} /></div></ApiState>
    <div className="card"><div className="filters"><input className="in" placeholder="UID" onChange={v('uid')} /><input className="in" placeholder="合约，如 BTCUSDT" onChange={v('symbol')} />
      <select className="sel" onChange={v('side')}><option value="">全部方向</option><option value="long">多</option><option value="short">空</option></select></div></div>
    <ApiState q={q}>{total(q.data)}<DataTable rows={rows.map((r, i) => ({ ...r, id: r.uid + r.symbol + r.positionSide + i }))}
      cols={[{ t: 'UID', k: 'uid' }, { t: '合约', k: 'symbol' }, { t: '方向', render: r => <span className={+r.qty > 0 ? 'up' : 'down'}>{+r.qty > 0 ? '多' : '空'}</span> }, { t: '杠杆', render: r => r.leverage + 'x' }, { t: '模式', render: r => r.marginMode === 'isolated' ? '逐仓' : '全仓' },
        { t: '数量', r: 1, num: 1, render: r => r.qty }, { t: '仓位价值', r: 1, num: 1, render: r => num(r.notional) }, { t: '开仓价', r: 1, num: 1, render: r => r.entryPrice }, { t: '标记价', r: 1, num: 1, render: r => r.markPrice || '-' },
        { t: '未实现盈亏', r: 1, num: 1, render: r => <span className={+r.unrealizedPnl >= 0 ? 'up' : 'down'}>{num(r.unrealizedPnl)}</span> }, { t: '已实现盈亏', r: 1, num: 1, render: r => num(r.realizedPnl) }]} /></ApiState></>
}

export function Grid() {
  const [f, v] = useQ(); const q = useApi('grids', { ...f, size: 200 })
  return <><PageHead title="网格策略" desc="用户现货网格策略（只读）；强制停止暂无后台接口" />
    <div className="card"><div className="filters"><input className="in" placeholder="UID" onChange={v('uid')} /><input className="in" placeholder="交易对" onChange={v('symbol')} />
      <select className="sel" onChange={v('status')}><option value="">全部状态</option><option value="running">运行中</option><option value="stopped">已停止</option></select></div></div>
    <ApiState q={q}>{total(q.data)}<DataTable rows={items(q.data)}
      cols={[{ t: '策略 ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' }, { t: '模式', render: r => r.mode === 'geometric' ? '等比' : r.mode === 'arithmetic' ? '等差' : r.mode }, { t: '区间', render: r => r.lowerPrice + ' ~ ' + r.upperPrice }, { t: '网格数', k: 'grids' },
        { t: '投入', r: 1, num: 1, render: r => num(r.investment) }, { t: '网格收益', r: 1, num: 1, render: r => <span className={+r.gridProfit >= 0 ? 'up' : 'down'}>{num(r.gridProfit, 4)}</span> },
        { t: '状态', render: r => <Tag k={r.status === 'running' ? 'ok' : ''}>{G_ST[r.status] || r.status}</Tag> }, { t: '创建', render: r => ft(r.createdAt) }, { t: '停止', render: r => ft(r.stoppedAt) }]} /></ApiState></>
}
