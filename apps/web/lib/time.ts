/** 统一本地时区时间显示：YYYY-MM-DD HH:mm:ss (UTC±h[:mm])。接受毫秒时间戳、秒时间戳或 ISO 字符串。 */
export function tzLabel(d = new Date()): string {
  const o = -d.getTimezoneOffset(), s = o >= 0 ? "+" : "-", a = Math.abs(o);
  return `UTC${s}${Math.floor(a / 60)}${a % 60 ? ":" + String(a % 60).padStart(2, "0") : ""}`;
}
export function fmtLocal(t?: number | string | null, withZone = true): string {
  if (t == null || t === "") return "-";
  let n: number | string = t;
  if (typeof t === "string" && /^\d+$/.test(t)) n = Number(t);
  if (typeof n === "number" && n < 1e12) n *= 1000;
  const d = new Date(n);
  if (isNaN(d.getTime())) return String(t);
  const p = (x: number) => String(x).padStart(2, "0");
  const s = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
  return withZone ? `${s} (${tzLabel(d)})` : s;
}
