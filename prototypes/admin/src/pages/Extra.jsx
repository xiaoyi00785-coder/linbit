import { useState } from 'react'
import { ApiState, DataTable, Empty, Kpi, PageHead, Tabs, Tag } from '../ui.jsx'
import { ft, items, num, useApi } from '../api.js'
import { useQ } from './Trade.jsx'

/* 钱包管理：热钱包链上余额 + 归集记录（钱包服务 real-chain 模式提供） */
const SW_ST = { pending: '待处理', topup_sent: '补 gas 中', sweep_sent: '归集中', done: '已完成', failed: '失败' }
export function Wallets() {
  const [t, setT] = useState('bal'); const hot = useApi('wallet/hot'); const sw = useApi(t === 'sw' ? 'wallet/sweeps' : null); const nets = useApi('wallet/networks')
  const META = ['network', 'chain', 'hot', 'cold']
  return <><PageHead title="钱包管理" desc="热钱包地址与链上实时余额、冷钱包地址、归集记录；冷钱包多签操作在链下完成，不在后台发起" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['bal', '热钱包余额'], ['sw', '归集记录'], ['net', '网络配置']]} /></div>
    {t === 'bal' && <ApiState q={hot}>{items(hot.data).length === 0 ? <div className="card"><Empty text="未配置链上钱包（钱包服务未启用 real-chain 模式）" /></div> :
      <DataTable rows={items(hot.data).map(r => ({ ...r, id: r.network }))} cols={[{ t: '网络', render: r => <b>{r.network}</b> }, { t: '链', k: 'chain' }, { t: '热钱包地址', k: 'hot' }, { t: '冷钱包地址', render: r => r.cold || '-' },
        { t: '余额', render: r => Object.entries(r).filter(([k]) => !META.includes(k) && k !== 'id').map(([k, b]) => <div key={k} className="num">{b} {k}</div>) }]} />}</ApiState>}
    {t === 'sw' && <ApiState q={sw}><DataTable rows={items(sw.data)} cols={[{ t: 'ID', k: 'id' }, { t: '网络', k: 'network' }, { t: '币种', k: 'asset' }, { t: '充值地址', k: 'address' }, { t: '金额', r: 1, num: 1, k: 'amount' },
      { t: '阶段', render: r => <Tag k={r.stage === 'done' ? 'ok' : r.stage === 'failed' ? 'bad' : 'in'}>{SW_ST[r.stage] || r.stage}</Tag> }, { t: '归集交易', render: r => r.sweepTx || '-' }, { t: '更新', render: r => ft(r.updatedAt) }]} /></ApiState>}
    {t === 'net' && <ApiState q={nets}><DataTable rows={items(nets.data).map((r, i) => ({ ...r, id: (r.asset || '') + (r.network || '') + i }))}
      cols={[{ t: '币种', k: 'asset' }, { t: '网络', k: 'network' }, { t: '充值', render: r => <Tag k={r.depositEnabled ? 'ok' : 'bad'}>{r.depositEnabled ? '开启' : '关闭'}</Tag> }, { t: '提现', render: r => <Tag k={r.withdrawEnabled ? 'ok' : 'bad'}>{r.withdrawEnabled ? '开启' : '关闭'}</Tag> },
        { t: '提现手续费', r: 1, render: r => r.withdrawFee ?? '-' }, { t: '最小提现', r: 1, render: r => r.minWithdraw ?? '-' }, { t: '确认数', r: 1, render: r => r.confirmations ?? '-' }]} /></ApiState>}</>
}

/* 强平与风险：强平记录 + 风险保障基金快照 */
export function Liquidation() {
  const [f, v] = useQ(); const q = useApi('futures/liquidations', { ...f, size: 200 }); const rows = items(q.data); const ins = (q.data && q.data.insurance) || []
  return <><PageHead title="强平与风险" desc="强平记录（按标记价触发）与风险保障基金余额快照" />
    <ApiState q={q}><div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="风险保障基金" v={ins[0] ? num(ins[0].balance) + ' ' + ins[0].asset : '-'} d={ins[0] ? '快照 ' + ft(ins[0].ts) : '暂无快照'} />
      <Kpi l="强平记录总数" v={q.data ? q.data.total : '-'} /><Kpi l="当前页强平价值" v={'$' + num(rows.reduce((a, r) => a + Math.abs(+r.qty) * +r.markPrice, 0), 0)} /><Kpi l="当前页穿仓合计" v={num(rows.reduce((a, r) => a + +r.shortfall, 0))} up={false} /></div></ApiState>
    <div className="card"><div className="filters"><input className="in" placeholder="UID" onChange={v('uid')} /><input className="in" placeholder="合约，如 BTCUSDT" onChange={v('symbol')} /></div></div>
    <ApiState q={q}><DataTable rows={rows} cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '合约', k: 'symbol' }, { t: '模式', render: r => r.marginMode === 'isolated' ? '逐仓' : '全仓' }, { t: '数量', r: 1, num: 1, k: 'qty' },
      { t: '标记价', r: 1, num: 1, k: 'markPrice' }, { t: '强平费', r: 1, num: 1, render: r => num(r.fee, 4) }, { t: '穿仓', r: 1, num: 1, render: r => <span className={+r.shortfall > 0 ? 'down' : ''}>{num(r.shortfall, 4)}</span> }, { t: '时间', render: r => ft(r.createdAt) }]} /></ApiState>
    {ins.length > 1 && <div className="card pad" style={{ marginTop: 12 }}><h3>风险保障基金快照</h3><table className="t"><tbody>{ins.map(x => <tr key={x.ts}><td>{ft(x.ts)}</td><td className="num r">{num(x.balance)} {x.asset}</td><td className="num r muted">权益 {num(x.equity)}</td></tr>)}</tbody></table></div>}</>
}

export { Partners, Coupons, Notify } from './Live.jsx'
