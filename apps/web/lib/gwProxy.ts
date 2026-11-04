/**
 * Server-only /gw/* proxy to the API gateway (replaces the next.config rewrite).
 * Shared verbatim by apps/web and apps/partner (scripts/check-gw-proxy.mjs asserts both copies match).
 *
 * Target: GATEWAY_INTERNAL_URL (e.g. http://gateway:8080, docker network, no Cloudflare hop),
 * falling back to API_PROXY_TARGET (legacy public URL). Neither is NEXT_PUBLIC.
 *
 * Client identity: the browser's own X-Forwarded-For / X-Real-IP / Forwarded / CF-Connecting-IP and
 * any edge-auth header are dropped. The client IP is taken from CF-Connecting-IP (overwritten by
 * Cloudflare; the origin only accepts Cloudflare, web listens on loopback behind nginx), else
 * X-Real-IP set by nginx. It is sent as X-Real-IP / X-Forwarded-For together with CF-IPCountry and,
 * only on the internal hop, X-Linbit-Edge-Auth = GATEWAY_EDGE_SECRET. The gateway trusts those
 * headers only when that secret matches (or the TCP peer is in TRUSTED_PROXY_CIDRS).
 *
 * Failures: timeout → 504 GATEWAY_TIMEOUT, connect/other errors → 502 UPSTREAM_UNAVAILABLE (JSON).
 */
export const EDGE_AUTH_HEADER = "x-linbit-edge-auth";

const HOP = new Set(["connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade", "host", "content-length"]);
const CLIENT_IDENTITY = new Set(["x-forwarded-for", "x-forwarded-host", "x-forwarded-proto", "x-forwarded-port", "x-real-ip", "forwarded", "cf-connecting-ip", "true-client-ip", EDGE_AUTH_HEADER]);
// fetch() already decoded the body, so the upstream encoding/length no longer apply.
const RESP_DROP = new Set([...HOP, "content-encoding"]);

export type GwEnv = { GATEWAY_INTERNAL_URL?: string; API_PROXY_TARGET?: string; GATEWAY_EDGE_SECRET?: string; GATEWAY_PROXY_TIMEOUT_MS?: string; NODE_ENV?: string };

export function gwTarget(env: GwEnv): { base: string; internal: boolean } | null {
  const i = (env.GATEWAY_INTERNAL_URL || "").trim();
  if (i) return { base: i.replace(/\/+$/, ""), internal: true };
  const p = (env.API_PROXY_TARGET || "").trim();
  return p ? { base: p.replace(/\/+$/, ""), internal: false } : null;
}

const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
const IPV6 = /^[0-9a-fA-F:.]{2,45}$/;
export const validIP = (s: string) => IPV4.test(s) || (s.includes(":") && IPV6.test(s));

/** Real client IP from the edge headers; "" when unknown (local dev). */
export function edgeClientIP(h: Headers): string {
  for (const k of ["cf-connecting-ip", "x-real-ip"]) {
    const v = (h.get(k) || "").trim();
    if (v && validIP(v)) return v;
  }
  return "";
}

/** Headers sent to the gateway. */
export function upstreamHeaders(inc: Headers, env: GwEnv, internal: boolean): Headers {
  const out = new Headers();
  inc.forEach((v, k) => { const l = k.toLowerCase(); if (!HOP.has(l) && !CLIENT_IDENTITY.has(l)) out.set(k, v); });
  const ip = edgeClientIP(inc);
  if (ip) { out.set("x-real-ip", ip); out.set("x-forwarded-for", ip); }
  const secret = (env.GATEWAY_EDGE_SECRET || "").trim();
  if (internal && secret) out.set(EDGE_AUTH_HEADER, secret);
  return out;
}

const jsonErr = (status: number, code: string, message: string) =>
  new Response(JSON.stringify({ code, message }), { status, headers: { "content-type": "application/json; charset=utf-8", "cache-control": "no-store" } });

export async function proxyGw(req: Request, path: string[], env: GwEnv = process.env as GwEnv, fetchImpl: typeof fetch = fetch): Promise<Response> {
  const t = gwTarget(env);
  if (!t) return jsonErr(502, "UPSTREAM_UNAVAILABLE", "网关未配置");
  // 直连网关时必须带 edge secret，否则网关不信任转发的 IP/国家头（地区限制会失效）：生产环境直接拒绝，避免静默放行
  if (t.internal && (env.GATEWAY_EDGE_SECRET || "").trim().length < 32 && env.NODE_ENV === "production") {
    console.error("gw proxy: GATEWAY_INTERNAL_URL set but GATEWAY_EDGE_SECRET missing/short (<32); refusing to proxy");
    return jsonErr(502, "UPSTREAM_UNAVAILABLE", "服务暂时不可用，请稍后重试");
  }
  const src = new URL(req.url);
  const url = t.base + "/" + path.map(encodeURIComponent).join("/") + src.search;
  const ms = Math.max(1000, Number(env.GATEWAY_PROXY_TIMEOUT_MS) || 15000);
  const hasBody = req.method !== "GET" && req.method !== "HEAD";
  let r: Response;
  try {
    r = await fetchImpl(url, {
      method: req.method,
      headers: upstreamHeaders(req.headers, env, t.internal),
      body: hasBody ? await req.arrayBuffer() : undefined,
      redirect: "manual",
      cache: "no-store",
      signal: AbortSignal.timeout(ms),
    });
  } catch (e) {
    const name = (e as { name?: string })?.name;
    const timeout = name === "TimeoutError" || name === "AbortError";
    console.error(`gw proxy ${req.method} /${path.join("/")} -> ${t.internal ? "internal" : "public"}: ${timeout ? "timeout " + ms + "ms" : String((e as { cause?: unknown })?.cause ?? e)}`);
    return timeout
      ? jsonErr(504, "GATEWAY_TIMEOUT", "服务响应超时，请稍后重试；如为下单操作，请先在委托记录中确认结果")
      : jsonErr(502, "UPSTREAM_UNAVAILABLE", "服务暂时不可用，请稍后重试");
  }
  const h = new Headers();
  r.headers.forEach((v, k) => { const l = k.toLowerCase(); if (!RESP_DROP.has(l) && l !== "set-cookie") h.set(k, v); });
  const sc = (r.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie?.() ?? [];
  for (const c of sc) h.append("set-cookie", c);
  return new Response(req.method === "HEAD" ? null : r.body, { status: r.status, statusText: r.statusText, headers: h });
}
