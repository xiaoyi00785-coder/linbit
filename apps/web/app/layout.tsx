import "@linbit/ui/tokens.css";
import "./globals.css";
import type { Metadata } from "next";
import { Providers } from "./providers";
import { SiteNav } from "@/components/SiteNav";

export const metadata: Metadata = {
  title: "Linbit | 数字资产交易平台",
  description: "Linbit 加密货币交易所 —— 现货交易、实时行情与资产管理。",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="zh-CN">
      <body className="min-h-screen bg-lb-bg text-lb-text antialiased">
        <Providers>
          <SiteNav />
          <main>{children}</main>
        </Providers>
      </body>
    </html>
  );
}
