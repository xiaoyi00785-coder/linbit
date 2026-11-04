"use client";
/** 首页运营数据：/home/placements（carousel、promo_bar）+ /home/newcomer。接口不可用时调用方保留内置文案。 */
import { useEffect, useState } from "react";
import { rget } from "./rest";
export type Placement = { id: number; slot: string; title: string; subtitle?: string; imageUrl?: string; imageDarkUrl?: string; linkUrl?: string; countdownEndAt?: string | null };
export type Newcomer = { available: boolean; total?: string; asset?: string; link?: string; cta?: string; degraded?: boolean };
export function useHome() {
  const [slots, setSlots] = useState<Record<string, Placement[]> | null>(null);
  const [nc, setNc] = useState<Newcomer | null>(null);
  // 新人礼包金额：/home/newcomer 优先，/growth/config.newcomer 兜底；两者返回前 ncReady=false，调用方展示骨架
  const [ncReady, setReady] = useState(false);
  useEffect(() => {
    rget<{ slots: Record<string, Placement[]> }>("/home/placements?slots=carousel,promo_bar&platform=web&locale=zh-CN").then((r) => r.ok && r.data?.slots && setSlots(r.data.slots));
    Promise.all([
      rget<Newcomer>("/home/newcomer"),
      rget<{ payoutAsset?: string; newcomer?: { available?: boolean; total?: string } }>("/growth/config"),
    ]).then(([h, g]) => {
      if (h.ok && h.data) setNc(h.data);
      else if (g.ok && g.data?.newcomer?.total) setNc({ available: g.data.newcomer.available !== false, total: g.data.newcomer.total, asset: g.data.payoutAsset || "USDT" });
      setReady(true);
    });
  }, []);
  return { carousel: slots?.carousel || [], bar: slots?.promo_bar?.[0] || null, newcomer: nc?.available ? nc : null, ncReady };
}
