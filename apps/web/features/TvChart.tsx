"use client";
import { useEffect, useId, useRef, useState } from "react";

type TvWidget = { remove?: () => void };
declare global {
  interface Window {
    TradingView?: { widget: new (opts: Record<string, unknown>) => TvWidget };
  }
}

/** 公开行情图。合约用 Binance U 本位永续，现货用 Binance 现货。价格来自 TradingView，不跟本站盘口对齐。 */
function tvSymbol(symbol: string, fut: boolean) {
  const s = symbol.toUpperCase().replace(/[^A-Z0-9]/g, "");
  return fut ? `BINANCE:${s}.P` : `BINANCE:${s}`;
}

export function TvChart({ symbol, fut }: { symbol: string; fut?: boolean }) {
  const rid = useId().replace(/:/g, "");
  const host = useRef<HTMLDivElement>(null);
  const widget = useRef<TvWidget | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    let dead = false;
    const mount = () => {
      if (dead || !host.current || !window.TradingView) return;
      try { widget.current?.remove?.(); } catch { /* 重复挂载时旧实例可能已销毁 */ }
      host.current.innerHTML = "";
      const el = document.createElement("div");
      el.id = "tv_" + rid;
      el.style.cssText = "height:100%;width:100%";
      host.current.appendChild(el);
      widget.current = new window.TradingView.widget({
        autosize: true,
        symbol: tvSymbol(symbol, !!fut),
        interval: "1",
        timezone: "Asia/Shanghai",
        theme: "dark",
        style: "1",
        locale: "zh_CN",
        toolbar_bg: "#070808",
        enable_publishing: false,
        hide_top_toolbar: false,
        hide_legend: false,
        hide_side_toolbar: false,
        allow_symbol_change: false,
        save_image: false,
        container_id: el.id,
        backgroundColor: "#070808",
        gridColor: "rgba(29, 29, 29, 1)",
        studies: ["Volume@tv-basicstudies"],
        withdateranges: true,
      });
      setErr("");
    };
    if (window.TradingView) mount();
    else {
      const src = "https://s3.tradingview.com/tv.js";
      let s = document.querySelector<HTMLScriptElement>(`script[src="${src}"]`);
      if (!s) {
        s = document.createElement("script");
        s.src = src;
        s.async = true;
        s.onerror = () => { if (!dead) setErr("TradingView 图表加载失败"); };
        document.body.appendChild(s);
      }
      s.addEventListener("load", mount);
      return () => {
        dead = true;
        s?.removeEventListener("load", mount);
        try { widget.current?.remove?.(); } catch { /* */ }
        widget.current = null;
      };
    }
    return () => {
      dead = true;
      try { widget.current?.remove?.(); } catch { /* */ }
      widget.current = null;
    };
  }, [symbol, fut, rid]);

  return <div ref={host} className="tvchart">{err && <div className="tvchart-err">{err}</div>}</div>;
}
