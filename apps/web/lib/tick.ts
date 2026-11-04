/** 实时行情的纯函数（无 React 依赖，便于脚本校验）。 */
export function flashDir(prev: number | null, next: number): "up" | "down" | null {
  if (prev == null || !Number.isFinite(prev) || !Number.isFinite(next) || prev === next) return null;
  return next > prev ? "up" : "down";
}
/** 把一笔实时价格并入当前 K 线：更新 close，并扩展 high/low。 */
export function mergeTick<B extends { high: number; low: number; close: number }>(b: B, px: number): B {
  return { ...b, close: px, high: Math.max(b.high, px), low: Math.min(b.low, px) };
}
export type LiveBar = { time: number; open: number; high: number; low: number; close: number; v: number; src?: string };
/** WS futures-kline 帧 → 图表 K 线（秒级 time）。 */
export function wsKline(d: any): LiveBar | null {
  const t = Number(d?.time ?? d?.openTime);
  if (!Number.isFinite(t) || !(Number(d?.close) > 0)) return null;
  return { time: t > 1e12 ? Math.floor(t / 1000) : t, open: +d.open, high: +d.high, low: +d.low, close: +d.close, v: +d.volume || 0, src: d.source === "index" ? "index" : undefined };
}
/** 合并 WS 当前根：同一根时与已有根合并 high/low（重启后首根只覆盖部分区间）；新 time 即上一根收盘，直接追加；旧 time 忽略。 */
export function mergeWsBar(last: LiveBar | null, b: LiveBar): LiveBar | null {
  if (!last || b.time > last.time) return b;
  if (b.time < last.time) return null;
  return { ...b, high: Math.max(last.high, b.high), low: Math.min(last.low, b.low) };
}
