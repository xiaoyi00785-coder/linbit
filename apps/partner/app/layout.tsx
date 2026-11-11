// 合伙人站根布局：套上导航外壳，页面标题为合伙人中心。
import "@linbit/ui/tokens.css";
import "./partner.css";
import type { Metadata } from "next";
import Shell from "./Shell";

export const metadata: Metadata = { title: "linbit 合伙人中心", description: "linbit 合伙人（代理）独立站" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body><Shell>{children}</Shell></body>
    </html>
  );
}
