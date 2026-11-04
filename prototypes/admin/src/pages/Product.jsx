/* 币种（链/网络参数）与合约参数，数据来自 admin/v1 */
import { useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, PageHead, Sw, Tag } from '../ui.jsx'
import { api, items, num, useApi } from '../api.js'
import { ConfigCard } from './Common.jsx'

export function Coins() {
  const q = useApi('wallet/networks'); const [cur, setCur] = useState(null); const [d, setD] = useState({}); const [ok, setOk] = useState(false)
  const rows = items(q.data).map(r => ({ ...r, id: r.asset + '/' + r.network }))
  const open = r => { setCur(r); setD({ depositEnabled: r.depositEnabled, withdrawEnabled: r.withdrawEnabled, minDeposit: r.minDeposit, minWithdraw: r.minWithdraw, maxWithdraw: r.maxWithdraw || '', withdrawFee: r.withdrawFee, confirmations: r.confirmations }) }
  const changed = cur ? Object.keys(d).filter(k => String(d[k] ?? '') !== String(cur[k] ?? '')) : []
  return <><PageHead title="币种与网络" desc="每个币种/链的充提开关、最小额、手续费、确认数（钱包服务实时配置）" />
    <ApiState q={q}><DataTable rows={rows} onRow={open} cols={[{ t: '币种', k: 'asset', render: r => <b>{r.asset}</b> }, { t: '网络', k: 'network' },
      { t: '充值', render: r => <Tag k={r.depositEnabled ? 'ok' : 'bad'}>{r.depositEnabled ? '开' : '关'}</Tag> }, { t: '提现', render: r => <Tag k={r.withdrawEnabled ? 'ok' : 'bad'}>{r.withdrawEnabled ? '开' : '关'}</Tag> },
      { t: '最小充值', r: 1, num: 1, k: 'minDeposit' }, { t: '最小提现', r: 1, num: 1, k: 'minWithdraw' }, { t: '单笔上限', r: 1, num: 1, render: r => r.maxWithdraw || '-' },
      { t: '提现手续费', r: 1, num: 1, render: r => r.withdrawFee + ' ' + (r.withdrawFeeAsset || '') }, { t: '确认数', k: 'confirmations' }, { t: 'Memo', render: r => r.memoRequired ? '需要' : '-' }]} /></ApiState>
    <ConfigCard path="asset-settings" title="资产服务设置" />
    {cur && <Drawer title={`${cur.asset} · ${cur.network}`} onClose={() => setCur(null)} footer={<button className="btn p" disabled={!changed.length} onClick={() => setOk(true)}>保存</button>}>
      {['depositEnabled', 'withdrawEnabled'].map(k => <div className="fr" key={k}><label>{k === 'depositEnabled' ? '充值开关' : '提现开关'}</label><Sw on={d[k]} onClick={() => setD({ ...d, [k]: !d[k] })} /></div>)}
      {[['minDeposit', '最小充值'], ['minWithdraw', '最小提现'], ['maxWithdraw', '单笔上限（空=不限）'], ['withdrawFee', '提现手续费'], ['confirmations', '确认数']].map(([k, l]) => <div className="fr" key={k}><label>{l}</label>
        <input className="in" value={d[k] ?? ''} onChange={e => setD({ ...d, [k]: k === 'confirmations' ? +e.target.value || 0 : e.target.value })} /></div>)}</Drawer>}
    {ok && <Confirm danger title={`修改 ${cur.asset}/${cur.network}`} rows={changed.map(k => [k, `${cur[k] ?? '-'} → ${d[k]}`])} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { const body = Object.fromEntries(changed.map(k => [k, d[k]])); await api(`wallet/networks/${encodeURIComponent(cur.asset)}/${encodeURIComponent(cur.network)}`, { method: 'PUT', body: { ...body, totpCode, reason } }); setCur(null); q.reload(); return '已保存' }} />}</>
}

const C_ST = { trading: '交易中', reduce_only: '仅减仓', halted: '暂停', delisted: '已下架' }
export function Futures() {
  const q = useApi('futures/contracts')
  return <><PageHead title="合约参数" desc="永续合约列表、杠杆、维持保证金率、费率与未平仓量（只读）；下单参数可在下方调整" />
    <ApiState q={q}><DataTable rowKey="symbol" rows={items(q.data)} cols={[{ t: '合约', k: 'symbol', render: r => <b>{r.symbol}</b> }, { t: '最大杠杆', render: r => r.maxLeverage + 'x' },
      { t: 'MMR', r: 1, num: 1, k: 'mmr' }, { t: 'Tick', r: 1, num: 1, k: 'tickSize' }, { t: '最小/最大数量', r: 1, render: r => r.minQty + ' / ' + r.maxQty },
      { t: 'Maker/Taker', r: 1, render: r => r.makerFee + ' / ' + r.takerFee }, { t: '风险档位', k: 'tiers' }, { t: '未平仓名义', r: 1, num: 1, render: r => num(r.openInterest) },
      { t: '状态', render: r => <Tag k={r.status === 'trading' ? 'ok' : 'wn'}>{C_ST[r.status] || r.status}</Tag> }]} /></ApiState>
    <ConfigCard path="futures/order-config" title="合约下单参数" /></>
}

export { Pairs, Fees } from './Live.jsx'
