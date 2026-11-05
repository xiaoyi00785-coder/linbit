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

/** 首页跟单区：真实接口 GET /futures/copy/summary（交易员数 / 跟单者累计收益）+ GET /futures/copy/traders（近 30 日收益率前 3）。
 *  无数据、接口失败时返回空：调用方隐藏人数 / 卡片 / 整块，绝不展示写死的人数或示例交易员。 */
export type HomeTrader = { id: string; name: string; roi: number; followers: number; maxFollowers: number; aum: number };
export type CopyHome = { ready: boolean; traders: number; followerProfit: number; list: HomeTrader[] };
const fin = (v: unknown) => { const n = Number(v); return Number.isFinite(n) ? n : 0; };
export function useCopyHome(): CopyHome {
  const [st, set] = useState<CopyHome>({ ready: false, traders: 0, followerProfit: 0, list: [] });
  useEffect(() => {
    let live = true;
    Promise.all([
      rget<{ traders?: number; followerProfit?: string; maxFollowers?: number }>("/futures/copy/summary"),
      rget<{ traders?: { traderId: string; nickname?: string; roi?: string; followers?: number; maxFollowers?: number; aum?: string }[] }>("/futures/copy/traders?days=30&sort=roi&limit=3"),
    ]).then(([s, t]) => {
      if (!live) return;
      const maxF = fin(s.data?.maxFollowers);
      const list = (t.ok ? t.data?.traders ?? [] : []).slice(0, 3).map((x) => ({ id: x.traderId, name: x.nickname || x.traderId, roi: fin(x.roi) * 100, followers: fin(x.followers), maxFollowers: fin(x.maxFollowers) || maxF, aum: fin(x.aum) }));
      set({ ready: true, traders: s.ok ? fin(s.data?.traders) : 0, followerProfit: s.ok ? fin(s.data?.followerProfit) : 0, list });
    });
    return () => { live = false; };
  }, []);
  return st;
}
