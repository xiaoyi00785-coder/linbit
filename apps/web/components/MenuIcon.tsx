/** 菜单插画图标：白色主体 + 主题色点缀，32x32 */
import type { ReactNode } from "react";
const W = "var(--mi-w,#fff)", G = "var(--lb-primary)";
const s = { fill: "none", stroke: W, strokeWidth: 2, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
const g = { ...s, stroke: G };
const P: Record<string, ReactNode> = {
  card: <><rect x="4" y="8" width="22" height="15" rx="3" {...s} /><path d="M4 13h22" {...s} /><circle cx="24" cy="23" r="5" fill={G} /><path d="M22 23h4M24 21v4" stroke="#070808" strokeWidth="1.6" /></>,
  deposit: <><path d="M16 4v14M10 12l6 6 6-6" {...s} /><path d="M5 20v5a2 2 0 002 2h18a2 2 0 002-2v-5" {...g} /></>,
  bankcard: <><rect x="4" y="7" width="24" height="18" rx="3" {...s} /><rect x="8" y="12" width="6" height="5" rx="1" fill={G} /><path d="M18 20h6" {...s} /></>,
  spot: <><path d="M6 11h18l-5-5M26 21H8l5 5" {...s} /><circle cx="16" cy="16" r="2.5" fill={G} /></>,
  grid: <><rect x="5" y="5" width="22" height="22" rx="3" {...s} /><path d="M5 16h22M16 5v22" {...s} /><path d="M8 22l5-5 4 3 7-8" {...g} /></>,
  convert: <><circle cx="11" cy="16" r="6" {...s} /><circle cx="21" cy="16" r="6" {...g} /></>,
  perp: <><rect x="5" y="5" width="22" height="22" rx="5" {...s} /><path d="M11 10v7a5 5 0 0010 0v-7" {...g} /></>,
  stock: <><path d="M5 27h22" {...s} /><rect x="7" y="16" width="4" height="9" fill={W} /><rect x="14" y="10" width="4" height="15" fill={G} /><rect x="21" y="6" width="4" height="19" fill={W} /></>,
  demo: <><rect x="4" y="6" width="24" height="16" rx="2" {...s} /><path d="M12 27h8M16 22v5" {...s} /><path d="M14 11l5 3-5 3z" fill={G} /></>,
  tradfi: <><path d="M4 12l12-7 12 7z" {...s} /><path d="M7 14v9M13 14v9M19 14v9M25 14v9" {...s} /><path d="M4 26h24" {...g} /></>,
  explore: <><circle cx="16" cy="16" r="11" {...s} /><path d="M20 12l-2.5 6-5.5 2 2.5-6z" fill={G} /></>,
  pie: <><circle cx="15" cy="17" r="10" {...s} /><path d="M18 4v10h10A10 10 0 0018 4z" fill={G} /></>,
  flex: <><rect x="6" y="9" width="20" height="17" rx="3" {...s} /><path d="M11 9V7a5 5 0 0110 0v2" {...s} /><path d="M12 18l3 3 6-6" {...g} /></>,
  lock: <><rect x="6" y="14" width="20" height="13" rx="3" {...s} /><path d="M10 14v-4a6 6 0 0112 0v4" {...s} /><circle cx="16" cy="20.5" r="2.5" fill={G} /></>,
  coin: <><circle cx="13" cy="15" r="9" {...s} /><path d="M13 10v10M10 12.5h4.5a2 2 0 010 4H10" {...s} /><circle cx="24" cy="24" r="5" fill={G} /></>,
  chain: <><rect x="4" y="11" width="12" height="10" rx="5" {...s} /><rect x="16" y="11" width="12" height="10" rx="5" {...g} /></>,
  vip: <><path d="M4 11l6 5 6-9 6 9 6-5-3 14H7z" {...s} /><circle cx="16" cy="19" r="2.5" fill={G} /></>,
  loan: <><ellipse cx="13" cy="9" rx="8" ry="3" {...s} /><path d="M5 9v12c0 1.7 3.6 3 8 3" {...s} /><path d="M5 15c0 1.7 3.6 3 8 3" {...s} /><circle cx="23" cy="22" r="6" fill={G} /><path d="M21 22h4M23 20v4" stroke="#070808" strokeWidth="1.6" /></>,
  gift: <><rect x="5" y="12" width="22" height="15" rx="2" {...s} /><path d="M3 12h26M16 12v15" {...s} /><path d="M16 12c-2-5-7-6-7-2 0 2 3 2 7 2zM16 12c2-5 7-6 7-2 0 2-3 2-7 2z" {...g} /></>,
  cashback: <><circle cx="16" cy="16" r="8" {...s} /><path d="M16 12v8M13.5 14h4a1.5 1.5 0 010 3h-3" {...s} /><path d="M5 12a12 12 0 0120-4M27 20a12 12 0 01-20 4" {...g} /></>,
  lottery: <><rect x="3" y="8" width="26" height="16" rx="2" {...s} /><path d="M11.7 8v16M20.3 8v16" {...s} /><path d="M6 14h3l-2 5M15 14h3l-2 5M24 14h3l-2 5" {...g} /></>,
  invite: <><circle cx="13" cy="10" r="5" {...s} /><path d="M4 26a9 9 0 0115-6" {...s} /><circle cx="23" cy="23" r="5" fill={G} /><path d="M21 23h4M23 21v4" stroke="#070808" strokeWidth="1.6" /></>,
  refer: <><path d="M4 22l7 4h10l7-6-3-2-6 4h-6" {...s} /><circle cx="17" cy="10" r="6" {...g} /><path d="M17 7v6M14 10h6" {...g} /></>,
  bars: <><path d="M5 27h22" {...s} /><path d="M9 23v-6M16 23V9M23 23v-9" {...s} /><circle cx="16" cy="7" r="3" fill={G} /></>,
  trophy: <><path d="M10 5h12v7a6 6 0 01-12 0z" {...s} /><path d="M10 8H6a4 4 0 004 5M22 8h4a4 4 0 01-4 5M16 18v5M11 27h10" {...s} /><path d="M14 9l2-1.5L18 9" {...g} /></>,
  target: <><circle cx="16" cy="16" r="11" {...s} /><circle cx="16" cy="16" r="6" {...s} /><circle cx="16" cy="16" r="2.5" fill={G} /></>,
  star: <><path d="M16 4l3.5 7.5 8 1-6 5.5 1.5 8L16 22l-7 4 1.5-8-6-5.5 8-1z" {...s} /><circle cx="16" cy="16" r="3" fill={G} /></>,
  envelope: <><rect x="5" y="9" width="22" height="18" rx="2" {...s} /><path d="M5 11l11 7 11-7" {...s} /><circle cx="16" cy="6" r="3" fill={G} /></>,
  calendar: <><rect x="5" y="7" width="22" height="20" rx="3" {...s} /><path d="M5 13h22M11 4v5M21 4v5" {...s} /><path d="M11 20l3 3 6-6" {...g} /></>,
  scale: <><path d="M16 5v22M8 27h16M6 9h20" {...s} /><path d="M6 9l-3 8h6zM26 9l-3 8h6z" {...g} /></>,
  house: <><path d="M5 15L16 6l11 9v11H5z" {...s} /><rect x="13" y="18" width="6" height="8" fill={G} /></>,
  chart: <><rect x="4" y="5" width="24" height="22" rx="3" {...s} /><path d="M8 21l5-6 4 4 7-8" {...g} /></>,
  index: <><path d="M5 27h22" {...s} /><rect x="7" y="15" width="4" height="10" fill={W} /><rect x="14" y="9" width="4" height="16" fill={G} /><rect x="21" y="12" width="4" height="13" fill={W} /></>,
  rate: <><rect x="6" y="4" width="20" height="24" rx="3" {...s} /><path d="M12 20l8-8" {...g} /><circle cx="12" cy="12" r="2" fill={W} /><circle cx="20" cy="20" r="2" fill={W} /></>,
  shield: <><path d="M16 4l10 4v7c0 6-4 11-10 13C10 26 6 21 6 15V8z" {...s} /><path d="M12 16l3 3 5-6" {...g} /></>,
  book: <><path d="M4 7h9a3 3 0 013 3v17a3 3 0 00-3-3H4zM28 7h-9a3 3 0 00-3 3v17a3 3 0 013-3h9z" {...s} /><path d="M20 12h4M20 16h4" {...g} /></>,
  api: <><rect x="4" y="6" width="24" height="20" rx="3" {...s} /><path d="M11 13l-3 3 3 3M21 13l3 3-3 3" {...s} /><path d="M17.5 12l-3 8" {...g} /></>,
  help: <><circle cx="16" cy="16" r="11" {...s} /><path d="M12.5 13a3.5 3.5 0 117 0c0 2.5-3.5 2.5-3.5 5" {...s} /><circle cx="16" cy="22.5" r="1.8" fill={G} /></>,
  announce: <><path d="M5 13v6h4l9 6V7l-9 6z" {...s} /><path d="M22 12a5 5 0 010 8M25 9a9 9 0 010 14" {...g} /></>,
  doc: <><path d="M8 4h11l6 6v18H8z" {...s} /><path d="M12 15h9M12 20h9" {...g} /></>,
};
export function MenuIcon({ n, size = 32 }: { n: string; size?: number }) {
  return <svg className="mic" width={size} height={size} viewBox="0 0 32 32" aria-hidden>{P[n] ?? P.doc}</svg>;
}
export const hasMenuIcon = (n: string) => n in P;
