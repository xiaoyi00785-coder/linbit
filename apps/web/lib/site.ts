/** 站点地址与邀请信息：一律使用真实域名（NEXT_PUBLIC_SITE_URL，未配置时取当前 origin），不再出现示例域名 */
export const siteUrl = () =>
  (process.env.NEXT_PUBLIC_SITE_URL || (typeof window !== "undefined" ? window.location.origin : "")).replace(/\/$/, "");
/** 邀请码来自 /account（inviteCode 字段，nullable；后端 fix/acceptance-r2 提供）；没有时返回 null，界面显示 "--" 且不可复制 */
export const inviteCodeOf = (me?: unknown) => ((me as { inviteCode?: string | null } | null | undefined)?.inviteCode || null);
/** 邀请链接：{inviteBaseUrl}/{inviteCode}。inviteBaseUrl 优先取 GET /growth/config 返回值，
 *  其次 NEXT_PUBLIC_INVITE_BASE_URL，默认主网 https://www.cowa.click/r（/r/{code} 路由由 web 端另行提供）。 */
export const INVITE_BASE_DEFAULT = "https://www.cowa.click/r";
export const inviteLink = (code?: string | null, base?: string | null) =>
  code ? `${(base || process.env.NEXT_PUBLIC_INVITE_BASE_URL || INVITE_BASE_DEFAULT).replace(/\/$/, "")}/${encodeURIComponent(code)}` : "";
