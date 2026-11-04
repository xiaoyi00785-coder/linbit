// Pure display helpers for the assets / deposit pages (acceptance r8).

/** 接口不可用时按币种回退的充值网络（代码与 wallet 服务一致）；绝不把 USDT 的网络套到其他币种。 */
export const DEP_NETS_FALLBACK: Record<string, string[]> = {
  USDT: ["TRC20", "ERC20", "BEP20", "SOL", "ARBITRUM"],
  BTC: ["BTC"],
  ETH: ["ERC20", "ARBITRUM"],
  SOL: ["SOL"],
  BNB: ["BEP20"],
  TRX: ["TRC20"],
};

type NetRes = { ok?: boolean; source?: string; data?: { networks?: { asset?: string; network: string; depositEnabled?: boolean }[] } } | null | undefined;

/** 充值网络：GET /wallet/networks?asset= 的生效配置，只列 depositEnabled 且属于该币种的网络。 */
export function depositNetsOf(asset: string, r: NetRes): string[] {
  if (r && r.ok && r.source === "api") {
    return (r.data?.networks ?? []).filter((x) => x.depositEnabled !== false && (!x.asset || x.asset === asset)).map((x) => x.network);
  }
  return DEP_NETS_FALLBACK[asset] ?? [];
}

/** 单价：用后端 priceUsd（market-data 实时指数价），不用 估值/数量 反推。 */
export function unitPrice(r: { price?: number }, fmt: (v: number) => string): string {
  return r.price && r.price > 0 ? "$" + fmt(r.price) : "--";
}

const ACCT: Record<string, string> = { funding: "资金账户", spot: "现货账户", futures: "合约账户", earn: "理财账户" };
type Trade = { createdAt: string; tradeId?: string | number; id?: string | number; symbol: string; side: string; quantity: string };
type Transfer = { createdAt: string; id?: string | number; ref?: string; asset: string; amount: string; from: string; to: string };

/** 现货记录：现货成交 + 涉及现货账户的划转，按时间倒序合并。 */
export function spotRecords(trades: Trade[] | undefined, transfers: Transfer[] | undefined, fmt: (v: string) => string) {
  return [
    ...(trades ?? []).map((x) => ({ t: x.createdAt, k: "t" + (x.tradeId ?? x.id), row: [x.symbol, x.side === "buy" ? "买入" : "卖出", fmt(x.quantity)] })),
    ...(transfers ?? []).filter((x) => x.from === "spot" || x.to === "spot").map((x) => ({
      t: x.createdAt,
      k: "x" + (x.id ?? x.ref ?? x.createdAt),
      row: [x.asset, x.to === "spot" ? `划转转入（${ACCT[x.from] ?? x.from}）` : `划转转出（${ACCT[x.to] ?? x.to}）`, (x.to === "spot" ? "+" : "-") + fmt(x.amount)],
    })),
  ].sort((a, b) => +new Date(b.t) - +new Date(a.t));
}
