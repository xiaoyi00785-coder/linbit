import { TEAM, fmtU } from "@/lib/rules";
export default function Team() {
  return (
    <>
      <h1>我的团队</h1>
      <p className="muted">仅展示直属用户与直属下级合伙人；二级奖励最多计算 2 级。</p>
      <table><thead><tr><th>UID</th><th>类型</th><th>注册时间</th><th>近 30 日合约交易量</th><th>近 30 日手续费</th></tr></thead>
        <tbody>{TEAM.map((t) => <tr key={t.uid}><td>{t.uid}</td><td>{t.type}</td><td>{t.joined}</td><td>{fmtU(t.vol30d)}</td><td>{fmtU(t.fee30d)}</td></tr>)}</tbody></table>
    </>
  );
}
