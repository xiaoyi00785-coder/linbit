"use client";
/** 法币汇率 GET /market/fiat-rates?base=USDT（公开）。失败时返回 null，调用方显示 USDT 原值。 */
import { useEffect, useState } from "react";
import { rget } from "./rest";
export type FiatRates = { base: string; rates: Record<string, number>; source?: string; updatedAt?: string | null; stale?: boolean };
let cache: Promise<FiatRates | null> | null = null;
export const loadFiat = () => (cache ||= rget<FiatRates>("/market/fiat-rates?base=USDT").then((r) => (r.ok && r.data?.rates ? r.data : null)));
export const CUR_SYM: Record<string, string> = { CNY: "¥", USD: "$", EUR: "€", HKD: "HK$", JPY: "¥", KRW: "₩", GBP: "£" };
/** 计价货币唯一存储：localStorage "lb_currency"（顶栏地球弹窗与 /setting/settings 共用；兼容迁移旧键 "lb_cur"） */
export const CUR_KEY = "lb_currency";
export function getCurrency(): string {
  if (typeof localStorage === "undefined") return "CNY";
  const v = localStorage.getItem(CUR_KEY);
  if (v) return v;
  const old = localStorage.getItem("lb_cur");
  if (old) { localStorage.setItem(CUR_KEY, old); localStorage.removeItem("lb_cur"); return old; }
  return "CNY";
}
export function setCurrency(c: string) {
  localStorage.setItem(CUR_KEY, c);
  window.dispatchEvent(new Event("lb-filter"));
}
export function useFiat() {
  const [r, setR] = useState<FiatRates | null>(null);
  const [cur, setCur] = useState(getCurrency);
  useEffect(() => {
    loadFiat().then(setR);
    const h = () => setCur(getCurrency());
    const st = (e: StorageEvent) => { if (e.key === CUR_KEY) h(); };
    window.addEventListener("lb-filter", h); window.addEventListener("storage", st);
    return () => { window.removeEventListener("lb-filter", h); window.removeEventListener("storage", st); };
  }, []);
  return { rates: r, cur, opts: r ? Object.keys(r.rates) : ["CNY", "USD", "EUR", "HKD"], conv: (usdt: number) => (r && r.rates[cur] ? usdt * r.rates[cur] : null) };
}
