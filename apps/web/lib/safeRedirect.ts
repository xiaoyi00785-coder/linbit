/** 登录后回跳目标：仅允许同源站内路径。拒绝控制字符与反斜杠（URLSearchParams 会解码 %09/%0A，new URL 会将其剥离，可绕过 "//" 检查）。 */
export function safeRedirectFrom(search: string, origin: string): string {
  const r = new URLSearchParams(search).get("redirect") || "";
  if (!r.startsWith("/") || /[\u0000-\u001f\u007f\\]/.test(r)) return "";
  try {
    const u = new URL(r, origin);
    return u.origin === origin ? u.pathname + u.search + u.hash : "";
  } catch { return ""; }
}
