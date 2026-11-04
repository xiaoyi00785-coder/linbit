import { fmtLocal } from "./time";
/**
 * 业务服务适配层。
 *
 * - openapi.yaml 已定义的接口：优先通过 @linbit/api-client 调用（Prism mock / 真实后端）；
 *   请求失败（mock 未启动、网络错误）时回退到本地 mock 数据，保证原型可演示。
 * - openapi.yaml 尚未定义的接口（验证码、找回密码、卡券、通知、公告、设备、地址白名单、
 *   提现配置、KYC 文件上传）：只走本地 mock，函数名与返回结构按未来 REST 形态预留，
 *   接口落地后只需替换函数体。见 docs/ui/api-gaps.md。
 */
import type { components } from "@linbit/api-client";
import { api } from "./api";
import { rest } from "./rest";
import { MOCK_ENABLED, NET_ERR } from "./mockFlag";
import { API_BASE } from "./api";
import { useAuthStore } from "./store";

type S = components["schemas"];
export type AuthTokens = S["AuthTokens"];
export type Withdrawal = S["Withdrawal"];
export type WithdrawRequest = S["WithdrawRequest"];
export type KycSubmission = S["KycSubmission"];

/** 统一结果：source 标明数据来自 API 还是本地 mock，便于联调排查 */
export interface Result<T> {
  ok: boolean;
  data?: T;
  error?: string;
  /** 后端业务错误码（APIError.code），如 TWOFA_REQUIRED / INVALID_TOTP */
  code?: string;
  /** 429 时的 Retry-After（秒） */
  retryAfter?: number;
  source: "api" | "mock";
}

const delay = (ms = 300) => new Promise((r) => setTimeout(r, ms));

const MOCK_TOKENS: AuthTokens = {
  accessToken: "mock-access-token",
  refreshToken: "mock-refresh-token",
  tokenType: "Bearer",
  expiresIn: 3600,
};

async function tryApi<T>(
  call: () => Promise<{ data?: T; error?: unknown; response: Response }>,
  fallback: () => T | Promise<T>,
): Promise<Result<T>> {
  try {
    const { data, error, response } = await call();
    if (data !== undefined && response.ok) return { ok: true, data, source: "api" };
    const msg =
      (error as { message?: string } | undefined)?.message ?? `HTTP ${response.status}`;
    // 4xx 视为业务错误，直接返回；5xx 回退 mock
    if (response.status >= 400 && response.status < 500) return { ok: false, error: msg, code: (error as { code?: string } | undefined)?.code, source: "api" };
    // 带后端文案 / 业务码的 5xx：原样返回，不回退 mock（避免下单 500 时显示假成功或无提示）
    const em = (error as { message?: string; code?: string } | undefined);
    if (response.status >= 500 && (em?.message || em?.code)) return { ok: false, error: em?.message || NET_ERR, code: em?.code, source: "api" };
  } catch {
    /* 网络错误 → mock */
  }
  if (!MOCK_ENABLED) return { ok: false, error: NET_ERR, code: "NETWORK", source: "api" };
  return { ok: true, data: await fallback(), source: "mock" };
}

/* ---------------- Auth（已有接口） ---------------- */

/** 已对接：POST /auth/register（验证码字段待接口补充，当前仅前端校验） */
export function register(body: S["RegisterRequest"]) {
  return tryApi(() => api.POST("/auth/register", { body }), () => MOCK_TOKENS);
}

/** 已对接：POST /auth/login（含可选 totpCode） */
export function login(body: S["LoginRequest"]) {
  return tryApi(() => api.POST("/auth/login", { body }), () => MOCK_TOKENS);
}

/** 已对接：POST /wallet/withdraw（含 memo、totpCode） */
export function withdraw(body: WithdrawRequest) {
  return tryApi(
    () => api.POST("/wallet/withdraw", { body }),
    (): Withdrawal => ({
      id: "wd_mock_0001",
      asset: body.asset,
      network: body.network,
      address: body.address,
      amount: body.amount,
      fee: estimateGas(body.asset, body.network),
      status: "pending",
      createdAt: new Date().toISOString(),
    }),
  );
}

/** 已对接：GET /wallet/withdraw/records */
export function withdrawRecords(asset?: string) {
  return tryApi(
    () => api.GET("/wallet/withdraw/records", { params: { query: asset ? { asset } : {} } }),
    () => ({ page: 1, pageSize: 20, total: 0, items: [] as Withdrawal[] }),
  );
}

/** 已对接：POST /kyc（基础信息 + documents{front,selfie,back?}，fileId 来自 POST /kyc/documents） */
export function submitKyc(body: KycSubmission) {
  return tryApi(
    () => api.POST("/kyc", { body }),
    () => ({ level: 0, status: "pending" as const, submittedAt: new Date().toISOString(), reason: null }),
  );
}

/* ---------------- 缺口接口（仅 mock） ---------------- */

/** 缺口：POST /auth/verification-code（注册 / 找回密码 / 提现安全验证） */
/** 已对接：GET /wallet/withdraw/networks?asset=（网络列表 + gasFeeEstimate）。
 *  手续费 = 接口返回的 withdrawFee（wallet_network_settings 生效值）。接口未实现（404/5xx/网络错误）时回退 mock。 */
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
const WD_CONFIG: Record<string, { min: string; max: string }> = {
  USDT: { min: "12", max: "20000" },
  BTC: { min: "0.001", max: "2" },
  ETH: { min: "0.01", max: "50" },
};
/** mock：各网络链上 Gas 预估（USDT 计），实际以接口实时返回为准 */
const GAS_USDT: Record<string, number> = {
  "Arbitrum One": 1,
  "BEP20(BSC)": 1,
  TRC20: 2,
  ERC20: 10,
  Solana: 1,
  Bitcoin: 5,
};
const PRICE: Record<string, number> = { USDT: 1, BTC: 76768, ETH: 2600 };
export function estimateGas(asset: string, network: string): string {
  const usdt = GAS_USDT[network] ?? 1;
  const p = PRICE[asset] ?? 1;
  return asset === "USDT" ? String(usdt) : (usdt / p).toFixed(6);
}
const MOCK_NETS: Record<string, string[]> = {
  USDT: ["TRC20", "ERC20", "BEP20(BSC)", "Solana", "Arbitrum One"],
  BTC: ["Bitcoin"],
  ETH: ["ERC20", "Arbitrum One"],
};
function mockNetworks(asset: string): S["WithdrawNetworkOptions"] {
  return {
    asset,
    networks: (MOCK_NETS[asset] ?? MOCK_NETS.USDT).map((network) => ({
      network,
      minWithdraw: (WD_CONFIG[asset] ?? WD_CONFIG.USDT).min,
      gasFeeEstimate: estimateGas(asset, network),
      gasFeeAsset: asset,
      estimatedArrivalMinutes: null,
    })),
  };
}
export async function listWithdrawNetworks(asset: string): Promise<Result<S["WithdrawNetworkOptions"]>> {
  try {
    const { data, response } = await api.GET("/wallet/withdraw/networks", { params: { query: { asset } } });
    if (response.ok && data && data.networks?.length) return { ok: true, data, source: "api" };
  } catch {
    /* 网络错误 → mock */
  }
  if (!MOCK_ENABLED) return { ok: false, error: "提现网络暂时无法获取，请稍后再试", source: "api" };
  await delay(150);
  return { ok: true, data: mockNetworks(asset), source: "mock" };
}
export async function getWithdrawConfig(asset: string, network: string): Promise<Result<WithdrawConfig>> {
  const r = await listWithdrawNetworks(asset);
  const nets = r.data?.networks ?? [];
  const n = nets.find((x) => x.network === network) ?? nets[0];
  const c = WD_CONFIG[asset] ?? WD_CONFIG.USDT;
  const apiMin = Number(n?.minWithdraw ?? c.min);
  // 直接使用接口 minWithdraw；mock 回退时取 WD_CONFIG（USDT 12）
  const min = String(apiMin);
  const gas = n?.gasFeeEstimate ?? estimateGas(asset, network);
  return {
    ok: true,
    source: r.source,
    data: {
      asset,
      network: n?.network ?? network,
      minAmount: min,
      maxAmount: c.max,
      gasFeeEstimate: gas,
      gasFeeAsset: n?.gasFeeAsset ?? asset,
      estimatedArrivalMinutes: n?.estimatedArrivalMinutes ?? null,
      memoRequired: false,
    },
  };
}

/* ---------------- 现货交易（已对接 /spot/*，API 不可达时回退本地 mock） ---------------- */
export type Order = S["Order"];
export type SpotTrade = S["Trade"];
export type PlaceOrderRequest = S["PlaceOrderRequest"];
type Page<T> = { items: T[]; page: number; pageSize: number; total: number };

const mockOrders: Order[] = [];
const page = <T,>(items: T[]): Page<T> => ({ items, page: 1, pageSize: 20, total: items.length });
let mockSeq = 9000000001;

export function placeSpotOrder(body: PlaceOrderRequest) {
  return tryApi(
    () => api.POST("/spot/orders", { body }),
    (): Order => {
      const o: Order = {
        orderId: String(mockSeq++),
        clientOrderId: body.clientOrderId ?? null,
        symbol: body.symbol,
        side: body.side,
        type: body.type,
        price: body.price ?? null,
        quantity: body.quantity,
        filledQuantity: body.type === "market" ? body.quantity : "0",
        status: body.type === "market" ? "filled" : "new",
        timeInForce: body.timeInForce ?? "GTC",
        createdAt: new Date().toISOString(),
      } as Order;
      mockOrders.unshift(o);
      return o;
    },
  );
}
export function cancelSpotOrder(orderId: string, symbol: string) {
  return tryApi(
    () => api.DELETE("/spot/orders/{orderId}", { params: { path: { orderId }, query: { symbol } } }),
    (): Order => {
      const o = mockOrders.find((x) => x.orderId === orderId);
      if (o) o.status = "canceled";
      return o as Order;
    },
  );
}
const OPEN = ["new", "partially_filled"];
export function listOpenOrders(symbol?: string) {
  return tryApi(
    () => api.GET("/spot/orders/open", { params: { query: { symbol } } }),
    () => page(mockOrders.filter((o) => OPEN.includes(o.status))),
  );
}
export function listOrderHistory(symbol?: string) {
  return tryApi(
    () => api.GET("/spot/orders/history", { params: { query: { symbol } } }),
    () => page(mockOrders.filter((o) => !OPEN.includes(o.status))),
  );
}
export function listMyTrades(symbol?: string) {
  return tryApi(
    () => api.GET("/spot/trades", { params: { query: { symbol } } }),
    () =>
      page<SpotTrade>(
        mockOrders
          .filter((o) => o.status === "filled")
          .map((o) => ({
            tradeId: "t" + o.orderId,
            orderId: o.orderId,
            symbol: o.symbol,
            side: o.side,
            price: o.price ?? "76768.4",
            quantity: o.quantity,
            fee: (Number(o.quantity) * 0.001).toFixed(8), // 现货 0.1%，从所得币种扣
            feeAsset: o.side === "buy" ? "BTC" : "USDT",
            role: "taker",
            createdAt: o.createdAt,
          }) as SpotTrade),
      ),
  );
}

/** 已对接：POST /kyc/documents（multipart：file + side，201 返回 { fileId, side }）。
 *  仅 NEXT_PUBLIC_ENABLE_MOCK=1 且网关不可达时才回退演示 fileId。 */
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
  } catch { /* 网络错误 */ }
  if (!MOCK_ENABLED) return { ok: false, error: NET_ERR, code: "NETWORK", source: "api" };
  await delay(600);
  return { ok: true, data: { fileId: `mock_${file.name}` }, source: "mock" };
}

/** GET /kyc/status：提交后刷新审核状态 */
export function getKycStatus() {
  return tryApi(
    () => api.GET("/kyc/status"),
    (): S["KycStatus"] => ({ level: 0, status: "pending", submittedAt: new Date().toISOString(), reason: null }),
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
/** 已对接：GET /coupons、GET /coupons/summary、POST /coupons/{id}/activate（growth 服务）。网关不可达时回退演示数据。 */
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
  if (r.source === "api" || !MOCK_ENABLED) return { ok: false, error: r.error, source: "api", data: [] };
  await delay();
  return { ok: true, source: "mock", data: [
      { id: "c1", type: "fee", amount: 5, title: "手续费抵扣券", desc: "可抵扣现货 / 合约交易手续费", expireAt: "2026-12-31", status: "unused" },
      { id: "c2", type: "trial", amount: 5, title: "合约体验金", desc: "激活需合约账户自有资金 ≥ 5 USDT", expireAt: "2026-12-15", status: "unused" },
      { id: "c3", type: "trial", amount: 10, title: "合约体验金", desc: "新手合约交易 Lv.2 奖励", expireAt: "2026-11-20", status: "active" },
      { id: "c4", type: "fee", amount: 5, title: "手续费抵扣券", desc: "已于 2026-10-12 使用", expireAt: "2026-10-30", status: "used" },
    ] };
}
export async function couponSummary() {
  const r = await rest<{ available: number; active: number }>("GET", "/coupons/summary");
  return r.ok ? r.data : undefined;
}
export async function activateCoupon(id: string): Promise<Result<true>> {
  const r = await rest<{ id: number; status: string }>("POST", `/coupons/${id}/activate`, {});
  if (r.source === "api") return r.ok ? { ok: true, data: true, source: "api" } : { ok: false, error: r.error, source: "api" };
  if (!MOCK_ENABLED) return { ok: false, error: r.error ?? NET_ERR, code: "NETWORK", source: "api" };
  return { ok: true, data: true, source: "mock" };
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
  if (r.source === "api") return r.ok && r.data ? { ok: true, source: "api", data: r.data.items.map((n) => ({ ...n, time: fmtLocal(n.time) })) } : { ok: false, error: r.error, source: "api", data: [] };
  if (!MOCK_ENABLED) return { ok: false, error: r.error ?? NET_ERR, code: "NETWORK", source: "api", data: [] };
  await delay();
  return { ok: true, source: "mock", data: [
      { id: "n1", kind: "security", title: "新设备登录提醒", body: "你的账户于 Chrome · macOS 登录，如非本人操作请立即修改密码。", time: "2026-11-02 09:12", read: false },
      { id: "n2", kind: "activity", title: "新人奖励已到账", body: "5 USDT 手续费抵扣券已发放至卡券中心。", time: "2026-11-01 18:40", read: false },
      { id: "n3", kind: "trade", title: "提现申请已提交", body: "提现 100 USDT（TRC20）审核中。", time: "2026-10-30 14:03", read: true },
      { id: "n4", kind: "system", title: "系统维护通知", body: "11 月 5 日 02:00–03:00 (UTC+8) 进行系统升级。", time: "2026-10-29 10:00", read: true },
    ] };
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
/** 已对接：GET /announcements（公开，已发布，最多 100 条）。网关不可达且未开启 mock 时返回错误。 */
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
  if (r.source === "api") return r.ok && r.data ? { ok: true, source: "api", data: (r.data.items ?? []).map((a) => ({
    id: String(a.id), category: ANN_CAT[a.category ?? ""] ?? a.category ?? "最新公告", title: a.title,
    date: a.publishAt ? new Date(a.publishAt).toLocaleDateString("sv-SE") : "-", pinned: !!a.pinned })) } : { ok: false, error: r.error, source: "api", data: [] };
  if (!MOCK_ENABLED) return { ok: false, error: r.error ?? NET_ERR, code: "NETWORK", source: "api", data: [] };
  await delay();
  return {
    ok: true,
    source: "mock",
    data: [
      { id: "a1", category: "最新公告", title: "linbit 现货、合约交易手续费说明：现货 0.1%、合约 0.05%，提现仅收链上 Gas", date: "2026-11-01", pinned: true },
      { id: "a2", category: "新币上线", title: "示例代币 DEMO/USDT 现货交易即将上线", date: "2026-10-30" },
      { id: "a3", category: "活动", title: "新人任务上线：完成任务领取奖励", date: "2026-10-28" },
      { id: "a4", category: "系统维护", title: "11 月 5 日系统升级维护公告", date: "2026-10-27" },
      { id: "a5", category: "最新公告", title: "关于加强提现安全验证（2FA）的公告", date: "2026-10-20" },
    ],
  };
}

