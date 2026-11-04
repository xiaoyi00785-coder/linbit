"use client";
/** 前端展示用的“接口来源”数值：费率 / 理财最高 APR / 现货可买币种。加载中返回 null，调用方显示骨架或隐藏，不写死数字。 */
import { useEffect, useMemo, useState } from "react";
import { useContracts } from "@/features/TradeKit";
import { useSymbols } from "./market";
import { rget } from "./rest";

/** 合约费率：/futures/market/contracts 的 maker/takerFeeRate，取全部合约的最低值（“低至”文案用） */
export function useFutFeeRates(): { maker: number; taker: number } | null {
  const cs = useContracts();
  return useMemo(() => {
    const m = cs.map((c) => Number(c.makerFeeRate)).filter(Number.isFinite);
    const t = cs.map((c) => Number(c.takerFeeRate)).filter(Number.isFinite);
    return m.length && t.length ? { maker: Math.min(...m), taker: Math.min(...t) } : null;
  }, [cs]);
}
export const feePct = (r: number) => `${+(r * 100).toFixed(3)}%`;

type EarnProduct = { kind?: string; apr?: string; bonusApr?: string };
const aprP: Record<string, Promise<number | null>> = {};
/** 理财最高 APR：GET /earn/products?kind=…，取 max(apr)。失败/无产品 → null（调用方隐藏标签） */
export function useMaxApr(kind: "fixed" | "flexible" | "" = "fixed"): number | null {
  const [v, setV] = useState<number | null>(null);
  useEffect(() => {
    let live = true;
    (aprP[kind] ??= rget<{ items?: EarnProduct[] }>("/earn/products" + (kind ? "?kind=" + kind : "")).then((r) => {
      const l = (r.ok && r.source === "api" ? r.data?.items ?? [] : []).map((p) => Number(p.apr)).filter((x) => Number.isFinite(x) && x > 0);
      return l.length ? Math.max(...l) : null;
    })).then((x) => live && setV(x));
    return () => { live = false; };
  }, [kind]);
  return v;
}
export const aprPct = (r: number) => `${+(r * 100).toFixed(2)}%`;

const PREF = ["BTC", "ETH", "SOL", "XRP", "DOGE", "BNB"];
/** 现货实际存在的 USDT 交易对基础币（/market/symbols），常用币优先，最多 n 个 */
export function useSpotBases(n = 5): string[] {
  const m = useSymbols();
  return useMemo(() => {
    const b = Object.values(m).filter((x) => x.quote === "USDT").map((x) => x.base);
    return [...PREF.filter((x) => b.includes(x)), ...b.filter((x) => !PREF.includes(x))].slice(0, n);
  }, [m, n]);
}
