"use client";
import { useState } from "react";
import { call, toast, useApi, useToken, type Me, type Rules } from "@/lib/api";
import { OVERRIDE, RULE_NOTES, TIERS } from "@/lib/rules";
import { Demo, Skel, num, pct } from "../ui";

const demo = (): Rules => ({ tiers: TIERS.map((t) => ({ key: t.key, name: t.name, minVolume30d: String(t.minVolume30d), futuresRate: String(t.futuresRate), spotRate: String(t.spotRate) })), tierBasis: "直推用户近30日合约交易额", overrideRate: String(OVERRIDE.rate), overrideLevels: OVERRIDE.maxLevels, overrideFunding: "平台出资，不扣减下级合伙人佣金", settlement: "每日结算至资金账户", settleHourUtc: 0, payoutAsset: "USDT", validity: "终身有效", rebateBase: "实际支付手续费（不含体验金及手续费抵扣券抵扣部分）", autoApprove: false });
const ST: Record<string, string> = { pending: "审核中", active: "已是合伙人", rejected: "未通过", suspended: "已暂停" };

export default function Apply() {
  const tok = useToken();
  const r = useApi<Rules>("/partner/rules", demo);
  const me = useApi<Me>(tok ? "/partner/me" : null);
  const [f, setF] = useState({ contact: "", channel: "", scale: "" }); const [busy, setB] = useState(false);
  const R = r.data;
  const st = me.data?.partner?.status || (me.data?.status !== "none" ? me.data?.status : "");
  const submit = async () => {
    if (!f.contact.trim()) return toast("请填写联系方式", "err");
    setB(true); const x = await call("POST", "/partner/apply", f); setB(false);
    if (!x.ok) return toast(x.error || "提交失败", "err");
    toast(R?.autoApprove ? "申请已通过" : "申请已提交，请等待审核"); me.reload();
  };
  return (
    <>
      <h1>合伙人等级与规则 <Demo on={r.demo} /></h1>
      <div className="g4">
        {!R ? <Skel n={4} h={140} /> : R.tiers.map((t) => (
          <div key={t.key} className={"card tier" + (me.data?.tier === t.key ? " on" : "")}>
            <b className="lime">{t.name}</b>
            <small>{+t.minVolume30d ? `${R.tierBasis} ≥ ${num(t.minVolume30d)} USDT` : "提交申请，审核通过"}</small>
            <div>合约返佣 <b>{pct(t.futuresRate)}</b></div>
            <div>现货返佣 <b>{pct(t.spotRate)}</b></div>
          </div>
        ))}
      </div>
      <h2>二级奖励</h2>
      {/* 产品确认：最多 2 级（与后端 overrideLevels 对照见报告） */}
      <div className="card">上级额外获得下级合伙人佣金的 <b className="lime">{pct(R?.overrideRate ?? OVERRIDE.rate)}</b>（{R?.overrideFunding ?? "平台出资"}），最多 {OVERRIDE.maxLevels} 级。</div>
      <h2>规则说明</h2>
      <ol>
        {R && !r.demo ? <>
          <li>普通合伙人需提交申请{R.autoApprove ? "（当前自动通过）" : "并经平台审核"}；之后按{R.tierBasis}自动升降档。</li>
          <li>返佣基数：{R.rebateBase}。</li>
          <li>佣金{R.settlement}（每日 {R.settleHourUtc}:00 UTC），发放币种 {R.payoutAsset}；被邀请用户{R.validity}。</li>
          <li>合伙人返佣与普通邀请返佣不叠加；跟单分润不计入返佣。</li>
        </> : RULE_NOTES.map((x) => <li key={x}>{x}</li>)}
      </ol>
      <h2>申请成为合伙人</h2>
      <div className="card" style={{ maxWidth: 480 }}>
        {!tok ? <div className="muted">请先 <a className="lime" href="/">登录</a> 后申请</div>
          : me.loading ? <Skel h={60} />
          : st && st !== "rejected" ? <div>申请状态：<b className="lime">{ST[st] || st}</b></div>
          : <>
            {st === "rejected" && <div className="err">上次申请未通过{me.data?.partner?.reviewNote ? `：${me.data.partner.reviewNote}` : ""}，可重新提交</div>}
            <input placeholder="Telegram / 联系方式（必填）" maxLength={64} value={f.contact} onChange={(e) => setF({ ...f, contact: e.target.value })} />
            <input placeholder="推广渠道" maxLength={64} value={f.channel} onChange={(e) => setF({ ...f, channel: e.target.value })} />
            <input placeholder="预估用户规模" maxLength={64} value={f.scale} onChange={(e) => setF({ ...f, scale: e.target.value })} />
            <button className="btn" disabled={busy} onClick={submit}>{busy ? "提交中…" : "提交申请"}</button>
          </>}
      </div>
    </>
  );
}
