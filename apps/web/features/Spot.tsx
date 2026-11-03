"use client";
/** 现货下单面板与委托列表：对接 /spot/*（@linbit/api-client），API 不可达时回退本地 mock */
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "@/lib/router";
import { useAuthStore } from "@/lib/store";
import {
  cancelSpotOrder, listMyTrades, listOpenOrders, listOrderHistory, placeSpotOrder,
  type Order, type SpotTrade,
} from "@/lib/services";
import { SPOT_FEE } from "@/lib/fees";
import { Src } from "./common";
import { useFillPrice, type PairMeta } from "./TradeKit";
import { useTicker } from "@/lib/market";
import { useBalances } from "@/lib/queries";

const EVT = "lb-spot-orders";
const bump = () => window.dispatchEvent(new Event(EVT));
const num = (v: string) => v.replace(/[^\d.]/g, "");

export function SpotForm({ pair }: { pair: PairMeta }) {
  const SYMBOL = pair.symbol, B = pair.base;
  const { ticker } = useTicker(SYMBOL);
  const last = Number(ticker?.lastPrice) || 0;
  const nav = useNavigate();
  const authed = useAuthStore((s) => !!s.accessToken);
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [type, setType] = useState<"limit" | "market">("limit");
  const [price, setPrice] = useState("");
  useFillPrice((p) => { setPrice(p); setType("limit"); });
  useEffect(() => { setPrice(""); setQty(""); }, [SYMBOL]);
  const fix = (v: string, d: number) => { const [a, b] = v.split("."); return b === undefined ? a : a + "." + b.slice(0, d); };
  const [qty, setQty] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; t: string }>();
  const [busy, setBusy] = useState(false);
  const q = Number(qty) || 0;
  const px = type === "limit" ? Number(price) || 0 : last;
  const bal = useBalances();
  const freeOf = (a: string) => Number(((bal.data as any)?.items || []).find((x: any) => x.asset === a)?.free) || 0;
  const avail = side === "buy" ? freeOf("USDT") : freeOf(B);
  const [pc, setPc] = useState(0);
  useEffect(() => setPc(0), [side, SYMBOL]);
  const setByPct = (p: number) => {
    setPc(p);
    if (!avail) return;
    const qn = side === "buy" ? (px ? (avail * p) / 100 / px : 0) : (avail * p) / 100;
    setQty(qn ? (Math.floor(qn * 10 ** pair.qp) / 10 ** pair.qp).toFixed(pair.qp) : "");
  };
  const fee = side === "buy" ? `${(q * SPOT_FEE.taker).toFixed(pair.qp + 2)} ${B}` : `${(q * px * SPOT_FEE.taker).toFixed(4)} USDT`;

  const submit = async () => {
    if (!authed) return nav("/login");
    if (!q) return setMsg({ ok: false, t: "请输入数量" });
    if (type === "market" && !last) return setMsg({ ok: false, t: "暂无市价，请使用限价单" });
    if (type === "limit" && !Number(price)) return setMsg({ ok: false, t: "请输入价格" });
    setBusy(true);
    const r = await placeSpotOrder({
      symbol: SYMBOL, side, type, quantity: qty,
      price: type === "limit" ? price : null,
      timeInForce: type === "limit" ? "GTC" : "IOC",
      clientOrderId: `web-${Date.now()}`,
    });
    setBusy(false);
    setMsg(r.ok ? { ok: true, t: `下单成功 #${r.data?.orderId}（${r.data?.status}）` } : { ok: false, t: r.error ?? "下单失败" });
    if (r.ok) { setQty(""); bump(); }
  };

  return (
    <div data-guide="form" className="of">
      <div className="sp-side" role="tablist">
        {(["buy", "sell"] as const).map((s) => <button key={s} role="tab" aria-selected={side === s} className={(side === s ? "on " : "") + s} onClick={() => setSide(s)}>{s === "buy" ? "买入" : "卖出"}</button>)}
      </div>
      <div className="pt sm">
        {(["limit", "market"] as const).map((t) => <b key={t} className={type === t ? "on" : ""} onClick={() => setType(t)}>{t === "limit" ? "限价" : "市价"}</b>)}
      </div>
      {type === "limit" && <div className="inp"><small>价格(USDT)</small><input inputMode="decimal" value={price} onChange={(e) => setPrice(fix(num(e.target.value), pair.pp))} /></div>}
      <div className="inp"><small>数量({B})</small><input inputMode="decimal" value={qty} onChange={(e) => setQty(fix(num(e.target.value), pair.qp))} /><em>{B}</em></div>
      <div className={"slider lb-sl sp-sl " + side}><div className="line" /><div className="fill" style={{ width: pc + "%" }} />{[0, 25, 50, 75, 100].map((p) => <span key={p} className={pc >= p ? "on" : ""} style={{ left: p + "%" }} onClick={() => setByPct(p)}><i /><small>{p}%</small></span>)}<input type="range" min="0" max="100" value={pc} onChange={(e) => setByPct(+e.target.value)} aria-label="下单比例" /></div>
      <div className="kv2 g"><span>可用</span><span>{authed ? `${avail} ${side === "buy" ? "USDT" : B}` : "--"}</span></div>
      <div className="kv2 g"><span>{type === "limit" ? "成交额" : "预估成交额"}</span><span>{(q * px).toFixed(2)} USDT</span></div>
      <div className="kv2 g"><span>预估手续费（0.1%）</span><span>{fee}</span></div>
      {msg && <div className={msg.ok ? "fx-ok" : "fx-err"} style={{ fontSize: 12, margin: "6px 0" }}>{msg.t}</div>}
      <button className="lbtn" disabled={busy} onClick={submit}>{authed ? (side === "buy" ? `买入 ${B}` : `卖出 ${B}`) : "登录/注册"}</button>
    </div>
  );
}

const TABS = ["当前委托", "历史委托", "成交明细"] as const;
const ST: Record<string, string> = { new: "未成交", partially_filled: "部分成交", filled: "完全成交", canceled: "已撤销", rejected: "已拒绝" };
const t = (iso: string) => new Date(iso).toLocaleString("zh-CN", { hour12: false });

export function SpotOrders({ symbol: SYMBOL }: { symbol: string }) {
  const [tab, setTab] = useState(0);
  const [rows, setRows] = useState<(Order | SpotTrade)[]>([]);
  const [src, setSrc] = useState<"api" | "mock">();
  const [err, setErr] = useState("");
  const authed = useAuthStore((s) => !!s.accessToken);

  const load = useCallback(async () => {
    if (!authed) { setRows([]); return; }
    const r = await (tab === 0 ? listOpenOrders(SYMBOL) : tab === 1 ? listOrderHistory(SYMBOL) : listMyTrades(SYMBOL));
    setSrc(r.source);
    setErr(r.ok ? "" : r.error ?? "加载失败");
    setRows((r.data?.items as (Order | SpotTrade)[]) ?? []);
  }, [tab, authed, SYMBOL]);

  useEffect(() => { load(); window.addEventListener(EVT, load); return () => window.removeEventListener(EVT, load); }, [load]);

  const cancel = async (o: Order) => {
    const r = await cancelSpotOrder(o.orderId, o.symbol);
    if (!r.ok) setErr(r.error ?? "撤单失败");
    bump();
  };

  const head = tab === 2
    ? ["时间", "交易对", "方向", "成交价", "成交数量", "手续费", "角色"]
    : ["时间", "交易对", "类型", "方向", "价格", "数量", "已成交", "状态", tab === 0 ? "操作" : ""];

  return (
    <div className="orders" data-guide="orders">
      <div className="pt sm">
        {TABS.map((x, i) => <b key={x} className={tab === i ? "on" : ""} onClick={() => setTab(i)}>{x}{i === 0 && tab === 0 ? `(${rows.length})` : ""}</b>)}
        <span className="sp" />{src && <Src s={src} />}
      </div>
      {err && <div className="fx-err" style={{ margin: "8px 12px" }}>{err}</div>}
      {!rows.length ? <p className="empty">{authed ? "暂无数据" : "登录后查看委托"}</p> : (
        <table className="sotbl">
          <thead><tr>{head.map((h) => <th key={h}>{h}</th>)}</tr></thead>
          <tbody>
            {tab === 2
              ? (rows as SpotTrade[]).map((r) => (
                <tr key={r.tradeId}><td>{t(r.createdAt)}</td><td>{r.symbol}</td><td className={r.side === "buy" ? "up" : "down"}>{r.side === "buy" ? "买入" : "卖出"}</td><td>{r.price}</td><td>{r.quantity}</td><td>{r.fee ?? "-"} {r.feeAsset ?? ""}</td><td>{r.role === "maker" ? "Maker" : "Taker"}</td></tr>
              ))
              : (rows as Order[]).map((o) => (
                <tr key={o.orderId}><td>{t(o.createdAt)}</td><td>{o.symbol}</td><td>{o.type === "limit" ? "限价" : "市价"}</td><td className={o.side === "buy" ? "up" : "down"}>{o.side === "buy" ? "买入" : "卖出"}</td><td>{o.price ?? "市价"}</td><td>{o.quantity}</td><td>{o.filledQuantity}</td><td>{ST[o.status] ?? o.status}</td><td>{tab === 0 && <a className="lime2" style={{ cursor: "pointer" }} onClick={() => cancel(o)}>撤单</a>}</td></tr>
              ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
