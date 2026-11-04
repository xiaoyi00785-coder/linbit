/** 订单/条件单状态中文映射（大小写不敏感），全站共享 */
const M: Record<string, string> = {
  new: "未成交", pending: "待处理", pending_new: "待处理", partially_filled: "部分成交", filled: "已成交",
  canceled: "已撤销", cancelled: "已撤销", pending_cancel: "撤销中", partially_canceled: "部分成交已撤销",
  expired: "已过期", rejected: "已拒绝", failed: "失败",
  active: "待触发", triggered: "已触发", closed: "已平仓", liquidated: "强平", adl: "自动减仓",
};
export const stLabel = (s?: string | null) => (s ? M[s.toLowerCase()] ?? s : "--");
export const isOpenStatus = (s?: string) => { const v = (s || "").toLowerCase(); return v === "new" || v === "partially_filled"; };
/** 合约下单/平仓接口返回 { order, trades }：取 order 的实际状态 */
export const futOrderOf = (d: any): { status?: string; filledQty?: string } => (d && typeof d === "object" && d.order ? d.order : d) ?? {};
/** 市价平仓按实际状态提示 */
export function closeToast(status?: string, filled?: string | number): { ok: boolean; t: string } {
  const v = (status || "").toLowerCase(); const f = Number(filled) || 0;
  if (v === "filled") return { ok: true, t: "已市价平仓" };
  if (v === "partially_filled") return { ok: true, t: "部分平仓成交" };
  if (v === "new" || v === "pending" || v === "pending_new" || !v) return { ok: true, t: "已提交市价平仓" };
  if (v === "canceled" || v === "cancelled" || v === "expired" || v === "partially_canceled")
    return f > 0 ? { ok: true, t: "部分平仓成交，剩余已撤销" } : { ok: false, t: "平仓未成交，已自动撤销（市场深度不足）" };
  if (v === "rejected") return { ok: false, t: "平仓被拒绝" };
  return { ok: false, t: `平仓状态：${stLabel(v)}` };
}
/** 按下单接口返回的实际状态给出提示 */
export function orderToast(status?: string, filled?: string | number): { ok: boolean; t: string } {
  const v = (status || "").toLowerCase(); const f = Number(filled) || 0;
  if (v === "filled") return { ok: true, t: "下单成功，已全部成交" };
  if (v === "partially_filled") return { ok: true, t: "下单成功，部分成交" };
  if (v === "new" || v === "pending" || v === "pending_new" || !v) return { ok: true, t: "下单成功，等待成交" };
  if (v === "canceled" || v === "cancelled" || v === "expired" || v === "partially_canceled")
    return f > 0 ? { ok: true, t: `部分成交，剩余${v === "expired" ? "已过期" : "已撤销"}` } : { ok: false, t: `订单未成交，已${v === "expired" ? "过期" : "自动撤销"}（市场深度不足）` };
  if (v === "rejected") return { ok: false, t: "订单被拒绝" };
  return { ok: false, t: `订单状态：${stLabel(v)}` };
}
