"use client";
import { useEffect, useState } from "react";
import { toast } from "@/lib/ui";

const ACCTS = ["资金账户", "现货账户", "合约账户", "理财账户", "传统金融账户", "策略账户"];
export const openTransfer = (from?: string) => window.dispatchEvent(new CustomEvent("lb:transfer", { detail: from }));

/** 全局划转弹窗：任意页面 dispatch lb:transfer 打开 */
export function TransferHost() {
  const [open, setOpen] = useState(false);
  const [from, setFrom] = useState(ACCTS[0]);
  const [to, setTo] = useState(ACCTS[1]);
  const [coin, setCoin] = useState("USDT");
  const [amt, setAmt] = useState("");
  useEffect(() => {
    const h = (e: Event) => { const f = (e as CustomEvent).detail as string | undefined; if (f && ACCTS.includes(f)) { setFrom(f); setTo(f === ACCTS[0] ? ACCTS[1] : ACCTS[0]); } setAmt(""); setOpen(true); };
    window.addEventListener("lb:transfer", h);
    return () => window.removeEventListener("lb:transfer", h);
  }, []);
  if (!open) return null;
  return (
    <div className="fx-modal light" onClick={() => setOpen(false)}>
      <div className="trm" onClick={(e) => e.stopPropagation()}>
        <div className="trh"><h3>划转</h3><a onClick={() => setOpen(false)}>✕</a></div>
        <div className="trw">
          <label>从<select className="fsel" value={from} onChange={(e) => setFrom(e.target.value)}>{ACCTS.filter((a) => a !== to).map((a) => <option key={a}>{a}</option>)}</select></label>
          <button className="trx" title="交换" onClick={() => { setFrom(to); setTo(from); }}>⇅</button>
          <label>到<select className="fsel" value={to} onChange={(e) => setTo(e.target.value)}>{ACCTS.filter((a) => a !== from).map((a) => <option key={a}>{a}</option>)}</select></label>
        </div>
        <label className="trl">币种<select className="fsel" value={coin} onChange={(e) => setCoin(e.target.value)}>{["USDT", "BTC", "ETH"].map((a) => <option key={a}>{a}</option>)}</select></label>
        <label className="trl">数量<span className="wamt"><input inputMode="decimal" placeholder="请输入划转数量" value={amt} onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))} /><a className="lime2" onClick={() => setAmt("0")}>全部</a><em>{coin}</em></span></label>
        <div className="lg trv">可用 0 {coin}</div>
        <button className="lb dark wide" onClick={() => { if (!Number(amt)) return toast("请输入划转数量", "err"); toast("可用余额不足", "err"); }}>确认划转</button>
      </div>
    </div>
  );
}
