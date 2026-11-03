"use client";
/**
 * 闪兑：选币 → 数量 → 实时报价（10s 倒计时，到期自动重新报价）→ 确认 → 成功弹窗 → 兑换记录（分页）。
 * 接口（docs/api/convert.md）：
 *   GET  /convert/pairs  (public) → { assets:[{asset,precision}], pairs:[{from,to,fromPrecision,toPrecision,spreadBps,route}], minUsdt, maxUsdt, spreadBps, fee, quoteTtlSeconds }
 *   POST /convert/quote  {from,to,fromAmount|toAmount} → { quoteId, rate, inverseRate, fromAmount, toAmount, fee, expiresAt, ttlSeconds }
 *   POST /convert/accept {quoteId} → { order, replay }（幂等）
 *   GET  /convert/orders?page&pageSize&asset → { items, page, pageSize, total }
 * pairs 接口不可用时才显示「闪兑暂未开放」。
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@/lib/router";
import { useBalances } from "@/lib/queries";
import { useSymbols, useTickers } from "@/lib/market";
import { rest } from "@/lib/rest";
import { toast } from "@/lib/ui";
import { useAuthStore } from "@/lib/store";
import { CoinIcon } from "@/components/CoinIcon";
import { useDismiss } from "@/lib/dismiss";
// @ts-ignore legacy
import { Footer } from "@/legacy/App";

type Pair = { from: string; to: string; fromPrecision: number; toPrecision: number; spreadBps: number; route: string };
type PairsRes = { assets: { asset: string; precision: number }[]; pairs: Pair[]; minUsdt: string; maxUsdt: string; spreadBps: number; fee: string; quoteTtlSeconds: number };
type Quote = { quoteId: string; rate: string; inverseRate?: string; fromAmount: string; toAmount: string; fee?: string; feeAsset?: string; expiresAt: number; ttlSeconds?: number };
type Order = { id: number; quoteId: string; from: string; to: string; fromAmount: string; toAmount: string; rate: string; usdtValue?: string; status: string; createdAt: number };
const ERR: Record<string, string> = {
  VALIDATION_ERROR: "请求参数不合法", PAIR_NOT_SUPPORTED: "暂不支持该币对", PRICE_UNAVAILABLE: "价格暂不可用，请稍后再试",
  QUOTE_NOT_FOUND: "报价不存在，已重新报价", QUOTE_EXPIRED: "报价已过期，已重新报价", INSUFFICIENT_BALANCE: "可用余额不足",
  PAIR_PAUSED_DEPEG: "USDC 脱锚，该兑换对已暂停", // 503
};
const PAGE = 10;

function Picker({ value, list, onPick }: { value: string; list: string[]; onPick: (s: string) => void }) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  useDismiss(open, () => setOpen(false), ref);
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
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [done, setDone] = useState<Order | null>(null);
  const [cfg, setCfg] = useState<PairsRes | null>(null);

  useEffect(() => {
    rest<PairsRes>("GET", "/convert/pairs").then((r) => {
      if (r.ok && r.data?.pairs) { setCfg(r.data); setPairs(r.data.pairs); setAvail("yes"); } else setAvail("no");
    });
  }, []);
  const loadOrders = () => {
    if (!(authed && avail === "yes")) return;
    rest<{ items: Order[]; total: number }>("GET", `/convert/orders?page=${page}&pageSize=${PAGE}`).then((r) => { if (r.ok) { setOrders(r.data?.items ?? []); setTotal(r.data?.total ?? 0); } });
  };
  useEffect(loadOrders, [authed, avail, page]); // eslint-disable-line

  // 可选币种：优先 /convert/pairs，否则用现货 USDT 交易对（仅参考价）
  const assets = useMemo(() => {
    const set = new Set<string>(["USDT"]);
    if (cfg) { set.clear(); cfg.assets.forEach((a) => set.add(a.asset)); }
    else Object.values(syms).forEach((m) => m.quote === "USDT" && set.add(m.base));
    if (set.size < 2) ["BTC", "ETH", "SOL"].forEach((s) => set.add(s));
    return [...set];
  }, [cfg, syms]);
  const pair = pairs?.find((p) => p.from === from && p.to === to);
  const px = (a: string) => (a === "USDT" ? 1 : Number(tickers[a + "USDT"]?.lastPrice) || 0);
  const refRate = px(from) && px(to) ? px(from) / px(to) : 0;
  const free = Number(((bal.data as any)?.items ?? (bal.data as any) ?? []).find?.((b: any) => b.asset === from)?.free ?? 0);
  const n = Number(amt);
  const fp = pair?.fromPrecision ?? 8;
  const usdt = n * px(from);
  const minU = Number(cfg?.minUsdt ?? 0), maxU = Number(cfg?.maxUsdt ?? Infinity);
  const amtErr = !amt ? "" : !(n > 0) ? "请输入有效数量" : (amt.split(".")[1]?.length ?? 0) > fp ? `${from} 最多 ${fp} 位小数`
    : px(from) && usdt < minU ? `单笔最少 ${cfg?.minUsdt} USDT 等值` : px(from) && usdt > maxU ? `单笔最多 ${cfg?.maxUsdt} USDT 等值` : authed && n > free ? "可用余额不足" : "";

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
    const r = await rest<Quote>("POST", "/convert/quote", { from, to, fromAmount: amt });
    qBusy.current = false;
    if (r.ok && r.data) { setQuote(r.data); setQErr(""); return; }
    setQuote(null);
    // AMOUNT_TOO_SMALL/LARGE 后端文案已含 min/max USDT；其余按 code 映射
    const c = r.code ?? "";
    setQErr(c === "AMOUNT_TOO_SMALL" || c === "AMOUNT_TOO_LARGE" ? (r.error || (c === "AMOUNT_TOO_SMALL" ? `单笔最少 ${cfg?.minUsdt} USDT 等值` : `单笔最多 ${cfg?.maxUsdt} USDT 等值`)) : ERR[c] ?? r.error ?? "报价失败");
  };
  useEffect(() => { const t = setTimeout(getQuote, 400); return () => clearTimeout(t); }, [from, to, amt, avail, authed]); // eslint-disable-line
  useEffect(() => {
    if (!quote) return;
    const tick = () => { const s = Math.max(0, Math.ceil((quote.expiresAt - Date.now()) / 1000)); setLeft(s); if (s <= 0) getQuote(); };
    tick(); const i = setInterval(tick, 1000); return () => clearInterval(i);
  }, [quote, confirm]); // eslint-disable-line

  const execute = async () => {
    if (!quote) return;
    if (left === 0) { toast("报价已过期，已为你刷新", "info"); setConfirm(false); getQuote(); return; }
    setBusy(true);
    const r = await rest<{ order: Order; replay: boolean }>("POST", "/convert/accept", { quoteId: quote.quoteId });
    setBusy(false);
    if (r.ok && r.data?.order) { setConfirm(false); setDone(r.data.order); setAmt(""); setQuote(null); bal.refetch?.(); setPage(1); loadOrders(); return; }
    if (r.code === "QUOTE_EXPIRED" || r.code === "QUOTE_NOT_FOUND") { toast(ERR[r.code], "info"); getQuote(); return; }
    setConfirm(false);
    toast((r.code && ERR[r.code]) || r.error || "兑换失败", "err");
  };

  const toAmt = quote ? quote.toAmount : refRate && n > 0 ? (n * refRate).toFixed(to === "USDT" ? 2 : 8) : "";
  const canGo = avail === "yes" && authed && !!quote && !amtErr;
  return (
    <main className="light">
      <div className="cvh">
        <h1>闪兑，一步完成币币兑换</h1>
        <p>零手续费，锁定报价 {cfg?.quoteTtlSeconds ?? 10} 秒，即时到账。</p>
        <div className="cvbox">
          <div className="lrow"><b>闪兑 <em className="new">零手续费</em></b><button type="button" className="cv-ref" title="刷新报价" onClick={() => { if (!authed) return toast("请先登录后获取报价", "info"); if (!(n > 0)) return toast("请输入兑换数量", "info"); getQuote(); toast("报价已刷新", "info"); }}>⟳</button></div>
          {avail === "no" && <div className="cv-warn">闪兑暂未开放，以下为行情参考价，暂不支持下单。</div>}
          <div className="cvi">
            <div className="lrow"><span className="lg">从</span>{authed && <span className="lg">可用 {free} {from} <button type="button" className="cv-max" onClick={() => setAmt(String(free))}>最大</button></span>}</div>
            <div className="lrow"><Picker value={from} list={assets} onPick={(s) => pick("from", s)} /><input className="cv-amt" inputMode="decimal" placeholder={cfg ? `${cfg.minUsdt} - ${cfg.maxUsdt} USDT 等值` : "0.00"} value={amt} onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))} /></div>
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
              <tbody>{orders.map((o) => <tr key={o.id}><td>{new Date(o.createdAt).toLocaleString()}</td><td>{o.fromAmount} {o.from}</td><td>{o.toAmount} {o.to}</td><td>{o.rate}</td><td>{o.status === "filled" ? "已完成" : o.status}</td></tr>)}</tbody></table>
          )}
          {total > PAGE && (
            <div className="cv-pg">
              <button type="button" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button>
              <span>{page} / {Math.ceil(total / PAGE)}</span>
              <button type="button" disabled={page >= Math.ceil(total / PAGE)} onClick={() => setPage(page + 1)}>下一页</button>
            </div>
          )}
        </section>
      )}
      {confirm && quote && (
        <div className="fx-modal" aria-busy={busy} onClick={() => setConfirm(false)}>
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
      {done && (
        <div className="fx-modal" onClick={() => setDone(null)}>
          <div className="cv-modal" onClick={(e) => e.stopPropagation()}>
            <h3>兑换成功 ✓</h3>
            <div className="lrow"><span>支付</span><b><CoinIcon s={done.from} size={18} /> {done.fromAmount} {done.from}</b></div>
            <div className="lrow"><span>获得</span><b><CoinIcon s={done.to} size={18} /> {done.toAmount} {done.to}</b></div>
            <div className="lrow lg"><span>成交价</span><span>1 {done.from} = {done.rate} {done.to}</span></div>
            <div className="lrow"><Link to="/assets" className="lb ghost">查看资产</Link><button type="button" className="lb" onClick={() => setDone(null)}>继续兑换</button></div>
          </div>
        </div>
      )}
      <Footer light />
    </main>
  );
}
