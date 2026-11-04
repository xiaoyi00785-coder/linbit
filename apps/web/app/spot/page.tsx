"use client";
// 行情/交易页依赖 window、localStorage 与图表，仅在客户端渲染
import dynamic from "next/dynamic";
import type { ComponentType } from "react";
import { useState } from "react";
import { prefetchMarket } from "@/lib/market";

const View = dynamic(() => import("@/legacy/App").then((m) => m.Trade as ComponentType<any>), { ssr: false });

export default function Page() {
  useState(() => { if (typeof window !== "undefined") prefetchMarket(new URLSearchParams(window.location.search).get("symbol")?.toUpperCase().replace(/[_\-/]/g, "") || "BTCUSDT"); });
  return <View />;
}
