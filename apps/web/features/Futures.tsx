"use client";
// 合约下单区底部：仓位 / 当前委托 / 历史委托 —— 对接 GET /futures/positions、GET /futures/orders
import { useEffect, useState } from "react";
import { useRest, rest } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
import { toast } from "@/lib/ui";

type Pos = { symbol: string; side: string; qty: string; entryPrice: string; markPrice: string; unrealizedPnl: string; roe: string; leverage: number; margin: string; liquidationPrice: string; marginMode: string };
type Ord = { orderId: string; symbol: string; side: string; type: string; price: string; qty: string; filledQty: string; status: string; leverage: number; createdAt: number | string };
const T = ["仓位", "当前委托", "历史委托"] as const;
const sideTxt = (s: string) => (/buy|long/i.test(s) ? "多" : "空");
const ts = (v: number | string) => new Date(typeof v === "number" ? v : v).toLocaleString("zh-CN", { hour12: false });

export function FutOrders() {
  const authed = !!useAuthStore((s) => s.accessToken);
  const [tab, setTab] = useState(0);
  const pos = useRest<{ positions: Pos[] }>(authed ? "/futures/positions" : null);
  const open = useRest<{ orders: Ord[] }>(authed ? "/futures/orders?status=open" : null);
  const hist = useRest<{ orders: Ord[] }>(authed && tab === 2 ? "/futures/orders" : null);
  const P = pos.data?.positions ?? [], O = open.data?.orders ?? [], H = (hist.data?.orders ?? []).filter((o) => o.status !== "NEW" && o.status !== "PARTIALLY_FILLED");
  const cur = [pos, open, hist][tab];
  const cancel = async (id: string) => {
    const r = await rest("DELETE", `/futures/orders/${id}`);
    if (!r.ok) return toast(r.error || "撤单失败", "err");
    toast("已撤单"); open.reload();
  };
  const close = async (sym: string) => {
    const r = await rest("POST", `/futures/positions/${sym}/close`, {});
    if (!r.ok) return toast(r.error || "平仓失败", "err");
    toast("已提交市价平仓"); pos.reload();
  };
  useEffect(() => { const h = () => { pos.reload(); open.reload(); }; window.addEventListener("lb-fut-orders", h); return () => window.removeEventListener("lb-fut-orders", h); }); // eslint-disable-line
  const labels = [`仓位(${P.length})`, `当前委托(${O.length})`, "历史委托"];
  return (
    <div className="orders" data-guide="orders">
      <div className="pt sm">{labels.map((x, i) => <b key={x} className={tab === i ? "on" : ""} onClick={() => setTab(i)}>{x}</b>)}<span className="sp" />{cur.source === "demo" && <span className="fx-src">接口不可达</span>}</div>
      {!authed ? <p className="empty">登录后查看{T[tab]}</p>
        : cur.loading ? <div style={{ padding: 12 }}><div className="lb-skel" style={{ height: 80 }} /></div>
        : tab === 0 ? (P.length ? <table className="sotbl"><thead><tr><th>合约</th><th>数量</th><th>开仓价</th><th>标记价</th><th>强平价</th><th>保证金</th><th>未实现盈亏(ROE)</th><th /></tr></thead><tbody>
            {P.map((p) => <tr key={p.symbol + p.side}><td>{p.symbol} <span className={sideTxt(p.side) === "多" ? "up" : "down"}>{sideTxt(p.side)} {p.leverage}x</span></td><td>{p.qty}</td><td>{p.entryPrice}</td><td>{p.markPrice}</td><td>{p.liquidationPrice}</td><td>{p.margin}</td><td className={+p.unrealizedPnl >= 0 ? "up" : "down"}>{p.unrealizedPnl} ({(+p.roe * 100).toFixed(2)}%)</td><td><a className="lime2" onClick={() => close(p.symbol)}>市价平仓</a></td></tr>)}
          </tbody></table> : <p className="empty">暂无仓位</p>)
        : (() => { const L = tab === 1 ? O : H; return L.length ? <table className="sotbl"><thead><tr><th>时间</th><th>合约</th><th>类型</th><th>方向</th><th>价格</th><th>数量</th><th>已成交</th><th>状态</th>{tab === 1 && <th />}</tr></thead><tbody>
            {L.map((o) => <tr key={o.orderId}><td>{ts(o.createdAt)}</td><td>{o.symbol}</td><td>{o.type}</td><td className={/buy/i.test(o.side) ? "up" : "down"}>{/buy/i.test(o.side) ? "买" : "卖"}</td><td>{o.price}</td><td>{o.qty}</td><td>{o.filledQty}</td><td>{o.status}</td>{tab === 1 && <td><a className="lime2" onClick={() => cancel(o.orderId)}>撤单</a></td>}</tr>)}
          </tbody></table> : <p className="empty">暂无数据</p>; })()}
    </div>
  );
}
