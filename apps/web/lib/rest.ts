/**
 * 未进入 openapi.yaml 的真实接口（growth / futures copy / notifications）通用调用。
 * source：api = 网关有响应；network = 网关不可达（网络错误 / 无业务码 5xx）。
 * 主网规则：只用真实接口，任何错误都返回 ok:false，由界面显示错误或空状态，绝不使用本地伪造数据。
 */
import { API_BASE } from "./api";
import { useAuthStore } from "./store";
import { useEffect, useState, useCallback } from "react";

export interface RestResult<T> { ok: boolean; data?: T; error?: string; code?: string; status?: number; source: "api" | "network" }

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

export async function rest<T>(method: string, path: string, body?: unknown, retried = false): Promise<RestResult<T>> {
  const tok = useAuthStore.getState().accessToken;
  try {
    const r = await fetch(API_BASE + path, {
      method,
      headers: { ...(body !== undefined ? { "Content-Type": "application/json" } : {}), ...(tok ? { Authorization: `Bearer ${tok}` } : {}) },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    if (r.status === 401 && tok && !retried && !path.startsWith("/auth/")) {
      if (await refreshAccess()) return rest<T>(method, path, body, true);
    }
    const txt = await r.text();
    let j: any = {}; try { j = txt ? JSON.parse(txt) : {}; } catch { j = {}; }
    if (r.ok) return { ok: true, data: j as T, status: r.status, source: "api" };
    // 4xx 或带业务 code 的 5xx（如 503 PRICE_UNAVAILABLE）原样返回；写操作的 5xx 带后端 message 时原样返回
    if (r.status < 500 || j?.code || (method !== "GET" && (j?.message || j?.error))) return { ok: false, error: j?.message || j?.error || `HTTP ${r.status}`, code: j?.code, status: r.status, source: "api" };
  } catch { /* 网络错误 */ }
  return { ok: false, error: "服务暂不可用", source: "network" };
}
// GET 是幂等的：网关瞬时不可达（网络错误 / 无业务码 5xx / UPSTREAM_UNAVAILABLE）时稍等重试，最多 2 次，
// 避免下单后立即刷新仓位等场景因上游短暂抖动显示"接口不可达"。
const transient = <T,>(r: RestResult<T>) => r.source === "network" || r.code === "UPSTREAM_UNAVAILABLE";
export async function rget<T>(p: string): Promise<RestResult<T>> {
  let r = await rest<T>("GET", p);
  for (let i = 0; i < 2 && transient(r); i++) {
    await new Promise((ok) => setTimeout(ok, 400 * (i + 1)));
    r = await rest<T>("GET", p);
  }
  return r;
}

/** 余额变化全局事件（划转成功等） */
export const BALANCES_EVT = "lb:balances";
/** 受余额变化影响的接口（精确路径，忽略 query） */
const BAL_PATHS = new Set(["/assets/overview", "/assets/balances", "/assets/transfers", "/futures/account"]);
export const emitBalances = () => { if (typeof window !== "undefined") window.dispatchEvent(new Event(BALANCES_EVT)); };

/** React hook：加载 + 刷新；loading 用于骨架屏 */
export function useRest<T>(path: string | null) {
  const [st, set] = useState<{ loading: boolean; res?: RestResult<T> }>({ loading: !!path });
  /** silent=true：后台刷新（轮询/下单后），保留现有数据，不切回骨架屏 */
  const load = useCallback(async (silent?: unknown) => {
    if (!path) return;
    if (silent !== true) set((s) => ({ ...s, loading: true }));
    const res = await rget<T>(path);
    // 刷新失败（后台刷新出错 / 网关瞬时不可达）时保留上一次真实数据
    set((s) => (s.res?.ok && s.res.source === "api" && ((silent === true && !res.ok) || res.source === "network") ? { loading: false, res: s.res } : { loading: false, res }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);
  useEffect(() => { load(); }, [load]);
  // 划转/下单等改变余额后广播 BALANCES_EVT：所有余额类接口实例静默重取
  useEffect(() => {
    if (!path || !BAL_PATHS.has(path.split("?")[0])) return;
    const f = () => load(true);
    window.addEventListener(BALANCES_EVT, f);
    return () => window.removeEventListener(BALANCES_EVT, f);
  }, [load, path]);
  return { loading: st.loading, data: st.res?.data, source: st.res?.source, error: st.res?.ok === false ? st.res.error : undefined, status: st.res?.status, reload: load, refresh: () => load(true) };
}
