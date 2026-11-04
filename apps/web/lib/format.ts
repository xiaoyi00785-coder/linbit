/** 数字格式化：小额保留更多小数位。 */
export function fmtNum(value: string | number | undefined, maxFrac = 2): string {
  if (value === undefined) return "--";
  const n = typeof value === "string" ? Number(value) : value;
  if (!Number.isFinite(n)) return "--";
  return n.toLocaleString("en-US", {
    maximumFractionDigits: Math.abs(n) < 1 ? 8 : maxFrac,
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

/** 去尾零的定点小数，绝不输出科学计数法（用于数值→字符串，包括回填输入框/请求体） */
export function trimFixed(v: string | number | null | undefined, dp = 8): string {
  const n = typeof v === "string" ? Number(v) : v ?? NaN;
  if (!Number.isFinite(n)) return "--";
  const s = (n as number).toFixed(dp);
  const t = s.includes(".") ? s.replace(/\.?0+$/, "") : s;
  return t === "-0" ? "0" : t;
}

/**
 * 金额/数量统一格式化（全站共用）：最多 maxDp 位小数、去尾零、可选千分位；
 * 比 10^-maxDp 还小的非零值改用有效数字显示，避免显示成 0 或 4.2e-7。
 */
export function fmtAmt(v: string | number | null | undefined, maxDp = 8, group = false): string {
  const n = typeof v === "string" ? Number(v) : v ?? NaN;
  if (!Number.isFinite(n)) return "--";
  if (n === 0) return "0";
  if (Math.abs(n as number) < 10 ** -maxDp) return fmtSig(n as number, 4, 0, 18);
  return group ? (n as number).toLocaleString("en-US", { maximumFractionDigits: maxDp }) : trimFixed(n, maxDp);
}

/**
 * 接口返回的手续费字符串：后端已返回全精度（非零小额手续费不会被截断，如 0.000000065），
 * 这里只去尾零、不再四舍五入，避免把 0.000000065 显示成 0.00000007 或 0。
 */
export function fmtFeeStr(s: string | number | null | undefined): string {
  if (s == null || s === "") return "--";
  const str = String(s).trim();
  const m = /^(-?)(\d+)(?:\.(\d*))?$/.exec(str);
  if (m) {
    const frac = (m[3] ?? "").replace(/0+$/, "");
    const int = m[2].replace(/^0+(?=\d)/, "");
    const out = frac ? `${int}.${frac}` : int;
    return out === "0" || !m[1] ? out : "-" + out;
  }
  return fmtAmt(str, 18);
}

/** 手续费 / 资金费等账单金额：原样显示后端给出的精确字符串（后端已统一补齐到 8 位小数），
 *  不去尾零、不经 Number 转换，避免同一列出现 -0.0038384 与 -0.00383384 这种位数不一致。 */
export function fmtFeeExact(s: string | number | null | undefined): string {
  if (s == null || s === "") return "--";
  const str = String(s).trim();
  return /^-?\d+(\.\d+)?$/.test(str) ? str : fmtFeeStr(str);
}
