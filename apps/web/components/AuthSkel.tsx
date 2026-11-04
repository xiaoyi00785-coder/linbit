"use client";
/** 首屏骨架：/login、/register 动态加载与已登录跳转期间展示，避免白屏 */
export function AuthSkel() {
  return (
    <main className="auth" aria-busy="true">
      <div className="aleft"><div className="sk-in" style={{ width: "70%", height: 36, marginBottom: 12 }} /><div className="sk-in" style={{ width: "50%", height: 36 }} /></div>
      <div className="aright">{[40, 44, 44, 44, 48].map((h, i) => <div key={i} className="sk-in" style={{ height: h, marginBottom: 14, borderRadius: 8 }} />)}</div>
    </main>
  );
}
