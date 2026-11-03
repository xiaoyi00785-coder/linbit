"use client";
// 币种图标：本地打包的真实 logo（public/coins，来源见 public/coins/LICENSE.txt），未知币种回退字母徽标
import { useEffect, useState } from "react";
const COL: Record<string, string> = {};
/** 代码别名 → 图标文件名 */
const ALIAS: Record<string, string> = { GOLD: "xau", XAUUSD: "xau", SILVER: "xag", XAGUSD: "xag", USOIL: "wti", CL: "wti", UKOIL: "brent", NATGAS: "ngas", GOOG: "googl", WBTC: "wbtc", MATIC: "pol", "1000PEPE": "pepe", "1000SHIB": "shib" };
/** 交易对 / 资产代码 → 基础资产代码（BTCUSDT、BTC/USDT、BTC-USDT-SWAP、btc → BTC；USDT 本身保持不变） */
export function baseOf(s: string): string {
  let x = (s || "?").toUpperCase().trim().split(/[\/\-_ ]/)[0];
  const q = x.match(/^(.+?)(USDT|USDC|USD|BUSD)$/);
  if (q && q[1].length >= 2) x = q[1];
  return x;
}
export function coinSrc(s: string) { const b = baseOf(s); return `/coins/${ALIAS[b] || b.toLowerCase()}.svg`; }
export function CoinIcon({ s, size = 20, col, className = "" }: { s: string; size?: number; col?: string; className?: string }) {
  const sym = baseOf(s);
  const [bad, setBad] = useState(false);
  useEffect(() => setBad(false), [sym]);
  const st = { width: size, height: size, minWidth: size } as const;
  if (bad) return <i className={"ico " + className} style={{ ...st, background: col || COL[sym] || "#37393a", fontSize: size * 0.5 }}>{sym[0]}</i>;
  return <img className={"ico cimg " + className} src={coinSrc(sym)} alt={sym} width={size} height={size} style={st} loading="lazy" onError={() => setBad(true)} />;
}
