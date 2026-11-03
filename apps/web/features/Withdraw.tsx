"use client";
import { useEffect, useState } from "react";
import { Link } from "@/lib/router";
import { useBalances } from "@/lib/queries";
import { listWithdrawNetworks, getWithdrawConfig, withdraw, withdrawRecords, type WithdrawConfig, type Withdrawal } from "@/lib/services";
import { Src } from "./common";
// @ts-ignore legacy JS 组件
import { Ico } from "@/legacy/App";
// @ts-ignore
import { coins } from "@/legacy/data";

const coinOf = (s: string) => (coins as { s: string; col: string }[]).find((x) => x.s === s) ?? { col: "var(--lb-usdt)", s };


const ASSETS = ["USDT", "BTC", "ETH"];
const NETS: Record<string, string[]> = {
  USDT: ["TRC20", "ERC20", "BEP20(BSC)", "Solana", "Arbitrum One"],
  BTC: ["Bitcoin"],
  ETH: ["ERC20", "Arbitrum One"],
};

/** 提现：币种 → 网络 → 地址/memo/数量（最小额、链上网络费来自配置接口，平台不收提现手续费）→ 2FA 确认 */
export function Withdraw() {
  const [coinOpen, setCoinOpen] = useState(false);
  const [asset, setAsset] = useState("USDT");
  const [net, setNet] = useState("TRC20");
  const [addr, setAddr] = useState("");
  const [memo, setMemo] = useState("");
  const [amt, setAmt] = useState("");
  const [cfg, setCfg] = useState<WithdrawConfig>();
  const [cfgSrc, setCfgSrc] = useState<"api" | "mock">();
  const [confirm, setConfirm] = useState(false);
  const [totp, setTotp] = useState("");
  const [err, setErr] = useState("");
  const [done, setDone] = useState<Withdrawal>();
  const [records, setRecords] = useState<Withdrawal[]>([]);
  const [recSrc, setRecSrc] = useState<"api" | "mock">();
  const bal = useBalances();
  const [apiNets, setApiNets] = useState<Record<string, string[]>>({});
  const netsOf = (a: string) => apiNets[a] ?? NETS[a];

  // 网络列表来自 GET /wallet/withdraw/networks（失败回退本地列表）
  useEffect(() => {
    listWithdrawNetworks(asset).then((r) => {
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

  const items = (bal.data as { items?: { asset: string; free: string }[] } | undefined)?.items;
  const free = items?.find((b) => b.asset === asset)?.free ?? "0";
  const n = Number(amt);
  const min = Number(cfg?.minAmount ?? 0);
  const max = Number(cfg?.maxAmount ?? Infinity);
  const fee = Number(cfg?.gasFeeEstimate ?? 0);
  const receive = n > fee ? String(+(n - fee).toFixed(8)) : "0";

  const check = () => {
    if (addr.length < 20) return "请输入正确的提现地址";
    if (!n) return "请输入提现数量";
    if (n < min) return `最小提现数量 ${cfg?.minAmount} ${asset}`;
    if (n > max) return `单笔最大提现 ${cfg?.maxAmount} ${asset}`;
    if (cfg?.memoRequired && !memo) return "该网络需要填写 Memo / Tag";
    return "";
  };

  return (
    <main className="light">
      <div className="dwrap">
        <div className="crumb lg">资产 / 提现</div>
        <h1 className="dh">提现</h1>
        <div className="dgrid">
          <div className="lcard">
            <div className="fx-field">
              币种
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
              <div className="chips">
                {ASSETS.map((a) => <span key={a} className={asset===a?'on':''} onClick={() => { setAsset(a); setNet(netsOf(a)[0]); }}><Ico c={coinOf(a)} s={14} />{a}</span>)}
              </div>
            </div>
            <div className="fx-field">
              网络
              <select className="fsel" value={net} onChange={(e) => setNet(e.target.value)}>
                {netsOf(asset).map((a) => <option key={a}>{a}</option>)}
              </select>
            </div>
            <div className="fx-field">
              <span>提现地址 <Link to="/setting/address" className="lime2">从地址白名单选择 ›</Link></span>
              <input placeholder="输入或粘贴地址" value={addr} onChange={(e) => setAddr(e.target.value.trim())} />
            </div>
            <div className="fx-field">
              Memo / Tag{cfg?.memoRequired ? "（必填）" : "（选填，交易所地址请按要求填写）"}
              <input placeholder="Memo / Tag" value={memo} onChange={(e) => setMemo(e.target.value.trim())} />
            </div>
            <div className="fx-field">
              <span>数量 <Src s={cfgSrc} /></span>
              <input inputMode="decimal" placeholder={cfg ? `最小提现 ${cfg.minAmount} ${asset}` : "加载中…"} value={amt} onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))} />
            </div>
            <div className="fx-row">
              <span>可用 {free} {asset} <a className="lime2" onClick={() => setAmt(free)}>全部</a></span>
              <span>网络费（链上 Gas）≈ {cfg?.gasFeeEstimate ?? "-"} {asset} · 平台手续费 0</span>
            </div>
            <div className="fx-row">
              <span>单笔限额 {cfg?.minAmount ?? "-"} ~ {cfg?.maxAmount ?? "-"} {asset}</span>
              <span>到账数量 <b>{receive} {asset}</b></span>
            </div>
            {err && <div className="fx-err">{err}</div>}
            {done && <div className="fx-ok">✓ 提现申请已提交（{done.id}），状态：审核中</div>}
            <button className="lb lime wide" onClick={() => { const e = check(); setErr(e); if (!e) setConfirm(true); }}>
              提现
            </button>
          </div>
          <div className="dtips">
            <div className="lcard sm">
              <b>温馨提示</b>
              <p className="lg">提现需完成谷歌验证器（2FA）验证。修改密码或安全设置后 24 小时内禁止提现。linbit 不收取提现手续费，仅扣除链上网络费（Gas），金额随网络拥堵实时变化：Arbitrum 约 1 USDT、BNB Chain 约 1 USDT、TRON 约 2 USDT、Ethereum 约 10 USDT。请仔细核对地址与网络，错误地址将导致资产无法找回。</p>
            </div>
          </div>
        </div>
        <div className="lcard">
          <h3>提现记录 <Src s={recSrc} /></h3>
          {records.length === 0 ? <div className="lg">暂无数据</div> : records.map((r) => (
            <div key={r.id} className="fx-item"><span>{r.createdAt.slice(0, 16).replace("T", " ")}</span><span className="sp">{r.amount} {r.asset} · {r.network}</span><span>{r.status}</span></div>
          ))}
        </div>
      </div>
      {confirm && (
        <div className="fx-modal" onClick={() => setConfirm(false)}>
          <div onClick={(e) => e.stopPropagation()}>
            <h3>安全验证</h3>
            <p className="lg">提现 {amt} {asset}（{net}），到账 {receive} {asset}</p>
            <div className="fx-field">
              谷歌验证器动态码
              <input inputMode="numeric" maxLength={6} placeholder="6 位 2FA 动态码" value={totp} onChange={(e) => setTotp(e.target.value.replace(/\D/g, ""))} />
            </div>
            <button
              className="lb lime wide"
              onClick={async () => {
                if (totp.length !== 6) return setErr("请输入 6 位 2FA 动态码");
                const r = await withdraw({ asset, network: net, address: addr, amount: amt, memo: memo || null, totpCode: totp });
                setConfirm(false);
                setTotp("");
                if (r.ok) { setDone(r.data); setErr(""); } else setErr(r.error ?? "提现失败");
              }}
            >
              确认提现
            </button>
          </div>
        </div>
      )}
    </main>
  );
}
