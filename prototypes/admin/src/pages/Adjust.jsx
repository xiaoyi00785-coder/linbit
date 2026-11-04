/* 人工调账：发起（金额/币种/账户真实输入）、待复核列表、复核/驳回、阈值设置。
   ≥ 阈值（USDT 计价，无法估值按需复核）须另一名管理员复核，发起人不能复核自己的单。 */
import { useMemo, useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Modal, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { api, ft, items, num, useApi } from '../api.js'

const ST = { pending_approval: ['待复核', 'wn'], applying: ['入账中', 'in'], applied: ['已入账', 'ok'], rejected: ['已驳回', 'bad'], failed: ['失败', 'bad'] }
const ACCT = { spot: '现货账户', funding: '资金账户' }
const st = s => { const [t, k] = ST[s] || [s, '']; return <Tag k={k}>{t}</Tag> }
const newRef = () => 'adj-' + (crypto.randomUUID ? crypto.randomUUID() : Date.now() + '-' + Math.random().toString(36).slice(2))
const AMT = /^\d+(\.\d{1,8})?$/

function NewAdjust({ uid0, onClose, onDone, threshold }) {
  const nets = useApi('wallet/networks')
  const assets = useMemo(() => [...new Set(items(nets.data).map(n => n.asset))].sort(), [nets.data])
  const [f, setF] = useState({ uid: uid0 || '', account: 'spot', asset: 'USDT', dir: 'credit', amount: '' })
  const [ref] = useState(newRef); const [ok, setOk] = useState(false)
  const bal = useApi(f.uid.trim() ? `users/${encodeURIComponent(f.uid.trim())}/balances` : null)
  const set = k => e => setF({ ...f, [k]: e.target.value })
  const valid = f.uid.trim() && f.asset && AMT.test(f.amount.trim()) && +f.amount > 0
  const signed = (f.dir === 'debit' ? '-' : '') + f.amount.trim()
  const cur = (items(bal.data).length ? items(bal.data) : (bal.data && bal.data.balances) || []).find(b => b.asset === f.asset && (!b.account || b.account === f.account))
  return <><Modal title="新建调账单" onClose={onClose} footer={<><button className="btn" onClick={onClose}>取消</button><button className="btn p" disabled={!valid} onClick={() => setOk(true)}>下一步</button></>}>
    <div className="fr"><label>UID</label><input className="in" value={f.uid} onChange={set('uid')} placeholder="用户 UID" /></div>
    <div className="fr"><label>账户</label><select className="sel" value={f.account} onChange={set('account')}>{Object.entries(ACCT).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></div>
    <div className="fr"><label>币种</label><select className="sel" value={f.asset} onChange={set('asset')}>{(assets.length ? assets : ['USDT']).map(a => <option key={a}>{a}</option>)}</select></div>
    <div className="fr"><label>方向</label><select className="sel" value={f.dir} onChange={set('dir')}><option value="credit">增加（贷记用户）</option><option value="debit">扣减（借记用户）</option></select></div>
    <div className="fr"><label>金额</label><input className="in num" value={f.amount} onChange={set('amount')} placeholder="> 0，最多 8 位小数" inputMode="decimal" /></div>
    {cur && <div className="muted">当前可用：{cur.free} {f.asset}</div>}
    <div className="muted" style={{ marginTop: 8 }}>≥ {num(threshold)} USDT（等值）需另一名管理员复核后入账；以下 2FA 后立即入账。扣减不能使余额为负。仅支持现货/资金账户（合约账户请先划转）。</div></Modal>
    {ok && <Confirm danger title={`确认调账 · UID ${f.uid.trim()}`} reasonLabel="调账原因" rows={[['账户', ACCT[f.account]], ['币种', f.asset], ['金额', signed + ' ' + f.asset], ['幂等键', ref]]}
      onClose={() => setOk(false)} onOk={async ({ totpCode, reason }) => {
        const r = await api('adjustments', { method: 'POST', body: { uid: f.uid.trim(), asset: f.asset, account: f.account, amount: signed, ref, reason, totpCode } })
        onDone(); onClose(); const a = r && r.adjustment; return a && a.status === 'pending_approval' ? '已提交，待复核' : a && a.status === 'applied' ? '已入账' : '已提交' }} />}</>
}

function Detail({ x, onClose, onDone }) {
  const { me } = useApp(); const [act, setAct] = useState(null)
  const self = me && me.user === x.createdByName
  return <><Drawer title={`调账单 #${x.id}`} onClose={onClose} footer={x.status === 'pending_approval' ? <>
    {self && <span className="muted" style={{ marginRight: 8 }}>发起人不能复核自己的调账单</span>}
    <button className="btn d" disabled={self} onClick={() => setAct('reject')}>驳回</button><button className="btn p" disabled={self} onClick={() => setAct('approve')}>复核通过并入账</button></> : null}>
    <dl className="kv">{[['状态', st(x.status)], ['UID', x.uid], ['账户', ACCT[x.account] || x.account], ['金额', <span className={x.direction === 'debit' ? 'down' : 'up'}>{x.amount} {x.asset}</span>],
      ['USDT 估值', x.usdtValue ? num(x.usdtValue) : '无法估值（按需复核）'], ['需复核', x.needsSecond ? '是' : '否'], ['原因', x.reason], ['幂等键', x.ref],
      ['发起人', `${x.createdByName} · ${ft(x.createdAt)} · ${x.createdIp || '-'}`], ['复核人', x.reviewedByName ? `${x.reviewedByName} · ${ft(x.reviewedAt)}` : '-'], ['复核意见', x.reviewNote || '-'],
      ['调整前可用', x.freeBefore ?? '-'], ['调整后可用', x.freeAfter ?? '-'], ['入账时间', ft(x.appliedAt)], ['错误', x.error || '-']].flatMap(([a, b]) => [<dt key={a}>{a}</dt>, <dd key={a + 'v'}>{b}</dd>])}</dl></Drawer>
    {act && <Confirm danger={act === 'reject'} title={(act === 'approve' ? '复核通过 · ' : '驳回 · ') + `#${x.id}`} reasonLabel={act === 'approve' ? '复核意见' : '驳回原因'} okText={act === 'approve' ? '通过并入账' : '驳回'}
      rows={[['UID', x.uid], ['金额', `${x.amount} ${x.asset}`], ['账户', ACCT[x.account]]]} onClose={() => setAct(null)}
      onOk={async ({ totpCode, reason }) => { await api(`adjustments/${x.id}/${act}`, { method: 'POST', body: { totpCode, note: reason } }); onDone(); onClose(); return act === 'approve' ? '已入账' : '已驳回' }} />}</>
}

export function Adjust() {
  const { q } = useApp(); const [tab, setTab] = useState('pending_approval'); const [cur, setCur] = useState(null); const [mk, setMk] = useState(!!(q && q.uid)); const [cfg, setCfg] = useState(false)
  const [uid, setUid] = useState(''); const [th, setTh] = useState('')
  const r = useApi('adjustments', { status: tab === 'all' ? '' : tab, uid: uid.trim(), size: 200 })
  const threshold = (r.data && r.data.thresholdUsdt) || '1000'
  return <><PageHead title="人工调账" desc={`借贷双边分录（system:admin_adjust）· ≥ ${num(threshold)} USDT 需另一名管理员复核 · 全程 2FA + 审计 · 对账中单独标记为无链上来源的调账`}>
    <button className="btn" onClick={() => { setTh(threshold); setCfg(true) }}>复核阈值</button><button className="btn p" onClick={() => setMk(true)}>新建调账</button></PageHead>
    <div className="card"><Tabs value={tab} onChange={setTab} items={[['pending_approval', '待复核'], ['applied', '已入账'], ['rejected', '已驳回'], ['failed', '失败'], ['all', '全部']]} /></div>
    <ApiState q={r}><DataTable rows={items(r.data)} onRow={setCur} filters={<input className="in" placeholder="按 UID 筛选" value={uid} onChange={e => setUid(e.target.value)} />}
      cols={[{ t: '单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '账户', render: x => ACCT[x.account] || x.account }, { t: '币种', k: 'asset' },
        { t: '金额', r: 1, num: 1, render: x => <span className={x.direction === 'debit' ? 'down' : 'up'}>{x.amount}</span> }, { t: 'USDT 估值', r: 1, render: x => x.usdtValue ? num(x.usdtValue) : '-' },
        { t: '原因', k: 'reason' }, { t: '发起人', k: 'createdByName' }, { t: '复核人', render: x => x.reviewedByName || '-' }, { t: '时间', render: x => ft(x.createdAt) }, { t: '状态', render: x => st(x.status) }]} /></ApiState>
    {mk && <NewAdjust uid0={q && q.uid} threshold={threshold} onClose={() => setMk(false)} onDone={r.reload} />}
    {cur && <Detail x={cur} onClose={() => setCur(null)} onDone={r.reload} />}
    {cfg && <Confirm title="调账复核阈值（USDT 等值）" rows={[['当前', num(threshold) + ' USDT']]} onClose={() => setCfg(false)}
      onOk={async ({ totpCode, reason }) => { await api('adjustments/config', { method: 'PUT', body: { secondReviewUsdt: th.trim(), reason, totpCode } }); r.reload(); return '已保存' }}>
      <div className="fr"><label>新阈值</label><input className="in num" value={th} onChange={e => setTh(e.target.value)} /></div></Confirm>}</>
}
