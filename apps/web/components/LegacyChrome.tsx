"use client";
import dynamic from "next/dynamic";
import type { ComponentType } from "react";
import { usePathname } from "next/navigation";

// 导航/弹层读取 localStorage（演示登录态、Cookie 提示），仅在客户端渲染
const nav = (k: string) =>
  dynamic(() => import("@/legacy/Nav").then((m) => (m as any)[k] as ComponentType<any>), { ssr: false });
const Header = nav("Header");
const Cookie = nav("Cookie");
const Service = nav("Service");
const DemoToggle = nav("DemoToggle");

/** 全站外框：顶部导航 + Cookie 提示 + 在线客服 + 演示开关（API 文档页隐藏导航）。 */
export default function LegacyChrome({ children }: { children: React.ReactNode }) {
  const p = usePathname();
  return (
    <>
      {p !== "/api/docs" && <Header />}
      {children}
      <Cookie />
      <Service />
      <DemoToggle />
    </>
  );
}
