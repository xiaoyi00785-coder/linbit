"use client";
// 行情/交易页依赖 window、localStorage 与图表，仅在客户端渲染
import dynamic from "next/dynamic";
import type { ComponentType } from "react";
import { useState } from "react";
import { prefetchMarket } from "@/lib/market";

const View = dynamic(() => import("@/legacy/App").then((m) => m.Trade as ComponentType<{ fut?: boolean; symbol?: string }>), { ssr: false });

export function TradeView({ fut, symbol }: { fut?: boolean; symbol?: string }) {
  // 现货首屏：交易页代码块加载期间即并行预取 ticker + 深度（App 组件挂载后直接命中缓存/在途请求）
  useState(() => { if (!fut && typeof window !== "undefined") prefetchMarket(symbol || new URLSearchParams(window.location.search).get("symbol")?.toUpperCase() || "BTCUSDT"); });
  return <View fut={fut} symbol={symbol} />;
}
