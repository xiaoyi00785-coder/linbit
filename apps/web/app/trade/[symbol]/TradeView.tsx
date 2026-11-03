"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { KLineChart, type Candle } from "@linbit/chart";
import { Button, Card, Input, Tabs } from "@linbit/ui";
import type { KlineInterval, OrderSide } from "@linbit/api-client";
import { api } from "@/lib/api";
import { genCandles } from "@/lib/demo";
import {
  useDepth,
  useKlines,
  useOpenOrders,
  useTickers,
} from "@/lib/queries";
import { useAuthStore } from "@/lib/store";
import { changeClass, fmtNum, fmtPct } from "@/lib/format";

const INTERVALS: KlineInterval[] = ["1m", "5m", "15m", "1h", "4h", "1d", "1w"];

export default function TradeView({ symbol }: { symbol: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const isAuthed = useAuthStore((s) => s.isAuthed);

  const [interval, setInterval] = useState<KlineInterval>("15m");
  const { data: klines } = useKlines(symbol, interval);
  const { data: depth } = useDepth(symbol);
  const { data: tickers } = useTickers();
  const { data: openOrders } = useOpenOrders(symbol);

  const ticker = (tickers ?? []).find((t) => t.symbol === symbol);

  // 把 REST K 线映射成图表数据；为空时用占位数据保证可渲染。
  const candles: Candle[] = useMemo(() => {
    if (klines && klines.length > 0) {
      return klines.map((k) => ({
        time: k.time,
        open: Number(k.open),
        high: Number(k.high),
        low: Number(k.low),
        close: Number(k.close),
        volume: Number(k.volume),
      }));
    }
    return genCandles();
  }, [klines]);

  return (
    <div className="mx-auto max-w-[1400px] px-4 py-4">
      {/* 顶部行情条 */}
      <Card
        padded={false}
        className="mb-3 flex flex-wrap items-center gap-6 px-5 py-3"
      >
        <div className="text-lg font-semibold">{symbol}</div>
        <div className={`text-xl font-semibold ${changeClass(ticker?.priceChangePercent)}`}>
          {fmtNum(ticker?.lastPrice)}
        </div>
        <Stat label="24h 涨跌">
          <span className={changeClass(ticker?.priceChangePercent)}>
            {fmtPct(ticker?.priceChangePercent)}
          </span>
        </Stat>
        <Stat label="24h 最高">{fmtNum(ticker?.high)}</Stat>
        <Stat label="24h 最低">{fmtNum(ticker?.low)}</Stat>
        <Stat label="24h 成交额">{fmtNum(ticker?.quoteVolume, 0)}</Stat>
      </Card>

      <div className="grid gap-3 lg:grid-cols-[1fr_320px_320px]">
        {/* K 线 */}
        <Card className="min-h-[460px]">
          <div className="mb-3 flex items-center gap-2">
            {INTERVALS.map((iv) => (
              <button
                key={iv}
                onClick={() => setInterval(iv)}
                className={`rounded-lb px-2.5 py-1 text-xs transition-colors ${
                  iv === interval
                    ? "bg-lb-card-2 text-lb-text"
                    : "text-lb-text-2 hover:text-lb-text"
                }`}
              >
                {iv}
              </button>
            ))}
          </div>
          <div className="h-[400px]">
            <KLineChart data={candles} />
          </div>
        </Card>

        {/* 订单簿 */}
        <Card className="min-h-[460px]">
          <h3 className="mb-3 text-sm font-semibold">订单簿</h3>
          <OrderBook
            bids={depth?.bids ?? []}
            asks={depth?.asks ?? []}
            last={ticker?.lastPrice}
            lastClass={changeClass(ticker?.priceChangePercent)}
          />
        </Card>

        {/* 下单 */}
        <OrderForm
          symbol={symbol}
          price={ticker?.lastPrice}
          isAuthed={isAuthed}
          onNeedLogin={() => router.push("/login")}
          onPlaced={() => {
            queryClient.invalidateQueries({ queryKey: ["open-orders"] });
            queryClient.invalidateQueries({ queryKey: ["balances"] });
          }}
        />
      </div>

      {/* 当前委托 */}
      <Card className="mt-3">
        <h3 className="mb-3 text-sm font-semibold">当前委托</h3>
        {!isAuthed ? (
          <p className="py-6 text-center text-sm text-lb-text-3">
            登录后查看当前委托
          </p>
        ) : !openOrders || openOrders.items.length === 0 ? (
          <p className="py-6 text-center text-sm text-lb-text-3">暂无委托</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-lb-text-2">
                <th className="py-2 font-normal">时间</th>
                <th className="py-2 font-normal">交易对</th>
                <th className="py-2 font-normal">方向</th>
                <th className="py-2 text-right font-normal">价格</th>
                <th className="py-2 text-right font-normal">数量</th>
                <th className="py-2 text-right font-normal">已成交</th>
                <th className="py-2 text-right font-normal">操作</th>
              </tr>
            </thead>
            <tbody>
              {openOrders.items.map((o) => (
                <tr key={o.orderId} className="border-t border-lb-border tabular">
                  <td className="py-2 text-lb-text-2">
                    {new Date(o.createdAt).toLocaleString("zh-CN")}
                  </td>
                  <td className="py-2">{o.symbol}</td>
                  <td
                    className={`py-2 ${o.side === "buy" ? "text-lb-up" : "text-lb-down"}`}
                  >
                    {o.side === "buy" ? "买入" : "卖出"}
                  </td>
                  <td className="py-2 text-right">{fmtNum(o.price ?? undefined)}</td>
                  <td className="py-2 text-right">{fmtNum(o.quantity, 6)}</td>
                  <td className="py-2 text-right">
                    {fmtNum(o.filledQuantity, 6)}
                  </td>
                  <td className="py-2 text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={async () => {
                        await api.DELETE("/spot/orders/{orderId}", {
                          params: {
                            path: { orderId: o.orderId },
                            query: { symbol: o.symbol },
                          },
                        });
                        queryClient.invalidateQueries({
                          queryKey: ["open-orders"],
                        });
                      }}
                    >
                      撤单
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  );
}

function Stat({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col">
      <span className="text-xs text-lb-text-3">{label}</span>
      <span className="text-sm tabular">{children}</span>
    </div>
  );
}

function OrderBook({
  bids,
  asks,
  last,
  lastClass,
}: {
  bids: string[][];
  asks: string[][];
  last?: string;
  lastClass: string;
}) {
  const topAsks = asks.slice(0, 10).reverse();
  const topBids = bids.slice(0, 10);
  const empty = bids.length === 0 && asks.length === 0;

  if (empty) {
    return (
      <p className="py-10 text-center text-sm text-lb-text-3">
        暂无盘口（请先运行 pnpm mock）
      </p>
    );
  }

  return (
    <div className="text-xs tabular">
      <div className="mb-1 flex justify-between text-lb-text-3">
        <span>价格</span>
        <span>数量</span>
      </div>
      {topAsks.map((r, i) => (
        <div key={`a${i}`} className="flex justify-between py-0.5">
          <span className="text-lb-down">{fmtNum(r[0])}</span>
          <span className="text-lb-text-2">{fmtNum(r[1], 5)}</span>
        </div>
      ))}
      <div className={`my-1.5 text-base font-semibold ${lastClass}`}>
        {fmtNum(last)}
      </div>
      {topBids.map((r, i) => (
        <div key={`b${i}`} className="flex justify-between py-0.5">
          <span className="text-lb-up">{fmtNum(r[0])}</span>
          <span className="text-lb-text-2">{fmtNum(r[1], 5)}</span>
        </div>
      ))}
    </div>
  );
}

function OrderForm({
  symbol,
  price,
  isAuthed,
  onNeedLogin,
  onPlaced,
}: {
  symbol: string;
  price?: string;
  isAuthed: boolean;
  onNeedLogin: () => void;
  onPlaced: () => void;
}) {
  const [side, setSide] = useState<OrderSide>("buy");
  const [type, setType] = useState<"limit" | "market">("limit");
  const [priceInput, setPriceInput] = useState("");
  const [qty, setQty] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  const effectivePrice = priceInput || price || "";

  const submit = async () => {
    if (!isAuthed) {
      onNeedLogin();
      return;
    }
    if (!qty || (type === "limit" && !effectivePrice)) {
      setMsg("请填写价格与数量");
      return;
    }
    setSubmitting(true);
    setMsg(null);
    try {
      const { error } = await api.POST("/spot/orders", {
        body: {
          symbol,
          side,
          type,
          quantity: qty,
          ...(type === "limit" ? { price: effectivePrice } : {}),
        },
      });
      if (error) throw error;
      setMsg("下单成功");
      setQty("");
      onPlaced();
    } catch {
      setMsg("下单失败，请稍后重试");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Card className="min-h-[460px]">
      <Tabs
        className="mb-4"
        value={side}
        onChange={(k) => setSide(k as OrderSide)}
        items={[
          { key: "buy", label: "买入" },
          { key: "sell", label: "卖出" },
        ]}
      />
      <div className="mb-3 flex gap-2 text-sm">
        {(["limit", "market"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setType(t)}
            className={`rounded-lb px-2.5 py-1 transition-colors ${
              t === type
                ? "bg-lb-card-2 text-lb-text"
                : "text-lb-text-2 hover:text-lb-text"
            }`}
          >
            {t === "limit" ? "限价" : "市价"}
          </button>
        ))}
      </div>

      <div className="space-y-3">
        {type === "limit" && (
          <Input
            label="价格 (USDT)"
            inputMode="decimal"
            placeholder={price ? fmtNum(price) : "0.00"}
            value={priceInput}
            onChange={(e) => setPriceInput(e.target.value)}
          />
        )}
        <Input
          label="数量"
          inputMode="decimal"
          placeholder="0.00"
          value={qty}
          onChange={(e) => setQty(e.target.value)}
        />
      </div>

      {msg && <p className="mt-3 text-xs text-lb-text-2">{msg}</p>}

      <Button
        className="mt-4"
        block
        variant={side === "buy" ? "primary" : "danger"}
        disabled={submitting}
        onClick={submit}
      >
        {!isAuthed
          ? "登录 / 注册"
          : side === "buy"
            ? `买入 ${symbol}`
            : `卖出 ${symbol}`}
      </Button>
    </Card>
  );
}
