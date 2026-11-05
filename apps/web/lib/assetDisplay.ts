// Pure display helpers for the assets / deposit pages (acceptance r8).

/** 接口不可用（未登录 / 网关不可达）时按币种回退的充值网络；与主网 WALLET_NETWORKS_JSON 一致（USDT：TRC20、ERC20、BEP20、SOL，不含 ARBITRUM）；绝不把 USDT 的网络套到其他币种。 */
export const DEP_NETS_FALLBACK: Record<string, string[]> = {
  USDT: ["TRC20", "ERC20", "BEP20", "SOL"],
  USDC: ["ERC20", "SOL"],
  BTC: ["BTC"],
  ETH: ["ERC20"],
  SOL: ["SOL"],
  BNB: ["BEP20"],
  TRX: ["TRC20"],
};

/** 网络代码归一化：大写并去掉括号、空格、连字符等非字母数字字符，使 SOL / Solana、ARBITRUM / "Arbitrum One"、BEP20 / "BEP20(BSC)" 落到同一个键。 */
const netKey = (code: string) => code.toUpperCase().replace(/[^A-Z0-9]/g, "");

/** 网络展示名（中文，写法：名称（代码））。接口与下单仍用原始代码。 */
const NET_NAMES: Record<string, string> = {
  TRC20: "Tron（TRC20）", TRX: "Tron（TRC20）", TRON: "Tron（TRC20）",
  ERC20: "以太坊（ERC20）", ETH: "以太坊（ERC20）", ETHEREUM: "以太坊（ERC20）",
  BEP20: "BNB智能链（BEP20）", BEP20BSC: "BNB智能链（BEP20）", BSC: "BNB智能链（BEP20）", BNB: "BNB智能链（BEP20）", BNBSMARTCHAIN: "BNB智能链（BEP20）",
  ARBITRUM: "Arbitrum One（ARB）", ARBITRUMONE: "Arbitrum One（ARB）", ARB: "Arbitrum One（ARB）",
  SOL: "Solana（SOL）", SOLANA: "Solana（SOL）",
  BTC: "比特币（BTC）", BITCOIN: "比特币（BTC）",
  POLYGON: "Polygon（MATIC）", MATIC: "Polygon（MATIC）", POL: "Polygon（MATIC）",
  TON: "Toncoin（TON）", TONCOIN: "Toncoin（TON）",
  OP: "Optimism（OP）", OPTIMISM: "Optimism（OP）",
  AVAXC: "Avalanche C链（AVAX-C）", AVAXCCHAIN: "Avalanche C链（AVAX-C）", AVALANCHECCHAIN: "Avalanche C链（AVAX-C）", AVALANCHE: "Avalanche C链（AVAX-C）",
  BASE: "Base",
};
export const netLabel = (code?: string | null) => (code ? NET_NAMES[netKey(code)] ?? code : "");

/**
 * 某个网络的最小提现额（全站唯一规则，只认后端，没有任何前端写死的下限）：
 * 该网络接口值 minWithdraw（/wallet/networks 或回退 /wallet/withdraw/networks，须为 > 0）
 * → 该网络的提现配置 cfg.minAmount（须为 > 0，且 cfg 正是这个 asset + network 的配置，避免把别的网络 / 上一个币种的值串过来）
 * → 都没有则返回 undefined（界面显示 "--" / 不展示，不校验最小额，由后端兜底拒绝）。
 * 下拉每一行、「单笔最小提现」行、占位符、温馨提示和数量校验都只调用它，不再各自拼规则，因此不会出现不一致。
 */
export function networkWithdrawMin(
  asset: string,
  network: string | undefined,
  info: { minWithdraw?: unknown } | null | undefined,
  cfg: { asset?: string; network?: string; minAmount?: unknown } | null | undefined,
): string | undefined {
  const cfgMin = cfg && cfg.asset === asset && cfg.network === network ? positiveAmount(cfg.minAmount) : undefined;
  return positiveAmount(info?.minWithdraw) ?? cfgMin;
}

/**
 * 接口金额字符串 → 展示用字符串：仅当它是 > 0 的十进制数时返回（去掉尾零，不经过浮点）；
 * 缺省字段在后端会序列化为 "0" / "0.00000000"，这些以及空值、非数字都返回 undefined（不展示）。
 */
export function positiveAmount(v: unknown): string | undefined {
  if (typeof v !== "string") return undefined;
  const s = v.trim();
  if (!/^\d+(\.\d+)?$/.test(s) || !/[1-9]/.test(s)) return undefined;
  const [i, f = ""] = s.split(".");
  const int = i.replace(/^0+(?=\d)/, "");
  const frac = f.replace(/0+$/, "");
  return frac ? `${int}.${frac}` : int;
}

type NetRow = { asset?: string; network: string; depositEnabled?: boolean; withdrawEnabled?: boolean; minDeposit?: string; minWithdraw?: string; estimatedArrivalMinutes?: number; recommended?: boolean };
type NetRes = { ok?: boolean; source?: string; data?: { networks?: NetRow[] } } | null | undefined;

/** 网络代码 → 本地图标（public/coins/*.svg）；未收录的网络回退为首字母圆标。 */
const NET_ICON: Record<string, string> = {
  BTC: "btc", BITCOIN: "btc", ETH: "eth", ERC20: "eth", ETHEREUM: "eth", TRX: "trx", TRC20: "trx", TRON: "trx",
  BSC: "bnb", BEP20: "bnb", BEP20BSC: "bnb", BNB: "bnb", BNBSMARTCHAIN: "bnb", SOL: "sol", SOLANA: "sol",
  ARBITRUM: "arb", ARBITRUMONE: "arb", ARB: "arb", POLYGON: "matic", MATIC: "matic", POL: "matic",
  OP: "op", OPTIMISM: "op", AVAXC: "avax", AVAXCCHAIN: "avax", AVALANCHECCHAIN: "avax", AVALANCHE: "avax", TON: "ton", TONCOIN: "ton",
};
export const netIcon = (code: string): string | null => NET_ICON[netKey(code)] ?? null;

/** 预计到账分钟数：只认后端给的正数（0 / 缺失 / 非数值 ⇒ undefined，绝不编造默认值）。充值与提现共用。 */
export function etaMinutes(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) && v > 0 ? v : undefined;
}

/** 提现页「温馨提示」里随币种 / 网络变化的一句：最小提现额（networkWithdrawMin 的唯一取值）+ 后端给出的预计到账时间；两者都缺失时返回 undefined（不展示）。 */
export function withdrawTip(asset: string, network: string | undefined, min: string | undefined, eta: number | undefined): string | undefined {
  if (!min && !eta) return undefined;
  const head = `当前选择 ${asset}${network ? ` · ${netLabel(network)}` : ""}：`;
  return head + [min ? `单笔最小提现 ${min} ${asset}` : "", eta ? `预计到账约 ${eta} 分钟` : ""].filter(Boolean).join("，") + "。";
}

/** 网络下拉的一行：附加信息只取后端字段（minDeposit / minWithdraw / estimatedArrivalMinutes / recommended），缺失则不展示，绝不编造。 */
export type NetInfo = { network: string; min?: string; eta?: number; recommended?: boolean };
export function netInfosOf(asset: string, r: NetRes, out = false): NetInfo[] {
  if (r && r.ok && r.source === "api") {
    return (r.data?.networks ?? [])
      .filter((x) => (out ? x.withdrawEnabled !== false : x.depositEnabled !== false) && (!x.asset || x.asset === asset))
      .map((x) => ({
        network: x.network,
        min: positiveAmount(out ? x.minWithdraw : x.minDeposit),
        eta: etaMinutes(x.estimatedArrivalMinutes), // 充值与提现同一份后端字段，提现下拉也要展示
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
