import "@linbit/ui/tokens.css";
import "./globals.css";
import "./linbit.css";
import type { Metadata } from "next";
import { Providers } from "./providers";
import LegacyChrome from "@/components/LegacyChrome";

export const metadata: Metadata = {
  title: "linbit - 数字资产交易平台",
  description: "Linbit 加密货币交易所 —— 现货交易、实时行情与资产管理。",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="zh-CN">
      <body>
        <Providers>
          <LegacyChrome>{children}</LegacyChrome>
        </Providers>
      </body>
    </html>
  );
}
