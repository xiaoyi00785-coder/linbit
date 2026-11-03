"use client";
/**
 * 合伙人站 API：经 /gw 转发到网关（growth 服务 /partner/*）。
 * 主站与合伙人站不同源，无法共享 lb-auth，故本站单独登录（POST /auth/login），令牌存 localStorage「lbp-auth」。
 * source：api = 真实接口；demo = 网关不可达（网络错误 / 5xx）时的演示回退。
 */
import { useCallback, useEffect, useState } from "react";
const BASE = process.env.NEXT_PUBLIC_API_BASE || "/gw";
const KEY = "lbp-auth";
export const getToken = () => (typeof window === "undefined" ? null : localStorage.getItem(KEY));
export const setToken = (t: string | null) => { t ? localStorage.setItem(KEY, t) : localStorage.removeItem(KEY); window.dispatchEvent(new Event("lbp-auth")); };
export type Res<T> = { ok: boolean; data?: T; error?: string; code?: string; status?: number; source: "api" | "demo" };

export async function call<T>(method: string, path: string, body?: unknown): Promise<Res<T>> {
  const tok = getToken();
  try {
    const r = await fetch(BASE + path, { method, headers: { ...(body ? { "Content-Type": "application/json" } : {}), ...(tok ? { Authorization: `Bearer ${tok}` } : {}) }, body: body ? JSON.stringify(body) : undefined });
    const t = await r.text(); const j = t ? JSON.parse(t) : {};
    if (r.ok) return { ok: true, data: j, status: r.status, source: "api" };
    if (r.status === 401) setToken(null);
    if (r.status < 500) return { ok: false, error: j.message || `HTTP ${r.status}`, code: j.code, status: r.status, source: "api" };
  } catch { /* 不可达 */ }
  return { ok: false, error: "服务暂不可用", source: "demo" };
}

export function useApi<T>(path: string | null, demo?: () => T) {
  const [s, set] = useState<{ loading: boolean; r?: Res<T> }>({ loading: !!path });
  const load = useCallback(async () => {
    if (!path) return;
    set((x) => ({ ...x, loading: true }));
    const r = await call<T>("GET", path);
    set({ loading: false, r: r.source === "demo" && demo ? { ...r, ok: true, data: demo() } : r });
  }, [path]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { load(); }, [load]);
  return { loading: s.loading, data: s.r?.ok ? s.r.data : undefined, err: s.r && !s.r.ok ? s.r : undefined, demo: s.r?.source === "demo", reload: load };
}

export function useToken() {
  const [t, setT] = useState<string | null | undefined>(undefined);
  useEffect(() => { const f = () => setT(getToken()); f(); window.addEventListener("lbp-auth", f); return () => window.removeEventListener("lbp-auth", f); }, []);
  return t;
}

export function toast(msg: string, kind: "ok" | "err" | "info" = "ok") {
  let box = document.getElementById("lbp-toasts");
  if (!box) { box = document.createElement("div"); box.id = "lbp-toasts"; document.body.appendChild(box); }
  const el = document.createElement("div"); el.className = "toast " + kind; el.textContent = msg; box.appendChild(el);
  setTimeout(() => { el.classList.add("out"); setTimeout(() => el.remove(), 300); }, 2400);
}
export async function copy(text: string) {
  try { await navigator.clipboard.writeText(text); toast("已复制"); } catch { toast("复制失败，请手动复制", "err"); }
}

/* ---- 接口类型（services/go/growth/partner.go） ---- */
export type ApiTier = { key: string; name: string; minVolume30d: string; futuresRate: string; spotRate: string };
export type Rules = { tiers: ApiTier[]; tierBasis: string; overrideRate: string; overrideLevels: number; /** 上级可拿额外奖励的层数（不含直推层），后端默认 2 */ overrideDepth?: number; overrideFunding: string; settlement: string; settleHourUtc: number; payoutAsset: string; validity: string; rebateBase: string; autoApprove: boolean };
export type Me = { uid: string; status: string; partner?: { status: string; reviewNote: string; appliedAt: number }; tier?: string; tierName?: string; futuresRate?: string; spotRate?: string };
export type Summary = { tier: string; tierName: string; futuresRate: string; spotRate: string; volume30d: string; nextTier?: { tier: string; name: string; minVolume30d: string; gap: string } | null; directs: number; directPartners: number; unsettledEstimate: string; yesterday: string; month: string; total: string; overrideTotal: string; pendingPayout: string; payoutAsset: string };
export type Daily = { items: { day: string; futures: string; spot: string; override: string; referral: string; total: string }[]; days: number };
export type Comm = { items: { settlementId: number; sourceUid: string; kind: string; tier: string; rate: string; base: string; amount: string; day: string; settledAt: number; asset: string }[]; total: number; page: number; pageSize: number };
export type Team = { items: { uid: string; joinedAt: number; kycLevel: number; isPartner: boolean; tier: string; futuresVolume30d: string; spotVolume30d: string; fee30d: string; commission: string }[] };
export type Links = { code: string; url: string; registerUrl: string };
