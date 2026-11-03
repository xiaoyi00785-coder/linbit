/** 站点地址与邀请信息：一律使用真实域名（NEXT_PUBLIC_SITE_URL，未配置时取当前 origin），不再出现示例域名 */
export const siteUrl = () =>
  (process.env.NEXT_PUBLIC_SITE_URL || (typeof window !== "undefined" ? window.location.origin : "")).replace(/\/$/, "");
/** 邀请码来自 /account（inviteCode 字段，后端待补充）；没有时返回 null，界面显示 "--" 且不可复制 */
export const inviteCodeOf = (me?: unknown) => ((me as { inviteCode?: string | null } | null | undefined)?.inviteCode || null);
export const inviteLink = (code?: string | null) => (code ? `${siteUrl()}/register?inviteCode=${encodeURIComponent(code)}` : "");
