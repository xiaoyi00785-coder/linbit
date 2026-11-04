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

/** 充值网络代码 → 展示名（接口与下单仍用原始代码） */
const NET_NAMES: Record<string, string> = {
  BTC: "Bitcoin", ETH: "Ethereum", ERC20: "Ethereum (ERC20)", TRX: "TRON (TRC20)", TRC20: "TRON (TRC20)",
  BSC: "BNB Smart Chain (BEP20)", BEP20: "BNB Smart Chain (BEP20)", SOL: "Solana", ARBITRUM: "Arbitrum One", ARB: "Arbitrum One",
  POLYGON: "Polygon", MATIC: "Polygon", OP: "Optimism", OPTIMISM: "Optimism", AVAXC: "Avalanche C-Chain", TON: "TON", BASE: "Base",
};
export const netLabel = (code?: string | null) => (code ? NET_NAMES[code.toUpperCase()] ?? code : "");

type NetRow = { asset?: string; network: string; depositEnabled?: boolean; withdrawEnabled?: boolean; minDeposit?: string; minWithdraw?: string; estimatedArrivalMinutes?: number; recommended?: boolean };
type NetRes = { ok?: boolean; source?: string; data?: { networks?: NetRow[] } } | null | undefined;

/** 网络代码 → 本地图标（public/coins/*.svg）；未收录的网络回退为首字母圆标。 */
const NET_ICON: Record<string, string> = {
  BTC: "btc", ETH: "eth", ERC20: "eth", TRX: "trx", TRC20: "trx", BSC: "bnb", BEP20: "bnb", SOL: "sol", ARBITRUM: "arb", ARB: "arb",
  POLYGON: "matic", MATIC: "matic", OP: "op", OPTIMISM: "op", AVAXC: "avax", TON: "ton",
};
export const netIcon = (code: string): string | null => NET_ICON[code.toUpperCase()] ?? null;

/** 网络下拉的一行：附加信息只取后端字段（minDeposit / minWithdraw / estimatedArrivalMinutes / recommended），缺失则不展示，绝不编造。 */
export type NetInfo = { network: string; min?: string; eta?: number; recommended?: boolean };
export function netInfosOf(asset: string, r: NetRes, out = false): NetInfo[] {
  if (r && r.ok && r.source === "api") {
    return (r.data?.networks ?? [])
      .filter((x) => (out ? x.withdrawEnabled !== false : x.depositEnabled !== false) && (!x.asset || x.asset === asset))
      .map((x) => ({
        network: x.network,
        min: (out ? x.minWithdraw : x.minDeposit) || undefined,
        eta: !out && typeof x.estimatedArrivalMinutes === "number" && x.estimatedArrivalMinutes > 0 ? x.estimatedArrivalMinutes : undefined,
        recommended: x.recommended === true || undefined,
      }));
  }
  return (DEP_NETS_FALLBACK[asset] ?? []).map((network) => ({ network }));
}

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
