"use client";
/** 法币汇率 GET /market/fiat-rates?base=USDT（公开）。失败时返回 null，调用方显示 USDT 原值。 */
import { useEffect, useState } from "react";
import { rget } from "./rest";
export type FiatRates = { base: string; rates: Record<string, number>; source?: string; updatedAt?: string | null; stale?: boolean };
let cache: Promise<FiatRates | null> | null = null;
export const loadFiat = () => (cache ||= rget<FiatRates>("/market/fiat-rates?base=USDT").then((r) => (r.ok && r.data?.rates ? r.data : null)));
export const CUR_SYM: Record<string, string> = { CNY: "¥", USD: "$", EUR: "€", HKD: "HK$", JPY: "¥", KRW: "₩", GBP: "£" };
export function useFiat() {
  const [r, setR] = useState<FiatRates | null>(null);
  const [cur, setCur] = useState(() => (typeof localStorage !== "undefined" && localStorage.getItem("lb_currency")) || "CNY");
  useEffect(() => { loadFiat().then(setR); const h = () => setCur(localStorage.getItem("lb_currency") || "CNY"); window.addEventListener("lb-filter", h); return () => window.removeEventListener("lb-filter", h); }, []);
  return { rates: r, cur, opts: r ? Object.keys(r.rates) : ["CNY", "USD", "EUR", "HKD"], conv: (usdt: number) => (r && r.rates[cur] ? usdt * r.rates[cur] : null) };
}
