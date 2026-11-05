// 合伙人站点地址。纯 JS（无依赖），供 next.config.mjs（构建期 redirects）、页面（lib/partnerUrl.ts）与
// scripts/check-partner-url.mjs 共用，保证重定向与页面链接指向同一个地址。
// 环境变量 NEXT_PUBLIC_PARTNER_URL（构建期内联，见 deploy/next.Dockerfile）；未设置或非法时用主网默认值，
// 与 NEXT_PUBLIC_INVITE_BASE_URL 的处理方式一致，所以主网部署不必新增变量。
export const PARTNER_URL_DEFAULT = "https://partner.cowa.click";

/** 只接受 https://host[/path]，或本机调试用的 http://localhost / 127.0.0.1；其余（含 javascript:、相对路径、空串）回退默认值。 */
export function partnerBase(raw) {
  const v = String(raw ?? "").trim().replace(/\/+$/, "");
  const ok = /^https:\/\/[^\s/?#]+([/][^\s?#]*)?$/i.test(v) || /^http:\/\/(localhost|127\.0\.0\.1)(:\d+)?([/][^\s?#]*)?$/i.test(v);
  return ok ? v : PARTNER_URL_DEFAULT;
}

/** 合伙人站点页面：partnerUrl(base) → 首页 `${base}/`；partnerUrl(base, "/apply") → `${base}/apply` */
export function partnerUrl(base, path = "/") {
  return partnerBase(base) + (path.startsWith("/") ? path : "/" + path);
}
