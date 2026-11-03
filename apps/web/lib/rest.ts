/**
 * 未进入 openapi.yaml 的真实接口（growth / futures copy / notifications）通用调用。
 * 返回 source 标明数据来源：api = 真实网关；demo = 网关不可达（网络错误 / 5xx）时的演示回退。
 * 4xx 视为业务错误原样返回，不回退演示数据。
 */
import { API_BASE } from "./api";
import { useAuthStore } from "./store";
import { useEffect, useState, useCallback } from "react";

export interface RestResult<T> { ok: boolean; data?: T; error?: string; code?: string; status?: number; source: "api" | "demo" }

export async function rest<T>(method: string, path: string, body?: unknown, demo?: () => T): Promise<RestResult<T>> {
  const tok = useAuthStore.getState().accessToken;
  try {
    const r = await fetch(API_BASE + path, {
      method,
      headers: { ...(body !== undefined ? { "Content-Type": "application/json" } : {}), ...(tok ? { Authorization: `Bearer ${tok}` } : {}) },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    const txt = await r.text();
    const j = txt ? JSON.parse(txt) : {};
    if (r.ok) return { ok: true, data: j as T, status: r.status, source: "api" };
    // 4xx 或带业务 code 的 5xx（如 503 PRICE_UNAVAILABLE）原样返回，不回退演示数据
    if (r.status < 500 || j?.code) return { ok: false, error: j?.message || j?.error || `HTTP ${r.status}`, code: j?.code, status: r.status, source: "api" };
  } catch { /* 网络错误 → demo */ }
  return demo ? { ok: true, data: demo(), source: "demo" } : { ok: false, error: "服务暂不可用", source: "demo" };
}
export const rget = <T,>(p: string, demo?: () => T) => rest<T>("GET", p, undefined, demo);

/** React hook：加载 + 刷新；loading 用于骨架屏 */
export function useRest<T>(path: string | null, demo?: () => T) {
  const [st, set] = useState<{ loading: boolean; res?: RestResult<T> }>({ loading: !!path });
  const load = useCallback(async () => {
    if (!path) return;
    set((s) => ({ ...s, loading: true }));
    const res = await rget<T>(path, demo);
    set({ loading: false, res });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);
  useEffect(() => { load(); }, [load]);
  return { loading: st.loading, data: st.res?.data, source: st.res?.source, error: st.res?.ok === false ? st.res.error : undefined, status: st.res?.status, reload: load };
}
