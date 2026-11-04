"use client";
import dynamic from "next/dynamic";
import { Suspense } from "react";
import { Gate } from "@/features/Flagged";

const View = dynamic(() => import("@/features/SupportTickets").then((m) => m.SupportTickets), { ssr: false });

/** 站内客服工单（supportTickets 开关，默认开启）：关闭 ⇒ /coming-soon/support-tickets */
export default function Page() {
  return <Gate any={["supportTickets"]} slug="support-tickets"><Suspense><View /></Suspense></Gate>;
}
