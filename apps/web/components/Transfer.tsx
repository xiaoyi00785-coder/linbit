"use client";
import { Dropdown } from "@/features/Kit";
import { useEffect, useState } from "react";
import { goto, toast } from "@/lib/ui";
import { MSG_TOTP_MISSING, MSG_TWOFA_REQUIRED, TWOFA_BIND, twofaBound, twofaKind } from "@/lib/twofa";
import { ACCT_KEY, ACCT_NAME, createTransfer, listTransfers, newClientId, type TransferRec } from "@/lib/transfer";
import { amountError, cleanNum } from "@/lib/validate";
import { errMsg, useOverview, type AcctKey } from "@/lib/wallet";

const ACCTS = ["资金账户", "现货账户", "合约账户", "理财账户"];
/** 打开全站唯一的划转弹窗。from/to 为中文账户名（资金账户/现货账户/合约账户/理财账户） */
export const openTransfer = (from?: string, to?: string) => window.dispatchEvent(new CustomEvent("lb:transfer", { detail: { from, to } }));

/** 全局划转弹窗：任意页面 dispatch lb:transfer 打开 */
export function TransferHost() {
  const [open, setOpen] = useState(false);
  const [from, setFrom] = useState(ACCTS[0]);
  const [to, setTo] = useState(ACCTS[1]);
  const [coin, setCoin] = useState("USDT");
  const [amt, setAmt] = useState("");
  const [busy, setBusy] = useState(false);
  const ov = useOverview();
  const [hist, setHist] = useState<TransferRec[]>([]);
  const loadH = () => listTransfers("?page=1&pageSize=5").then((r) => setHist(r.ok ? r.data?.items ?? [] : []));
  const fk = ACCT_KEY[from];
  const avail = ov.data?.accounts?.find((a) => a.account === fk)?.balances?.find((b) => b.asset === coin)?.free ?? "0";
  const amtE = amountError(amt, { dp: 8 }) || (amt && Number(amt) > Number(avail) ? `可用余额不足（可用 ${avail} ${coin}）` : "");
  const canSubmit = !busy && !!amt && !amtE;
  const submit = async () => {
    if (!canSubmit) return;
    if (!Number(amt)) return toast("请输入划转数量", "err");
    const f = ACCT_KEY[from], t = ACCT_KEY[to];
    if (f === "earn" || t === "earn") return toast("理财账户不支持划转，请使用申购 / 赎回", "info");
    if ((f === "futures" || t === "futures") && coin !== "USDT") return toast("合约账户仅支持 USDT 划转", "info");
    if (Number(amt) > Number(avail)) return toast("可用余额不足", "err");
    setBusy(true);
    const r = await createTransfer({ clientId: newClientId(), asset: coin, amount: amt, from: f as AcctKey, to: t as AcctKey });
    setBusy(false);
    if (r.ok) { toast("划转成功", "ok"); setOpen(false); ov.reload(); window.dispatchEvent(new Event("lb:balances")); window.dispatchEvent(new Event("lb-refresh")); }
    else if (twofaKind(r) === "required") { if (await twofaBound()) toast(MSG_TOTP_MISSING, "err"); else { toast(MSG_TWOFA_REQUIRED, "err"); setOpen(false); goto(TWOFA_BIND); } }
    else toast(errMsg(r), "err");
  };
  useEffect(() => {
    const h = (e: Event) => { const d = ((e as CustomEvent).detail ?? {}) as { from?: string; to?: string }; const f = d.from && ACCTS.includes(d.from) ? d.from : ACCTS[0]; const t = d.to && ACCTS.includes(d.to) && d.to !== f ? d.to : f === ACCTS[0] ? ACCTS[1] : ACCTS[0]; setFrom(f); setTo(t); loadH(); setAmt(""); setOpen(true); ov.reload(); };
    window.addEventListener("lb:transfer", h);
    return () => window.removeEventListener("lb:transfer", h);
  }, []);
  if (!open) return null;
  return (
    <div className="fx-modal light" aria-busy={busy} onClick={() => setOpen(false)}>
      <div className="trm" onClick={(e) => e.stopPropagation()}>
        <div className="trh"><h3>划转</h3><a role="button" aria-label="关闭" onClick={() => setOpen(false)}>✕</a></div>
        <div className="trw">
          <label>从<select className="fsel" value={from} onChange={(e) => setFrom(e.target.value)}>{ACCTS.filter((a) => a !== to).map((a) => <option key={a}>{a}</option>)}</select></label>
          <button className="trx" title="交换" onClick={() => { setFrom(to); setTo(from); }}>⇅</button>
          <label>到<select className="fsel" value={to} onChange={(e) => setTo(e.target.value)}>{ACCTS.filter((a) => a !== from).map((a) => <option key={a}>{a}</option>)}</select></label>
        </div>
        <div className="trl"><span>币种</span><Dropdown cls="fsel" icons v={coin} opts={["USDT", "BTC", "ETH"]} onChange={setCoin} /></div>
        <label className="trl">数量<span className="wamt"><input inputMode="decimal" placeholder="请输入划转数量" value={amt} onChange={(e) => setAmt(cleanNum(e.target.value))} aria-invalid={!!amtE} /><a className="lime2" onClick={() => setAmt(avail)}>全部</a><em>{coin}</em></span></label>
        {amtE && <div className="fx-err fe-inline">{amtE}</div>}
        <div className="lg trv">可用 {avail} {coin}</div>
        {hist.length > 0 && <div className="trhist"><div className="lg">最近划转</div>{hist.map((h) => <div key={h.id}><span>{ACCT_NAME[h.from] ?? h.from} → {ACCT_NAME[h.to] ?? h.to}</span><span>{h.amount} {h.asset}</span><span className="lg">{{ pending: "处理中", done: "已完成", failed: "失败" }[h.status]}</span></div>)}</div>}
        <button className={"lb dark wide" + (canSubmit ? "" : " dis")} disabled={!canSubmit} onClick={submit}>{busy ? "划转中…" : "确认划转"}</button>
      </div>
    </div>
  );
}
