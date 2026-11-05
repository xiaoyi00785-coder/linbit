/**
 * VIP 申请（POST /vip/applications，GET /vip/application）。纯函数、无运行时依赖，
 * 供 legacy/Pages2.tsx 的 Vip 页使用，并由 scripts/check-vip-apply.mjs 校验。
 *
 * 后端在提交时硬校验资格：总资产 ≥ 阈值 或 近 30 日交易量 ≥ 阈值；不满足 403 VIP_NOT_ELIGIBLE，
 * 数据不可用 503 VIP_ELIGIBILITY_UNAVAILABLE（可重试），已是 VIP 409 ALREADY_VIP，已有审核中 409 APPLICATION_PENDING。
 */

/** 联系方式类型（页面下拉的显示名）→ 后端 channel */
export const VIP_CHANNEL: Record<string, string> = { 邮箱: "email", 手机: "phone", Telegram: "telegram" };

export const vipPayload = (label: string, contact: string) => ({ channel: VIP_CHANNEL[label] || "", contact: contact.trim() });

/** 后端未返回 message 时的兜底文案（后端 message 优先） */
export const VIP_CODE_MSG: Record<string, string> = {
  VIP_NOT_ELIGIBLE: "暂不满足 VIP 申请条件，请在资产或交易量达标后再申请",
  VIP_ELIGIBILITY_UNAVAILABLE: "暂时无法核验您的资产与交易量，请稍后重试",
  ALREADY_VIP: "您已是 VIP",
  APPLICATION_PENDING: "已有审核中的申请，请耐心等待审核结果",
  VALIDATION_ERROR: "请检查联系方式后重试",
  AUTH_UNAUTHORIZED: "请先登录",
  UNAUTHORIZED: "请先登录",
};

export interface VipRestResult { ok: boolean; error?: string; code?: string; status?: number; source?: string }

/** 提交失败时展示给用户的文案：优先后端中文 message（不满足条件 / 已在审核 / 已是 VIP 等），否则按错误码兜底。 */
export function vipApplyError(r: VipRestResult): string {
  if (r.source === "network") return "服务暂不可用，请稍后重试";
  if (r.source === "api" && r.error && !/^HTTP \d+$/.test(r.error)) return r.error;
  if (r.code && VIP_CODE_MSG[r.code]) return VIP_CODE_MSG[r.code];
  return r.status === 503 ? VIP_CODE_MSG.VIP_ELIGIBILITY_UNAVAILABLE : "申请提交失败，请稍后重试";
}

export interface VipState { vip?: boolean; status?: string; conditions?: { minTotalAssetsUSDT?: string; min30dVolumeUSDT?: string } }

const grouped = (s: string) => s.replace(/\B(?=(\d{3})+(?!\d))/g, ",");

/** 申请要求文案：优先用后端实际执行的阈值（可配置），拿不到时回退到公示默认值。 */
export function vipRequirements(c?: VipState["conditions"]): [string, string] {
  const a = c?.minTotalAssetsUSDT && /^\d+(\.\d+)?$/.test(c.minTotalAssetsUSDT) ? c.minTotalAssetsUSDT : "500000";
  const v = c?.min30dVolumeUSDT && /^\d+(\.\d+)?$/.test(c.min30dVolumeUSDT) ? c.min30dVolumeUSDT : "50000000";
  const wan = Number(v) % 10000 === 0 ? grouped(String(Number(v) / 10000)) + " 万" : grouped(v);
  return [`账户总资产（资金 + 现货 + 合约账户）≥ ${grouped(a)} USDT`, `近 30 日交易量（现货 + 合约）≥ ${wan} USDT`];
}

/** 按钮文案 / 是否可提交 */
export function vipSubmitState(s: VipState | null | undefined, authed: boolean, busy: boolean): { label: string; disabled: boolean } {
  if (busy) return { label: "提交中…", disabled: true };
  if (!authed) return { label: "登录后提交", disabled: false };
  if (s?.vip || s?.status === "approved") return { label: "您已是 VIP", disabled: true };
  if (s?.status === "pending") return { label: "审核中（1-3 个工作日）", disabled: true };
  return { label: "提交", disabled: false };
}
