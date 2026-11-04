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
/** chip 名称 ↔ 后端分类 id（GET /market/categories、symbols[].tags 使用 id） */
export const CAT_ID: Record<string, string> = { 公链: "layer1", 贵金属: "metals", 大宗商品: "commodities", 股票: "stocks", Meme: "meme", AI: "ai", DeFi: "defi", "Layer 2": "layer2", 生态: "ecosystem" };
export type CatMarket = "spot" | "futures";
const catNames: Partial<Record<CatMarket, string[]>> = {};
const catP: Partial<Record<CatMarket, Promise<Record<string, string[]> | null>>> = {};
/** GET /market/categories?market=&nonEmpty=true → { id: symbols[] }（服务端已剔除空分类，如合约下的 stocks）；失败时返回 null（退回 tags，再退回本地映射）。 */
export function loadCategories(market: CatMarket = "spot") {
  return (catP[market] ??= fetch(restBase() + `/market/categories?market=${market}&nonEmpty=true`)
    .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
    .then((l: { id: string; name?: string; symbols: string[] }[]) => {
      if (!Array.isArray(l)) return null;
      const ne = l.filter((c) => (c.symbols?.length ?? 0) > 0); // 双保险：前端也隐藏空分类
      catNames[market] = ne.map((c) => { const nm = c.name || c.id; CAT_ID[nm] = c.id; return nm; });
      return Object.fromEntries(ne.map((c) => [c.id, c.symbols ?? []]));
    })
    .catch(() => (delete catP[market], null)));
}
/** chip 列表：后端返回什么就渲染什么（前置“全部”）；接口不可用时用本地 CATS（合约下不含“股票”）。 */
export function useCatChips(market: CatMarket = "spot"): string[] {
  const [n, setN] = useState<string[] | null>(catNames[market] ?? null);
  useEffect(() => { setN(catNames[market] ?? null); void loadCategories(market).then(() => setN(catNames[market] ?? null)); }, [market]);
  return n ? ["全部", ...n] : market === "futures" ? CATS.filter((c) => c !== "股票") : [...CATS];
}
export function useCategories(market: CatMarket = "spot") {
  const [c, setC] = useState<Record<string, string[]> | null>(null);
  useEffect(() => { setC(null); void loadCategories(market).then(setC); }, [market]);
  return c;
}
/** 判断交易对是否属于某 chip：categories 接口 → symbols[].tags → 本地映射 */
export function inCat(chip: string, symbol: string, base: string, tags: string[] | undefined, cats: Record<string, string[]> | null): boolean {
  if (chip === "全部") return true;
  const id = CAT_ID[chip];
  if (cats && id in cats) return cats[id].includes(symbol);
  if (tags?.length) return tags.includes(id) || tags.includes(chip);
  return (CAT_MAP[base.toUpperCase()] ?? []).includes(chip);
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
          return { symbol: c.symbol, base: c.baseAsset, tags: c.tags, pp: pp(c.tickSize), last: t?.lastPrice && Number(t.lastPrice) ? t.lastPrice : t?.markPrice, chg: t?.priceChangePercent ?? t?.priceChangePercent24h, hi: t?.high ?? t?.highPrice24h, lo: t?.low ?? t?.lowPrice24h, vol: t?.quoteVolume ?? t?.turnover24h };
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

/**
 * 服务端排序：GET /market/ticker?sort=symbol|lastPrice|priceChangePercent|high|low|quoteVolume|baseVolume（别名 gainers/losers/volume）&order=asc|desc&limit=&category=&symbols=
 * （后端 fix/acceptance-r3-clean 新增）。返回排好序的 symbol 列表；接口不支持/失败时返回 null，调用方回退本地排序。
 */
export type RankSort = "gainers" | "losers" | "volume" | "symbol" | "lastPrice" | "priceChangePercent" | "high" | "low" | "quoteVolume" | "baseVolume";
export function useRank(sort: RankSort | null, opt: { limit?: number; category?: string; symbols?: string[]; order?: "asc" | "desc" } = {}) {
  const [syms, setSyms] = useState<string[] | null>(null);
  const key = [sort, opt.order, opt.limit, opt.category, opt.symbols?.join(",")].join("|");
  useEffect(() => {
    if (!sort) return setSyms(null);
    let dead = false;
    const q = new URLSearchParams({ sort });
    if (opt.order) q.set("order", opt.order);
    if (opt.limit) q.set("limit", String(opt.limit));
    if (opt.category) q.set("category", opt.category);
    if (opt.symbols?.length) q.set("symbols", opt.symbols.join(","));
    const load = () =>
      fetch(restBase() + "/market/ticker?" + q)
        .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
        .then((l: { symbol: string }[]) => !dead && setSyms(Array.isArray(l) ? l.map((x) => x.symbol) : null))
        .catch(() => !dead && setSyms(null));
    load();
    const t = setInterval(() => document.visibilityState === "visible" && load(), 10000);
    return () => { dead = true; clearInterval(t); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return syms;
}
/** 按服务端顺序重排本地行（行内价格仍取实时 WS）；未在服务端结果中的行丢弃。syms 为 null 时返回 null。 */
export function byRank<T extends { symbol: string }>(rows: T[], syms: string[] | null, reverse = false): T[] | null {
  if (!syms) return null;
  const m = new Map(rows.map((r) => [r.symbol, r]));
  const out = syms.map((s) => m.get(s)).filter(Boolean) as T[];
  return reverse ? out.reverse() : out;
}
