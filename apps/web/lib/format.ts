/** 数字格式化：小额保留更多小数位。 */
export function fmtNum(value: string | number | undefined, maxFrac = 2): string {
  if (value === undefined) return "--";
  const n = typeof value === "string" ? Number(value) : value;
  if (!Number.isFinite(n)) return "--";
  return n.toLocaleString("en-US", {
    maximumFractionDigits: Math.abs(n) < 1 ? 5 : maxFrac,
  });
}

/** 百分比格式化，带正负号。 */
export function fmtPct(value: string | number | undefined): string {
  if (value === undefined) return "--";
  const n = typeof value === "string" ? Number(value) : value;
  if (!Number.isFinite(n)) return "--";
  return `${n >= 0 ? "+" : ""}${n.toFixed(2)}%`;
}

/** 根据涨跌返回 token 文字颜色类。 */
export function changeClass(value: string | number | undefined): string {
  const n = typeof value === "string" ? Number(value) : value ?? 0;
  return n >= 0 ? "text-lb-up" : "text-lb-down";
}

/** 自适应精度：保留 sig 位有效数字（至少 minDp 位小数，最多 maxDp），去尾零，绝不输出科学计数法 */
export function fmtSig(v: number, sig = 4, minDp = 2, maxDp = 12): string {
  if (!isFinite(v) || v === 0) return "0";
  const a = Math.abs(v);
  const dp = Math.min(maxDp, Math.max(minDp, sig - 1 - Math.floor(Math.log10(a))));
  const s = v.toFixed(dp);
  return s.includes(".") ? s.replace(/\.?0+$/, "") : s;
}
