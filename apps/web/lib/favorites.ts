"use client";
/** 自选：登录后走 /account/favorites（GET / PUT /{symbol} / DELETE /{symbol}），未登录存 localStorage。
 *  首次登录时把本地自选合并上传（PUT /account/favorites 整体替换）。 */
import { useCallback, useEffect, useState } from "react";
import { rest, rget } from "./rest";
import { useAuthStore } from "./store";
import { toast } from "./ui";
type Mkt = "spot" | "futures";
const LK = (m: Mkt) => "lb_fav_" + m;
const local = (m: Mkt): string[] => { try { return JSON.parse(localStorage.getItem(LK(m)) || "[]"); } catch { return []; } };
const saveLocal = (m: Mkt, s: string[]) => { try { localStorage.setItem(LK(m), JSON.stringify(s)); } catch {} };
export function useFavorites(market: Mkt) {
  const token = useAuthStore((s) => s.accessToken);
  const [fav, setFav] = useState<string[]>([]);
  useEffect(() => {
    const lo = local(market); setFav(lo);
    if (!token) return;
    rget<{ symbols: string[] }>("/account/favorites?market=" + market).then(async (r) => {
      if (!r.ok) return;
      let s = r.data?.symbols || [];
      const add = lo.filter((x) => !s.includes(x));
      if (add.length) { const p = await rest<{ symbols: string[] }>("PUT", "/account/favorites", { market, symbols: [...s, ...add].slice(0, 200) }); if (p.ok) { s = p.data?.symbols || [...s, ...add]; saveLocal(market, []); } }
      setFav(s);
    });
  }, [market, token]);
  const toggle = useCallback(async (sym: string) => {
    const on = fav.includes(sym); const next = on ? fav.filter((x) => x !== sym) : [...fav, sym];
    setFav(next);
    if (!token) { saveLocal(market, next); toast(on ? "已取消自选" : "已加入自选（登录后同步）", "info"); return; }
    const r = await rest<{ symbols: string[] }>(on ? "DELETE" : "PUT", `/account/favorites/${encodeURIComponent(sym)}?market=${market}`);
    if (!r.ok) { setFav(fav); return toast(r.code === "FAVORITES_LIMIT" ? "自选最多 200 个" : r.error || "自选同步失败", "err"); }
    if (r.data?.symbols) setFav(r.data.symbols);
    toast(on ? "已取消自选" : "已加入自选", "ok");
  }, [fav, market, token]);
  return { fav, toggle, isFav: (s: string) => fav.includes(s) };
}
