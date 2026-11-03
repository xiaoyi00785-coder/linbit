import { rest } from "@/lib/rest";
import type { AcctKey } from "@/lib/wallet";
export { newClientId } from "@/lib/wallet";
/** 唯一的划转实现（components/Transfer.tsx 使用；PR #9 Kit.tsx 的 Transfer 弹窗 rebase 时改为调用 openTransfer）。
 * 账户间划转 POST /assets/transfer：funding / spot / futures（futures 仅 USDT）；理财不是划转账户。 */
export const ACCT_KEY: Record<string, AcctKey | "earn" | undefined> = { 资金账户: "funding", 现货账户: "spot", 合约账户: "futures", 理财账户: "earn" };
export interface TransferReq { clientId: string; asset: string; amount: string; from: AcctKey; to: AcctKey }
export interface TransferRec { id: string; clientId: string | null; asset: string; amount: string; from: string; to: string; status: "pending" | "done" | "failed"; kind?: string; createdAt?: string; updatedAt?: string }
export const createTransfer = (b: TransferReq) => rest<TransferRec>("POST", "/assets/transfer", b);
export const listTransfers = (q = "") => rest<{ items: TransferRec[]; total: number }>("GET", `/assets/transfers${q}`);
export const ACCT_NAME: Record<string, string> = { funding: "资金账户", spot: "现货账户", futures: "合约账户", earn: "理财账户" };
