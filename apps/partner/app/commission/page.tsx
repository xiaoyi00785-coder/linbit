import { DAILY } from "@/lib/rules";
export default function Commission() {
  return (
    <>
      <h1>佣金明细</h1>
      <p className="muted">佣金每日结算（UTC+8 次日）并发放至资金账户。二级奖励由平台出资。</p>
      <table><thead><tr><th>结算日</th><th>类型</th><th>来源</th><th>金额 (USDT)</th><th>状态</th></tr></thead>
        <tbody>{DAILY.flatMap((d) => [
          <tr key={d.date + "f"}><td>{d.date}</td><td>合约返佣</td><td>直属用户</td><td>{d.futures}</td><td className="lime">已发放</td></tr>,
          <tr key={d.date + "s"}><td>{d.date}</td><td>现货返佣</td><td>直属用户</td><td>{d.spot}</td><td className="lime">已发放</td></tr>,
          <tr key={d.date + "o"}><td>{d.date}</td><td>二级奖励（10%）</td><td>下级合伙人</td><td>{d.override}</td><td className="lime">已发放</td></tr>,
        ])}</tbody></table>
    </>
  );
}
