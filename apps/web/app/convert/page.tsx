"use client";
// 原型页面读取 localStorage（演示登录态），在客户端渲染
import dynamic from "next/dynamic";
import { useEffect, useState, type ComponentType } from "react";
import { useRouter } from "next/navigation";
import { isOn, loadFeatures } from "@/lib/features";

const View = dynamic(() => import("@/legacy/Pages").then((m) => m.Convert as ComponentType<any>), { ssr: false });

/** 闪兑入口受 features.convert 控制：关闭（或开关接口失败）⇒ /coming-soon/convert */
export default function Page() {
  const r = useRouter(); const [ok, setOk] = useState(false);
  useEffect(() => { loadFeatures().then(() => (isOn("convert") ? setOk(true) : r.replace("/coming-soon/convert"))); }, [r]);
  return ok ? <View /> : null;
}
