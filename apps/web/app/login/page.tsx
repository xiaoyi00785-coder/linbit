"use client";
// 新版页面（features/），依赖 localStorage / 浏览器 API，在客户端渲染
import dynamic from "next/dynamic";
import { AuthSkel } from "@/components/AuthSkel";

const View = dynamic(() => import("@/features/Auth").then((m) => m.Login), { ssr: false, loading: () => <AuthSkel /> });

export default function Page() {
  return <View />;
}
