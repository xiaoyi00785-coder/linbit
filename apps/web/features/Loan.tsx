"use client";
import { G } from "@/components/Glyph";
/**
 * 质押借币：对接 earn 服务 /loans/*（gateway 前缀 /loans；market、quote 公开，其余需登录）。
 * 仅移除 USDC 作为可借 / 质押币种；初始质押率以后端 initialLtv 为准（默认 80%）。
 */
import { useEffect, useState } from "react";
import { useAuthStore } from "@/lib/store";
import { rest, useRest } from "@/lib/rest";
import { toast } from "@/lib/ui";
import { CoinIcon } from "@/components/CoinIcon";

type Term = { term: string; days: number; hourlyRate: string; annualRate: string; status: string; remaining?: string };
type Market = { asset: string; terms: Term[]; collaterals: { asset: string; cap?: string; price?: string }[]; initialLtv: string; warnLtv: string; liquidationLtv: string; minAmount: string; userLimit: string; earlyRepayPenaltyRate?: string; liquidationFeeRate?: string };
type LoanV = { id: number; asset: string; collateralAsset: string; term: string; principal: string; collateral: string; interest: string; debt: string; status: string; hourlyRate: string; startedAt: number; dueAt?: number; closedAt?: number; ltv?: string; liquidationPrice?: string; warning?: boolean; overdue?: boolean; initialPrincipal: string; initialCollateral: string };
type Quote = { collateral: string; ltv: string; hourlyInterest: string; termInterest?: string; liquidationPrice: string; price: string };

const BLOCK = new Set(["USDC"]); // 已下线：USDC 不再作为借贷币种
const DEMO: Market = {
  asset: "USDT", initialLtv: "0.8", warnLtv: "0.88", liquidationLtv: "0.95", minAmount: "10", userLimit: "1200000",
  terms: [
    { term: "flexible", days: 0, hourlyRate: "0.000003847", annualRate: "0.0337", status: "online" },
    { term: "7d", days: 7, hourlyRate: "0.0000042808", annualRate: "0.0375", status: "online" },
    { term: "30d", days: 30, hourlyRate: "0.0000068493", annualRate: "0.06", status: "online" },
  ],
  collaterals: [{ asset: "BTC" }, { asset: "ETH" }],
};
const TN: Record<string, string> = { flexible: "活期 · 浮动利率", "7d": "7 天 · 固定利率", "30d": "30 天 · 固定利率" };
const tn = (t: string) => TN[t] ?? t;
const pc = (x?: string | number, d = 2) => (x == null || x === "" ? "--" : (Number(x) * 100).toFixed(d) + "%");
const n = (x?: string | number, d = 4) => (x == null || x === "" ? "--" : Number(x).toLocaleString("en-US", { maximumFractionDigits: d }));
const dt = (ms?: number) => (ms ? new Date(ms).toLocaleString("zh-CN", { hour12: false }) : "--");
const ST: Record<string, string> = { active: "借款中", repaid: "已还清", liquidated: "已强平" };

export function useLoanMarket() {
  const m = useRest<Market>("/loans/market", () => DEMO);
  const d = m.data;
  const market = d && { ...d, collaterals: d.collaterals.filter((c) => !BLOCK.has(c.asset)), terms: d.terms.filter((t) => t.status === "online") };
  return { ...m, market: market && !BLOCK.has(market.asset) ? market : null };
}

function Mask({ title, sub, onClose, children }: { title: string; sub?: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="xmask" onClick={onClose}>
      <div className="xmodal lt fx-loanm" onClick={(e) => e.stopPropagation()}>
        <span className="xx" onClick={onClose}>×</span>
        <h3>{title}</h3>
        {sub && <p className="xsub">{sub}</p>}
        {children}
      </div>
    </div>
  );
}

/** 借款弹窗：GET /loans/quote 实时试算 → POST /loans */
export function BorrowModal({ m, term0, onClose, onDone }: { m: Market; term0: string; onClose: () => void; onDone: () => void }) {
  const [amt, setAmt] = useState("");
  const [col, setCol] = useState(m.collaterals[0]?.asset ?? "BTC");
  const [term, setTerm] = useState(term0);
  const [q, setQ] = useState<Quote | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const authed = !!useAuthStore((s) => s.accessToken);
  useEffect(() => {
    setQ(null);
    if (!(Number(amt) > 0)) return;
    const t = setTimeout(async () => {
      const r = await rest<Quote>("GET", `/loans/quote?collateralAsset=${col}&term=${term}&amount=${amt}`);
      if (r.ok && r.data) (setQ(r.data), setErr(""));
      else setErr(r.error ?? "试算失败");
    }, 300);
    return () => clearTimeout(t);
  }, [amt, col, term]);
  const submit = async () => {
    if (!authed) return toast("请先登录", "info");
    if (Number(amt) < Number(m.minAmount)) return setErr(`最低借款 ${m.minAmount} ${m.asset}`);
    setBusy(true);
    const r = await rest("POST", "/loans", { collateralAsset: col, amount: amt, term, ...(q ? { collateralAmount: q.collateral } : {}) });
    setBusy(false);
    if (!r.ok) return setErr(r.source === "demo" ? "服务暂不可用（演示环境）" : r.error ?? "借款失败");
    toast(`借款成功：${amt} ${m.asset}`);
    onDone();
  };
  return (
    <Mask title={"借 " + m.asset} sub="选择质押物与借款期限" onClose={onClose}>
      <label className="lg">借款期限</label>
      <div className="pt fx-chips">{m.terms.map((t) => <b key={t.term} className={term === t.term ? "on" : ""} onClick={() => setTerm(t.term)}>{tn(t.term).split(" · ")[0]} {pc(t.annualRate)}</b>)}</div>
      <label className="lg">借款数量（{m.asset}）</label>
      <input className="xin" inputMode="decimal" placeholder={`最低 ${m.minAmount}`} value={amt} onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))} />
      <label className="lg">质押币种</label>
      <div className="pt fx-chips">{m.collaterals.map((c) => <b key={c.asset} className={col === c.asset ? "on" : ""} onClick={() => setCol(c.asset)}><CoinIcon s={c.asset} size={16} /> {c.asset}</b>)}</div>
      <div className="lrow"><span className="lg">初始质押率</span><span className="sp" /><b>{pc(m.initialLtv, 0)}</b></div>
      <div className="lrow"><span className="lg">需质押</span><span className="sp" /><b>{q ? `${n(q.collateral, 8)} ${col}` : "--"}</b></div>
      <div className="lrow"><span className="lg">预估每小时利息</span><span className="sp" /><b>{q ? `${n(q.hourlyInterest, 6)} ${m.asset}` : "--"}</b></div>
      {q?.termInterest && term !== "flexible" && <div className="lrow"><span className="lg">到期总利息</span><span className="sp" /><b>{n(q.termInterest, 6)} {m.asset}</b></div>}
      <div className="lrow"><span className="lg">强平价格</span><span className="sp" /><b>{q ? `${n(q.liquidationPrice, 2)} USDT` : "--"}</b></div>
      <p className="lg">质押率达 {pc(m.warnLtv, 0)} 时预警，达 {pc(m.liquidationLtv, 0)} 将触发强平。</p>
      {err && <div className="fx-err">{err}</div>}
      <button className={"lb dark wide" + (busy ? " dis" : "")} onClick={submit}>{busy ? "提交中…" : authed ? "确认借款" : "登录后借款"}</button>
    </Mask>
  );
}

/** 还款 / 追加质押：POST /loans/{id}/repay {amount|all}、/loans/{id}/collateral {amount} */
function ActModal({ l, kind, onClose, onDone }: { l: LoanV; kind: "repay" | "collateral"; onClose: () => void; onDone: () => void }) {
  const [v, setV] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const go = async (all = false) => {
    setBusy(true);
    const r = await rest("POST", `/loans/${l.id}/${kind}`, kind === "repay" && all ? { all: true } : { amount: v });
    setBusy(false);
    if (!r.ok) return setErr(r.error ?? "操作失败");
    toast(kind === "repay" ? "还款成功" : "已追加质押");
    onDone();
  };
  return (
    <Mask title={kind === "repay" ? "还款" : "追加质押"} sub={`#${l.id} · ${tn(l.term)}`} onClose={onClose}>
      <div className="lrow"><span className="lg">{kind === "repay" ? "待还总额" : "当前质押"}</span><span className="sp" /><b>{kind === "repay" ? `${n(l.debt, 6)} ${l.asset}` : `${n(l.collateral, 8)} ${l.collateralAsset}`}</b></div>
      <input className="xin" inputMode="decimal" placeholder={kind === "repay" ? `还款数量（${l.asset}）` : `追加数量（${l.collateralAsset}）`} value={v} onChange={(e) => setV(e.target.value.replace(/[^\d.]/g, ""))} />
      {kind === "repay" && l.term !== "flexible" && <p className="lg">定期借款提前还款可能收取违约金，以实际扣款为准。</p>}
      {err && <div className="fx-err">{err}</div>}
      <div className="lbtns">
        {kind === "repay" && <button className="lb" onClick={() => go(true)}>全部还清</button>}
        <button className={"lb dark" + (busy || !(Number(v) > 0) ? " dis" : "")} onClick={() => Number(v) > 0 && go()}>{busy ? "提交中…" : "确认"}</button>
      </div>
    </Mask>
  );
}

/** 页面主体：汇总 + 我的借款 + 借贷市场 */
export function LoanBody({ faq }: { faq?: React.ReactNode }) {
  const authed = !!useAuthStore((s) => s.accessToken);
  const { market, source, loading, reload } = useLoanMarket();
  const [tab, setTab] = useState<"active" | "closed">("active");
  const sum = useRest<{ active: number; totalDebt: string; totalCollateralValue: string; asset: string }>(authed ? "/loans/summary" : null);
  const loans = useRest<{ items: LoanV[] }>(authed ? `/loans?status=${tab}` : null);
  const [b, setB] = useState<string | null>(null);
  const [act, setAct] = useState<{ l: LoanV; k: "repay" | "collateral" } | null>(null);
  const refresh = () => (reload(), sum.reload(), loans.reload());
  const renew = async (l: LoanV) => {
    const r = await rest("POST", `/loans/${l.id}/renew`, {});
    r.ok ? (toast("续借成功"), refresh()) : toast(r.error ?? "续借失败", "err");
  };
  const items = loans.data?.items ?? [];
  return (
    <>
      <div className="eh lb2"><div className="ewrap">
        <div className="chk">{["随借随还", "极速放款", "零手续费", "支持提现"].map((x) => <span key={x}><i>✓</i>{x}</span>)}</div>
        <div className="ebar"><div><span className="g">当前借贷 ({sum.data?.active ?? 0})</span></div></div>
        <div className="ebar">
          <div><span className="g">总负债({sum.data?.asset ?? "USDT"})</span><b>{n(sum.data?.totalDebt ?? 0, 4)}</b></div>
          <div><span className="g">总质押(USDT)</span><b>{n(sum.data?.totalCollateralValue ?? 0, 2)}</b></div>
          <span className="sp" />
          <button className={"pill " + (tab === "closed" ? "lime" : "white")} onClick={() => setTab("closed")}>历史</button>
          <button className={"pill " + (tab === "active" ? "lime" : "white")} aria-selected={tab === "active"} onClick={() => setTab("active")}>进行中</button>
        </div>
        {authed && (
          <div className="fx-loans">
            {loans.loading ? <div className="fx-skel" style={{ height: 56 }} /> : items.length === 0 ? <div className="g fx-empty">{tab === "active" ? "暂无进行中的借款" : "暂无历史借款"}</div> : items.map((l) => (
              <div key={l.id} className={"fx-loan" + (l.warning ? " warn" : "")}>
                <span><CoinIcon s={l.asset} size={20} /> <b>{n(l.debt, 4)} {l.asset}</b><small className="g">{tn(l.term)}</small></span>
                <span><small className="g">质押物</small><b><CoinIcon s={l.collateralAsset} size={16} /> {n(l.collateral, 8)} {l.collateralAsset}</b></span>
                <span><small className="g">质押率</small><b className={l.warning ? "down2" : ""}>{pc(l.ltv)}</b></span>
                <span><small className="g">强平价</small><b>{n(l.liquidationPrice, 2)}</b></span>
                <span><small className="g">{l.dueAt ? "到期" : "借款时间"}</small><b>{dt(l.dueAt ?? l.startedAt)}{l.overdue ? " · 已逾期" : ""}</b></span>
                {l.status === "active" ? (
                  <span className="r">
                    <button className="lb sm" onClick={() => setAct({ l, k: "collateral" })}>追加质押</button>
                    {l.term !== "flexible" && <button className="lb sm" onClick={() => renew(l)}>续借</button>}
                    <button className="lb lime sm" onClick={() => setAct({ l, k: "repay" })}>还款</button>
                  </span>
                ) : <span className="r g">{ST[l.status] ?? l.status}</span>}
              </div>
            ))}
          </div>
        )}
      </div></div>
      <div className="light"><div className="ewrap">
        <div className="lsteps">{["01 提交申请", "02 收到借款", "03 还款付息", "04 回收质押物"].map((x, i) => <span key={x}><i>{i + 1}</i>{x}</span>)}</div>
        <div className="lrow"><h2 className="lh2">借贷市场</h2>{source === "demo" && <span className="fx-src">演示数据（服务不可达）</span>}<span className="lg">▤ 初始质押率 {pc(market?.initialLtv ?? 0.8, 0)} · 质押物 {market?.collaterals.map((c) => c.asset).join(" / ") ?? "--"}</span></div>
        <div className="lth"><span>可借币种</span>{(market?.terms ?? DEMO.terms).map((t) => <span key={t.term}>{tn(t.term)}</span>)}<span className="r">操作</span></div>
        {loading && !market ? <div className="fx-skel" style={{ height: 48 }} /> : market ? (
          <div className="ltr e5">
            <span><CoinIcon s={market.asset} size={22} /> {market.asset}</span>
            {market.terms.map((t) => <span key={t.term}>{(Number(t.hourlyRate) * 100).toFixed(6)}% / <b>{pc(t.annualRate)}</b></span>)}
            <span className="r"><button className="lb lime sm" onClick={() => setB(market.terms[0]?.term ?? "flexible")}><G n="bolt" s={14} /> 借贷</button></span>
          </div>
        ) : <div className="lg fx-empty">暂无可借币种</div>}
        {faq}
      </div></div>
      {b && market && <BorrowModal m={market} term0={b} onClose={() => setB(null)} onDone={() => (setB(null), refresh())} />}
      {act && <ActModal l={act.l} kind={act.k} onClose={() => setAct(null)} onDone={() => (setAct(null), refresh())} />}
    </>
  );
}

/** 借贷订单：GET /loans/records?type=borrow|repay|collateral|renew|liquidate */
const RT = [["borrow", "借款记录"], ["repay", "还款记录"], ["collateral", "质押率调整记录"], ["renew", "续借记录"], ["liquidate", "强平记录"]] as const;
type Rec = { id: number; loanId: number; type: string; amount: string; interest: string; penalty: string; collateral: string; term: string; asset: string; collateralAsset: string; time: number; ltv?: string; price?: string; detail?: { dueAt?: number } | null };
const ymd = (ms: number) => { const d = new Date(ms); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`; };
export function LoanRecords() {
  const authed = !!useAuthStore((s) => s.accessToken);
  const [t, setT] = useState<(typeof RT)[number][0]>("borrow");
  const r = useRest<{ items: Rec[] }>(authed ? `/loans/records?type=${t}` : null);
  const cols = ["时间", "订单号", "订单类型", "借款币种", t === "liquidate" ? "偿还本金" : "数量", "利息 / 违约金", "质押币种", "质押数量", "质押率"];
  const [fk, setFk] = useState({ term: "", asset: "", col: "", days: "7" });
  const now = Date.now();
  const rows = (r.data?.items ?? []).filter((x) => (!fk.term || (fk.term === "flexible" ? x.term === "flexible" : x.term !== "flexible")) && (!fk.asset || x.asset === fk.asset) && (!fk.col || x.collateralAsset === fk.col) && now - x.time <= Number(fk.days) * 864e5);
  const sel = (k: keyof typeof fk, l: string, o: [string, string][]) => <label className="lnf">{l}<select className="fsel" value={fk[k]} onChange={(e) => setFk({ ...fk, [k]: e.target.value })}>{o.map(([v, t]) => <option key={v} value={v}>{t}</option>)}</select></label>;
  return (
    <div className="lcard">
      <div className="pt fx-rt">{RT.map(([k, l]) => <b key={k} className={t === k ? "on" : ""} onClick={() => setT(k)}>{l}</b>)}</div>
      <div className="lnfr">{sel("term", "订单类型", [["", "全部"], ["flexible", "活期"], ["fixed", "定期"]])}{sel("asset", "借款币种", [["", "全部"], ["USDT", "USDT"], ["USDC", "USDC"]])}{sel("col", "质押币种", [["", "全部"], ["BTC", "BTC"], ["ETH", "ETH"]])}{sel("days", "时间", [["7", "近 7 天"], ["30", "近 30 天"], ["90", "近 90 天"]])}<button className="lb sm lnrf" onClick={() => r.reload()}>⟳ 刷新</button></div>
      <div className="fx-tbl">
        <div className="fx-tr th">{cols.map((c) => <span key={c}>{c}</span>)}</div>
        {!authed ? <div className="lg fx-empty">登录后查看借贷订单</div> : r.loading ? <div className="fx-skel" style={{ height: 40 }} /> : r.error ? <div className="fx-err">{r.error}</div> : rows.length === 0 ? <div className="lempty"><div className="lbox">▤</div><span>暂无数据</span></div> : rows.map((x) => (
          <div key={x.id} className="fx-tr">
            <span>{dt(x.time)}</span><span>#{x.loanId}</span><span>{tn(x.term).split(" · ")[0]}</span>
            <span><CoinIcon s={x.asset} size={16} /> {x.asset}</span><span>{x.type === "renew" ? (x.detail?.dueAt ? `新到期 ${ymd(x.detail.dueAt)}` : "--") : n(x.amount, 6)}</span>
            <span>{n(x.interest, 6)} / {n(x.penalty, 6)}</span>
            <span><CoinIcon s={x.collateralAsset} size={16} /> {x.collateralAsset}</span><span>{!x.collateral || (x.type === "renew" && !(Number(x.collateral) > 0)) ? "--" : n(x.collateral, 8)}</span><span>{pc(x.ltv)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
