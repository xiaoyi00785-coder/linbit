"use client";
/**
 * 行情卡片迷你走势（sparkline）的真实数据：GET /market/klines?interval=1h&limit=24（近 24 小时每小时收盘价）。
 * 主网规则：只用真实接口，取不到 / 不足 2 根 / 数据异常时返回 null，调用方不画线，绝不伪造走势。
 * 按交易对缓存 5 分钟并合并并发请求，同一屏多张卡片 / 多次渲染不会反复请求。
 */
import { useEffect, useState } from "react";
import { rget } from "./rest";
import { sparkPoints, type RawK } from "./sparkLine";

const TTL = 5 * 60_000;
const cache = new Map<string, { at: number; pts: number[] | null }>();
const inflight = new Map<string, Promise<number[] | null>>();

export function loadSpark(symbol: string): Promise<number[] | null> {
  const hit = cache.get(symbol);
  if (hit && Date.now() - hit.at < TTL) return Promise.resolve(hit.pts);
  const p = inflight.get(symbol);
  if (p) return p;
  const q = rget<RawK[]>(`/market/klines?symbol=${encodeURIComponent(symbol)}&interval=1h&limit=24`)
    .then((r) => {
      const pts = r.ok ? sparkPoints(r.data) : null;
      // 接口失败不缓存（下次渲染可重试）；成功但无数据缓存，避免对无成交的交易对反复请求
      if (r.ok) cache.set(symbol, { at: Date.now(), pts });
      return pts;
    })
    .catch(() => null)
    .finally(() => { inflight.delete(symbol); });
  inflight.set(symbol, q);
  return q;
}

export function useSpark(symbol: string | undefined): number[] | null {
  const [pts, setPts] = useState<number[] | null>(null);
  useEffect(() => {
    let live = true;
    setPts(null);
    if (symbol) loadSpark(symbol).then((p) => { if (live) setPts(p); });
    return () => { live = false; };
  }, [symbol]);
  return pts;
}
