/** 安全中心：对接 feat/security-settings 的 /account/security* 接口 */
import { rest, rget, type RestResult } from "./rest";
import { useCallback, useEffect, useState } from "react";

export interface SecurityOverview {
  twoFactor?: { enabled: boolean; method?: "totp" | null };
  email?: string | null; emailVerified?: boolean;
  phone?: string | null; phoneVerified?: boolean;
  fundPasswordSet?: boolean; fundPasswordLockedUntil?: string | null;
  antiPhishingSet?: boolean; antiPhishingHint?: string | null;
  passkeysCount?: number;
  withdrawLockedUntil?: string | null; withdrawLockReason?: string | null;
  recentDevices?: { deviceId: string; name?: string; ip?: string; lastSeen?: string }[];
  policy?: { fundPasswordForWithdraw?: boolean; fundPasswordForInternalTransfer?: boolean; fundPasswordMandatory?: boolean; withdrawLockHours?: number };
}
export interface SessionItem { id: string; deviceId?: string; name?: string; ip?: string; userAgent?: string; current?: boolean; createdAt?: string; expiresAt?: string }
export interface PasskeyItem { id: string; name?: string; createdAt?: string; lastUsedAt?: string | null }
/** 二次验证：已开 2FA 传 totpCode，否则传 code(purpose=security) */
export type Verify = { totpCode?: string; code?: string };

export function useSecurity() {
  const [s, set] = useState<{ loading: boolean; data?: SecurityOverview; res?: RestResult<SecurityOverview> }>({ loading: true });
  const load = useCallback(async () => { const res = await rget<SecurityOverview>("/account/security"); set({ loading: false, data: res.ok ? res.data : undefined, res }); }, []);
  useEffect(() => { load(); const h = () => load(); window.addEventListener("lb:security", h); return () => window.removeEventListener("lb:security", h); }, [load]);
  return { ...s, reload: load };
}
export const secChanged = () => window.dispatchEvent(new Event("lb:security"));

export const changePassword = (b: { oldPassword: string; newPassword: string } & Verify) =>
  rest<{ ok: boolean; revokedSessions?: number }>("POST", "/account/password/change", { ...b, revokeOtherSessions: true });
export const sendContactCode = (b: { email?: string; phone?: string; countryCode?: string }) =>
  rest<{ sent: boolean; channel: string; ttlSeconds: number }>("POST", "/account/contact/code", b);
export const changeContact = (kind: "email" | "phone", b: { email?: string; phone?: string; countryCode?: string; newCode: string; oldCode: string; totpCode?: string }) =>
  rest<{ ok: boolean }>("PUT", `/account/${kind}`, b);
export const fundPassword = (mode: "set" | "change" | "reset", b: { fundPassword: string; oldFundPassword?: string } & Verify) =>
  rest<{ ok: boolean }>(mode === "change" ? "PUT" : "POST", mode === "reset" ? "/account/fund-password/reset" : "/account/fund-password", b);
export const setAntiPhishing = (b: { antiPhishingCode: string } & Verify) => rest<{ ok: boolean }>("PUT", "/account/anti-phishing", b);
export const listSessions = () => rget<{ items: SessionItem[] }>("/account/sessions");
export const revokeSession = (id: string) => rest<unknown>("DELETE", `/account/sessions/${encodeURIComponent(id)}`);
export const revokeOthers = () => rest<{ revoked: number }>("POST", "/account/sessions/revoke-others");
export const listPasskeys = () => rget<{ enabled: boolean; items: PasskeyItem[] }>("/account/passkeys");
export const deletePasskey = (id: string) => rest<unknown>("DELETE", `/account/passkeys/${encodeURIComponent(id)}`);

/* ---------- WebAuthn（二进制字段 base64url ↔ ArrayBuffer） ---------- */
const b64uToBuf = (s: string) => { const b = atob(s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4)); return Uint8Array.from(b, (c) => c.charCodeAt(0)).buffer; };
const bufToB64u = (b: ArrayBuffer) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
type J = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any
const unwrap = (pk: J): J => (pk && pk.publicKey ? pk.publicKey : pk); // 兼容 {publicKey:{...}} 包装
function credToJSON(c: PublicKeyCredential): J {
  const anyc = c as unknown as { toJSON?: () => J };
  if (typeof anyc.toJSON === "function") { try { return anyc.toJSON(); } catch { /* 部分浏览器未实现 */ } }
  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse;
  const resp: J = { clientDataJSON: bufToB64u(r.clientDataJSON) };
  if ("attestationObject" in r && r.attestationObject) { resp.attestationObject = bufToB64u(r.attestationObject); resp.transports = r.getTransports?.() ?? []; }
  if ("authenticatorData" in r && r.authenticatorData) { resp.authenticatorData = bufToB64u(r.authenticatorData); resp.signature = bufToB64u(r.signature); if (r.userHandle) resp.userHandle = bufToB64u(r.userHandle); }
  return { id: c.id, rawId: bufToB64u(c.rawId), type: c.type, response: resp, clientExtensionResults: c.getClientExtensionResults?.() ?? {}, authenticatorAttachment: c.authenticatorAttachment ?? undefined };
}
export const webauthnSupported = () => typeof window !== "undefined" && !!window.PublicKeyCredential && !!navigator.credentials;

export async function createPasskey(verify: Verify, name: string): Promise<RestResult<PasskeyItem>> {
  const b = await rest<{ sessionId: string; publicKey: J }>("POST", "/account/passkeys/register/begin", verify);
  if (!b.ok || !b.data) return b as RestResult<never>;
  const o = unwrap(b.data.publicKey);
  const opts: PublicKeyCredentialCreationOptions = {
    ...o, challenge: b64uToBuf(o.challenge), user: { ...o.user, id: b64uToBuf(o.user.id) },
    excludeCredentials: (o.excludeCredentials ?? []).map((c: J) => ({ ...c, id: b64uToBuf(c.id) })),
  } as PublicKeyCredentialCreationOptions;
  let cred: PublicKeyCredential | null;
  try { cred = (await navigator.credentials.create({ publicKey: opts })) as PublicKeyCredential | null; }
  catch (e) { return { ok: false, error: (e as Error)?.name === "NotAllowedError" ? "已取消或超时" : "设备不支持或创建失败", source: "api" }; }
  if (!cred) return { ok: false, error: "已取消", source: "api" };
  return rest<PasskeyItem>("POST", "/account/passkeys/register/finish", { sessionId: b.data.sessionId, name, credential: credToJSON(cred) });
}
export async function passkeyLogin(device: { deviceId: string; deviceName: string }) {
  const b = await rest<{ sessionId: string; publicKey: J }>("POST", "/auth/passkey/login/begin", {});
  if (!b.ok || !b.data) return b as RestResult<never>;
  const o = unwrap(b.data.publicKey);
  const opts = { ...o, challenge: b64uToBuf(o.challenge), allowCredentials: (o.allowCredentials ?? []).map((c: J) => ({ ...c, id: b64uToBuf(c.id) })) } as PublicKeyCredentialRequestOptions;
  let cred: PublicKeyCredential | null;
  try { cred = (await navigator.credentials.get({ publicKey: opts })) as PublicKeyCredential | null; }
  catch (e) { return { ok: false, error: (e as Error)?.name === "NotAllowedError" ? "已取消或超时" : "通行密钥验证失败", source: "api" } as RestResult<never>; }
  if (!cred) return { ok: false, error: "已取消", source: "api" } as RestResult<never>;
  return rest<{ accessToken: string; refreshToken: string; expiresIn?: number }>("POST", "/auth/passkey/login/finish", { sessionId: b.data.sessionId, credential: credToJSON(cred), ...device });
}

/** 通用安全错误码 → 中文 */
export const SEC_ERR: Record<string, string> = {
  OLD_PASSWORD_INVALID: "原登录密码错误", PASSWORD_UNCHANGED: "新密码不能与原密码相同", AUTH_CODE_INVALID: "验证码错误或已过期",
  EMAIL_EXISTS: "该邮箱已被其他账户使用", PHONE_EXISTS: "该手机号已被其他账户使用", SMS_UNAVAILABLE: "短信服务暂不可用，请稍后再试",
  CONTACT_UNCHANGED: "与当前绑定的联系方式相同", FUND_PASSWORD_EXISTS: "资金密码已设置", FUND_PASSWORD_INVALID: "资金密码错误",
  FUND_PASSWORD_NOT_SET: "请先设置资金密码", FUND_PASSWORD_REQUIRED: "请输入资金密码", PASSKEY_UNAVAILABLE: "通行密钥服务暂未启用",
  WITHDRAW_LOCKED: "安全设置变更后 24 小时内禁止提现",
};
export const fmtUntil = (s?: string | null) => (s ? new Date(s).toLocaleString("zh-CN", { hour12: false }) : "");
export const secMsg = (r: { code?: string; error?: string; status?: number }) =>
  (r.code === "FUND_PASSWORD_LOCKED" ? "资金密码错误次数过多，已被锁定" : null) ?? (r.code && SEC_ERR[r.code]) ?? (r.status === 404 || r.status === undefined ? "服务暂不可用，请稍后再试" : r.error ?? "操作失败");
