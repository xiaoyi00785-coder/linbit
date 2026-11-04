"use client";
/** 站内客服工单：我的工单列表 / 提交工单（?new=1）/ 工单对话（?id=）。内容一律纯文本渲染（不识别链接）。 */
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";
import { Footer } from "@/legacy/App";
import { fmtLocal } from "@/lib/time";
import {
  ATTACH_MAX, CATEGORIES, STATUS, attachmentBlobURL, catName, closeTicket, createTicket, getTicket, listTickets, replyTicket, ticketErr, uploadAttachment,
  type Ticket, type TicketAttachment, type TicketCategory,
} from "@/lib/supportTickets";

const fmt = (s?: string) => (s ? fmtLocal(s, false) : "--");
const LOGIN = (back: string) => "/login?redirect=" + encodeURIComponent(back);
const TABS: [string, string][] = [["", "全部"], ["open", "待处理"], ["replied", "客服已回复"], ["closed", "已关闭"]];

function Shell({ title, sub, children }: { title: string; sub?: string; children: ReactNode }) {
  return <main className="light tk"><div className="ff-wrap"><div className="ff-hd"><h1>{title}</h1>{sub && <p>{sub}</p>}</div>{children}</div><Footer light /></main>;
}
function Empty({ t, children }: { t: string; children?: ReactNode }) { return <div className="ff-empty"><div className="ff-empty-i" aria-hidden="true"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg></div><div>{t}</div>{children}</div>; }
function StatusTag({ s }: { s?: string }) { const [n, c] = STATUS[s || ""] || [s || "--", ""]; return <span className={"ff-tag" + (c ? " " + c : "")}>{n}</span>; }

/** 纯文本：React 文本节点 + pre-wrap，不 linkify；服务端已将链接去活。 */
function Plain({ text }: { text: string }) { return <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", lineHeight: 1.7 }} data-testid="tk-body">{text}</div>; }

function Img({ a }: { a: TicketAttachment }) {
  const [u, setU] = useState<string | null | false>(null);
  useEffect(() => { let url: string | null = null; let live = true; attachmentBlobURL(a.id).then((x) => { url = x; if (live) setU(x || false); else if (x) URL.revokeObjectURL(x); }); return () => { live = false; if (url) URL.revokeObjectURL(url); }; }, [a.id]);
  return u ? <a href={u} target="_blank" rel="noopener noreferrer" aria-label="查看附件大图"><img src={u} alt="附件" className="tk-img" /></a>
    : <span className="ff-mut">{u === false ? "图片加载失败" : "图片加载中…"}</span>;
}

function Attach({ ids, setIds, onBusy }: { ids: TicketAttachment[]; setIds: (f: (x: TicketAttachment[]) => TicketAttachment[]) => void; onBusy: (b: boolean) => void }) {
  const inp = useRef<HTMLInputElement>(null); const [busy, setBusyRaw] = useState(false);
  const setBusy = (b: boolean) => { setBusyRaw(b); onBusy(b); };
  const pick = async (fs: FileList | null) => {
    if (!fs) return;
    setBusy(true);
    try { for (const f of Array.from(fs).slice(0, ATTACH_MAX - ids.length)) {
      const r = await uploadAttachment(f);
      if (r.ok && r.data) { const d = r.data; setIds((x) => [...x, d]); } else toast(ticketErr(r), "err");
    } } finally { setBusy(false); } if (inp.current) inp.current.value = "";
  };
  return <div className="ff-f"><span>图片附件（选填，最多 {ATTACH_MAX} 张，JPG/PNG/WEBP，单张 ≤ 5 MB）</span>
    <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
      {ids.map((a, i) => <span key={a.id} className="ff-tag tk-att">图片 {i + 1} · {(a.size / 1024).toFixed(0)} KB <button type="button" aria-label={`移除图片 ${i + 1}`} onClick={() => setIds((x) => x.filter((y) => y.id !== a.id))}>×</button></span>)}
      {ids.length < ATTACH_MAX && <button type="button" className="ff-btn" disabled={busy} onClick={() => inp.current?.click()}>{busy ? "上传中…" : "添加图片"}</button>}
      <input ref={inp} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={(e) => pick(e.target.files)} data-testid="tk-file" />
    </div></div>;
}

function NewTicket({ onDone }: { onDone: (t: Ticket) => void }) {
  const [cat, setCat] = useState<TicketCategory | "">(""); const [sub, setSub] = useState(""); const [body, setBody] = useState("");
  const [att, setAtt] = useState<TicketAttachment[]>([]); const [busy, setBusy] = useState(false); const [upl, setUpl] = useState(false);
  const [tried, setTried] = useState(false);
  const ok = !!cat && sub.trim().length > 0 && sub.length <= 120 && body.trim().length > 0 && body.length <= 5000;
  const miss = !cat ? "请选择问题分类" : !sub.trim() ? "请填写标题" : !body.trim() ? "请填写问题描述" : "";
  const go = async () => {
    setTried(true);
    if (!ok || !cat || busy || upl) return; setBusy(true);
    const r = await createTicket({ category: cat, subject: sub.trim(), content: body.trim(), attachments: att.map((a) => a.id) });
    setBusy(false);
    if (r.ok && r.data) { toast("工单已提交，客服回复后会通过站内消息通知您"); onDone(r.data); } else toast(ticketErr(r), "err");
  };
  return <div className="ff-card tk-card" data-testid="tk-new">
    <label className="ff-f"><span>问题分类</span><select className="tk-in" value={cat} onChange={(e) => setCat(e.target.value as TicketCategory)}><option value="">请选择</option>{CATEGORIES.map(([k, n]) => <option key={k} value={k}>{n}</option>)}</select></label>
    <label className="ff-f"><span>标题 <em className="tk-cnt">{sub.length}/120</em></span><input className="tk-in" value={sub} maxLength={120} onChange={(e) => setSub(e.target.value)} placeholder="一句话描述问题" /></label>
    <label className="ff-f"><span>问题描述 <em className="tk-cnt">{body.length}/5000</em></span><textarea className="tk-in" value={body} maxLength={5000} rows={6} onChange={(e) => setBody(e.target.value)} placeholder="请描述问题经过，涉及充提时可附上币种、网络与交易哈希。请勿填写密码、验证码或助记词。" /></label>
    <Attach ids={att} setIds={setAtt} onBusy={setUpl} />
    <p className="ff-mut">客服不会索要您的密码、验证码或私钥；工单中的链接不会被打开。</p>
    {tried && miss && <p className="tk-err" role="alert">{miss}</p>}
    <div className="ff-mact"><Link className="ff-btn" href="/support/tickets">取消</Link><button type="button" className="ff-btn pri" disabled={busy || upl} aria-busy={busy} onClick={go} data-testid="tk-submit">{busy ? "提交中…" : upl ? "等待图片上传…" : "提交工单"}</button></div>
  </div>;
}

function Thread({ id }: { id: string }) {
  const [t, setT] = useState<Ticket | null>(null); const [err, setErr] = useState("");
  const [body, setBody] = useState(""); const [att, setAtt] = useState<TicketAttachment[]>([]); const [busy, setBusy] = useState(false); const [upl, setUpl] = useState(false);
  const [ask, setAsk] = useState(false); const [closing, setClosing] = useState(false);
  const load = useCallback(() => { setErr(""); getTicket(id).then((r) => (r.ok && r.data ? setT(r.data) : setErr(ticketErr(r)))); }, [id]);
  useEffect(() => { load(); }, [load]);
  if (err) return <Empty t={err}><div className="tk-row"><button type="button" className="ff-btn" onClick={load}>重试</button><Link className="ff-btn" href="/support/tickets">返回我的工单</Link></div></Empty>;
  if (!t) return <div className="ff-skel" aria-label="加载中" />;
  const send = async () => {
    if (busy || upl || !body.trim()) return; setBusy(true); const r = await replyTicket(id, body.trim(), att.map((a) => a.id)); setBusy(false);
    if (r.ok && r.data) { setT(r.data); setBody(""); setAtt([]); } else toast(ticketErr(r), "err");
  };
  const close = async () => { setClosing(true); const r = await closeTicket(id); setClosing(false); setAsk(false); if (r.ok && r.data) { setT(r.data); toast("工单已关闭"); } else toast(ticketErr(r), "err"); };
  return <>
    <div className="ff-bar"><span><Link href="/support/tickets">← 我的工单</Link> · {catName(t.category)} · <StatusTag s={t.status} /></span>
      {t.status !== "closed" && <button type="button" className="ff-btn" onClick={() => setAsk(true)} data-testid="tk-close">关闭工单</button>}</div>
    {ask && <div className="ff-mask" onClick={() => !closing && setAsk(false)}><div className="ff-modal" role="dialog" aria-modal="true" aria-labelledby="tk-close-h" onClick={(e) => e.stopPropagation()}>
      <h3 id="tk-close-h">关闭工单<button type="button" className="ff-x" aria-label="关闭" onClick={() => setAsk(false)}>×</button></h3>
      <p className="ff-mut">关闭后将无法继续回复此工单，如有新问题需重新提交。</p>
      <div className="ff-mact"><button type="button" className="ff-btn" onClick={() => setAsk(false)} disabled={closing}>取消</button><button type="button" className="ff-btn pri" onClick={close} disabled={closing} data-testid="tk-close-ok">{closing ? "关闭中…" : "确认关闭"}</button></div>
    </div></div>}
    <h3 className="tk-sub">{t.subject}</h3>
    <div className="tk-msgs">
      {(t.messages || []).map((m) => <div key={m.id} className={"ff-card tk-card" + (m.author === "support" ? " tk-sup" : "")}>
        <div className="ff-mut" style={{ marginBottom: 6 }}>{m.author === "support" ? "客服" : "我"} · {fmt(m.createdAt)}</div>
        <Plain text={m.body} />
        {m.attachments.length > 0 && <div style={{ display: "flex", gap: 8, marginTop: 8, flexWrap: "wrap" }}>{m.attachments.map((a) => <Img key={a.id} a={a} />)}</div>}
      </div>)}
    </div>
    {t.status === "closed" ? <p className="ff-mut" style={{ marginTop: 16 }}>工单已关闭。如有新问题，请 <Link href="/support/tickets?new=1">重新提交工单</Link>。</p>
      : <div className="ff-card tk-card" style={{ marginTop: 16 }}>
        <label className="ff-f"><span>补充回复 <em className="tk-cnt">{body.length}/5000</em></span><textarea className="tk-in" value={body} maxLength={5000} rows={4} onChange={(e) => setBody(e.target.value)} /></label>
        <Attach ids={att} setIds={setAtt} onBusy={setUpl} />
        <div className="ff-mact"><button type="button" className="ff-btn pri" disabled={!body.trim() || busy || upl} aria-busy={busy} onClick={send} data-testid="tk-send">{busy ? "发送中…" : upl ? "等待图片上传…" : "发送"}</button></div>
      </div>}
  </>;
}

export function SupportTickets() {
  const authed = useAuthStore((s) => s.isAuthed);
  const sp = useSearchParams(); const r = useRouter();
  const id = sp.get("id"); const isNew = sp.get("new") === "1";
  const [page, setPage] = useState(1); const [st, setSt] = useState(""); const [nonce, setNonce] = useState(0);
  const [list, setList] = useState<{ items: Ticket[]; total: number } | null>(null); const [err, setErr] = useState("");
  useEffect(() => {
    if (!authed || id || isNew) return;
    let live = true; setList(null); setErr("");
    listTickets(page, st).then((x) => { if (live) (x.ok && x.data ? setList({ items: x.data.items, total: x.data.total }) : setErr(ticketErr(x))); });
    return () => { live = false; };
  }, [authed, id, isNew, page, st, nonce]);
  const here = id ? "/support/tickets?id=" + encodeURIComponent(id) : isNew ? "/support/tickets?new=1" : "/support/tickets";
  return <Shell title={isNew ? "提交工单" : id ? "工单详情" : "我的工单"} sub="遇到问题可在线提交工单，客服回复后会通过站内消息通知您。">
    {!authed ? <Empty t="登录后提交或查看工单"><Link className="ff-btn pri" href={LOGIN(here)}>登录</Link></Empty>
      : id ? <Thread id={id} />
      : isNew ? <NewTicket onDone={(t) => r.replace("/support/tickets?id=" + encodeURIComponent(t.id))} />
      : <>
        <div className="ff-bar"><div className="ff-tabs tk-tabs" role="tablist">{TABS.map(([k, n]) => <button key={k} type="button" role="tab" aria-selected={st === k} className={st === k ? "on" : ""} onClick={() => { setSt(k); setPage(1); }}>{n}</button>)}</div><Link className="ff-btn pri" href="/support/tickets?new=1" data-testid="tk-new-btn">提交工单</Link></div>
        {err ? <Empty t={err}><button type="button" className="ff-btn" onClick={() => setNonce((n) => n + 1)}>重试</button></Empty> : !list ? <div className="ff-skel" aria-label="加载中" /> : <>{list.total > 0 && <p className="ff-mut tk-total">共 {list.total} 个工单</p>}
        {!list.items.length ? <Empty t={st ? "该状态下暂无工单" : "暂无工单"}>{!st && <Link className="ff-btn pri" href="/support/tickets?new=1">提交工单</Link>}</Empty> : <table className="ff-t"><thead><tr><th>标题</th><th>分类</th><th>状态</th><th>最近更新</th></tr></thead>
          <tbody>{list.items.map((t) => <tr key={t.id} className="tk-tr" onClick={() => r.push("/support/tickets?id=" + encodeURIComponent(t.id))}><td><Link href={"/support/tickets?id=" + encodeURIComponent(t.id)} onClick={(e) => e.stopPropagation()}>{t.unread && <span className="ff-tag ok">新回复</span>} {t.subject}</Link></td>
            <td>{catName(t.category)}</td><td><StatusTag s={t.status} /></td><td className="ff-mut">{fmt(t.lastReplyAt)}</td></tr>)}</tbody></table>}
        {list.total > 20 && <div className="ff-bar"><button type="button" className="ff-btn" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><span className="ff-mut">{page} / {Math.ceil(list.total / 20)}</span><button type="button" className="ff-btn" disabled={page * 20 >= list.total} onClick={() => setPage(page + 1)}>下一页</button></div>}
      </>}</>}
  </Shell>;
}
