"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Button, Card, Input } from "@linbit/ui";
import { useTickers } from "@/lib/queries";
import { changeClass, fmtNum, fmtPct } from "@/lib/format";

type SortKey = "symbol" | "lastPrice" | "priceChangePercent" | "quoteVolume";

export default function MarketsPage() {
  const { data: tickers, isLoading } = useTickers();
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<SortKey>("quoteVolume");
  const [desc, setDesc] = useState(true);

  const rows = useMemo(() => {
    const list = (tickers ?? []).filter((t) =>
      t.symbol.toLowerCase().includes(q.trim().toLowerCase()),
    );
    const num = (v: string) => Number(v);
    list.sort((a, b) => {
      let d = 0;
      if (sort === "symbol") d = a.symbol.localeCompare(b.symbol);
      else d = num(a[sort]) - num(b[sort]);
      return desc ? -d : d;
    });
    return list;
  }, [tickers, q, sort, desc]);

  const toggleSort = (key: SortKey) => {
    if (key === sort) setDesc((d) => !d);
    else {
      setSort(key);
      setDesc(true);
    }
  };

  const arrow = (key: SortKey) => (sort === key ? (desc ? " ↓" : " ↑") : "");

  return (
    <div className="mx-auto max-w-6xl px-5 py-8">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">行情中心</h1>
        <div className="w-64">
          <Input
            placeholder="搜索交易对，如 BTC"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>

      <Card padded={false} className="overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-lb-text-2">
              <th
                className="cursor-pointer px-5 py-3 font-normal"
                onClick={() => toggleSort("symbol")}
              >
                交易对{arrow("symbol")}
              </th>
              <th
                className="cursor-pointer px-5 py-3 text-right font-normal"
                onClick={() => toggleSort("lastPrice")}
              >
                最新价{arrow("lastPrice")}
              </th>
              <th
                className="cursor-pointer px-5 py-3 text-right font-normal"
                onClick={() => toggleSort("priceChangePercent")}
              >
                24h 涨跌{arrow("priceChangePercent")}
              </th>
              <th className="px-5 py-3 text-right font-normal">24h 最高/最低</th>
              <th
                className="cursor-pointer px-5 py-3 text-right font-normal"
                onClick={() => toggleSort("quoteVolume")}
              >
                24h 成交额{arrow("quoteVolume")}
              </th>
              <th className="px-5 py-3 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={6} className="px-5 py-10 text-center text-lb-text-3">
                  加载中…
                </td>
              </tr>
            )}
            {!isLoading && rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-5 py-10 text-center text-lb-text-3">
                  暂无数据（请先运行 pnpm mock）
                </td>
              </tr>
            )}
            {rows.map((t) => (
              <tr key={t.symbol} className="border-t border-lb-border tabular">
                <td className="px-5 py-4 font-medium">{t.symbol}</td>
                <td className="px-5 py-4 text-right">{fmtNum(t.lastPrice)}</td>
                <td
                  className={`px-5 py-4 text-right ${changeClass(t.priceChangePercent)}`}
                >
                  {fmtPct(t.priceChangePercent)}
                </td>
                <td className="px-5 py-4 text-right text-lb-text-2">
                  {fmtNum(t.high)} / {fmtNum(t.low)}
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
    </div>
  );
}
