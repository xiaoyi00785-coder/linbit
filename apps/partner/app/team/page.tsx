"use client";
import { useState } from "react";
import { useApi, type Team } from "@/lib/api";
import { ErrBox, Skel, Tabs, day, num } from "../ui";

type K = "" | "user" | "partner";

export default function TeamPage() {
  const [k, setK] = useState<K>("");
  const r = useApi<Team>(`/partner/team${k ? `?type=${k}` : ""}`);
  const L = r.data?.items || [];
  return (
    <>
      <h1>我的团队</h1>
      <Tabs<K> items={[["", "全部"], ["user", "直属用户"], ["partner", "下级合伙人"]]} cur={k} set={setK} />
      {r.loading ? <Skel h={44} n={5} /> : r.err ? <ErrBox err={r.err} reload={r.reload} /> : !L.length ? <div className="card empty">暂无团队成员，去 <a className="lime" href="/links">推广链接</a> 邀请用户</div> : (
        <table className="fade"><thead><tr><th>UID</th><th>类型</th><th>注册时间</th><th>KYC</th><th>近 30 日合约量</th><th>近 30 日现货量</th><th>近 30 日手续费</th><th>为我贡献佣金</th></tr></thead>
          <tbody>{L.map((t) => <tr key={t.uid}><td>{t.uid}</td><td>{t.isPartner ? <span className="tag">下级合伙人{t.tier ? ` · ${t.tier}` : ""}</span> : "直属用户"}</td><td>{day(t.joinedAt)}</td><td>Lv.{t.kycLevel}</td><td>{num(t.futuresVolume30d)}</td><td>{num(t.spotVolume30d)}</td><td>{num(t.fee30d, 4)}</td><td className="lime">{num(t.commission, 4)}</td></tr>)}</tbody></table>
      )}
    </>
  );
}
