/**
 * 未进入 openapi.yaml 的真实接口（growth / futures copy / notifications）通用调用。
 * 返回 source 标明数据来源：api = 真实网关；demo = 网关不可达（网络错误 / 5xx）时的演示回退。
 * 4xx 视为业务错误原样返回，不回退演示数据。
 */
import { API_BASE } from "./api";
import { useAuthStore } from "./store";
import { useEffect, useState, useCallback } from "react";
import { MOCK_ENABLED } from "./mockFlag";

export interface RestResult<T> { ok: boolean; data?: T; error?: string; code?: string; status?: number; source: "api" | "demo" }

// 访问令牌过期 / 被吊销时用 refreshToken 换新令牌（并发请求共用一次刷新）；刷新失败则本地退出，
// 页面显示未登录，而不是把 401 静默显示成 0 余额。
let refreshing: Promise<boolean> | null = null;
export function refreshAccess(): Promise<boolean> {
  if (refreshing) return refreshing;
  const { refreshToken, email } = useAuthStore.getState();
  if (!refreshToken) return Promise.resolve(false);
  refreshing = (async () => {
    try {
      const r = await fetch(API_BASE + "/auth/refresh", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ refreshToken }) });
      if (!r.ok) { if (r.status === 400 || r.status === 401) useAuthStore.getState().logout(); return false; }
      const j = await r.json();
      if (!j?.accessToken) return false;
      useAuthStore.getState().login({ accessToken: j.accessToken, refreshToken: j.refreshToken || refreshToken, email });
      return true;
    } catch { return false; } finally { setTimeout(() => { refreshing = null; }, 0); }
  })();
  return refreshing;
}

export async function rest<T>(method: string, path: string, body?: unknown, demo?: () => T, retried = false): Promise<RestResult<T>> {
  const tok = useAuthStore.getState().accessToken;
  try {
    const r = await fetch(API_BASE + path, {
      method,
      headers: { ...(body !== undefined ? { "Content-Type": "application/json" } : {}), ...(tok ? { Authorization: `Bearer ${tok}` } : {}) },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    if (r.status === 401 && tok && !retried && !path.startsWith("/auth/")) {
      if (await refreshAccess()) return rest<T>(method, path, body, demo, true);
    }
    const txt = await r.text();
    const j = txt ? JSON.parse(txt) : {};
    if (r.ok) return { ok: true, data: j as T, status: r.status, source: "api" };
    // 4xx 或带业务 code 的 5xx（如 503 PRICE_UNAVAILABLE）原样返回，不回退演示数据
    if (r.status < 500 || j?.code) return { ok: false, error: j?.message || j?.error || `HTTP ${r.status}`, code: j?.code, status: r.status, source: "api" };
  } catch { /* 网络错误 → demo */ }
  return demo && MOCK_ENABLED ? { ok: true, data: demo(), source: "demo" } : { ok: false, error: "服务暂不可用", source: "demo" };
}
// GET 是幂等的：网关瞬时不可达（网络错误 / 无业务码 5xx / UPSTREAM_UNAVAILABLE）时稍等重试，最多 2 次，
// 避免下单后立即刷新仓位等场景因上游短暂抖动显示"接口不可达"。
const transient = <T,>(r: RestResult<T>) => r.source === "demo" || r.code === "UPSTREAM_UNAVAILABLE";
export async function rget<T>(p: string, demo?: () => T): Promise<RestResult<T>> {
  let r = await rest<T>("GET", p, undefined, demo);
  for (let i = 0; i < 2 && transient(r); i++) {
    await new Promise((ok) => setTimeout(ok, 400 * (i + 1)));
    r = await rest<T>("GET", p, undefined, demo);
  }
  return r;
}

/** React hook：加载 + 刷新；loading 用于骨架屏 */
export function useRest<T>(path: string | null, demo?: () => T) {
  const [st, set] = useState<{ loading: boolean; res?: RestResult<T> }>({ loading: !!path });
  /** silent=true：后台刷新（轮询/下单后），保留现有数据，不切回骨架屏 */
  const load = useCallback(async (silent?: unknown) => {
    if (!path) return;
    if (silent !== true) set((s) => ({ ...s, loading: true }));
    const res = await rget<T>(path, demo);
    // 刷新失败（后台刷新出错 / 瞬时不可达回退演示）时保留上一次真实数据，不切成空/演示数据
    set((s) => (s.res?.ok && s.res.source === "api" && ((silent === true && !res.ok) || res.source === "demo") ? { loading: false, res: s.res } : { loading: false, res }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);
  useEffect(() => { load(); }, [load]);
  return { loading: st.loading, data: st.res?.data, source: st.res?.source, error: st.res?.ok === false ? st.res.error : undefined, status: st.res?.status, reload: load, refresh: () => load(true) };
}
