"use client";
import { G } from "@/components/Glyph";
import { useParams, useRouter } from "next/navigation";
import { SOON, bySlug } from "@/lib/features";

export default function ComingSoon() {
  const { feature } = useParams<{ feature: string }>();
  const r = useRouter();
  const k = bySlug(feature);
  const it = k ? SOON[k] : { name: decodeURIComponent(feature || ""), intro: "该功能即将上线，敬请期待。", icon: "◇" };
  const back = () => (typeof window !== "undefined" && window.history.length > 1 && document.referrer.startsWith(location.origin) ? r.back() : r.push("/"));
  return (
    <main className="cs-wrap">
      <div className="cs-card">
        <span className="cs-ic"><G n={it.icon} s={28} /></span>
        <em className="cs-badge">即将上线</em>
        <h1>{it.name}</h1>
        <p>{it.intro}</p>
        <div className="cs-btns"><button className="cs-b1" onClick={back}>返回</button><a className="cs-b2" href="/">回到首页</a></div>
      </div>
    </main>
  );
}
