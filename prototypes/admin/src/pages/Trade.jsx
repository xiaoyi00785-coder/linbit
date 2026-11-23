/* 交易监控：现货订单 / 合约持仓 / 网格策略（只读，数据来自 admin/v1） */
import { useEffect, useRef, useState } from 'react'
import { Filters, useFilters } from './P1.jsx'
import { ApiState, Confirm, DataTable, Kpi, PageHead, Tag, useApp } from '../ui.jsx'
import { api, ft, items, num, useApi } from '../api.js'

const O_ST = { new: '挂单中', partially_filled: '部分成交', filled: '已成交', canceled: '已撤销', rejected: '已拒绝', expired: '已过期' }
const G_ST = { running: '运行中', stopped: '已停止', stopping: '停止中', failed: '失败' }
/** 筛选状态：下拉框立即生效；输入框停止输入 600ms 后才请求（不再每个按键都查库） */
export const useQ = () => {
  const { q } = useApp(); const [f, setF] = useState(() => ({ ...(q || {}) })); const draft = useRef({ ...(q || {}) }); const timer = useRef(null)
  useEffect(() => () => clearTimeout(timer.current), [])
  const commit = () => setF(Object.fromEntries(Object.entries(draft.current).filter(([, x]) => x !== '')))
  const v = k => e => { draft.current = { ...draft.current, [k]: e.target.value.trim() }; clearTimeout(timer.current)
    if (e.target.tagName === 'SELECT') commit(); else timer.current = setTimeout(commit, 600) }
  return [f, v]
}
const total = d => d && d.total > (d.items || []).length ? <div className="muted" style={{ margin: '0 0 8px' }}>共 {d.total} 条，显示最新 {d.items.length} 条，请用筛选缩小范围</div> : null

export function SpotOrders() {
  const f = useFilters({ status: 'open' }); const [stack, setStack] = useState([]); const page = stack.length
  const key = JSON.stringify(f.applied); const [k0, setK0] = useState(key); if (k0 !== key) { setK0(key); setStack([]) }
  const needUid = f.applied.status !== 'open' && !f.applied.uid && !f.applied.orderId
  const q = useApi(needUid ? null : 'spot/orders', { ...f.applied, size: 50, page: page + 1 })
  const pager = <><span>第 {page + 1} 页 · 每页 50 条（大表不统计总数）</span><span className="sp" /><b style={{ opacity: page ? 1 : .4 }} onClick={() => page && setStack(stack.slice(0, -1))}>‹ 上一页</b>
    <b style={{ opacity: q.data && q.data.hasMore ? 1 : .4 }} onClick={() => q.data && q.data.hasMore && setStack([...stack, 1])}>下一页 ›</b></>
  const table = <DataTable rows={needUid ? [] : items(q.data)} rowKey="orderId" pager={pager} emptyText={needUid ? '查询历史订单请先输入 UID 或订单号' : undefined} filters={<Filters f={f} fields={[['uid', 'UID'], ['orderId', '订单号'], ['symbol', '交易对，如 BTCUSDT'], ['side', '', [['', '全部方向'], ['buy', '买入'], ['sell', '卖出']]],
      ['status', '', [['open', '挂单中/部分成交'], ['', '全部状态（需 UID）'], ['filled', '已成交（需 UID）'], ['canceled', '已撤销（需 UID）']]]]} />}
    cols={[{ t: '订单号', k: 'orderId' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' }, { t: '方向', render: r => <span className={r.side === 'buy' ? 'up' : 'down'}>{r.side === 'buy' ? '买入' : '卖出'}</span> }, { t: '类型', k: 'type' },
      { t: '价格', r: 1, num: 1, render: r => r.price }, { t: '数量', r: 1, num: 1, render: r => r.quantity }, { t: '已成交', r: 1, num: 1, render: r => r.filled },
      { t: '状态', render: r => <Tag k={r.status === 'filled' ? 'ok' : r.status === 'canceled' ? '' : 'in'}>{O_ST[r.status] || r.status}</Tag> }, { t: '时间', render: r => ft(r.createdAt) }]} />
  return <><PageHead title="现货订单" desc="全站现货订单查询（只读）。历史订单量大，查询已成交/已撤销需指定 UID 或订单号。强制撤单暂无后台接口" />
    {needUid ? table : <ApiState q={q}>{table}</ApiState>}</>
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
  const [f, v] = useQ(); const q = useApi('grids', { ...f, size: 200 }); const [stop, setStop] = useState(null); const [sell, setSell] = useState(false)
  const app = useApp(); const canStop = ((app && app.me && app.me.perms) || []).includes('strategy:force_stop')
  return <><PageHead title="网格策略" desc="用户现货网格策略。强制停止：撤销该网格全部挂单并冻结终值，默认不卖出底仓；需 strategy:force_stop 权限 + Google 验证码。" />
    {stop && <Confirm danger title={`强制停止网格 #${stop.id}`} rows={[['UID', stop.uid], ['交易对', stop.symbol], ['投入', num(stop.investment)]]} onClose={() => setStop(null)}
      note="停止后资金退回用户现货账户；勾选卖出会按市价卖出网格持有的基础币。"
      onOk={async ({ totpCode, reason }) => { await api(`grids/${stop.id}/force-stop`, { method: 'POST', body: { totpCode, reason, sellBase: sell } }); q.reload(); return '已强制停止' }}>
      <div className="fr"><label>卖出底仓</label><input type="checkbox" checked={sell} onChange={e => setSell(e.target.checked)} /></div></Confirm>}
    <div className="card"><div className="filters"><input className="in" placeholder="UID" onChange={v('uid')} /><input className="in" placeholder="交易对" onChange={v('symbol')} />
      <select className="sel" onChange={v('status')}><option value="">全部状态</option><option value="running">运行中</option><option value="stopped">已停止</option></select></div></div>
    <ApiState q={q}>{total(q.data)}<DataTable rows={items(q.data)}
      cols={[{ t: '策略 ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' }, { t: '模式', render: r => r.mode === 'geometric' ? '等比' : r.mode === 'arithmetic' ? '等差' : r.mode }, { t: '区间', render: r => r.lowerPrice + ' ~ ' + r.upperPrice }, { t: '网格数', k: 'grids' },
        { t: '投入', r: 1, num: 1, render: r => num(r.investment) }, { t: '网格收益', r: 1, num: 1, render: r => <span className={+r.gridProfit >= 0 ? 'up' : 'down'}>{num(r.gridProfit, 4)}</span> },
        { t: '状态', render: r => <Tag k={r.status === 'running' ? 'ok' : ''}>{G_ST[r.status] || r.status}</Tag> }, { t: '创建', render: r => ft(r.createdAt) }, { t: '停止', render: r => ft(r.stoppedAt) },
        { t: '', render: r => canStop && r.status === 'running' ? <a onClick={() => { setSell(false); setStop(r) }}>强制停止</a> : null }]} /></ApiState></>
}
