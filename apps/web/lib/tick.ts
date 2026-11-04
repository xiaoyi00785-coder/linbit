/** 实时行情的纯函数（无 React 依赖，便于脚本校验）。 */
export function flashDir(prev: number | null, next: number): "up" | "down" | null {
  if (prev == null || !Number.isFinite(prev) || !Number.isFinite(next) || prev === next) return null;
  return next > prev ? "up" : "down";
}
/** 把一笔实时价格并入当前 K 线：更新 close，并扩展 high/low。 */
export function mergeTick<B extends { high: number; low: number; close: number }>(b: B, px: number): B {
  return { ...b, close: px, high: Math.max(b.high, px), low: Math.min(b.low, px) };
}
