/* 运营：积分活动 / 积分兑换 / 跟单交易员 / 返佣，数据来自 admin/v1 */
import { useState } from 'react'
import { ApiState, Confirm, DataTable, Drawer, Kpi, PageHead, Tabs, Tag } from '../ui.jsx'
import { api, ft, items, num, useApi } from '../api.js'
import { useQ } from './Trade.jsx'

const CP_ST = { upcoming: ['未开始', 'in'], active: ['进行中', 'ok'], ended: ['已结束', ''] }
const toTs = s => s ? Math.floor(new Date(s).getTime() / 1000) : 0

export function Campaigns() {
  const q = useApi('points/campaigns'); const [cur, setCur] = useState(null); const [add, setAdd] = useState(false); const [f, setF] = useState({ key: '', title: '', startsAt: '', endsAt: '' })
  const v = k => e => setF({ ...f, [k]: e.target.value })
  return <><PageHead title="积分活动" desc="积分赛季（交易量/充值/邀请计分，积分兑换体验金/抵扣券）"><button className="btn p" onClick={() => setAdd(true)}>+ 新建活动</button></PageHead>
    <ApiState q={q}><DataTable rows={items(q.data)} onRow={setCur} cols={[{ t: 'ID', k: 'id' }, { t: 'Key', k: 'key' }, { t: '名称', k: 'title', render: r => <b>{r.title}</b> },
      { t: '开始', render: r => ft(r.startsAt) }, { t: '结束', render: r => ft(r.endsAt) }, { t: '日交易量积分上限', k: 'dailyMaxVolumePoints' },
      { t: '状态', render: r => { const [l, k] = CP_ST[r.status] || [r.status, '']; return <Tag k={k}>{l}</Tag> } }]} /></ApiState>
    {cur && <Drawer title={`活动 · ${cur.title}`} onClose={() => setCur(null)}>{(() => { const ru = cur.rules || {}; return <>
      <dl className="kv"><dt>Key</dt><dd>{cur.key}</dd><dt>时间</dt><dd>{ft(cur.startsAt)} ~ {ft(cur.endsAt)}</dd><dt>充值计分</dt><dd>每 {ru.depositUnit} USDT 1 分，日上限 {ru.depositCap}</dd><dt>邀请计分</dt><dd>每人 {ru.invitePoints} 分，日上限 {ru.inviteCap}</dd></dl>
      <div className="sec">交易量档位</div><table className="t"><thead><tr><th className="r">交易量 ≥</th><th className="r">积分</th></tr></thead><tbody>{(ru.volumeTiers || []).map((t, i) => <tr key={i}><td className="r num">{num(t.volume, 0)}</td><td className="r">{t.points}</td></tr>)}</tbody></table>
      <div className="sec">兑换档位</div><table className="t"><thead><tr><th className="r">积分</th><th>类型</th><th className="r">面额</th></tr></thead><tbody>{(ru.redeem || []).map((t, i) => <tr key={i}><td className="r">{t.points}</td><td>{t.kind === 'trial' ? '体验金' : '抵扣券'}</td><td className="r num">{t.amount}</td></tr>)}</tbody></table></> })()}</Drawer>}
    {add && <Confirm title="新建积分活动" noReason onClose={() => setAdd(false)} onOk={async ({ totpCode }) => {
      if (!f.key.trim() || !f.title.trim() || !f.startsAt || !f.endsAt) throw new Error('请填写完整')
      await api('points/campaigns', { method: 'POST', body: { key: f.key.trim(), title: f.title.trim(), startsAt: toTs(f.startsAt), endsAt: toTs(f.endsAt), totpCode } }); q.reload(); return '已创建' }}>
      <div className="fr"><label>Key</label><input className="in" placeholder="如 s2-2026" value={f.key} onChange={v('key')} /></div><div className="fr"><label>名称</label><input className="in" value={f.title} onChange={v('title')} /></div>
      <div className="fr"><label>开始</label><input className="in" type="datetime-local" value={f.startsAt} onChange={v('startsAt')} /></div><div className="fr"><label>结束</label><input className="in" type="datetime-local" value={f.endsAt} onChange={v('endsAt')} /></div>
      <div className="muted">计分与兑换规则使用服务端默认配置。</div></Confirm>}</>
}

export function Points() {
  const [f, v] = useQ(); const q = useApi('points/redemptions', { ...f, size: 200 }); const c = useApi('points/campaigns')
  return <><PageHead title="积分兑换记录" desc="用户积分兑换体验金 / 抵扣券的流水" />
    <div className="card"><div className="filters"><input className="in" placeholder="UID" onChange={v('uid')} />
      <select className="sel" onChange={v('campaignId')}><option value="">全部活动</option>{items(c.data).map(x => <option key={x.id} value={x.id}>{x.title}</option>)}</select></div></div>
    <ApiState q={q}>{q.data && q.data.total > items(q.data).length && <div className="muted">共 {q.data.total} 条，显示最新 {items(q.data).length} 条</div>}
      <DataTable rows={items(q.data).map((r, i) => ({ ...r, id: r.couponId || i }))} cols={[{ t: '时间', render: r => ft(r.at) }, { t: 'UID', k: 'uid' }, { t: '活动 ID', k: 'campaignId' }, { t: '积分', r: 1, k: 'points' }, { t: '券 ID', k: 'couponId' }]} /></ApiState></>
}

const T_ST = { pending: ['待审核', 'wn'], active: ['已通过', 'ok'], rejected: ['已驳回', 'bad'], suspended: ['已暂停', ''] }
const tst = s => { const [l, k] = T_ST[s] || [s, '']; return <Tag k={k}>{l}</Tag> }
export function Copy() {
  const [t, setT] = useState('traders'); const tr = useApi(t === 'traders' ? 'copy/traders' : null, { size: 200 }); const ap = useApi(t === 'apps' ? 'copy/applications' : null, { status: 'pending', pageSize: 100 })
  const [act, setAct] = useState(null)
  return <><PageHead title="跟单交易员" desc="带单交易员、跟随人数、跟单本金与分润；交易员申请审核" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['traders', '交易员'], ['apps', '待审核申请']]} /></div>
    {t === 'traders' && <ApiState q={tr}><DataTable rowKey="uid" rows={items(tr.data)} cols={[{ t: 'UID', k: 'uid' }, { t: '昵称', k: 'nickname', render: r => <b>{r.nickname}</b> }, { t: '分润比例', r: 1, render: r => num(r.profitShare * 100, 1) + '%' },
      { t: '跟随人数', r: 1, k: 'followers' }, { t: '跟单本金', r: 1, num: 1, render: r => num(r.aum) }, { t: '累计分润', r: 1, num: 1, render: r => num(r.sharePaid) }, { t: '状态', render: r => tst(r.status) }]} /></ApiState>}
    {t === 'apps' && <ApiState q={ap}><DataTable rowKey="traderId" rows={items(ap.data)} cols={[{ t: 'UID', k: 'traderId' }, { t: '昵称', k: 'nickname' }, { t: '简介', k: 'bio' }, { t: '分润比例', r: 1, render: r => num(r.profitShare * 100, 1) + '%' },
      { t: '合约权益', r: 1, num: 1, render: r => num(r.equity) }, { t: '申请时间', render: r => ft(r.createdAt) },
      { t: '操作', render: r => <><a className="btn g" onClick={e => { e.stopPropagation(); setAct([r, 'approve']) }}>通过</a><a className="btn g" onClick={e => { e.stopPropagation(); setAct([r, 'reject']) }}>驳回</a></> }]} /></ApiState>}
    {act && <Confirm danger={act[1] === 'reject'} title={`${act[1] === 'approve' ? '通过' : '驳回'}交易员申请 · ${act[0].traderId}`} noCode noReason={act[1] === 'approve'} reasonLabel="驳回原因（展示给用户）"
      onClose={() => setAct(null)} onOk={async ({ reason }) => { await api(`copy/applications/${encodeURIComponent(act[0].traderId)}/${act[1]}`, { method: 'POST', body: { reason } }); ap.reload(); return '已处理' }} />}</>
}

const K_CN = { spot: '现货', futures: '合约', fut: '合约', partner: '合伙人', referral: '邀请' }
export function Rebate() {
  const [f, v] = useQ(); const q = useApi('rebate/commissions', { ...f, size: 200 }); const sum = (q.data && q.data.summary30d) || []
  return <><PageHead title="返佣记录" desc="邀请/合伙人返佣按日结算明细；近 30 日汇总" />
    <div className="grid g4">{sum.length ? sum.map(s => <Kpi key={s.kind} l={'近 30 日 · ' + (K_CN[s.kind] || s.kind)} v={num(s.amount)} hint={s.count != null ? s.count + ' 笔' : ''} />) : <Kpi l="近 30 日返佣" v="0" />}</div>
    <div className="card" style={{ marginTop: 12 }}><div className="filters"><input className="in" placeholder="受益人 UID" onChange={v('uid')} /><input className="in" placeholder="来源 UID" onChange={v('sourceUid')} />
      <input className="in" placeholder="类型（kind）" onChange={v('kind')} /></div></div>
    <ApiState q={q}><DataTable rows={items(q.data).map((r, i) => ({ ...r, id: `${r.day}-${r.uid}-${r.sourceUid}-${r.kind}-${i}` }))} cols={[{ t: '结算日', render: r => String(r.day).slice(0, 10) }, { t: '受益人', k: 'uid' }, { t: '来源用户', k: 'sourceUid' },
      { t: '类型', render: r => K_CN[r.kind] || r.kind }, { t: '基数', r: 1, num: 1, render: r => num(r.base) }, { t: '比例', r: 1, render: r => num(r.rate * 100, 2) + '%' }, { t: '返佣', r: 1, num: 1, render: r => num(r.amount, 4) }]} /></ApiState></>
}

export { Rewards, Content } from './Live.jsx'
