import { useState } from 'react'
import { DataTable, Kpi, Modal, PageHead, Tag, eq, has, useApp, useFilter } from '../ui.jsx'
import { fmt, grids, positions, spotOrders } from '../mock.js'
export function SpotOrders() {
  const [rows, set] = useFilter(spotOrders, (r, f) => (has(r.uid, f.k) || has(r.id, f.k)) && eq(r.status, f.s) && eq(r.side, f.d))
  return <><PageHead title="现货订单" desc="仅现货（无杠杆/借币）· 支持强制撤单（需原因并记审计）" />
    <DataTable rows={rows} select filters={<><input className="in" placeholder="UID / 订单号" onChange={set('k')} /><input className="in" placeholder="交易对，如 BTC/USDT" />
      <select className="sel" onChange={set('d')}><option>全部</option><option>买入</option><option>卖出</option></select><select className="sel" onChange={set('s')}><option>全部</option><option>挂单中</option><option>部分成交</option><option>已成交</option><option>已撤销</option></select></>}
      toolbar={sel => <button className="btn d" disabled={!sel.length}>强制撤单</button>}
      cols={[{ t: '订单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'sym' }, { t: '方向', render: r => <span className={r.side === '买入' ? 'up' : 'down'}>{r.side}</span> }, { t: '类型', k: 'type' },
        { t: '价格', r: 1, num: 1, render: r => fmt(r.px, r.px < 10 ? 4 : 2) }, { t: '数量', r: 1, num: 1, render: r => r.qty }, { t: '成交率', r: 1, render: r => r.filled + '%' },
        { t: '状态', render: r => <Tag k={r.status === '已成交' ? 'ok' : r.status === '已撤销' ? '' : 'in'}>{r.status}</Tag> }, { t: '时间', k: 'time' }]} /></>
}
export function Positions() {
  const { say } = useApp(); const [m, setM] = useState(null)
  const [rows, set] = useFilter(positions, (r, f) => has(r.uid, f.k) && has(r.sym, f.s) && eq(r.side, f.d) && (!f.r || f.r === '全部' || r.mr > 80))
  return <><PageHead title="合约持仓" desc="USDT 本位永续 · 保证金率 ≥ 80% 预警 · 100% 触发强平"><button className="btn">强平记录</button><button className="btn">ADL 队列</button></PageHead>
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="总持仓价值" v="$684.5M" /><Kpi l="多 / 空" v="$361.2M / $323.3M" /><Kpi l="高风险仓位（MR≥80%）" v={positions.filter(p => p.mr > 80).length} up={false} d="需关注" /><Kpi l="风险准备金" v="$41.2M" d="▲ $0.3M 今日" up /></div>
    <DataTable rows={rows} filters={<><input className="in" placeholder="UID" onChange={set('k')} /><input className="in" placeholder="合约，如 BTCUSDT" onChange={set('s')} />
      <select className="sel" onChange={set('d')}><option>全部</option><option>多</option><option>空</option></select><select className="sel" onChange={set('r')}><option>全部</option><option>仅高风险</option></select></>}
      cols={[{ t: '仓位 ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '合约', k: 'sym' }, { t: '方向', render: r => <span className={r.side === '多' ? 'up' : 'down'}>{r.side}</span> }, { t: '杠杆', render: r => r.lev + 'x' }, { t: '模式', k: 'mode' },
        { t: '仓位价值', r: 1, num: 1, render: r => fmt(r.size, 0) }, { t: '开仓价', r: 1, num: 1, render: r => fmt(r.entry, r.entry < 10 ? 4 : 2) }, { t: '标记价', r: 1, num: 1, render: r => fmt(r.mark, r.mark < 10 ? 4 : 2) },
        { t: '强平价', r: 1, num: 1, render: r => fmt(r.liq, r.liq < 10 ? 4 : 2) }, { t: '未实现盈亏', r: 1, num: 1, render: r => <span className={r.upnl > 0 ? 'up' : 'down'}>{fmt(r.upnl)}</span> },
        { t: '保证金率', r: 1, render: r => <span className={r.mr > 80 ? 'down' : r.mr > 50 ? 'warn' : ''}>{r.mr}%</span> }, { t: '操作', render: r => <a className="btn g" onClick={() => setM(r)}>强制减仓</a> }]} />
    {m && <Modal title={`强制减仓 · ${m.sym} · UID ${m.uid}`} onClose={() => setM(null)} footer={<><button className="btn" onClick={() => setM(null)}>取消</button><button className="btn d" onClick={() => { say('已提交，待风控主管确认'); setM(null) }}>提交</button></>}>
      <div className="banner bad">高危操作，仅限风控主管；需双人确认，执行后通知用户。</div><div className="fr"><label>减仓比例</label><select className="sel"><option>25%</option><option>50%</option><option>100%</option></select></div><div className="fr"><label>原因</label><input className="in" placeholder="如：异常价格操纵" /></div></Modal>}</>
}
export function Grid() {
  return <><PageHead title="网格策略" desc="仅现货网格：2 ~ 100 格 · 等差/等比 · 支持止盈止损" />
    <DataTable rows={grids} filters={<><input className="in" placeholder="UID" /><select className="sel"><option>全部状态</option><option>运行中</option><option>已停止</option></select></>}
      cols={[{ t: '策略 ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'sym' }, { t: '模式', k: 'mode' }, { t: '网格数', k: 'n' }, { t: '投入 USDT', r: 1, num: 1, render: r => fmt(r.inv, 0) },
        { t: '收益', r: 1, num: 1, render: r => <span className={r.pnl > 0 ? 'up' : 'down'}>{fmt(r.pnl)}</span> }, { t: '运行时长', k: 'runtime' }, { t: '状态', render: r => <Tag k={r.status === '运行中' ? 'ok' : ''}>{r.status}</Tag> }, { t: '操作', render: () => <a className="btn g">强制停止</a> }]} /></>
}
