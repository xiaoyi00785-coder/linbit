import Link from "next/link";

export default function NotFound() {
  return (
    <main data-nf="" className="wrap" style={{ minHeight: "60vh", display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 16 }}>
      <svg width="56" height="56" viewBox="0 0 24 24" fill="none" stroke="#abe127" strokeWidth="1.4" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4M8.5 8.5l5 5M13.5 8.5l-5 5" /></svg>
      <h2 style={{ margin: 0 }}>页面不存在</h2>
      <p style={{ margin: 0, opacity: 0.6 }}>链接可能已失效，或交易对不存在。</p>
      <div style={{ display: "flex", gap: 12 }}><Link className="abtn" href="/">返回首页</Link><Link className="abtn" href="/spot/BTCUSDT">去交易</Link></div>
    </main>
  );
}
