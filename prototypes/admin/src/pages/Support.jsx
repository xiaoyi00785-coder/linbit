/* 客服工单：按状态/分类/UID/处理人筛选，查看对话（图片附件鉴权拉取），回复（回复后站内通知用户）、改状态、指派。
   用户内容已在服务端去活链接，这里一律纯文本渲染。 */
import { useEffect, useState } from 'react'
import { ApiState, DataTable, Drawer, PageHead, Tabs, Tag, useApp } from '../ui.jsx'
import { API_BASE, api, errText, ft, getToken, items, useApi } from '../api.js'

const ST = { open: ['待处理', 'wn'], replied: ['已回复', 'ok'], closed: ['已关闭', ''] }
const CAT = { account: '账户', deposit_withdraw: '充值与提现', trade: '交易', kyc: '身份认证', security: '账户安全', other: '其他' }
const st = s => { const [t, k] = ST[s] || [s, '']; return <Tag k={k}>{t}</Tag> }
const pre = { whiteSpace: 'pre-wrap', wordBreak: 'break-word', lineHeight: 1.6 }

function Img({ id }) {
  const [u, setU] = useState(null)
  useEffect(() => {
    let url = null, dead = false
    fetch(API_BASE + 'support/attachments/' + encodeURIComponent(id), { headers: { Authorization: 'Bearer ' + (getToken() || '') } })
      .then(r => r.ok ? r.blob() : null).then(b => { if (b && !dead) { url = URL.createObjectURL(b); setU(url) } }).catch(() => {})
    return () => { dead = true; if (url) URL.revokeObjectURL(url) }
  }, [id])
  return u ? <a href={u} target="_blank" rel="noopener noreferrer"><img src={u} alt="附件" style={{ maxWidth: 180, maxHeight: 140, borderRadius: 4 }} /></a> : <span className="muted">附件加载中…</span>
}

function Detail({ id, onClose, onDone }) {
  const { me, say } = useApp(); const canReply = me && (me.perms || []).includes('support:reply')
  const t = useApi(`support/tickets/${id}`); const who = useApi(canReply ? 'support/assignees' : null)
  const [body, setBody] = useState(''); const [next, setNext] = useState('replied'); const [busy, setBusy] = useState(false)
  const x = t.data
  const run = async (fn, ok) => { setBusy(true); try { await fn(); t.reload(); onDone(); say(ok) } catch (e) { say('失败：' + errText(e)) } finally { setBusy(false) } }
  const send = () => run(async () => { await api(`support/tickets/${id}/messages`, { method: 'POST', body: { content: body.trim(), status: next } }); setBody('') }, '已回复，用户将收到站内通知')
  const patch = b => run(() => api(`support/tickets/${id}`, { method: 'PATCH', body: b }), '已更新')
  return <Drawer title={`工单 #${id}`} onClose={onClose} footer={x && canReply ? <>
    <select className="sel" value={next} onChange={e => setNext(e.target.value)}><option value="replied">回复后：已回复</option><option value="closed">回复并关闭</option></select>
    <button className="btn p" disabled={busy || !body.trim()} onClick={send}>发送回复</button></> : null}>
    <ApiState q={t}>{x && <>
      <dl className="kv">{[['状态', st(x.status)], ['UID', x.uid], ['分类', CAT[x.category] || x.category], ['标题', <span style={pre}>{x.subject}</span>],
        ['处理人', x.assigneeName || '未指派'], ['创建', ft(x.createdAt)], ['最近回复', ft(x.lastReplyAt)]].flatMap(([a, b]) => [<dt key={a}>{a}</dt>, <dd key={a + 'v'}>{b}</dd>])}</dl>
      {canReply && <div style={{ display: 'flex', gap: 8, margin: '12px 0', flexWrap: 'wrap' }}>
        <select className="sel" value={x.status} disabled={busy} onChange={e => patch({ status: e.target.value })}>{Object.entries(ST).map(([k, [n]]) => <option key={k} value={k}>{n}</option>)}</select>
        <select className="sel" value={x.assigneeId || 0} disabled={busy} onChange={e => patch({ assigneeId: +e.target.value })}>
          <option value={0}>未指派</option>{items(who.data).map(a => <option key={a.id} value={a.id}>{a.username}</option>)}</select></div>}
      <div style={{ display: 'grid', gap: 10 }}>{(x.messages || []).map(m => <div key={m.id} className="card" style={m.author === 'support' ? { borderLeft: '3px solid var(--p, #3b82f6)' } : undefined}>
        <div className="muted" style={{ marginBottom: 4 }}>{m.author === 'support' ? `客服 ${m.authorName || ''}` : `用户 ${x.uid}`} · {ft(m.createdAt)}</div>
        <div style={pre}>{m.body}</div>
        {(m.attachments || []).length > 0 && <div style={{ display: 'flex', gap: 8, marginTop: 6, flexWrap: 'wrap' }}>{m.attachments.map(a => <Img key={a.id} id={a.id} />)}</div>}</div>)}</div>
      {canReply && x.status !== 'closed' && <textarea className="in" rows={5} style={{ width: '100%', marginTop: 12 }} maxLength={5000} value={body} onChange={e => setBody(e.target.value)} placeholder="回复内容（纯文本，用户端不会识别链接）" />}
      {canReply && x.status === 'closed' && <div className="muted" style={{ marginTop: 12 }}>工单已关闭；如需继续回复请先把状态改回“待处理”。</div>}
    </>}</ApiState></Drawer>
}

export function Support() {
  const [tab, setTab] = useState('open'); const [cat, setCat] = useState(''); const [uid, setUid] = useState(''); const [asg, setAsg] = useState(''); const [cur, setCur] = useState(null)
  const r = useApi('support/tickets', { status: tab === 'all' ? '' : tab, category: cat, uid: uid.trim(), assigneeId: asg, pageSize: 100 })
  return <><PageHead title="客服工单" desc="用户站内提交的工单 · 回复后通过站内消息通知用户 · 回复/改状态/指派均记审计" />
    <div className="card"><Tabs value={tab} onChange={setTab} items={[['open', '待处理'], ['replied', '已回复'], ['closed', '已关闭'], ['all', '全部']]} /></div>
    <ApiState q={r}><DataTable rows={items(r.data)} onRow={x => setCur(x.id)} filters={<>
      <select className="sel" value={cat} onChange={e => setCat(e.target.value)}><option value="">全部分类</option>{Object.entries(CAT).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select>
      <input className="in" placeholder="按 UID 筛选" value={uid} onChange={e => setUid(e.target.value)} />
      <select className="sel" value={asg} onChange={e => setAsg(e.target.value)}><option value="">全部处理人</option><option value="me">指派给我</option><option value="-1">未指派</option></select></>}
      cols={[{ t: '单号', k: 'id' }, { t: 'UID', k: 'uid' }, { t: '分类', render: x => CAT[x.category] || x.category }, { t: '标题', render: x => <span style={pre}>{x.subject}</span> },
        { t: '处理人', render: x => x.assigneeName || '-' }, { t: '最近回复', render: x => (x.lastReplyBy === 'user' ? '用户 · ' : '客服 · ') + ft(x.lastReplyAt) },
        { t: '创建', render: x => ft(x.createdAt) }, { t: '状态', render: x => st(x.status) }]} /></ApiState>
    {cur && <Detail id={cur} onClose={() => setCur(null)} onDone={r.reload} />}</>
}
