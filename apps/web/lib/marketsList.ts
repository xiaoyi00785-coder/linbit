"use client";
import { useEffect, useState } from "react";
import { restBase } from "./market";

/** 前端分类映射（/market/symbols 暂无 tags 字段；后端补 symbols[].tags 后优先用 tags）。 */
export const CATS = ["全部", "公链", "贵金属", "大宗商品", "股票", "Meme", "AI", "DeFi", "Layer 2", "生态"] as const;
const CAT_MAP: Record<string, string[]> = {
  BTC: ["公链"], ETH: ["公链", "生态"], SOL: ["公链", "生态"], BNB: ["公链", "生态"], TRX: ["公链"],
  ADA: ["公链"], AVAX: ["公链"], DOT: ["公链"], SUI: ["公链"], APT: ["公链"], NEAR: ["公链", "AI"], TON: ["公链"],
  XAU: ["贵金属"], XAG: ["贵金属"], PAXG: ["贵金属"], XAUT: ["贵金属"], OIL: ["大宗商品"], WTI: ["大宗商品"],
  DOGE: ["Meme"], SHIB: ["Meme"], PEPE: ["Meme"], WIF: ["Meme"], BONK: ["Meme"], FLOKI: ["Meme"],
  FET: ["AI"], RNDR: ["AI"], TAO: ["AI"], WLD: ["AI"],
  UNI: ["DeFi"], AAVE: ["DeFi"], LINK: ["DeFi"], MKR: ["DeFi"], CRV: ["DeFi"],
  ARB: ["Layer 2"], OP: ["Layer 2"], POL: ["Layer 2"], MATIC: ["Layer 2"], STRK: ["Layer 2"],
};
export function catsOf(base: string, tags?: string[]): string[] {
  return tags?.length ? tags : CAT_MAP[base.toUpperCase()] ?? [];
}

export type FutRow = { symbol: string; base: string; tags?: string[]; last?: string; chg?: string; hi?: string; lo?: string; vol?: string; pp?: number };

/** 合约列表：/futures/market/contracts + /futures/market/ticker（合约无 WS，5s 轮询，页面隐藏时暂停）。 */
export function useFuturesRows(enabled: boolean) {
  const [rows, setRows] = useState<FutRow[] | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let dead = false, timer: ReturnType<typeof setInterval> | undefined;
    const get = (p: string) => fetch(restBase() + p).then((r) => (r.ok ? r.json() : Promise.reject(r.status)));
    let contracts: { symbol: string; baseAsset: string; tags?: string[]; tickSize?: string }[] = [];
    const pp = (t?: string) => (t && t.includes(".") ? t.split(".")[1].replace(/0+$/, "").length || 0 : 2);
    const tick = () => {
      if (document.hidden) return;
      get("/futures/market/ticker").then((j) => {
        if (dead) return;
        const m = new Map<string, any>((j.tickers ?? []).map((t: any) => [t.symbol, t]));
        setRows(contracts.map((c) => {
          const t = m.get(c.symbol);
          return { symbol: c.symbol, base: c.baseAsset, tags: c.tags, pp: pp(c.tickSize), last: t?.lastPrice && Number(t.lastPrice) ? t.lastPrice : t?.markPrice, chg: t?.priceChangePercent24h, hi: t?.highPrice24h, lo: t?.lowPrice24h, vol: t?.turnover24h };
        }));
      }).catch(() => {});
    };
    get("/futures/market/contracts").then((j) => {
      if (dead) return;
      contracts = j.contracts ?? [];
      setRows(contracts.map((c) => ({ symbol: c.symbol, base: c.baseAsset, tags: c.tags, pp: pp(c.tickSize) })));
      tick();
      timer = setInterval(tick, 5_000);
    }).catch(() => !dead && setRows([]));
    const vis = () => !document.hidden && tick();
    document.addEventListener("visibilitychange", vis);
    return () => { dead = true; clearInterval(timer); document.removeEventListener("visibilitychange", vis); };
  }, [enabled]);
  return rows;
}
