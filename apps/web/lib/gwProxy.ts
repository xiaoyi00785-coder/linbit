/**
 * 只在服务端使用的 /gw/* 反代，替代 next.config 里的 rewrite。
 * apps/web 和 apps/partner 各有一份，内容必须逐字相同（scripts/check-gw-proxy.mjs 会比对）。
 *
 * 目标地址：GATEWAY_INTERNAL_URL（例如 http://gateway:8080，docker 网络，不经 Cloudflare），
 * 没有则用 API_PROXY_TARGET（旧的公网地址）。这两个都不是 NEXT_PUBLIC。
 *
 * 客户端身份：浏览器自带的 X-Forwarded-For / X-Real-IP / Forwarded / CF-Connecting-IP
 * 以及边缘鉴权头一律丢掉。客户端 IP 优先取 CF-Connecting-IP（Cloudflare 会覆盖；源站只收 Cloudflare，
 * web 在 nginx 后面监听 loopback），否则用 nginx 写入的 X-Real-IP。
 * 转给网关时带上 X-Real-IP / X-Forwarded-For 和 CF-IPCountry；只有内网这一跳才带
 * X-Linbit-Edge-Auth = GATEWAY_EDGE_SECRET。网关只在密钥匹配，或 TCP 对端落在 TRUSTED_PROXY_CIDRS 时信任这些头。
 *
 * 失败：超时 504 GATEWAY_TIMEOUT，连接或其他错误 502 UPSTREAM_UNAVAILABLE（JSON）。
 */
export const EDGE_AUTH_HEADER = "x-linbit-edge-auth";

const HOP = new Set(["connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade", "host", "content-length"]);
const CLIENT_IDENTITY = new Set(["x-forwarded-for", "x-forwarded-host", "x-forwarded-proto", "x-forwarded-port", "x-real-ip", "forwarded", "cf-connecting-ip", "true-client-ip", EDGE_AUTH_HEADER]);
// fetch() 已经解开了 body，上游的 content-encoding 和 content-length 不再适用。
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
