"use client";
/**
 * 闪兑：选币（图标/搜索/方向互换）→ 数量（余额/最大）→ 实时报价（倒计时自动刷新）→ 确认弹窗 → 执行 → 兑换记录。
 * 后端接口契约（尚未实现，见 PR 描述）：
 *   GET  /convert/pairs                       → { items: [{ from, to, minAmount, maxAmount }] }
 *   POST /convert/quote   { from, to, amount } → { quoteId, rate, fromAmount, toAmount, fee, expiresAt }
 *   POST /convert/accept { quoteId }          → { orderId, status, fromAmount, toAmount, rate }
 *   GET  /convert/orders?limit=20              → { items: [{ orderId, from, to, fromAmount, toAmount, rate, status, createdAt }] }
 * 接口 404/不可达时页面显示「闪兑暂未开放」，仅展示行情参考价，不允许下单。
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@/lib/router";
import { useBalances } from "@/lib/queries";
import { useSymbols, useTickers } from "@/lib/market";
import { rest } from "@/lib/rest";
import { toast } from "@/lib/ui";
import { useAuthStore } from "@/lib/store";
import { CoinIcon } from "@/components/CoinIcon";
// @ts-ignore legacy
import { Footer } from "@/legacy/App";

type Pair = { from: string; to: string; minAmount?: string; maxAmount?: string };
type Quote = { quoteId: string; rate: string; fromAmount: string; toAmount: string; fee?: string; expiresAt: number };
type Order = { orderId: string; from: string; to: string; fromAmount: string; toAmount: string; rate: string; status: string; createdAt: number };

function Picker({ value, list, onPick }: { value: string; list: string[]; onPick: (s: string) => void }) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => { const h = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); }; document.addEventListener("mousedown", h); return () => document.removeEventListener("mousedown", h); }, []);
  const items = list.filter((s) => s.toLowerCase().includes(q.trim().toLowerCase()));
  return (
    <div className="cv-pick" ref={ref}>
      <button type="button" className="cv-pick-btn" onClick={() => setOpen((o) => !o)}><CoinIcon s={value} size={22} /> <b>{value}</b> <span className="cv-caret">▾</span></button>
      {open && (
        <div className="cv-pop">
          <input autoFocus placeholder="搜索币种" value={q} onChange={(e) => setQ(e.target.value)} />
          <div className="cv-list">
            {items.length === 0 && <div className="cv-empty">无匹配币种</div>}
            {items.map((s) => (
              <button type="button" key={s} className={s === value ? "on" : ""} onClick={() => { onPick(s); setOpen(false); setQ(""); }}><CoinIcon s={s} size={20} /> {s}</button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function Convert() {
  const authed = useAuthStore((s) => s.isAuthed);
  const syms = useSymbols();
  const { tickers } = useTickers();
  const bal = useBalances();
  const [pairs, setPairs] = useState<Pair[] | null>(null);
  const [avail, setAvail] = useState<"loading" | "yes" | "no">("loading");
  const [from, setFrom] = useState("BTC");
  const [to, setTo] = useState("USDT");
  const [amt, setAmt] = useState("");
  const [quote, setQuote] = useState<Quote | null>(null);
  const [qErr, setQErr] = useState("");
  const [left, setLeft] = useState(0);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [orders, setOrders] = useState<Order[]>([]);

  useEffect(() => {
    rest<{ items: Pair[] }>("GET", "/convert/pairs").then((r) => {
      if (r.ok && r.data?.items?.length) { setPairs(r.data.items); setAvail("yes"); } else setAvail("no");
    });
  }, []);
  const loadOrders = () => { if (authed && avail === "yes") rest<{ items: Order[] }>("GET", "/convert/orders?limit=20").then((r) => r.ok && setOrders(r.data?.items ?? [])); };
  useEffect(loadOrders, [authed, avail]);

  // 可选币种：优先 /convert/pairs，否则用现货 USDT 交易对（仅参考价）
  const assets = useMemo(() => {
    const set = new Set<string>(["USDT"]);
    if (pairs) pairs.forEach((p) => { set.add(p.from); set.add(p.to); });
    else Object.values(syms).forEach((m) => m.quote === "USDT" && set.add(m.base));
    if (set.size < 2) ["BTC", "ETH", "SOL"].forEach((s) => set.add(s));
    return [...set];
  }, [pairs, syms]);
  const pair = pairs?.find((p) => p.from === from && p.to === to);
  const px = (a: string) => (a === "USDT" ? 1 : Number(tickers[a + "USDT"]?.lastPrice) || 0);
  const refRate = px(from) && px(to) ? px(from) / px(to) : 0;
  const free = Number(((bal.data as any)?.items ?? (bal.data as any) ?? []).find?.((b: any) => b.asset === from)?.free ?? 0);
  const n = Number(amt);
  const min = Number(pair?.minAmount ?? 0), max = Number(pair?.maxAmount ?? Infinity);
  const amtErr = !amt ? "" : !(n > 0) ? "请输入有效数量" : n < min ? `最小兑换 ${pair?.minAmount} ${from}` : n > max ? `最大兑换 ${pair?.maxAmount} ${from}` : authed && n > free ? "可用余额不足" : "";

  const pick = (side: "from" | "to", s: string) => {
    if (side === "from") { if (s === to) setTo(from); setFrom(s); } else { if (s === from) setFrom(to); setTo(s); }
    setQuote(null);
  };
  const swap = () => { setFrom(to); setTo(from); setAmt(""); setQuote(null); };

  // 报价：输入停顿 400ms 后请求；到期自动刷新
  const qBusy = useRef(false);
  const getQuote = async () => {
    if (qBusy.current) return;
    if (avail !== "yes" || !authed || !(n > 0) || amtErr) { setQuote(null); return; }
    qBusy.current = true;
    const r = await rest<Quote>("POST", "/convert/quote", { from, to, amount: amt });
    qBusy.current = false;
    if (r.ok && r.data) { setQuote(r.data); setQErr(""); } else { setQuote(null); setQErr(r.error || "报价失败"); }
  };
  useEffect(() => { const t = setTimeout(getQuote, 400); return () => clearTimeout(t); }, [from, to, amt, avail, authed]); // eslint-disable-line
  useEffect(() => {
    if (!quote) return;
    const tick = () => { const s = Math.max(0, Math.ceil((quote.expiresAt - Date.now()) / 1000)); setLeft(s); if (s <= 2 && !confirm) getQuote(); };
    tick(); const i = setInterval(tick, 1000); return () => clearInterval(i);
  }, [quote, confirm]); // eslint-disable-line

  const execute = async () => {
    if (!quote) return;
    if (left === 0) { toast("报价已过期，已为你刷新", "info"); setConfirm(false); getQuote(); return; }
    setBusy(true);
    const r = await rest<Order>("POST", "/convert/accept", { quoteId: quote.quoteId });
    setBusy(false); setConfirm(false);
    if (r.ok) { toast(`兑换成功：${r.data?.fromAmount ?? amt} ${from} → ${r.data?.toAmount ?? quote.toAmount} ${to}`); setAmt(""); setQuote(null); bal.refetch?.(); loadOrders(); }
    else toast(r.error || "兑换失败", "err");
  };

  const toAmt = quote ? quote.toAmount : refRate && n > 0 ? (n * refRate).toFixed(to === "USDT" ? 2 : 8) : "";
  const canGo = avail === "yes" && authed && !!quote && !amtErr;
  return (
    <main className="light">
      <div className="cvh">
        <h1>闪兑，一步完成币币兑换</h1>
        <p>零手续费，锁定报价，即时到账。</p>
        <div className="cvbox">
          <div className="lrow"><b>闪兑 <em className="new">零手续费</em></b><button type="button" className="cv-ref" title="刷新报价" onClick={getQuote}>⟳</button></div>
          {avail === "no" && <div className="cv-warn">闪兑暂未开放，以下为行情参考价，暂不支持下单。</div>}
          <div className="cvi">
            <div className="lrow"><span className="lg">从</span>{authed && <span className="lg">可用 {free} {from} <button type="button" className="cv-max" onClick={() => setAmt(String(free))}>最大</button></span>}</div>
            <div className="lrow"><Picker value={from} list={assets} onPick={(s) => pick("from", s)} /><input className="cv-amt" inputMode="decimal" placeholder={pair?.minAmount ? `${pair.minAmount} - ${pair.maxAmount}` : "0.00"} value={amt} onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))} /></div>
          </div>
          {amtErr && <div className="cv-err">{amtErr}</div>}
          <button type="button" className="cvsw cv-swap" onClick={swap} title="互换方向">⇅</button>
          <div className="cvi">
            <span className="lg">到</span>
            <div className="lrow"><Picker value={to} list={assets} onPick={(s) => pick("to", s)} /><span className="cv-amt ro">{toAmt || "0.00"}</span></div>
          </div>
          <div className="lrow lg">
            <span>{quote ? <>报价 <em className="cv-cd">{left}s</em> 后刷新</> : "参考价格"}</span>
            <span>1 {from} ≈ {quote ? quote.rate : refRate ? refRate.toLocaleString("en-US", { maximumFractionDigits: 8 }) : "--"} {to}</span>
          </div>
          {qErr && <div className="cv-err">{qErr}</div>}
          {!authed ? <Link to="/login" className="lb wide">登录后兑换</Link>
            : <button type="button" className={"lb wide" + (canGo ? "" : " dis")} disabled={!canGo} onClick={() => setConfirm(true)}>{avail === "no" ? "暂未开放" : "兑换"}</button>}
        </div>
      </div>
      {authed && avail === "yes" && (
        <section className="cv-hist">
          <h3>兑换记录</h3>
          {orders.length === 0 ? <div className="cv-empty">暂无记录</div> : (
            <table><thead><tr><th>时间</th><th>从</th><th>到</th><th>价格</th><th>状态</th></tr></thead>
              <tbody>{orders.map((o) => <tr key={o.orderId}><td>{new Date(o.createdAt).toLocaleString()}</td><td>{o.fromAmount} {o.from}</td><td>{o.toAmount} {o.to}</td><td>{o.rate}</td><td>{o.status}</td></tr>)}</tbody></table>
          )}
        </section>
      )}
      {confirm && quote && (
        <div className="fx-modal" onClick={() => setConfirm(false)}>
          <div className="cv-modal" onClick={(e) => e.stopPropagation()}>
            <h3>确认兑换</h3>
            <div className="lrow"><span>支付</span><b><CoinIcon s={from} size={18} /> {quote.fromAmount} {from}</b></div>
            <div className="lrow"><span>获得</span><b><CoinIcon s={to} size={18} /> {quote.toAmount} {to}</b></div>
            <div className="lrow lg"><span>价格</span><span>1 {from} = {quote.rate} {to}</span></div>
            <div className="lrow lg"><span>手续费</span><span>{quote.fee ?? "0"}</span></div>
            <div className="lrow lg"><span>报价有效</span><span>{left}s</span></div>
            <div className="lrow"><button type="button" className="lb ghost" onClick={() => setConfirm(false)}>取消</button><button type="button" className="lb" disabled={busy} onClick={execute}>{busy ? "处理中…" : left ? "确认兑换" : "刷新报价"}</button></div>
          </div>
        </div>
      )}
      <Footer light />
    </main>
  );
}
