"use client";
/** 站内客服工单：我的工单列表 / 提交工单（?new=1）/ 工单对话（?id=）。内容一律纯文本渲染（不识别链接）。 */
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";
import { Footer } from "@/legacy/App";
import {
  ATTACH_MAX, CATEGORIES, STATUS, attachmentBlobURL, catName, closeTicket, createTicket, getTicket, listTickets, replyTicket, ticketErr, uploadAttachment,
  type Ticket, type TicketAttachment, type TicketCategory,
} from "@/lib/supportTickets";

const fmt = (s?: string) => (s ? new Date(s).toLocaleString("zh-CN", { hour12: false }) : "--");

function Shell({ title, sub, children }: { title: string; sub?: string; children: ReactNode }) {
  return <main className="light"><div className="ff-wrap"><div className="ff-hd"><h1>{title}</h1>{sub && <p>{sub}</p>}</div>{children}</div><Footer light /></main>;
}
function Empty({ t, children }: { t: string; children?: ReactNode }) { return <div className="ff-empty"><div className="ff-empty-i">◌</div><div>{t}</div>{children}</div>; }
function StatusTag({ s }: { s?: string }) { const [n, c] = STATUS[s || ""] || [s || "--", ""]; return <span className={"ff-tag" + (c ? " " + c : "")}>{n}</span>; }

/** 纯文本：React 文本节点 + pre-wrap，不 linkify；服务端已将链接去活。 */
function Plain({ text }: { text: string }) { return <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", lineHeight: 1.7 }} data-testid="tk-body">{text}</div>; }

function Img({ a }: { a: TicketAttachment }) {
  const [u, setU] = useState<string | null>(null);
  useEffect(() => { let url: string | null = null; attachmentBlobURL(a.id).then((x) => { url = x; setU(x); }); return () => { if (url) URL.revokeObjectURL(url); }; }, [a.id]);
  return u ? <a href={u} target="_blank" rel="noopener noreferrer"><img src={u} alt="附件" style={{ maxWidth: 160, maxHeight: 120, borderRadius: 6, objectFit: "cover" }} /></a>
    : <span className="ff-mut">图片加载中…</span>;
}

function Attach({ ids, setIds }: { ids: TicketAttachment[]; setIds: (f: (x: TicketAttachment[]) => TicketAttachment[]) => void }) {
  const inp = useRef<HTMLInputElement>(null); const [busy, setBusy] = useState(false);
  const pick = async (fs: FileList | null) => {
    if (!fs) return;
    setBusy(true);
    for (const f of Array.from(fs).slice(0, ATTACH_MAX - ids.length)) {
      const r = await uploadAttachment(f);
      if (r.ok && r.data) { const d = r.data; setIds((x) => [...x, d]); } else toast(ticketErr(r), "err");
    }
    setBusy(false); if (inp.current) inp.current.value = "";
  };
  return <div className="ff-f"><span>图片附件（选填，最多 {ATTACH_MAX} 张，JPG/PNG/WEBP，单张 ≤ 5 MB）</span>
    <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
      {ids.map((a) => <span key={a.id} className="ff-tag">{a.id.slice(-10)} <button aria-label="移除附件" onClick={() => setIds((x) => x.filter((y) => y.id !== a.id))}>×</button></span>)}
      {ids.length < ATTACH_MAX && <button className="ff-btn" disabled={busy} onClick={() => inp.current?.click()}>{busy ? "上传中…" : "添加图片"}</button>}
      <input ref={inp} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={(e) => pick(e.target.files)} data-testid="tk-file" />
    </div></div>;
}

function NewTicket({ onDone }: { onDone: (t: Ticket) => void }) {
  const [cat, setCat] = useState<TicketCategory | "">(""); const [sub, setSub] = useState(""); const [body, setBody] = useState("");
  const [att, setAtt] = useState<TicketAttachment[]>([]); const [busy, setBusy] = useState(false);
  const ok = !!cat && sub.trim().length > 0 && sub.length <= 120 && body.trim().length > 0 && body.length <= 5000;
  const go = async () => {
    if (!ok || !cat) return; setBusy(true);
    const r = await createTicket({ category: cat, subject: sub.trim(), content: body.trim(), attachments: att.map((a) => a.id) });
    setBusy(false);
    if (r.ok && r.data) { toast("工单已提交，客服回复后会通过站内消息通知您"); onDone(r.data); } else toast(ticketErr(r), "err");
  };
  return <div className="ff-card" data-testid="tk-new">
    <label className="ff-f"><span>问题分类</span><select value={cat} onChange={(e) => setCat(e.target.value as TicketCategory)}><option value="">请选择</option>{CATEGORIES.map(([k, n]) => <option key={k} value={k}>{n}</option>)}</select></label>
    <label className="ff-f"><span>标题</span><input value={sub} maxLength={120} onChange={(e) => setSub(e.target.value)} placeholder="一句话描述问题" /></label>
    <label className="ff-f"><span>问题描述</span><textarea value={body} maxLength={5000} rows={6} onChange={(e) => setBody(e.target.value)} placeholder="请描述问题经过，涉及充提时可附上币种、网络与交易哈希。请勿填写密码、验证码或助记词。" /></label>
    <Attach ids={att} setIds={setAtt} />
    <p className="ff-mut">客服不会索要您的密码、验证码或私钥；工单中的链接不会被打开。</p>
    <div className="ff-mact"><Link className="ff-btn" href="/support/tickets">取消</Link><button className="ff-btn pri" disabled={!ok || busy} onClick={go}>{busy ? "提交中…" : "提交工单"}</button></div>
  </div>;
}

function Thread({ id }: { id: string }) {
  const [t, setT] = useState<Ticket | null>(null); const [err, setErr] = useState("");
  const [body, setBody] = useState(""); const [att, setAtt] = useState<TicketAttachment[]>([]); const [busy, setBusy] = useState(false);
  const load = useCallback(() => { getTicket(id).then((r) => (r.ok && r.data ? setT(r.data) : setErr(ticketErr(r)))); }, [id]);
  useEffect(() => { load(); }, [load]);
  if (err) return <Empty t={err}><Link className="ff-btn" href="/support/tickets">返回我的工单</Link></Empty>;
  if (!t) return <div className="ff-skel" />;
  const send = async () => {
    setBusy(true); const r = await replyTicket(id, body.trim(), att.map((a) => a.id)); setBusy(false);
    if (r.ok && r.data) { setT(r.data); setBody(""); setAtt([]); } else toast(ticketErr(r), "err");
  };
  const close = async () => { const r = await closeTicket(id); if (r.ok && r.data) { setT(r.data); toast("工单已关闭"); } else toast(ticketErr(r), "err"); };
  return <>
    <div className="ff-bar"><span><Link href="/support/tickets">← 我的工单</Link> · {catName(t.category)} · <StatusTag s={t.status} /></span>
      {t.status !== "closed" && <button className="ff-btn" onClick={close}>关闭工单</button>}</div>
    <h3 style={{ margin: "8px 0 16px" }}>{t.subject}</h3>
    <div style={{ display: "grid", gap: 12 }}>
      {(t.messages || []).map((m) => <div key={m.id} className="ff-card" style={m.author === "support" ? { borderLeft: "3px solid var(--lb-primary)" } : undefined}>
        <div className="ff-mut" style={{ marginBottom: 6 }}>{m.author === "support" ? "客服" : "我"} · {fmt(m.createdAt)}</div>
        <Plain text={m.body} />
        {m.attachments.length > 0 && <div style={{ display: "flex", gap: 8, marginTop: 8, flexWrap: "wrap" }}>{m.attachments.map((a) => <Img key={a.id} a={a} />)}</div>}
      </div>)}
    </div>
    {t.status === "closed" ? <p className="ff-mut" style={{ marginTop: 16 }}>工单已关闭。如有新问题，请 <Link href="/support/tickets?new=1">重新提交工单</Link>。</p>
      : <div className="ff-card" style={{ marginTop: 16 }}>
        <label className="ff-f"><span>补充回复</span><textarea value={body} maxLength={5000} rows={4} onChange={(e) => setBody(e.target.value)} /></label>
        <Attach ids={att} setIds={setAtt} />
        <div className="ff-mact"><button className="ff-btn pri" disabled={!body.trim() || busy} onClick={send}>{busy ? "发送中…" : "发送"}</button></div>
      </div>}
  </>;
}

export function SupportTickets() {
  const authed = useAuthStore((s) => s.isAuthed);
  const sp = useSearchParams(); const r = useRouter();
  const id = sp.get("id"); const isNew = sp.get("new") === "1";
  const [page, setPage] = useState(1);
  const [list, setList] = useState<{ items: Ticket[]; total: number } | null>(null); const [err, setErr] = useState("");
  useEffect(() => {
    if (!authed || id || isNew) return;
    setList(null); listTickets(page).then((x) => (x.ok && x.data ? setList({ items: x.data.items, total: x.data.total }) : setErr(ticketErr(x))));
  }, [authed, id, isNew, page]);
  return <Shell title={isNew ? "提交工单" : "我的工单"} sub="遇到问题可在线提交工单，客服回复后会通过站内消息通知您。">
    {!authed ? <Empty t="登录后提交或查看工单"><Link className="ff-btn pri" href="/login">登录</Link></Empty>
      : id ? <Thread id={id} />
      : isNew ? <NewTicket onDone={(t) => r.replace("/support/tickets?id=" + encodeURIComponent(t.id))} />
      : err ? <Empty t={err} /> : !list ? <div className="ff-skel" /> : <>
        <div className="ff-bar"><span className="ff-mut">共 {list.total} 个工单</span><Link className="ff-btn pri" href="/support/tickets?new=1" data-testid="tk-new-btn">提交工单</Link></div>
        {!list.items.length ? <Empty t="暂无工单" /> : <table className="ff-t"><thead><tr><th>标题</th><th>分类</th><th>状态</th><th>最近更新</th></tr></thead>
          <tbody>{list.items.map((t) => <tr key={t.id}><td><Link href={"/support/tickets?id=" + encodeURIComponent(t.id)}>{t.unread && <span className="ff-tag ok">新回复</span>} {t.subject}</Link></td>
            <td>{catName(t.category)}</td><td><StatusTag s={t.status} /></td><td className="ff-mut">{fmt(t.lastReplyAt)}</td></tr>)}</tbody></table>}
        {list.total > 20 && <div className="ff-bar"><button className="ff-btn" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><span className="ff-mut">{page} / {Math.ceil(list.total / 20)}</span><button className="ff-btn" disabled={page * 20 >= list.total} onClick={() => setPage(page + 1)}>下一页</button></div>}
      </>}
  </Shell>;
}
