import "@linbit/ui/tokens.css";
import "./partner.css";
import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = { title: "linbit 合伙人中心", description: "linbit 合伙人（代理）独立站" };

const NAV = [["/", "总览"], ["/commission", "佣金明细"], ["/team", "我的团队"], ["/links", "推广链接"], ["/apply", "申请 / 规则"]];

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>
        <header className="ph">
          <b className="logo">linbit <span>Partner</span></b>
          <nav>{NAV.map(([h, t]) => <Link key={h} href={h}>{t}</Link>)}</nav>
          <span className="me">lb_Demo8K2 · UID 80000001</span>
        </header>
        <main className="pm">{children}</main>
      </body>
    </html>
  );
}
