/* batch6（t84）：资金类配置双人审批、业务模块（launchpad/质押/NFT/期权/Web3 钱包/模拟交易/定投/IM）、
   App 版本、帮助中心与活动文案、App 推送。所有写操作需 Google 验证码 + 原因并写审计。 */
import { useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Kpi, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { api, ft, items, num, useApi } from '../api.js'

function usePg(path, query, size = 50) {
  const [page, setPage] = useState(1); const key = JSON.stringify(query); const [k0, setK0] = useState(key); if (k0 !== key) { setK0(key); setPage(1) }
  const q = useApi(path, { ...query, page, size }); const total = q.data ? (q.data.total ?? 0) : 0; const pages = Math.max(1, Math.ceil(total / size))
  const pager = <><span>共 {total} 条 · 第 {page}/{pages} 页</span><span className="sp" /><b onClick={() => page > 1 && setPage(page - 1)} style={{ opacity: page > 1 ? 1 : .4 }}>‹ 上一页</b>
    <b onClick={() => page < pages && setPage(page + 1)} style={{ opacity: page < pages ? 1 : .4 }}>下一页 ›</b></>
  return [q, pager]
}
const useF = () => { const [f, setF] = useState({}); return [f, k => e => setF({ ...f, [k]: e.target.value.trim() })] }
const pend = x => x && x.pendingApproval ? `已提交双人审批 #${x.changeId}，待另一名管理员审批` : '已保存'
const J = v => v == null ? '-' : <pre style={{ margin: 0, maxHeight: 260, overflow: 'auto', fontSize: 12 }}>{JSON.stringify(v, null, 2)}</pre>
const ST = { pending: ['待审批', 'warn'], applying: ['执行中', 'warn'], applied: ['已生效', 'ok'], failed: ['执行失败', 'bad'], rejected: ['已驳回', ''] }

/** 通用列表：cols 为 DataTable 列；filters 为 [参数, 占位] */
function BizTable({ path, cols, filters = [], onRow, extra }) {
  const [f, v] = useF(); const [q, pager] = usePg(path, f)
  return <><div className="card"><div className="filters">{filters.map(([k, ph]) => <input key={k} className="in" placeholder={ph} onChange={v(k)} />)}{extra}</div></div>
    <ApiState q={q}><DataTable rows={items(q.data).map((r, i) => ({ ...r, __k: i }))} cols={cols} pager={pager} onRow={onRow} rowKey="__k" /></ApiState></>
}
const withKey = rows => rows

// ---------------- 资金类配置审批 ----------------
export function FundChanges() {
  const { me } = useApp() || {}; const [st, setSt] = useState('pending'); const q = useApi('fund-changes', { status: st === 'all' ? '' : st })
  const [cur, setCur] = useState(null); const [act, setAct] = useState(null)
  const canApprove = ((me && me.perms) || []).includes('config:approve')
  return <><PageHead title="资金类配置审批" desc="质押产品、Launchpad 项目/结算/派发/取消、做市参数与库存基线、闪兑配置：提交后须另一名有 config:approve 权限的管理员审批才生效（提交人不能审批自己）；停用做市可直接生效。" />
    <Tabs value={st} onChange={setSt} items={[['pending', '待审批'], ['applied', '已生效'], ['failed', '失败'], ['rejected', '已驳回'], ['all', '全部']]} />
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} cols={[{ t: '#', k: 'id' }, { t: '类型', render: r => r.targetName || r.target }, { t: '对象', k: 'key' },
      { t: '状态', render: r => { const [n, k] = ST[r.status] || [r.status, '']; return <Tag k={k}>{n}</Tag> } }, { t: '提交人', k: 'submittedBy' }, { t: '原因', k: 'reason' },
      { t: '审批记录', render: r => <span className="muted">{r.decisions || '-'}</span> }, { t: '提交时间', render: r => ft(r.createdAt) }, { t: '生效', render: r => ft(r.appliedAt) }]} /></ApiState>
    {cur && <Drawer width={720} title={`变更单 #${cur.id} · ${cur.targetName || cur.target}`} onClose={() => setCur(null)}
      footer={cur.status === 'pending' && (canApprove ? <><button className="btn" onClick={() => setAct('reject')}>驳回</button><button className="btn p" onClick={() => setAct('approve')}>审批通过并执行</button></> : <span className="muted">需要 config:approve 权限才能审批</span>)}>
      <dl className="kv"><dt>对象</dt><dd>{cur.key}</dd><dt>提交人</dt><dd>{cur.submittedBy}</dd><dt>原因</dt><dd>{cur.reason}</dd>{cur.error && <><dt>错误</dt><dd className="down">{cur.error}</dd></>}</dl>
      <div className="sec">变更对比</div><div className="grid g2"><div className="card pad down">{J(cur.before)}</div><div className="card pad up">{J(cur.after)}</div></div></Drawer>}
    {act && <Confirm danger={act === 'approve'} title={act === 'approve' ? `审批通过 #${cur.id}（立即执行）` : `驳回 #${cur.id}`} reasonLabel={act === 'approve' ? '审批意见' : '驳回原因'} onClose={() => setAct(null)}
      onOk={async ({ totpCode, comment }) => { await api(`fund-changes/${cur.id}/${act}`, { method: 'POST', body: { totpCode, comment } }); setCur(null); q.reload(); return act === 'approve' ? '已执行' : '已驳回' }} />}</>
}

/** 提交资金类变更（JSON 编辑） */
function SubmitChange({ target, keyv, body, title, onClose, onDone }) {
  const [txt, setTxt] = useState(JSON.stringify(body || {}, null, 2)); let parsed = null; try { parsed = JSON.parse(txt) } catch { /* invalid */ }
  return <Confirm danger title={title} onClose={onClose} note="提交后进入「资金类配置审批」，需另一名管理员审批才会生效。"
    onOk={async ({ totpCode, reason }) => { if (!parsed && body) throw new Error('JSON 格式不正确'); const x = await api('fund-changes', { method: 'POST', body: { target, key: keyv, body: body ? parsed : undefined, totpCode, reason } }); onDone && onDone(); return pend(x) }}>
    {body && <textarea className="in" style={{ width: '100%', minHeight: 220, fontFamily: 'monospace', fontSize: 12 }} value={txt} onChange={e => setTxt(e.target.value)} />}
    {body && !parsed && <div className="banner bad">JSON 格式不正确</div>}</Confirm>
}

// ---------------- 业务模块 ----------------
const LP_ST = { upcoming: '未开始', open: '认购中', computing: '计算中', settled: '已结算', distributed: '已派发', cancelled: '已取消' }
export function Launchpad() {
  const [t, setT] = useState('projects'); const [cur, setCur] = useState(null); const [sub, setSub] = useState(null); const [n, setN] = useState(0)
  const blank = { id: '', symbol: '', name: '', baseAsset: 'USDT', price: '', totalSupply: '', minSub: '0', maxSub: null, startAt: Date.now() + 86400000, endAt: Date.now() + 3 * 86400000, settleAt: Date.now() + 3 * 86400000 + 3600000, distributeAt: Date.now() + 3 * 86400000 + 7200000, logo: '', description: '', rulesMd: '', sort: 0 }
  const edit = r => ({ id: r.id, symbol: r.symbol, name: r.name, baseAsset: r.baseAsset, price: r.price, totalSupply: r.totalSupply, minSub: r.minSub, maxSub: r.maxSub, startAt: +new Date(r.startAt), endAt: +new Date(r.endAt), settleAt: +new Date(r.settleAt), distributeAt: +new Date(r.distributeAt), logo: r.logo, description: r.description, rulesMd: r.rulesMd, sort: r.sort })
  return <><PageHead title="Launchpad / IEO" desc="项目创建修改、取消（退款）、强制结算、强制派发均为资金操作：走双人审批。已开始或已有认购的项目不能再改参数。">
    <button className="btn p" onClick={() => setSub({ target: 'launchpad_project', key: '', body: blank, title: '新建 Launchpad 项目（id 必须与 key 一致）' })}>新建项目</button></PageHead>
    <Tabs value={t} onChange={setT} items={[['projects', '项目'], ['subscriptions', '认购记录']]} />
    {t === 'projects' && <BizTable key={n} path="biz/launchpad/projects" filters={[['status', '状态 upcoming/open/…']]} onRow={setCur} cols={[{ t: 'ID', k: 'id' }, { t: '代币', k: 'symbol' }, { t: '名称', k: 'name' },
      { t: '价格', r: 1, render: r => r.price + ' ' + r.baseAsset }, { t: '总量', r: 1, num: 1, render: r => num(r.totalSupply) }, { t: '已募', r: 1, num: 1, render: r => num(r.raised) }, { t: '认购数', r: 1, k: 'subscriptions' },
      { t: '状态', render: r => <Tag k={r.status === 'open' ? 'ok' : ''}>{LP_ST[r.status] || r.status}</Tag> }, { t: '开始', render: r => ft(r.startAt) }, { t: '结束', render: r => ft(r.endAt) }]} />}
    {t === 'subscriptions' && <BizTable path="biz/launchpad/subscriptions" filters={[['uid', 'UID'], ['project', '项目 ID'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '项目', k: 'projectId' },
      { t: '认购', r: 1, num: 1, render: r => num(r.amount) }, { t: '分配', r: 1, num: 1, render: r => num(r.resultAmount) }, { t: '退款', r: 1, num: 1, render: r => num(r.refundAmount) }, { t: '状态', k: 'status' }, { t: '时间', render: r => ft(r.lockedAt) }]} />}
    {cur && <Drawer width={640} title={`${cur.symbol} · ${cur.name}`} onClose={() => setCur(null)} footer={<>
      <button className="btn" disabled={cur.status !== 'upcoming' || +cur.subscriptions > 0} onClick={() => setSub({ target: 'launchpad_project', key: cur.id, body: edit(cur), title: '修改项目 ' + cur.id })}>修改参数</button>
      <button className="btn d" disabled={['cancelled', 'distributed'].includes(cur.status)} onClick={() => setSub({ target: 'launchpad_cancel', key: cur.id, title: '取消项目并退款 ' + cur.id })}>取消项目</button>
      <button className="btn" onClick={() => setSub({ target: 'launchpad_settle', key: cur.id, title: '强制结算 ' + cur.id })}>强制结算</button>
      <button className="btn" onClick={() => setSub({ target: 'launchpad_distribute', key: cur.id, title: '强制派发 ' + cur.id })}>强制派发</button></>}>
      {J(cur)}</Drawer>}
    {sub && <SubmitChange {...sub} keyv={sub.key || (sub.body && sub.body.id)} onClose={() => setSub(null)} onDone={() => { setCur(null); setN(n + 1) }} />}</>
}

export function Staking() {
  const [t, setT] = useState('products'); const [sub, setSub] = useState(null)
  const blank = { id: '', asset: '', chain: '', validator: '', apr: '0.05', unbondingDays: 7, minAmount: '1', maxAmount: null, totalQuota: null, commission: '0', logoUrl: '', status: 'offline', hot: false, sort: 0 }
  const edit = r => ({ id: r.id, asset: r.asset, chain: r.chain, validator: r.validator, apr: r.apr, unbondingDays: r.unbondingDays, minAmount: r.minAmount, maxAmount: r.maxAmount, totalQuota: r.totalQuota, commission: r.commission, logoUrl: r.logoUrl, status: r.status, hot: r.hot, sort: r.sort })
  return <><PageHead title="质押（Staking）" desc="产品上架、修改 APR/额度、上下线、删除走双人审批；持仓只读。APR 为小数（0.05 = 5%）。">
    <button className="btn p" onClick={() => setSub({ target: 'staking_product', body: blank, title: '新建质押产品' })}>新建产品</button></PageHead>
    <Tabs value={t} onChange={setT} items={[['products', '产品'], ['positions', '持仓']]} />
    {t === 'products' && <BizTable path="biz/staking/products" filters={[['asset', '币种'], ['status', 'online/offline']]} onRow={r => setSub({ target: 'staking_product', body: edit(r), title: '修改质押产品 ' + r.id })} cols={[{ t: 'ID', k: 'id' }, { t: '币种', k: 'asset' }, { t: '链', k: 'chain' }, { t: '验证人', k: 'validator' },
      { t: 'APR', r: 1, render: r => (r.apr * 100).toFixed(2) + '%' }, { t: '解锁天数', r: 1, k: 'unbondingDays' }, { t: '已售/额度', r: 1, render: r => num(r.sold) + ' / ' + (r.totalQuota ? num(r.totalQuota) : '∞') },
      { t: '状态', render: r => <Tag k={r.status === 'online' ? 'ok' : ''}>{r.status === 'online' ? '上线' : '下线'}</Tag> }, { t: '', render: r => <a onClick={e => { e.stopPropagation(); setSub({ target: 'staking_product_delete', key: r.id, title: '删除质押产品 ' + r.id }) }}>删除</a> }]} />}
    {t === 'positions' && <BizTable path="biz/staking/positions" filters={[['uid', 'UID'], ['product', '产品 ID'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '产品', k: 'productId' },
      { t: '数量', r: 1, num: 1, render: r => num(r.amount, 6) + ' ' + r.asset }, { t: 'APR', r: 1, render: r => (r.apr * 100).toFixed(2) + '%' }, { t: '待领收益', r: 1, num: 1, render: r => num(r.pendingRewards, 6) },
      { t: '已领', r: 1, num: 1, render: r => num(r.claimedRewards, 6) }, { t: '状态', k: 'status' }, { t: '开始', render: r => ft(r.startedAt) }, { t: '解锁', render: r => ft(r.unbondAt) }]} />}
    {sub && <SubmitChange {...sub} keyv={sub.key || (sub.body && sub.body.id)} onClose={() => setSub(null)} />}</>
}

export function NFT() {
  const [t, setT] = useState('collections'); const [cur, setCur] = useState(null); const [n, setN] = useState(0)
  return <><PageHead title="NFT 市场" desc="合集列表、成交记录；合集可隐藏/恢复（需验证码与原因）。" />
    <Tabs value={t} onChange={setT} items={[['collections', '合集'], ['trades', '成交']]} />
    {t === 'collections' && <BizTable key={n} path="biz/nft/collections" filters={[['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: 'slug', k: 'slug' }, { t: '名称', k: 'name' }, { t: '分类', k: 'category' },
      { t: '版税', r: 1, render: r => (r.royaltyBps / 100) + '%' }, { t: '藏品', r: 1, k: 'itemCount' }, { t: '地板价', r: 1, render: r => num(r.floorPrice, 4) + ' ' + r.payAsset }, { t: '总成交', r: 1, render: r => num(r.volumeTotal) },
      { t: '状态', render: r => <Tag k={r.status === 'active' ? 'ok' : ''}>{r.status}</Tag> }, { t: '', render: r => <a onClick={() => setCur(r)}>{r.status === 'hidden' ? '恢复' : '隐藏'}</a> }]} />}
    {t === 'trades' && <BizTable path="biz/nft/trades" filters={[['uid', '买家 UID'], ['collection', '合集 ID']]} cols={[{ t: 'ID', k: 'id' }, { t: '藏品', k: 'itemId' }, { t: '合集', k: 'collectionId' }, { t: '卖家', k: 'seller' }, { t: '买家', k: 'buyer' },
      { t: '价格', r: 1, render: r => num(r.price, 4) + ' ' + r.asset }, { t: '版税', r: 1, render: r => num(r.royalty, 4) }, { t: '来源', k: 'source' }, { t: '时间', render: r => ft(r.createdAt) }]} />}
    {cur && <Confirm title={`${cur.status === 'hidden' ? '恢复' : '隐藏'}合集 ${cur.name}`} onClose={() => setCur(null)}
      onOk={async ({ totpCode, reason }) => { await api(`biz/nft/collections/${encodeURIComponent(cur.slug)}`, { method: 'PATCH', body: { status: cur.status === 'hidden' ? 'active' : 'hidden', totpCode, reason } }); setN(n + 1); return '已更新' }} />}</>
}

export function Options() {
  const [t, setT] = useState('instruments'); const cfg = useApi('biz/options/config')
  return <><PageHead title="期权" desc="合约、持仓、委托只读。options 服务的定价参数来自服务环境变量，opt_config 表当前未被服务读取，因此不提供编辑。" />
    <ApiState q={cfg}><div className="grid g4" style={{ marginBottom: 12 }}>{items(cfg.data).slice(0, 1).map(c => [<Kpi key="a" l="无风险利率" v={c.riskFreeRate} />, <Kpi key="b" l="手续费率" v={c.feeRate} />, <Kpi key="c" l="最小权利金" v={c.minPremium} />, <Kpi key="d" l="单笔最大张数" v={c.maxOrderQty} />])}</div></ApiState>
    <Tabs value={t} onChange={setT} items={[['instruments', '合约'], ['positions', '持仓'], ['orders', '委托']]} />
    {t === 'instruments' && <BizTable path="biz/options/instruments" filters={[['underlying', '标的'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: '名称', k: 'name' }, { t: '标的', k: 'underlying' }, { t: '类型', render: r => r.optionType === 'call' ? '看涨' : '看跌' },
      { t: '行权价', r: 1, render: r => num(r.strike) }, { t: '到期', render: r => ft(r.expiry) }, { t: '状态', k: 'status' }, { t: '结算价', r: 1, render: r => num(r.settlementPrice) }]} />}
    {t === 'positions' && <BizTable path="biz/options/positions" filters={[['uid', 'UID']]} cols={[{ t: 'UID', k: 'uid' }, { t: '合约', k: 'instrumentId' }, { t: '数量', r: 1, k: 'qty' }, { t: '均价', r: 1, render: r => num(r.avgPrice, 4) },
      { t: '已实现', r: 1, render: r => num(r.realizedPnl, 4) }, { t: '保证金', r: 1, render: r => num(r.margin, 4) }, { t: '已结算', render: r => r.settled ? '是' : '否' }, { t: '更新', render: r => ft(r.updatedAt) }]} />}
    {t === 'orders' && <BizTable path="biz/options/orders" filters={[['uid', 'UID'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '合约', k: 'instrumentId' }, { t: '方向', k: 'side' }, { t: '价格', r: 1, k: 'price' },
      { t: '数量', r: 1, k: 'qty' }, { t: '成交', r: 1, k: 'filledQty' }, { t: '状态', k: 'status' }, { t: '时间', render: r => ft(r.createdAt) }]} />}</>
}

export function Web3Wallet() {
  const [t, setT] = useState('wallets')
  return <><PageHead title="Web3 钱包" desc="非托管钱包：后台只看钱包状态与链上交易，不读取、不展示任何助记词或密钥。" />
    <Tabs value={t} onChange={setT} items={[['wallets', '钱包'], ['transactions', '链上交易']]} />
    {t === 'wallets' && <BizTable path="biz/web3/wallets" filters={[['uid', 'UID']]} cols={[{ t: 'UID', k: 'uid' }, { t: 'PIN 版本', k: 'pinVersion' }, { t: 'PIN 错误次数', k: 'pinFailures' }, { t: '锁定至', render: r => ft(r.lockedUntil) },
      { t: '已备份', render: r => r.backupAt ? ft(r.backupAt) : <Tag k="warn">未备份</Tag> }, { t: '创建', render: r => ft(r.createdAt) }]} />}
    {t === 'transactions' && <BizTable path="biz/web3/transactions" filters={[['uid', 'UID'], ['chain', '链'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '链', k: 'chain' }, { t: '类型', k: 'kind' },
      { t: '金额', r: 1, render: r => num(r.value, 6) + ' ' + (r.token || '') }, { t: 'To', render: r => <span className="mono">{(r.to || '').slice(0, 10)}…</span> }, { t: 'Tx', render: r => <span className="mono">{(r.txHash || '').slice(0, 12)}…</span> }, { t: '状态', k: 'status' }, { t: '时间', render: r => ft(r.createdAt) }]} />}</>
}

export function Paper() {
  return <><PageHead title="模拟交易" desc="模拟合约账户（虚拟 USDX，不涉及真实资金），只读。" />
    <BizTable path="biz/paper/accounts" filters={[['uid', 'UID']]} cols={[{ t: 'UID', k: 'uid' }, { t: '余额', r: 1, render: r => num(r.balance) }, { t: '冻结', r: 1, render: r => num(r.frozen) }, { t: '持仓数', r: 1, k: 'positions' },
      { t: '持仓模式', k: 'positionMode' }, { t: '重置代数', k: 'generation' }, { t: '更新', render: r => ft(r.updatedAt) }]} /></>
}

const DCA_ST = { active: ['运行中', 'ok'], paused: ['已暂停', 'warn'], stopped: ['已停止', ''], completed: ['已完成', ''] }
export function DCA() {
  const [t, setT] = useState('plans')
  return <><PageHead title="定投（DCA）" desc="用户定投计划与执行记录，只读。" />
    <Tabs value={t} onChange={setT} items={[['plans', '计划'], ['executions', '执行记录']]} />
    {t === 'plans' && <BizTable path="biz/dca/plans" filters={[['uid', 'UID'], ['symbol', '交易对'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' }, { t: '方向', k: 'side' },
      { t: '每期', r: 1, render: r => num(r.amountPerRun) }, { t: '周期', r: 1, render: r => Math.round(r.intervalSeconds / 3600) + 'h' }, { t: '已执行', r: 1, k: 'totalRuns' }, { t: '累计投入', r: 1, render: r => num(r.totalQuote) },
      { t: '状态', render: r => { const [n, k] = DCA_ST[r.status] || [r.status, '']; return <Tag k={k}>{n}</Tag> } }, { t: '下次', render: r => ft(r.nextRunAt) }]} />}
    {t === 'executions' && <BizTable path="biz/dca/executions" filters={[['plan', '计划 ID'], ['uid', 'UID'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: '计划', k: 'planId' }, { t: 'UID', k: 'uid' }, { t: '交易对', k: 'symbol' },
      { t: '请求', r: 1, render: r => num(r.requestedQuote) }, { t: '成交', r: 1, render: r => num(r.filledBase, 6) }, { t: '均价', r: 1, render: r => num(r.avgPrice, 4) }, { t: '状态', k: 'status' }, { t: '错误', k: 'error' }, { t: '时间', render: r => ft(r.createdAt) }]} />}</>
}

export function IMGroups() {
  const [t, setT] = useState('groups'); const [cur, setCur] = useState(null); const [n, setN] = useState(0)
  return <><PageHead title="IM 群组与红包" desc="群组可全员禁言/解除（需验证码与原因）；红包只读（过期退款由 IM 服务自动处理）。" />
    <Tabs value={t} onChange={setT} items={[['groups', '群组'], ['redpackets', '红包']]} />
    {t === 'groups' && <BizTable key={n} path="biz/im/groups" filters={[['owner', '群主 UID']]} cols={[{ t: 'ID', k: 'id' }, { t: '群名', k: 'name' }, { t: '群主', k: 'owner' }, { t: '人数', r: 1, render: r => r.members + '/' + r.maxMembers },
      { t: '全员禁言', render: r => <Tag k={r.muteAll ? 'warn' : ''}>{r.muteAll ? '是' : '否'}</Tag> }, { t: '公告', render: r => <span className="muted">{(r.announcement || '').slice(0, 30)}</span> }, { t: '创建', render: r => ft(r.createdAt) },
      { t: '', render: r => <a onClick={() => setCur(r)}>{r.muteAll ? '解除禁言' : '全员禁言'}</a> }]} />}
    {t === 'redpackets' && <BizTable path="biz/im/redpackets" filters={[['sender', '发送者 UID'], ['group', '群 ID'], ['status', '状态']]} cols={[{ t: 'ID', k: 'id' }, { t: '群', k: 'groupId' }, { t: '发送者', k: 'sender' },
      { t: '金额', r: 1, render: r => num(r.total, 6) + ' ' + r.asset }, { t: '份数', r: 1, render: r => (r.shares - r.remainingShares) + '/' + r.shares }, { t: '剩余', r: 1, render: r => num(r.remaining, 6) }, { t: '类型', k: 'mode' },
      { t: '状态', k: 'status' }, { t: '过期', render: r => ft(r.expiresAt) }, { t: '退款', render: r => ft(r.refundedAt) }]} />}
    {cur && <Confirm title={`${cur.muteAll ? '解除' : '开启'}全员禁言：${cur.name}`} onClose={() => setCur(null)}
      onOk={async ({ totpCode, reason }) => { await api(`biz/im/groups/${cur.id}/mute`, { method: 'POST', body: { muteAll: !cur.muteAll, totpCode, reason } }); setN(n + 1); return '已更新' }} />}</>
}

// ---------------- App 版本 ----------------
export function AppVersions() {
  const q = useApi('app/versions'); const [cur, setCur] = useState(null); const [d, setD] = useState({}); const [ok, setOk] = useState(false)
  const rows = ['ios', 'android'].map(p => items(q.data).find(x => x.platform === p) || { platform: p, latestVersion: '', latestBuild: 0, minVersion: '', downloadUrl: '', releaseNotes: '', enabled: false, __new: true })
  const F = [['latestVersion', '最新版本（如 1.4.0）'], ['latestBuild', '构建号'], ['minVersion', '最低可用版本（低于则强制更新，留空不强制）'], ['downloadUrl', '下载 / 商店地址（https）']]
  return <><PageHead title="App 版本与升级" desc={'iOS / Android 升级配置。' + ((q.data && q.data.note) || '')} />
    <ApiState q={q}><DataTable rows={rows} rowKey="platform" onRow={r => { setCur(r); setD({ ...r }) }} cols={[{ t: '平台', render: r => r.platform === 'ios' ? 'iOS' : 'Android' }, { t: '最新版本', render: r => r.latestVersion || '-' },
      { t: '构建号', k: 'latestBuild' }, { t: '强制更新低于', render: r => r.minVersion || '不强制' }, { t: '下载地址', render: r => <span className="muted">{r.downloadUrl || '-'}</span> },
      { t: '启用', render: r => <Tag k={r.enabled ? 'ok' : ''}>{r.__new ? '未配置' : r.enabled ? '是' : '否'}</Tag> }, { t: '更新', render: r => <span className="muted">{ft(r.updatedAt)} {r.updatedBy || ''}</span> }]} /></ApiState>
    {cur && <Drawer title={(cur.platform === 'ios' ? 'iOS' : 'Android') + ' 升级配置'} onClose={() => setCur(null)} footer={<button className="btn p" onClick={() => setOk(true)}>保存</button>}>
      {F.map(([k, l]) => <div className="fr" key={k}><label>{l}</label><input className="in" value={d[k] ?? ''} onChange={e => setD({ ...d, [k]: k === 'latestBuild' ? (parseInt(e.target.value) || 0) : e.target.value })} /></div>)}
      <div className="fr"><label>更新说明</label><textarea className="in" style={{ minHeight: 120 }} value={d.releaseNotes || ''} onChange={e => setD({ ...d, releaseNotes: e.target.value })} /></div>
      <div className="fr"><label>启用</label><input type="checkbox" checked={!!d.enabled} onChange={e => setD({ ...d, enabled: e.target.checked })} /></div></Drawer>}
    {ok && <Confirm title="保存升级配置" rows={[['平台', cur.platform], ['最新', d.latestVersion], ['强制更新低于', d.minVersion || '不强制']]} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { await api(`app/versions/${cur.platform}`, { method: 'PUT', body: { latestVersion: d.latestVersion, latestBuild: d.latestBuild, minVersion: d.minVersion, downloadUrl: d.downloadUrl, releaseNotes: d.releaseNotes, enabled: d.enabled, totpCode, reason } }); setCur(null); q.reload(); return '已保存' }} />}</>
}

// ---------------- 帮助中心 / 活动文案 ----------------
export function ContentDocs() {
  const q = useApi('content/docs'); const [cur, setCur] = useState(null); const [ok, setOk] = useState(false)
  const open = r => setCur({ key: r.key, title: r.title || '', format: r.format || (r.key === 'help_center' ? 'json' : 'markdown'), body: r.body || '', published: !!r.published, isNew: !!r.isNew })
  let bad = cur && cur.format === 'json' && (() => { try { JSON.parse(cur.body); return false } catch { return true } })()
  return <><PageHead title="帮助中心与活动文案" desc={'key：help_center（JSON）、activity:<slug>、page:<slug>（Markdown）。' + ((q.data && q.data.note) || '')}>
    <button className="btn p" onClick={() => open({ key: 'activity:', isNew: true })}>新建文档</button></PageHead>
    <ApiState q={q}><DataTable rows={items(q.data)} rowKey="key" onRow={open} cols={[{ t: 'Key', k: 'key' }, { t: '标题', k: 'title' }, { t: '格式', k: 'format' }, { t: '已发布', render: r => <Tag k={r.published ? 'ok' : ''}>{r.published ? '是' : '否'}</Tag> },
      { t: '大小', r: 1, render: r => (r.body || '').length }, { t: '更新', render: r => <span className="muted">{ft(r.updatedAt)} {r.updatedBy}</span> }]} emptyText="还没有文档：客户端目前使用 compat 内置的帮助中心与活动文案" /></ApiState>
    {cur && <Drawer width={820} title={'编辑 ' + (cur.key || '新文档')} onClose={() => setCur(null)} footer={<button className="btn p" disabled={bad || !cur.key || !cur.body.trim()} onClick={() => setOk(true)}>保存</button>}>
      <div className="fr"><label>Key</label><input className="in" disabled={!cur.isNew} value={cur.key} onChange={e => setCur({ ...cur, key: e.target.value.trim() })} /></div>
      <div className="fr"><label>标题</label><input className="in" value={cur.title} onChange={e => setCur({ ...cur, title: e.target.value })} /></div>
      <div className="fr"><label>格式</label><select className="sel" value={cur.format} onChange={e => setCur({ ...cur, format: e.target.value })}><option value="markdown">Markdown</option><option value="json">JSON</option></select></div>
      <div className="fr"><label>发布</label><input type="checkbox" checked={cur.published} onChange={e => setCur({ ...cur, published: e.target.checked })} /></div>
      <textarea className="in" style={{ width: '100%', minHeight: 420, fontFamily: 'monospace', fontSize: 12 }} value={cur.body} onChange={e => setCur({ ...cur, body: e.target.value })} />
      {bad && <div className="banner bad">JSON 格式不正确</div>}</Drawer>}
    {ok && <Confirm title={'保存文档 ' + cur.key} rows={[['发布', cur.published ? '是' : '否（草稿）'], ['大小', cur.body.length + ' 字符']]} onClose={() => setOk(false)}
      onOk={async ({ totpCode, reason }) => { await api(`content/docs/${encodeURIComponent(cur.key)}`, { method: 'PUT', body: { title: cur.title, format: cur.format, body: cur.body, published: cur.published, totpCode, reason } }); setCur(null); q.reload(); return '已保存' }} />}</>
}

// ---------------- App 推送 ----------------
const PJ_ST = { no_credentials: ['缺推送凭证·仅记录', 'warn'], queued: ['已排队', ''], sent: ['已发送', 'ok'], failed: ['失败', 'bad'] }
export function AppPush() {
  const dv = useApi('push/devices'); const jobs = useApi('push/jobs'); const [nw, setNw] = useState(null)
  const d = dv.data || {}; const sm = d.summary || {}; const cr = d.credentials || {}
  return <><PageHead title="App 推送" desc="设备来自 compat 已登记的推送 token（只读）；推送任务全部记录。未配置 APNs / FCM 凭证时任务状态为「缺推送凭证」，不会实际下发。">
    <button className="btn p" onClick={() => setNw({ title: '', body: '', uid: '', platform: 'all' })}>新建推送任务</button></PageHead>
    <ApiState q={dv}><div className="grid g4" style={{ marginBottom: 12 }}><Kpi l="已登记设备" v={d.total ?? 0} d={`iOS ${sm.ios || 0} · Android ${sm.android || 0}`} />
      <Kpi l="有推送 token" v={(sm.ios_with_token || 0) + (sm.android_with_token || 0)} d={`iOS ${sm.ios_with_token || 0} · Android ${sm.android_with_token || 0}`} />
      <Kpi l="APNs 凭证" v={cr.apns ? '已配置' : '缺失'} up={!!cr.apns} /><Kpi l="FCM 凭证" v={cr.fcm ? '已配置' : '缺失'} up={!!cr.fcm} /></div>
      <DataTable rows={d.items || []} rowKey="deviceId" cols={[{ t: '设备', k: 'deviceId' }, { t: 'UID', k: 'uid' }, { t: '平台', k: 'platform' }, { t: '通道', k: 'provider' }, { t: '机型', k: 'model' }, { t: '版本', k: 'appVersion' },
        { t: 'Token', render: r => <span className="mono">{r.token || '-'}</span> }, { t: '更新', render: r => ft(r.updatedAt) }]} emptyText="还没有设备登记推送（App 调用 user_device 后出现）" /></ApiState>
    <h3 style={{ margin: '16px 0 8px' }}>推送任务</h3>
    <ApiState q={jobs}><DataTable rows={items(jobs.data)} cols={[{ t: '#', k: 'id' }, { t: '标题', k: 'title' }, { t: '目标', k: 'target' }, { t: '平台', k: 'platform' }, { t: '设备数', r: 1, k: 'deviceCount' },
      { t: '状态', render: r => { const [n, k] = PJ_ST[r.status] || [r.status, '']; return <Tag k={k}>{n}</Tag> } }, { t: '说明', render: r => <span className="muted">{r.note}</span> }, { t: '创建', render: r => <span className="muted">{ft(r.createdAt)} {r.createdBy}</span> }]} /></ApiState>
    {nw && <Confirm title="新建推送任务" onClose={() => setNw(null)} rows={[['目标', nw.uid ? 'UID ' + nw.uid : '全部设备'], ['平台', nw.platform]]}
      onOk={async ({ totpCode, reason }) => { const x = await api('push/jobs', { method: 'POST', body: { ...nw, totpCode, reason } }); jobs.reload(); return `任务 #${x.id}：${(PJ_ST[x.status] || [x.status])[0]}（${x.deviceCount} 台设备）` }}>
      <div className="fr"><label>标题</label><input className="in" value={nw.title} onChange={e => setNw({ ...nw, title: e.target.value })} /></div>
      <div className="fr"><label>内容</label><textarea className="in" value={nw.body} onChange={e => setNw({ ...nw, body: e.target.value })} /></div>
      <div className="fr"><label>UID（留空=全部）</label><input className="in" value={nw.uid} onChange={e => setNw({ ...nw, uid: e.target.value.trim() })} /></div>
      <div className="fr"><label>平台</label><select className="sel" value={nw.platform} onChange={e => setNw({ ...nw, platform: e.target.value })}><option value="all">全部</option><option value="ios">iOS</option><option value="android">Android</option></select></div></Confirm>}</>
}
export { withKey }
