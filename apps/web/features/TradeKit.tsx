"use client";
/** 交易页组件：交易对切换、订单簿（视图/精度/累计深度/点价填单）、合约下单面板（模式/杠杆/计算器）。
 *  精度一律来自 /market/symbols 或 /futures/market/contracts，不写死。 */
import { useEffect, useMemo, useRef, useState } from "react";
import { normSymbol } from "./symbol";
import { useNavigate } from "@/lib/router";
import { useAuthStore } from "@/lib/store";
import { rest } from "@/lib/rest";
import { toast } from "@/lib/ui";
import { CoinIcon } from "@/components/CoinIcon";
import type { components } from "@linbit/api-client";
import { fmtFixed, restBase, useOrderBook, useRecentTrades, useSymbols, useTicker, type Level, type SymbolMeta } from "@/lib/market";

/* ---------- 交易对 / 精度 ---------- */
export type Contract = { symbol: string; baseAsset: string; quoteAsset: string; maxLeverage: number; maintenanceMarginRate: string; tickSize: string; minQty: string; makerFeeRate: string; takerFeeRate: string };
const decOf = (s?: string) => (s && s.includes(".") ? s.split(".")[1].replace(/0+$/, "").length : 0);
let ctP: Promise<Contract[]> | null = null;
export function loadContracts() {
  ctP ??= fetch(restBase() + "/futures/market/contracts").then((r) => (r.ok ? r.json() : Promise.reject())).then((j) => j.contracts ?? []).catch(() => ((ctP = null), []));
  return ctP;
}
export function useContracts() {
  const [c, setC] = useState<Contract[]>([]);
  useEffect(() => void loadContracts().then(setC), []);
  return c;
}
export type PairMeta = { symbol: string; base: string; quote: string; pp: number; qp: number; contract?: Contract };
/** 当前交易对：URL ?symbol=，缺省取列表第一个；精度来自接口。 */
/** 路径段 /spot/:symbol、/futures/:symbol、/trade/:symbol 的交易对：大小写与 _ - / 分隔符均可（btc_usdt → BTCUSDT）。 */
const PATH_RE = /^\/(spot|futures|trade)\/[^/]+\/?$/;

export function useTradePair(fut: boolean, initial?: string): [PairMeta, PairMeta[], (s: string) => void] {
  const meta = useSymbols();
  const cts = useContracts();
  const [sym, setSym] = useState(() => normSymbol(initial) || (typeof window === "undefined" ? "" : normSymbol(new URLSearchParams(location.search).get("symbol"))));
  const list: PairMeta[] = fut
    ? cts.map((c) => ({ symbol: c.symbol, base: c.baseAsset, quote: c.quoteAsset, pp: decOf(c.tickSize), qp: decOf(c.minQty), contract: c }))
    : Object.values(meta).map((m: SymbolMeta) => ({ symbol: m.symbol, base: m.base, quote: m.quote, pp: m.pricePrecision, qp: m.quantityPrecision }));
  const cur = list.find((x) => x.symbol === sym) ?? list.find((x) => x.symbol === "BTCUSDT") ?? list[0] ?? { symbol: sym || "BTCUSDT", base: (sym || "BTCUSDT").replace(/USDT$/, ""), quote: "USDT", pp: 2, qp: 4 };
  const pick = (s: string) => {
    setSym(s);
    const u = new URL(location.href);
    if (PATH_RE.test(u.pathname)) u.pathname = u.pathname.replace(/[^/]+\/?$/, s);
    else u.searchParams.set("symbol", s);
    history.replaceState(history.state, "", u);
  };
  // 路径里的交易对不存在时（列表已加载），回退到默认交易对并修正地址栏
  const loaded = list.length > 0;
  useEffect(() => {
    if (!loaded || !sym || list.some((x) => x.symbol === sym)) return;
    const u = new URL(location.href);
    if (PATH_RE.test(u.pathname)) { u.pathname = u.pathname.replace(/[^/]+\/?$/, cur.symbol); history.replaceState(history.state, "", u); }
  }, [loaded, sym]); // eslint-disable-line react-hooks/exhaustive-deps
  return [cur, list, pick];
}
export function PairPicker({ cur, list, pick, fut }: { cur: PairMeta; list: PairMeta[]; pick: (s: string) => void; fut: boolean }) {
  const [o, setO] = useState(false);
  const [q, setQ] = useState("");
  useEffect(() => {
    if (!o) return;
    const c = () => setO(false);
    const k = (e: KeyboardEvent) => e.key === "Escape" && setO(false);
    const t = setTimeout(() => window.addEventListener("click", c));
    window.addEventListener("keydown", k);
    return () => { clearTimeout(t); window.removeEventListener("click", c); window.removeEventListener("keydown", k); };
  }, [o]);
  return (
    <span className="pair tk-pp" data-guide="pair" onClick={() => setO(!o)}>
      <CoinIcon s={cur.base} size={22} /> {fut ? cur.symbol : `${cur.base}/${cur.quote}`} ▾
      {o && (
        <div className="tk-pop" onClick={(e) => e.stopPropagation()}>
          <input autoFocus placeholder="搜索交易对" value={q} onChange={(e) => setQ(e.target.value.toUpperCase())} />
          {list.filter((x) => x.symbol.includes(q)).map((x) => (
            <div key={x.symbol} className={"tk-opt" + (x.symbol === cur.symbol ? " on" : "")} onClick={() => { pick(x.symbol); setO(false); }}>
              <CoinIcon s={x.base} size={18} /> {fut ? x.symbol : `${x.base}/${x.quote}`}
            </div>
          ))}
          {!list.length && <div className="tk-opt g">加载中…</div>}
        </div>
      )}
    </span>
  );
}

/* ---------- 点价填单 ---------- */
const FILL = "lb-fill-price";
export const fillPrice = (p: string) => { window.dispatchEvent(new CustomEvent(FILL, { detail: p })); toast("已填入价格 " + p, "info"); };
export function useFillPrice(set: (p: string) => void) {
  const r = useRef(set); r.current = set;
  useEffect(() => { const h = (e: Event) => r.current((e as CustomEvent).detail); window.addEventListener(FILL, h); return () => window.removeEventListener(FILL, h); }, []);
}

/* ---------- 合约深度（无 WS：3s 轮询，隐藏时暂停） ---------- */
function useFutDepth(sym: string, on: boolean) {
  const [b, setB] = useState<{ bids: Level[]; asks: Level[] } | null>(null);
  useEffect(() => {
    if (!on) return;
    let dead = false;
    const get = () => !document.hidden && fetch(`${restBase()}/futures/market/depth?symbol=${sym}`).then((r) => (r.ok ? r.json() : null)).then((j) => !dead && j && setB({ bids: j.bids ?? [], asks: j.asks ?? [] })).catch(() => {});
    get(); const t = setInterval(get, 3000);
    const v = () => get(); document.addEventListener("visibilitychange", v);
    return () => { dead = true; clearInterval(t); document.removeEventListener("visibilitychange", v); };
  }, [sym, on]);
  return b;
}
function useFutTicker(sym: string, on: boolean) {
  const [t, setT] = useState<any>(null);
  useEffect(() => {
    if (!on) return;
    let dead = false;
    const get = () => !document.hidden && fetch(`${restBase()}/futures/market/ticker?symbol=${sym}`).then((r) => (r.ok ? r.json() : null)).then((j) => !dead && j && setT((j.tickers ?? [j]).find((x: any) => x.symbol === sym) ?? null)).catch(() => {});
    get(); const i = setInterval(get, 5000);
    const v = () => get(); document.addEventListener("visibilitychange", v);
    return () => { dead = true; clearInterval(i); document.removeEventListener("visibilitychange", v); };
  }, [sym, on]);
  return t;
}
export { useFutTicker };

/** 按精度步长聚合价位：卖盘向上取整、买盘向下取整 */
function group(l: Level[], step: number, up: boolean): [number, number][] {
  const m = new Map<number, number>();
  for (const [p, q] of l) {
    const k = (up ? Math.ceil(+p / step - 1e-9) : Math.floor(+p / step + 1e-9)) * step;
    m.set(k, (m.get(k) ?? 0) + +q);
  }
  return [...m.entries()].sort((a, b) => (up ? a[0] - b[0] : b[0] - a[0]));
}
const VIEWS = [
  { k: "both", tip: "买卖盘" },
  { k: "bids", tip: "买盘" },
  { k: "asks", tip: "卖盘" },
] as const;
function ViewIcon({ k }: { k: string }) {
  const r = (y: number, c: string) => <rect x="2" y={y} width="12" height="5" rx="1" fill={c} />;
  return <svg width="16" height="16" viewBox="0 0 16 16">{k === "both" ? <>{r(2, "#f43f5e")}{r(9, "#abe127")}</> : k === "bids" ? <>{r(2, "#abe127")}{r(9, "#abe127")}</> : <>{r(2, "#f43f5e")}{r(9, "#f43f5e")}</>}</svg>;
}

export function OrderBookPanel({ fut, pair }: { fut: boolean; pair: PairMeta }) {
  const { pp, qp, base, symbol } = pair;
  const [tab, setTab] = useState(0);
  const [view, setView] = useState<"both" | "bids" | "asks">("both");
  const steps = useMemo(() => [0, 1, 2, 3].map((i) => 10 ** (i - pp)), [pp]);
  const [si, setSi] = useState(0);
  const [po, setPo] = useState(false);
  useEffect(() => setSi(0), [symbol]);
  const step = steps[si];
  const sd = Math.max(0, -Math.round(Math.log10(step)));
  const spot = useOrderBook(fut ? "" : symbol).book;
  const fb = useFutDepth(symbol, fut);
  const book = fut ? fb : spot;
  const { ticker } = useTicker(fut ? "" : symbol);
  const ft = useFutTicker(symbol, fut);
  const { trades } = useRecentTrades(fut ? "" : symbol);
  const n = view === "both" ? 9 : 18;
  const build = (l: Level[], up: boolean) => {
    const g = group(l, step, up).slice(0, n);
    let c = 0;
    const rows = g.map(([p, q]) => ((c += q), { p, q, c }));
    return { rows, max: c || 1 };
  };
  const A = build(book?.asks ?? [], true), B = build(book?.bids ?? [], false);
  const bestA = A.rows[0]?.p, bestB = B.rows[0]?.p;
  const lastRaw = fut ? (Number(ft?.lastPrice) ? ft.lastPrice : ft?.markPrice) : Number(ticker?.lastPrice) ? ticker!.lastPrice : undefined;
  const mid = lastRaw ?? (bestA && bestB ? (bestA + bestB) / 2 : undefined);
  const chg = Number(fut ? ft?.priceChangePercent24h : ticker?.priceChangePercent) || 0;
  const row = (r: { p: number; q: number; c: number }, max: number, side: "a" | "b") => (
    <div key={side + r.p} className="obr tk-r" onClick={() => fillPrice(r.p.toFixed(sd))} title="点击填入价格">
      <i style={{ width: (r.c / max) * 100 + "%", background: side === "a" ? "rgba(244,63,94,.12)" : "rgba(171,225,39,.1)" }} />
      <span className={side === "a" ? "down" : "up"}>{fmtFixed(r.p, sd)}</span><span>{fmtFixed(r.q, qp, false)}</span><span>{fmtFixed(r.c, qp, false)}</span>
    </div>
  );
  const empty = <div className="tk-empty">暂无挂单</div>;
  return (
    <div data-guide="book" className="ob">
      <div className="pt">{["订单簿", "最新成交"].map((x, i) => <b key={x} className={tab === i ? "on" : ""} onClick={() => setTab(i)}>{x}</b>)}</div>
      {tab === 0 ? (
        <>
          <div className="obt">
            <span className="tk-views">{VIEWS.map((v) => <button key={v.k} className={"tk-vb" + (view === v.k ? " on" : "")} data-tip={v.tip} aria-label={v.tip} onClick={() => setView(v.k)}><ViewIcon k={v.k} /></button>)}</span>
            <span className="tk-prec" onClick={() => setPo(!po)}>{step.toFixed(sd)} ▾
              {po && <div className="tk-pop sm" onClick={(e) => e.stopPropagation()}>{steps.map((s, i) => <div key={s} className={"tk-opt" + (i === si ? " on" : "")} onClick={() => { setSi(i); setPo(false); }}>{s.toFixed(Math.max(0, -Math.round(Math.log10(s))))}</div>)}</div>}
            </span>
          </div>
          <div className="obh"><span>价格({pair.quote})</span><span>数量({base})</span><span>累计({base})</span></div>
          {view !== "bids" && <div className={"tk-side" + (view === "asks" ? " full" : "")}>{A.rows.length ? [...A.rows].reverse().map((r) => row(r, A.max, "a")) : empty}</div>}
          <div className={"obm tk-mid " + (chg >= 0 ? "up" : "down")} onClick={() => mid ? fillPrice(Number(mid).toFixed(pp)) : toast("暂无最新成交价", "info")}>
            {mid != null ? fmtFixed(mid, pp) : "--"} <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true" style={{ transform: chg >= 0 ? undefined : "rotate(180deg)", verticalAlign: "-1px" }}><path d="M12 19V5M6 11l6-6 6 6" /></svg>
            {fut && ft?.markPrice && <small className="g"> 标记 {fmtFixed(ft.markPrice, pp)}</small>}
          </div>
          {view !== "asks" && <div className={"tk-side" + (view === "bids" ? " full" : "")}>{B.rows.length ? B.rows.map((r) => row(r, B.max, "b")) : empty}</div>}
        </>
      ) : (
        <>
          <div className="obh"><span>价格({pair.quote})</span><span>数量({base})</span><span>时间</span></div>
          {!fut && trades.length ? trades.slice(0, 24).map((t) => (
            <div key={t.tradeId} className="obr tk-r" onClick={() => fillPrice(Number(t.price).toFixed(pp))}><span className={t.side === "buy" ? "up" : "down"}>{fmtFixed(t.price, pp)}</span><span>{fmtFixed(t.quantity, qp, false)}</span><span>{new Date(t.time).toLocaleTimeString("en-GB", { hour12: false })}</span></div>
          )) : <div className="tk-empty">暂无成交</div>}
        </>
      )}
    </div>
  );
}

/* ---------- 合约下单面板 ---------- */
type Mode = { margin: "cross" | "isolated"; pos: "merged" | "split" };
const LS = (s: string) => `lb_fut_${s}`;
const readLocal = (s: string): { lev: number; mode: Mode } => {
  try { const j = JSON.parse(localStorage.getItem(LS(s)) || ""); if (j?.lev) return j; } catch { /* */ }
  return { lev: 20, mode: { margin: "cross", pos: "merged" } };
};
export function Modal({ title, onClose, children, foot }: { title: string; onClose: () => void; children: React.ReactNode; foot?: React.ReactNode }) {
  useEffect(() => { const k = (e: KeyboardEvent) => e.key === "Escape" && onClose(); window.addEventListener("keydown", k); return () => window.removeEventListener("keydown", k); }, [onClose]);
  return (
    <div className="fx-modal" onClick={onClose}>
      <div className="tk-modal" onClick={(e) => e.stopPropagation()}>
        <div className="tk-mh"><b>{title}</b><span className="tk-x" onClick={onClose}>✕</span></div>
        {children}
        {foot && <div className="tk-mf">{foot}</div>}
      </div>
    </div>
  );
}
function Opt({ on, t, d, onClick }: { on: boolean; t: string; d?: string; onClick: () => void }) {
  return <div className={"tk-opt2" + (on ? " on" : "")} onClick={onClick}><b>{t}</b>{d && <small>{d}</small>}{on && <i className="tk-ck">✓</i>}</div>;
}

// tp/slTriggerPriceType 依赖 feat/futures-order-amend2 的 openapi（FuturesPlaceOrderRequest 新增字段，默认 MARK）；
// 该分支合入并重新生成 schema 后，删掉这里的补充字段，直接用生成的类型。
type TrigType = "LAST" | "MARK";
type PlaceReq = components["schemas"]["FuturesPlaceOrderRequest"] & { tpTriggerPriceType?: TrigType; slTriggerPriceType?: TrigType };
type Tif = "GTC" | "IOC" | "POST_ONLY";
const TIF: [Tif, string][] = [["GTC", "GTC"], ["IOC", "IOC"], ["POST_ONLY", "只做 Maker"]];

export function FutForm({ pair }: { pair: PairMeta }) {
  const nav = useNavigate();
  const authed = useAuthStore((s) => !!s.accessToken);
  const c = pair.contract;
  const maxLev = c?.maxLeverage ?? 100;
  const [st, setSt] = useState(() => (typeof window === "undefined" ? { lev: 20, mode: { margin: "cross", pos: "merged" } as Mode } : readLocal(pair.symbol)));
  const [act, setAct] = useState<0 | 1>(0);
  const [ty, setTy] = useState<"limit" | "market" | "plan">("limit");
  const [price, setPrice] = useState("");
  const [trig, setTrig] = useState("");
  const [qty, setQty] = useState("");
  const [pc, setPc] = useState(0);
  const [modal, setModal] = useState<"" | "mode" | "lev" | "calc">("");
  const [busy, setBusy] = useState(false);
  const [avail, setAvail] = useState<string | null>(null);
  const [tif, setTif] = useState<Tif>("GTC");
  const [trigType, setTrigType] = useState<"LAST" | "MARK">("LAST");
  const [tpsl, setTpsl] = useState(false);
  const [tp, setTp] = useState("");
  const [sl, setSl] = useState("");
  const [tpTT, setTpTT] = useState<"LAST" | "MARK">("MARK");
  const [slTT, setSlTT] = useState<"LAST" | "MARK">("MARK");
  useFillPrice((p) => { setPrice(p); if (ty === "market") setTy("limit"); });
  useEffect(() => { setSt(readLocal(pair.symbol)); setPrice(""); }, [pair.symbol]);
  const save = (n: typeof st) => { setSt(n); localStorage.setItem(LS(pair.symbol), JSON.stringify(n)); };
  // 已登录：读取后端杠杆/保证金模式 + 合约账户可用
  useEffect(() => {
    if (!authed) return setAvail(null);
    rest<any>("GET", `/futures/leverage?symbol=${pair.symbol}`).then((r) => {
      if (r.ok && r.data?.leverage) save({ lev: +r.data.leverage, mode: { ...readLocal(pair.symbol).mode, margin: r.data.marginMode === "isolated" ? "isolated" : "cross" } });
    });
    rest<components["schemas"]["FuturesPositionMode"]>("GET", "/futures/position-mode").then((r) => {
      if (r.ok && r.data?.positionMode) { const cur = readLocal(pair.symbol); save({ ...cur, mode: { ...cur.mode, pos: String(r.data.positionMode).toUpperCase() === "HEDGE" ? "split" : "merged" } }); }
    });
    rest<any>("GET", "/futures/account").then((r) => r.ok && setAvail(String(r.data?.availableBalance ?? r.data?.available ?? r.data?.availableMargin ?? "0")));
  }, [authed, pair.symbol]); // eslint-disable-line
  const pushLev = async (lev: number, margin: Mode["margin"]) => {
    if (!authed) return true;
    const r = await rest("POST", "/futures/leverage", { symbol: pair.symbol, leverage: lev, marginMode: margin });
    if (!r.ok) { toast(/HAS_POSITION|HAS_OPEN_ORDERS/.test(r.error ?? "") ? "存在持仓或挂单，无法调整" : r.error || "设置失败", "err"); return false; }
    return true;
  };
  /** 单向/双向持仓：POST /futures/position-mode（需无持仓、无挂单、无计划/止盈止损委托） */
  const pushPosMode = async (pos: Mode["pos"]) => {
    if (!authed) return true;
    const r = await rest("POST", "/futures/position-mode", { positionMode: pos === "split" ? "HEDGE" : "ONE_WAY" });
    if (r.ok) return true;
    const e = r.error ?? "";
    toast(/HAS_POSITION|HAS_OPEN_ORDERS/.test(e) ? "存在持仓、挂单或条件单，无法切换持仓模式" : /COPY_TRADER/.test(e) ? "带单交易员仅支持单向持仓" : /HEDGE_MODE_DISABLED/.test(e) ? "双向持仓暂未开放" : e || "切换失败", "err");
    return false;
  };
  const qn = Number(qty) || 0;
  const px = ty === "market" ? Number(price) || 0 : Number(price) || 0;
  const notional = qn * px;
  const margin = st.lev ? notional / st.lev : 0;
  const fee = notional * Number(c?.takerFeeRate ?? 0.0005);
  const submit = async (side: "buy" | "sell") => {
    if (!authed) return nav("/login");
    if (!qn) return toast("请输入数量", "err");
    if (ty !== "market" && !Number(price)) return toast("请输入价格", "err");
    if (ty === "plan" && !Number(trig)) return toast("请输入触发价", "err");
    const hedge = st.mode.pos === "split";
    // 双向持仓：开多/平多 → LONG，开空/平空 → SHORT；单向持仓用 reduceOnly 表示平仓
    const positionSide: PlaceReq["positionSide"] = hedge ? (act === 0 ? (side === "buy" ? "LONG" : "SHORT") : (side === "buy" ? "SHORT" : "LONG")) : undefined;
    const isLimit = ty === "limit" || (ty === "plan" && !!Number(price));
    const body: PlaceReq = {
      symbol: pair.symbol, side, qty,
      type: isLimit ? "limit" : "market",
      timeInForce: ty === "limit" ? tif : isLimit ? "GTC" : "IOC",
      price: isLimit ? price : undefined,
      reduceOnly: !hedge && act === 1 ? true : undefined,
      positionSide,
      triggerPrice: ty === "plan" ? trig : undefined,
      triggerPriceType: ty === "plan" ? trigType : undefined,
      tpPrice: act === 0 && tpsl && tp ? tp : undefined,
      slPrice: act === 0 && tpsl && sl ? sl : undefined,
      tpTriggerPriceType: act === 0 && tpsl && tp ? tpTT : undefined,
      slTriggerPriceType: act === 0 && tpsl && sl ? slTT : undefined,
    };
    setBusy(true);
    const r = await rest<any>("POST", "/futures/orders", body);
    setBusy(false);
    if (!r.ok) {
      const e = r.error ?? "";
      const M: Record<string, string> = { POST_ONLY_REJECTED: "只做 Maker 订单会立即成交，已被拒绝", POST_ONLY_DISABLED: "只做 Maker 暂未开放", TRIGGER_DEVIATION: "触发价偏离当前价格过大", ALGO_LIMIT: "条件单数量已达上限", INVALID_TPSL: "止盈止损价格不合理", POSITION_SIDE_REQUIRED: "双向持仓需指定仓位方向", POSITION_SIDE_MISMATCH: "仓位方向与持仓模式不一致", REDUCE_ONLY_REJECTED: "无可平仓位" };
      const k = Object.keys(M).find((x) => e.includes(x));
      return toast(k ? M[k] : e || "下单失败", "err");
    }
    toast(ty === "plan" ? "计划委托已提交" : "下单成功"); setQty(""); window.dispatchEvent(new Event("lb-fut-orders"));
  };
  const setByPct = (p: number) => {
    setPc(p);
    const a = Number(avail) || 0;
    if (a && px) setQty(((a * st.lev * p) / 100 / px).toFixed(pair.qp));
  };
  const L = st.mode;
  return (
    <div data-guide="form" className="of">
      <div className="selrow tk-sel">
        <button className="tk-chip" onClick={() => setModal("mode")}>{L.margin === "cross" ? "全仓" : "逐仓"} {L.pos === "merged" ? "单向" : "双向"}</button>
        <button className="tk-chip long" onClick={() => setModal("lev")}>{st.lev}x</button>
        <button className="tk-chip short" onClick={() => setModal("lev")}>{st.lev}x</button>
        <button className="tk-chip ic" data-tip="计算器" aria-label="计算器" onClick={() => setModal("calc")}>
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="3" y="1.5" width="10" height="13" rx="1.5" /><path d="M5.5 4.5h5M5.5 8h1M9.5 8h1M5.5 11h1M9.5 11h1" /></svg>
        </button>
      </div>
      <div className="seg">{["开仓", "平仓"].map((x, i) => <b key={x} className={act === i ? "on" : ""} onClick={() => setAct(i as 0 | 1)}>{x}</b>)}</div>
      <div className="pt sm">{([["limit", "限价"], ["market", "市价"], ["plan", "计划委托"]] as const).map(([k, x]) => <b key={k} className={ty === k ? "on" : ""} onClick={() => setTy(k)}>{x}</b>)}</div>
      <div className="kv2"><span>可用</span><b>{avail != null ? fmtFixed(avail, 4) : "--"} USDT</b></div>
      {ty === "plan" && <div className="inp"><small>触发价</small><input inputMode="decimal" value={trig} onChange={(e) => setTrig(e.target.value.replace(/[^\d.]/g, ""))} /><select className="tk-tt" aria-label="触发价类型" value={trigType} onChange={(e) => setTrigType(e.target.value as "LAST" | "MARK")}><option value="LAST">最新价</option><option value="MARK">标记价</option></select></div>}
      {ty !== "market" ? <div className="inp"><small>{ty === "plan" ? "委托价" : "价格"}</small><input inputMode="decimal" placeholder={ty === "plan" ? "留空则市价" : ""} value={price} onChange={(e) => setPrice(e.target.value.replace(/[^\d.]/g, ""))} /><em>USDT</em></div> : <div className="inp dis"><small>价格</small><input disabled value="市价" /></div>}
      <div className="inp"><small>数量</small><input inputMode="decimal" value={qty} onChange={(e) => { setQty(e.target.value.replace(/[^\d.]/g, "")); setPc(0); }} /><em>{pair.base}</em></div>
      <div className="slider lb-sl"><div className="line" /><div className="fill" style={{ width: pc + "%" }} />{[0, 25, 50, 75, 100].map((p) => <span key={p} className={pc >= p ? "on" : ""} style={{ left: p + "%" }} onClick={() => setByPct(p)}><i /><small>{p}%</small></span>)}<input type="range" min="0" max="100" value={pc} onChange={(e) => setByPct(+e.target.value)} aria-label="比例" /></div>
      {ty === "limit" && <div className="tk-tif">{TIF.map(([k, x]) => <b key={k} className={tif === k ? "on" : ""} onClick={() => setTif(k)}>{x}</b>)}</div>}
      {act === 0 && <label className="tk-ck2"><input type="checkbox" checked={tpsl} onChange={(e) => setTpsl(e.target.checked)} />止盈/止损</label>}
      {act === 0 && tpsl && <div className="tk-tp2"><div className="inp"><small>止盈</small><input inputMode="decimal" placeholder="触发价" value={tp} onChange={(e) => setTp(e.target.value.replace(/[^\d.]/g, ""))} /><select className="tk-tt" aria-label="止盈触发价类型" value={tpTT} onChange={(e) => setTpTT(e.target.value as "LAST" | "MARK")}><option value="MARK">标记价</option><option value="LAST">最新价</option></select></div><div className="inp"><small>止损</small><input inputMode="decimal" placeholder="触发价" value={sl} onChange={(e) => setSl(e.target.value.replace(/[^\d.]/g, ""))} /><select className="tk-tt" aria-label="止损触发价类型" value={slTT} onChange={(e) => setSlTT(e.target.value as "LAST" | "MARK")}><option value="MARK">标记价</option><option value="LAST">最新价</option></select></div></div>}
      <div className="kv2 g"><span>保证金</span><span>{fmtFixed(margin, 4)} USDT</span></div>
      <div className="kv2 g"><span>预估手续费（{(Number(c?.takerFeeRate ?? 0.0005) * 100).toFixed(2)}%）</span><span>{fmtFixed(fee, 4)} USDT</span></div>
      {authed ? (
        <div className="tk-btns">
          <button className="lbtn tk-long" disabled={busy} onClick={() => submit(act === 0 ? "buy" : "buy")}>{act === 0 ? "开多" : "平空"}</button>
          <button className="lbtn tk-short" disabled={busy} onClick={() => submit("sell")}>{act === 0 ? "开空" : "平多"}</button>
        </div>
      ) : <button className="lbtn" onClick={() => nav("/login")}>登录/注册</button>}

      {modal === "mode" && <ModeModal mode={L} onClose={() => setModal("")} onOk={async (m) => { if (m.margin !== L.margin && !(await pushLev(st.lev, m.margin))) return; if (m.pos !== L.pos && !(await pushPosMode(m.pos))) return; save({ ...st, mode: m }); setModal(""); toast("交易模式已更新"); }} />}
      {modal === "lev" && <LevModal lev={st.lev} max={maxLev} onClose={() => setModal("")} onOk={async (v) => { if (!(await pushLev(v, L.margin))) return; save({ ...st, lev: v }); setModal(""); toast(`杠杆已调整为 ${v}x`); }} />}
      {modal === "calc" && <CalcModal pair={pair} lev={st.lev} max={maxLev} onClose={() => setModal("")} />}
    </div>
  );
}

function ModeModal({ mode, onClose, onOk }: { mode: Mode; onClose: () => void; onOk: (m: Mode) => void }) {
  const [m, setM] = useState(mode);
  return (
    <Modal title="调整交易模式" onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" onClick={() => onOk(m)}>确认</button></>}>
      <p className="tk-lbl">保证金模式</p>
      <div className="tk-pair"><Opt on={m.margin === "cross"} t="全仓" onClick={() => setM({ ...m, margin: "cross" })} /><Opt on={m.margin === "isolated"} t="逐仓" onClick={() => setM({ ...m, margin: "isolated" })} /></div>
      <p className="tk-txt">全仓：所有仓位共享合约账户的可用余额作为保证金，盈亏可相互抵消，强平时可能损失全部可用余额。逐仓：每个仓位单独分配保证金，最大损失限于该仓位保证金。</p>
      <p className="tk-lbl">持仓模式</p>
      <div className="tk-pair"><Opt on={m.pos === "merged"} t="单向持仓" onClick={() => setM({ ...m, pos: "merged" })} /><Opt on={m.pos === "split"} t="双向持仓" onClick={() => setM({ ...m, pos: "split" })} /></div>
      <p className="tk-txt">单向持仓：同一合约只持有一个方向的仓位，反向下单会先减仓。双向持仓：同一合约可同时持有多仓和空仓，开多/开空、平多/平空分别对应各自仓位。</p>
      <p className="tk-txt g">存在持仓、挂单或计划/止盈止损委托时无法切换持仓模式。</p>
    </Modal>
  );
}

function LevModal({ lev, max, onClose, onOk }: { lev: number; max: number; onClose: () => void; onOk: (v: number) => void }) {
  const [v, setV] = useState(lev);
  const clamp = (x: number) => Math.max(1, Math.min(max, Math.round(x) || 1));
  const presets = [1, 5, 10, 20, 50, 100, 125, 150, 200].filter((x) => x <= max);
  return (
    <Modal title="调整杠杆" onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" onClick={() => onOk(clamp(v))}>确认</button></>}>
      <div className="tk-levin"><button onClick={() => setV(clamp(v - 1))}>−</button><input inputMode="numeric" value={v} onChange={(e) => setV(Number(e.target.value.replace(/\D/g, "")) || 1)} onBlur={() => setV(clamp(v))} /><span>x</span><button onClick={() => setV(clamp(v + 1))}>+</button></div>
      <input className="tk-range" type="range" min={1} max={max} value={v} onChange={(e) => setV(+e.target.value)} aria-label="杠杆" />
      <div className="tk-presets">{presets.map((p) => <span key={p} className={v === p ? "on" : ""} onClick={() => setV(p)}>{p}x</span>)}</div>
      <p className="tk-txt">最大杠杆 {max}x。杠杆越高，强平风险越大，请谨慎设置。</p>
      {v >= 50 && <p className="tk-txt down">当前杠杆较高，价格小幅波动即可能触发强平。</p>}
    </Modal>
  );
}

function CalcModal({ pair, lev: l0, max, onClose }: { pair: PairMeta; lev: number; max: number; onClose: () => void }) {
  const [tab, setTab] = useState(0);
  const [side, setSide] = useState<"long" | "short">("long");
  const [lev, setLev] = useState(l0);
  const [entry, setEntry] = useState("");
  const [exit, setExit] = useState("");
  const [qty, setQty] = useState("");
  const [roe, setRoe] = useState("");
  const e = +entry || 0, x = +exit || 0, q = +qty || 0, s = side === "long" ? 1 : -1;
  const mmr = Number(pair.contract?.maintenanceMarginRate ?? 0.004);
  const im = lev ? (e * q) / lev : 0;
  const pnl = (x - e) * q * s;
  const tgt = e && lev ? e * (1 + (s * (+roe || 0)) / 100 / lev) : 0;
  const liq = e && lev ? e * (1 - s / lev + s * mmr) : 0;
  const n = (k: string, v: string, set: (v: string) => void, u = "USDT") => <div className="inp"><small>{k}</small><input inputMode="decimal" value={v} onChange={(ev) => set(ev.target.value.replace(/[^\d.]/g, ""))} /><em>{u}</em></div>;
  return (
    <Modal title="计算器" onClose={onClose}>
      <div className="pt sm">{["收益", "目标价格", "强平价格"].map((t, i) => <b key={t} className={tab === i ? "on" : ""} onClick={() => setTab(i)}>{t}</b>)}</div>
      <div className="seg">{(["long", "short"] as const).map((k) => <b key={k} className={side === k ? "on" : ""} onClick={() => setSide(k)}>{k === "long" ? "做多" : "做空"}</b>)}</div>
      <div className="kv2"><span>杠杆</span><span><input className="tk-levsm" value={lev} onChange={(ev) => setLev(Math.max(1, Math.min(max, +ev.target.value.replace(/\D/g, "") || 1)))} />x</span></div>
      {n("开仓价格", entry, setEntry)}
      {tab === 0 && <>{n("平仓价格", exit, setExit)}{n("数量", qty, setQty, pair.base)}</>}
      {tab === 1 && n("回报率", roe, setRoe, "%")}
      {tab === 2 && n("数量", qty, setQty, pair.base)}
      <div className="tk-res">
        {tab === 0 && <><div className="kv2"><span>起始保证金</span><b>{fmtFixed(im, 4)} USDT</b></div><div className="kv2"><span>收益</span><b className={pnl >= 0 ? "up" : "down"}>{fmtFixed(pnl, 4)} USDT</b></div><div className="kv2"><span>回报率</span><b className={pnl >= 0 ? "up" : "down"}>{im ? fmtFixed((pnl / im) * 100, 2) : "0.00"}%</b></div></>}
        {tab === 1 && <div className="kv2"><span>目标价格</span><b>{tgt ? fmtFixed(tgt, pair.pp) : "--"} USDT</b></div>}
        {tab === 2 && <div className="kv2"><span>预估强平价格（逐仓）</span><b>{liq > 0 ? fmtFixed(liq, pair.pp) : "--"} USDT</b></div>}
      </div>
      <p className="tk-txt g">计算结果仅供参考，未计入手续费与资金费用。维持保证金率 {(mmr * 100).toFixed(2)}%。</p>
    </Modal>
  );
}

/** 合约 K 线：GET /futures/market/klines（无 WS，订阅时 5s 轮询，页面隐藏暂停）。
 *  兼容数组或 { source, items|klines }；顶层 source==='index' 时整段按“无成交”样式绘制。 */
type FutK = { time?: number; openTime?: number; open: string; high: string; low: string; close: string; volume: string; source?: string };
const futFeeds: Record<string, ReturnType<typeof mkFutFeed>> = {};
function mkFutFeed(symbol: string) {
  const load = async (interval: string) => {
    try {
      const r = await fetch(`${restBase()}/futures/market/klines?symbol=${symbol}&interval=${interval}&limit=300`);
      if (!r.ok) return null;
      const j = await r.json();
      const raw = j?.data ?? j;
      const top: string | undefined = Array.isArray(raw) ? undefined : raw?.source;
      const l: FutK[] = Array.isArray(raw) ? raw : raw?.items ?? raw?.klines ?? [];
      return l
        .map((k) => {
          const t = Number(k.time ?? k.openTime);
          return { time: t > 1e12 ? Math.floor(t / 1000) : t, open: +k.open, high: +k.high, low: +k.low, close: +k.close, v: +k.volume, src: k.source ?? (top && top !== "trade" ? "index" : undefined) };
        })
        .sort((a, b) => a.time - b.time);
    } catch { return null; }
  };
  return {
    load,
    subscribe(interval: string, cb: (b: { time: number; open: number; high: number; low: number; close: number; v: number; src?: string }) => void) {
      const id = setInterval(async () => { if (document.hidden) return; const l = await load(interval); if (l?.length) cb(l[l.length - 1]); }, 5000);
      return () => clearInterval(id);
    },
  };
}
export const futFeedOf = (s: string) => (futFeeds[s] ??= mkFutFeed(s));

/** 非成交价提示：priceSource 不是 trade 时显示“参考价” */
export function RefHint({ src }: { src?: string }) {
  if (!src || src === "trade") return null;
  const why: Record<string, string> = { mid: "盘口中间价", index: "指数价格", mark: "标记价格", none: "暂无价格" };
  return <span className="tk-ref" data-tip={`暂无成交，展示${why[src] ?? "参考价"}；24h 数据为 0`}>参考价</span>;
}
