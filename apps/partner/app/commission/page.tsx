"use client";
import { useState } from "react";
import { useApi, type Comm } from "@/lib/api";
import { DAILY } from "@/lib/rules";
import { Coin, Demo, ErrBox, Skel, Tabs, day, num, pct } from "../ui";

type K = "" | "futures" | "spot" | "override" | "referral";
const KN: Record<string, string> = { futures: "合约返佣", spot: "现货返佣", override: "二级奖励", referral: "邀请返佣" };
const demo = (): Comm => ({ page: 1, pageSize: 20, total: 3, items: DAILY.map((d, i) => ({ settlementId: i, sourceUid: "80000123", kind: "futures", tier: "silver", rate: "0.4", base: String(d.futures / 0.4), amount: String(d.futures), day: d.date, settledAt: Date.parse(d.date), asset: "USDT" })) });

export default function Commission() {
  const [k, setK] = useState<K>(""); const [page, setPage] = useState(1);
  const r = useApi<Comm>(`/partner/commissions?page=${page}&pageSize=20${k ? `&kind=${k}` : ""}`, demo);
  const pages = r.data ? Math.max(1, Math.ceil(r.data.total / r.data.pageSize)) : 1;
  return (
    <>
      <h1>佣金明细 <Demo on={r.demo} /></h1>
      <Tabs<K> items={[["", "全部"], ["futures", "合约"], ["spot", "现货"], ["override", "二级奖励"], ["referral", "邀请"]]} cur={k} set={(x) => { setK(x); setPage(1); }} />
      {r.loading ? <Skel h={44} n={6} /> : r.err ? <ErrBox err={r.err} reload={r.reload} /> : !r.data?.items.length ? <div className="card empty">暂无佣金记录</div> : (
        <table className="fade"><thead><tr><th>结算日</th><th>来源 UID</th><th>类型</th><th>档位</th><th>返佣比例</th><th>手续费基数</th><th>佣金</th></tr></thead>
          <tbody>{r.data.items.map((c) => <tr key={c.settlementId + c.kind + c.sourceUid}><td>{c.day || day(c.settledAt)}</td><td>{c.sourceUid}</td><td>{KN[c.kind] || c.kind}</td><td>{c.tier || "-"}</td><td>{pct(c.rate)}</td><td>{num(c.base, 6)}</td><td className="lime"><Coin s={c.asset} /> {num(c.amount, 6)} {c.asset}</td></tr>)}</tbody></table>
      )}
      {pages > 1 && <div className="pager"><button disabled={page <= 1} onClick={() => setPage(page - 1)}>‹</button><span>{page} / {pages}</span><button disabled={page >= pages} onClick={() => setPage(page + 1)}>›</button></div>}
    </>
  );
}
