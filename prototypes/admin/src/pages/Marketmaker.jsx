/* 做市管理：状态 / 启停、交易对参数、库存基线、账户充值、合约划转。数据来自 admin/v1/mm/*（marketmaker 服务） */
import { useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Kpi, PageHead, Sw, Tag } from '../ui.jsx'
import { api, ft, items, useApi } from '../api.js'

const ST = { quoting: ['报价中', 'ok'], paused: ['暂停', 'warn'], disabled: ['未启用', ''], stopped: ['已停止', ''], error: ['异常', 'bad'] }
// 交易对可调参数：[字段, 名称, 是否整数, 仅合约]
const FIELDS = [
  ['spreadBps', '价差 (bps, 0.5–1000)'], ['levels', '档位数 (1–200)', true], ['levelStepBps', '档位间距 (bps, 0–500)'],
  ['levelNotional', '每档名义金额 (USDT)'], ['sizeGrowth', '档位数量递增 (1–3)'], ['refreshMs', '刷新间隔 (ms, 500–60000)', true],
  ['requoteBps', '重新报价阈值 (bps, 0–100)'], ['targetBaseRatio', '目标基础币占比 (0–1)'], ['maxPosition', '最大净持仓'],
  ['maxSkewBps', '最大偏移 (bps, 0–500)'], ['leverage', '杠杆 (1–125)', true, true],
  ['depthTargetQty', '大单深度目标 (每侧基础币数量, 0=关闭)', false, true], ['depthBandBps', '深度目标范围 (距中间价 bps, 1–500)', false, true],
]

export default function Marketmaker() {
  const st = useApi('mm/status'); const pq = useApi('mm/pairs'); const bq = useApi('mm/baselines')
  const [run, setRun] = useState(null) // 'start' | 'stop'
  const s = st.data || {}
  const reload = () => { st.reload(); pq.reload(); bq.reload() }
  return <><PageHead title="做市管理" desc={`做市账户 ${s.account || 'mm:main'} · 网络 ${s.network || '-'} · 报价跟 Binance/OKX 盘口 · 所有写操作需 Google 验证码并写入审计`}>
      {s.running ? <button className="btn d" onClick={() => setRun('stop')}>停止做市</button>
        : <button className="btn p" disabled={!s.enabled} title={s.enabled ? '' : '服务器 MM_ENABLED=false'} onClick={() => setRun('start')}>启动做市</button>}
      <button className="btn" onClick={reload}>刷新</button></PageHead>
    <ApiState q={st}>
      {!s.enabled && <div className="banner bad">做市服务当前被服务器配置关闭（MM_ENABLED=false），无法启动。需在服务器 .env 设置 MM_ENABLED=true 并重启 marketmaker 后才能启动；参数与基线仍可提前配置。</div>}
      <div className="kpis" style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 12 }}>
        <Kpi l="服务开关" v={s.enabled ? '已开启' : '已关闭'} d="MM_ENABLED" />
        <Kpi l="运行状态" v={s.running ? '运行中' : '已停止'} />
        <Kpi l="报价模式" v={s.quoteMode === 'follow-book' ? '跟盘' : (s.quoteMode || '跟价梯子')} d={s.bookFeed || ''} />
        <Kpi l="行情中断交易对" v={s.feedDownPairs ?? 0} />
        <Kpi l="上次循环" v={ft(s.lastCycle)} />
      </div>
      <Pairs q={pq} states={s.pairs || []} onSaved={reload} />
      <Baselines q={bq} onSaved={reload} />
      <Credit status={s} onSaved={reload} />
      <Transfer onSaved={reload} />
    </ApiState>
    {run && <Confirm danger={run === 'stop'} title={run === 'start' ? '启动做市' : '停止做市（将撤销全部做市挂单）'} onClose={() => setRun(null)}
      onOk={async ({ totpCode, reason }) => { await api('mm/' + run, { method: 'POST', body: { totpCode, reason } }); reload(); return run === 'start' ? '做市已启动' : '做市已停止' }} />}</>
}

function Pairs({ q, states, onSaved }) {
  const [cur, setCur] = useState(null); const [d, setD] = useState({}); const [ok, setOk] = useState(false)
  const stOf = r => states.find(x => x.market === r.market && x.symbol === r.symbol)
  const rows = items(q.data).map(r => ({ ...r, id: r.market + '/' + r.symbol }))
  const fields = cur ? FIELDS.filter(f => !f[3] || cur.market === 'futures') : []
  const open = r => { setCur(r); setD(Object.fromEntries([['enabled', r.enabled], ...FIELDS.map(([k]) => [k, r[k] ?? ''])])) }
  const changed = cur ? ['enabled', ...fields.map(f => f[0])].filter(k => String(d[k] ?? '') !== String(cur[k] ?? '')) : []
  const body = () => Object.fromEntries(changed.map(k => { const f = FIELDS.find(x => x[0] === k); return [k, f && f[2] ? parseInt(d[k], 10) : d[k]] }))
  return <><h3 style={{ margin: '16px 0 8px' }}>交易对参数</h3>
    <ApiState q={q}><DataTable rows={rows} onRow={open} cols={[
      { t: '市场', render: r => r.market === 'futures' ? '合约' : '现货' }, { t: '交易对', render: r => <b>{r.symbol}</b> },
      { t: '启用', render: r => <Tag k={r.enabled ? 'ok' : ''}>{r.enabled ? '是' : '否'}</Tag> },
      { t: '运行状态', render: r => { const x = stOf(r); const [n, k] = ST[x?.status] || [x?.status || '-', '']; return <span title={x?.reason || x?.lastError || x?.bookSource || ''}><Tag k={k}>{n}</Tag></span> } },
      { t: '盘口来源', render: r => stOf(r)?.bookSource || '-' },
      { t: '价差 bps', r: 1, num: 1, k: 'spreadBps' }, { t: '档位', r: 1, k: 'levels' }, { t: '每档金额', r: 1, num: 1, k: 'levelNotional' },
      { t: '最大持仓', r: 1, num: 1, k: 'maxPosition' }, { t: '挂单数', r: 1, render: r => stOf(r)?.openOrders ?? '-' },
      { t: '更新', render: r => <span className="muted">{ft(r.updatedAt)} {r.updatedBy || ''}</span> }]} /></ApiState>
    {cur && <Drawer title={`${cur.market === 'futures' ? '合约' : '现货'} · ${cur.symbol}`} onClose={() => setCur(null)} footer={<button className="btn p" disabled={!changed.length} onClick={() => setOk(true)}>保存</button>}>
      <div className="fr"><label>启用做市</label><Sw on={d.enabled} onClick={() => setD({ ...d, enabled: !d.enabled })} /></div>
      {fields.map(([k, l]) => <div className="fr" key={k}><label>{l}</label><input className="in" value={d[k] ?? ''} onChange={e => setD({ ...d, [k]: e.target.value })} /></div>)}</Drawer>}
    {ok && <Confirm danger title={`修改 ${cur.symbol} 做市参数`} rows={changed.map(k => [k, `${cur[k] ?? '-'} → ${d[k]}`])} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { await api(`mm/pairs/${cur.market}/${encodeURIComponent(cur.symbol)}`, { method: 'PUT', body: { ...body(), totpCode, reason } }); setCur(null); onSaved(); return '已保存' }} />}</>
}

function Baselines({ q, onSaved }) {
  const [cur, setCur] = useState(null); const [d, setD] = useState({}); const [ok, setOk] = useState(false)
  const open = r => { setCur(r); setD({ asset: r.asset || '', targetBase: r.targetBase ?? '', clearTarget: false, resetStart: false }) }
  const valid = d.asset && (d.clearTarget || d.resetStart || String(d.targetBase).trim() !== '')
  return <><h3 style={{ margin: '16px 0 8px' }}>库存基线 <span className="muted" style={{ fontSize: 12 }}>（现货最大持仓按 净持仓 = 余额 − 基线 计算）</span>
    <button className="btn" style={{ marginLeft: 8 }} onClick={() => open({})}>新增 / 修改</button></h3>
    <ApiState q={q}><DataTable rowKey="asset" rows={items(q.data)} onRow={open} cols={[{ t: '资产', render: r => <b>{r.asset}</b> },
      { t: '生效基线', r: 1, num: 1, k: 'baseline' }, { t: '来源', k: 'source' }, { t: '目标基线', r: 1, num: 1, render: r => r.targetBase ?? '-' },
      { t: '启动时余额', r: 1, num: 1, render: r => r.startBase ?? '-' }, { t: '更新人', render: r => r.updatedBy || '-' }]} /></ApiState>
    {cur && <Drawer title="库存基线" onClose={() => setCur(null)} footer={<button className="btn p" disabled={!valid} onClick={() => setOk(true)}>保存</button>}>
      <div className="fr"><label>资产</label><input className="in" value={d.asset} disabled={!!cur.asset} onChange={e => setD({ ...d, asset: e.target.value.toUpperCase() })} placeholder="BTC" /></div>
      <div className="fr"><label>目标基线</label><input className="in" value={d.targetBase} disabled={d.clearTarget} onChange={e => setD({ ...d, targetBase: e.target.value })} /></div>
      <div className="fr"><label>清除目标基线</label><Sw on={d.clearTarget} onClick={() => setD({ ...d, clearTarget: !d.clearTarget })} /></div>
      <div className="fr"><label>下次启动重新记录余额</label><Sw on={d.resetStart} onClick={() => setD({ ...d, resetStart: !d.resetStart })} /></div></Drawer>}
    {ok && <Confirm danger title={`修改 ${d.asset} 库存基线`} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => {
        const body = { clearTarget: d.clearTarget, resetStart: d.resetStart, totpCode, reason }
        if (!d.clearTarget && String(d.targetBase).trim() !== '') body.targetBase = String(d.targetBase).trim()
        await api(`mm/baselines/${encodeURIComponent(d.asset)}`, { method: 'PUT', body }); setCur(null); onSaved(); return '已保存' }} />}</>
}

function Credit({ status, onSaved }) {
  const [d, setD] = useState({ asset: 'USDT', amount: '' }); const [ok, setOk] = useState(false)
  const max = status?.seedMax || '10000000'
  return <><h3 style={{ margin: '16px 0 8px' }}>做市账户充值 <span className="muted" style={{ fontSize: 12 }}>（直接入账到现货账户，主网可用，单笔上限 {max}）</span></h3>
    <div className="card pad">
      <div className="fr"><label>资产</label><input className="in" value={d.asset} onChange={e => setD({ ...d, asset: e.target.value.toUpperCase() })} placeholder="USDT" /></div>
      <div className="fr"><label>金额</label><input className="in" value={d.amount} onChange={e => setD({ ...d, amount: e.target.value })} placeholder="输入要增加的数量" /></div>
      <button className="btn p" disabled={!(d.asset && +d.amount > 0)} onClick={() => setOk(true)}>充值</button></div>
    {ok && <Confirm danger title="做市账户充值" rows={[['账户', status?.account || 'mm:main'], ['资产', d.asset], ['金额', d.amount]]} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => {
        const ref = 'mm-credit-' + Date.now() + '-' + Math.random().toString(36).slice(2, 8)
        await api('mm/credit', { method: 'POST', body: { ref, asset: d.asset, amount: String(d.amount).trim(), totpCode, reason } })
        setD({ ...d, amount: '' }); onSaved(); return '已入账 ' + d.amount + ' ' + d.asset
      }} />}</>
}

function Transfer({ onSaved }) {
  const [d, setD] = useState({ direction: 'in', amount: '' }); const [ok, setOk] = useState(false)
  return <><h3 style={{ margin: '16px 0 8px' }}>做市账户 现货 ⇄ 合约 划转（USDT）</h3>
    <div className="card pad">
      <div className="fr"><label>方向</label><select className="sel" value={d.direction} onChange={e => setD({ ...d, direction: e.target.value })}>
        <option value="in">现货 → 合约</option><option value="out">合约 → 现货</option></select></div>
      <div className="fr"><label>金额</label><input className="in" value={d.amount} onChange={e => setD({ ...d, amount: e.target.value })} placeholder="USDT" /></div>
      <button className="btn p" disabled={!(+d.amount > 0)} onClick={() => setOk(true)}>划转</button></div>
    {ok && <Confirm danger title="做市账户划转" rows={[['方向', d.direction === 'in' ? '现货 → 合约' : '合约 → 现货'], ['金额', d.amount + ' USDT']]} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { await api('mm/futures-transfer', { method: 'POST', body: { ...d, totpCode, reason } }); setD({ ...d, amount: '' }); onSaved(); return '划转已提交' }} />}</>
}
