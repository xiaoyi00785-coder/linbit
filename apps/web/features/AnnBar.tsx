"use client";
// 公告栏：取最新一条真实公告（置顶优先）；加载中/无公告/接口失败时整栏不渲染，不再用固定文案冒充公告
import { useEffect, useState } from "react";
import { Link } from "@/lib/router";
import { listAnnouncements, type Announcement } from "@/lib/services";

export function useLatestAnnouncement(): Announcement | null {
  const [a, setA] = useState<Announcement | null>(null);
  useEffect(() => {
    let on = true;
    listAnnouncements().then((r) => { if (on && r.ok && r.source === "api" && r.data) setA(r.data.find((x) => x.pinned) ?? r.data[0] ?? null); });
    return () => { on = false; };
  }, []);
  return a;
}

const Speaker = () => <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><path d="M3 10v4h4l5 4V6L7 10z" /><path d="M16 9a4 4 0 0 1 0 6" /></svg>;

/** wrapCls：外层容器 class；row：内层行 class（首页 notice / 理财 lrow ann） */
export function AnnBar({ wrapCls, row }: { wrapCls?: string; row: string }) {
  const a = useLatestAnnouncement();
  if (!a) return null;
  const inner = <div className={row} data-testid="ann-bar"><Link className="notice-t" to={`/announcements?id=${encodeURIComponent(a.id)}`}><Speaker /> {a.title}</Link><Link to="/announcements"><b>更多 →</b></Link></div>;
  return wrapCls ? <section className={wrapCls}>{inner}</section> : inner;
}
