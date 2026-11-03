"use client";
/**
 * 行情数据层：REST 快照 + WebSocket 增量（docs/api/websocket.md）。
 * - 单例连接、多频道订阅；断线指数退避重连并自动重新订阅。
 * - 客户端 20s ping，响应服务端 ping；60s 无帧主动断开重连。
 * - REST/WS 均不可用时返回 source="mock"，由调用方回退演示数据。
 */
import { useEffect, useRef, useState } from "react";
import { API_BASE } from "./api";

export type Source = "api" | "mock" | "loading";
export type SymbolMeta = { symbol: string; base: string; quote: string; pricePrecision: number; quantityPrecision: number; minQuantity?: string; status?: string; tags?: string[] };
export type Ticker = { symbol: string; lastPrice: string; priceChangePercent: string; high: string; low: string; volume: string; quoteVolume: string; priceSource?: "trade" | "mid" | "index" | "mark" | "none" };
export type Level = [string, string];
export type Book = { lastUpdateId: number; bids: Level[]; asks: Level[] };
export type Trade = { tradeId: string; price: string; quantity: string; side: "buy" | "sell"; time: number };
export type Bar = { time: number; open: number; high: number; low: number; close: number; v: number; src?: string };

export function restBase(): string {
  return API_BASE.replace(/\/$/, "");
}
export const WS_DEFAULT = "wss://api.cowa.click/ws";
/** 行情 WS 地址：始终为绝对 wss://；NEXT_PUBLIC_WS_URL 仅在是绝对 wss:// 时生效（www/partner 同源 /ws 为 404）。 */
export function wsUrl(): string {
  const env = process.env.NEXT_PUBLIC_WS_URL ?? "";
  return /^wss:\/\//.test(env) ? env : WS_DEFAULT;
}

async function rest<T>(path: string): Promise<T> {
  const r = await fetch(restBase() + path, { cache: "no-store" });
  if (!r.ok) throw new Error("HTTP " + r.status);
  return (await r.json()) as T;
}

// ---------------- WebSocket 单例 ----------------
type Handler = (data: any, frame: any) => void;
class MarketWS {
  ws: WebSocket | null = null;
  subs = new Map<string, Set<Handler>>();
  status: "idle" | "connecting" | "open" | "closed" = "idle";
  statusListeners = new Set<(s: string) => void>();
  retry = 0;
  pingT: any = 0;
  idleT: any = 0;
  reT: any = 0;
  seq = 0;

  setStatus(s: MarketWS["status"]) {
    this.status = s;
    this.statusListeners.forEach((f) => f(s));
  }
  connect() {
    if (typeof window === "undefined" || this.ws || this.status === "connecting") return;
    const url = wsUrl();
    if (!url) return;
    this.setStatus("connecting");
    let ws: WebSocket;
    try {
      ws = new WebSocket(url);
    } catch {
      this.schedule();
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      this.retry = 0;
      this.setStatus("open");
      const ch = [...this.subs.keys()];
      if (ch.length) this.send({ op: "subscribe", args: ch });
      clearInterval(this.pingT);
      this.pingT = setInterval(() => this.send({ op: "ping" }), 20_000);
      this.bump();
    };
    ws.onmessage = (e) => {
      this.bump();
      let m: any;
      try {
        m = JSON.parse(e.data);
      } catch {
        return;
      }
      if (m.event === "ping") return this.send({ op: "pong" });
      if (m.event) return;
      if (m.channel && this.subs.has(m.channel)) this.subs.get(m.channel)!.forEach((h) => h(m.data, m));
    };
    ws.onclose = ws.onerror = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      clearInterval(this.pingT);
      clearTimeout(this.idleT);
      try { ws.close(); } catch {}
      this.setStatus("closed");
      this.schedule();
    };
  }
  bump() {
    clearTimeout(this.idleT);
    this.idleT = setTimeout(() => this.ws?.close(), 60_000);
  }
  schedule() {
    clearTimeout(this.reT);
    if (!this.subs.size) return;
    const d = Math.min(15_000, 1000 * 2 ** this.retry++);
    this.reT = setTimeout(() => this.connect(), d);
  }
  send(o: any) {
    if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify({ id: "c" + ++this.seq, ...o }));
  }
  subscribe(channel: string, h: Handler) {
    let set = this.subs.get(channel);
    if (!set) {
      set = new Set();
      this.subs.set(channel, set);
      this.send({ op: "subscribe", args: [channel] });
    }
    set.add(h);
    this.connect();
    return () => {
      set!.delete(h);
      if (!set!.size) {
        this.subs.delete(channel);
        this.send({ op: "unsubscribe", args: [channel] });
      }
    };
  }
}
export const marketWS = new MarketWS();

export function useWsStatus() {
  const [s, setS] = useState(marketWS.status);
  useEffect(() => {
    const f = (x: any) => setS(x);
    marketWS.statusListeners.add(f);
    return () => void marketWS.statusListeners.delete(f);
  }, []);
  return s;
}

// ---------------- 精度 / 格式化 ----------------
const DEFAULT_META: Record<string, SymbolMeta> = {
  BTCUSDT: { symbol: "BTCUSDT", base: "BTC", quote: "USDT", pricePrecision: 2, quantityPrecision: 6 },
  ETHUSDT: { symbol: "ETHUSDT", base: "ETH", quote: "USDT", pricePrecision: 2, quantityPrecision: 5 },
};
let metaCache: Record<string, SymbolMeta> | null = null;
let metaP: Promise<Record<string, SymbolMeta>> | null = null;
export function loadSymbols() {
  if (metaCache) return Promise.resolve(metaCache);
  metaP ??= rest<SymbolMeta[]>("/market/symbols")
    .then((l) => (metaCache = Object.fromEntries(l.map((s) => [s.symbol, s]))))
    .catch(() => {
      metaP = null;
      return DEFAULT_META;
    });
  return metaP;
}
export function useSymbols() {
  const [m, setM] = useState<Record<string, SymbolMeta>>(metaCache ?? DEFAULT_META);
  useEffect(() => void loadSymbols().then(setM), []);
  return m;
}
export function metaOf(m: Record<string, SymbolMeta>, sym: string): SymbolMeta {
  return m[sym] ?? DEFAULT_META[sym] ?? { symbol: sym, base: sym.replace(/USDT$/, ""), quote: "USDT", pricePrecision: 2, quantityPrecision: 6 };
}
/** 按交易对精度定点格式化（千分位），非数值返回 "--"。 */
export function fmtFixed(v: string | number | undefined | null, digits: number, group = true): string {
  const n = typeof v === "string" ? Number(v) : v;
  if (n === undefined || n === null || !Number.isFinite(n)) return "--";
  return group ? n.toLocaleString("en-US", { minimumFractionDigits: digits, maximumFractionDigits: digits }) : n.toFixed(digits);
}

// ---------------- Hooks ----------------
/** 单交易对 24h ticker：REST 快照 + ticker 频道。 */
export function useTicker(symbol: string) {
  const [t, setT] = useState<Ticker | null>(null);
  const [src, setSrc] = useState<Source>("loading");
  useEffect(() => {
    if (!symbol) return;
    let live = true;
    rest<Ticker[] | Ticker>(`/market/ticker?symbol=${symbol}`)
      .then((d) => { if (!live) return; const x = Array.isArray(d) ? d[0] : d; if (x) setT(x); setSrc("api"); })
      .catch(() => live && setSrc((s) => (s === "api" ? s : "mock")));
    const un = marketWS.subscribe(`ticker:${symbol}`, (d) => { setT(d); setSrc("api"); });
    return () => { live = false; un(); };
  }, [symbol]);
  return { ticker: t, source: src };
}

/**
 * 全部交易对 ticker：模块级共享 store（所有组件共用一个轮询 + 一组 WS 订阅）。
 * - WS 在线时不轮询；WS 不在线时 REST 兜底，间隔 30s（>=5s）。
 * - 页面隐藏（document.hidden）时暂停轮询，回到前台立即补拉一次再恢复。
 */
const TICKER_POLL_MS = 30_000;
const tickerStore = {
  map: {} as Record<string, Ticker>,
  src: "loading" as Source,
  subs: new Set<() => void>(),
  wsUns: new Map<string, () => void>(),
  timer: null as ReturnType<typeof setInterval> | null,
  inflight: false,
  emit() { this.subs.forEach((f) => f()); },
  load() {
    if (this.inflight) return;
    this.inflight = true;
    rest<Ticker[]>("/market/ticker")
      .then((l) => {
        this.map = { ...Object.fromEntries(l.map((x) => [x.symbol, x])), ...this.map };
        this.src = "api";
        l.forEach((x) => {
          const ch = `ticker:${x.symbol}`;
          if (!this.wsUns.has(ch)) this.wsUns.set(ch, marketWS.subscribe(ch, (d) => { this.map = { ...this.map, [d.symbol]: d }; this.emit(); }));
        });
        this.emit();
      })
      .catch(() => { if (this.src !== "api") { this.src = "mock"; this.emit(); } })
      .finally(() => { this.inflight = false; });
  },
  schedule() {
    const want = this.subs.size > 0 && typeof document !== "undefined" && !document.hidden && marketWS.status !== "open";
    if (want && !this.timer) this.timer = setInterval(() => this.load(), TICKER_POLL_MS);
    if (!want && this.timer) { clearInterval(this.timer); this.timer = null; }
  },
  onVis: () => { if (!document.hidden && tickerStore.subs.size) tickerStore.load(); tickerStore.schedule(); },
  onWs: () => tickerStore.schedule(),
  add(f: () => void) {
    if (this.subs.size === 0) {
      document.addEventListener("visibilitychange", this.onVis);
      marketWS.statusListeners.add(this.onWs);
      this.load();
    }
    this.subs.add(f);
    this.schedule();
    return () => {
      this.subs.delete(f);
      if (this.subs.size === 0) {
        document.removeEventListener("visibilitychange", this.onVis);
        marketWS.statusListeners.delete(this.onWs);
        this.wsUns.forEach((u) => u()); this.wsUns.clear();
      }
      this.schedule();
    };
  },
};
export function useTickers() {
  const [, force] = useState(0);
  useEffect(() => tickerStore.add(() => force((x) => x + 1)), []);
  return { tickers: tickerStore.map, source: tickerStore.src };
}

function applyLevels(side: Level[], upd: Level[], desc: boolean): Level[] {
  const m = new Map(side.map((l) => [l[0], l]));
  for (const [p, q] of upd) Number(q) === 0 ? m.delete(p) : m.set(p, [p, q]);
  return [...m.values()].sort((a, b) => (desc ? Number(b[0]) - Number(a[0]) : Number(a[0]) - Number(b[0])));
}
/** 订单簿：REST 快照 + depth 频道（订阅首帧为全量，其后为增量，量 0 删除价位）。 */
export function useOrderBook(symbol: string, limit = 50) {
  const [book, setBook] = useState<Book | null>(null);
  const [src, setSrc] = useState<Source>("loading");
  const first = useRef(true);
  const status = useWsStatus();
  useEffect(() => { if (status !== "open") first.current = true; }, [status]);
  useEffect(() => {
    if (!symbol) return;
    let live = true;
    first.current = true;
    rest<Book>(`/market/depth?symbol=${symbol}&limit=${limit}`)
      .then((d) => { if (!live) return; setBook((b) => (b && b.lastUpdateId >= d.lastUpdateId ? b : d)); setSrc("api"); })
      .catch(() => live && setSrc((s) => (s === "api" ? s : "mock")));
    const un = marketWS.subscribe(`depth:${symbol}`, (d) => {
      setSrc("api");
      if (first.current) {
        first.current = false;
        setBook({ lastUpdateId: d.lastUpdateId, bids: applyLevels([], d.bids || [], true), asks: applyLevels([], d.asks || [], false) });
        return;
      }
      setBook((b) => {
        if (!b) return { lastUpdateId: d.lastUpdateId, bids: applyLevels([], d.bids || [], true), asks: applyLevels([], d.asks || [], false) };
        if (d.lastUpdateId && d.lastUpdateId <= b.lastUpdateId) return b;
        return { lastUpdateId: d.lastUpdateId ?? b.lastUpdateId, bids: applyLevels(b.bids, d.bids || [], true), asks: applyLevels(b.asks, d.asks || [], false) };
      });
    });
    return () => { live = false; un(); };
  }, [symbol, limit]);
  return { book, source: src };
}

/** 近期成交：REST + trades 频道。 */
export function useRecentTrades(symbol: string, limit = 30) {
  const [list, setList] = useState<Trade[]>([]);
  const [src, setSrc] = useState<Source>("loading");
  useEffect(() => {
    if (!symbol) return;
    let live = true;
    const merge = (a: Trade[], b: Trade[]) => {
      const m = new Map<string, Trade>();
      [...a, ...b].forEach((x) => m.set(x.tradeId, x));
      return [...m.values()].sort((x, y) => y.time - x.time || Number(y.tradeId) - Number(x.tradeId)).slice(0, limit);
    };
    rest<Trade[]>(`/market/trades?symbol=${symbol}&limit=${limit}`)
      .then((l) => { if (!live) return; setList((c) => merge(c, l)); setSrc("api"); })
      .catch(() => live && setSrc((s) => (s === "api" ? s : "mock")));
    const un = marketWS.subscribe(`trades:${symbol}`, (d) => { setSrc("api"); setList((c) => merge([d], c)); });
    return () => { live = false; un(); };
  }, [symbol, limit]);
  return { trades: list, source: src };
}

/** K 线 feed（供 @linbit/chart KLinePro）：load 返回 null 表示不可用→图表回退演示数据。 */
type RawK = { time?: number; openTime?: number; open: string; high: string; low: string; close: string; volume: string; source?: string };
const toBar = (k: RawK): Bar => ({ time: Number(k.time ?? k.openTime), open: +k.open, high: +k.high, low: +k.low, close: +k.close, v: +k.volume, src: k.source });
export function klineFeed(symbol: string) {
  return {
    async load(interval: string): Promise<Bar[] | null> {
      try {
        const l = await rest<RawK[]>(`/market/klines?symbol=${symbol}&interval=${interval}&limit=300`);
        return l.map(toBar).map((b) => (b.time > 1e12 ? { ...b, time: Math.floor(b.time / 1000) } : b)).sort((a, b) => a.time - b.time);
      } catch {
        return null;
      }
    },
    subscribe(interval: string, cb: (b: Bar) => void) {
      return marketWS.subscribe(`kline:${symbol}:${interval}`, (d) => {
        const b = toBar(d);
        cb(b.time > 1e12 ? { ...b, time: Math.floor(b.time / 1000) } : b);
      });
    },
  };
}
