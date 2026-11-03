"use client";
/**
 * 账户模块服务层（注册/登录/2FA/找回密码/设备/地址簿/资料）。
 * 真实接口优先；5xx 或网络错误时回退本地 mock（4xx 视为业务错误直接返回）。
 */
import { useEffect, useState } from "react";
import type { components } from "@linbit/api-client";
import { api, API_BASE } from "./api";
import { useAuthStore } from "./store";
import type { Result } from "./services";

type S = components["schemas"];
export type AccountInfo = S["Account"];
export type AuthTokens = S["AuthTokens"];
export type LoginChallenge = S["LoginChallenge"];
export type DeviceItem = S["Device"] & { current?: boolean }; // current：后端已返回，schema.ts 待重新生成
export type AddressItem = S["WithdrawAddress"];
/** 对应 openapi SendCodeResponse（schema.ts 待 openapi-growth 合入后重新生成） */
type SendCodeRes = { sent: boolean; channel: "email" | "sms"; ttlSeconds?: number };

const delay = (ms = 250) => new Promise((r) => setTimeout(r, ms));
const errMsg = (e: unknown, status: number) =>
  (e as { message?: string } | undefined)?.message ?? `HTTP ${status}`;

async function call<T>(
  fn: () => Promise<{ data?: T; error?: unknown; response: Response }>,
  fallback: () => T | Promise<T>,
): Promise<Result<T>> {
  try {
    const { data, error, response } = await fn();
    if (response.ok) return { ok: true, data: (data ?? (true as unknown)) as T, source: "api" };
    if (response.status < 500) return { ok: false, error: errMsg(error, response.status), source: "api" };
  } catch {
    /* 网络错误 → mock */
  }
  await delay();
  return { ok: true, data: await fallback(), source: "mock" };
}

/* ---------- 工具 ---------- */
export const COUNTRIES: [string, string][] = [
  ["86", "中国大陆"], ["852", "中国香港"], ["853", "中国澳门"], ["886", "中国台湾"],
  ["1", "美国/加拿大"], ["44", "英国"], ["65", "新加坡"], ["81", "日本"], ["82", "韩国"],
  ["61", "澳大利亚"], ["60", "马来西亚"], ["66", "泰国"], ["84", "越南"], ["63", "菲律宾"],
  ["62", "印度尼西亚"], ["91", "印度"], ["971", "阿联酋"], ["49", "德国"], ["33", "法国"],
];
export const toE164 = (cc: string, local: string) => `+${cc}${local.replace(/\D/g, "").replace(/^0+/, "")}`;
export function maskPhone(p?: string | null, cc?: string | null) {
  if (!p) return "";
  const d = p.replace(/\D/g, "");
  const c = cc && d.startsWith(cc) ? cc : "";
  const n = c ? d.slice(c.length) : d;
  const m = n.length > 7 ? `${n.slice(0, 3)}****${n.slice(-4)}` : n.length > 4 ? `${n.slice(0, 2)}***${n.slice(-2)}` : n;
  return c ? `+${c} ${m}` : p.startsWith("+") ? `+${m}` : m;
}
export function maskEmail(e?: string | null) {
  if (!e || !e.includes("@")) return e ?? "";
  const [u, d] = e.split("@");
  return `${u.slice(0, Math.min(3, Math.max(1, u.length - 1)))}***@${d}`;
}
/** 账户展示名：优先邮箱，手机号用户（token 中 email 为空）显示脱敏手机号 */
export function displayAccount(a?: { email?: string | null; phone?: string | null; countryCode?: string | null } | null) {
  if (!a) return "";
  if (a.email) return maskEmail(a.email);
  if (a.phone) return maskPhone(a.phone, a.countryCode);
  return "";
}
/** 解析 JWT 声明（只读展示，不做校验） */
export function jwtClaims(t?: string | null): Record<string, unknown> {
  try {
    if (!t || t.split(".").length !== 3) return {};
    const b = t.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(decodeURIComponent(escape(atob(b))));
  } catch {
    return {};
  }
}
export function deviceId() {
  let id = localStorage.getItem("lb_device_id");
  if (!id) {
    id = "web-" + Math.random().toString(36).slice(2, 10);
    localStorage.setItem("lb_device_id", id);
  }
  return id;
}
export function deviceName() {
  const ua = navigator.userAgent;
  const b = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "Browser";
  const o = /Mac OS/.test(ua) ? "macOS" : /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : "Linux";
  return `${b} on ${o}`;
}

/* ---------- 认证 ---------- */
const MOCK_TOKENS: AuthTokens = { accessToken: "mock-access-token", refreshToken: "mock-refresh-token", tokenType: "Bearer", expiresIn: 3600 };
export type Target = { email?: string; phone?: string; countryCode?: string };

export const sendAuthCode = (purpose: "register" | "reset_password", t: Target) =>
  call(() => api.POST("/auth/code/send", { body: { purpose, ...t } }), () => ({ sent: true, channel: t.email ? "email" : "sms", ttlSeconds: 60 }) as SendCodeRes);

export const register = (b: Target & { password: string; code: string; inviteCode?: string | null }) =>
  call(() => api.POST("/auth/register", { body: { ...b, deviceId: deviceId(), deviceName: deviceName() } as never }), () => MOCK_TOKENS);

export const login = (b: Target & { password: string }) =>
  call<AuthTokens | LoginChallenge>(
    () => api.POST("/auth/login", { body: { email: b.email, phone: b.phone, password: b.password, deviceId: deviceId(), deviceName: deviceName() } }) as never,
    () => MOCK_TOKENS,
  );
export const isChallenge = (x: unknown): x is LoginChallenge => !!x && typeof (x as LoginChallenge).challengeToken === "string";

export const loginVerify = (challengeToken: string, method: string, code: string) =>
  call(() => api.POST("/auth/login/verify", { body: method === "totp" ? { challengeToken, totpCode: code } : { challengeToken, code } }), () => MOCK_TOKENS);
export const loginResend = (challengeToken: string) =>
  call(() => api.POST("/auth/login/code", { body: { challengeToken } }) as never, () => true);

/** 找回密码第一步：校验验证码，换取一次性 resetToken（默认 600 秒有效） */
export const verifyResetCode = (b: Target & { code: string }) =>
  call<{ resetToken: string; expiresIn: number }>(() => (api as any).POST("/auth/password/verify-code", { body: b }), () => ({ resetToken: "mock-reset-token", expiresIn: 600 }));
/** 找回密码第二步：用 resetToken 设置新密码（令牌一次性） */
export const resetPassword = (b: { resetToken: string; newPassword: string }) =>
  call(() => (api as any).POST("/auth/password/reset", { body: b }) as never, () => true);

/* ---------- 账户资料 ---------- */
const isMockToken = () => (useAuthStore.getState().accessToken ?? "").startsWith("mock-");
function mockAccount(): AccountInfo {
  const c = jwtClaims(useAuthStore.getState().accessToken);
  return {
    uid: String(c.sub ?? "80000001"),
    email: (c.email as string) || useAuthStore.getState().email || null,
    phone: (c.phone as string) || null,
    nickname: "lb_Demo8K2",
    avatarUrl: null,
    kycLevel: 0,
    twoFactorEnabled: false,
    createdAt: new Date().toISOString(),
  };
}
export const getAccount = () =>
  isMockToken() ? Promise.resolve<Result<AccountInfo>>({ ok: true, data: mockAccount(), source: "mock" }) : call(() => api.GET("/account"), mockAccount);

let meCache: AccountInfo | null = null;
const subs = new Set<(a: AccountInfo | null) => void>();
export function setMe(a: AccountInfo | null) {
  meCache = a;
  subs.forEach((f) => f(a));
}
/** 当前账户（登录后自动拉取 GET /account） */
export function useMe() {
  const token = useAuthStore((s) => s.accessToken);
  const [me, set] = useState<AccountInfo | null>(meCache);
  useEffect(() => {
    subs.add(set);
    return () => void subs.delete(set);
  }, []);
  useEffect(() => {
    if (!token) return setMe(null);
    getAccount().then((r) => r.ok && r.data && setMe(r.data));
  }, [token]);
  return me;
}
export const updateProfile = (b: { nickname?: string | null; avatarUrl?: string | null }) =>
  call(() => api.PATCH("/account/profile", { body: b }), () => ({ ...(meCache ?? mockAccount()), ...b }) as AccountInfo);

/* ---------- 设备 ---------- */
let mockDevices: DeviceItem[] = [
  { deviceId: "cid:web-mock", name: "Chrome on macOS", ip: "203.0.113.24", firstSeen: "2026-11-01T10:00:00Z", lastSeen: new Date().toISOString() },
  { deviceId: "cid:ios-demo", name: "linbit App · iOS", ip: "198.51.100.7", firstSeen: "2026-10-20T10:00:00Z", lastSeen: "2026-11-01T13:30:00Z" },
];
export const listDevices = () =>
  call(async () => {
    const r = await api.GET("/account/devices");
    return { ...r, data: r.data?.items } as never;
  }, () => mockDevices) as Promise<Result<DeviceItem[]>>;
export const removeDevice = (id: string) =>
  call(() => api.DELETE("/account/devices/{deviceId}", { params: { path: { deviceId: id } } }) as never, () => {
    mockDevices = mockDevices.filter((d) => d.deviceId !== id);
    return true;
  });

/* ---------- 地址簿 / 白名单 ---------- */
let mockAddr: AddressItem[] = [
  { id: "w1", asset: "USDT", network: "TRC20", address: "TDemo0000000000000000000000000001", label: "示例钱包", whitelisted: true, createdAt: "2026-11-01T00:00:00Z" },
];
let mockWl = false;
export const sendAccountCode = (purpose: "address" | "bind" | "reset_password" = "address") =>
  call(() => api.POST("/account/code/send", { body: { purpose } }) as never, () => ({ sent: true, channel: "sms", ttlSeconds: 60 }) as SendCodeRes);
export const listAddresses = () =>
  call(async () => {
    const r = await api.GET("/account/withdraw-addresses");
    return { ...r, data: r.data?.items } as never;
  }, () => mockAddr) as Promise<Result<AddressItem[]>>;
export const addAddress = (b: S["CreateWithdrawAddressRequest"]) =>
  call(() => api.POST("/account/withdraw-addresses", { body: b }), () => {
    const a: AddressItem = { id: `w${Date.now()}`, asset: b.asset, network: b.network, address: b.address, memo: b.memo ?? "", label: b.label ?? "", whitelisted: b.whitelisted ?? true, createdAt: new Date().toISOString() };
    mockAddr = [...mockAddr, a];
    return a;
  });
export const deleteAddress = (id: string, code: { code?: string; totpCode?: string }) =>
  call(() => api.DELETE("/account/withdraw-addresses/{id}", { params: { path: { id }, query: code } }) as never, () => {
    mockAddr = mockAddr.filter((a) => a.id !== id);
    return true;
  });
export const getWhitelist = () => call(() => api.GET("/account/withdraw-whitelist"), () => ({ enabled: mockWl }));
export const setWhitelist = (b: S["SetWhitelistRequest"]) =>
  call(() => api.PUT("/account/withdraw-whitelist", { body: b }) as never, () => {
    mockWl = b.enabled;
    return { enabled: mockWl };
  });

/* ---------- 头像上传（multipart，字段 file；jpeg/png/webp ≤2MB） ---------- */
export async function uploadAvatar(file: File): Promise<Result<AccountInfo>> {
  if (isMockToken()) return { ok: true, data: { ...(meCache ?? mockAccount()), avatarUrl: URL.createObjectURL(file) }, source: "mock" };
  const fd = new FormData();
  fd.append("file", file);
  try {
    const r = await fetch(API_BASE + "/account/avatar", { method: "POST", body: fd, headers: { Authorization: `Bearer ${useAuthStore.getState().accessToken}` } });
    const j = await r.json().catch(() => ({}));
    if (r.ok) return { ok: true, data: j as AccountInfo, source: "api" };
    const msg = r.status === 413 ? "头像超过 2MB 大小限制" : r.status === 415 ? "仅支持 JPEG/PNG/WEBP 图片" : j?.message || `上传失败（HTTP ${r.status}）`;
    return { ok: false, error: msg, source: "api" };
  } catch {
    return { ok: false, error: "网络错误，上传失败", source: "api" };
  }
}
/** avatarUrl 可能是相对路径 /account/avatars/{file}，需拼接 API 基地址 */
export const avatarSrc = (u?: string | null) => (!u ? null : /^(https?:|blob:|data:)/.test(u) ? u : API_BASE + u);
