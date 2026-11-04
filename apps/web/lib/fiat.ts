"use client";
/**
 * 法币折算。
 * - 汇率：GET /market/fiat-rates?base=USDT（公开），失败时返回 null，调用方显示 USDT 原值。
 * - 计价货币唯一来源：已登录 → GET /account 的 quoteCurrency（读写走 GET/PUT /account/preferences）；
 *   未登录 → 本地 localStorage "lb_currency"（兼容迁移旧键 "lb_cur"）。
 * 所有折算都通过 useFiat() 读取，不在页面内自行读取 localStorage。
 */
import { useEffect, useState } from "react";
import { rest, rget } from "./rest";
import { useAuthStore } from "./store";
import { getAccount, setMe, useMe } from "./account";
export type FiatRates = { base: string; rates: Record<string, number>; source?: string; updatedAt?: string | null; stale?: boolean };
export type AccountPreferences = { quoteCurrency: string; defaultQuoteCurrency: string; supportedQuoteCurrencies: string[] };
let cache: Promise<FiatRates | null> | null = null;
export const loadFiat = () => (cache ||= rget<FiatRates>("/market/fiat-rates?base=USDT").then((r) => (r.ok && r.data?.rates ? r.data : null)));
export const CUR_SYM: Record<string, string> = { CNY: "¥", USD: "$", EUR: "€", HKD: "HK$", JPY: "¥", KRW: "₩", GBP: "£" };
export const CUR_KEY = "lb_currency";
const FALLBACK = "CNY";

/** 本地（未登录）计价货币 */
export function getLocalCurrency(): string {
  if (typeof localStorage === "undefined") return FALLBACK;
  const v = localStorage.getItem(CUR_KEY);
  if (v) return v;
  const old = localStorage.getItem("lb_cur");
  if (old) { localStorage.setItem(CUR_KEY, old); localStorage.removeItem("lb_cur"); return old; }
  return FALLBACK;
}
/** 已登录时由 /account.quoteCurrency 写入的服务端值 */
let serverCur: string | null = null;
const authed = () => !!useAuthStore.getState().accessToken;
export const getCurrency = () => (authed() ? serverCur ?? FALLBACK : getLocalCurrency());

let prefsP: Promise<AccountPreferences | null> | null = null;
/** GET /account/preferences（登录态缓存一次） */
export const loadPreferences = () =>
  (prefsP ||= rget<AccountPreferences>("/account/preferences").then((r) => (r.ok && r.data ? r.data : null)));

/** 切换计价货币：未登录写本地；已登录 PUT /account/preferences，成功后以服务端返回为准 */
export async function setCurrency(c: string): Promise<{ ok: boolean; error?: string }> {
  // 已登录：只读写 /account/preferences，不落 localStorage；未登录：本地 lb_currency 兜底
  if (!authed()) { localStorage.setItem(CUR_KEY, c); window.dispatchEvent(new Event("lb-filter")); return { ok: true }; }
  const r = await rest<AccountPreferences>("PUT", "/account/preferences", { quoteCurrency: c });
  if (r.ok && r.data?.quoteCurrency) {
    serverCur = r.data.quoteCurrency;
    prefsP = Promise.resolve(r.data);
    getAccount().then((a) => a.ok && a.data && setMe(a.data));
    window.dispatchEvent(new Event("lb-filter"));
    return { ok: true };
  }
  window.dispatchEvent(new Event("lb-filter"));
  return { ok: false, error: r.error || "计价货币保存失败" };
}

export function useFiat() {
  const me = useMe() as (ReturnType<typeof useMe> & { quoteCurrency?: string }) | null;
  const tok = useAuthStore((s) => s.accessToken);
  const [r, setR] = useState<FiatRates | null>(null);
  const [prefs, setPrefs] = useState<AccountPreferences | null>(null);
  const [, bump] = useState(0);
  if (tok && me?.quoteCurrency) serverCur = me.quoteCurrency;
  if (!tok) serverCur = null;
  useEffect(() => {
    loadFiat().then(setR);
    const h = () => bump((x) => x + 1);
    const st = (e: StorageEvent) => { if (e.key === CUR_KEY) h(); };
    window.addEventListener("lb-filter", h); window.addEventListener("storage", st);
    return () => { window.removeEventListener("lb-filter", h); window.removeEventListener("storage", st); };
  }, []);
  useEffect(() => {
    if (!tok) { prefsP = null; setPrefs(null); return; }
    loadPreferences().then((p) => {
      setPrefs(p);
      if (p?.quoteCurrency && !serverCur) { serverCur = p.quoteCurrency; bump((x) => x + 1); }
    });
  }, [tok]);
  const cur = tok ? serverCur ?? prefs?.defaultQuoteCurrency ?? FALLBACK : getLocalCurrency();
  const opts = prefs?.supportedQuoteCurrencies?.length ? prefs.supportedQuoteCurrencies : r ? Object.keys(r.rates) : ["CNY", "USD", "EUR", "HKD"];
  return { rates: r, cur, opts, sym: CUR_SYM[cur] ?? "", conv: (usdt: number) => (r && r.rates[cur] ? usdt * r.rates[cur] : null) };
}
