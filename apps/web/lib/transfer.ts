import { rest } from "@/lib/rest";
/** 账户间划转（docs/api/openapi.yaml · feat/home-transfer）。后端目前仅支持 spot ⇄ futures。 */
export type TransferAcct = "spot" | "futures";
export const ACCT_KEY: Record<string, TransferAcct | undefined> = { 现货账户: "spot", 合约账户: "futures" };
export interface TransferReq { clientId: string; asset: string; amount: string; from: TransferAcct; to: TransferAcct }
export interface TransferRec { id: string; clientId: string | null; asset: string; amount: string; from: string; to: string; createdAt?: string }
export const newClientId = () => `c-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
export const createTransfer = (b: TransferReq) => rest<TransferRec>("POST", "/assets/transfer", b);
export const listTransfers = (q = "") => rest<{ items: TransferRec[]; total: number }>("GET", `/assets/transfers${q}`);
const ERR: Record<string, string> = { INSUFFICIENT_BALANCE: "可用余额不足", ACCOUNT_NOT_SUPPORTED: "该账户暂不支持划转", SAME_ACCOUNT: "转出与转入账户不能相同", VALIDATION_ERROR: "参数有误，请检查", IDEMPOTENCY_CONFLICT: "请求重复，请刷新后重试" };
export const transferErr = (e?: string, status?: number) => status === 404 || status === undefined ? "划转服务即将上线" : (e && ERR[e]) || e || "划转失败，请稍后重试";
