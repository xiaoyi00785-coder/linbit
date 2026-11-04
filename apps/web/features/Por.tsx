"use client";
import { useProofOfReserves } from "@/lib/growth";
import { fmtLocal } from "@/lib/time";

const ratio = (r: string) => (r === "" ? "--" : `${(Number(r) * 100).toFixed(2)}%`);

/** 储备证明：GET /market/proof-of-reserves（后台手动录入的最新快照） */
export function Por() {
  const d = useProofOfReserves();
  if (d === undefined) return <section className="box pgc"><div className="lb-skel" style={{ height: 120 }} /></section>;
  if (!d || !d.available) return <section className="box pgc"><h3>最新快照</h3><p className="g">储备证明快照尚未发布</p></section>;
  return (
    <section className="box pgc">
      <h3>最新快照</h3>
      <p className="g">快照时间：{d.snapshotAt ? fmtLocal(d.snapshotAt) : "--"}</p>
      <table className="por-t">
        <thead><tr><th>币种</th><th>用户资产</th><th>平台储备</th><th>储备率</th></tr></thead>
        <tbody>{d.items.map((x) => <tr key={x.asset}><td>{x.asset}</td><td>{x.liabilities}</td><td>{x.reserves}</td><td className={Number(x.ratio) >= 1 ? "up" : "dn"}>{ratio(x.ratio)}</td></tr>)}</tbody>
      </table>
    </section>
  );
}
