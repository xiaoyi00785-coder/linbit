import type { Candle } from "@linbit/chart";

/**
 * 当 mock / 后端不可用或 K 线为空时，生成占位蜡烛数据，保证图表可渲染。
 * 正式环境数据应来自 api-client 的 /market/klines。
 */
export function genCandles(count = 160, end = 76768): Candle[] {
  const intervalSec = 900; // 15m
  const start = Math.floor(Date.now() / 1000) - count * intervalSec;
  let price = end * 1.03;
  const out: Candle[] = [];
  for (let i = 0; i < count; i++) {
    const open = price;
    const close = open * (1 + (Math.random() - 0.52) * 0.004);
    out.push({
      time: start + i * intervalSec,
      open,
      close,
      high: Math.max(open, close) * (1 + Math.random() * 0.0015),
      low: Math.min(open, close) * (1 - Math.random() * 0.0015),
      volume: Math.random() * 8,
    });
    price = close;
  }
  const k = end / price;
  return out.map((c) => ({
    ...c,
    open: c.open * k,
    high: c.high * k,
    low: c.low * k,
    close: c.close * k,
  }));
}
