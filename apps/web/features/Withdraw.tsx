"use client";
import React, { useEffect, useState } from "react";
import { Link } from "@/lib/router";
import { useBalances } from "@/lib/queries";
import { listWithdrawNetworks, getWithdrawConfig, withdraw, withdrawRecords, type WithdrawConfig, type WithdrawRequest, type Withdrawal } from "@/lib/services";
import { Src } from "./common";
import { toast } from "@/lib/ui";
import { useSecurity, fmtUntil } from "@/lib/security";
import { goto } from "@/lib/ui";
import { maskEmail } from "@/lib/account";
import { addressError, amountError, cleanNum, DEFAULT_MIN_WITHDRAW } from "@/lib/validate";
import { TotpErr, TwofaRequired, twofaKind, type TwofaKind } from "@/lib/twofa";
import { NetPicker } from "@/legacy/Pages";
import { createInternalTransfer, errMsg, getNetworks, listInternalTransfers, newClientId, SOON, useOverview, type InternalTransfer, type WalletNetwork } from "@/lib/wallet";
// @ts-ignore legacy JS 组件
import { Ico } from "@/legacy/App";
// @ts-ignore
import { coins } from "@/legacy/data";
import { fmtAmt, trimFixed } from "@/lib/format";

const coinOf = (s: string) => (coins as { s: string; col: string }[]).find((x) => x.s === s) ?? { col: "var(--lb-usdt)", s };


const ASSETS = ["USDT", "BTC", "ETH"];

const St = ({ n, t, children }: { n: string; t: string; children: React.ReactNode }) => <div className="step"><div className="stn"><b>{n}</b>{t}</div><div className="stb">{children}</div></div>;

/** 提现：币种 → 网络 → 地址/memo/数量（最小额、提现手续费来自 wallet_network_settings 配置接口，接口不可用时显示 --）→ 2FA 确认 */
const WLOCK = "安全设置变更后 24 小时内禁止提现";
export function Withdraw() {
  const [coinOpen, setCoinOpen] = useState(false);
  const [asset, setAsset] = useState("USDT");
  const [sending, setSending] = useState(false);
  const [net, setNet] = useState("TRC20");
  const [addr, setAddr] = useState("");
  const [memo, setMemo] = useState("");
  const [tab, setTab] = useState<"chain" | "inner">("chain");
  const [to, setTo] = useState("");
  const [amt, setAmt] = useState("");
  const [cfg, setCfg] = useState<WithdrawConfig>();
  const [cfgSrc, setCfgSrc] = useState<"api" | "network">();
  const [confirm, setConfirm] = useState(false);
  const [totp, setTotp] = useState("");
  const [tfa, setTfa] = useState<TwofaKind>(null);
  const [fp, setFp] = useState("");
  /** 资金密码错误态：invalid → 输入框下提示；unset → 引导设置；locked → 锁定提示 */
  const [fpe, setFpe] = useState<null | "invalid" | "unset" | "missing" | { locked: string }>(null);
  const sec = useSecurity();
  const fpSet = !!sec.data?.fundPasswordSet;
  const wLockUntil = sec.data?.withdrawLockedUntil && new Date(sec.data.withdrawLockedUntil) > new Date() ? sec.data.withdrawLockedUntil : null;
  /** 资金密码 / 提现锁定错误码；返回 true 表示已处理 */
  const fundErr = (r: { code?: string; error?: string; data?: unknown }): boolean => {
    switch (r.code) {
      case "FUND_PASSWORD_INVALID": setFpe("invalid"); setFp(""); return true;
      case "FUND_PASSWORD_REQUIRED": case "FUND_PASSWORD_NOT_SET": setFpe(fpSet && r.code === "FUND_PASSWORD_REQUIRED" ? "missing" : "unset"); return true;
      case "FUND_PASSWORD_LOCKED": setFpe({ locked: fmtUntil((r as { lockedUntil?: string }).lockedUntil) }); return true;
      case "WITHDRAW_LOCKED": setConfirm(false); setErr(WLOCK); sec.reload(); return true;
    }
    return false;
  };
  const [err, setErr] = useState("");
  const [done, setDone] = useState<Withdrawal>();
  const [records, setRecords] = useState<Withdrawal[]>([]);
  const [recSrc, setRecSrc] = useState<"api" | "network">();
  const bal = useBalances();
  const [apiNets, setApiNets] = useState<Record<string, string[]>>({});
  const netsOf = (a: string) => apiNets[a] ?? [];
  const [netInfo, setNetInfo] = useState<Record<string, WalletNetwork>>({});
  const ni = netInfo[`${asset}:${net}`];
  const ov = useOverview();
  const [inner, setInner] = useState<InternalTransfer[]>([]);
  const [innerSrc, setInnerSrc] = useState<"api" | "network">();
  const [innerSoon, setInnerSoon] = useState(false);

  // 网络列表：GET /wallet/networks（含开关/最小额/手续费/到账时间）→ 回退 /wallet/withdraw/networks；都不可用时网络列表为空（不使用本地列表）
  useEffect(() => {
    getNetworks(asset).then((r) => {
      const l = (r.ok && r.source === "api" ? r.data?.networks ?? [] : []).filter((x) => x.withdrawEnabled !== false);
      if (l.length) {
        setNetInfo((m) => ({ ...m, ...Object.fromEntries(l.map((x) => [`${asset}:${x.network}`, x])) }));
        setApiNets((m) => ({ ...m, [asset]: l.map((x) => x.network) }));
        setNet((cur) => (l.some((x) => x.network === cur) ? cur : l[0].network));
        return;
      }
      fallbackNets();
    });
    const fallbackNets = () => listWithdrawNetworks(asset).then((r) => {
      const l = (r.data?.networks ?? []).map((x) => x.network);
      if (r.source === "api" && l.length) {
        setApiNets((m) => ({ ...m, [asset]: l }));
        setNet((cur) => (l.includes(cur) ? cur : l[0]));
      }
    });
  }, [asset]);

  useEffect(() => {
    getWithdrawConfig(asset, net).then((r) => {
      setCfg(r.data);
      setCfgSrc(r.source);
    });
  }, [asset, net]);
  useEffect(() => {
    withdrawRecords().then((r) => {
      setRecords(r.data?.items ?? []);
      setRecSrc(r.source);
    });
  }, [done]);
  // 站内转账记录 GET /wallet/internal-transfers
  const [innerDone, setInnerDone] = useState(0);
  useEffect(() => {
    if (tab !== "inner") return;
    listInternalTransfers().then((r) => {
      setInner(r.data?.items ?? []);
      setInnerSrc(r.ok ? "api" : undefined);
      setInnerSoon(!r.ok && errMsg(r) === SOON);
    });
  }, [tab, innerDone]);

  const items = (bal.data as { items?: { asset: string; free: string }[] } | undefined)?.items;
  // 提现/站内转账从 withdrawAccount（默认资金账户）扣款，余额优先取 /assets/overview
  const wAcct = ov.data?.withdrawAccount ?? "funding";
  const ovFree = ov.data?.accounts?.find((a) => a.account === wAcct)?.balances?.find((b) => b.asset === asset)?.free;
  const free = ovFree ?? items?.find((b) => b.asset === asset)?.free ?? "0";
  const n = Number(amt);
  const minS = ni?.minWithdraw ?? cfg?.minAmount;
  const maxS = ni ? ni.maxWithdraw ?? undefined : cfg?.maxAmount;
  // 手续费只认接口值（wallet_network_settings.withdraw_fee 生效值），接口不可用时不展示估算值
  const feeS = ni?.withdrawFee ?? (cfgSrc === "api" ? cfg?.gasFeeEstimate : undefined);
  const eta = ni?.estimatedArrivalMinutes ? `约 ${ni.estimatedArrivalMinutes} 分钟` : "约 5 分钟";
  const memoReq = ni?.memoRequired ?? cfg?.memoRequired;
  const min = Math.max(Number(minS ?? 0) || 0, DEFAULT_MIN_WITHDRAW[asset] ?? 0);
  const max = Number(maxS ?? Infinity);
  const fee = Number(feeS ?? 0);
  const toKey = /^\S+@\S+\.\S+$/.test(to) ? "email" : /^\d{5,20}$/.test(to) ? "uid" : "";
  const checkInner = () => {
    if (!toKey) return "请输入正确的收款方 UID 或邮箱";
    if (!amt) return "请输入转账数量";
    return amtE;
  };
  const freeN = Number(free) || 0;
  // 实时校验（输入即提示），任一错误时禁用提交
  const addrE = tab === "chain" ? addressError(net, addr) : "";
  const fmtE = amountError(amt, { dp: 8 });
  const amtE = fmtE || (!amt ? "" :
    n > freeN ? `可用余额不足（可用 ${fmtAmt(free)} ${asset}）` :
    tab === "chain" && n < min ? `最小提现数量 ${min} ${asset}` :
    tab === "chain" && n > max ? `单笔最大提现 ${maxS} ${asset}` :
    tab === "chain" && n <= fee ? `提现数量需大于链上网络费（Gas）${fee} ${asset}` : "");
  const amtOk = !!amt && !amtE;
  // 到账数量：数量无效或不足以覆盖手续费时显示 --，绝不显示负数或误导性的 0
  const receive = amtOk && n > fee ? trimFixed(n - fee, 8) : "--";

  const check = () => {
    if (!addr) return "请输入提现地址";
    if (addrE) return addrE;
    if (!amt) return "请输入提现数量";
    if (amtE) return amtE;
    if (memoReq && !memo) return "该网络需要填写 Memo / Tag";
    return "";
  };

  const submitOk = !wLockUntil && (tab === "inner" ? !!toKey && amtOk : !!addr && !addrE && amtOk && (!memoReq || !!memo));

  return (
    <main className="light">
      <div className="dwrap">
        <div className="crumb lg">资产 / 提现</div>
        <h1 className="dh">提现</h1>
        <div className="dgrid">
          <div className="wsteps">
            <St n="1" t="选择币种">
              <div className="lsel big2" onClick={() => setCoinOpen(!coinOpen)}>
                <Ico c={coinOf(asset)} s={20} /> {asset} <em>▾</em>
              </div>
              {coinOpen && (
                <div className="lmenu">
                  {ASSETS.map((a) => (
                    <a key={a} style={{ display: "flex", alignItems: "center", gap: 8 }} onClick={() => { setAsset(a); setNet(netsOf(a)[0]); setCoinOpen(false); }}>
                      <Ico c={coinOf(a)} s={18} /> {a}
                    </a>
                  ))}
                </div>
              )}
              <div className="chips cchips">
                {ASSETS.map((a) => <span key={a} className={asset===a?'on':''} onClick={() => { setAsset(a); setNet(netsOf(a)[0]); }}><Ico c={coinOf(a)} s={14} />{a}</span>)}
              </div>
            </St>
            <St n="2" t={tab === "chain" ? "提现网络" : "收款账户"}>
              <div className="wtabs">
                <a className={tab === "chain" ? "on" : ""} onClick={() => { setTab("chain"); setErr(""); }}>链上提现</a>
                <a className={tab === "inner" ? "on" : ""} onClick={() => { setTab("inner"); setErr(""); }}>内部转账 <i>免费</i></a>
              </div>
              {tab === "chain" ? (<>
                <div className="wwarn"><b>!</b>请确认收款平台支持所选币种与网络，选错币种或网络可能导致资产丢失。</div>
                <div className="fx-field">
                  提现网络
                  <NetPicker out coin={asset} net={net} setNet={setNet}
                    nets={netsOf(asset).map((n) => { const i = netInfo[`${asset}:${n}`]; return { network: n, min: i?.minWithdraw || undefined, recommended: i?.recommended === true || undefined }; })} />
                </div>
                <div className="fx-field">
                  <span className="wlab">提现地址 <Link to="/setting/address" className="wbook">地址簿 ›</Link></span>
                  <input placeholder="输入或粘贴地址" value={addr} onChange={(e) => setAddr(e.target.value.trim())} aria-invalid={!!addrE} />
                  {addrE && <div className="fx-err fe-inline">{addrE}</div>}
                </div>
                <div className="fx-field">
                  Memo / Tag{memoReq ? "（必填）" : "（选填）"}
                  <input placeholder="Memo / Tag" value={memo} onChange={(e) => setMemo(e.target.value.trim())} />
                </div>
              </>) : (<>
                <div className="wwarn"><b>!</b>转给 linbit 用户，免手续费、实时到账、不上链。需已绑定谷歌验证器；不支持转入子账户；24 小时额度 50,000 USDT。</div>
                <div className="fx-field">
                  收款方
                  <input placeholder="输入对方 UID 或邮箱" value={to} onChange={(e) => setTo(e.target.value.trim())} />
                  {to && <span className="lg wto">{toKey === "uid" ? "按 UID 转账" : toKey === "email" ? "按邮箱转账" : "仅支持 UID 或邮箱"}</span>}
                </div>
              </>)}
            </St>
            <St n="3" t={tab === "inner" ? "转账数量" : "提现数量"}>
              <div className="fx-field">
                <span className="wamt"><input inputMode="decimal" placeholder={tab === "inner" ? "请输入转账数量" : minS ? `最小提现数量：${minS}` : "加载中…"} value={amt} onChange={(e) => setAmt(cleanNum(e.target.value))} aria-invalid={!!amtE} /><a className="lime2" onClick={() => setAmt(free)}>全部</a><em>{asset}</em></span>
                {amtE && <div className="fx-err fe-inline">{amtE}</div>}
              </div>
              <div className="wkv">
                <span>{wAcct === "funding" ? "资金账户可用" : "可用"}</span><b>{fmtAmt(free)} {asset}</b>
                {tab === "inner" ? <><span>24 小时额度</span><b>50,000 USDT</b></> : <>
                <span>单笔最小提现</span><b>{min || "--"} {asset}</b>
                <span>单笔最大提现</span><b>{maxS ?? "不限"} {asset}</b></>}
              </div>
              <div className="wkv wdiv">
                <span>平台手续费</span><b>0 {asset}</b>
                {tab !== "inner" && <><span>链上网络费（Gas）<Src s={cfgSrc} /></span><b>{feeS ?? "--"} {asset}</b></>}
                <span>到账数量</span><b>{tab === "inner" ? (amtOk ? amt : "--") : receive} {asset}</b>
                <span>预计到账时间</span><b>{tab === "inner" ? "实时到账" : eta}</b>
              </div>
              {tfa === "required" && <TwofaRequired />}
              {wLockUntil && !err && <div className="sec-lock"><b>!</b>{WLOCK}，预计 {fmtUntil(wLockUntil)} 解除。</div>}
              {err && <div className="fx-err">{err}</div>}
              {done && <div className="fx-ok">✓ 提现申请已提交（{done.id}），状态：审核中</div>}
              <button className={"lb dark wide" + (submitOk ? "" : " dis")} disabled={!submitOk} onClick={() => { const e = tab === "inner" ? checkInner() : check(); setErr(e); if (!e) { setFpe(null); setTfa(null); setConfirm(true); } }}>
                {tab === "inner" ? "转账" : "提现"}
              </button>
            </St>
          </div>
          <div className="dtips">
            <div className="lcard sm">
              <b>温馨提示</b>
              <p className="lg">请确认设备安全，谨防信息被篡改或泄露。<br/>谨防诈骗！linbit 不会向你索要账户密码等敏感信息，也不会要求私下转账或提现。<br/>linbit 不收取平台提现手续费，仅从提现数量中扣除链上网络费（Gas），到账数量 = 提现数量 − Gas。<br/>USDT 单笔最小提现 12 USDT。</p>
            </div>
            <div className="lcard sm">
              <b>常见问题</b>
              <p className="lg">1. 如何从 linbit 提现？<br/>2. 提现为什么还没到账？</p>
            </div>
          </div>
        </div>
        <div className="lcard">
          {tab === "inner" ? (<>
            <h3>站内转账记录 <Src s={innerSrc} /></h3>
            {innerSoon ? <div className="lg">{SOON}</div> : inner.length === 0 ? <div className="lg">暂无数据</div> : inner.map((r) => (
              <div key={r.id} className="fx-item"><span>{r.createdAt.slice(0, 16).replace("T", " ")}</span><span className="sp">{r.direction === "out" ? "转出" : "转入"} {r.amount} {r.asset} · {r.direction === "out" ? (r.toMaskedEmail ? maskEmail(r.toMaskedEmail) : `UID ${r.toUid}`) : `UID ${r.fromUid}`}</span><span>{{ pending: "处理中", completed: "已完成", failed: "失败" }[r.status]}</span></div>
            ))}
          </>) : (<>
          <h3>提现记录 <Src s={recSrc} /></h3>
          {records.length === 0 ? <div className="lg">暂无数据</div> : records.map((r) => (
            <div key={r.id} className="fx-item"><span>{r.createdAt.slice(0, 16).replace("T", " ")}</span><span className="sp">{r.amount} {r.asset} · {r.network}</span><span>{r.status}</span></div>
          ))}</>)}
        </div>
      </div>
      {confirm && (
        <div className="fx-modal" data-dismissible="false" aria-busy={sending} onClick={() => setConfirm(false)}>
          <div onClick={(e) => e.stopPropagation()}>
            <span className="fx-x" role="button" aria-label="关闭" onClick={() => { if (!sending) setConfirm(false); }}>✕</span>
            <h3>安全验证</h3>
            <p className="lg">{tab === "inner" ? `站内转账 ${amt} ${asset} 至 ${to}，免手续费，实时到账` : `提现 ${amt} ${asset}（${net}），扣除链上 Gas ${feeS ?? "--"} ${asset}，到账 ${receive} ${asset}（平台不收手续费）`}</p>
            <div className="fx-field">
              谷歌验证器动态码
              <input inputMode="numeric" maxLength={6} placeholder="6 位 2FA 动态码" value={totp} onChange={(e) => { setTotp(e.target.value.replace(/\D/g, "")); if (tfa === "invalid") setTfa(null); }} />
              <TotpErr show={tfa === "invalid"} />
            </div>
            {fpe !== "unset" && !fpSet && <div className="lg fp-unset">你尚未设置资金密码，本次无需填写。建议前往 <a role="button" onClick={() => { setConfirm(false); goto("/setting/security#fund"); }}>安全中心</a> 设置以提升资金安全。</div>}
            {fpe !== "unset" && fpSet && <div className="fx-field">
              资金密码
              <input type="password" inputMode="numeric" maxLength={6} placeholder="6 位资金密码" value={fp} onChange={(e) => { setFp(e.target.value.replace(/\D/g, "")); if (fpe === "invalid" || fpe === "missing") setFpe(null); }} />
              {fpe === "invalid" && <div className="fx-err tfa-inv">资金密码错误，请重新输入</div>}
              {fpe === "missing" && <div className="fx-err tfa-inv">请输入资金密码</div>}
            </div>}
            {fpe === "unset" && <div className="tfa-req"><span><b>!</b>请先设置资金密码，设置后才能{tab === "inner" ? "转账" : "提现"}。</span><button className="lb sm lime" onClick={() => { setConfirm(false); goto("/setting/security#fund"); }}>去设置</button></div>}
            {fpe && typeof fpe === "object" && <div className="tfa-req"><span><b>!</b>资金密码错误次数过多，已锁定{fpe.locked ? `至 ${fpe.locked}` : ""}。</span><button className="lb sm lime" onClick={() => { setConfirm(false); goto("/setting/security#fund"); }}>重置资金密码</button></div>}
            <button
              className={"lb lime wide" + (sending ? " dis" : "")}
              disabled={sending}
              onClick={async () => {
                if (sending) return;
                if (totp.length !== 6) return setErr("请输入 6 位 2FA 动态码");
                if (fpSet && fp.length !== 6) return setFpe("missing");
                setTfa(null); setFpe(null);
                const fpw = fp ? { fundPassword: fp } : {};
                if (tab === "inner") {
                  setSending(true);
                  const r = await createInternalTransfer({ clientId: newClientId(), asset, amount: amt, totpCode: totp, ...fpw, ...(toKey === "email" ? { toEmail: to } : { toUid: to }) }).finally(() => setSending(false));
                  const k = twofaKind(r);
                  if (fundErr(r)) return;
                  if (k === "invalid") { setTfa("invalid"); setTotp(""); return; }
                  setConfirm(false); setTotp(""); setFp("");
                  if (k === "required") { setTfa("required"); setErr(""); return; }
                  if (r.ok) { setErr(""); setAmt(""); toast("转账成功", "ok"); setInnerDone((x) => x + 1); ov.reload(); }
                  else { const m = errMsg(r); setErr(m); if (m === SOON) toast(m, "info"); }
                  return;
                }
                setSending(true);
                const r = await withdraw({ asset, network: net, address: addr, amount: amt, memo: memo || null, totpCode: totp, ...fpw }).finally(() => setSending(false));
                const k = twofaKind(r);
                if (fundErr(r)) return;
                if (k === "invalid") { setTfa("invalid"); setTotp(""); return; }
                setConfirm(false);
                setTotp(""); setFp("");
                if (k === "required") { setTfa("required"); setErr(""); return; }
                if (r.ok) { setDone(r.data); setErr(""); } else setErr(r.error ?? "提现失败");
              }}
            >
              {tab === "inner" ? "确认转账" : "确认提现"}
            </button>
          </div>
        </div>
      )}
    </main>
  );
}
