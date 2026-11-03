"use client";
import Link from "next/link";
import { useState } from "react";
import type { Res } from "@/lib/api";

export const Demo = ({ on }: { on: boolean }) => (on ? <span className="demo" title="网关不可达，显示演示数据">演示数据</span> : null);
export const Skel = ({ h = 120, n = 1 }: { h?: number; n?: number }) => <>{Array.from({ length: n }, (_, i) => <div key={i} className="skel" style={{ height: h }} />)}</>;
export const num = (v: string | number | undefined, d = 2) => Number(v || 0).toLocaleString("en-US", { maximumFractionDigits: d });
export const pct = (v: string | number | undefined) => `${+(Number(v || 0) * 100).toFixed(2)}%`;
export const day = (ms: number) => new Date(ms).toLocaleDateString("sv-SE");

/** 403 NOT_PARTNER 等错误状态 */
export function ErrBox({ err, reload }: { err?: Partial<Res<unknown>>; reload?: () => void }) {
  if (!err) return null;
  if (err.code === "NOT_PARTNER") return <div className="card empty fade">您还不是合伙人。<Link className="lime" href="/apply">查看规则并申请 →</Link></div>;
  return <div className="card empty">{err.error || "加载失败"} {reload && <a className="lime" onClick={reload}>重试</a>}</div>;
}

export function Tabs<T extends string>({ items, cur, set }: { items: [T, string][]; cur: T; set: (t: T) => void }) {
  return <div className="tabs">{items.map(([k, t]) => <b key={k} className={cur === k ? "on" : ""} onClick={() => set(k)}>{t}</b>)}</div>;
}

/** 每日佣金堆叠柱状图（合约 / 现货 / 二级奖励 / 邀请），hover 显示明细 */
export function DailyChart({ items }: { items: { day: string; futures: string; spot: string; override: string; referral: string; total: string }[] }) {
  const [h, setH] = useState<number | null>(null);
  const rows = [...items].sort((a, b) => a.day.localeCompare(b.day));
  const max = Math.max(1, ...rows.map((r) => +r.total));
  const K: [keyof (typeof rows)[0], string, string][] = [["futures", "合约", "#abe127"], ["spot", "现货", "#6fa8ff"], ["override", "二级奖励", "#f0b90b"], ["referral", "邀请", "#b48cff"]];
  if (!rows.length) return <div className="card empty">暂无结算数据</div>;
  const W = 100 / rows.length;
  return (
    <div className="card chart">
      <div className="legend">{K.map(([, n, c]) => <span key={n}><i style={{ background: c }} />{n}</span>)}</div>
      <div className="bars" onMouseLeave={() => setH(null)}>
        {rows.map((r, i) => {
          let acc = 0;
          return (
            <div key={r.day} className={"col" + (h === i ? " on" : "")} style={{ width: `${W}%` }} onMouseEnter={() => setH(i)}>
              {K.map(([k, , c]) => { const v = +r[k]; const el = <i key={k} style={{ height: `${(v / max) * 100}%`, bottom: `${(acc / max) * 100}%`, background: c }} />; acc += v; return el; })}
              {h === i && <div className="tip"><b>{r.day}</b>{K.map(([k, n]) => <div key={n}><span>{n}</span><span>{num(r[k], 4)}</span></div>)}<div><span>合计</span><b className="lime">{num(r.total, 4)}</b></div></div>}
            </div>
          );
        })}
      </div>
      <div className="axis"><span>{rows[0].day}</span><span>{rows[rows.length - 1].day}</span></div>
    </div>
  );
}

/** 真实币种 logo（public/coins），未知币种回退字母徽标 */
export function Coin({ s, size = 16 }: { s: string; size?: number }) {
  const [bad, setBad] = useState(false);
  return bad ? <i className="coin fb" style={{ width: size, height: size, fontSize: size * 0.55 }}>{s[0]}</i> : <img className="coin" src={`/coins/${s.toLowerCase()}.svg`} alt={s} width={size} height={size} onError={() => setBad(true)} />;
}
