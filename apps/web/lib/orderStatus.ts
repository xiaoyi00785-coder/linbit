/**
 * 全站统一的状态中文映射（大小写不敏感）：订单 / 条件单 / 充值 / 提现 / 划转 / 闪兑 / 内部转账 / 策略等历史表格共用。
 * 后端各服务的状态枚举不同（如合约划转用 done，钱包用 completed），都收在这里，避免页面直接显示英文枚举。
 * 未知值原样返回（便于发现新枚举后补充），空值为 "--"。
 */
const M: Record<string, string> = {
  // 订单 / 条件单
  new: "未成交", pending: "待处理", pending_new: "待处理", partially_filled: "部分成交", partial: "部分成交", filled: "已成交",
  canceled: "已撤销", cancelled: "已撤销", pending_cancel: "撤销中", partially_canceled: "部分成交已撤销",
  expired: "已过期", rejected: "已拒绝", failed: "失败", open: "未成交", resting: "挂单中",
  active: "待触发", triggering: "触发中", triggered: "已触发", closed: "已平仓", liquidated: "强平", adl: "自动减仓",
  // 资金 / 划转 / 闪兑 / 内部转账
  completed: "已完成", complete: "已完成", done: "已完成", success: "成功", succeeded: "成功", processing: "处理中",
  approved: "已通过", pending_review: "审核中", pending_approval: "待审批", awaiting_second_review: "待复核", awaiting_second: "待复核",
  applying: "处理中", applied: "已完成", settling: "结算中", settled: "已结算", paid: "已支付", issued: "已发放", released: "已释放",
  // 策略 / 理财 / 借贷
  running: "运行中", stopping: "停止中", stopped: "已停止", holding: "持有中", matured: "已到期", unlocking: "解锁中", locked: "已锁定",
  repaid: "已还清", revoked: "已撤销", in_progress: "进行中", scheduled: "已排期", disabled: "已停用", suspended: "已暂停",
};
/** 按业务场景覆盖个别词义：充值 pending = 链上确认中；提现 pending = 审核中。 */
const CTX: Record<string, Record<string, string>> = {
  deposit: { pending: "确认中", processing: "确认中" },
  withdraw: { pending: "审核中", processing: "处理中" },
};
export const stLabel = (s?: string | null, ctx?: "deposit" | "withdraw") => (s ? CTX[ctx ?? ""]?.[s.toLowerCase()] ?? M[s.toLowerCase()] ?? s : "--");
/** 已知状态值列表（供脚本校验：每个值都必须有中文映射） */
export const KNOWN_STATUSES = Object.keys(M);
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
