"use client";
// 合约下单区底部：仓位 / 当前委托 / 计划委托 / 止盈止损 / 历史委托 / 历史仓位 / 成交明细（可只看当前合约）
// 对接 GET /futures/positions、/futures/orders、/futures/plan-orders、/futures/tpsl-orders、/futures/positions/history、/futures/trades；
// 改单 PUT /futures/orders/{id}，持仓止盈止损 POST /futures/tpsl-orders。
import { useEffect, useState } from "react";
import type { components } from "@linbit/api-client";
import { useRest, rest } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";
import { fmtLocal } from "@/lib/time";
import { Modal } from "./TradeKit";
import { tabProps } from "@/lib/a11y";

type S = components["schemas"];
type Algo = S["FuturesAlgoOrder"];
type PosHist = S["FuturesPositionHistory"];
type Pos = { symbol: string; side: string; positionSide?: string; qty: string; entryPrice: string; markPrice: string; unrealizedPnl: string; roe: string; leverage: number; margin: string; liquidationPrice: string; marginMode: string };
type Ord = { orderId: string; symbol: string; side: string; positionSide?: string; type: string; timeInForce?: string; price: string; qty: string; filledQty: string; status: string; leverage: number; reduceOnly?: boolean; createdAt: number | string };
const T = ["仓位", "当前委托", "计划委托", "止盈止损", "历史委托", "历史仓位", "成交明细"] as const;
type Fill = { tradeId: number; symbol: string; side: "buy" | "sell"; price: string; qty: string; fee: string; role: "maker" | "taker"; time: string };
const sideTxt = (s: string) => (/buy|long/i.test(s) ? "多" : "空");
const num = (v: string) => v.replace(/[^\d.]/g, "");
const TT: Record<string, string> = { LAST: "最新价", MARK: "标记价" };
const ST: Record<string, string> = { NEW: "未成交", PARTIALLY_FILLED: "部分成交", FILLED: "已成交", CANCELED: "已撤销", REJECTED: "已拒绝", EXPIRED: "已过期", active: "待触发", triggered: "已触发", canceled: "已撤销", failed: "失败", closed: "已平仓", liquidated: "强平", adl: "自动减仓" };
const TIF: Record<string, string> = { GTC: "GTC", IOC: "IOC", POST_ONLY: "只做 Maker", GTX: "只做 Maker" };
const dir = (o: { side?: string; positionSide?: string; reduceOnly?: boolean }) => {
  const buy = /buy/i.test(o.side ?? "");
  if (o.positionSide === "LONG") return [buy ? "开多" : "平多", buy];
  if (o.positionSide === "SHORT") return [buy ? "平空" : "开空", buy];
  return [o.reduceOnly ? (buy ? "平空" : "平多") : buy ? "买入" : "卖出", buy] as const;
};

export function FutOrders({ symbol }: { symbol?: string }) {
  const authed = !!useAuthStore((s) => s.accessToken);
  const [tab, setTab] = useState(0);
  const [edit, setEdit] = useState<Ord | null>(null);
  const [tp, setTp] = useState<Pos | null>(null);
  const [only, setOnly] = useState(false);
  const pos = useRest<{ positions: Pos[] }>(authed ? "/futures/positions" : null);
  const open = useRest<{ orders: Ord[] }>(authed ? "/futures/orders?status=open" : null);
  const plan = useRest<{ orders: Algo[] }>(authed ? "/futures/plan-orders?status=active" : null);
  const tpsl = useRest<{ orders: Algo[] }>(authed ? "/futures/tpsl-orders?status=active" : null);
  const hist = useRest<{ orders: Ord[] }>(authed && tab === 4 ? "/futures/orders" : null);
  const ph = useRest<{ positions: PosHist[] }>(authed && tab === 5 ? "/futures/positions/history" : null);
  const fills = useRest<{ trades: Fill[] }>(authed && tab === 6 ? "/futures/trades?limit=100" : null);
  const mine = <X extends { symbol?: string }>(L: X[] | undefined) => (L ?? []).filter((x) => !only || !symbol || x.symbol === symbol);
  const P = mine(pos.data?.positions), O = mine(open.data?.orders), PL = mine(plan.data?.orders), TS = mine(tpsl.data?.orders);
  const H = mine(hist.data?.orders).filter((o) => o.status !== "NEW" && o.status !== "PARTIALLY_FILLED"), PH = mine(ph.data?.positions), FL = mine(fills.data?.trades);
  const cur = [pos, open, plan, tpsl, hist, ph, fills][tab];
  const reloadAll = () => { pos.reload(); open.reload(); plan.reload(); tpsl.reload(); };
  const del = async (path: string, ok: string) => {
    const r = await rest("DELETE", path);
    if (!r.ok) return toast(r.error || "撤单失败", "err");
    toast(ok); reloadAll();
  };
  const close = async (p: Pos) => {
    const r = await rest("POST", `/futures/positions/${p.symbol}/close`, p.positionSide && p.positionSide !== "BOTH" ? { positionSide: p.positionSide } : {});
    if (!r.ok) return toast(r.error || "平仓失败", "err");
    toast("已提交市价平仓"); pos.reload();
  };
  useEffect(() => { window.addEventListener("lb-fut-orders", reloadAll); return () => window.removeEventListener("lb-fut-orders", reloadAll); }); // eslint-disable-line
  const labels = [`仓位(${P.length})`, `当前委托(${O.length})`, `计划委托(${PL.length})`, `止盈止损(${TS.length})`, "历史委托", "历史仓位", "成交明细"];
  const empty = <p className="empty">暂无数据</p>;
  const algoTable = (L: Algo[], kind: "plan" | "tpsl") => L.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>{kind === "plan" ? "方向" : "类型"}</th><th>触发条件</th><th>委托价</th><th>数量</th><th>状态</th><th /></tr></thead><tbody>
    {L.map((o) => { const [d, b] = dir(o); return <tr key={o.algoId}><td>{fmtLocal(o.createdAt)}</td><td>{o.symbol}</td><td className={kind === "plan" ? (b ? "up" : "down") : o.algoType === "tp" ? "up" : "down"}>{kind === "plan" ? d : o.algoType === "tp" ? "止盈" : "止损"}</td><td>{TT[o.triggerPriceType ?? "LAST"]} {o.triggerDirection === "down" ? "≤" : "≥"} {o.triggerPrice}</td><td>{o.price || "市价"}</td><td>{o.qty || "全部仓位"}</td><td>{ST[o.status ?? ""] ?? o.status}</td><td><a className="lime2" onClick={() => del(`/futures/${kind === "plan" ? "plan" : "tpsl"}-orders/${o.algoId}`, "已撤销")}>撤销</a></td></tr>; })}
  </tbody></table> : empty;
  return (
    <div className="orders" data-guide="orders">
      <div className="pt sm" role="tablist" aria-label="合约订单">{labels.map((x, i) => <b key={x} {...tabProps(tab === i, () => setTab(i))} className={tab === i ? "on" : ""} onClick={() => setTab(i)}>{x}</b>)}<span className="sp" />{cur.source === "demo" && <span className="fx-src">接口不可达</span>}{symbol && <label className="tk-only"><input type="checkbox" checked={only} onChange={(e) => setOnly(e.target.checked)} />只看当前合约</label>}</div>
      {!authed ? <p className="empty">登录后查看{T[tab]}</p>
        : cur.loading ? <div style={{ padding: 12 }}><div className="lb-skel" style={{ height: 80 }} /></div>
        : tab === 0 ? (P.length ? <table className="sotbl"><thead><tr><th>合约</th><th>数量</th><th>开仓价</th><th>标记价</th><th>强平价</th><th>保证金</th><th>未实现盈亏(ROE)</th><th>止盈/止损</th><th /></tr></thead><tbody>
            {P.map((p) => { const ps = TS.filter((o) => o.symbol === p.symbol && (!p.positionSide || o.positionSide === p.positionSide)); const tpv = ps.find((o) => o.algoType === "tp")?.triggerPrice, slv = ps.find((o) => o.algoType === "sl")?.triggerPrice;
              return <tr key={p.symbol + p.side + (p.positionSide ?? "")}><td>{p.symbol} <span className={sideTxt(p.side) === "多" ? "up" : "down"}>{sideTxt(p.side)} {p.leverage}x</span></td><td>{p.qty}</td><td>{p.entryPrice}</td><td>{p.markPrice}</td><td>{p.liquidationPrice}</td><td>{p.margin}</td><td className={+p.unrealizedPnl >= 0 ? "up" : "down"}>{p.unrealizedPnl} ({(+p.roe * 100).toFixed(2)}%)</td><td>{tpv || "--"} / {slv || "--"} <a className="lime2" onClick={() => setTp(p)}>设置</a></td><td><a className="lime2" onClick={() => close(p)}>市价平仓</a></td></tr>; })}
          </tbody></table> : <p className="empty">暂无仓位</p>)
        : tab === 2 ? algoTable(PL, "plan")
        : tab === 3 ? algoTable(TS, "tpsl")
        : tab === 6 ? (FL.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>方向</th><th>成交价</th><th>成交量</th><th>手续费</th><th>角色</th></tr></thead><tbody>
            {FL.map((t) => <tr key={t.tradeId}><td>{fmtLocal(t.time)}</td><td>{t.symbol}</td><td className={t.side === "buy" ? "up" : "down"}>{t.side === "buy" ? "买入" : "卖出"}</td><td>{t.price}</td><td>{t.qty}</td><td>{t.fee}</td><td>{t.role === "maker" ? "Maker" : "Taker"}</td></tr>)}
          </tbody></table> : empty)
        : tab === 5 ? (PH.length ? <table className="sotbl"><thead><tr><th>平仓时间</th><th>合约</th><th>方向</th><th>开仓均价</th><th>平仓均价</th><th>最大持仓</th><th>已实现盈亏</th><th>手续费 / 资金费</th><th>状态</th></tr></thead><tbody>
            {PH.map((p) => <tr key={p.id}><td>{fmtLocal(p.closeTime)}</td><td>{p.symbol}</td><td className={p.side === "long" ? "up" : "down"}>{p.side === "long" ? "多" : "空"} {p.leverage}x</td><td>{p.entryPrice}</td><td>{p.closePrice}</td><td>{p.maxQty}</td><td className={+(p.netPnl ?? p.realizedPnl ?? 0) >= 0 ? "up" : "down"}>{p.netPnl ?? p.realizedPnl}</td><td>{p.fee} / {p.fundingFee}</td><td>{ST[p.status ?? ""] ?? p.status}</td></tr>)}
          </tbody></table> : empty)
        : (() => { const L = tab === 1 ? O : H; return L.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>类型</th><th>方向</th><th>价格</th><th>数量</th><th>已成交</th><th>状态</th>{tab === 1 && <th />}</tr></thead><tbody>
            {L.map((o) => { const [d, b] = dir(o); return <tr key={o.orderId}><td>{fmtLocal(o.createdAt)}</td><td>{o.symbol}</td><td>{o.type === "limit" ? "限价" : o.type === "market" ? "市价" : o.type}{o.timeInForce && o.type === "limit" ? ` · ${TIF[o.timeInForce] ?? o.timeInForce}` : ""}</td><td className={b ? "up" : "down"}>{d}</td><td>{o.price}</td><td>{o.qty}</td><td>{o.filledQty}</td><td>{ST[o.status] ?? o.status}</td>{tab === 1 && <td>{o.type === "limit" && <a className="lime2" onClick={() => setEdit(o)}>改单</a>} <a className="lime2" onClick={() => del(`/futures/orders/${o.orderId}`, "已撤单")}>撤单</a></td>}</tr>; })}
          </tbody></table> : empty; })()}
      {edit && <EditModal o={edit} onClose={() => setEdit(null)} onDone={() => { setEdit(null); open.reload(); }} />}
      {tp && <TpslModal p={tp} onClose={() => setTp(null)} onDone={() => { setTp(null); tpsl.reload(); }} />}
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
    if (!r.ok) { const e = r.error ?? ""; return toast(/AMEND_LIMIT/.test(e) ? "改单次数已达上限" : /POST_ONLY_REJECTED/.test(e) ? "只做 Maker 订单改价后会立即成交，已被拒绝" : /NO_CHANGE/.test(e) ? "价格和数量均未修改" : e || "改单失败", "err"); }
    toast("改单成功"); onDone();
  };
  return (
    <Modal title={`改单 · ${o.symbol}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <div className="tk-ed">
        <div className="inp"><small>价格</small><input inputMode="decimal" value={price} onChange={(e) => setPrice(num(e.target.value))} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" value={qty} onChange={(e) => setQty(num(e.target.value))} /></div>
        <p className="tk-txt g">改价或增加数量会失去时间优先级；减少数量保留原排队位置。已成交 {o.filledQty}。</p>
      </div>
    </Modal>
  );
}

/** 持仓止盈止损：POST /futures/tpsl-orders，可部分仓位，单次可同时建 TP 与 SL */
function TpslModal({ p, onClose, onDone }: { p: Pos; onClose: () => void; onDone: () => void }) {
  const [f, setF] = useState({ tp: "", sl: "", qty: "", tt: "MARK" as "LAST" | "MARK" });
  const [busy, setBusy] = useState(false);
  const ok = async () => {
    if (!f.tp && !f.sl) return toast("请填写止盈或止损触发价", "err");
    const body: S["FuturesTpslOrderRequest"] = {
      symbol: p.symbol,
      positionSide: (p.positionSide as S["FuturesTpslOrderRequest"]["positionSide"]) || undefined,
      qty: f.qty || undefined,
      tpTriggerPrice: f.tp || undefined, tpTriggerPriceType: f.tp ? f.tt : undefined,
      slTriggerPrice: f.sl || undefined, slTriggerPriceType: f.sl ? f.tt : undefined,
    };
    setBusy(true);
    const r = await rest("POST", "/futures/tpsl-orders", body);
    setBusy(false);
    if (!r.ok) { const e = r.error ?? ""; return toast(/INVALID_TPSL/.test(e) ? "止盈止损价格不合理" : /ALGO_LIMIT/.test(e) ? "条件单数量已达上限" : /NO_POSITION/.test(e) ? "仓位已不存在" : e || "设置失败", "err"); }
    toast("止盈止损已设置"); onDone();
  };
  return (
    <Modal title={`止盈止损 · ${p.symbol} ${sideTxt(p.side)}`} onClose={onClose} foot={<><button className="tk-b2" onClick={onClose}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>确认</button></>}>
      <div className="tk-ed">
        <div className="kv2 g"><span>开仓价 / 标记价</span><span>{p.entryPrice} / {p.markPrice}</span></div>
        <div className="tk-tif">{(["MARK", "LAST"] as const).map((k) => <b key={k} className={f.tt === k ? "on" : ""} onClick={() => setF({ ...f, tt: k })}>按{TT[k]}触发</b>)}</div>
        <div className="inp"><small>止盈触发价</small><input inputMode="decimal" value={f.tp} onChange={(e) => setF({ ...f, tp: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>止损触发价</small><input inputMode="decimal" value={f.sl} onChange={(e) => setF({ ...f, sl: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" placeholder={`留空为全部仓位（${p.qty}）`} value={f.qty} onChange={(e) => setF({ ...f, qty: num(e.target.value) })} /></div>
        <p className="tk-txt g">触发后以市价平仓。</p>
      </div>
    </Modal>
  );
}
