"use client";
import Link from "next/link";
import { useState } from "react";
import { useApi, type Daily, type Summary } from "@/lib/api";
import { DAILY, ME, STATS, TIERS } from "@/lib/rules";
import { Coin, DailyChart, Demo, ErrBox, Skel, num, pct } from "./ui";

// 网关不可达时的演示回退
const demoSum = (): Summary => { const i = TIERS.findIndex((t) => t.key === ME.tier); const c = TIERS[i], n = TIERS[i + 1];
  return { tier: c.key, tierName: c.name, futuresRate: String(c.futuresRate), spotRate: String(c.spotRate), volume30d: String(ME.volume30d), nextTier: n ? { tier: n.key, name: n.name, minVolume30d: String(n.minVolume30d), gap: String(n.minVolume30d - ME.volume30d) } : null, directs: STATS.directs, directPartners: STATS.subPartners, unsettledEstimate: "0", yesterday: String(STATS.today), month: String(STATS.month), total: String(STATS.total), overrideTotal: "0", pendingPayout: "0", payoutAsset: "USDT" }; };
const demoDaily = (): Daily => ({ days: 3, items: DAILY.map((d) => ({ day: d.date, futures: String(d.futures), spot: String(d.spot), override: String(d.override), referral: "0", total: String(d.total) })) });

export default function Home() {
  const [days, setDays] = useState(30);
  const s = useApi<Summary>("/partner/summary", demoSum);
  const d = useApi<Daily>(`/partner/commissions/daily?days=${days}`, demoDaily);
  if (s.err) return <><h1>合伙人总览</h1><ErrBox err={s.err} reload={s.reload} /></>;
  const S = s.data;
  const prog = S?.nextTier ? Math.min(100, (+S.volume30d / +S.nextTier.minVolume30d) * 100) : 100;
  return (
    <>
      <h1>合伙人总览 {S && <span className="tag">{S.tierName}</span>} <Demo on={s.demo} /></h1>
      <div className="g4">
        {!S ? <Skel n={4} h={92} /> : [["昨日佣金", S.yesterday], ["本月佣金", S.month], ["累计佣金", S.total], ["待结算（预估）", S.unsettledEstimate]].map(([a, b]) => (
          <div key={a} className="card stat"><small>{a}</small><b><Coin s={S.payoutAsset} size={22} /> {num(b, 4)} <em>{S.payoutAsset}</em></b></div>
        ))}
      </div>
      <div className="g4" style={{ marginTop: 16 }}>
        {S && [["直属用户", String(S.directs)], ["下级合伙人", String(S.directPartners)], ["二级奖励累计", num(S.overrideTotal, 4)], ["待发放", num(S.pendingPayout, 4)]].map(([a, b]) => (
          <div key={a} className="card stat sm"><small>{a}</small><b>{b}</b></div>
        ))}
      </div>
      <h2>当前档位与升级进度</h2>
      {!S ? <Skel /> : <div className="card">
        <div>当前：<b className="lime">{S.tierName}</b> · 合约返佣 {pct(S.futuresRate)} · 现货返佣 {pct(S.spotRate)}</div>
        <div className="muted">直属用户近 30 日合约交易量 {num(S.volume30d)} USDT</div>
        <div className="bar"><i style={{ width: `${prog}%` }} /></div>
        {S.nextTier ? <div className="muted">距 {S.nextTier.name}（≥ {num(S.nextTier.minVolume30d)} USDT）还差 {num(S.nextTier.gap)} USDT</div> : <div className="muted">已达最高档位</div>}
      </div>}
      <h2 className="row">每日佣金 <span className="sp" />{[7, 30, 90].map((n) => <a key={n} className={"chip" + (days === n ? " on" : "")} onClick={() => setDays(n)}>近 {n} 天</a>)}<Demo on={d.demo} /></h2>
      {d.loading ? <Skel h={240} /> : d.err ? <ErrBox err={d.err} reload={d.reload} /> : <DailyChart items={d.data?.items || []} />}
      <p className="muted">佣金每日结算，发放至资金账户。<Link className="lime" href="/commission">查看明细 →</Link></p>
    </>
  );
}
