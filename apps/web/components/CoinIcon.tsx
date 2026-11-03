"use client";
// 币种图标：本地打包的真实 logo（public/coins，来源见 public/coins/LICENSE.txt），未知币种回退字母徽标
import { useState } from "react";
const COL: Record<string, string> = {};
export function CoinIcon({ s, size = 20, col, className = "" }: { s: string; size?: number; col?: string; className?: string }) {
  const sym = (s || "?").replace(/USDT$|\/.*$/i, "") || s;
  const [bad, setBad] = useState(false);
  const st = { width: size, height: size, minWidth: size } as const;
  if (bad) return <i className={"ico " + className} style={{ ...st, background: col || COL[sym] || "#37393a", fontSize: size * 0.5 }}>{sym[0]}</i>;
  return <img className={"ico cimg " + className} src={`/coins/${sym.toLowerCase()}.svg`} alt={sym} width={size} height={size} style={st} loading="lazy" onError={() => setBad(true)} />;
}
