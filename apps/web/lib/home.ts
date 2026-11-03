"use client";
/** 首页运营数据：/home/placements（carousel、promo_bar）+ /home/newcomer。接口不可用时调用方保留内置文案。 */
import { useEffect, useState } from "react";
import { rget } from "./rest";
export type Placement = { id: number; slot: string; title: string; subtitle?: string; imageUrl?: string; imageDarkUrl?: string; linkUrl?: string; countdownEndAt?: string | null };
export type Newcomer = { available: boolean; total?: string; asset?: string; link?: string; cta?: string; degraded?: boolean };
export function useHome() {
  const [slots, setSlots] = useState<Record<string, Placement[]> | null>(null);
  const [nc, setNc] = useState<Newcomer | null>(null);
  useEffect(() => {
    rget<{ slots: Record<string, Placement[]> }>("/home/placements?slots=carousel,promo_bar&platform=web&locale=zh-CN").then((r) => r.ok && r.data?.slots && setSlots(r.data.slots));
    rget<Newcomer>("/home/newcomer").then((r) => r.ok && r.data && setNc(r.data));
  }, []);
  return { carousel: slots?.carousel || [], bar: slots?.promo_bar?.[0] || null, newcomer: nc?.available ? nc : null };
}
