"use client";
/**
 * 功能开关控制的新业务前端：TradFi 资产目录、事件合约、聊天室、子账户。
 * 全部受 /app/features 控制；关闭或接口失败 ⇒ 跳转 /coming-soon/<slug>。接口见 docs/api/openapi.yaml。
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { isOn, loadFeatures, SOON, type FeatureKey } from "@/lib/features";
import { rest, rget, type RestResult } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";
import { CoinIcon } from "@/components/CoinIcon";
import { Footer } from "@/legacy/App";
import type { components, paths } from "@linbit/api-client";

/* ---------- 接口类型：全部来自 openapi 生成的 schema.ts ---------- */
type S = components["schemas"];
type Ok<P extends keyof paths, M extends "get" | "post" = "get"> = paths[P][M] extends { responses: { 200: { content: { "application/json": infer T } } } } ? T : never;
type TFAsset = S["TradFiAsset"];
type EvProducts = S["EventProducts"];
type EvOrder = S["EventOrder"];
type Room = S["ChatRoom"];
type Msg = S["ChatMessage"];
type Rules = S["ChatRules"];
type Sub = S["SubAccount"];
/** 列表响应的 limit（子账户上限）后端已返回，但 openapi 尚未声明 */
type SubList = Ok<"/sub-accounts"> & { limit?: number };
type SubBal = NonNullable<Ok<"/sub-accounts/assets">["items"]>[number];

/* ---------- 开关守卫 ---------- */
export function Gate({ any, slug, children }: { any: (FeatureKey | string)[]; slug: string; children: ReactNode }) {
  const r = useRouter(); const [ok, setOk] = useState(false);
  useEffect(() => {
    const chk = () => (any.some((k) => isOn(k)) ? setOk(true) : r.replace("/coming-soon/" + slug));
    loadFeatures().then(chk); window.addEventListener("lb-features", chk); return () => window.removeEventListener("lb-features", chk);
  }, [r, slug, any.join()]); // eslint-disable-line
  return ok ? <>{children}</> : null;
}

const ERR: Record<string, string> = {
  REGION_RESTRICTED: "根据您所在地区的法规要求，暂无法使用该服务", FEATURE_DISABLED: "该功能暂未开放", SERVICE_UNAVAILABLE: "服务暂不可用，请稍后再试",
  SYMBOL_NOT_SUPPORTED: "暂不支持该标的", SYMBOL_PAUSED: "该标的已暂停交易", PERIOD_NOT_SUPPORTED: "暂不支持该周期", STAKE_OUT_OF_RANGE: "下单金额超出范围",
  DAILY_LIMIT_EXCEEDED: "已超出今日下单限额", EXPOSURE_LIMIT: "平台风险敞口已满，请稍后再试", INSUFFICIENT_BALANCE: "可用余额不足", INDEX_UNAVAILABLE: "指数价格暂不可用",
  KYC_REQUIRED: "请先完成身份认证", RATE_LIMITED: "发言过于频繁", TOTP_REQUIRED: "请先开启谷歌验证",
  TWOFA_REQUIRED: "请先开启谷歌验证（2FA）", INVALID_TOTP: "谷歌验证码错误或已过期", SUB_ACCOUNT_LIMIT: "子账户数量已达上限",
  CHAT_KYC_REQUIRED: "完成身份认证后才能发言", CHAT_RATE_LIMITED: "发言过于频繁，请稍后再试", CHAT_LINK_BLOCKED: "消息中不能包含链接",
  CHAT_CONTACT_BLOCKED: "消息中不能包含联系方式", CHAT_MUTED: "你已被禁言，暂时无法发言", CHAT_BANNED: "你已被禁止使用聊天室",
};
const errMsg = (r: RestResult<unknown>) => (r.code && ERR[r.code]) || r.error || "操作失败";
const cid = (p: string) => `${p}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
const num = (s?: string | null, d = 2) => (s == null || s === "" || isNaN(+s) ? "--" : (+s).toLocaleString("en-US", { maximumFractionDigits: d, minimumFractionDigits: Math.min(d, 2) }));

function Page({ title, sub, children }: { title: string; sub?: string; children: ReactNode }) {
  return <main className="light"><div className="ff-wrap"><div className="ff-hd"><h1>{title}</h1>{sub && <p>{sub}</p>}</div>{children}</div><Footer light /></main>;
}
function Empty({ t, children }: { t: string; children?: ReactNode }) { return <div className="ff-empty"><div className="ff-empty-i">◌</div><div>{t}</div>{children}</div>; }
function NeedLogin() { return <Empty t="登录后查看"><Link className="ff-btn pri" href="/login">登录</Link></Empty>; }

/* ---------- TradFi ---------- */
// TradFi 整体由 tokenizedStocks 开关控制
const KINDS = [["stock", "股票"], ["metal", "贵金属"], ["commodity", "大宗商品"]] as const;
export function TradFi() {
  const sp = useSearchParams(); const r = useRouter();
  const kinds = isOn("tokenizedStocks") ? KINDS : [];
  const kind = (kinds.find(([k]) => k === sp.get("kind"))?.[0] || kinds[0]?.[0] || "stock") as string;
  const [res, setRes] = useState<RestResult<{ items: TFAsset[] }> | null>(null);
  const [q, setQ] = useState("");
  useEffect(() => { setRes(null); rget<Ok<"/tradfi/assets">>("/tradfi/assets?kind=" + kind).then(setRes); }, [kind]);
  const items = (res?.data?.items || []).filter((a) => !q || ((a.asset || "") + (a.name || "") + (a.symbol || "")).toLowerCase().includes(q.toLowerCase()));
  const go = (a: TFAsset) => { if (a.status !== "active") return toast("该资产已暂停交易", "info"); r.push((a.market === "futures" ? "/futures/" : "/spot/") + a.symbol); };
  return (
    <Page title="传统金融" sub="用 USDT 交易代币化股票、贵金属与大宗商品，参考价来自 Binance / OKX 永续合约。">
      <div className="ff-bar">
        <div className="ff-tabs">{kinds.map(([k, n]) => <button key={k} className={k === kind ? "on" : ""} onClick={() => r.replace("/tradfi/assets?kind=" + k)}>{n}</button>)}</div>
        <input className="ff-search" placeholder="搜索资产" value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      {!res ? <div className="ff-skel" /> : res.code === "REGION_RESTRICTED" ? (
        <div className="ff-restrict"><b>服务受限</b><p>{ERR.REGION_RESTRICTED}。TradFi 资产的交易、充值与提现在美国及受制裁地区不可用；已有持仓可正常平仓。</p><Link href="/markets" className="ff-btn">查看其他市场</Link></div>
      ) : !res.ok ? <Empty t={errMsg(res)}><button className="ff-btn" onClick={() => { setRes(null); rget<Ok<"/tradfi/assets">>("/tradfi/assets?kind=" + kind).then(setRes); }}>重试</button></Empty>
      : !items.length ? <Empty t="暂无资产" /> : (
        <table className="ff-t"><thead><tr><th>资产</th><th>市场</th><th className="r">最新价</th><th className="r">参考价</th><th className="r">偏离</th><th className="r">操作</th></tr></thead>
          <tbody>{items.map((a) => <tr key={a.symbol} onClick={() => go(a)}>
            <td><div className="ff-as"><CoinIcon s={a.asset || ""} size={28} /><div><b>{a.asset}</b><span>{a.underlying || a.name}</span></div></div></td>
            <td><span className="ff-tag">{a.market === "futures" ? "永续" : "现货"}</span>{a.status !== "active" && <span className="ff-tag warn">暂停</span>}</td>
            <td className="r">{num(a.lastPrice, 4)}</td><td className="r">{num(a.referencePrice, 4)}</td>
            <td className={"r" + (a.widened ? " ff-dn" : "")}>{a.currentDeviationBps == null || a.currentDeviationBps === "" || isNaN(+a.currentDeviationBps) ? "--" : `${(+a.currentDeviationBps / 100).toFixed(2)}%`}{a.widened ? " · 价差放宽" : ""}</td>
            <td className="r"><button className="ff-btn sm pri" disabled={a.status !== "active"} onClick={(e) => { e.stopPropagation(); go(a); }}>交易</button></td>
          </tr>)}</tbody></table>
      )}
    </Page>
  );
}

/* ---------- 事件合约 ---------- */
const RES: Record<string, string> = { won: "盈利", lost: "亏损", tie: "平局退款", refunded: "已退款" };
const ST: Record<string, string> = { pending: "处理中", open: "进行中", settling: "结算中", settled: "已结算", rejected: "已拒绝" };
const left = (ms: number) => { const s = Math.max(0, Math.round(ms / 1000)); const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = s % 60; return (h ? h + ":" : "") + String(m).padStart(2, "0") + ":" + String(x).padStart(2, "0"); };
export function Events() {
  const authed = useAuthStore((s) => s.isAuthed);
  const [p, setP] = useState<EvProducts | null>(null); const [perr, setPerr] = useState("");
  const [sym, setSym] = useState(""); const [per, setPer] = useState(""); const [stake, setStake] = useState(""); const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<"open" | "settled" | "all">("open"); const [orders, setOrders] = useState<EvOrder[] | null>(null); const [used, setUsed] = useState("0");
  const [now, setNow] = useState(Date.now());
  const loadP = useCallback(() => rget<EvProducts>("/event/products").then((r) => { if (r.ok && r.data) { setP(r.data); setSym((s) => s || r.data!.products?.find((x) => !x.paused)?.symbol || ""); setPer((x) => x || r.data!.periods?.[0]?.period || ""); } else setPerr(errMsg(r)); }), []);
  const loadO = useCallback(() => { if (!authed) return; rget<Ok<"/event/orders">>(`/event/orders?status=${tab}&limit=50`).then((r) => { setOrders(r.ok ? r.data?.orders || [] : []); if (r.data?.dailyUsed) setUsed(r.data.dailyUsed); }); }, [authed, tab]);
  useEffect(() => { loadP(); const t = setInterval(loadP, 5000); return () => clearInterval(t); }, [loadP]);
  useEffect(() => { setOrders(null); loadO(); const t = setInterval(loadO, 5000); return () => clearInterval(t); }, [loadO]);
  useEffect(() => { const t = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(t); }, []);
  const prod = p?.products?.find((x) => x.symbol === sym);
  const ratio = +(p?.payoutRatio || 0);
  const place = async (direction: "up" | "down") => {
    if (!authed) { window.location.assign("/login"); return; }
    if (!p?.enabled) return toast(ERR.FEATURE_DISABLED, "err");
    const v = +stake; if (!(v >= +(p.minStake || 0) && v <= +(p.maxStake || Infinity))) return toast(`下单金额需在 ${p.minStake} – ${p.maxStake} ${p.stakeAsset} 之间`, "err");
    setBusy(true); const r = await rest<EvOrder>("POST", "/event/orders", { symbol: sym, period: per, direction, stake: String(v), clientId: cid("ev") }); setBusy(false);
    if (r.ok) { toast(`已下单：${direction === "up" ? "看涨" : "看跌"} ${v} ${p.stakeAsset}`); setStake(""); setTab("open"); loadO(); } else toast(errMsg(r), "err");
  };
  if (perr && !p) return <Page title="事件合约"><Empty t={perr}><button className="ff-btn" onClick={() => { setPerr(""); loadP(); }}>重试</button></Empty></Page>;
  return (
    <Page title="事件合约" sub="预测到期时指数价格的涨跌方向。猜对获得本金 + 本金 × 赔率，猜错损失本金；到期价等于开仓价则退还本金。">
      {!p ? <div className="ff-skel" /> : <div className="ff-ev">
        <div className="ff-card">
          <div className="ff-lbl">标的</div>
          <div className="ff-chips">{(p.products || []).map((x) => <button key={x.symbol} className={x.symbol === sym ? "on" : ""} disabled={x.paused} onClick={() => setSym(x.symbol || "")}><CoinIcon s={x.baseAsset || ""} size={18} />{x.baseAsset}{x.paused && <em>暂停</em>}</button>)}</div>
          <div className="ff-idx"><span>指数价格</span><b>{prod?.priceAvailable ? num(prod.indexPrice, 2) : "暂不可用"}</b><small>{prod?.quoteAsset}</small></div>
          <div className="ff-lbl">周期</div>
          <div className="ff-chips">{(p.periods || []).map((x) => <button key={x.period} className={x.period === per ? "on" : ""} onClick={() => setPer(x.period || "")}>{x.period}</button>)}</div>
          <div className="ff-lbl">金额 <span>可下 {p.minStake} – {p.maxStake} {p.stakeAsset} · 今日已用 {num(used)} / {num(p.dailyLimit)}</span></div>
          <div className="ff-inp"><input inputMode="decimal" placeholder={`最少 ${p.minStake}`} value={stake} onChange={(e) => setStake(e.target.value.replace(/[^\d.]/g, ""))} /><span>{p.stakeAsset}</span></div>
          <div className="ff-chips sm">{["10", "50", "100", "500"].map((v) => <button key={v} onClick={() => setStake(v)}>{v}</button>)}</div>
          <div className="ff-row"><span>赔率</span><b>{(ratio * 100).toFixed(0)}%</b></div>
          <div className="ff-row"><span>预计盈利</span><b>{stake ? num(String(+stake * ratio)) : "--"} {p.stakeAsset}</b></div>
          {!p.enabled && <div className="ff-note">事件合约暂未开放下单</div>}
          <div className="ff-ud"><button className="up" disabled={busy || !p.enabled || !prod || prod.paused || !prod.priceAvailable} onClick={() => place("up")}>看涨</button><button className="dn" disabled={busy || !p.enabled || !prod || prod.paused || !prod.priceAvailable} onClick={() => place("down")}>看跌</button></div>
        </div>
        <div className="ff-card grow">
          <div className="ff-tabs sm">{([["open", "进行中"], ["settled", "已结算"], ["all", "全部"]] as const).map(([k, n]) => <button key={k} className={tab === k ? "on" : ""} onClick={() => setTab(k)}>{n}</button>)}</div>
          {!authed ? <NeedLogin /> : !orders ? <div className="ff-skel sm" /> : !orders.length ? <Empty t="暂无订单" /> : (
            <table className="ff-t"><thead><tr><th>标的 / 周期</th><th>方向</th><th className="r">金额</th><th className="r">开仓价</th><th className="r">{tab === "open" ? "剩余" : "结算价"}</th><th className="r">状态 / 盈亏</th></tr></thead>
              <tbody>{orders.map((o) => <tr key={o.id}><td><b>{o.symbol}</b> <span className="ff-mut">{o.period}</span></td><td className={o.direction === "up" ? "ff-up" : "ff-dn"}>{o.direction === "up" ? "看涨" : "看跌"}</td><td className="r">{num(o.stake)}</td><td className="r">{num(o.openPrice, 4)}</td>
                <td className="r">{o.status === "open" ? left((o.expiryTime || 0) - now) : o.status === "pending" || o.status === "settling" ? <span className="ff-tag">{ST[o.status!]}</span> : num(o.settlePrice, 4)}</td>
                <td className={"r " + (o.pnl && +o.pnl > 0 ? "ff-up" : o.pnl && +o.pnl < 0 ? "ff-dn" : "")}>{o.status === "pending" || o.status === "settling" ? <span className="ff-mut">{o.status === "pending" ? "订单确认中…" : "等待结算…"}</span> : o.result ? `${RES[o.result] || o.result} ${o.pnl ? (+o.pnl > 0 ? "+" : "") + num(o.pnl) : ""}` : ST[o.status || ""] || o.status}</td></tr>)}</tbody></table>
          )}
        </div>
      </div>}
    </Page>
  );
}

/* ---------- 聊天室 ---------- */
export function Chat() {
  const authed = useAuthStore((s) => s.isAuthed);
  const [rooms, setRooms] = useState<Room[] | null>(null); const [rules, setRules] = useState<Rules | null>(null); const [err, setErr] = useState("");
  const [room, setRoom] = useState("general"); const [msgs, setMsgs] = useState<Msg[]>([]); const [more, setMore] = useState(false);
  const [text, setText] = useState(""); const [last, setLast] = useState(0); const [rep, setRep] = useState<Msg | null>(null); const [why, setWhy] = useState("");
  const box = useRef<HTMLDivElement>(null); const mineIds = useRef<Set<number>>(new Set());
  useEffect(() => { rget<Ok<"/chat/rooms">>("/chat/rooms").then((r) => { if (r.ok) { setRooms(r.data?.rooms || []); setRules(r.data?.rules || null); } else setErr(errMsg(r)); }); }, []);
  const load = useCallback((before?: number) => rget<Ok<"/chat/rooms/{room}/messages">>(`/chat/rooms/${encodeURIComponent(room)}/messages?limit=50${before ? "&before=" + before : ""}`).then((r) => {
    if (!r.ok) return; const it = [...(r.data?.items || [])].reverse(); setMore(!!r.data?.hasMore);
    setMsgs((m) => { if (before) return [...it, ...m]; const seen = new Set(m.map((x) => x.id)); return it.some((x) => !seen.has(x.id)) || m.length !== it.length ? it : m; });
  }), [room]);
  useEffect(() => { setMsgs([]); load(); const t = setInterval(() => load(), 4000); return () => clearInterval(t); }, [load]);
  useEffect(() => { box.current?.scrollTo({ top: box.current.scrollHeight }); }, [msgs.length, room]);
  const send = async () => {
    if (!authed) { window.location.assign("/login"); return; }
    const t = text.trim(); if (!t) return;
    if (rules?.maxLength && t.length > rules.maxLength) return toast(`消息不能超过 ${rules.maxLength} 字`, "err");
    if (rules?.minIntervalMs && Date.now() - last < rules.minIntervalMs) return toast(ERR.RATE_LIMITED, "err");
    const clientId = cid("chat");
    const r = await rest<Msg>("POST", `/chat/rooms/${encodeURIComponent(room)}/messages`, { clientId, text: t });
    if (r.ok && r.data) { setText(""); setLast(Date.now()); mineIds.current.add(r.data.id); setMsgs((m) => (m.some((x) => x.id === r.data!.id) ? m : [...m, { ...r.data!, mine: true }])); } else toast(errMsg(r), "err");
  };
  const report = async () => { if (!rep) return; const r = await rest("POST", `/chat/messages/${rep.id}/report`, { reason: why || "其他" }); setRep(null); setWhy(""); toast(r.ok ? "已举报，感谢反馈" : errMsg(r), r.ok ? "ok" : "err"); };
  const name = (x: Room) => x.name || (x.type === "general" ? "综合讨论" : `${x.symbol || x.id} ${x.type === "futures" ? "合约" : "现货"}`);
  // 自己的消息：历史/发送响应带 mine；WS 推送 chat:{room} 既无 mine 也无 clientId ⇒ 用本端发送响应的 id 集合比对
  const isMine = (m: Msg) => m.mine === true || mineIds.current.has(m.id);
  if (err) return <Page title="聊天室"><Empty t={err} /></Page>;
  return (
    <Page title="聊天室" sub="与全球交易者实时交流行情观点。请勿发布链接、联系方式或投资建议。">
      {!rooms ? <div className="ff-skel" /> : <div className="ff-chat">
        <div className="ff-rooms">{rooms.map((x) => <button key={x.id} className={x.id === room ? "on" : ""} onClick={() => setRoom(x.id)}>{x.symbol ? <CoinIcon s={x.symbol} size={20} /> : <i className="ff-gen">#</i>}<span>{name(x)}</span><small>{x.online} 在线</small></button>)}</div>
        <div className="ff-cmain">
          <div className="ff-msgs" ref={box}>
            {more && <button className="ff-more" onClick={() => load(msgs[0]?.id)}>加载更早消息</button>}
            {!msgs.length ? <Empty t="还没有消息，来说第一句吧" /> : msgs.map((m) => <div key={m.id} className={"ff-msg" + (isMine(m) ? " me" : "")}>
              <div className="ff-mh"><b>{isMine(m) ? "我" : m.sender}</b><span>{new Date(m.createdAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}</span>{!isMine(m) && authed && <button onClick={() => setRep(m)}>举报</button>}</div>
              <div className="ff-mb">{m.text}</div></div>)}
          </div>
          <div className="ff-send">
            {authed ? <><input value={text} maxLength={rules?.maxLength || 200} placeholder={rules?.minKycLevel ? `需完成 KYC Lv.${rules.minKycLevel} 才能发言` : "说点什么…"} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => e.key === "Enter" && send()} /><span className="ff-mut">{text.length}/{rules?.maxLength || 200}</span><button className="ff-btn pri" disabled={!text.trim()} onClick={send}>发送</button></>
              : <div className="ff-login"><span>登录后参与讨论</span><Link className="ff-btn pri" href="/login">登录</Link></div>}
          </div>
        </div>
      </div>}
      {rep && <div className="ff-mask" onClick={() => setRep(null)}><div className="ff-modal" onClick={(e) => e.stopPropagation()}>
        <h3>举报消息</h3><p className="ff-quote">{rep.text}</p>
        <div className="ff-chips">{["垃圾广告", "诈骗", "辱骂", "其他"].map((x) => <button key={x} className={why === x ? "on" : ""} onClick={() => setWhy(x)}>{x}</button>)}</div>
        <div className="ff-mact"><button className="ff-btn" onClick={() => setRep(null)}>取消</button><button className="ff-btn pri" onClick={report}>提交</button></div>
      </div></div>}
    </Page>
  );
}

/* ---------- 子账户 ---------- */
export function SubAccounts() {
  const authed = useAuthStore((s) => s.isAuthed);
  const [list, setList] = useState<Sub[] | null>(null); const [limit, setLimit] = useState<number | null>(null); const [err, setErr] = useState(""); const [assets, setAssets] = useState<Record<string, SubBal>>({});
  const [mode, setMode] = useState<null | "new" | { tr: Sub } | { key: Sub }>(null);
  const load = useCallback(() => {
    if (!authed) return;
    rget<SubList>("/sub-accounts?page=1&size=50").then((r) => { if (r.ok) { setList(r.data?.items || []); if (typeof r.data?.limit === "number") setLimit(r.data.limit); } else setErr(errMsg(r)); });
    rget<Ok<"/sub-accounts/assets">>("/sub-accounts/assets").then((r) => r.ok && setAssets(Object.fromEntries((r.data?.items || []).map((x) => [x.uid, x]))));
  }, [authed]);
  useEffect(() => { load(); }, [load]);
  const freeze = async (s: Sub) => { const r = await rest("POST", `/sub-accounts/${s.uid}/${s.status === "frozen" ? "unfreeze" : "freeze"}`, {}); toast(r.ok ? (s.status === "frozen" ? "已解冻" : "已冻结") : errMsg(r), r.ok ? "ok" : "err"); load(); };
  return (
    <Page title="子账户" sub="为不同策略或团队成员创建独立子账户，资产隔离、统一管理。子账户 API Key 无提现权限。">
      {!authed ? <NeedLogin /> : err ? <Empty t={err} /> : !list ? <div className="ff-skel" /> : <>
        <div className="ff-bar"><span className="ff-mut">共 {list.length}{limit != null ? ` / ${limit}` : ""} 个子账户</span><button className="ff-btn pri" disabled={limit != null && list.length >= limit} title={limit != null && list.length >= limit ? ERR.SUB_ACCOUNT_LIMIT : undefined} onClick={() => setMode("new")}>{limit != null && list.length >= limit ? "已达上限" : "创建子账户"}</button></div>
        {!list.length ? <Empty t="暂无子账户" /> : <table className="ff-t"><thead><tr><th>子账户</th><th>登录邮箱</th><th className="r">资产</th><th>状态</th><th className="r">操作</th></tr></thead>
          <tbody>{list.map((s) => <tr key={s.uid}><td><b>{s.nickname || s.username}</b><div className="ff-mut">{s.username}{s.note ? " · " + s.note : ""}</div></td><td className="ff-mut">{s.loginEmail}</td>
            <td className="r">{(() => { const tot = (x: S["SubBalance"]) => (x.total != null && x.total !== "" ? +x.total : +(x.free || 0) + +(x.locked || 0)); const b = (assets[s.uid || ""]?.balances || []).filter((x) => tot(x) > 0); return b.length ? b.slice(0, 3).map((x) => `${num(String(tot(x)), 4)} ${x.asset}`).join(" · ") + (b.length > 3 ? ` 等 ${b.length} 种` : "") : "--"; })()}</td><td><span className={"ff-tag" + (s.status === "frozen" ? " warn" : "")}>{s.status === "frozen" ? "已冻结" : "正常"}</span>{!s.loginEnabled && <span className="ff-tag">禁止登录</span>}</td>
            <td className="r ff-ops"><button onClick={() => setMode({ tr: s })}>划转</button><button onClick={() => setMode({ key: s })}>API Key</button><button onClick={() => freeze(s)}>{s.status === "frozen" ? "解冻" : "冻结"}</button></td></tr>)}</tbody></table>}
      </>}
      {mode === "new" && <NewSub limit={limit} onClose={() => setMode(null)} onDone={() => { setMode(null); load(); }} />}
      {mode && typeof mode === "object" && "tr" in mode && <SubTransfer s={mode.tr} onClose={() => setMode(null)} onDone={() => { setMode(null); load(); }} />}
      {mode && typeof mode === "object" && "key" in mode && <SubKey s={mode.key} onClose={() => setMode(null)} />}
    </Page>
  );
}
function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return <div className="ff-mask" onClick={onClose}><div className="ff-modal" onClick={(e) => e.stopPropagation()}><h3>{title}<button className="ff-x" onClick={onClose}>×</button></h3>{children}</div></div>;
}
function NewSub({ onClose, onDone, limit }: { onClose: () => void; onDone: () => void; limit?: number | null }) {
  const [u, setU] = useState(""); const [n, setN] = useState(""); const [note, setNote] = useState(""); const ok = /^[a-z0-9][a-z0-9_]{2,19}$/.test(u);
  const go = async () => { const r = await rest("POST", "/sub-accounts", { username: u, nickname: n || undefined, note: note || undefined }); if (r.ok) { toast("子账户已创建"); onDone(); } else toast(errMsg(r), "err"); };
  return <Modal title="创建子账户" onClose={onClose}>
    <label className="ff-f"><span>子账户名</span><input value={u} onChange={(e) => setU(e.target.value.toLowerCase())} placeholder="3–20 位小写字母、数字或下划线" /></label>
    {u && !ok && <div className="ff-note">格式不正确</div>}
    <label className="ff-f"><span>昵称（选填）</span><input value={n} maxLength={32} onChange={(e) => setN(e.target.value)} /></label>
    <label className="ff-f"><span>备注（选填）</span><input value={note} maxLength={200} onChange={(e) => setNote(e.target.value)} /></label>
    <p className="ff-mut">母账户需已开启谷歌验证{limit != null ? `；最多 ${limit} 个子账户` : ""}。</p>
    <div className="ff-mact"><button className="ff-btn" onClick={onClose}>取消</button><button className="ff-btn pri" disabled={!ok} onClick={go}>创建</button></div>
  </Modal>;
}
function SubTransfer({ s, onClose, onDone }: { s: Sub; onClose: () => void; onDone: () => void }) {
  const [dir, setDir] = useState<"to_sub" | "to_parent">("to_sub"); const [asset, setAsset] = useState("USDT"); const [amt, setAmt] = useState("");
  const go = async () => { if (!(+amt > 0)) return toast("请输入划转数量", "err"); const r = await rest("POST", "/sub-accounts/transfer", { clientId: cid("sub"), subAccountId: s.uid, direction: dir, asset, amount: amt }); if (r.ok) { toast("划转成功"); onDone(); } else toast(errMsg(r), "err"); };
  return <Modal title={`划转 · ${s.nickname || s.username}`} onClose={onClose}>
    <div className="ff-chips"><button className={dir === "to_sub" ? "on" : ""} onClick={() => setDir("to_sub")}>母账户 → 子账户</button><button className={dir === "to_parent" ? "on" : ""} onClick={() => setDir("to_parent")}>子账户 → 母账户</button></div>
    <label className="ff-f"><span>币种</span><div className="ff-inp"><CoinIcon s={asset} size={20} /><input value={asset} onChange={(e) => setAsset(e.target.value.toUpperCase())} /></div></label>
    <label className="ff-f"><span>数量</span><input inputMode="decimal" value={amt} onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))} /></label>
    <div className="ff-mact"><button className="ff-btn" onClick={onClose}>取消</button><button className="ff-btn pri" onClick={go}>确认划转</button></div>
  </Modal>;
}
function SubKey({ s, onClose }: { s: Sub; onClose: () => void }) {
  const [keys, setKeys] = useState<{ apiKey: string; label?: string; permissions: string[]; createdAt?: string }[] | null>(null);
  const [perm, setPerm] = useState<string[]>(["read"]); const [label, setLabel] = useState(""); const [totp, setTotp] = useState(""); const [made, setMade] = useState<{ apiKey: string; secret?: string } | null>(null);
  const load = useCallback(() => rget<{ items: typeof keys }>(`/sub-accounts/${s.uid}/api-keys`).then((r) => setKeys(r.data?.items || [])), [s.uid]);
  useEffect(() => { load(); }, [load]);
  const P: Record<string, string> = { read: "读取", spot_trade: "现货交易", futures_trade: "合约交易" };
  const create = async () => { if (!/^\d{6}$/.test(totp)) return toast("请输入 6 位谷歌验证码", "err"); const r = await rest<{ apiKey: string; secret?: string }>("POST", `/sub-accounts/${s.uid}/api-keys`, { label: label || undefined, permissions: perm, totpCode: totp }); if (r.ok && r.data) { setMade(r.data); setTotp(""); load(); } else toast(errMsg(r), "err"); };
  const del = async (k: string) => { const r = await rest("DELETE", `/sub-accounts/${s.uid}/api-keys/${k}`); toast(r.ok ? "已删除" : errMsg(r), r.ok ? "ok" : "err"); load(); };
  return <Modal title={`API Key · ${s.nickname || s.username}`} onClose={onClose}>
    {made && <div className="ff-note ok">API Key：{made.apiKey}{made.secret && <><br />Secret：{made.secret}（仅显示一次，请妥善保存）</>}</div>}
    {!keys ? <div className="ff-skel sm" /> : !keys.length ? <p className="ff-mut">暂无 API Key</p> : keys.map((k) => <div key={k.apiKey} className="ff-krow"><div><b>{k.label || "未命名"}</b><div className="ff-mut">{k.apiKey} · {k.permissions.map((x) => P[x] || x).join(" / ")}</div></div><button className="ff-btn sm" onClick={() => del(k.apiKey)}>删除</button></div>)}
    <label className="ff-f"><span>备注名</span><input value={label} maxLength={40} onChange={(e) => setLabel(e.target.value)} /></label>
    <div className="ff-chips">{Object.keys(P).map((x) => <button key={x} className={perm.includes(x) ? "on" : ""} onClick={() => setPerm((p) => (p.includes(x) ? p.filter((y) => y !== x) : [...p, x]))}>{P[x]}</button>)}</div>
    <label className="ff-f"><span>母账户谷歌验证码</span><input inputMode="numeric" maxLength={6} value={totp} onChange={(e) => setTotp(e.target.value.replace(/\D/g, ""))} /></label>
    <div className="ff-mact"><button className="ff-btn" onClick={onClose}>关闭</button><button className="ff-btn pri" disabled={!perm.length} onClick={create}>创建 API Key</button></div>
  </Modal>;
}

