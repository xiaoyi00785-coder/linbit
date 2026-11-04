"use client";
// 原型页面读取 localStorage（演示登录态），在客户端渲染
import dynamic from "next/dynamic";
import type { ComponentType } from "react";
import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";

const View = dynamic(() => import("@/legacy/Pages2").then((m) => m.EarnHub as ComponentType<any>), { ssr: false });
/** EarnHub 支持的子页；其余（如旧链接 /earn/wealth）回到理财总览，避免空白页 */
const TABS = new Set(["vip", "loan", "bao", "simple", "onchain"]);

export default function Page() {
  const { tab } = useParams<{ tab: string }>();
  const router = useRouter();
  const ok = TABS.has(String(tab));
  useEffect(() => { if (!ok) router.replace("/earn"); }, [ok, router]);
  return ok ? <View /> : null;
}
