"use client";
/**
 * 价格跳动闪烁：数值较上次上涨闪绿底、下跌闪红底（约 0.8s 渐隐）；首帧与非数值不闪。
 * 闪烁只是瞬时背景动画，不改文字颜色；静态（静止时）颜色由 chg（24h 涨跌）决定，与 tick 方向无关。
 */
import { useEffect, useRef, useState, type ReactNode } from "react";
import { flashDir } from "@/lib/tick";
import { changeClass } from "@/lib/format";

export function Flash({ v, chg, children, className = "" }: { v: string | number | null | undefined; /** 24h 涨跌幅：决定静态颜色；缺省则不着色（继承） */ chg?: string | number | null; children: ReactNode; className?: string }) {
  const prev = useRef<number | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [st, setSt] = useState<{ d: "up" | "down"; n: number } | null>(null);
  useEffect(() => () => clearTimeout(timer.current), []);
  useEffect(() => {
    const n = Number(v);
    if (v == null || v === "" || !Number.isFinite(n)) return;
    const d = flashDir(prev.current, n);
    prev.current = n;
    if (!d) return;
    setSt((s) => ({ d, n: (s?.n ?? 0) + 1 }));
    // 计时器不随 v 变化清理：否则相同数值的重复帧会让闪烁类一直残留
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSt(null), 800);
  }, [v]);
  // 交替两个动画名，连续同向跳动也能重新触发
  const cls = st ? ` lb-flash-${st.d}${st.n % 2 ? "" : "2"}` : "";
  const rest = chg === undefined ? "" : " " + changeClass(chg);
  return <span className={"lb-flash" + cls + rest + (className ? " " + className : "")} data-flash={st?.d ?? ""}>{children}</span>;
}
