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
    if (response.status >= 400 && response.status < 500) return { ok: false, error: msg, source: "api" };
  } catch {
    /* 网络错误 → mock */
  }
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

/** 已对接：POST /kyc（基础信息）；证件文件上传接口待定 */
export function submitKyc(body: KycSubmission) {
  return tryApi(
    () => api.POST("/kyc", { body }),
    () => ({ level: 0, status: "pending" as const, submittedAt: new Date().toISOString(), reason: null }),
  );
}

/* ---------------- 缺口接口（仅 mock） ---------------- */

/** 缺口：POST /auth/verification-code（注册 / 找回密码 / 提现安全验证） */
export async function sendVerificationCode(
  _target: string,
  _scene: "register" | "reset_password" | "withdraw",
): Promise<Result<{ ttl: number }>> {
  await delay();
  return { ok: true, data: { ttl: 60 }, source: "mock" };
}

/** 缺口：POST /auth/password/reset */
export async function resetPassword(_b: {
  account: string;
  code: string;
  password: string;
}): Promise<Result<true>> {
  await delay();
  return { ok: true, data: true, source: "mock" };
}

/** 已对接：GET /wallet/withdraw/networks?asset=（网络列表 + gasFeeEstimate）。
 *  平台不收提现手续费，仅收链上 Gas。接口未实现（404/5xx/网络错误）时回退 mock。 */
export interface WithdrawConfig {
  asset: string;
  network: string;
  minAmount: string;
  maxAmount: string;
  /** 链上网络费（Gas）预估，平台不另收手续费 */
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
/** linbit 规则：USDT 最小提现 12 */
const MIN_FLOOR: Record<string, number> = { USDT: 12 };
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
  await delay(150);
  return { ok: true, data: mockNetworks(asset), source: "mock" };
}
export async function getWithdrawConfig(asset: string, network: string): Promise<Result<WithdrawConfig>> {
  const r = await listWithdrawNetworks(asset);
  const nets = r.data?.networks ?? [];
  const n = nets.find((x) => x.network === network) ?? nets[0];
  const c = WD_CONFIG[asset] ?? WD_CONFIG.USDT;
  const apiMin = Number(n?.minWithdraw ?? c.min);
  const min = String(Math.max(apiMin, MIN_FLOOR[asset] ?? 0));
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

/** 缺口：POST /kyc/documents（multipart，返回 fileId，提交 /kyc 时引用） */
export async function uploadKycDocument(file: File, _side: "front" | "selfie"): Promise<Result<{ fileId: string }>> {
  await delay(600);
  return { ok: true, data: { fileId: `mock_${file.name}` }, source: "mock" };
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
export async function listCoupons(): Promise<Result<Coupon[]>> {
  await delay();
  return {
    ok: true,
    source: "mock",
    data: [
      { id: "c1", type: "fee", amount: 5, title: "手续费抵扣券", desc: "可抵扣现货 / 合约交易手续费", expireAt: "2026-12-31", status: "unused" },
      { id: "c2", type: "trial", amount: 5, title: "合约体验金", desc: "激活需合约账户自有资金 ≥ 5 USDT", expireAt: "2026-12-15", status: "unused" },
      { id: "c3", type: "trial", amount: 10, title: "合约体验金", desc: "新手合约交易 Lv.2 奖励", expireAt: "2026-11-20", status: "active" },
      { id: "c4", type: "fee", amount: 5, title: "手续费抵扣券", desc: "已于 2026-10-12 使用", expireAt: "2026-10-30", status: "used" },
    ],
  };
}
/** 缺口：POST /coupons/{id}/activate */
export async function activateCoupon(_id: string): Promise<Result<true>> {
  await delay();
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
/** 缺口：GET /notifications、POST /notifications/read */
export async function listNotifications(): Promise<Result<Notice[]>> {
  await delay();
  return {
    ok: true,
    source: "mock",
    data: [
      { id: "n1", kind: "security", title: "新设备登录提醒", body: "你的账户于 Chrome · macOS 登录，如非本人操作请立即修改密码。", time: "2026-11-02 09:12", read: false },
      { id: "n2", kind: "activity", title: "新人奖励已到账", body: "5 USDT 手续费抵扣券已发放至卡券中心。", time: "2026-11-01 18:40", read: false },
      { id: "n3", kind: "trade", title: "提现申请已提交", body: "提现 100 USDT（TRC20）审核中。", time: "2026-10-30 14:03", read: true },
      { id: "n4", kind: "system", title: "系统维护通知", body: "11 月 5 日 02:00–03:00 (UTC+8) 进行系统升级。", time: "2026-10-29 10:00", read: true },
    ],
  };
}

export interface Announcement {
  id: string;
  category: string;
  title: string;
  date: string;
  pinned?: boolean;
}
/** 缺口：GET /announcements?category=&page= */
export async function listAnnouncements(): Promise<Result<Announcement[]>> {
  await delay();
  return {
    ok: true,
    source: "mock",
    data: [
      { id: "a1", category: "最新公告", title: "linbit 现货、合约交易手续费说明：现货 0.1%、合约 0.05%，提现仅收链上 Gas", date: "2026-11-01", pinned: true },
      { id: "a2", category: "新币上线", title: "示例代币 DEMO/USDT 现货交易即将上线", date: "2026-10-30" },
      { id: "a3", category: "活动", title: "新人任务上线：最高领取 215 USDT 奖励", date: "2026-10-28" },
      { id: "a4", category: "系统维护", title: "11 月 5 日系统升级维护公告", date: "2026-10-27" },
      { id: "a5", category: "最新公告", title: "关于加强提现安全验证（2FA）的公告", date: "2026-10-20" },
    ],
  };
}

export interface Device {
  id: string;
  name: string;
  ip: string;
  location: string;
  lastActive: string;
  current?: boolean;
}
/** 缺口：GET /account/devices、DELETE /account/devices/{id} */
export async function listDevices(): Promise<Result<Device[]>> {
  await delay();
  return {
    ok: true,
    source: "mock",
    data: [
      { id: "d1", name: "Chrome · macOS", ip: "203.0.113.24", location: "示例城市", lastActive: "当前在线", current: true },
      { id: "d2", name: "linbit App · iOS", ip: "198.51.100.7", location: "示例城市", lastActive: "2026-11-01 21:30" },
      { id: "d3", name: "Edge · Windows", ip: "192.0.2.55", location: "示例城市", lastActive: "2026-10-22 08:15" },
    ],
  };
}
export async function removeDevice(_id: string): Promise<Result<true>> {
  await delay();
  return { ok: true, data: true, source: "mock" };
}

export interface WhitelistAddress {
  id: string;
  asset: string;
  network: string;
  address: string;
  memo?: string;
  label: string;
}
/** 缺口：GET/POST/DELETE /wallet/withdraw/addresses（新增需 2FA） */
export async function listAddresses(): Promise<Result<WhitelistAddress[]>> {
  await delay();
  return {
    ok: true,
    source: "mock",
    data: [{ id: "w1", asset: "USDT", network: "TRC20", address: "TDemo0000000000000000000000000001", label: "示例钱包" }],
  };
}
export async function addAddress(
  a: Omit<WhitelistAddress, "id"> & { totpCode: string },
): Promise<Result<WhitelistAddress>> {
  await delay();
  const { totpCode: _t, ...rest } = a;
  return { ok: true, data: { ...rest, id: `w${Date.now()}` }, source: "mock" };
}
