"use client";
// 合约页底部：仓位 / 当前委托 / 历史仓位 / 历史委托 / 成交明细 / 平仓盈亏 / 跟单 / 卡券。
// 当前委托与历史委托再分 限价、计划委托、止盈/止损、移动止盈/止损。
// 对接 GET /futures/positions、/futures/orders、/futures/plan-orders、/futures/tpsl-orders、
// /futures/positions/history、/futures/trades、/futures/account、/futures/copy/follows、/coupons。
import { stLabel, isOpenStatus, futOrderOf, closeToast } from "@/lib/orderStatus";
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { components } from "@linbit/api-client";
import { useRest, rest } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";
import { fmtLocal } from "@/lib/time";
import { openTransfer } from "@/components/Transfer";
import { Modal, useMarkPrice } from "./TradeKit";

/** 仓位行标记价：与顶部行情栏同一共享源 */
/** 未实现盈亏：有开仓价/数量/方向时按实时标记价计算（U 本位线性合约），否则用接口快照 */
function PnlCell({ p }: { p: Pos }) {
  const m = Number(useMarkPrice(p.symbol, p.markPrice));
  const e = Number(p.entryPrice), q = Math.abs(Number(p.qty)), mg = Number(p.margin);
  const dir = sideTxt(p.side) === "多" ? 1 : -1;
  const live = m > 0 && e > 0 && q > 0;
  const pnl = live ? (m - e) * q * dir : Number(p.unrealizedPnl);
  const roe = live ? (mg > 0 ? pnl / mg : NaN) : Number(p.roe);
  const n = Number.isFinite(pnl) ? pnl : NaN;
  const sign = n > 0 ? "+" : "";
  return <td data-testid="pos-pnl" className={n >= 0 ? "up" : "down"}>{Number.isFinite(n) ? sign + n.toFixed(4) : "--"} USDT ({Number.isFinite(roe) ? (roe > 0 ? "+" : "") + (roe * 100).toFixed(2) : "--"}%)</td>;
}
function MarkCell({ sym, fb }: { sym: string; fb: string }) { return <>{useMarkPrice(sym, fb)}</>; }
function liveUpnl(p: Pos, mark: number) {
  const e = Number(p.entryPrice), q = Math.abs(Number(p.qty)), dir = sideTxt(p.side) === "多" ? 1 : -1;
  return mark > 0 && e > 0 && q > 0 ? (mark - e) * q * dir : Number(p.unrealizedPnl);
}
function ValueCell({ p }: { p: Pos }) {
  const m = Number(useMarkPrice(p.symbol, p.markPrice));
  const q = Math.abs(Number(p.qty));
  const v = m > 0 && q > 0 ? q * m : Math.abs(Number(p.notional));
  return <>{Number.isFinite(v) ? v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + " USDT" : "--"}</>;
}
function RatioCell({ p, cross }: { p: Pos; cross?: { avail: number; im: number; mm: number } }) {
  const m = Number(useMarkPrice(p.symbol, p.markPrice));
  const up = liveUpnl(p, m);
  const mm = Number(p.maintenanceMargin), mg = Number(p.margin);
  let r = NaN;
  if (p.marginMode === "isolated") { const d = mg + up; if (d > 0) r = mm / d; }
  else if (cross && cross.avail + cross.im > 0) r = cross.mm / (cross.avail + cross.im);
  else if (mg > 0) r = mm / mg;
  if (!Number.isFinite(r)) return <>--</>;
  const pct = r * 100;
  return <span className={pct >= 80 ? "down" : pct >= 50 ? "fx-warn" : ""}>{pct.toFixed(2)}%</span>;
}
function Adl({ n }: { n?: number }) {
  const k = Math.max(0, Math.min(5, n || 0));
  return <span className="fx-adl" title="ADL">{[1, 2, 3, 4, 5].map((i) => <i key={i} className={i <= k ? "on" : ""} />)}</span>;
}
import { tabProps } from "@/lib/a11y";
import { fmtFeeExact } from "@/lib/format";
import { liqText } from "@/lib/orderDisplay";

type S = components["schemas"];
type Algo = S["FuturesAlgoOrder"];
type PosHist = S["FuturesPositionHistory"];
type Pos = { symbol: string; side: string; positionSide?: string; qty: string; entryPrice: string; markPrice: string; notional?: string; unrealizedPnl: string; roe: string; realizedPnl?: string; leverage: number; margin: string; maintenanceMargin?: string; liquidationPrice: string; marginMode: string; adlRank?: number };
type Ord = { orderId: string; symbol: string; side: string; positionSide?: string; type: string; timeInForce?: string; price: string; qty: string; filledQty: string; status: string; leverage: number; reduceOnly?: boolean; createdAt: number | string };
type Tab = "pos" | "open" | "ph" | "hist" | "fills" | "pnl" | "copy" | "coupon";
type Sub = "limit" | "plan" | "tpsl" | "trail";
type Fill = { tradeId: number; symbol: string; side: "buy" | "sell"; price: string; qty: string; fee: string; role: "maker" | "taker"; time: string };
type Follow = { id: number; traderId?: string; status?: string; principal?: string; returned?: string; sharePaid?: string };
type CouponItem = { id: number; kind: "trial" | "deduction"; amount: string; asset?: string; title: string; status: string; bonus?: { remaining?: string } };
type Acct = { availableBalance?: string; crossMaintenanceMargin?: string; crossPositionMargin?: string };
const sideTxt = (s: string) => (/buy|long/i.test(s) ? "多" : "空");
const num = (v: string) => v.replace(/[^\d.]/g, "");
const TT: Record<string, string> = { LAST: "最新价", MARK: "标记价" };
const TIF: Record<string, string> = { GTC: "GTC", IOC: "IOC", POST_ONLY: "只做 Maker", GTX: "只做 Maker" };
const dir = (o: { side?: string; positionSide?: string; reduceOnly?: boolean }) => {
  const buy = /buy/i.test(o.side ?? "");
  if (o.positionSide === "LONG") return [buy ? "开多" : "平多", buy];
  if (o.positionSide === "SHORT") return [buy ? "平空" : "开空", buy];
  return [o.reduceOnly ? (buy ? "平空" : "平多") : buy ? "买入" : "卖出", buy] as const;
};

export function FutOrders({ symbol }: { symbol?: string }) {
  const authed = !!useAuthStore((s) => s.accessToken);
  const [tab, setTab] = useState<Tab>("pos");
  const [sub, setSub] = useState<Sub>("limit");
  const [edit, setEdit] = useState<Ord | null>(null);
  const [tp, setTp] = useState<{ p: Pos; tp?: Algo; sl?: Algo } | null>(null);
  const [pe, setPe] = useState<Algo | null>(null);
  const [lc, setLc] = useState<Pos | null>(null);
  const [rv, setRv] = useState<Pos | null>(null);
  const [only, setOnly] = useState(false);
  const [kind, setKind] = useState<"all" | "usdt">("all");
  const [pick, setPick] = useState("");
  const [menu, setMenu] = useState<"" | "kind" | "sym">("");
  const [find, setFind] = useState("");
  const pos = useRest<{ positions: Pos[] }>(authed ? "/futures/positions" : null);
  const posRef = useRef(pos); posRef.current = pos;
  const open = useRest<{ orders: Ord[] }>(authed ? "/futures/orders?status=open" : null);
  const plan = useRest<{ orders: Algo[] }>(authed ? "/futures/plan-orders?status=active" : null);
  const tpsl = useRest<{ orders: Algo[] }>(authed ? "/futures/tpsl-orders?status=active" : null);
  const hist = useRest<{ orders: Ord[] }>(authed && tab === "hist" ? "/futures/orders" : null);
  const planH = useRest<{ orders: Algo[] }>(authed && tab === "hist" ? "/futures/plan-orders?status=history" : null);
  const tpslH = useRest<{ orders: Algo[] }>(authed && tab === "hist" ? "/futures/tpsl-orders?status=history" : null);
  const ph = useRest<{ positions: PosHist[] }>(authed && (tab === "ph" || tab === "pnl") ? "/futures/positions/history" : null);
  const fills = useRest<{ trades: Fill[] }>(authed && tab === "fills" ? "/futures/trades?limit=100" : null);
  const acct = useRest<Acct>(authed ? "/futures/account" : null);
  const follows = useRest<{ follows: Follow[] }>(authed ? "/futures/copy/follows" : null);
  const coupons = useRest<{ items: CouponItem[] }>(authed ? "/coupons" : null);
  const cts = useRest<{ contracts: { symbol: string }[] }>("/futures/market/contracts");
  const pass = <X extends { symbol?: string }>(x: X) => {
    if (only && symbol && x.symbol !== symbol) return false;
    if (pick && x.symbol !== pick) return false;
    if (kind === "usdt" && x.symbol && !x.symbol.endsWith("USDT")) return false;
    return true;
  };
  const mine = <X extends { symbol?: string }>(L: X[] | undefined) => (L ?? []).filter(pass);
  const P = mine(pos.data?.positions), O = mine(open.data?.orders), PL = mine(plan.data?.orders), TS = mine(tpsl.data?.orders);
  const H = mine(hist.data?.orders).filter((o) => !isOpenStatus(o.status)), PH = mine(ph.data?.positions), FL = mine(fills.data?.trades);
  const PLH = mine(planH.data?.orders), TSH = mine(tpslH.data?.orders);
  const FLW = (follows.data?.follows ?? []).filter((x) => !x.status || x.status === "active");
  const cur = tab === "pos" ? pos : tab === "open" ? (sub === "plan" ? plan : sub === "tpsl" ? tpsl : open) : tab === "ph" || tab === "pnl" ? ph : tab === "hist" ? (sub === "plan" ? planH : sub === "tpsl" ? tpslH : hist) : tab === "fills" ? fills : tab === "copy" ? follows : tab === "coupon" ? coupons : pos;
  // 下单/撤单/改单后静默刷新（不闪骨架屏）；撮合与条件单落库有延迟，再补两次
  const refreshAll = () => { pos.refresh(); open.refresh(); plan.refresh(); tpsl.refresh(); hist.refresh(); planH.refresh(); tpslH.refresh(); ph.refresh(); fills.refresh(); acct.refresh(); follows.refresh(); coupons.refresh(); };
  const reloadAll = () => { refreshAll(); window.setTimeout(refreshAll, 800); window.setTimeout(refreshAll, 2500); };
  // 仓位标记价 / 未实现盈亏：每 3s 静默轮询（页面隐藏时暂停）
  useEffect(() => {
    if (!authed) return;
    const id = window.setInterval(() => { if (document.visibilityState === "visible") posRef.current.refresh(); }, 3000);
    return () => window.clearInterval(id);
  }, [authed]);
  // 防重复点击：同一撤单/平仓请求在途时忽略再次点击（ref 同步生效，不依赖重渲染）
  const inflight = useRef(new Set<string>());
  const once = async (key: string, fn: () => Promise<void>) => {
    if (inflight.current.has(key)) return;
    inflight.current.add(key);
    try { await fn(); } catch { toast("网络异常，请稍后重试", "err", 4000); } finally { inflight.current.delete(key); }
  };
  const del = (path: string, ok: string) => once("del " + path, async () => {
    const r = await rest("DELETE", path);
    if (!r.ok) return toast(r.error || "撤单失败", "err", 4000);
    toast(ok); reloadAll();
  });
  const close = (p: Pos) => once(`close ${p.symbol} ${p.positionSide ?? ""}`, async () => {
    const r = await rest<any>("POST", `/futures/positions/${p.symbol}/close`, p.positionSide && p.positionSide !== "BOTH" ? { positionSide: p.positionSide } : {});
    if (!r.ok) return toast(r.error || "平仓失败", "err", 4000);
    const o = futOrderOf(r.data); const m = closeToast(o.status, o.filledQty); toast(m.t, m.ok ? undefined : "err"); reloadAll();
  });
  const rlRef = useRef(reloadAll); rlRef.current = reloadAll;
  useEffect(() => { const f = () => rlRef.current(); window.addEventListener("lb-fut-orders", f); return () => window.removeEventListener("lb-fut-orders", f); }, []);
  useEffect(() => {
    if (!menu) return;
    const f = () => setMenu("");
    document.addEventListener("pointerdown", f);
    return () => document.removeEventListener("pointerdown", f);
  }, [menu]);
  const nlab = (t: string, n?: number) => n == null ? t : `${t}\u00a0(${n})`;
  const tabs: { id: Tab; label: string }[] = [
    { id: "pos", label: nlab("仓位", P.length) },
    { id: "open", label: nlab("当前委托", O.length + PL.length + TS.length) },
    { id: "ph", label: "历史仓位" },
    { id: "hist", label: "历史委托" },
    { id: "fills", label: "成交明细" },
    { id: "pnl", label: "平仓盈亏" },
    { id: "copy", label: nlab("跟单", FLW.length) },
    { id: "coupon", label: nlab("卡券", (coupons.data?.items ?? []).filter((c) => c.status === "issued" || c.status === "active").length) },
  ];
  const subs: { id: Sub; label: string }[] = tab === "hist"
    ? [{ id: "limit", label: nlab("限价", H.length) }, { id: "plan", label: nlab("计划委托", PLH.length) }, { id: "tpsl", label: nlab("止盈/止损", TSH.length) }, { id: "trail", label: nlab("移动止盈/止损", 0) }]
    : [{ id: "limit", label: nlab("限价", O.length) }, { id: "plan", label: nlab("计划委托", PL.length) }, { id: "tpsl", label: nlab("止盈/止损", TS.length) }, { id: "trail", label: nlab("移动止盈/止损", 0) }];
  const syms = (cts.data?.contracts ?? []).map((c) => c.symbol).filter((s) => kind === "all" || s.endsWith("USDT")).sort();
  const shownSyms = syms.filter((s) => !find || s.toLowerCase().includes(find.toLowerCase()));
  const availN = acct.data ? Number(acct.data.availableBalance) : null;
  const broke = availN != null && !(availN > 0);
  const cross = acct.data ? { avail: Number(acct.data.availableBalance) || 0, im: Number(acct.data.crossPositionMargin) || 0, mm: Number(acct.data.crossMaintenanceMargin) || 0 } : undefined;
  const empty = <p className="empty">暂无数据</p>;
  const promo = <div className="fx-zero"><div>可用余额:&nbsp;{availN === 0 ? "0" : (availN ?? 0).toLocaleString("en-US", { maximumFractionDigits: 8 })}</div><div className="fx-zero-try">尝试 0风险体验模拟交易</div><div className="fx-zero-acts"><button type="button" onClick={() => openTransfer("资金账户", "合约账户")}>划转</button><a href="/deposit">充值</a><a href="/sim">模拟交易</a></div></div>;
  const algoTable = (L: Algo[], mode: "plan" | "tpsl", histRow?: boolean) => L.length ? <table className="sotbl"><thead><tr><th>订单时间</th><th>合约</th><th>{mode === "plan" ? "方向" : "类型"}</th><th>触发价格</th><th>委托价格</th><th>数量</th><th>状态</th>{!histRow && <th />}</tr></thead><tbody>
    {L.map((o) => { const [d, b] = dir(o); return <tr key={o.algoId}><td>{fmtLocal(o.createdAt)}</td><td>{o.symbol}</td><td className={mode === "plan" ? (b ? "up" : "down") : o.algoType === "tp" ? "up" : "down"}>{mode === "plan" ? d : o.algoType === "tp" ? "止盈" : "止损"}</td><td>{TT[o.triggerPriceType ?? "LAST"]} {o.triggerDirection === "down" ? "≤" : "≥"} {o.triggerPrice}</td><td>{o.price || "市价"}</td><td>{o.qty || "全部仓位"}</td><td>{stLabel(o.status)}</td>{!histRow && <td>{mode === "plan" && <a className="lime2" onClick={() => setPe(o)}>修改</a>} <a className="lime2" onClick={() => del(`/futures/${mode === "plan" ? "plan" : "tpsl"}-orders/${o.algoId}`, "已撤销")}>撤销</a></td>}</tr>; })}
  </tbody></table> : empty;
  const ordTable = (L: Ord[], live: boolean) => L.length ? <table className="sotbl"><thead><tr><th>订单时间</th><th>合约</th><th>委托类型</th><th>方向</th><th>委托价格</th><th>数量</th><th>已成交</th><th>状态</th>{live && <th />}</tr></thead><tbody>
    {L.map((o) => { const [d, b] = dir(o); return <tr key={o.orderId}><td>{fmtLocal(o.createdAt)}</td><td>{o.symbol}</td><td>{o.type === "limit" ? "限价" : o.type === "market" ? "市价" : o.type}{o.timeInForce && o.type === "limit" ? ` · ${TIF[o.timeInForce] ?? o.timeInForce}` : ""}</td><td className={b ? "up" : "down"}>{d}</td><td>{o.price || "市价"}</td><td>{o.qty}</td><td>{o.filledQty}</td><td>{stLabel(o.status)}</td>{live && <td>{o.type === "limit" && <a className="lime2" onClick={() => setEdit(o)}>修改</a>} <a className="lime2" onClick={() => del(`/futures/orders/${o.orderId}`, "已撤单")}>撤单</a></td>}</tr>; })}
  </tbody></table> : empty;
  const blank = (node: ReactNode, n: number) => broke && n === 0 ? promo : node;
  const body = () => {
    if (tab === "coupon") {
      const items = coupons.data?.items ?? [];
      const sum = (k: CouponItem["kind"]) => { const xs = items.filter((c) => c.kind === k && (c.status === "issued" || c.status === "active")); return xs.length ? xs.reduce((a, c) => a + Number(c.bonus?.remaining ?? c.amount ?? 0), 0) : null; };
      const trial = sum("trial"), fee = sum("deduction");
      const fmt = (v: number | null) => v == null ? "--" : v.toLocaleString("en-US", { maximumFractionDigits: 4 });
      return <div className="fx-coupon"><div className="kv2"><span>抵扣金</span><b>{fmt(fee)} USDT</b></div><div className="kv2"><span>体验金</span><b>{fmt(trial)} USDT</b></div>{items.length ? <table className="sotbl"><thead><tr><th>卡券</th><th>面额</th><th>状态</th></tr></thead><tbody>{items.map((c) => <tr key={c.id}><td>{c.title}</td><td>{c.amount} {c.asset || "USDT"}</td><td>{c.status === "issued" ? "待使用" : c.status === "active" ? "使用中" : c.status === "expired" ? "已过期" : "已失效"}</td></tr>)}</tbody></table> : <p className="empty">暂无卡券</p>}<a className="fx-more" href="/coupons">前往卡券中心</a></div>;
    }
    if (tab === "copy") {
      return blank(FLW.length ? <table className="sotbl"><thead><tr><th>交易员</th><th>状态</th><th>本金</th><th>已退回</th><th>已付分润</th><th /></tr></thead><tbody>{FLW.map((x) => <tr key={x.id}><td>{x.traderId}</td><td>{x.status === "active" ? "跟单中" : x.status}</td><td>{x.principal}</td><td>{x.returned}</td><td>{x.sharePaid}</td><td><a className="lime2" onClick={() => del(`/futures/copy/follows/${x.id}`, "已停止跟单")}>停止跟单</a></td></tr>)}</tbody></table> : <p className="empty">暂无跟单 <a href="/copy">前往跟单</a></p>, FLW.length);
    }
    if (tab === "pos") {
      return blank(P.length ? <div className="fx-scroll"><table className="sotbl fx-pos"><thead><tr><th>合约</th><th>持仓量</th><th>持仓价值</th><th>开仓均价</th><th>预估强平价</th><th>标记价格</th><th>仓位保证金</th><th>保证金比率</th><th>未实现盈亏 (回报率)</th><th>加仓</th><th>全部止盈/止损</th><th>部分止盈/止损</th><th>移动止盈/止损</th><th>反手</th><th>ADL</th><th>平仓</th></tr></thead><tbody>
        {P.map((p) => { const ps = TS.filter((o) => o.symbol === p.symbol && (!p.positionSide || !o.positionSide || o.positionSide === p.positionSide)); const full = ps.filter((o) => !o.qty), part = ps.filter((o) => !!o.qty); const tpo = full.find((o) => o.algoType === "tp"), slo = full.find((o) => o.algoType === "sl"); const hedge = p.positionSide === "LONG" || p.positionSide === "SHORT";
          return <tr key={p.symbol + p.side + (p.positionSide ?? "")}><td className="fx-sym"><b>{p.symbol}</b><small className={sideTxt(p.side) === "多" ? "up" : "down"}>{sideTxt(p.side)}·{p.leverage}X·{p.marginMode === "isolated" ? "逐仓" : "全仓"}·{hedge ? "分仓" : "合仓"}</small></td><td>{p.qty}</td><td><ValueCell p={p} /></td><td>{p.entryPrice}</td><td title={liqText(p.liquidationPrice, p.markPrice, p.side) ? undefined : (p.marginMode === "cross" ? "全仓保证金充足，暂无强平价" : "保证金充足，暂无强平价")}>{liqText(p.liquidationPrice, p.markPrice, p.side) ?? "--"}</td><td data-testid="pos-mark"><MarkCell sym={p.symbol} fb={p.markPrice} /></td><td>{p.margin} USDT</td><td><RatioCell p={p} cross={cross} /></td><PnlCell p={p} /><td>—</td><td>{tpo || slo ? <button type="button" className="fx-mini" onClick={() => setTp({ p, tp: tpo, sl: slo })}>{tpo?.triggerPrice || "--"} / {slo?.triggerPrice || "--"}</button> : <button type="button" className="fx-mini" onClick={() => setTp({ p })}>+</button>}</td><td>{part.length ? <button type="button" className="fx-mini" onClick={() => setTp({ p, tp: part.find((o) => o.algoType === "tp"), sl: part.find((o) => o.algoType === "sl") })}>数量, {part[0].qty}</button> : <button type="button" className="fx-mini" onClick={() => setTp({ p })}>+</button>}</td><td>—</td><td><button type="button" className="fx-mini" onClick={() => setRv(p)}>反手</button></td><td><Adl n={p.adlRank} /></td><td><div className="fx-acts"><button type="button" className="fx-mini" onClick={() => setLc(p)}>限价</button><button type="button" className="fx-mini" onClick={() => close(p)}>市价</button></div></td></tr>; })}
      </tbody></table></div> : <p className="empty">暂无仓位</p>, P.length);
    }
    if (tab === "ph") return blank(PH.length ? <table className="sotbl"><thead><tr><th>合约</th><th>方向</th><th>开仓均价</th><th>平仓均价</th><th>最大持仓</th><th>已平数量</th><th>价格盈亏</th><th>手续费</th><th>资金费</th><th>净盈亏</th><th>开仓时间</th><th>平仓时间</th><th>状态</th></tr></thead><tbody>
      {PH.map((p) => <tr key={p.id}><td>{p.symbol}</td><td className={p.side === "long" ? "up" : "down"}>{p.side === "long" ? "多" : "空"} {p.leverage}x</td><td>{p.entryPrice}</td><td>{p.closePrice}</td><td>{p.maxQty}</td><td>{p.closedQty}</td><td className={Number(p.realizedPnl) >= 0 ? "up" : "down"}>{fmtFeeExact(p.realizedPnl)}</td><td>{fmtFeeExact(p.fee)}</td><td>{fmtFeeExact(p.fundingFee)}</td><td className={Number(p.netPnl) >= 0 ? "up" : "down"}>{fmtFeeExact(p.netPnl)}</td><td>{fmtLocal(p.openTime)}</td><td>{fmtLocal(p.closeTime)}</td><td>{stLabel(p.status)}</td></tr>)}
    </tbody></table> : empty, PH.length);
    if (tab === "pnl") return blank(PH.length ? <table className="sotbl"><thead><tr><th>合约</th><th>方向</th><th>平仓数量</th><th>开仓均价</th><th>平仓均价</th><th>价格盈亏</th><th>手续费</th><th>资金费</th><th>净盈亏</th><th>开仓时间</th><th>平仓时间</th></tr></thead><tbody>
      {PH.map((p) => <tr key={p.id}><td>{p.symbol}</td><td className={p.side === "long" ? "up" : "down"}>{p.side === "long" ? "多" : "空"}·{p.leverage}X</td><td>{p.closedQty}</td><td>{p.entryPrice}</td><td>{p.closePrice}</td><td className={Number(p.realizedPnl) >= 0 ? "up" : "down"}>{fmtFeeExact(p.realizedPnl)}</td><td>{fmtFeeExact(p.fee)}</td><td>{fmtFeeExact(p.fundingFee)}</td><td className={Number(p.netPnl) >= 0 ? "up" : "down"}>{fmtFeeExact(p.netPnl)}</td><td>{fmtLocal(p.openTime)}</td><td>{fmtLocal(p.closeTime)}</td></tr>)}
    </tbody></table> : empty, PH.length);
    if (tab === "fills") return blank(FL.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>方向</th><th>成交价格</th><th>成交数量</th><th>手续费</th><th>角色</th></tr></thead><tbody>
      {FL.map((t) => <tr key={t.tradeId}><td>{fmtLocal(t.time)}</td><td>{t.symbol}</td><td className={t.side === "buy" ? "up" : "down"}>{t.side === "buy" ? "买入" : "卖出"}</td><td>{t.price}</td><td>{t.qty}</td><td>{fmtFeeExact(t.fee)}</td><td>{t.role === "maker" ? "Maker" : "Taker"}</td></tr>)}
    </tbody></table> : empty, FL.length);
    if (sub === "trail") return blank(<p className="empty">暂无移动止盈止损</p>, 0);
    if (sub === "plan") return blank(algoTable(tab === "hist" ? PLH : PL, "plan", tab === "hist"), (tab === "hist" ? PLH : PL).length);
    if (sub === "tpsl") return blank(algoTable(tab === "hist" ? TSH : TS, "tpsl", tab === "hist"), (tab === "hist" ? TSH : TS).length);
    return blank(ordTable(tab === "hist" ? H : O, tab === "open"), (tab === "hist" ? H : O).length);
  };
  return (
    <div className="orders" data-guide="orders">
      <div className="pt sm" role="tablist" aria-label="合约订单">{tabs.map((x) => <b key={x.id} {...tabProps(tab === x.id, () => { setTab(x.id); setSub("limit"); })} className={tab === x.id ? "on" : ""} onClick={() => { setTab(x.id); setSub("limit"); }}>{x.label}</b>)}<span className="sp" />{cur.source === "network" && <span className="fx-src" role="button" style={{ cursor: "pointer" }} onClick={() => cur.reload()}>加载失败，点击重试</span>}{symbol && <label className="tk-only"><input type="checkbox" checked={only} onChange={(e) => setOnly(e.target.checked)} />当前交易对</label>}</div>
      {!authed ? <p className="empty">请 <a href="/login">登录</a> 或 <a href="/register">注册</a> 后开始交易</p> : <>
        {(tab === "open" || tab === "hist") && <div className="fx-sub" role="tablist" aria-label="委托类型">{subs.map((x) => <b key={x.id} className={sub === x.id ? "on" : ""} onClick={() => setSub(x.id)}>{x.label}</b>)}</div>}
        {tab !== "coupon" && <div className="fx-filters">
          <div className={"fx-drop" + (menu === "kind" ? " open" : "")} onPointerDown={(e) => e.stopPropagation()}>
            <button type="button" className="fx-drop-btn" onClick={() => setMenu(menu === "kind" ? "" : "kind")}>合约类型：{kind === "usdt" ? "USDT合约" : "全部"}<i /></button>
            {menu === "kind" && <div className="fx-menu">{([["all", "全部"], ["usdt", "USDT合约"]] as const).map(([k, t]) => <div key={k} className={"fx-opt" + (kind === k ? " on" : "")} onClick={() => { setKind(k); setMenu(""); }}><span>{t}</span>{kind === k && <span>✓</span>}</div>)}</div>}
          </div>
          <div className={"fx-drop" + (menu === "sym" ? " open" : "")} onPointerDown={(e) => e.stopPropagation()}>
            <button type="button" className="fx-drop-btn" onClick={() => setMenu(menu === "sym" ? "" : "sym")}>合约筛选：{pick || "全部"}<i /></button>
            {menu === "sym" && <div className="fx-menu"><input placeholder="筛选" value={find} onChange={(e) => setFind(e.target.value.toUpperCase())} /><div className={"fx-opt" + (!pick ? " on" : "")} onClick={() => { setPick(""); setMenu(""); }}><span>全部</span>{!pick && <span>✓</span>}</div>{shownSyms.map((s) => <div key={s} className={"fx-opt" + (pick === s ? " on" : "")} onClick={() => { setPick(s); setMenu(""); }}><span>{s}</span>{pick === s && <span>✓</span>}</div>)}</div>}
          </div>
        </div>}
        {cur.loading && !cur.data ? <div style={{ padding: 12 }}><div className="lb-skel" style={{ height: 80 }} /></div> : body()}
      </>}
      {edit && <EditModal o={edit} onClose={() => setEdit(null)} onDone={() => { setEdit(null); reloadAll(); }} />}
      {tp && <TpslModal p={tp.p} cur={tp} onClose={() => setTp(null)} onDone={() => { setTp(null); reloadAll(); }} />}
      {pe && <PlanEditModal o={pe} onClose={() => setPe(null)} onDone={() => { setPe(null); reloadAll(); }} />}
      {lc && <LimitCloseModal p={lc} onClose={() => setLc(null)} onDone={() => { setLc(null); reloadAll(); }} />}
      {rv && <ReverseModal p={rv} onClose={() => setRv(null)} onDone={() => { setRv(null); reloadAll(); }} />}
    </div>
  );
}

/** 改单：PUT /futures/orders/{id}，仅限价单；改价或加量失去时间优先级，减量保留 */
function EditModal({ o, onClose, onDone }: { o: Ord; onClose: () => void; onDone: () => void }) {
  const [price, setPrice] = useState(o.price);
  const [qty, setQty] = useState(o.qty);
  const [busy, setBusy] = useState(false);
  const ok = async () => {
    const body: S["FuturesAmendOrderRequest"] = {};
    if (price !== o.price) body.price = price;
    if (qty !== o.qty) body.qty = qty;
    if (!body.price && !body.qty) return toast("价格和数量均未修改", "err");
    setBusy(true);
    const r = await rest("PUT", `/futures/orders/${o.orderId}`, body);
    setBusy(false);
    if (!r.ok) { const e = r.error ?? ""; if (r.status === 404 || r.status === 405) return toast("当前服务暂不支持修改计划委托，请撤销后重新下单", "err"); return toast(/AMEND_LIMIT/.test(e) ? "改单次数已达上限" : /POST_ONLY_REJECTED/.test(e) ? "只做 Maker 订单改价后会立即成交，已被拒绝" : /NO_CHANGE/.test(e) ? "价格和数量均未修改" : e || "改单失败", "err"); }
    toast("改单成功"); onDone();
  };
  return (
    <Modal title={`改单 · ${o.symbol}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <div className="tk-ed">
        <div className="inp"><small>价格</small><input inputMode="decimal" value={price} onChange={(e) => setPrice(num(e.target.value))} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" value={qty} onChange={(e) => setQty(num(e.target.value))} /></div>
        <p className="tk-txt g">{`改价或增加数量会失去时间优先级；减少数量保留原排队位置。已成交 ${o.filledQty}。`}</p>
      </div>
    </Modal>
  );
}

const tpslErr = (e: string, d: string) => (/INVALID_TPSL/.test(e) ? "止盈止损价格不合理" : /ALGO_LIMIT/.test(e) ? "条件单数量已达上限" : /NO_POSITION/.test(e) ? "仓位已不存在" : /ORDER_NOT_OPEN/.test(e) ? "委托已触发或已撤销" : e || d);

/** 持仓止盈止损：已有 TP/SL → 预填当前值，修改走 PUT /futures/tpsl-orders/{id}、清空则撤销；没有则 POST /futures/tpsl-orders 新建 */
function TpslModal({ p, cur, onClose, onDone }: { p: Pos; cur: { tp?: Algo; sl?: Algo }; onClose: () => void; onDone: () => void }) {
  const mark = useMarkPrice(p.symbol, p.markPrice);
  const init = { tp: cur.tp?.triggerPrice ?? "", sl: cur.sl?.triggerPrice ?? "", qty: cur.tp?.qty || cur.sl?.qty || "", tt: ((cur.tp ?? cur.sl)?.triggerPriceType ?? "MARK") as "LAST" | "MARK" };
  const [f, setF] = useState(init);
  const [busy, setBusy] = useState(false);
  const ok = async () => {
    if (!f.tp && !f.sl && !cur.tp && !cur.sl) return toast("请填写止盈或止损触发价", "err");
    const ps = (p.positionSide as S["FuturesTpslOrderRequest"]["positionSide"]) || undefined;
    const jobs: (() => Promise<{ ok: boolean; error?: string }>)[] = [];
    const create: S["FuturesTpslOrderRequest"] = { symbol: p.symbol, positionSide: ps, qty: f.qty || undefined };
    let needCreate = false;
    for (const k of ["tp", "sl"] as const) {
      const o = cur[k], v = f[k];
      if (o) {
        if (!v) jobs.push(() => rest("DELETE", `/futures/tpsl-orders/${o.algoId}`));
        else if (v !== o.triggerPrice || f.tt !== (o.triggerPriceType ?? "MARK") || f.qty !== init.qty) {
          const body: S["FuturesAlgoAmendRequest"] = { triggerPrice: v, triggerPriceType: f.tt };
          if (f.qty && f.qty !== init.qty) body.qty = f.qty;
          // 后端未部署改单接口（404/405）时回退为“撤销原单 + 新建”
          jobs.push(async () => {
            const r = await rest("PUT", `/futures/tpsl-orders/${o.algoId}`, body);
            if (r.ok || (r.status !== 404 && r.status !== 405)) return r;
            const d = await rest("DELETE", `/futures/tpsl-orders/${o.algoId}`);
            if (!d.ok) return d;
            const one: S["FuturesTpslOrderRequest"] = { symbol: p.symbol, positionSide: ps, qty: f.qty || undefined, ...(k === "tp" ? { tpTriggerPrice: v, tpTriggerPriceType: f.tt } : { slTriggerPrice: v, slTriggerPriceType: f.tt }) };
            return rest("POST", "/futures/tpsl-orders", one);
          });
        }
      } else if (v) {
        needCreate = true;
        if (k === "tp") { create.tpTriggerPrice = v; create.tpTriggerPriceType = f.tt; } else { create.slTriggerPrice = v; create.slTriggerPriceType = f.tt; }
      }
    }
    if (needCreate) jobs.push(() => rest("POST", "/futures/tpsl-orders", create));
    if (!jobs.length) return toast("止盈止损均未修改", "err");
    setBusy(true);
    for (const j of jobs) { const r = await j(); if (!r.ok) { setBusy(false); onDone(); return toast(tpslErr(r.error ?? "", "设置失败"), "err"); } }
    setBusy(false);
    toast(cur.tp || cur.sl ? "止盈止损已更新" : "止盈止损已设置"); onDone();
  };
  return (
    <Modal title={`止盈止损 · ${p.symbol} ${sideTxt(p.side)}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <div className="tk-ed">
        <div className="kv2 g"><span>开仓价 / 标记价</span><span>{p.entryPrice} / <span data-testid="tpsl-mark">{mark}</span></span></div>
        {(cur.tp || cur.sl) && <div className="kv2 g"><span>当前止盈 / 止损</span><span>{cur.tp?.triggerPrice || "--"} / {cur.sl?.triggerPrice || "--"}</span></div>}
        <div className="tk-tif">{(["MARK", "LAST"] as const).map((k) => <b key={k} className={f.tt === k ? "on" : ""} onClick={() => setF({ ...f, tt: k })}>{`按${TT[k]}触发`}</b>)}</div>
        <div className="inp"><small>止盈触发价</small><input inputMode="decimal" aria-label="止盈触发价" value={f.tp} onChange={(e) => setF({ ...f, tp: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>止损触发价</small><input inputMode="decimal" aria-label="止损触发价" value={f.sl} onChange={(e) => setF({ ...f, sl: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" placeholder={`留空为全部仓位（${p.qty}）`} value={f.qty} onChange={(e) => setF({ ...f, qty: num(e.target.value) })} /></div>
        <p className="tk-txt g">触发后以市价平仓。{cur.tp || cur.sl ? "清空某一项并确认将撤销该止盈/止损。" : ""}</p>
      </div>
    </Modal>
  );
}

/** 修改计划委托：PUT /futures/plan-orders/{id}（触发价 / 触发价类型 / 委托价 / 数量）；委托价留空 = 触发后市价 */
function PlanEditModal({ o, onClose, onDone }: { o: Algo; onClose: () => void; onDone: () => void }) {
  const [f, setF] = useState({ trig: o.triggerPrice ?? "", tt: (o.triggerPriceType ?? "LAST") as "LAST" | "MARK", price: o.price ?? "", qty: o.qty ?? "" });
  const [busy, setBusy] = useState(false);
  const ok = async () => {
    if (!Number(f.trig)) return toast("请输入触发价", "err");
    if (f.qty && !(Number(f.qty) > 0)) return toast("数量必须大于 0", "err");
    const body: S["FuturesAlgoAmendRequest"] = {};
    if (f.trig !== o.triggerPrice) body.triggerPrice = f.trig;
    if (f.tt !== (o.triggerPriceType ?? "LAST")) body.triggerPriceType = f.tt;
    if (f.price !== (o.price ?? "")) { if (Number(f.price)) { body.orderType = "limit"; body.price = f.price; } else body.orderType = "market"; }
    if (f.qty && f.qty !== o.qty) body.qty = f.qty;
    if (!Object.keys(body).length) return toast("未做任何修改", "err");
    setBusy(true);
    const r = await rest("PUT", `/futures/plan-orders/${o.algoId}`, body);
    setBusy(false);
    if (!r.ok) { const e = r.error ?? ""; if (r.status === 404 || r.status === 405) return toast("当前服务暂不支持修改计划委托，请撤销后重新下单", "err"); return toast(/INVALID_TRIGGER|TRIGGER_DEVIATION/.test(e) ? "触发价不合理" : /INVALID_PRICE/.test(e) ? "委托价不合理" : /INVALID_QTY/.test(e) ? "数量不合法" : /ORDER_NOT_OPEN/.test(e) ? "委托已触发或已撤销" : /NOT_FOUND|404/.test(e) ? "委托不存在或后端暂不支持修改" : e || "修改失败", "err"); }
    toast("计划委托已修改"); onDone();
  };
  return (
    <Modal title={`修改计划委托 · ${o.symbol}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <div className="tk-ed">
        <div className="tk-tif">{(["LAST", "MARK"] as const).map((k) => <b key={k} className={f.tt === k ? "on" : ""} onClick={() => setF({ ...f, tt: k })}>{`按${TT[k]}触发`}</b>)}</div>
        <div className="inp"><small>触发价</small><input inputMode="decimal" aria-label="触发价" value={f.trig} onChange={(e) => setF({ ...f, trig: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>委托价</small><input inputMode="decimal" aria-label="委托价" placeholder="留空则市价" value={f.price} onChange={(e) => setF({ ...f, price: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" aria-label="数量" value={f.qty} onChange={(e) => setF({ ...f, qty: num(e.target.value) })} /></div>
      </div>
    </Modal>
  );
}

/** 限价平仓：只减仓限价单，数量默认等于当前持仓 */
function LimitCloseModal({ p, onClose, onDone }: { p: Pos; onClose: () => void; onDone: () => void }) {
  const [price, setPrice] = useState("");
  const [qty, setQty] = useState(p.qty.replace(/^-/, ""));
  const [busy, setBusy] = useState(false);
  const ok = async () => {
    if (!(Number(price) > 0)) return toast("请输入价格", "err");
    if (!(Number(qty) > 0)) return toast("数量必须大于 0", "err");
    const hedge = p.positionSide === "LONG" || p.positionSide === "SHORT";
    setBusy(true);
    const r = await rest("POST", "/futures/orders", { symbol: p.symbol, side: sideTxt(p.side) === "多" ? "sell" : "buy", qty, type: "limit", timeInForce: "GTC", price, reduceOnly: hedge ? undefined : true, positionSide: hedge ? p.positionSide : undefined });
    setBusy(false);
    if (!r.ok) return toast(r.error || "下单失败", "err", 4000);
    toast("限价平仓已提交"); onDone();
  };
  return (
    <Modal title={`限价平仓 · ${p.symbol}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <div className="tk-ed">
        <div className="inp"><small>价格</small><input inputMode="decimal" aria-label="平仓价格" value={price} onChange={(e) => setPrice(num(e.target.value))} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" aria-label="平仓数量" value={qty} onChange={(e) => setQty(num(e.target.value))} /></div>
        <p className="tk-txt g">只减仓。不会增加持仓。</p>
      </div>
    </Modal>
  );
}

/** 反手：先市价平掉当前仓位，再按相同数量反向市价开仓 */
function ReverseModal({ p, onClose, onDone }: { p: Pos; onClose: () => void; onDone: () => void }) {
  const [busy, setBusy] = useState(false);
  const ok = async () => {
    const hedge = p.positionSide === "LONG" || p.positionSide === "SHORT";
    const qty = p.qty.replace(/^-/, "");
    setBusy(true);
    const c = await rest("POST", `/futures/positions/${p.symbol}/close`, hedge ? { positionSide: p.positionSide } : {});
    if (!c.ok) { setBusy(false); return toast(c.error || "平仓失败", "err", 4000); }
    const r = await rest("POST", "/futures/orders", { symbol: p.symbol, side: sideTxt(p.side) === "多" ? "sell" : "buy", qty, type: "market", timeInForce: "IOC", positionSide: hedge ? (p.positionSide === "LONG" ? "SHORT" : "LONG") : undefined });
    setBusy(false);
    onDone();
    if (!r.ok) return toast(r.error || "反向开仓失败，原仓位已平", "err", 4000);
    toast("已反向开仓");
  };
  return (
    <Modal title={`反手 · ${p.symbol}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <p className="tk-txt">以市价平掉当前{sideTxt(p.side)}仓 {p.qty}，并按相同数量反向开仓。</p>
    </Modal>
  );
}
