import { OVERRIDE, RULE_NOTES, TIERS, fmtU, pct } from "@/lib/rules";
export default function Apply() {
  return (
    <>
      <h1>合伙人等级与规则</h1>
      <div className="g4">
        {TIERS.map((t) => (
          <div key={t.key} className="card">
            <b className="lime">{t.name}</b>
            <small>{t.minVolume30d ? `直属 30 日合约量 ≥ ${fmtU(t.minVolume30d)} USDT` : "提交申请，审核通过"}</small>
            <div>合约返佣 <b>{pct(t.futuresRate)}</b></div>
            <div>现货返佣 <b>{pct(t.spotRate)}</b></div>
          </div>
        ))}
      </div>
      <h2>二级奖励</h2>
      <div className="card">上级额外获得下级合伙人佣金的 <b className="lime">{pct(OVERRIDE.rate)}</b>（{OVERRIDE.fundedBy}出资），最多 {OVERRIDE.maxLevels} 级。</div>
      <h2>规则说明</h2>
      <ol>{RULE_NOTES.map((r) => <li key={r}>{r}</li>)}</ol>
      <h2>申请成为合伙人</h2>
      <div className="card" style={{ maxWidth: 480 }}>
        <input placeholder="linbit UID" defaultValue="80000001" />
        <input placeholder="Telegram / 联系方式" />
        <input placeholder="推广渠道与预估用户规模" />
        <button className="btn">提交申请（演示）</button>
      </div>
    </>
  );
}
