"use client";
import { useEffect, useState } from "react";
import { isOn, loadFeatures, type FeatureKey } from "./features";
/** 运行时功能开关（/app/features）。首屏（含 SSR 与水合）一律按"关闭"渲染，挂载后再读取运行时开关，
 *  避免服务端与客户端渲染的链接 / 「即将上线」角标不一致（水合不会修正不一致的 href）。 */
export function useFeatureOn(): (k: FeatureKey) => boolean {
  const [ready, setReady] = useState(false); const [, setV] = useState(0);
  useEffect(() => {
    setReady(true);
    const h = () => setV((v) => v + 1);
    window.addEventListener("lb-features", h); loadFeatures().then(h);
    return () => window.removeEventListener("lb-features", h);
  }, []);
  return (k) => ready && isOn(k);
}
