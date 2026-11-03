"use client";
// 原型页面读取 localStorage（演示登录态），在客户端渲染
import dynamic from "next/dynamic";
import type { ComponentType } from "react";

const View = dynamic(() => import("@/legacy/Pages3").then((m) => m.Records as ComponentType<any>), { ssr: false });

export default function Page() {
  return <View />;
}
