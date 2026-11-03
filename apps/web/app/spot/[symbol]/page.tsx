import { notFound } from "next/navigation";
import { TradeView } from "@/legacy/TradeView";
import { normSymbol, SYMBOL_RE } from "@/features/symbol";

// /spot/BTCUSDT（也接受 btc_usdt、BTC-USDT）。格式非法返回 404；格式合法但不存在的交易对由页面回退到默认交易对。
export default function Page({ params }: { params: { symbol: string } }) {
  const s = normSymbol(decodeURIComponent(params.symbol));
  if (!SYMBOL_RE.test(s)) notFound();
  return <TradeView symbol={s} />;
}
