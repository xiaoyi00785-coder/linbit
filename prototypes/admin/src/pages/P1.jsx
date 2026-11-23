/* P1 只读列表：成交 / 合约委托 / 登录历史 / 报表导出 / 理财借贷 / 闪兑 / 事件合约 / 单用户费率 / 子账户 / VIP 申请 /
   邀请关系 / 内部转账 / 用户卡券 / 合约风险参数。全部来自 admin/v1 真实接口；筛选条件按回车或「查询」才请求。
   大表（订单/成交）用游标分页，不做总数统计。 */
import { useState } from 'react'
import { ApiState, DataTable, Drawer, Empty, Kpi, LineChart, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { ft, items, useApi } from '../api.js'

/* ---------- 通用 ---------- */
export const dec = (v, max = 8) => { if (v === null || v === undefined || v === '') return '-'; const n = Number(v); if (!isFinite(n)) return String(v)
  return n.toLocaleString('en-US', { maximumFractionDigits: Math.abs(n) >= 1000 ? 2 : max }) }
export const pct = (v, d = 4) => v === null || v === undefined || v === '' ? '-' : (Number(v) * 100).toFixed(d).replace(/0+$/, '').replace(/\.$/, '') + '%'
const side = s => <span className={s === 'buy' || s === 'up' || s === 'long' ? 'up' : 'down'}>{{ buy: '买入', sell: '卖出', up: '看涨', down: '看跌' }[s] || s}</span>
const yes = b => b ? <Tag k="ok">是</Tag> : <span className="muted">否</span>

/** 筛选：编辑中的 draft 与已提交的 applied 分开，回车或点「查询」才发请求 */
export function useFilters(init = {}) {
  const { q } = useApp(); init = { ...init, ...(q || {}) } // 顶栏搜索跳转带来的 ?uid= / ?orderId= 等
  const [draft, setDraft] = useState(init); const [applied, setApplied] = useState(() => Object.fromEntries(Object.entries(init).filter(([k, v]) => v !== '' || k === 'status')))
  const set = k => e => setDraft({ ...draft, [k]: e.target.value })
  const apply = (next) => setApplied(Object.fromEntries(Object.entries(next || draft).map(([k, v]) => [k, typeof v === 'string' ? v.trim() : v]).filter(([, v]) => v !== '')))
  return { draft, applied, set, apply, setDraft }
}
export function Filters({ f, fields, children }) {
  const onKey = e => { if (e.key === 'Enter') f.apply() }
  return <>{fields.map(([k, ph, opts]) => opts
    ? <select key={k} className="sel" value={f.draft[k] || ''} onChange={e => { const d = { ...f.draft, [k]: e.target.value }; f.setDraft(d); f.apply(d) }}>{opts.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
    : <input key={k} className="in" placeholder={ph} value={f.draft[k] || ''} onChange={f.set(k)} onKeyDown={onKey} />)}
    <button className="btn" onClick={() => f.apply()}>查询</button>{children}</>
}

/** 游标分页（before = 上一页最后一条 id） */
function useSeek(path, applied, size = 50) {
  const [stack, setStack] = useState([]); const before = stack[stack.length - 1]
  const key = JSON.stringify(applied); const [k0, setK0] = useState(key); if (k0 !== key) { setK0(key); setStack([]) }
  const q = useApi(path, { ...applied, size, ...(before ? { before } : {}) })
  const pager = <><span>第 {stack.length + 1} 页 · 每页 {size} 条（大表不统计总数）</span><span className="sp" />
    <b onClick={() => stack.length && setStack(stack.slice(0, -1))} style={{ opacity: stack.length ? 1 : .4 }}>‹ 上一页</b>
    <b onClick={() => q.data && q.data.hasMore && setStack([...stack, q.data.nextBefore])} style={{ opacity: q.data && q.data.hasMore ? 1 : .4 }}>下一页 ›</b></>
  return [q, pager]
}
/** 服务端页码分页（返回 total） */
function usePaged(path, applied, size = 50, sizeKey = 'size') {
  const [page, setPage] = useState(1); const key = JSON.stringify(applied); const [k0, setK0] = useState(key); if (k0 !== key) { setK0(key); setPage(1) }
  const q = useApi(path, { ...applied, page, [sizeKey]: size }); const total = q.data ? (q.data.total ?? 0) : 0; const pages = Math.max(1, Math.ceil(total / size))
  const pager = <><span>共 {total} 条 · 第 {page}/{pages} 页</span><span className="sp" /><b onClick={() => page > 1 && setPage(page - 1)} style={{ opacity: page > 1 ? 1 : .4 }}>‹ 上一页</b>
    <b onClick={() => page < pages && setPage(page + 1)} style={{ opacity: page < pages ? 1 : .4 }}>下一页 ›</b></>
  return [q, pager]
}
/** 行详情：所有字段键值展示 */
export function RowDrawer({ title, row, onClose, labels = {} }) {
  if (!row) return null
  const show = v => v === null || v === undefined || v === '' ? '-' : typeof v === 'object' ? <pre className="card pad" style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{JSON.stringify(v, null, 2)}</pre> : String(v)
  return <Drawer title={title} onClose={onClose}><dl className="kv">{Object.entries(row).flatMap(([k, v]) => [<dt key={k}>{labels[k] || k}</dt>,
    <dd key={k + 'v'} style={{ wordBreak: 'break-all' }}>{/(At|Time|ts|Seen)$/.test(k) && v ? ft(v) : show(v)}</dd>])}</dl></Drawer>
}

/* ===================== 现货成交 ===================== */
export function SpotTrades() {
  const f = useFilters(); const [q, pager] = useSeek('spot/trades', f.applied); const [cur, setCur] = useState(null)
  return <><PageHead title="现货成交" desc="全站现货成交明细（只读，按成交 ID 倒序游标分页）" />
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID'], ['symbol', '交易对，如 BTCUSDT'], ['orderId', '订单号'], ['side', '', [['', '全部方向'], ['buy', '买入'], ['sell', '卖出']]]]} />}
      cols={[{ t: '成交 ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' }, { t: '方向', render: r => side(r.side) }, { t: '角色', render: r => r.role === 'maker' ? 'Maker' : 'Taker' },
        { t: '价格', r: 1, num: 1, render: r => dec(r.price) }, { t: '数量', r: 1, num: 1, render: r => dec(r.quantity) }, { t: '成交额', r: 1, num: 1, render: r => dec(r.quoteQty, 4) },
        { t: '手续费', r: 1, num: 1, render: r => dec(r.fee) + ' ' + (r.feeAsset || '') }, { t: '订单号', k: 'orderId' }]} /></ApiState>
    <RowDrawer title={cur && '成交 #' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 合约委托 / 合约成交 ===================== */
const FO_ST = { new: '挂单中', partially_filled: '部分成交', filled: '已成交', canceled: '已撤销', rejected: '已拒绝', expired: '已过期' }
export function FutOrders() {
  const f = useFilters(); const [q, pager] = useSeek('futures/orders', f.applied); const [cur, setCur] = useState(null)
  return <><PageHead title="合约委托" desc="USDT 本位永续委托（只读）。指定 UID 时走用户索引，速度最快" />
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID'], ['symbol', '合约，如 BTCUSDT'], ['side', '', [['', '全部方向'], ['buy', '买入'], ['sell', '卖出']]],
      ['status', '', [['', '全部状态'], ['open', '挂单中/部分成交'], ['filled', '已成交'], ['canceled', '已撤销'], ['rejected', '已拒绝']]]]} />}
      cols={[{ t: '委托 ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: 'UID', k: 'uid' }, { t: '合约', k: 'symbol' }, { t: '方向', render: r => <>{side(r.side)}{r.reduceOnly ? <span className="muted"> · 只减仓</span> : ''}</> },
        { t: '类型', render: r => (r.type === 'market' ? '市价' : '限价') + (r.tif && r.tif !== 'GTC' ? ' ' + r.tif : '') }, { t: '价格', r: 1, num: 1, render: r => dec(r.price) }, { t: '数量', r: 1, num: 1, render: r => dec(r.qty) },
        { t: '已成交', r: 1, num: 1, render: r => dec(r.filled) }, { t: '均价', r: 1, num: 1, render: r => dec(r.avgPrice, 2) }, { t: '杠杆', render: r => r.leverage + 'x ' + (r.marginMode === 'isolated' ? '逐仓' : '全仓') },
        { t: '状态', render: r => <Tag k={r.status === 'filled' ? 'ok' : r.status === 'canceled' ? '' : 'in'}>{FO_ST[r.status] || r.status}</Tag> }]} /></ApiState>
    <RowDrawer title={cur && '合约委托 #' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}
export function FutTrades() {
  const f = useFilters(); const [q, pager] = useSeek('futures/trades', f.applied); const [cur, setCur] = useState(null)
  return <><PageHead title="合约成交" desc="永续合约成交（只读）。按 UID 查询时同时匹配 Maker / Taker；如超时请加合约过滤" />
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID'], ['symbol', '合约，如 BTCUSDT']]} />}
      cols={[{ t: '成交 ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: '合约', k: 'symbol' }, { t: 'Taker 方向', render: r => side(r.takerSide) }, { t: '价格', r: 1, num: 1, render: r => dec(r.price) },
        { t: '数量', r: 1, num: 1, render: r => dec(r.qty) }, { t: '名义价值', r: 1, num: 1, render: r => dec(r.notional, 2) }, { t: 'Maker', k: 'makerUid' }, { t: 'Taker', k: 'takerUid' },
        { t: 'Maker 费', r: 1, num: 1, render: r => dec(r.makerFee) }, { t: 'Taker 费', r: 1, num: 1, render: r => dec(r.takerFee) }]} /></ApiState>
    <RowDrawer title={cur && '合约成交 #' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 合约风险参数（只读） ===================== */
export function FutRisk() {
  const [t, setT] = useState('overview'); const [sym, setSym] = useState('BTCUSDT')
  const ov = useApi('futures/risk-overview'); const cs = (ov.data && ov.data.contracts) || []
  const symSel = <select className="sel" value={sym} onChange={e => setSym(e.target.value)}>{(cs.length ? cs.map(c => c.symbol) : [sym]).map(s => <option key={s}>{s}</option>)}</select>
  return <><PageHead title="合约风险参数" desc="资金费率、标记价/指数价、风险档位、保险基金与 ADL（只读）。修改合约参数请到「合约参数」，涉及资金的参数变更需单独审批" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['overview', '总览'], ['funding', '资金费率'], ['mark', '标记价'], ['tiers', '风险档位'], ['insurance', '保险基金'], ['adl', 'ADL 记录']]} /></div>
    {t === 'overview' && <ApiState q={ov}>{ov.data && <>{(ov.data.errors || []).length > 0 && <div className="banner bad" style={{ marginTop: 12 }}>部分数据加载失败：{ov.data.errors.join('、')}</div>}
      <div className="grid g4" style={{ margin: '12px 0' }}><Kpi l="保险基金" v={ov.data.insurance[0] ? dec(ov.data.insurance[0].balance, 2) + ' ' + ov.data.insurance[0].asset : '-'} d={ov.data.insurance[0] ? '快照 ' + ft(ov.data.insurance[0].ts) : '暂无快照'} />
        <Kpi l="合约数" v={cs.length} d={`交易中 ${cs.filter(c => c.status === 'trading').length}`} /><Kpi l="24h ADL 次数" v={(ov.data.adl24h || []).reduce((a, x) => a + +x.count, 0)} />
        <Kpi l="最大基差" v={cs.length ? Math.max(...cs.map(c => Math.abs(+c.basisBps || 0))).toFixed(2) + ' bps' : '-'} d="|标记价-指数价|/指数价" /></div>
      <DataTable rowKey="symbol" rows={cs} pager={false} cols={[{ t: '合约', render: r => <b>{r.symbol}</b> }, { t: '状态', render: r => <Tag k={r.status === 'trading' ? 'ok' : 'bad'}>{r.status === 'trading' ? '交易中' : '暂停'}</Tag> },
        { t: '标记价', r: 1, num: 1, render: r => dec(r.markPrice, 4) }, { t: '指数价', r: 1, num: 1, render: r => dec(r.indexPrice, 4) }, { t: '基差', r: 1, num: 1, render: r => r.basisBps ? r.basisBps + ' bps' : '-' },
        { t: '最近资金费率', r: 1, num: 1, render: r => <span className={+r.lastFundingRate >= 0 ? 'up' : 'down'}>{pct(r.lastFundingRate)}</span> }, { t: '结算时间', render: r => ft(r.lastFundingTime) },
        { t: '最大杠杆', r: 1, render: r => r.maxLeverage + 'x' }, { t: '维持保证金率', r: 1, render: r => pct(r.mmr, 3) }, { t: '档位', r: 1, render: r => r.tiers }, { t: '多/空持仓量', r: 1, num: 1, render: r => dec(r.longQty, 4) + ' / ' + dec(r.shortQty, 4) },
        { t: '标记价更新', render: r => <span className={r.markTs && Date.now() - new Date(r.markTs) > 120000 ? 'down' : 'muted'}>{ft(r.markTs)}</span> }]} /></>}</ApiState>}
    {t === 'funding' && <Series path="futures/funding" sym={sym} symSel={symSel} k="fundingTime" v="rate" lbl="资金费率" fmt={x => pct(x)} extra={[{ t: '标记价', r: 1, num: 1, render: r => dec(r.markPrice, 2) }, { t: '来源', k: 'source' }]} />}
    {t === 'mark' && <Series path="futures/marks" sym={sym} symSel={symSel} k="ts" v="markPrice" v2="indexPrice" lbl="标记价" fmt={x => dec(x, 4)} extra={[{ t: '指数价', r: 1, num: 1, render: r => dec(r.indexPrice, 4) }, { t: '基差', r: 1, num: 1, render: r => (r.basisBps || '-') + ' bps' }]} />}
    {t === 'tiers' && <Tiers sym={sym} symSel={symSel} />}
    {t === 'insurance' && <ApiState q={ov}>{ov.data && <><div className="card pad" style={{ marginTop: 12 }}><h3>保险基金余额（最近 {ov.data.insurance.length} 个快照）</h3>
      {ov.data.insurance.length > 1 ? <LineChart fit labels={[...ov.data.insurance].reverse().map(x => ft(x.ts).slice(5, 13))} series={[{ color: '#9be21e', data: [...ov.data.insurance].reverse().map(x => +x.balance) }]} /> : <Empty />}</div>
      <DataTable rowKey="ts" rows={ov.data.insurance} cols={[{ t: '时间', render: r => ft(r.ts) }, { t: '币种', k: 'asset' }, { t: '余额', r: 1, num: 1, render: r => dec(r.balance, 2) }, { t: '权益', r: 1, num: 1, render: r => dec(r.equity, 2) }]} /></>}</ApiState>}
    {t === 'adl' && <Adl />}</>
}
function Series({ path, sym, symSel, k, v, v2, lbl, fmt, extra }) {
  const q = useApi(path, { symbol: sym, size: 120 }); const rows = items(q.data); const rv = [...rows].reverse()
  return <ApiState q={q}><div className="card pad" style={{ marginTop: 12 }}><div style={{ display: 'flex', gap: 10, alignItems: 'center' }}><h3 style={{ margin: 0 }}>{lbl} · {sym}</h3>{symSel}<span className="muted">最近 {rows.length} 条{v2 ? ' · 绿=标记价 灰=指数价' : ''}</span></div>
    {rows.length > 1 ? <LineChart area={false} fit labels={rv.map(x => ft(x[k]).slice(5, 16))} series={[{ color: '#9be21e', data: rv.map(x => +x[v]) }, ...(v2 ? [{ color: '#8a94a6', data: rv.map(x => +x[v2]) }] : [])]} /> : <Empty />}</div>
    <DataTable rowKey={k} rows={rows} pageSize={20} cols={[{ t: '时间', render: r => ft(r[k]) }, { t: lbl, r: 1, num: 1, render: r => fmt(r[v]) }, ...extra]} /></ApiState>
}
function Tiers({ sym, symSel }) {
  const q = useApi('futures/risk-tiers', { symbol: sym })
  return <ApiState q={q}><div style={{ marginTop: 12 }}><DataTable rowKey="tier" rows={items(q.data)} pager={false} filters={<>{symSel}<span className="muted">名义价值越大，维持保证金率越高、最大杠杆越低</span></>}
    cols={[{ t: '档位', k: 'tier' }, { t: '名义价值上限 (USDT)', r: 1, num: 1, render: r => dec(r.maxNotional, 0) }, { t: '维持保证金率', r: 1, render: r => pct(r.mmr, 3) }, { t: '维持保证金速算额', r: 1, num: 1, render: r => dec(r.maintAmount, 2) }, { t: '最大杠杆', r: 1, render: r => r.maxLeverage + 'x' }]} /></div></ApiState>
}
function Adl() {
  const f = useFilters(); const [q, pager] = usePaged('futures/adl', f.applied)
  return <ApiState q={q}><div style={{ marginTop: 12 }}><DataTable rows={items(q.data)} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID（被强平方或对手方）'], ['symbol', '合约']]} />}
    cols={[{ t: 'ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: '合约', k: 'symbol' }, { t: '被强平 UID', k: 'liqUid' }, { t: '被减仓 UID', k: 'counterUid' }, { t: '数量', r: 1, num: 1, render: r => dec(r.qty) },
      { t: '成交价', r: 1, num: 1, render: r => dec(r.price, 2) }, { t: '对手方盈亏', r: 1, num: 1, render: r => <span className={+r.counterPnl >= 0 ? 'up' : 'down'}>{dec(r.counterPnl, 4)}</span> }]} /></div></ApiState>
}

/* ===================== 登录历史 ===================== */
export function LoginHistory() {
  const f = useFilters(); const [q, pager] = usePaged('login-history', f.applied)
  return <><PageHead title="登录历史" desc="用户登录设备与 IP（每台设备记录首次与最近一次登录）。「同 IP 用户数」>1 可能为多账户关联" />
    <ApiState q={q}><DataTable rows={items(q.data)} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID'], ['ip', 'IP']]} />}
      cols={[{ t: 'UID', k: 'uid' }, { t: '设备', k: 'name' }, { t: 'IP', k: 'ip' }, { t: '同 IP 用户数', r: 1, render: r => +r.ipUsers > 1 ? <Tag k="wn">{r.ipUsers}</Tag> : r.ipUsers }, { t: 'User-Agent', render: r => <span className="muted" style={{ fontSize: 12 }}>{r.userAgent}</span> },
        { t: '首次登录', render: r => ft(r.firstSeen) }, { t: '最近登录', render: r => ft(r.lastSeen) }]} /></ApiState></>
}

/* ===================== 报表导出 ===================== */
const RX_ST = { queued: ['排队中', 'in'], running: ['生成中', 'in'], ready: ['可下载', 'ok'], failed: ['失败', 'bad'], expired: ['已过期', ''] }
const RX_KIND = { trades: '成交', deposits: '充值', withdrawals: '提现', transfers: '划转', funding: '资金费', ledger: '流水' }
export function ReportExports() {
  const f = useFilters(); const [q, pager] = usePaged('report-exports', f.applied); const st = (q.data && q.data.stats) || {}
  return <><PageHead title="报表导出" desc="用户自助导出任务（只读）。不显示下载链接；排查失败原因或积压时使用" />
    <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="排队/生成中" v={st.pending ?? '-'} up={!(+st.pending > 5)} /><Kpi l="24h 新建" v={st.created24h ?? '-'} /><Kpi l="24h 失败" v={st.failed24h ?? '-'} up={!(+st.failed24h > 0)} /><Kpi l="当前筛选" v={q.data ? q.data.total : '-'} /></div>
    <ApiState q={q}><DataTable rows={items(q.data)} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID'], ['kind', '', [['', '全部类型'], ...Object.entries(RX_KIND)]], ['status', '', [['', '全部状态'], ...Object.entries(RX_ST).map(([k, v]) => [k, v[0]])]]]} />}
      cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '类型', render: r => RX_KIND[r.kind] || r.kind }, { t: '格式', render: r => (r.format || '').toUpperCase() }, { t: '时间范围', render: r => ft(r.fromAt).slice(0, 10) + ' ~ ' + ft(r.toAt).slice(0, 10) },
        { t: '状态', render: r => { const s = RX_ST[r.status] || [r.status]; return <Tag k={s[1]}>{s[0]}</Tag> } }, { t: '行数', r: 1, render: r => r.rowCount ?? '-' }, { t: '大小', r: 1, render: r => r.sizeBytes ? (r.sizeBytes / 1024).toFixed(1) + ' KB' : '-' },
        { t: '创建', render: r => ft(r.createdAt) }, { t: '完成', render: r => ft(r.finishedAt) }, { t: '错误', render: r => r.error ? <span className="down" style={{ fontSize: 12 }}>{r.error}</span> : '' }]} /></ApiState></>
}

/* ===================== 理财与借贷 ===================== */
const ONL = s => <Tag k={s === 'online' ? 'ok' : ''}>{{ online: '上线', offline: '下线', soldout: '售罄', paused: '暂停' }[s] || s}</Tag>
export function EarnLoans() {
  const [t, setT] = useState('earn'); const ep = useApi(t === 'earn' ? 'earn/products' : null); const lt = useApi(t === 'loan' ? 'loans/terms' : null); const lc = useApi(t === 'loan' ? 'loans/collaterals' : null); const rc = useApi(t === 'rate' ? 'rate-changes' : null)
  const [cur, setCur] = useState(null)
  return <><PageHead title="理财与借贷" desc="理财产品、借贷期限与抵押币种、利率变更记录（只读）。利率/额度修改走「利率变更」双人审批" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['earn', '理财产品'], ['loan', '借贷'], ['rate', '利率变更记录']]} /></div>
    {t === 'earn' && <ApiState q={ep}><div style={{ marginTop: 12 }}><DataTable rows={items(ep.data)} onRow={setCur} pageSize={20} cols={[{ t: '产品', render: r => <><b>{r.title}</b> <span className="muted">{r.id}</span></> }, { t: '类型', render: r => r.kind === 'simple' ? '活期' : r.kind === 'fixed' ? `定期 ${r.termDays} 天` : r.kind },
      { t: '币种', k: 'asset' }, { t: '年化', r: 1, render: r => pct(r.apr, 2) }, { t: '加息年化', r: 1, render: r => r.bonusApr ? pct(r.bonusApr, 2) + (r.bonusQuota ? ` (≤${dec(r.bonusQuota)})` : '') : '-' }, { t: '起投', r: 1, render: r => dec(r.minAmount) },
      { t: '已售', r: 1, num: 1, render: r => dec(r.sold) }, { t: '新人', render: r => yes(r.newcomer) }, { t: '热门', render: r => yes(r.hot) }, { t: '状态', render: r => ONL(r.status) }, { t: '赎回', k: 'redeemRule' }]} /></div></ApiState>}
    {t === 'loan' && <div className="grid g21" style={{ marginTop: 12 }}><ApiState q={lt}><DataTable rowKey="term" rows={items(lt.data)} pager={false} filters={<b>借贷期限</b>} cols={[{ t: '期限', render: r => r.term === 'flexible' ? '活期' : r.term }, { t: '天数', r: 1, k: 'days' },
      { t: '年化', r: 1, render: r => pct(r.annualRate, 2) }, { t: '时利率', r: 1, render: r => pct(r.hourlyRate, 6) }, { t: '在借余额', r: 1, num: 1, render: r => dec(r.outstanding, 2) }, { t: '状态', render: r => ONL(r.status) }]} /></ApiState>
      <ApiState q={lc}><DataTable rowKey="asset" rows={items(lc.data)} pager={false} filters={<b>抵押币种</b>} cols={[{ t: '币种', k: 'asset' }, { t: '抵押上限', r: 1, render: r => dec(r.cap) }, { t: '排序', r: 1, k: 'sort' }, { t: '状态', render: r => ONL(r.status) }]} /></ApiState></div>}
    {t === 'rate' && <ApiState q={rc}><div style={{ marginTop: 12 }}><DataTable rows={items(rc.data)} onRow={setCur} emptyText="暂无利率变更记录" cols={[{ t: 'ID', k: 'id' }, { t: '类型', render: r => r.kind === 'earn_rate' ? '理财利率' : r.kind === 'loan_rate' ? '借贷利率' : r.kind },
      { t: '对象', k: 'target' }, { t: '状态', k: 'status' }, { t: '提交人', k: 'submittedBy' }, { t: '时间', render: r => ft(r.createdAt) }]} /></div></ApiState>}
    <RowDrawer title={cur && (cur.title || '记录 ' + cur.id)} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 闪兑 ===================== */
export function Convert() {
  const [t, setT] = useState('orders'); const f = useFilters()
  const [oq, opager] = usePaged(t === 'orders' ? 'convert/orders' : null, f.applied, 50, 'pageSize'); const pos = useApi(t === 'position' ? 'convert/position' : null)
  const [hq, hpager] = usePaged(t === 'hedges' ? 'convert/hedges' : null, {}, 50, 'pageSize'); const [eq, epager] = usePaged(t === 'events' ? 'convert/events' : null, {}, 50, 'pageSize'); const cfg = useApi(t === 'config' ? 'convert/config' : null)
  const [cur, setCur] = useState(null)
  return <><PageHead title="闪兑" desc="闪兑订单、平台头寸与对冲、风控事件、当前配置（只读）" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['orders', '订单'], ['position', '头寸'], ['hedges', '对冲'], ['events', '事件'], ['config', '配置']]} /></div>
    {t === 'orders' && <ApiState q={oq}><div style={{ marginTop: 12 }}><DataTable rows={items(oq.data)} onRow={setCur} pager={opager} filters={<Filters f={f} fields={[['userId', 'UID'], ['asset', '币种']]} />}
      cols={[{ t: 'ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: 'UID', k: 'userId' }, { t: '兑换', render: r => `${dec(r.fromAmount)} ${r.from} → ${dec(r.toAmount)} ${r.to}` }, { t: '成交价', r: 1, num: 1, render: r => dec(r.rate) },
        { t: '指数价', r: 1, num: 1, render: r => dec(r.indexRate) }, { t: '点差', r: 1, render: r => r.spreadBps + ' bps' }, { t: 'USDT 价值', r: 1, num: 1, render: r => dec(r.usdtValue, 2) }, { t: '状态', render: r => <Tag k={r.status === 'filled' ? 'ok' : ''}>{r.status}</Tag> }]} /></div></ApiState>}
    {t === 'position' && <ApiState q={pos}>{pos.data && <div style={{ marginTop: 12 }}>
      {pos.data.depeg && <div className={'banner' + (pos.data.depeg.paused ? ' bad' : '')}>脱锚保护 {pos.data.depeg.pair}：{pos.data.depeg.paused ? `已暂停（${pos.data.depeg.reason}）` : '正常'} · 阈值 {pos.data.depeg.thresholdBps} bps</div>}
      <DataTable rowKey="asset" rows={items(pos.data)} pager={false} filters={<span className="muted">平台账户 {pos.data.account || '-'} · 对冲账户 {pos.data.hedgeAccount || '-'} · 对冲 {pos.data.hedgeEnabled ? '开启' : '关闭'}</span>}
        cols={[{ t: '币种', k: 'asset' }, { t: '平台头寸', r: 1, num: 1, render: r => dec(r.position) }, { t: '净头寸', r: 1, num: 1, render: r => dec(r.netPosition) }, { t: '对冲余额', r: 1, num: 1, render: r => dec(r.hedgeBalance) },
          { t: '未对冲 (USDT)', r: 1, num: 1, render: r => dec(r.hedge && r.hedge.unhedgedUsdt, 2) }, { t: '对冲状态', render: r => r.hedge ? (r.hedge.lastError ? <span className="down" style={{ fontSize: 12 }}>{r.hedge.lastSkip || '错误'}</span> : r.hedge.lastSkip || '正常') : '-' },
          { t: '最近运行', render: r => ft(r.hedge && r.hedge.lastRunAt) }]} /></div>}</ApiState>}
    {t === 'hedges' && <ApiState q={hq}><div style={{ marginTop: 12 }}><DataTable rows={items(hq.data)} onRow={setCur} pager={hpager} emptyText="暂无对冲单" cols={[{ t: 'ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: '币种', k: 'asset' }, { t: '方向', render: r => side(r.side) },
      { t: '数量', r: 1, num: 1, render: r => dec(r.qty) }, { t: '状态', k: 'status' }]} /></div></ApiState>}
    {t === 'events' && <ApiState q={eq}><div style={{ marginTop: 12 }}><DataTable rows={items(eq.data)} onRow={setCur} pager={epager} cols={[{ t: 'ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: '事件', k: 'kind' }, { t: '对象', k: 'target' },
      { t: '详情', render: r => <span className="muted" style={{ fontSize: 12 }}>{JSON.stringify(r.detail)}</span> }]} /></div></ApiState>}
    {t === 'config' && <ApiState q={cfg}>{cfg.data && <div className="grid g2" style={{ marginTop: 12 }}><div className="card pad"><h3>报价与限额</h3><dl className="kv"><dt>点差</dt><dd>{cfg.data.spreadBps} bps</dd><dt>单笔范围</dt><dd>{dec(cfg.data.minUsdt)} ~ {dec(cfg.data.maxUsdt)} USDT</dd>
      <dt>脱锚阈值</dt><dd>{cfg.data.depegBps} bps</dd><dt>更新</dt><dd>{cfg.data.updatedBy} · {ft(cfg.data.updatedAt)}</dd></dl>
      <h3 style={{ marginTop: 16 }}>对冲</h3><dl className="kv">{Object.entries(cfg.data.hedge || {}).flatMap(([k, v]) => [<dt key={k}>{k}</dt>, <dd key={k + 'v'}>{String(v)}</dd>])}</dl></div>
      <DataTable rowKey="asset" rows={cfg.data.assets || []} pager={false} filters={<b>币种</b>} cols={[{ t: '币种', k: 'asset' }, { t: '精度', r: 1, k: 'precision' }, { t: '启用', render: r => yes(r.enabled) }, { t: '对冲', render: r => yes(r.hedgeEnabled) }, { t: '对冲最小量', r: 1, render: r => dec(r.hedgeMinQty) }]} /></div>}</ApiState>}
    <RowDrawer title={cur && '记录 ' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 事件合约 ===================== */
const EV_ST = { pending: '待开仓', open: '进行中', settling: '结算中', settled: '已结算', rejected: '已拒绝' }
const EV_RES = { won: ['赢', 'ok'], lost: ['输', 'bad'], tie: ['平', ''], refunded: ['退款', ''] }
export function EventContracts() {
  const [t, setT] = useState('orders'); const f = useFilters(); const [stack, setStack] = useState([]); const before = stack[stack.length - 1]
  const key = JSON.stringify(f.applied); const [k0, setK0] = useState(key); if (k0 !== key) { setK0(key); setStack([]) }
  const oq = useApi(t === 'orders' ? 'event/orders' : null, { ...f.applied, limit: 50, ...(before ? { before } : {}) }); const ex = useApi(t === 'exposure' ? 'event/exposure' : null); const cfg = useApi(t === 'config' ? 'event/config' : null)
  const orders = (oq.data && oq.data.orders) || []; const [cur, setCur] = useState(null)
  const pager = <><span>第 {stack.length + 1} 页</span><span className="sp" /><b style={{ opacity: stack.length ? 1 : .4 }} onClick={() => stack.length && setStack(stack.slice(0, -1))}>‹ 上一页</b><b style={{ opacity: oq.data && oq.data.nextBefore ? 1 : .4 }} onClick={() => oq.data && oq.data.nextBefore && setStack([...stack, oq.data.nextBefore])}>下一页 ›</b></>
  return <><PageHead title="事件合约" desc="涨跌预测订单、各周期敞口与当前配置（只读）" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['orders', '订单'], ['exposure', '敞口'], ['config', '配置']]} /></div>
    {t === 'orders' && <ApiState q={oq}><div style={{ marginTop: 12 }}><DataTable rows={orders} onRow={setCur} pager={pager} filters={<Filters f={f} fields={[['uid', 'UID'], ['symbol', '标的'], ['status', '', [['', '全部状态'], ...Object.entries(EV_ST)]]]} />}
      cols={[{ t: 'ID', k: 'id' }, { t: '下单', render: r => ft(r.openTime) }, { t: 'UID', k: 'userId' }, { t: '标的', k: 'symbol' }, { t: '周期', k: 'period' }, { t: '方向', render: r => side(r.direction) }, { t: '本金', r: 1, num: 1, render: r => dec(r.stake, 2) },
        { t: '开仓价', r: 1, num: 1, render: r => dec(r.openPrice) }, { t: '结算价', r: 1, num: 1, render: r => dec(r.settlePrice) }, { t: '状态', render: r => EV_ST[r.status] || r.status },
        { t: '结果', render: r => r.result ? <Tag k={(EV_RES[r.result] || [])[1]}>{(EV_RES[r.result] || [r.result])[0]}</Tag> : '-' }, { t: '派彩', r: 1, num: 1, render: r => dec(r.payout ?? r.expectedPayout, 2) }, { t: '到期', render: r => ft(r.expiryTime) }]} /></div></ApiState>}
    {t === 'exposure' && <ApiState q={ex}><div style={{ marginTop: 12 }}><DataTable rowKey="k" rows={((ex.data && ex.data.books) || []).map(b => ({ ...b, k: b.symbol + b.period }))} pageSize={30}
      cols={[{ t: '标的', k: 'symbol' }, { t: '周期', k: 'period' }, { t: '看涨本金', r: 1, num: 1, render: r => dec(r.upStake, 2) }, { t: '看跌本金', r: 1, num: 1, render: r => dec(r.downStake, 2) }, { t: '净敞口', r: 1, num: 1, render: r => dec(r.netExposure, 2) },
        { t: '上限', r: 1, num: 1, render: r => dec(r.limit, 0) }, { t: '使用率', r: 1, render: r => <span className={+r.utilization > .8 ? 'down' : ''}>{pct(r.utilization, 2)}</span> }, { t: '进行中', r: 1, k: 'openOrders' }, { t: '暂停', render: r => yes(r.paused) }]} /></div></ApiState>}
    {t === 'config' && <ApiState q={cfg}>{cfg.data && cfg.data.config && <div className="grid g2" style={{ marginTop: 12 }}><div className="card pad"><h3>全局配置</h3><dl className="kv"><dt>周期</dt><dd>{(cfg.data.config.periods || []).join(' / ')}</dd><dt>赔率</dt><dd>{cfg.data.config.payoutRatio}</dd>
      <dt>单笔本金</dt><dd>{cfg.data.config.minStake} ~ {cfg.data.config.maxStake}</dd><dt>每日限额</dt><dd>{dec(cfg.data.config.dailyLimit)}</dd><dt>敞口上限</dt><dd>{dec(cfg.data.config.exposureLimit)}</dd><dt>结算宽限</dt><dd>{cfg.data.config.settleGraceSeconds} 秒</dd>
      <dt>更新</dt><dd>{cfg.data.config.updatedBy} · {ft(cfg.data.config.updatedAt)}</dd></dl></div>
      <DataTable rowKey="symbol" rows={cfg.data.symbols || []} pager={false} filters={<b>标的</b>} cols={[{ t: '标的', k: 'symbol' }, { t: '暂停', render: r => yes(r.paused) }, { t: '单独敞口上限', r: 1, render: r => r.exposureLimit ? dec(r.exposureLimit) : '默认' }, { t: '更新', render: r => ft(r.updatedAt) }]} /></div>}</ApiState>}
    <RowDrawer title={cur && '事件订单 #' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 单用户费率 ===================== */
export function FeeOverrides() {
  const [t, setT] = useState('spot'); const q = useApi('fee-overrides/' + t)
  return <><PageHead title="单用户费率" desc="对单个用户生效的 Maker/Taker 覆盖费率（只读；为空表示使用默认/VIP 费率）" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['spot', '现货'], ['futures', '合约']]} /></div>
    <ApiState q={q}><div style={{ marginTop: 12 }}><DataTable rowKey="userId" rows={items(q.data)} emptyText="暂无单用户费率" cols={[{ t: 'UID', k: 'userId' }, { t: 'Maker', r: 1, render: r => r.maker == null ? '默认' : pct(r.maker, 4) },
      { t: 'Taker', r: 1, render: r => r.taker == null ? '默认' : pct(r.taker, 4) }, { t: '备注', k: 'note' }, { t: '设置人', k: 'updatedBy' }, { t: '更新时间', render: r => ft(r.updatedAt) }]} /></div></ApiState></>
}

/* ===================== 子账户 ===================== */
export function SubAccounts() {
  const f = useFilters(); const [q, pager] = usePaged('sub-accounts', f.applied, 50); const st = useApi('sub-accounts/settings'); const [cur, setCur] = useState(null)
  return <><PageHead title="子账户" desc="母账户下的子账户与全局子账户策略（只读）。冻结/解冻请在用户详情中操作" />
    <ApiState q={st}>{st.data && <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="子账户功能" v={st.data.enabled ? '开启' : '关闭'} up={st.data.enabled} /><Kpi l="每个母账户上限" v={st.data.maxPerParent} />
      <Kpi l="子账户可独立登录" v={st.data.allowSubLogin ? '允许' : '禁止'} d={st.data.requireParent2fa ? '创建需母账户 2FA' : ''} /><Kpi l="子账户 API Key 上限" v={st.data.maxApiKeysPerSub} /></div>}</ApiState>
    <ApiState q={q}><DataTable rowKey="uid" rows={items(q.data)} onRow={setCur} pager={pager} emptyText="暂无子账户" filters={<Filters f={f} fields={[['parentUid', '母账户 UID'], ['q', '子账户 UID / 用户名'], ['status', '', [['', '全部状态'], ['active', '正常'], ['frozen', '冻结']]]]} />}
      cols={[{ t: '子账户 UID', k: 'uid' }, { t: '用户名', k: 'username' }, { t: '母账户', k: 'parentUid' }, { t: '备注', k: 'note' }, { t: '可登录', render: r => yes(r.loginEnabled) }, { t: '状态', k: 'status' }, { t: '创建', render: r => ft(r.createdAt) }]} /></ApiState>
    <RowDrawer title={cur && '子账户 ' + cur.uid} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== VIP 申请 ===================== */
const VIP_ST = { pending: ['待审核', 'wn'], approved: ['已通过', 'ok'], rejected: ['已驳回', 'bad'] }
export function VipApplications() {
  const f = useFilters({ status: 'pending' }); const [q, pager] = usePaged('vip/applications', f.applied, 50, 'pageSize'); const [cur, setCur] = useState(null)
  return <><PageHead title="VIP 申请" desc="用户提交的 VIP 升级申请（本批只读；审核操作下一批上线）" />
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pager={pager} emptyText="暂无申请" filters={<Filters f={f} fields={[['uid', 'UID'], ['status', '', [['', '全部状态'], ...Object.entries(VIP_ST).map(([k, v]) => [k, v[0]])]]]} />}
      cols={[{ t: '申请号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '联系渠道', k: 'channel' }, { t: '联系方式', k: 'contact' }, { t: '状态', render: r => { const s = VIP_ST[r.status] || [r.status]; return <Tag k={s[1]}>{s[0]}</Tag> } },
        { t: '审核人', k: 'reviewer' }, { t: '说明', k: 'reason' }, { t: '申请时间', render: r => ft(r.createdAt) }, { t: '审核时间', render: r => ft(r.reviewedAt) }]} /></ApiState>
    <RowDrawer title={cur && 'VIP 申请 ' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 邀请关系 ===================== */
export function Referrals() {
  const [uid, setUid] = useState(''); const [q0, setQ0] = useState(''); const r = useApi(q0 ? 'referrals/' + q0 : null); const [page, setPage] = useState(1); const inv = useApi(q0 ? `referrals/${q0}/invitees` : null, { page, size: 50 })
  const go = (u) => { setUid(u); setQ0(u.trim()); setPage(1) }
  return <><PageHead title="邀请关系" desc="查询用户的邀请码、上级、直接邀请的下级与返佣累计（只读）" />
    <div className="card"><div className="filters"><input className="in" placeholder="UID" value={uid} onChange={e => setUid(e.target.value)} onKeyDown={e => e.key === 'Enter' && go(uid)} /><button className="btn p" onClick={() => go(uid)}>查询</button></div></div>
    {!q0 ? <div className="card pad" style={{ marginTop: 12 }}><Empty text="输入 UID 查询邀请关系" /></div> : <ApiState q={r}>{r.data && <>
      <div className="grid g4" style={{ margin: '12px 0' }}><Kpi l="邀请码" v={r.data.code || '-'} /><Kpi l="上级" v={r.data.inviter ? <a onClick={() => go(r.data.inviter)}>{r.data.inviter}</a> : '无'} />
        <Kpi l="直接邀请" v={r.data.invited} d={'返佣有效期至 ' + ft(r.data.referralUntil).slice(0, 10)} /><Kpi l="合伙人" v={r.data.partner ? (r.data.partner.status || '是') : '否'} /></div>
      <div className="grid g21"><ApiState q={inv}><DataTable rowKey="uid" rows={items(inv.data)} onRow={x => go(x.uid)} pager={<><span>共 {inv.data ? inv.data.total : 0} 人</span><span className="sp" /><b onClick={() => page > 1 && setPage(page - 1)}>‹</b><b onClick={() => inv.data && page * 50 < inv.data.total && setPage(page + 1)}>›</b></>}
        filters={<b>直接下级（点击行查看其下级）</b>} emptyText="没有直接下级" cols={[{ t: 'UID', k: 'uid' }, { t: 'KYC', render: x => 'L' + x.kycLevel }, { t: '注册', render: x => ft(x.registeredAt) }, { t: '有效用户', render: x => x.validAt ? <Tag k="ok">{ft(x.validAt).slice(0, 10)}</Tag> : <span className="muted">否</span> }, { t: '其下级数', r: 1, k: 'subInvited' }]} /></ApiState>
        <div className="card pad"><h3>返佣累计</h3>{Object.keys(r.data.commissionTotals || {}).length ? <dl className="kv">{Object.entries(r.data.commissionTotals).flatMap(([k, v]) => [<dt key={k}>{k}</dt>, <dd key={k + 'v'}>{dec(v)} USDT</dd>])}</dl> : <Empty text="暂无返佣" />}
          <div className="muted" style={{ marginTop: 8 }}>注册时间 {ft(r.data.registeredAt)}</div></div></div></>}</ApiState>}</>
}

/* ===================== 内部转账 ===================== */
export function InternalTransfers() {
  const f = useFilters(); const [q, pager] = usePaged('internal-transfers', f.applied, 50, 'pageSize'); const cfg = useApi('internal-transfer/config'); const [cur, setCur] = useState(null)
  return <><PageHead title="内部转账" desc="用户之间的站内转账记录（只读）。开关与限额在「系统设置 → 内部转账」修改" />
    <ApiState q={cfg}>{cfg.data && <div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="内部转账" v={cfg.data.enabled ? '开启' : '关闭'} up={cfg.data.enabled} /><Kpi l="每日限额 (USDT)" v={dec(cfg.data.dailyLimitUsdt, 0)} /></div>}</ApiState>
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} pager={pager} emptyText="暂无内部转账" filters={<Filters f={f} fields={[['uid', 'UID（转出或转入）'], ['asset', '币种'], ['status', '', [['', '全部状态'], ['completed', '已完成'], ['pending', '处理中'], ['failed', '失败']]]]} />}
      cols={[{ t: 'ID', k: 'id' }, { t: '时间', render: r => ft(r.createdAt) }, { t: '转出 UID', k: 'fromUid' }, { t: '转入 UID', k: 'toUid' }, { t: '币种', k: 'asset' }, { t: '数量', r: 1, num: 1, render: r => dec(r.amount) }, { t: '手续费', r: 1, render: r => dec(r.fee) },
        { t: '状态', render: r => <Tag k={r.status === 'completed' ? 'ok' : r.status === 'failed' ? 'bad' : 'in'}>{{ completed: '已完成', pending: '处理中', failed: '失败' }[r.status] || r.status}</Tag> }, { t: '失败原因', k: 'failReason' }]} /></ApiState>
    <RowDrawer title={cur && '内部转账 #' + cur.id} row={cur} onClose={() => setCur(null)} /></>
}

/* ===================== 用户卡券 ===================== */
const CP_ST = { issued: ['待激活', 'in'], active: ['已激活', 'ok'], expired: ['已过期', ''], revoked: ['已作废', 'bad'] }
export function UserCoupons() {
  const [uid, setUid] = useState(''); const [q0, setQ0] = useState(''); const q = useApi(q0 ? `users/${q0}/coupons` : null)
  return <><PageHead title="用户卡券" desc="查询单个用户持有的体验金 / 抵扣券（只读）。发放走「卡券」审批流程" />
    <div className="card"><div className="filters"><input className="in" placeholder="UID" value={uid} onChange={e => setUid(e.target.value)} onKeyDown={e => e.key === 'Enter' && setQ0(uid.trim())} /><button className="btn p" onClick={() => setQ0(uid.trim())}>查询</button></div></div>
    {!q0 ? <div className="card pad" style={{ marginTop: 12 }}><Empty text="输入 UID 查询卡券" /></div> : <ApiState q={q}><div style={{ marginTop: 12 }}><DataTable rows={items(q.data)} emptyText="该用户没有卡券"
      cols={[{ t: 'ID', k: 'id' }, { t: '名称', k: 'title' }, { t: '类型', render: r => r.kind === 'trial' ? '体验金' : r.kind === 'deduction' ? '抵扣券' : r.kind }, { t: '面额', r: 1, render: r => dec(r.amount) + ' ' + (r.asset || '') },
        { t: '状态', render: r => { const s = CP_ST[r.status] || [r.status]; return <Tag k={s[1]}>{s[0]}</Tag> } }, { t: '激活截止', render: r => ft(r.activateBy) }, { t: '有效天数', r: 1, k: 'validDays' }, { t: '发放时间', render: r => ft(r.createdAt) }]} /></div></ApiState>}</>
}
