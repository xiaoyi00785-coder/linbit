import { DAILY, ME, STATS, TIERS, fmtU, pct } from "@/lib/rules";

export default function Home() {
  const idx = TIERS.findIndex((t) => t.key === ME.tier);
  const cur = TIERS[idx];
  const next = TIERS[idx + 1];
  return (
    <>
      <h1>合伙人总览 <span className="tag">{cur.name}</span></h1>
      <div className="g4">
        {[["今日佣金 (USDT)", STATS.today], ["本月佣金", STATS.month], ["累计佣金", STATS.total], ["直属用户 / 下级合伙人", `${STATS.directs} / ${STATS.subPartners}`]].map(([a, b]) => (
          <div key={a as string} className="card"><small>{a}</small><b>{typeof b === "number" ? fmtU(b) : b}</b></div>
        ))}
      </div>
      <h2>当前档位与升级进度</h2>
      <div className="card">
        <div>当前：<b className="lime">{cur.name}</b> · 合约返佣 {pct(cur.futuresRate)} · 现货返佣 {pct(cur.spotRate)}</div>
        <div className="muted">直属用户近 30 日合约交易量 {fmtU(ME.volume30d)} USDT</div>
        {next && <>
          <div className="bar"><i style={{ width: `${Math.min(100, (ME.volume30d / next.minVolume30d) * 100)}%` }} /></div>
          <div className="muted">距 {next.name}（≥ {fmtU(next.minVolume30d)} USDT）还差 {fmtU(next.minVolume30d - ME.volume30d)} USDT</div>
        </>}
      </div>
      <h2>近期每日结算</h2>
      <table><thead><tr><th>日期</th><th>合约佣金</th><th>现货佣金</th><th>二级奖励</th><th>合计（已发放至资金账户）</th></tr></thead>
        <tbody>{DAILY.map((d) => <tr key={d.date}><td>{d.date}</td><td>{d.futures}</td><td>{d.spot}</td><td>{d.override}</td><td className="lime">{d.total}</td></tr>)}</tbody></table>
    </>
  );
}
