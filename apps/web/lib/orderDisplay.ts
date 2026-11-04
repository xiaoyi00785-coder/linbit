// Display rules shared by futures positions and spot order lists.
// iOS should mirror these (see mobile Positions / Orders views).

const QUOTES = ["USDT", "USDC", "BTC", "ETH"];
export function quoteOf(symbol: string): string {
  return QUOTES.find((q) => symbol.endsWith(q) && symbol.length > q.length) ?? "";
}

type QtyOrder = { symbol: string; quantity?: string | null; quoteOrderQty?: string | null };

/** 委托数量: market buy placed by amount (quoteOrderQty) has quantity 0 — show the amount instead. */
export function orderQtyText(o: QtyOrder): string {
  if (o.quoteOrderQty && +o.quoteOrderQty > 0 && !(+(o.quantity ?? 0) > 0)) {
    return `按金额 ${o.quoteOrderQty} ${quoteOf(o.symbol)}`.trim();
  }
  return o.quantity ?? "--";
}

/** 委托金额: limit = price*qty; by-amount market buy = the budget; else unknown. */
export function orderAmountText(o: QtyOrder & { price?: string | null }, fmt: (v: number) => string): string {
  if (o.price && +o.price > 0 && o.quantity) return fmt(+o.price * +o.quantity);
  if (o.quoteOrderQty && +o.quoteOrderQty > 0) return fmt(+o.quoteOrderQty);
  return "--";
}

/**
 * Liquidation price display. The API value is mathematically exact, but in cross
 * mode the whole account equity backs the position, so a small short can get a
 * liq price far beyond any realistic market (e.g. 19,995,103 for BTC at 76,676).
 * Show "--" when liq <= 0, or when it is beyond 10x mark (short) / < mark/10 (long).
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
