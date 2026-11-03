"use client";
// 行情/交易页依赖 window、localStorage 与图表，仅在客户端渲染
import dynamic from "next/dynamic";
import type { ComponentType } from "react";

const View = dynamic(() => import("@/legacy/App").then((m) => m.Trade as ComponentType<{ fut?: boolean; symbol?: string }>), { ssr: false });

export function TradeView({ fut, symbol }: { fut?: boolean; symbol?: string }) {
  return <View fut={fut} symbol={symbol} />;
}
