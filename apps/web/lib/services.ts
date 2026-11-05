import { fmtLocal } from "./time";
/**
 * 业务服务适配层。
 *
 * 主网规则：只调用真实接口（@linbit/api-client / rest），没有任何本地伪造数据。
 * 任何失败（4xx / 5xx / 网络错误）都返回 ok:false，界面显示错误或空状态；
 * 下单 / 提现等写操作失败绝不返回“成功”。
 */
import type { components } from "@linbit/api-client";
import { api } from "./api";
import { rest } from "./rest";
const NET_ERR = "网络异常，请稍后再试";
import { API_BASE } from "./api";
import { useAuthStore } from "./store";

type S = components["schemas"];
export type AuthTokens = S["AuthTokens"];
export type Withdrawal = S["Withdrawal"];
export type WithdrawRequest = S["WithdrawRequest"];
export type KycSubmission = S["KycSubmission"];

/** 统一结果：source = api（网关有响应）/ network（网关不可达） */
export interface Result<T> {
  ok: boolean;
  data?: T;
  error?: string;
  /** 后端业务错误码（APIError.code），如 TWOFA_REQUIRED / INVALID_TOTP */
  code?: string;
  /** 429 时的 Retry-After（秒） */
  retryAfter?: number;
  source: "api" | "network";
}

async function callApi<T>(
  call: () => Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<Result<T>> {
  try {
    const { data, error, response } = await call();
    if (data !== undefined && response.ok) return { ok: true, data, source: "api" };
    const em = error as { message?: string; code?: string } | undefined;
    if (response.status < 500) return { ok: false, error: em?.message ?? `HTTP ${response.status}`, code: em?.code, source: "api" };
    return { ok: false, error: em?.message || NET_ERR, code: em?.code ?? "NETWORK", source: "api" };
  } catch {
    return { ok: false, error: NET_ERR, code: "NETWORK", source: "network" };
  }
}

/* ---------------- Auth（已有接口） ---------------- */

/** 已对接：POST /auth/register（验证码字段待接口补充，当前仅前端校验） */
export function register(body: S["RegisterRequest"]) {
  return callApi(() => api.POST("/auth/register", { body }));
}

/** 已对接：POST /auth/login（含可选 totpCode） */
export function login(body: S["LoginRequest"]) {
  return callApi(() => api.POST("/auth/login", { body }));
}

/** 已对接：POST /wallet/withdraw（含 memo、totpCode） */
export function withdraw(body: WithdrawRequest) {
  return callApi(
    () => api.POST("/wallet/withdraw", { body }),
  );
}

/** 已对接：GET /wallet/withdraw/records */
export function withdrawRecords(asset?: string) {
  return callApi(
    () => api.GET("/wallet/withdraw/records", { params: { query: asset ? { asset } : {} } }),
  );
}

/** 已对接：POST /kyc（基础信息 + documents{front,selfie,back?}，fileId 来自 POST /kyc/documents） */
export function submitKyc(body: KycSubmission) {
  return callApi(
    () => api.POST("/kyc", { body }),
  );
}

/** 已对接：GET /wallet/withdraw/networks?asset=（网络列表 + gasFeeEstimate）。
 *  手续费 = 接口返回的 withdrawFee（wallet_network_settings 生效值）。接口失败时返回错误，不做本地估算。 */
export interface WithdrawConfig {
  asset: string;
  network: string;
  minAmount: string;
  maxAmount: string;
  /** 提现手续费：接口 gasFeeEstimate（= wallet_network_settings.withdraw_fee 生效值） */
  gasFeeEstimate: string;
  gasFeeAsset: string;
  estimatedArrivalMinutes?: number | null;
  memoRequired: boolean;
}
export type WithdrawNetwork = S["WithdrawNetwork"];
export async function listWithdrawNetworks(asset: string): Promise<Result<S["WithdrawNetworkOptions"]>> {
  try {
    const { data, response } = await api.GET("/wallet/withdraw/networks", { params: { query: { asset } } });
    if (response.ok && data && data.networks?.length) return { ok: true, data, source: "api" };
    return { ok: false, error: "提现网络暂时无法获取，请稍后再试", source: "api" };
  } catch {
    return { ok: false, error: "提现网络暂时无法获取，请稍后再试", code: "NETWORK", source: "network" };
  }
}
export async function getWithdrawConfig(asset: string, network: string): Promise<Result<WithdrawConfig>> {
  const r = await listWithdrawNetworks(asset);
  const n = (r.data?.networks ?? []).find((x) => x.network === network) ?? r.data?.networks?.[0];
  // 只用接口值：拿不到网络配置就报错，不用本地估算的最小额 / 手续费
  if (!r.ok || !n || n.minWithdraw == null || n.gasFeeEstimate == null) return { ok: false, error: r.error ?? "提现配置暂时无法获取，请稍后再试", source: r.source };
  return {
    ok: true,
    source: "api",
    data: {
      asset,
      network: n.network,
      minAmount: String(n.minWithdraw),
      maxAmount: n.maxWithdraw ?? "",
      gasFeeEstimate: n.gasFeeEstimate,
      gasFeeAsset: n.gasFeeAsset ?? asset,
      estimatedArrivalMinutes: n.estimatedArrivalMinutes ?? null,
      memoRequired: n.memoRequired === true,
    },
  };
}

/* ---------------- 现货交易（已对接 /spot/*，失败如实返回错误） ---------------- */
export type Order = S["Order"];
export type SpotTrade = S["Trade"];
export type PlaceOrderRequest = S["PlaceOrderRequest"];

export function placeSpotOrder(body: PlaceOrderRequest) {
  return callApi(
    () => api.POST("/spot/orders", { body }),
  );
}
export function cancelSpotOrder(orderId: string, symbol: string) {
  return callApi(
    () => api.DELETE("/spot/orders/{orderId}", { params: { path: { orderId }, query: { symbol } } }),
  );
}
export function listOpenOrders(symbol?: string) {
  return callApi(
    () => api.GET("/spot/orders/open", { params: { query: { symbol } } }),
  );
}
export function listOrderHistory(symbol?: string) {
  return callApi(
    () => api.GET("/spot/orders/history", { params: { query: { symbol } } }),
  );
}
export function listMyTrades(symbol?: string) {
  return callApi(
    () => api.GET("/spot/trades", { params: { query: { symbol } } }),
  );
}

/** 已对接：POST /kyc/documents（multipart：file + side，201 返回 { fileId, side }）。
 *  失败如实返回错误。 */
export async function uploadKycDocument(file: File, side: "front" | "selfie" | "back" | "proof_of_address"): Promise<Result<{ fileId: string }>> {
  const fd = new FormData();
  fd.append("file", file);
  fd.append("side", side);
  const tok = useAuthStore.getState().accessToken;
  try {
    const res = await fetch(API_BASE + "/kyc/documents", { method: "POST", body: fd, headers: tok ? { Authorization: `Bearer ${tok}` } : {} });
    const txt = await res.text();
    const j = txt ? JSON.parse(txt) : {};
    if (res.ok && j?.fileId) return { ok: true, data: { fileId: String(j.fileId) }, source: "api" };
    const msg = res.status === 413 || j?.code === "FILE_TOO_LARGE" ? "文件过大" : res.status === 415 || j?.code === "UNSUPPORTED_MEDIA_TYPE" ? "仅支持 JPG / PNG" : j?.message || `HTTP ${res.status}`;
    if (res.status < 500 || j?.code) return { ok: false, error: msg, code: j?.code, source: "api" };
    return { ok: false, error: msg || NET_ERR, code: j?.code ?? "NETWORK", source: "api" };
  } catch {
    return { ok: false, error: NET_ERR, code: "NETWORK", source: "network" };
  }
}

/** GET /kyc/status：提交后刷新审核状态 */
export function getKycStatus() {
  return callApi(
    () => api.GET("/kyc/status"),
  );
}

export interface Coupon {
  id: string;
  type: "trial" | "fee";
  amount: number;
  title: string;
  desc: string;
  expireAt: string;
  status: "unused" | "active" | "used" | "expired";
}
/** 缺口：GET /coupons?status= */
/** 已对接：GET /coupons、GET /coupons/summary、POST /coupons/{id}/activate（growth 服务）。失败时返回错误（无本地兜底数据）。 */
type ApiCoupon = { id: number; kind: "trial" | "deduction"; amount: string; asset: string; title: string; status: "issued" | "active" | "expired" | "revoked"; activateBy: number; validDays: number; activatedAt?: number; bonus?: { remaining: string; status: string; expiresAt: number } };
const ymd = (ms?: number) => (ms ? new Date(ms).toLocaleDateString("sv-SE") : "-");
export async function listCoupons(): Promise<Result<Coupon[]>> {
  const r = await rest<{ items: ApiCoupon[] }>("GET", "/coupons");
  if (r.source === "api" && r.ok && r.data) {
    return { ok: true, source: "api", data: r.data.items.map((c) => {
      const used = c.bonus && c.bonus.status !== "active";
      const st: Coupon["status"] = c.status === "issued" ? "unused" : c.status === "active" ? (used ? "used" : "active") : c.status === "expired" ? "expired" : "used";
      return { id: String(c.id), type: c.kind === "trial" ? "trial" : "fee", amount: Number(c.amount), title: c.title,
        desc: c.kind === "trial" ? `激活后 ${c.validDays} 天有效${c.bonus ? ` · 剩余 ${c.bonus.remaining} ${c.asset}` : ""}` : "可抵扣现货 / 合约交易手续费",
        expireAt: c.status === "active" && c.bonus ? ymd(c.bonus.expiresAt) : ymd(c.activateBy), status: st };
    }) };
  }
  return { ok: false, error: r.error ?? NET_ERR, source: r.source, data: [] };
}
export async function couponSummary() {
  const r = await rest<{ available: number; active: number }>("GET", "/coupons/summary");
  return r.ok ? r.data : undefined;
}
export async function activateCoupon(id: string): Promise<Result<true>> {
  const r = await rest<{ id: number; status: string }>("POST", `/coupons/${id}/activate`, {});
  return r.ok ? { ok: true, data: true, source: "api" } : { ok: false, error: r.error ?? NET_ERR, code: r.code, source: r.source };
}

export interface Notice {
  id: string;
  kind: "system" | "trade" | "activity" | "security";
  title: string;
  body: string;
  time: string;
  read: boolean;
}
/** 已对接：GET /notifications、POST /notifications/read、GET /notifications/unread-count、GET|PUT /notifications/settings（account 服务） */
export async function listNotifications(kind?: string): Promise<Result<Notice[]>> {
  const r = await rest<{ items: { id: string; kind: Notice["kind"]; title: string; body: string; time: number; read: boolean }[] }>("GET", `/notifications?pageSize=50${kind ? `&kind=${kind}` : ""}`);
  return r.ok && r.data ? { ok: true, source: "api", data: r.data.items.map((n) => ({ ...n, time: fmtLocal(n.time) })) } : { ok: false, error: r.error ?? NET_ERR, source: r.source, data: [] };
}
export async function readNotifications(b: { ids?: string[]; all?: boolean }) {
  return rest<{ updated: number }>("POST", "/notifications/read", b);
}
export async function unreadCount() {
  const r = await rest<{ unread: number }>("GET", "/notifications/unread-count");
  return r.ok ? r.data?.unread ?? 0 : 0;
}
export type NotifyPrefs = { emailSystem: boolean; emailTrade: boolean; emailActivity: boolean; emailSecurity: boolean };
export const getNotifyPrefs = () => rest<NotifyPrefs>("GET", "/notifications/settings");
export const putNotifyPrefs = (p: Partial<NotifyPrefs>) => rest<NotifyPrefs>("PUT", "/notifications/settings", p);

export interface Announcement {
  id: string;
  category: string;
  title: string;
  date: string;
  pinned?: boolean;
}
/** 已对接：GET /announcements（公开，已发布，最多 100 条）。失败时返回错误。 */
type ApiAnnouncement = { id: number | string; category?: string; title: string; body?: string; publishAt?: string; pinned?: boolean };
const ANN_CAT: Record<string, string> = { general: "最新公告", listing: "新币上线", activity: "活动", maintenance: "系统维护", product: "产品更新" };
/** GET /announcements/{id}（r4）：未发布或不存在 → 404，调用方显示“公告不存在或已下线” */
export async function getAnnouncement(id: string): Promise<{ state: "ok"; title: string; body: string; date: string } | { state: "notfound" } | { state: "error"; error: string }> {
  if (!/^\d+$/.test(id)) return { state: "notfound" };
  const r = await rest<ApiAnnouncement & { date?: string }>("GET", `/announcements/${id}`);
  if (r.ok && r.data) return { state: "ok", title: r.data.title, body: r.data.body ?? "", date: r.data.date ?? (r.data.publishAt ? new Date(r.data.publishAt).toLocaleDateString("sv-SE") : "-") };
  if (r.status === 404) return { state: "notfound" };
  return { state: "error", error: r.error ?? NET_ERR };
}
export async function listAnnouncements(): Promise<Result<Announcement[]>> {
  const r = await rest<{ items: ApiAnnouncement[] }>("GET", "/announcements");
  return r.ok && r.data ? { ok: true, source: "api", data: (r.data.items ?? []).map((a) => ({
    id: String(a.id), category: ANN_CAT[a.category ?? ""] ?? a.category ?? "最新公告", title: a.title,
    date: a.publishAt ? new Date(a.publishAt).toLocaleDateString("sv-SE") : "-", pinned: !!a.pinned })) } : { ok: false, error: r.error ?? NET_ERR, source: r.source, data: [] };
}

