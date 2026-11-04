/** 客服联系渠道：仅读取配置，未配置的渠道不展示（不写死任何联系方式）。
 *  NEXT_PUBLIC_SUPPORT_URL      在线客服/工单系统链接（https://）
 *  NEXT_PUBLIC_SUPPORT_TELEGRAM 官方客服 Telegram（https://t.me/...）
 *  NEXT_PUBLIC_SUPPORT_EMAIL    客服邮箱
 *  后端目前没有 1 对 1 客服 / 工单接口；/chat 是公开的交易者聊天室，不是客服。 */
export type SupportChannel = { key: string; label: string; href: string; hint: string };
const url = (u?: string) => (u && /^https:\/\//.test(u) ? u : null);
export function supportChannels(): SupportChannel[] {
  const out: SupportChannel[] = [];
  const web = url(process.env.NEXT_PUBLIC_SUPPORT_URL);
  if (web) out.push({ key: "web", label: "在线客服", href: web, hint: "打开在线客服" });
  const tg = url(process.env.NEXT_PUBLIC_SUPPORT_TELEGRAM);
  if (tg) out.push({ key: "telegram", label: "Telegram 客服", href: tg, hint: tg.replace(/^https:\/\//, "") });
  const em = process.env.NEXT_PUBLIC_SUPPORT_EMAIL;
  if (em && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(em)) out.push({ key: "email", label: "邮件客服", href: "mailto:" + em, hint: em });
  return out;
}
/** 其他页面（如 VIP「联系客服」）打开客服面板 */
export const openSupport = () => { if (typeof window !== "undefined") window.dispatchEvent(new Event("lb-support-open")); };
