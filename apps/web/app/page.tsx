"use client";

import Link from "next/link";
import { Button, Card } from "@linbit/ui";
import { useTickers } from "@/lib/queries";
import { changeClass, fmtNum, fmtPct } from "@/lib/format";

const FEATURES = [
  {
    title: "储备证明",
    desc: "平台对用户资产保持 1:1 足额储备，并定期公开核验。",
  },
  {
    title: "多层加密存储",
    desc: "资产存放于多签冷钱包，风险隔离、全程可追溯。",
  },
  {
    title: "智能风控",
    desc: "全链路实时监控，及时拦截异常行为，保障系统稳定。",
  },
];

export default function HomePage() {
  const { data: tickers } = useTickers();
  const hot = (tickers ?? []).slice(0, 6);

  return (
    <div className="mx-auto max-w-6xl px-5">
      {/* Hero */}
      <section className="py-20 text-center">
        <h1 className="text-4xl font-bold leading-tight md:text-5xl">
          Linbit 数字资产交易平台
          <br />
          <span className="text-lb-primary">安全 · 高效 · 全球</span>
        </h1>
        <p className="mt-5 text-lb-text-2">
          现货交易、实时行情与一站式资产管理，新用户专享最高 10,000 USDT 礼包。
        </p>
        <div className="mt-8 flex justify-center gap-3">
          <Link href="/register">
            <Button size="lg">立即注册</Button>
          </Link>
          <Link href="/markets">
            <Button size="lg" variant="secondary">
              查看行情
            </Button>
          </Link>
        </div>
      </section>

      {/* 热门交易对 */}
      <section className="pb-20">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-2xl font-semibold">热门交易对</h2>
          <Link href="/markets" className="text-sm text-lb-primary">
            更多 →
          </Link>
        </div>
        <Card padded={false} className="overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-lb-text-2">
                <th className="px-5 py-3 font-normal">交易对</th>
                <th className="px-5 py-3 text-right font-normal">最新价</th>
                <th className="px-5 py-3 text-right font-normal">24h 涨跌</th>
                <th className="px-5 py-3 text-right font-normal">24h 成交额</th>
                <th className="px-5 py-3 text-right font-normal">操作</th>
              </tr>
            </thead>
            <tbody>
              {hot.length === 0 && (
                <tr>
                  <td
                    colSpan={5}
                    className="px-5 py-10 text-center text-lb-text-3"
                  >
                    暂无行情数据（请先运行 pnpm mock）
                  </td>
                </tr>
              )}
              {hot.map((t) => (
                <tr
                  key={t.symbol}
                  className="border-t border-lb-border tabular"
                >
                  <td className="px-5 py-4 font-medium">{t.symbol}</td>
                  <td className="px-5 py-4 text-right">{fmtNum(t.lastPrice)}</td>
                  <td
                    className={`px-5 py-4 text-right ${changeClass(t.priceChangePercent)}`}
                  >
                    {fmtPct(t.priceChangePercent)}
                  </td>
                  <td className="px-5 py-4 text-right text-lb-text-2">
                    {fmtNum(t.quoteVolume, 0)}
                  </td>
                  <td className="px-5 py-4 text-right">
                    <Link href={`/trade/${t.symbol}`}>
                      <Button variant="ghost" size="sm">
                        交易
                      </Button>
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      </section>

      {/* 安全 */}
      <section className="pb-24">
        <h2 className="mb-8 text-center text-2xl font-semibold">
          三重防护，守护每一份资产
        </h2>
        <div className="grid gap-5 md:grid-cols-3">
          {FEATURES.map((f) => (
            <Card key={f.title}>
              <h3 className="mb-2 text-lg font-semibold text-lb-primary">
                {f.title}
              </h3>
              <p className="text-sm text-lb-text-2">{f.desc}</p>
            </Card>
          ))}
        </div>
      </section>
    </div>
  );
}
