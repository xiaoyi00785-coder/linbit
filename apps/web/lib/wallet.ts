import type { components } from "@linbit/api-client";
import { rest, useRest, type RestResult } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
/** 资产/钱包新接口（feat/spot-asset-gaps @2039887，docs/api/openapi.yaml）。除 WalletNetwork 外为本地镜像。 */
export type AcctKey = "funding" | "spot" | "futures";
export interface OverviewAccount { account: AcctKey; available: boolean; totalValueUsdt: string; balances: { asset: string; free: string; locked?: string; valuationUsd?: string | null; priceUsd?: string | null }[] }
export interface AssetsOverview { totalValueUsdt: string; accounts: OverviewAccount[]; depositAccount?: "funding" | "spot"; withdrawAccount?: "funding" | "spot"; transferable?: Record<string, string[]> }
/** 派生自 api-client WalletNetwork（含 recommended、sortOrder）；asset / network 在本端为必填。 */
export type WalletNetwork = Omit<components["schemas"]["WalletNetwork"], "asset" | "network"> & { asset: string; network: string };
export interface InternalTransferReq { clientId: string; asset: string; amount: string; toUid?: string; toEmail?: string; totpCode: string; fundPassword?: string }
export interface InternalTransfer { id: string; clientId: string; direction: "out" | "in"; fromUid: string; toUid: string; toMaskedEmail?: string; asset: string; amount: string; fee: string; status: "pending" | "completed" | "failed"; failReason?: string | null; createdAt: string; completedAt?: string | null }
type Page<T> = { items: T[]; page: number; pageSize: number; total: number };

export const SOON = "该功能即将上线";
/** 404 或网关不可达（source=network）视为后端尚未上线 */
export const isSoon = (r: RestResult<unknown>) => !r.ok && !r.code && (r.status === 404 || r.status === undefined);
export const newClientId = () => `c-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

// 未登录不请求（避免 /login 等页面控制台出现 401）
export const useOverview = () => useRest<AssetsOverview>(useAuthStore((st) => st.isAuthed) ? "/assets/overview" : null);
export const getNetworks = (asset?: string) => rest<{ asset?: string; networks: WalletNetwork[] }>("GET", `/wallet/networks${asset ? `?asset=${encodeURIComponent(asset)}` : ""}`);
export const createInternalTransfer = (b: InternalTransferReq) => rest<InternalTransfer>("POST", "/wallet/internal-transfer", b);
export const listInternalTransfers = (page = 1, pageSize = 20) => rest<Page<InternalTransfer>>("GET", `/wallet/internal-transfers?page=${page}&pageSize=${pageSize}`);

const ERR: Record<string, string> = {
  VALIDATION_ERROR: "参数有误，请检查", WITHDRAW_LOCKED: "安全设置变更后 24 小时内禁止提现", FUND_PASSWORD_INVALID: "资金密码错误，请重新输入", FUND_PASSWORD_LOCKED: "资金密码已锁定", FUND_PASSWORD_NOT_SET: "请先设置资金密码", FUND_PASSWORD_REQUIRED: "请输入资金密码", TWOFA_REQUIRED: "请先绑定谷歌验证", INVALID_TOTP: "动态码错误，请重新输入",
  INTERNAL_TRANSFER_DISABLED: "内部转账暂时关闭", RECIPIENT_NOT_FOUND: "收款方不存在，请核对 UID / 邮箱", SELF_TRANSFER: "不能转给自己",
  RECIPIENT_SUB_ACCOUNT: "不支持转入子账户", RECIPIENT_UNAVAILABLE: "收款方账户状态异常，暂无法收款", PRICE_UNAVAILABLE: "暂无法估值，请稍后再试",
  INSUFFICIENT_BALANCE: "可用余额不足", IDEMPOTENCY_CONFLICT: "请求重复，请刷新后重试",
  ACCOUNT_NOT_SUPPORTED: "理财账户不支持划转，请使用申购 / 赎回", SAME_ACCOUNT: "转出与转入账户不能相同",
};
/** 404 无业务码 / 网关不可达 → 即将上线；否则优先业务码映射，其次后端中文 message */
export const errMsg = (r: RestResult<unknown>) => {
  // 站内转账 24 小时额度由后端配置，没有用户侧接口可读：只展示后端 message（带真实额度），不在前端写死数字
  if (r.code === "INTERNAL_TRANSFER_LIMIT_EXCEEDED") return r.error || "超出 24 小时转账额度";
  if (r.code && ERR[r.code]) return ERR[r.code];
  if (!r.code && isSoon(r)) return SOON;
  return r.error || "操作失败，请稍后重试";
};
