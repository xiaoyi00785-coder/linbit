/** 迷你走势的纯函数（无依赖，便于脚本直接断言）。 */
export type RawK = { time?: number; openTime?: number; close?: string | number };

/** 解析 K 线收盘价序列（按时间升序）；不足 2 根或含非法 / 非正价格返回 null。 */
export function sparkPoints(rows: RawK[] | undefined | null): number[] | null {
  if (!Array.isArray(rows)) return null;
  const ks = rows
    .map((k) => ({ t: Number(k.time ?? k.openTime), c: Number(k.close) }))
    .filter((k) => Number.isFinite(k.t) && Number.isFinite(k.c) && k.c > 0)
    .sort((a, b) => a.t - b.t)
    .slice(-24);
  return ks.length >= 2 ? ks.map((k) => k.c) : null;
}

/** 折线坐标（SVG viewBox 90×44，上下各留 4 的边距）；价格全相同则画成水平线。 */
export function sparkPath(pts: number[], w = 90, h = 44, pad = 4): string {
  const lo = Math.min(...pts), hi = Math.max(...pts), k = hi - lo || 1;
  return pts.map((p, i) => `${((i / (pts.length - 1)) * w).toFixed(1)},${(hi === lo ? h / 2 : h - pad - ((p - lo) / k) * (h - pad * 2)).toFixed(1)}`).join(" ");
}
