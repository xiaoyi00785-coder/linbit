"use client";
/** 现货下单面板与委托列表：对接 /spot/*（@linbit/api-client），API 不可达时显示错误，不生成本地数据 */
import { stLabel, orderToast } from "@/lib/orderStatus";
import { orderQtyText } from "@/lib/orderDisplay";
import { useCallback, useEffect, useRef, useState } from "react";
import { btnProps, tabProps } from "@/lib/a11y";
import { useNavigate } from "@/lib/router";
import { useAuthStore } from "@/lib/store";
import {
  cancelSpotOrder, listMyTrades, listOpenOrders, listOrderHistory, placeSpotOrder,
  type Order, type SpotTrade,
} from "@/lib/services";
import { fmtSig, fmtAmt, fmtFeeStr } from "@/lib/format";
import type { PlaceOrderRequest } from "@/lib/services";
import { Src } from "./common";
import { useFillPrice, type PairMeta } from "./TradeKit";
import { useTicker } from "@/lib/market";
import { useBalances } from "@/lib/queries";
import { rest } from "@/lib/rest";
import { openTransfer } from "@/components/Transfer";

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
  const [type, setType] = useState<"limit" | "market" | "plan">("limit");
  const [price, setPrice] = useState("");
  const [trig, setTrig] = useState("");
  const [tpsl, setTpsl] = useState(false);
  const [tp, setTp] = useState("");
  const [sl, setSl] = useState("");
  useFillPrice((p) => { setPrice(p); setType("limit"); });
  useEffect(() => { setPrice(""); setQty(""); setAmt(""); setTrig(""); setTp(""); setSl(""); }, [SYMBOL]);
  const fix = (v: string, d: number) => { const [a, b] = v.split("."); return b === undefined ? a : a + "." + b.slice(0, d); };
  const [qty, setQty] = useState("");
  // 市价买入按金额（USDT）下单：POST /spot/orders 的 quoteOrderQty；限价 / 市价卖出仍按数量
  const [amt, setAmt] = useState("");
  const byAmt = type === "market" && side === "buy";
  const [msg, setMsg] = useState<{ ok: boolean; t: string }>();
  const [busy, setBusy] = useState(false);
  const px = (type !== "market" && Number(price)) ? Number(price) : last;
  const an = Number(amt) || 0;
  // 按金额买入时，数量按最新价估算（仅用于展示）
  const q = byAmt ? (last ? an / last : 0) : Number(qty) || 0;
  const bal = useBalances();
  // 下单/撤单后（EVT）刷新可用余额：撮合与冻结异步，事件到达时再拉一次
  const balRefetch = bal.refetch;
  useEffect(() => { const f = () => { void balRefetch(); }; window.addEventListener(EVT, f); return () => window.removeEventListener(EVT, f); }, [balRefetch]);
  const freeOf = (a: string) => Number(((bal.data as any)?.items || []).find((x: any) => x.asset === a)?.free) || 0;
  const avail = side === "buy" ? freeOf("USDT") : freeOf(B);
  const [pc, setPc] = useState(0);
  useEffect(() => setPc(0), [side, SYMBOL, type]);
  // 卖出可用低于最小下单量（后端 minQuantity，缺省为一个步长）：禁用卖出按钮
  const minQ = pair.minQty || 10 ** -pair.qp;
  const sellBelowMin = authed && side === "sell" && avail > 0 && avail < minQ;
  const belowMinText = `可用 ${B} 低于最小下单量 ${fmtSig(minQ)} ${B}，无法卖出`;
  const setByPct = (p: number) => {
    if (!avail) {
      // 余额为 0：比例按钮与滑杆保持 0%，并给出提示
      setPc(0);
      if (p > 0) setMsg({ ok: false, t: !authed ? "请先登录" : `可用 ${side === "buy" ? "USDT" : B} 余额为 0，请先充值或划转` });
      return;
    }
    setPc(p);
    if (byAmt) { const a = Math.floor(((avail * p) / 100) * 100) / 100; setAmt(a ? a.toFixed(2) : ""); return; }
    const qn = side === "buy" ? (px ? (avail * p) / 100 / px : 0) : (avail * p) / 100;
    // 按交易对真实数量精度（后端 quantityPrecision）向下取整；不足一个步长或低于最小下单量时不填 0，直接提示
    const fl = qn ? Math.floor(qn * 10 ** pair.qp + 1e-9) / 10 ** pair.qp : 0;
    const mq = minQ;
    if (p > 0 && fl < mq) { setQty(""); setMsg({ ok: false, t: side === "sell" && avail < mq ? belowMinText : `所选比例数量低于最小下单量 ${fmtSig(mq)} ${B}，请调高比例` }); return; }
    setMsg(undefined);
    setQty(fl ? fl.toFixed(pair.qp) : "");
  };
  // 费率来自 GET /spot/fee-schedule（不可用时回退平台默认 0.1%）；限价按 maker 估算上限取 max(maker,taker)，市价按 taker
  const inflight = useRef(false); // 同步防重复提交（busy 状态要等重渲染才禁用按钮）
  const submit = async () => {
    if (inflight.current) return;
    if (!authed) return nav("/login");
    if (byAmt) {
      if (!amt.trim()) return setMsg({ ok: false, t: "请输入买入金额" });
      if (!(an > 0)) return setMsg({ ok: false, t: "金额必须大于 0" });
      if (authed && an > avail) return setMsg({ ok: false, t: `可用 USDT 不足（可用 ${fmtAmt(avail)}）` });
    } else {
      if (!qty.trim()) return setMsg({ ok: false, t: "请输入数量" });
      if (!(q > 0) || !isFinite(q)) return setMsg({ ok: false, t: "数量必须大于 0" });
      if ((qty.split(".")[1] ?? "").length > pair.qp) return setMsg({ ok: false, t: `数量最多 ${pair.qp} 位小数` });
      if (pair.minQty && q < pair.minQty) return setMsg({ ok: false, t: `最小下单量为 ${fmtSig(pair.minQty)} ${B}` });
      if (side === "sell" && authed && q > avail) return setMsg({ ok: false, t: `可用 ${B} 不足（可用 ${fmtAmt(avail)}）` });
    }
    if (type === "market" && !last) return setMsg({ ok: false, t: "暂无市价，请使用限价单" });
    if (type === "limit" && !Number(price)) return setMsg({ ok: false, t: "请输入价格" });
    if (type === "plan" && !Number(trig)) return setMsg({ ok: false, t: "请输入触发价" });
    setBusy(true);
    if (type === "plan") {
      const r = await rest("POST", "/spot/orders/conditional", { symbol: SYMBOL, side, type: Number(price) ? "limit" : "market", kind: "trigger", triggerPrice: trig, price: Number(price) ? price : undefined, quantity: qty, timeInForce: Number(price) ? "GTC" : "IOC", clientOrderId: `web-${Date.now()}` });
      setBusy(false);
      if (!r.ok) return setMsg({ ok: false, t: r.error || "计划委托失败" });
      setMsg({ ok: true, t: "计划委托已提交" }); setQty(""); setTrig(""); setPrice(""); setPc(0); bump(); return;
    }
    const body = {
      symbol: SYMBOL, side, type,
      ...(byAmt ? { quoteOrderQty: amt } : { quantity: qty }),
      price: type === "limit" ? price : null,
      timeInForce: type === "limit" ? "GTC" : "IOC",
      clientOrderId: `web-${Date.now()}`,
    } as PlaceOrderRequest & { quoteOrderQty?: string };
    inflight.current = true;
    let r: Awaited<ReturnType<typeof placeSpotOrder>>;
    try { r = await placeSpotOrder(body); }
    catch { r = { ok: false, error: "服务暂不可用" } as Awaited<ReturnType<typeof placeSpotOrder>>; }
    finally { inflight.current = false; setBusy(false); }
    if (!r.ok) return setMsg({ ok: false, t: r.error ?? "下单失败" });
    const d: any = r.data; const m = orderToast(d?.status, d?.filledQuantity);
    const filled = Number(d?.filledQuantity) || 0;
    if ((tp || sl) && filled > 0) {
      const opp = side === "buy" ? "sell" : "buy";
      const qf = String(d.filledQuantity);
      for (const [kind, pxv] of [["take_profit", tp], ["stop", sl]] as const) {
        if (!pxv) continue;
        const c = await rest("POST", "/spot/orders/conditional", { symbol: SYMBOL, side: opp, type: "market", kind, triggerPrice: pxv, quantity: qf, timeInForce: "IOC", clientOrderId: `web-${kind}-${Date.now()}` });
        if (!c.ok) { setMsg({ ok: false, t: `委托已成交，止盈止损未挂上：${c.error || "失败"}` }); setQty(""); setAmt(""); setPc(0); bump(); return; }
      }
    }
    setMsg({ ok: m.ok, t: `${m.t} #${d?.orderId}${(tp || sl) && !(filled > 0) ? "。尚未成交，止盈止损未挂出" : ""}` });
    if (r.ok) { setQty(""); setAmt(""); setPc(0); bump(); window.setTimeout(bump, 800); window.setTimeout(bump, 2500); }
  };

  return (
    <div data-guide="form" className="of">
      <div className="sp-side" role="tablist">
        {(["buy", "sell"] as const).map((s) => <button key={s} role="tab" aria-selected={side === s} className={(side === s ? "on " : "") + s} onClick={() => setSide(s)}>{s === "buy" ? "买入" : "卖出"}</button>)}
      </div>
      <div className="pt sm" role="tablist" aria-label="订单类型">
        {([["limit", "限价"], ["market", "市价"], ["plan", "计划委托"]] as const).map(([k, lab]) => <b key={k} {...tabProps(type === k, () => setType(k))} className={type === k ? "on" : ""} onClick={() => setType(k)}>{lab}</b>)}
      </div>
      {type === "plan" && <div className="inp"><small>触发价</small><input inputMode="decimal" aria-label="触发价" value={trig} onChange={(e) => setTrig(fix(num(e.target.value), pair.pp))} /><em>USDT</em></div>}
      {type !== "market" && <div className="inp"><small>{type === "plan" ? "委托价" : "价格"}</small><input inputMode="decimal" placeholder={type === "plan" ? "留空则市价" : ""} value={price} onChange={(e) => setPrice(fix(num(e.target.value), pair.pp))} /><em>USDT</em></div>}
      {byAmt ? <div className="inp"><small>金额(USDT)</small><input inputMode="decimal" aria-label="买入金额" placeholder="按金额买入" value={amt} onChange={(e) => { const v = e.target.value; if (/-/.test(v)) { setMsg({ ok: false, t: "金额不能为负数" }); return; } setMsg(undefined); setAmt(fix(num(v), 2)); setPc(0); }} aria-invalid={amt !== "" && !(an > 0)} /><em>USDT</em></div> :
      <div className="inp"><small>数量({B})</small><input inputMode="decimal" value={qty} onChange={(e) => { const v = e.target.value; if (/-/.test(v)) { setMsg({ ok: false, t: "数量不能为负数" }); return; } setMsg(undefined); setQty(fix(num(v), pair.qp)); }} aria-invalid={qty !== "" && !(q > 0)} /><em>{B}</em></div>}
      <div className={"slider lb-sl sp-sl " + side}><div className="line" /><div className="fill" style={{ width: pc + "%" }} />{[0, 25, 50, 75, 100].map((p) => <span key={p} {...btnProps(() => setByPct(p), `${p}%`)} className={pc >= p ? "on" : ""} style={{ left: p + "%" }} onClick={() => setByPct(p)}><i /><small>{p}%</small></span>)}<input type="range" min="0" max="100" value={pc} onChange={(e) => setByPct(+e.target.value)} aria-label="下单比例" /></div>
      {type !== "plan" && <label className="tk-ck2"><input type="checkbox" checked={tpsl} onChange={(e) => setTpsl(e.target.checked)} />止盈/止损</label>}
      {type !== "plan" && tpsl && <div className="tk-tp2"><div className="inp"><small>止盈</small><input inputMode="decimal" placeholder="触发价" value={tp} onChange={(e) => setTp(num(e.target.value))} /><em>USDT</em></div><div className="inp"><small>止损</small><input inputMode="decimal" placeholder="触发价" value={sl} onChange={(e) => setSl(num(e.target.value))} /><em>USDT</em></div></div>}
      <div className="kv2 g"><span>可用</span><b>{authed ? `${side === "buy" ? avail.toFixed(7) : avail.toFixed(pair.qp)} ${side === "buy" ? "USDT" : B}` : "--"}</b></div>
      <div className="kv2 g"><span>{side === "buy" ? "可买" : "可卖"}</span><b>{authed ? `${(side === "buy" ? (px ? avail / px : 0) : avail).toFixed(side === "buy" ? pair.qp : 4)} ${side === "buy" ? B : "USDT"}` : "--"}</b></div>
      {msg && <div className={msg.ok ? "fx-ok" : "fx-err"} style={{ fontSize: 12, margin: "6px 0" }}>{msg.t}</div>}
      <button className={"lbtn" + (authed && side === "sell" ? " sell" : "")} disabled={busy || sellBelowMin} title={sellBelowMin ? belowMinText : undefined} aria-busy={busy || undefined} onClick={submit}>{!authed ? "登录/注册" : sellBelowMin ? "低于最小下单量" : side === "buy" ? "买 入" : "卖 出"}</button>
    </div>
  );
}

const TABS = ["当前委托", "历史委托", "成交明细"] as const;
const t = (iso: string) => new Date(iso).toLocaleString("zh-CN", { hour12: false });
type Cond = { id: string; symbol: string; side: string; kind: string; type: string; triggerPrice: string; triggerDirection?: string; price?: string | null; quantity?: string | null; status: string; createdAt: string };

export function SpotOrders({ symbol: SYMBOL, base }: { symbol: string; base?: string }) {
  const [tab, setTab] = useState(0);
  const [sub, setSub] = useState<"limit" | "tpsl" | "plan">("limit");
  const [only, setOnly] = useState(false);
  const [conds, setConds] = useState<Cond[]>([]);
  const [rows, setRows] = useState<(Order | SpotTrade)[]>([]);
  const [src, setSrc] = useState<"api" | "network">();
  const [err, setErr] = useState("");
  const authed = useAuthStore((s) => !!s.accessToken);
  const bal = useBalances();
  const free = (a: string) => Number(((bal.data as { items?: { asset: string; free: string }[] })?.items || []).find((x) => x.asset === a)?.free) || 0;
  const coin = base || SYMBOL.replace(/USDT$/, "");

  const load = useCallback(async () => {
    if (!authed) { setRows([]); setConds([]); return; }
    const sym = only ? SYMBOL : undefined;
    const r = await (tab === 0 ? listOpenOrders(sym) : tab === 1 ? listOrderHistory(sym) : listMyTrades(sym));
    setSrc(r.source);
    setErr(r.ok ? "" : r.error ?? "加载失败");
    setRows((r.data?.items as (Order | SpotTrade)[]) ?? []);
    if (tab === 2) return;
    const q = new URLSearchParams({ status: tab === 1 ? "all" : "open", pageSize: "100" });
    if (sym) q.set("symbol", sym);
    const cr = await rest<{ items: Cond[] }>("GET", `/spot/orders/conditional?${q}`);
    if (!cr.ok) { setErr(cr.error || "条件单加载失败"); setConds([]); return; }
    const open = (c: Cond) => c.status === "pending" || c.status === "triggering";
    setConds((cr.data?.items ?? []).filter((c) => (tab === 1 ? !open(c) : open(c))));
  }, [tab, only, authed, SYMBOL]);

  useEffect(() => { load(); window.addEventListener(EVT, load); return () => window.removeEventListener(EVT, load); }, [load]);

  const cancel = async (o: Order) => {
    const r = await cancelSpotOrder(o.orderId, o.symbol);
    if (!r.ok) setErr(r.error ?? "撤单失败");
    bump();
  };
  const cancelCond = async (id: string) => {
    const r = await rest("DELETE", `/spot/orders/conditional/${id}`);
    if (!r.ok) setErr(r.error || "撤销失败");
    bump();
  };
  const shown = conds.filter((c) => sub === "plan" ? c.kind === "trigger" : c.kind === "stop" || c.kind === "take_profit");
  const nlab = (name: string, n: number) => `${name}\u00a0(${n})`;
  const broke = authed && !(free("USDT") > 0) && !(free(coin) > 0);
  const emptyN = tab === 2 || sub === "limit" ? rows.length : shown.length;
  const promo = <div className="fx-zero"><div>可用余额:&nbsp;0</div><div className="fx-zero-acts"><button type="button" onClick={() => openTransfer("资金账户", "现货账户")}>划转</button><a href="/deposit">充值</a></div></div>;

  return (
    <div className="orders" data-guide="orders">
      <div className="pt sm">
        {TABS.map((x, i) => <b key={x} {...tabProps(tab === i, () => { setTab(i); setSub("limit"); })} className={tab === i ? "on" : ""} onClick={() => { setTab(i); setSub("limit"); }}>{x}</b>)}
        <span className="sp" />{src === "network" && <Src s={src} />}
        <label className="tk-only"><input type="checkbox" checked={only} onChange={(e) => setOnly(e.target.checked)} />当前交易对</label>
      </div>
      {tab !== 2 && <div className="fx-sub">{([["limit", nlab("限价|市价", sub === "limit" ? rows.length : 0)], ["tpsl", nlab("止盈/止损", conds.filter((c) => c.kind === "stop" || c.kind === "take_profit").length)], ["plan", nlab("计划委托", conds.filter((c) => c.kind === "trigger").length)]] as const).map(([k, lab]) => <b key={k} className={sub === k ? "on" : ""} onClick={() => setSub(k)}>{lab}</b>)}</div>}
      {err && <div className="fx-err" style={{ margin: "8px 12px" }}>{err}</div>}
      {!authed ? <p className="empty">请 <a href="/login">登录</a> 或 <a href="/register">注册</a> 后开始交易</p>
        : broke && emptyN === 0 ? promo
        : emptyN === 0 ? <p className="empty">暂无数据</p>
        : tab === 2 ? (
          <table className="sotbl"><thead><tr><th>时间</th><th>交易对</th><th>方向</th><th>成交价</th><th>成交数量</th><th>手续费</th><th>角色</th></tr></thead><tbody>
            {(rows as SpotTrade[]).map((r) => <tr key={r.tradeId}><td>{t(r.createdAt)}</td><td>{r.symbol}</td><td className={r.side === "buy" ? "up" : "down"}>{r.side === "buy" ? "买入" : "卖出"}</td><td>{r.price}</td><td>{r.quantity}</td><td>{fmtFeeStr(r.fee)} {r.feeAsset ?? ""}</td><td>{r.role === "maker" ? "Maker" : "Taker"}</td></tr>)}
          </tbody></table>
        ) : sub === "limit" ? (
          <table className="sotbl"><thead><tr><th>时间</th><th>交易对</th><th>类型</th><th>方向</th><th>价格</th><th>数量</th><th>已成交</th><th>状态</th>{tab === 0 && <th />}</tr></thead><tbody>
            {(rows as Order[]).map((o) => <tr key={o.orderId}><td>{t(o.createdAt)}</td><td>{o.symbol}</td><td>{o.type === "limit" ? "限价" : "市价"}</td><td className={o.side === "buy" ? "up" : "down"}>{o.side === "buy" ? "买入" : "卖出"}</td><td>{o.price ?? "市价"}</td><td>{orderQtyText(o)}</td><td>{o.filledQuantity}</td><td>{stLabel(o.status)}</td>{tab === 0 && <td><a className="lime2" style={{ cursor: "pointer" }} onClick={() => cancel(o)}>撤单</a></td>}</tr>)}
          </tbody></table>
        ) : (
          <table className="sotbl"><thead><tr><th>时间</th><th>交易对</th><th>{sub === "plan" ? "方向" : "类型"}</th><th>触发价</th><th>委托价</th><th>数量</th><th>状态</th>{tab === 0 && <th />}</tr></thead><tbody>
            {shown.map((c) => <tr key={c.id}><td>{t(c.createdAt)}</td><td>{c.symbol}</td><td className={c.side === "buy" ? "up" : "down"}>{sub === "plan" ? (c.side === "buy" ? "买入" : "卖出") : c.kind === "take_profit" ? "止盈" : "止损"}</td><td>{c.triggerDirection === "down" ? "≤" : "≥"} {c.triggerPrice}</td><td>{c.price || "市价"}</td><td>{c.quantity || "--"}</td><td>{stLabel(c.status)}</td>{tab === 0 && <td><a className="lime2" style={{ cursor: "pointer" }} onClick={() => cancelCond(c.id)}>撤销</a></td>}</tr>)}
          </tbody></table>
        )}
    </div>
  );
}
