"use client";
/** 价格跳动闪烁：数值较上次上涨闪绿、下跌闪红（约 0.8s 渐隐）；首帧与非数值不闪。 */
import { useEffect, useRef, useState, type ReactNode } from "react";
import { flashDir } from "@/lib/tick";

export function Flash({ v, children, className = "" }: { v: string | number | null | undefined; children: ReactNode; className?: string }) {
  const prev = useRef<number | null>(null);
  const [st, setSt] = useState<{ d: "up" | "down"; n: number } | null>(null);
  useEffect(() => {
    const n = Number(v);
    if (v == null || v === "" || !Number.isFinite(n)) return;
    const d = flashDir(prev.current, n);
    prev.current = n;
    if (!d) return;
    setSt((s) => ({ d, n: (s?.n ?? 0) + 1 }));
    const t = setTimeout(() => setSt(null), 800);
    return () => clearTimeout(t);
  }, [v]);
  // 交替两个动画名，连续同向跳动也能重新触发
  const cls = st ? ` lb-flash-${st.d}${st.n % 2 ? "" : "2"}` : "";
  return <span className={"lb-flash" + cls + (className ? " " + className : "")} data-flash={st?.d ?? ""}>{children}</span>;
}
