"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Button, Card } from "@linbit/ui";
import { useBalances } from "@/lib/queries";
import { useAuthStore } from "@/lib/store";
import { fmtNum } from "@/lib/format";

export default function AssetsPage() {
  const isAuthed = useAuthStore((s) => s.isAuthed);
  const email = useAuthStore((s) => s.email);
  const { data, isLoading } = useBalances();

  // 避免 SSR / 客户端认证态不一致
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  if (!mounted) return null;

  if (!isAuthed) {
    return (
      <div className="mx-auto max-w-md px-5 py-20 text-center">
        <Card>
          <h1 className="mb-2 text-xl font-semibold">请先登录</h1>
          <p className="mb-6 text-sm text-lb-text-2">登录后查看你的资产余额。</p>
          <div className="flex justify-center gap-3">
            <Link href="/login">
              <Button>登录</Button>
            </Link>
            <Link href="/register">
              <Button variant="secondary">注册</Button>
            </Link>
          </div>
        </Card>
      </div>
    );
  }

  const items = data?.items ?? [];
  const totalUsd = items.reduce(
    (sum, b) => sum + (b.valuationUsd ? Number(b.valuationUsd) : 0),
    0,
  );

  return (
    <div className="mx-auto max-w-5xl px-5 py-8">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">我的资产</h1>
          {email && <p className="text-sm text-lb-text-3">{email}</p>}
        </div>
        <div className="flex gap-3">
          <Button variant="secondary" size="sm">
            充值
          </Button>
          <Button variant="secondary" size="sm">
            提现
          </Button>
        </div>
      </div>

      <Card className="mb-6">
        <p className="text-sm text-lb-text-2">账户总估值</p>
        <p className="mt-1 text-3xl font-semibold tabular">
          ≈ {fmtNum(totalUsd, 2)} <span className="text-lg text-lb-text-2">USD</span>
        </p>
      </Card>

      <Card padded={false} className="overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-lb-text-2">
              <th className="px-5 py-3 font-normal">资产</th>
              <th className="px-5 py-3 text-right font-normal">可用</th>
              <th className="px-5 py-3 text-right font-normal">冻结</th>
              <th className="px-5 py-3 text-right font-normal">合计</th>
              <th className="px-5 py-3 text-right font-normal">估值 (USD)</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={5} className="px-5 py-10 text-center text-lb-text-3">
                  加载中…
                </td>
              </tr>
            )}
            {!isLoading && items.length === 0 && (
              <tr>
                <td colSpan={5} className="px-5 py-10 text-center text-lb-text-3">
                  暂无资产（请先运行 pnpm mock）
                </td>
              </tr>
            )}
            {items.map((b) => (
              <tr key={b.asset} className="border-t border-lb-border tabular">
                <td className="px-5 py-4 font-medium">{b.asset}</td>
                <td className="px-5 py-4 text-right">{fmtNum(b.free, 6)}</td>
                <td className="px-5 py-4 text-right text-lb-text-2">
                  {fmtNum(b.locked, 6)}
                </td>
                <td className="px-5 py-4 text-right">{fmtNum(b.total, 6)}</td>
                <td className="px-5 py-4 text-right text-lb-text-2">
                  {b.valuationUsd ? fmtNum(b.valuationUsd, 2) : "--"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  );
}
