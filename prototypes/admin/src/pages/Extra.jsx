import { useState } from 'react'
import { ApiState, Confirm, DataTable, Empty, Kpi, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { api, ft, items, num, useApi } from '../api.js'
import { useQ } from './Trade.jsx'

/* 钱包管理：热钱包链上余额、充值地址→热钱包归集、手动热→冷 */
const SW_ST = { pending: '待处理', topup_sent: '补 gas 中', sweep_sent: '归集中', done: '已完成', failed: '失败' }
const CT_ST = { pending_review: '待复核', approved: '已批准待广播', processing: '广播中', completed: '已完成', failed: '失败', rejected: '已驳回', canceled: '已撤销' }
const ctTag = s => s === 'completed' ? 'ok' : ['failed', 'rejected', 'canceled'].includes(s) ? 'bad' : 'in'
const F = ({ l, children }) => <div className="fr"><label>{l}</label>{children}</div>
const META = ['network', 'chain', 'hot', 'cold']
const balEntries = r => Object.entries(r).filter(([k]) => !META.includes(k) && k !== 'id')
const shortAddr = a => !a ? '-' : (a.length > 16 ? a.slice(0, 8) + '…' + a.slice(-6) : a)
const hasPerm = (me, p) => (me && me.perms || []).includes(p)

export function Wallets() {
  const { me } = useApp()
  const canSweep = hasPerm(me, 'wallet:cold_sweep')
  const [t, setT] = useState('bal')
  const hot = useApi('wallet/hot')
  const sw = useApi(t === 'sw' ? 'wallet/sweeps' : null)
  const nets = useApi(t === 'net' ? 'wallet/networks' : null)
  const cts = useApi(t === 'cold' ? 'wallet/cold-transfers' : null)
  const [form, setForm] = useState(null) // { network, chain, hot, cold, asset, balance }
  const [mode, setMode] = useState('amount')
  const [amt, setAmt] = useState('')
  const [reserve, setReserve] = useState('0')
  const [ok, setOk] = useState(null) // 'create' | { decide, row }
  const openForm = (row, asset, balance) => {
    if (!row.cold) return
    setMode('amount'); setAmt(''); setReserve('0')
    setForm({ network: row.network, chain: row.chain, hot: row.hot, cold: row.cold, asset, balance })
  }
  const createBody = () => ({
    asset: form.asset, network: form.network, mode,
    amount: mode === 'amount' ? amt : undefined,
    reserveAmount: mode === 'reserve' ? reserve : undefined,
    idempotencyKey: (crypto.randomUUID && crypto.randomUUID()) || ('k-' + Date.now())
  })
  return <><PageHead title="钱包管理" desc="充值到账后，系统自动把资金从充值地址归集到热钱包。热钱包转到已配置的冷钱包需在此手动发起，并经另一名拥有权限的管理员批准后才会签名广播。不可自定义冷钱包地址。" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['bal', '热钱包余额'], ['cold', '热转冷'], ['sw', '充值归集记录'], ['net', '网络配置']]} /></div>
    {t === 'bal' && <ApiState q={hot}>{items(hot.data).length === 0 ? <div className="card"><Empty text="未配置链上钱包（钱包服务未启用 real-chain 模式）" /></div> :
      <DataTable rows={items(hot.data).map(r => ({ ...r, id: r.network }))} cols={[
        { t: '网络', render: r => <b>{r.network}</b> }, { t: '链', k: 'chain' },
        { t: '热钱包地址', k: 'hot' }, { t: '冷钱包地址', render: r => r.cold || <span className="muted">未配置</span> },
        { t: '余额', render: r => balEntries(r).map(([k, b]) => <div key={k} className="num">{b} {k}</div>) },
        { t: '操作', render: r => canSweep ? balEntries(r).map(([k, b]) => <button key={k} className="btn p" style={{ margin: '2px 4px 2px 0' }} disabled={!r.cold || !(+b > 0)} onClick={e => { e.stopPropagation(); openForm(r, k, b) }}>转冷 · {k}</button>) : <span className="muted">需 wallet:cold_sweep</span> }
      ]} />}</ApiState>}
    {t === 'cold' && <ApiState q={cts}><DataTable rows={items(cts.data)} cols={[
      { t: 'ID', k: 'id' }, { t: '币种', k: 'asset' }, { t: '网络', k: 'network' },
      { t: '金额', r: 1, num: 1, k: 'amount' }, { t: '模式', render: r => r.mode === 'reserve' ? '保留 ' + r.reserveAmount : '指定金额' },
      { t: '冷钱包', render: r => shortAddr(r.destination) },
      { t: '状态', render: r => <Tag k={ctTag(r.status)}>{CT_ST[r.status] || r.status}</Tag> },
      { t: '确认数', r: 1, k: 'confirmations' },
      { t: 'Tx', render: r => r.txHash || '-' },
      { t: '发起', render: r => r.initiatedByName || r.initiatedBy },
      { t: '复核', render: r => r.approvedByName || r.rejectedByName || '-' },
      { t: '原因', render: r => r.reason || '-' },
      { t: '时间', render: r => ft(r.updatedAt || r.createdAt) },
      { t: '操作', render: r => {
        if (!canSweep || r.status !== 'pending_review' || !me) return null
        if (r.initiatedBy === me.id) return <button className="btn d" onClick={e => { e.stopPropagation(); setOk({ decide: 'cancel', row: r }) }}>撤销</button>
        return <><button className="btn p" onClick={e => { e.stopPropagation(); setOk({ decide: 'approve', row: r }) }}>批准</button>
          <button className="btn d" style={{ marginLeft: 6 }} onClick={e => { e.stopPropagation(); setOk({ decide: 'reject', row: r }) }}>驳回</button></>
      } }
    ]} /></ApiState>}
    {t === 'sw' && <ApiState q={sw}><DataTable rows={items(sw.data)} cols={[{ t: 'ID', k: 'id' }, { t: '网络', k: 'network' }, { t: '币种', k: 'asset' }, { t: '充值地址', k: 'address' }, { t: '金额', r: 1, num: 1, k: 'amount' },
      { t: '阶段', render: r => <Tag k={r.stage === 'done' ? 'ok' : r.stage === 'failed' ? 'bad' : 'in'}>{SW_ST[r.stage] || r.stage}</Tag> }, { t: '归集交易', render: r => r.sweepTx || '-' }, { t: '更新', render: r => ft(r.updatedAt) }]} /></ApiState>}
    {t === 'net' && <ApiState q={nets}><DataTable rows={items(nets.data).map((r, i) => ({ ...r, id: (r.asset || '') + (r.network || '') + i }))}
      cols={[{ t: '币种', k: 'asset' }, { t: '网络', k: 'network' }, { t: '充值', render: r => <Tag k={r.depositEnabled ? 'ok' : 'bad'}>{r.depositEnabled ? '开启' : '关闭'}</Tag> }, { t: '提现', render: r => <Tag k={r.withdrawEnabled ? 'ok' : 'bad'}>{r.withdrawEnabled ? '开启' : '关闭'}</Tag> },
        { t: '提现手续费', r: 1, render: r => r.withdrawFee ?? '-' }, { t: '最小提现', r: 1, render: r => r.minWithdraw ?? '-' }, { t: '确认数', r: 1, render: r => r.confirmations ?? '-' }]} /></ApiState>}
    {form && !ok && <Confirm title={`热转冷 · ${form.asset} / ${form.network}`} okText="提交待复核" onClose={() => setForm(null)}
      rows={[['热钱包', form.hot], ['冷钱包（已配置，不可改）', form.cold], ['当前余额', String(form.balance) + ' ' + form.asset]]}
      onOk={async ({ totpCode, reason }) => { await api('wallet/cold-transfers', { method: 'POST', body: { ...createBody(), totpCode, reason } }); setForm(null); if (cts && cts.reload) cts.reload(); if (t !== 'cold') setT('cold'); return '已提交，等待另一名管理员批准后才会广播' }}>
      <F l="方式"><select className="sel" value={mode} onChange={e => setMode(e.target.value)}><option value="amount">指定金额</option><option value="reserve">保留部分在热钱包</option></select></F>
      {mode === 'amount'
        ? <F l="转出金额"><input className="in" value={amt} onChange={e => setAmt(e.target.value)} placeholder={'不超过 ' + form.balance} /></F>
        : <F l="热钱包保留"><input className="in" value={reserve} onChange={e => setReserve(e.target.value)} /></F>}
    </Confirm>}
    {ok && ok.decide === 'approve' && <Confirm title={`批准热转冷 #${ok.row.id}`} okText="批准并排队广播" onClose={() => setOk(null)}
      rows={[['币种/网络', ok.row.asset + ' / ' + ok.row.network], ['金额', String(ok.row.amount)], ['冷钱包', ok.row.destination], ['发起人', ok.row.initiatedByName || String(ok.row.initiatedBy)], ['原因', ok.row.reason]]}
      onOk={async ({ totpCode, reason }) => { await api('wallet/cold-transfers/' + ok.row.id + '/approve', { method: 'POST', body: { totpCode, reason } }); cts.reload(); return '已批准，worker 将签名广播' }} />}
    {ok && ok.decide === 'cancel' && <Confirm danger title={`撤销热转冷 #${ok.row.id}`} okText="撤销" onClose={() => setOk(null)}
      onOk={async ({ totpCode, reason }) => { await api('wallet/cold-transfers/' + ok.row.id + '/cancel', { method: 'POST', body: { totpCode, reason } }); cts.reload(); return '已撤销' }} />}
    {ok && ok.decide === 'reject' && <Confirm danger title={`驳回热转冷 #${ok.row.id}`} okText="驳回" onClose={() => setOk(null)}
      rows={[['币种/网络', ok.row.asset + ' / ' + ok.row.network], ['金额', String(ok.row.amount)], ['发起人', ok.row.initiatedByName || String(ok.row.initiatedBy)]]}
      onOk={async ({ totpCode, reason }) => { await api('wallet/cold-transfers/' + ok.row.id + '/reject', { method: 'POST', body: { totpCode, reason } }); cts.reload(); return '已驳回' }} />}
  </>
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
