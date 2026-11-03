"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { call, setToken, useApi, useToken, type Me } from "@/lib/api";

const NAV = [["/", "总览"], ["/commission", "佣金明细"], ["/team", "我的团队"], ["/links", "推广链接"], ["/apply", "申请 / 规则"]];

function Login() {
  const [email, setE] = useState(""); const [pw, setP] = useState(""); const [err, setErr] = useState(""); const [busy, setB] = useState(false);
  const go = async (e: React.FormEvent) => {
    e.preventDefault(); setErr(""); setB(true);
    const r = await call<{ accessToken?: string; challengeToken?: string }>("POST", "/auth/login", { email, password: pw });
    setB(false);
    if (!r.ok) return setErr(r.error || "登录失败");
    if (r.data?.challengeToken) return setErr("该账户已开启二次验证，请先在主站完成验证后再登录合伙人中心");
    if (r.data?.accessToken) setToken(r.data.accessToken);
  };
  return (
    <form className="card login fade" onSubmit={go}>
      <h1>登录合伙人中心</h1>
      <p className="muted">使用 linbit 主站账户登录</p>
      <input placeholder="邮箱" value={email} onChange={(e) => setE(e.target.value)} autoComplete="username" />
      <input placeholder="密码" type="password" value={pw} onChange={(e) => setP(e.target.value)} autoComplete="current-password" />
      {err && <div className="err">{err}</div>}
      <button className="btn wide" disabled={busy || !email || !pw}>{busy ? "登录中…" : "登录"}</button>
    </form>
  );
}

export default function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const tok = useToken();
  const me = useApi<Me>(tok ? "/partner/me" : null);
  const open = path === "/apply"; // 规则页无需登录
  return (
    <>
      <header className="ph">
        <Link href="/" className="logo">linbit <span>Partner</span></Link>
        <nav>{NAV.map(([h, t]) => <Link key={h} href={h} className={path === h ? "on" : ""}>{t}</Link>)}</nav>
        {tok ? <span className="me">{me.data ? <>UID {me.data.uid}{me.data.tierName ? ` · ${me.data.tierName}` : ""}</> : "…"} <a onClick={() => setToken(null)}>退出</a></span> : tok === null && <span className="me">未登录</span>}
      </header>
      <main className="pm" key={path}>{tok === undefined ? <div className="skel" style={{ height: 200 }} /> : !tok && !open ? <Login /> : <div className="fade">{children}</div>}</main>
    </>
  );
}
