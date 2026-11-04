"use client";
/** 前端展示用的“接口来源”数值：费率 / 理财最高 APR / 现货可买币种。加载中返回 null，调用方显示骨架或隐藏，不写死数字。 */
import { useEffect, useMemo, useState } from "react";
import { useSymbols } from "./market";
import { rget } from "./rest";
import { maxApr, type EarnAprFields } from "./earn";

type FeePair = { maker?: string; taker?: string };
let feeP: Promise<{ spot?: FeePair; futures?: FeePair | null } | null> | null = null;
const num = (p?: FeePair | null) => { const m = Number(p?.maker), t = Number(p?.taker); return p && p.maker != null && p.taker != null && Number.isFinite(m) && Number.isFinite(t) ? { maker: m, taker: t } : null; };
/** 平台默认费率：GET /spot/fee-schedule（小数）。kind=futures 时 futures 为 null（合约服务不可用）→ 返回 null，调用方显示“以合约详情为准” */
export function useFeeSchedule(kind: "spot" | "futures" = "futures"): { maker: number; taker: number } | null {
  const [v, setV] = useState<{ maker: number; taker: number } | null>(null);
  useEffect(() => {
    let live = true;
    // 失败（如旧网关对游客 401）不缓存，下次挂载/登录后重试
    (feeP ??= rget<{ spot?: FeePair; futures?: FeePair | null }>("/spot/fee-schedule").then((r) => { const d = r.ok && r.source === "api" ? r.data ?? null : null; if (!d) feeP = null; return d; })).then((d) => live && setV(num(d?.[kind])));
    return () => { live = false; };
  }, [kind]);
  return v;
}
/** 兼容旧调用：合约默认费率（来自 /spot/fee-schedule.futures） */
export const useFutFeeRates = () => useFeeSchedule("futures");
export const feePct = (r: number) => `${+(r * 100).toFixed(3)}%`;

type EarnProduct = EarnAprFields & { kind?: string };
const aprP: Record<string, Promise<number | null>> = {};
/** 理财最高 APR：GET /earn/products?kind=…，取 max(apr)。失败/无产品 → null（调用方隐藏标签） */
export function useMaxApr(kind: "fixed" | "flexible" | "" = "fixed"): number | null {
  const [v, setV] = useState<number | null>(null);
  useEffect(() => {
    let live = true;
    (aprP[kind] ??= rget<{ items?: EarnProduct[] }>("/earn/products" + (kind ? "?kind=" + kind : "")).then((r) => {
      return maxApr(r.ok && r.source === "api" ? r.data?.items ?? [] : []);
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
