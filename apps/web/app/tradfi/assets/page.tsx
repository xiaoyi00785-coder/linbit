"use client";
import dynamic from "next/dynamic";
import { Suspense } from "react";
import { Gate } from "@/features/Flagged";

const View = dynamic(() => import("@/features/Flagged").then((m) => m.TradFi), { ssr: false });

/** 受功能开关控制：关闭（或开关接口失败）⇒ /coming-soon/tradfi */
export default function Page() {
  return <Gate any={["tokenizedStocks","metals","tradfi"]} slug="tradfi"><Suspense><View /></Suspense></Gate>;
}
