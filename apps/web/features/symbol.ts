/** 交易对路径参数规范化：大小写与 _ - / 分隔符均可（btc_usdt → BTCUSDT）。服务端与客户端共用。 */
export const normSymbol = (s?: string | null) => (s || "").toUpperCase().replace(/[_\-/]/g, "");
export const SYMBOL_RE = /^[A-Z0-9]{2,24}$/;
