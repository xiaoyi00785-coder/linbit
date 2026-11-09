// 合约持仓与现货委托共用的展示规则。iOS 持仓 / 订单页应对齐同一口径。

const QUOTES = ["USDT", "USDC", "BTC", "ETH"];
export function quoteOf(symbol: string): string {
  return QUOTES.find((q) => symbol.endsWith(q) && symbol.length > q.length) ?? "";
}

type QtyOrder = { symbol: string; quantity?: string | null; quoteOrderQty?: string | null };

/** 委托数量：按计价币金额下的市价买单 quantity 为 0，改显示 quoteOrderQty。 */
export function orderQtyText(o: QtyOrder): string {
  if (o.quoteOrderQty && +o.quoteOrderQty > 0 && !(+(o.quantity ?? 0) > 0)) {
    return `按金额 ${o.quoteOrderQty} ${quoteOf(o.symbol)}`.trim();
  }
  return o.quantity ?? "--";
}

/** 委托金额：限价为价格×数量；按金额的市价买为预算；否则算不出，返回 "--"。 */
export function orderAmountText(o: QtyOrder & { price?: string | null }, fmt: (v: number) => string): string {
  if (o.price && +o.price > 0 && o.quantity) return fmt(+o.price * +o.quantity);
  if (o.quoteOrderQty && +o.quoteOrderQty > 0) return fmt(+o.quoteOrderQty);
  return "--";
}

/**
 * 强平价展示。接口值是算出来的精确数，但全仓时账户权益给仓位兜底，
 * 小额空仓的强平价可以远超市价。liq ≤ 0，或空仓超过标记价的 LIQ_SANITY 倍、
 * 多仓低于标记价的 1/LIQ_SANITY，返回 null，调用方显示 "--"。
 */
export const LIQ_SANITY = 10;
export function liqText(liq: string | number | null | undefined, mark: string | number | null | undefined, side: string): string | null {
  const l = +(liq ?? 0), m = +(mark ?? 0);
  if (!(l > 0) || !isFinite(l)) return null;
  if (m > 0) {
    const short = /short|sell/i.test(side);
    if (short && l > m * LIQ_SANITY) return null;
    if (!short && l < m / LIQ_SANITY) return null;
  }
  return String(liq);
}
