"use client";
// 合约下单区底部：仓位 / 当前委托 / 计划委托 / 止盈止损 / 历史委托 / 历史仓位 / 成交明细（可只看当前合约）
// 对接 GET /futures/positions、/futures/orders、/futures/plan-orders、/futures/tpsl-orders、/futures/positions/history、/futures/trades；
// 改单 PUT /futures/orders/{id}，持仓止盈止损 POST /futures/tpsl-orders。
import { stLabel, isOpenStatus, futOrderOf, closeToast } from "@/lib/orderStatus";
import { useEffect, useRef, useState } from "react";
import type { components } from "@linbit/api-client";
import { useRest, rest, errText } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";
import { fmtLocal } from "@/lib/time";
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
  return <td data-testid="pos-pnl" className={pnl >= 0 ? "up" : "down"}>{live ? pnl.toFixed(2) : p.unrealizedPnl} ({isFinite(roe) ? (roe * 100).toFixed(2) : "--"}%)</td>;
}
function MarkCell({ sym, fb }: { sym: string; fb: string }) { return <>{useMarkPrice(sym, fb)}</>; }
import { tabProps } from "@/lib/a11y";
import { fmtFeeExact } from "@/lib/format";
import { liqText } from "@/lib/orderDisplay";

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
  const [tp, setTp] = useState<{ p: Pos; tp?: Algo; sl?: Algo } | null>(null);
  const [pe, setPe] = useState<Algo | null>(null);
  const [only, setOnly] = useState(false);
  const pos = useRest<{ positions: Pos[] }>(authed ? "/futures/positions" : null);
  const posRef = useRef(pos); posRef.current = pos;
  const open = useRest<{ orders: Ord[] }>(authed ? "/futures/orders?status=open" : null);
  const plan = useRest<{ orders: Algo[] }>(authed ? "/futures/plan-orders?status=active" : null);
  const tpsl = useRest<{ orders: Algo[] }>(authed ? "/futures/tpsl-orders?status=active" : null);
  const hist = useRest<{ orders: Ord[] }>(authed && tab === 4 ? "/futures/orders" : null);
  const ph = useRest<{ positions: PosHist[] }>(authed && tab === 5 ? "/futures/positions/history" : null);
  const fills = useRest<{ trades: Fill[] }>(authed && tab === 6 ? "/futures/trades?limit=100" : null);
  const mine = <X extends { symbol?: string }>(L: X[] | undefined) => (L ?? []).filter((x) => !only || !symbol || x.symbol === symbol);
  const P = mine(pos.data?.positions), O = mine(open.data?.orders), PL = mine(plan.data?.orders), TS = mine(tpsl.data?.orders);
  const H = mine(hist.data?.orders).filter((o) => !isOpenStatus(o.status)), PH = mine(ph.data?.positions), FL = mine(fills.data?.trades);
  const cur = [pos, open, plan, tpsl, hist, ph, fills][tab];
  // 下单/撤单/改单后静默刷新（不闪骨架屏）；撮合与条件单落库有延迟，再补两次
  const refreshAll = () => { pos.refresh(); open.refresh(); plan.refresh(); tpsl.refresh(); hist.refresh(); ph.refresh(); fills.refresh(); };
  const reloadAll = () => { refreshAll(); window.setTimeout(refreshAll, 800); window.setTimeout(refreshAll, 2500); };
  // 仓位标记价 / 未实现盈亏：每 3s 静默轮询（页面隐藏时暂停）
  useEffect(() => {
    if (!authed) return;
    const id = window.setInterval(() => { if (document.visibilityState === "visible") posRef.current.refresh(); }, 3000);
    return () => window.clearInterval(id);
  }, [authed]);
  const [acting, setActing] = useState<string | null>(null);
  const del = async (path: string, ok: string) => {
    if (acting) return; setActing(path);
    const r = await rest("DELETE", path).catch(() => ({ ok: false, source: "demo" } as const)); setActing(null);
    if (!r.ok) return toast(errText(r, "撤单失败，请稍后重试"), "err");
    toast(ok); reloadAll();
  };
  const close = async (p: Pos) => {
    const key = "close:" + p.symbol + (p.positionSide || ""); if (acting) return; setActing(key);
    const r = await rest<any>("POST", `/futures/positions/${p.symbol}/close`, p.positionSide && p.positionSide !== "BOTH" ? { positionSide: p.positionSide } : {}).catch(() => ({ ok: false, source: "demo" } as const)); setActing(null);
    if (!r.ok) return toast(errText(r, "平仓失败，请稍后重试"), "err");
    const o = futOrderOf(r.data); const m = closeToast(o.status, o.filledQty); toast(m.t, m.ok ? undefined : "err"); reloadAll();
  };
  const rlRef = useRef(reloadAll); rlRef.current = reloadAll;
  useEffect(() => { const f = () => rlRef.current(); window.addEventListener("lb-fut-orders", f); return () => window.removeEventListener("lb-fut-orders", f); }, []);
  const labels = [`仓位(${P.length})`, `当前委托(${O.length})`, `计划委托(${PL.length})`, `止盈止损(${TS.length})`, "历史委托", "历史仓位", "成交明细"];
  const empty = <p className="empty">暂无数据</p>;
  const algoTable = (L: Algo[], kind: "plan" | "tpsl") => L.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>{kind === "plan" ? "方向" : "类型"}</th><th>触发条件</th><th>委托价</th><th>数量</th><th>状态</th><th /></tr></thead><tbody>
    {L.map((o) => { const [d, b] = dir(o); return <tr key={o.algoId}><td>{fmtLocal(o.createdAt)}</td><td>{o.symbol}</td><td className={kind === "plan" ? (b ? "up" : "down") : o.algoType === "tp" ? "up" : "down"}>{kind === "plan" ? d : o.algoType === "tp" ? "止盈" : "止损"}</td><td>{TT[o.triggerPriceType ?? "LAST"]} {o.triggerDirection === "down" ? "≤" : "≥"} {o.triggerPrice}</td><td>{o.price || "市价"}</td><td>{o.qty || "全部仓位"}</td><td>{stLabel(o.status)}</td><td>{kind === "plan" && <a className="lime2" onClick={() => setPe(o)}>修改</a>} <a className="lime2" onClick={() => del(`/futures/${kind === "plan" ? "plan" : "tpsl"}-orders/${o.algoId}`, "已撤销")}>撤销</a></td></tr>; })}
  </tbody></table> : empty;
  return (
    <div className="orders" data-guide="orders">
      <div className="pt sm" role="tablist" aria-label="合约订单">{labels.map((x, i) => <b key={x} {...tabProps(tab === i, () => setTab(i))} className={tab === i ? "on" : ""} onClick={() => setTab(i)}>{x}</b>)}<span className="sp" />{cur.source === "demo" && <span className="fx-src" role="button" style={{ cursor: "pointer" }} onClick={() => cur.reload()}>加载失败，点击重试</span>}{symbol && <label className="tk-only"><input type="checkbox" checked={only} onChange={(e) => setOnly(e.target.checked)} />只看当前合约</label>}</div>
      {!authed ? <p className="empty">登录后查看{T[tab]}</p>
        : cur.loading && !cur.data ? <div style={{ padding: 12 }}><div className="lb-skel" style={{ height: 80 }} /></div>
        : tab === 0 ? (P.length ? <table className="sotbl"><thead><tr><th>合约</th><th>数量</th><th>开仓价</th><th>标记价</th><th>强平价</th><th>保证金</th><th>未实现盈亏(ROE)</th><th>止盈/止损</th><th /></tr></thead><tbody>
            {P.map((p) => { const ps = TS.filter((o) => o.symbol === p.symbol && (!p.positionSide || o.positionSide === p.positionSide)); const tpo = ps.find((o) => o.algoType === "tp"), slo = ps.find((o) => o.algoType === "sl"); const tpv = tpo?.triggerPrice, slv = slo?.triggerPrice;
              return <tr key={p.symbol + p.side + (p.positionSide ?? "")}><td>{p.symbol} <span className={sideTxt(p.side) === "多" ? "up" : "down"}>{sideTxt(p.side)} {p.leverage}x</span></td><td>{p.qty}</td><td>{p.entryPrice}</td><td data-testid="pos-mark"><MarkCell sym={p.symbol} fb={p.markPrice} /></td><td title={liqText(p.liquidationPrice, p.markPrice, p.side) ? undefined : (p.marginMode === "cross" ? "全仓保证金充足，暂无强平价" : "保证金充足，暂无强平价")}>{liqText(p.liquidationPrice, p.markPrice, p.side) ?? "--"}</td><td>{p.margin}</td><PnlCell p={p} /><td>{tpv || "--"} / {slv || "--"} <a className="lime2" onClick={() => setTp({ p, tp: tpo, sl: slo })}>{tpo || slo ? "修改" : "设置"}</a></td><td><a className="lime2" aria-disabled={!!acting} onClick={() => close(p)}>{acting === "close:" + p.symbol + (p.positionSide || "") ? "平仓中…" : "市价平仓"}</a></td></tr>; })}
          </tbody></table> : <p className="empty">暂无仓位</p>)
        : tab === 2 ? algoTable(PL, "plan")
        : tab === 3 ? algoTable(TS, "tpsl")
        : tab === 6 ? (FL.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>方向</th><th>成交价</th><th>成交量</th><th>手续费</th><th>角色</th></tr></thead><tbody>
            {FL.map((t) => <tr key={t.tradeId}><td>{fmtLocal(t.time)}</td><td>{t.symbol}</td><td className={t.side === "buy" ? "up" : "down"}>{t.side === "buy" ? "买入" : "卖出"}</td><td>{t.price}</td><td>{t.qty}</td><td>{fmtFeeExact(t.fee)}</td><td>{t.role === "maker" ? "Maker" : "Taker"}</td></tr>)}
          </tbody></table> : empty)
        : tab === 5 ? (PH.length ? <table className="sotbl"><thead><tr><th>平仓时间</th><th>合约</th><th>方向</th><th>开仓均价</th><th>平仓均价</th><th>最大持仓</th><th>已实现盈亏</th><th>手续费 / 资金费</th><th>状态</th></tr></thead><tbody>
            {PH.map((p) => <tr key={p.id}><td>{fmtLocal(p.closeTime)}</td><td>{p.symbol}</td><td className={p.side === "long" ? "up" : "down"}>{p.side === "long" ? "多" : "空"} {p.leverage}x</td><td>{p.entryPrice}</td><td>{p.closePrice}</td><td>{p.maxQty}</td><td className={+(p.netPnl ?? p.realizedPnl ?? 0) >= 0 ? "up" : "down"}>{fmtFeeExact(p.netPnl ?? p.realizedPnl)}</td><td>{fmtFeeExact(p.fee)} / {fmtFeeExact(p.fundingFee)}</td><td>{stLabel(p.status)}</td></tr>)}
          </tbody></table> : empty)
        : (() => { const L = tab === 1 ? O : H; return L.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>类型</th><th>方向</th><th>价格</th><th>数量</th><th>已成交</th><th>状态</th>{tab === 1 && <th />}</tr></thead><tbody>
            {L.map((o) => { const [d, b] = dir(o); return <tr key={o.orderId}><td>{fmtLocal(o.createdAt)}</td><td>{o.symbol}</td><td>{o.type === "limit" ? "限价" : o.type === "market" ? "市价" : o.type}{o.timeInForce && o.type === "limit" ? ` · ${TIF[o.timeInForce] ?? o.timeInForce}` : ""}</td><td className={b ? "up" : "down"}>{d}</td><td>{o.price}</td><td>{o.qty}</td><td>{o.filledQty}</td><td>{stLabel(o.status)}</td>{tab === 1 && <td>{o.type === "limit" && <a className="lime2" onClick={() => setEdit(o)}>改单</a>} <a className="lime2" onClick={() => del(`/futures/orders/${o.orderId}`, "已撤单")}>撤单</a></td>}</tr>; })}
          </tbody></table> : empty; })()}
      {edit && <EditModal o={edit} onClose={() => setEdit(null)} onDone={() => { setEdit(null); reloadAll(); }} />}
      {tp && <TpslModal p={tp.p} cur={tp} onClose={() => setTp(null)} onDone={() => { setTp(null); reloadAll(); }} />}
      {pe && <PlanEditModal o={pe} onClose={() => setPe(null)} onDone={() => { setPe(null); reloadAll(); }} />}
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
        <p className="tk-txt g">改价或增加数量会失去时间优先级；减少数量保留原排队位置。已成交 {o.filledQty}。</p>
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
        <div className="tk-tif">{(["MARK", "LAST"] as const).map((k) => <b key={k} className={f.tt === k ? "on" : ""} onClick={() => setF({ ...f, tt: k })}>按{TT[k]}触发</b>)}</div>
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
        <div className="tk-tif">{(["LAST", "MARK"] as const).map((k) => <b key={k} className={f.tt === k ? "on" : ""} onClick={() => setF({ ...f, tt: k })}>按{TT[k]}触发</b>)}</div>
        <div className="inp"><small>触发价</small><input inputMode="decimal" aria-label="触发价" value={f.trig} onChange={(e) => setF({ ...f, trig: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>委托价</small><input inputMode="decimal" aria-label="委托价" placeholder="留空则市价" value={f.price} onChange={(e) => setF({ ...f, price: num(e.target.value) })} /><em>USDT</em></div>
        <div className="inp"><small>数量</small><input inputMode="decimal" aria-label="数量" value={f.qty} onChange={(e) => setF({ ...f, qty: num(e.target.value) })} /></div>
      </div>
    </Modal>
  );
}
