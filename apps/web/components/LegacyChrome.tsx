"use client";
import { initLang } from "@/lib/i18n";
import dynamic from "next/dynamic";
import type { ComponentType } from "react";
import { usePathname } from "next/navigation";
import { Fragment, useEffect, useState } from "react";
import { loadFeatures } from "@/lib/features";
import { installUiDelegates } from "@/lib/ui";

const RV = "main section, .wrap > section, section.center, .trader, .pcard, .two > *, .three > *, details.faq, .dl, .promo";
/** 滚动入场：元素进入视口时淡入上移（仅首屏外元素）。 */
function useReveal(path: string) {
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver((es) => es.forEach((e) => { if (e.isIntersecting) { e.target.classList.add("lb-rv-in"); io.unobserve(e.target); } }), { threshold: 0.12 });
    const scan = () => document.querySelectorAll<HTMLElement>(RV).forEach((el) => {
      if (el.dataset.rv) return; el.dataset.rv = "1";
      if (el.getBoundingClientRect().top < innerHeight) return;
      el.classList.add("lb-rv"); io.observe(el);
    });
    scan();
    const mo = new MutationObserver(scan); mo.observe(document.body, { childList: true, subtree: true });
    return () => { io.disconnect(); mo.disconnect(); };
  }, [path]);
}

// 导航/弹层读取 localStorage（演示登录态、Cookie 提示），仅在客户端渲染
const nav = (k: string) =>
  dynamic(() => import("@/legacy/Nav").then((m) => (m as any)[k] as ComponentType<any>), { ssr: false });
const Header = nav("Header");
const Cookie = nav("Cookie");
const Service = nav("Service");
const DemoToggle = nav("DemoToggle");
const ModalHost = dynamic(() => import("@/features/Kit").then((m) => m.ModalHost), { ssr: false });

/** 全站外框：顶部导航 + Cookie 提示 + 在线客服 + 演示开关（API 文档页隐藏导航）。 */
export default function LegacyChrome({ children }: { children: React.ReactNode }) {
  const p = usePathname();
  useReveal(p);
  useEffect(() => installUiDelegates(), []);
  useEffect(() => initLang(), []);
  // 功能开关变化（/app/features）时重挂载，使入口在「即将上线」与正常路由间切换
  const [fv, setFv] = useState(0);
  useEffect(() => { const h = () => setFv((v) => v + 1); window.addEventListener("lb-features", h); loadFeatures(); return () => window.removeEventListener("lb-features", h); }, []);
  return (
    <>
      <Fragment key={fv}>
        {p !== "/api/docs" && <Header />}
        {children}
      </Fragment>
      <Cookie />
      <Service />
      <DemoToggle />
      <ModalHost />
    </>
  );
}
