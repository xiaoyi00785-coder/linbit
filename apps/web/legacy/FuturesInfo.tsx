"use client";
import { useEffect, useMemo, useState } from "react";
import { rget } from "@/lib/rest";
import type { paths } from "@linbit/api-client";

/** 合约数据页（仓位档位 / 逐仓参数 / 标记价格历史 / 指数价格 / 资金费率历史 / 保险基金）— 真实公共接口，无需登录 */
export const FUTURES_INFO = ["仓位档位", "逐仓参数", "标记价格历史", "指数价格", "资金费率历史", "保险基金"] as const;

/** 响应类型取自 packages/api-client/src/schema.ts（OpenAPI 生成）。 */
type R<K extends keyof paths> = paths[K] extends { get: { responses: { 200: { content: { "application/json": infer J } } } } } ? J : never;
type St<T> = { loading: boolean; data?: T; error?: string };

const P = "/futures/market";
const num = (v: unknown, d = 2) => { const n = Number(v); return Number.isFinite(n) ? n.toLocaleString("en-US", { maximumFractionDigits: d, minimumFractionDigits: 0 }) : "--"; };
const big = (v?: string) => (Number(v) >= 1e17 ? "∞" : num(v, 0));
const pct = (v: unknown, d = 4) => { const n = Number(v); return Number.isFinite(n) ? (n * 100).toFixed(d) + "%" : "--"; };
const ts = (ms?: number) => { if (ms == null) return "--"; const d = new Date(ms); const p = (x: number) => String(x).padStart(2, "0"); return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`; };

function useGet<T>(path: string | null, nonce = 0): St<T> & { reload: () => void } {
  const [st, set] = useState<St<T>>({ loading: !!path });
  const [n, setN] = useState(0);
  useEffect(() => {
    if (!path) return; let live = true;
    set((s) => ({ ...s, loading: true, error: undefined }));
    rget<T>(path).then((r) => { if (live) set(r.ok ? { loading: false, data: r.data } : { loading: false, error: r.error || "加载失败" }); });
    return () => { live = false; };
  }, [path, n, nonce]);
  return { ...st, reload: () => setN((x) => x + 1) };
}

function State({ st, empty, children }: { st: St<unknown> & { reload: () => void }; empty: boolean; children: React.ReactNode }) {
  if (st.loading && !st.data) return <div className="fi-state"><span className="fi-spin" />加载中…</div>;
  if (st.error) return <div className="fi-state err">数据加载失败：{st.error}<button className="fi-btn" onClick={st.reload}>重试</button></div>;
  if (empty) return <div className="fi-state"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.4"><path d="M3 13l2.5-7h13L21 13v6H3z" /><path d="M3 13h5l1 2h6l1-2h5" /></svg>暂无数据</div>;
  return <div style={{ opacity: st.loading ? 0.5 : 1 }}>{children}</div>;
}

function Symbols({ list, v, on }: { list: string[]; v: string; on: (s: string) => void }) {
  return <div className="fi-sym">{list.map((s) => <button key={s} className={"fi-chip" + (s === v ? " on" : "")} onClick={() => on(s)}>{s}<span>永续</span></button>)}</div>;
}

/** 基于 nextEndTime 的游标分页：保存每页的 endTime 栈 */
function usePager(dep: string) {
  const [stack, setStack] = useState<(number | undefined)[]>([undefined]);
  useEffect(() => setStack([undefined]), [dep]);
  const end = stack[stack.length - 1];
  return { end, page: stack.length, next: (e?: number) => e && setStack((s) => [...s, e]), prev: () => setStack((s) => (s.length > 1 ? s.slice(0, -1) : s)) };
}
function Pager({ pg, next }: { pg: ReturnType<typeof usePager>; next?: number }) {
  return <div className="fi-pager"><button className="fi-btn" disabled={pg.page <= 1} onClick={pg.prev}>上一页</button><span>第 {pg.page} 页</span><button className="fi-btn" disabled={!next} onClick={() => pg.next(next)}>下一页</button></div>;
}

function Spark({ pts }: { pts: number[] }) {
  if (pts.length < 2) return null;
  const lo = Math.min(...pts), hi = Math.max(...pts), w = 1000, h = 160, k = hi - lo || 1;
  const d = pts.map((p, i) => `${i ? "L" : "M"}${(i / (pts.length - 1)) * w},${h - ((p - lo) / k) * (h - 10) - 5}`).join("");
  return <svg className="fi-spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none"><path d={d} fill="none" stroke="#abe127" strokeWidth="2" vectorEffect="non-scaling-stroke" /></svg>;
}

const LIM = 20;

export function FuturesInfo({ t }: { t: string }) {
  const cs = useGet<R<"/futures/market/contracts">>(P + "/contracts");
  const syms = useMemo(() => (cs.data?.contracts || []).map((c) => c.symbol).filter((x): x is string => !!x), [cs.data]);
  const [sym, setSym] = useState("BTCUSDT");
  useEffect(() => { if (syms.length && !syms.includes(sym)) setSym(syms[0]); }, [syms, sym]);
  const needSym = t !== "保险基金";
  return <div className="fi">
    {needSym && (syms.length ? <Symbols list={syms} v={sym} on={setSym} /> : cs.error ? null : <div className="fi-sym"><span className="fi-chip sk" /><span className="fi-chip sk" /></div>)}
    {t === "仓位档位" && <Tiers sym={sym} />}
    {t === "逐仓参数" && <Isolated sym={sym} />}
    {t === "标记价格历史" && <Mark sym={sym} />}
    {t === "指数价格" && <Index sym={sym} />}
    {t === "资金费率历史" && <Funding sym={sym} />}
    {t === "保险基金" && <Insurance />}
  </div>;
}

function Tiers({ sym }: { sym: string }) {
  const st = useGet<R<"/futures/market/risk-tiers">>(`${P}/risk-tiers?symbol=${sym}`);
  const tiers = st.data?.contracts?.[0]?.tiers || [];
  return <section className="box pgc"><h3>{sym} 永续 · 仓位档位</h3><State st={st} empty={!tiers.length}>
    <table className="pgt"><thead><tr><th>档位</th><th>仓位价值 (USDT)</th><th>最高杠杆</th><th>维持保证金率</th><th>维持保证金速算额 (USDT)</th></tr></thead>
      <tbody>{tiers.map((r) => <tr key={r.tier}><td>{r.tier}</td><td>{num(r.minNotional, 0)} – {big(r.maxNotional)}</td><td>{r.maxLeverage}x</td><td>{pct(r.maintenanceMarginRate, 2)}</td><td>{num(r.maintenanceAmount)}</td></tr>)}</tbody></table>
  </State></section>;
}

function Isolated({ sym }: { sym: string }) {
  const st = useGet<R<"/futures/market/isolated-params">>(`${P}/isolated-params?symbol=${sym}`);
  const c = st.data?.contracts?.[0];
  const rows: [string, string | undefined][] = c ? [
    ["保证金币种", c.marginAsset], ["保证金模式", (c.marginModes || []).map((m: string) => (m === "cross" ? "全仓" : "逐仓")).join(" / ")],
    ["默认杠杆", c.defaultLeverage + "x"], ["最高杠杆", c.maxLeverage + "x"], ["最小下单量", c.minQty], ["最大下单量", c.maxQty], ["价格精度", c.tickSize],
    ["Maker 费率", pct(c.makerFeeRate, 3)], ["Taker 费率", pct(c.takerFeeRate, 3)], ["基础维持保证金率", pct(c.maintenanceMarginRate, 2)],
    ["强平手续费率", pct(c.liquidationFeeRate, 2)], ["逐仓追加/减少保证金", c.marginAdjustable ? "支持" : "不支持"], ["资金费结算周期", c.fundingIntervalHours + " 小时"],
  ] : [];
  return <section className="box pgc"><h3>{sym} 永续 · 逐仓参数</h3><State st={st} empty={!c}>
    <div className="fi-kv">{rows.map(([k, v]) => <div key={k}><span>{k}</span><b>{v ?? "--"}</b></div>)}</div>
  </State></section>;
}

const IVS = ["", "1m", "5m", "15m", "1h", "4h", "1d"];
function Mark({ sym }: { sym: string }) {
  const [iv, setIv] = useState("1h");
  const pg = usePager(sym + iv);
  const st = useGet<R<"/futures/market/mark-history">>(`${P}/mark-history?symbol=${sym}&limit=${LIM}${iv ? "&interval=" + iv : ""}${pg.end ? "&endTime=" + pg.end : ""}`);
  const h = st.data?.history || [];
  const k = !!iv;
  const pts = [...h].reverse().map((r) => Number(k ? r.close : r.markPrice));
  return <section className="box pgc"><div className="fi-hd"><h3>{sym} 永续 · 标记价格历史</h3><div className="fi-tabs">{IVS.map((x) => <button key={x} className={x === iv ? "on" : ""} onClick={() => setIv(x)}>{x || "原始采样"}</button>)}</div></div>
    <State st={st} empty={!h.length}><Spark pts={pts} />
      <table className="pgt"><thead><tr><th>时间</th>{k ? <><th>开盘</th><th>最高</th><th>最低</th><th>收盘</th><th>指数收盘</th></> : <><th>标记价格</th><th>指数价格</th></>}</tr></thead>
        <tbody>{h.map((r) => <tr key={r.openTime ?? r.time}><td>{ts(r.openTime ?? r.time)}</td>{k ? <><td>{num(r.open)}</td><td>{num(r.high)}</td><td>{num(r.low)}</td><td>{num(r.close)}</td><td>{num(r.indexClose)}</td></> : <><td>{num(r.markPrice)}</td><td>{num(r.indexPrice)}</td></>}</tr>)}</tbody></table>
      <Pager pg={pg} next={st.data?.nextEndTime} /></State>
    {st.data?.sampleSeconds ? <p className="fi-note">采样间隔 {String(st.data.sampleSeconds)} 秒；时间为本地时区。</p> : null}</section>;
}

function Index({ sym }: { sym: string }) {
  const st = useGet<R<"/futures/market/index">>(`${P}/index?symbol=${sym}`);
  const d = st.data; const comps = d?.components || [];
  const has = d && Number(d.indexPrice) > 0;
  return <>
    <section className="box pgc"><h3>{sym} 指数价格</h3><State st={st} empty={!has}>
      <div className="fi-stats"><div><span>指数价格</span><b>{num(d?.indexPrice, 4)}</b></div><div><span>标记价格</span><b>{num(d?.markPrice, 4)}</b></div>
        <div><span>计算方式</span><b>{d?.method === "primary_failover" ? "主源优先 · 故障切换" : d?.method}</b></div><div><span>更新时间</span><b>{d?.indexUpdatedAt ? ts(d.indexUpdatedAt) : "--"}</b></div></div>
    </State></section>
    <section className="box pgc"><h3>成分交易所</h3><State st={st} empty={!comps.length}>
      <table className="pgt"><thead><tr><th>交易所</th><th>价格</th><th>权重</th><th>状态</th><th>更新时间</th></tr></thead>
        <tbody>{comps.map((c) => <tr key={c.exchange}><td>{c.exchange}</td><td>{c.price ? num(c.price, 4) : "--"}</td><td>{pct(c.weight, 0)}</td><td className={c.fresh ? "up" : "down"}>{c.fresh ? "正常" : "延迟"}</td><td>{c.updatedAt ? ts(c.updatedAt) : "--"}</td></tr>)}</tbody></table>
    </State><p className="fi-note">主源正常时权重为 100%；主源失效或数据过期时自动切换到备用源。</p></section>
  </>;
}

function Funding({ sym }: { sym: string }) {
  const pg = usePager(sym);
  const st = useGet<R<"/futures/market/funding-history">>(`${P}/funding-history?symbol=${sym}&limit=${LIM}${pg.end ? "&endTime=" + pg.end : ""}`);
  const h = st.data?.history || [];
  return <section className="box pgc"><h3>{sym} 永续 · 资金费率历史{st.data?.fundingIntervalHours ? <small> · 每 {String(st.data.fundingIntervalHours)} 小时结算</small> : null}</h3>
    <State st={st} empty={!h.length}>
      <table className="pgt"><thead><tr><th>结算时间</th><th>合约</th><th>资金费率</th><th>结算标记价格</th><th>来源</th></tr></thead>
        <tbody>{h.map((r) => <tr key={r.fundingTime}><td>{ts(r.fundingTime)}</td><td>{sym}</td><td className={Number(r.fundingRate) >= 0 ? "up" : "down"}>{pct(r.fundingRate)}</td><td>{Number(r.markPrice) ? num(r.markPrice) : "--"}</td><td>{SRC[r.source ?? ""] ?? "--"}</td></tr>)}</tbody></table>
      <Pager pg={pg} next={st.data?.nextEndTime} /></State></section>;
}

const SRC: Record<string, string> = { settle: "本平台结算", binance: "Binance 回填", okx: "OKX 回填" };

function Insurance() {
  const cur = useGet<R<"/futures/market/insurance-fund">>(`${P}/insurance-fund`);
  const pg = usePager("ins");
  const hs = useGet<R<"/futures/market/insurance-fund/history">>(`${P}/insurance-fund/history?limit=${LIM}${pg.end ? "&endTime=" + pg.end : ""}`);
  const h = hs.data?.history || []; const d = cur.data;
  return <>
    <section className="box pgc"><h3>当前余额</h3><State st={cur} empty={!d}>
      <div className="fi-stats"><div><span>币种</span><b>{d?.asset}</b></div><div><span>余额</span><b>{num(d?.balance)} {d?.asset}</b></div><div><span>权益</span><b>{num(d?.equity)} {d?.asset}</b></div><div><span>近期强平</span><b>{(d?.recentLiquidations || []).length} 笔</b></div></div>
    </State></section>
    <section className="box pgc"><h3>余额历史{hs.data?.snapshotMinutes ? <small> · 每 {String(hs.data.snapshotMinutes)} 分钟快照</small> : null}</h3>
      {!hs.loading && !hs.error && !h.length ? <div className="fi-state"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.4"><path d="M4 19V5M4 19h16M8 15l3-4 3 2 4-6" /></svg>暂无历史快照，系统将按周期自动记录余额</div> :
        <State st={hs} empty={false}><table className="pgt"><thead><tr><th>时间</th><th>币种</th><th>余额</th><th>权益</th></tr></thead>
          <tbody>{h.map((r) => <tr key={`${r.time}-${r.asset}`}><td>{ts(r.time)}</td><td>{r.asset}</td><td>{num(r.balance)}</td><td>{num(r.equity)}</td></tr>)}</tbody></table>
          <Pager pg={pg} next={hs.data?.nextEndTime} /></State>}
    </section>
  </>;
}
