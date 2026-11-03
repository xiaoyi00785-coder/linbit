"use client";

import dynamic from "next/dynamic";

// 现货交易页依赖图表库与浏览器环境，仅在客户端渲染（ssr:false）。
const TradeView = dynamic(() => import("./TradeView"), {
  ssr: false,
  loading: () => (
    <div className="grid min-h-[60vh] place-items-center text-lb-text-3">
      正在加载交易终端…
    </div>
  ),
});

export default function TradePage({
  params,
}: {
  params: { symbol: string };
}) {
  return <TradeView symbol={decodeURIComponent(params.symbol).toUpperCase()} />;
}
