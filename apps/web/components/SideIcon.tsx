/** 实心双色图标（深色主体 + 主题色点缀），用于侧栏 / 顶栏菜单，24x24 */
import type { ReactNode } from "react";
const D = "var(--si-d,#121212)", G = "var(--lb-primary)";
const P: Record<string, ReactNode> = {
  overview: <><rect x="3" y="7" width="18" height="13" rx="2" fill={D} /><rect x="3" y="3" width="13" height="4" fill={G} /><path d="M15 13.5l2-2 2 2-2 2z" fill="#fff" /></>,
  funding: <><circle cx="12" cy="12" r="9" fill={D} /><path d="M12 7.5l4.5 4.5-4.5 4.5L7.5 12z" fill={G} /></>,
  futures: <><rect x="3" y="11" width="4" height="9" rx="1" fill={G} /><path d="M9 4h3v10a1.5 1.5 0 003 0V4h3v10a4.5 4.5 0 01-9 0z" fill={D} /></>,
  spot: <><circle cx="13" cy="11" r="8" fill={G} /><circle cx="11" cy="13" r="7.5" fill={D} /><path d="M11 10l3 3-3 3-3-3z" fill="#fff" /></>,
  earn: <><path d="M9 3h6l-1.5 3h-3z" fill={D} /><path d="M6 8h12l1.5 12h-15z" fill={D} /><circle cx="12" cy="14" r="3" fill={G} /></>,
  tradfi: <><path d="M3 12a9 9 0 0118 0h-3a6 6 0 00-12 0z" fill={G} /><path d="M7 14h10l2 6H5z" fill={D} /><rect x="10" y="16" width="4" height="4" fill="#fff" /></>,
  bots: <><rect x="4" y="7" width="16" height="12" rx="2" fill={D} /><rect x="1.5" y="10" width="2.5" height="5" rx="1" fill={D} /><rect x="20" y="10" width="2.5" height="5" rx="1" fill={D} /><circle cx="9" cy="12" r="1.6" fill={G} /><circle cx="15" cy="12" r="1.6" fill={G} /><rect x="11" y="3" width="2" height="4" fill={D} /></>,
  records: <><rect x="3" y="9" width="4" height="11" rx="1" fill={G} /><rect x="8" y="4" width="13" height="16" rx="2" fill={D} /><path d="M11 9h7M11 12.5h7M11 16h4" stroke="#fff" strokeWidth="1.6" /></>,
  orders: <><path d="M6 3h9l4 4v8a6 6 0 00-6 6H6a2 2 0 01-2-2V5a2 2 0 012-2z" fill={D} /><circle cx="18" cy="18" r="4.5" fill={G} /><path d="M18 16v2.3l1.5 1" stroke={D} strokeWidth="1.4" fill="none" /></>,
  lock: <><path d="M7 10V7a5 5 0 0110 0v3h-2.5V7a2.5 2.5 0 00-5 0v3z" fill={G} /><rect x="4" y="10" width="16" height="11" rx="2" fill={D} /><circle cx="12" cy="15.5" r="1.8" fill="#fff" /></>,
  kyc: <><circle cx="10" cy="7" r="4" fill={D} /><path d="M2.5 20a7.5 7.5 0 0113.5-4.5V20z" fill={D} /><circle cx="18" cy="18" r="4.5" fill={G} /><path d="M16 18l1.5 1.5 2.5-3" stroke={D} strokeWidth="1.5" fill="none" /></>,
  gift: <><rect x="3" y="8" width="18" height="4" rx="1" fill={D} /><rect x="4.5" y="12" width="15" height="9" rx="1" fill={D} /><rect x="11" y="8" width="2" height="13" fill="#fff" /><path d="M12 8C10 3 6 4 7.5 8zM12 8c2-5 6-4 4.5 0z" fill={G} /></>,
  invite: <><circle cx="10" cy="7" r="4" fill={D} /><path d="M2.5 20a7.5 7.5 0 0113.5-4.5V20z" fill={D} /><circle cx="18" cy="17" r="4.5" fill={G} /><path d="M18 14.8v4.4M15.8 17h4.4" stroke={D} strokeWidth="1.5" /></>,
  settings: <><path d="M12 2l2 2.5 3.2-.6.6 3.2L20.5 9 19.5 12l1 3-2.7 1.9-.6 3.2-3.2-.6L12 22l-2-2.5-3.2.6-.6-3.2L3.5 15l1-3-1-3 2.7-1.9.6-3.2 3.2.6z" fill={D} /><circle cx="12" cy="12" r="3.5" fill={G} /></>,
  api: <><path d="M14 4l6 6-5 5-6-6z" fill={D} /><path d="M9 15l-5 5" stroke={D} strokeWidth="2.5" strokeLinecap="round" /><path d="M5 13l6 6" stroke={G} strokeWidth="2.5" strokeLinecap="round" /><path d="M16 3l5 5" stroke={G} strokeWidth="2" strokeLinecap="round" /></>,
  devices: <><rect x="2" y="4" width="16" height="11" rx="1.5" fill={D} /><rect x="6" y="17" width="8" height="2" fill={D} /><rect x="15" y="9" width="7" height="12" rx="1.5" fill={G} /></>,
  address: <><path d="M12 2a7 7 0 017 7c0 5-7 13-7 13S5 14 5 9a7 7 0 017-7z" fill={D} /><circle cx="12" cy="9" r="2.8" fill={G} /></>,
  bell: <><path d="M12 3a6 6 0 016 6v5l2 3H4l2-3V9a6 6 0 016-6z" fill={D} /><circle cx="12" cy="20" r="2" fill={G} /></>,
  shield: <><path d="M12 2l8 3v6c0 5-3.5 9-8 11-4.5-2-8-6-8-11V5z" fill={D} /><path d="M8.5 12l2.5 2.5 4.5-5" stroke={G} strokeWidth="2" fill="none" /></>,
  dash: <><rect x="3" y="3" width="8" height="8" rx="1.5" fill={G} /><rect x="13" y="3" width="8" height="8" rx="1.5" fill={D} /><rect x="3" y="13" width="8" height="8" rx="1.5" fill={D} /><rect x="13" y="13" width="8" height="8" rx="1.5" fill={D} /></>,
  copy: <><circle cx="8" cy="8" r="4" fill={D} /><circle cx="16" cy="8" r="4" fill={G} /><path d="M2 20a6 6 0 0112 0zM10 20a6 6 0 0112 0z" fill={D} /></>,
  convert: <><path d="M4 9h13l-4-4" stroke={D} strokeWidth="2.5" fill="none" strokeLinecap="round" strokeLinejoin="round" /><path d="M20 15H7l4 4" stroke={G} strokeWidth="2.5" fill="none" strokeLinecap="round" strokeLinejoin="round" /></>,
  loan: <><circle cx="9" cy="9" r="6" fill={D} /><path d="M7 9h4M9 7v4" stroke="#fff" strokeWidth="1.5" /><circle cx="16" cy="16" r="6" fill={G} /></>,
  buy: <><rect x="2" y="5" width="20" height="14" rx="2" fill={D} /><rect x="2" y="8" width="20" height="3" fill={G} /></>,
  logout: <><path d="M14 4H5v16h9" stroke={D} strokeWidth="2.5" fill="none" /><path d="M10 12h11M17 8l4 4-4 4" stroke={G} strokeWidth="2.5" fill="none" strokeLinecap="round" /></>,
};
export function SideIcon({ n, size = 20 }: { n: string; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden>{P[n] ?? P.dash}</svg>;
}
