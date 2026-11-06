"use client";
/**
 * 理财申购弹窗：POST /earn/products/{id}/subscribe。
 * 预计收益按 docs/api/earn.md 口径本地试算（定期：amount×apr×days/365；活期：阶梯年化/365），
 * 仅作展示；实际以接口返回的 expectedInterest 为准。扣款账户为现货（后端约定）。
 */
import { useMemo, useState } from "react";
import { useAuthStore } from "@/lib/store";
import { rest, emitBalances } from "@/lib/rest";
import { toast } from "@/lib/ui";
import { useOverview } from "@/lib/wallet";
import { fmtAmt, trimFixed } from "@/lib/format";
import { useNavigate } from "@/lib/router";

export type EarnProd = {
  id?: string;
  kind?: string;
  asset?: string;
  title?: string;
  apr?: string;
  bonusApr?: string;
  bonusQuota?: string;
  termDays?: number;
  minAmount?: string;
  perUserMax?: string;
  redeemRule?: string;
  hot?: boolean;
  newcomer?: boolean;
  status?: string;
};

const ERR: Record<string, string> = {
  AMOUNT_TOO_SMALL: "申购数量低于最低限额",
  INSUFFICIENT_BALANCE: "现货可用余额不足",
  QUOTA_EXCEEDED: "产品额度不足",
  USER_LIMIT_EXCEEDED: "已达个人申购上限",
  NEWCOMER_ONLY: "该产品仅限新用户",
  PRODUCT_OFFLINE: "产品已下架",
  PRODUCT_NOT_FOUND: "产品不存在",
  VALIDATION_ERROR: "请检查申购数量",
};

/** 对齐后端 floor8 */
function floor8(n: number) {
  if (!Number.isFinite(n) || n <= 0) return 0;
  return Math.floor(n * 1e8 + 1e-12) / 1e8;
}

export function pctApr(v?: string | number | null) {
  if (v == null || v === "") return "--";
  const n = Number(v);
  if (!Number.isFinite(n)) return "--";
  return (n * 100).toFixed(2).replace(/\.?0+$/, "") + "%";
}

export function aprLabel(p: EarnProd) {
  const a = Number(p.apr);
  const b = Number(p.bonusApr);
  if (Number.isFinite(b) && Number.isFinite(a) && b > a) return pctApr(p.apr) + " ~ " + pctApr(p.bonusApr);
  return pctApr(p.apr);
}

/** 本地预计收益：定期=到期利息；活期=日利息 */
export function estYield(p: EarnProd, amount: number) {
  if (!(amount > 0)) return null;
  const apr = Number(p.apr) || 0;
  if (p.kind === "fixed") {
    const days = Number(p.termDays) || 0;
    return { kind: "fixed" as const, total: floor8((amount * apr * days) / 365), days };
  }
  const bonusApr = Number(p.bonusApr) || 0;
  const bonusQuota = Number(p.bonusQuota) || 0;
  const yearly =
    bonusQuota > 0 && bonusApr > apr
      ? Math.min(amount, bonusQuota) * bonusApr + Math.max(0, amount - bonusQuota) * apr
      : amount * (apr || bonusApr);
  return { kind: "simple" as const, daily: floor8(yearly / 365) };
}

function spotFree(ov: ReturnType<typeof useOverview>["data"], asset?: string) {
  if (!ov || !asset) return null;
  const spot = ov.accounts?.find((a) => a.account === "spot");
  const bal = spot?.balances?.find((b) => b.asset === asset);
  return bal ? Number(bal.free) : 0;
}

export function EarnSubscribeModal({
  p,
  onClose,
  onDone,
}: {
  p: EarnProd;
  onClose: () => void;
  onDone?: () => void;
}) {
  const [amt, setAmt] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const authed = !!useAuthStore((s) => s.accessToken);
  const ov = useOverview();
  const nav = useNavigate();
  const asset = p.asset || "";
  const free = authed ? spotFree(ov.data, asset) : null;
  const min = Number(p.minAmount) || 0;
  const n = Number(amt);
  const y = useMemo(() => estYield(p, n), [p, n]);
  const fixed = p.kind === "fixed";
  const title = p.title || (fixed ? `${asset} 定期` : `${asset} 活期`);

  const submit = async () => {
    if (!authed) {
      const back = typeof location !== "undefined" ? location.pathname + location.search + location.hash : "/earn";
      return nav("/login?redirect=" + encodeURIComponent(back));
    }
    if (!p.id) return setErr("产品无效");
    if (!(n > 0) || (min > 0 && n < min)) return setErr(`最低申购 ${p.minAmount || min} ${asset}`);
    if (free != null && n > free) return setErr("现货可用余额不足");
    setBusy(true);
    setErr("");
    const r = await rest<{ expectedInterest?: string; orderId?: number }>("POST", `/earn/products/${encodeURIComponent(p.id)}/subscribe`, {
      amount: trimFixed(n, 8),
    });
    setBusy(false);
    if (!r.ok) {
      setErr(ERR[r.code || ""] || r.error || "申购失败");
      return;
    }
    emitBalances();
    const got = r.data?.expectedInterest;
    toast(got != null && got !== "" ? `申购成功，预计收益 ${fmtAmt(got)} ${asset}` : "申购成功");
    onDone?.();
    onClose();
  };

  return (
    <div className="xmask" onClick={onClose} data-testid="earn-subscribe-mask">
      <div className="xmodal lt" onClick={(e) => e.stopPropagation()} data-testid="earn-subscribe-modal">
        <span className="xx" onClick={onClose}>
          ×
        </span>
        <h3>申购 · {title}</h3>
        <p className="xsub">
          {fixed ? `锁仓 ${p.termDays ?? "--"} 天 · 参考年化 ${aprLabel(p)}` : `活期 · 参考年化 ${aprLabel(p)}`}
          {p.newcomer ? " · 新用户专享" : ""}
        </p>
        <div className="lrow">
          <span className="lg">赎回规则</span>
          <span className="sp" />
          <b>{p.redeemRule || (fixed ? "到期自动返还" : "随时赎回")}</b>
        </div>
        {p.perUserMax && (
          <div className="lrow">
            <span className="lg">个人上限</span>
            <span className="sp" />
            <b>
              {fmtAmt(p.perUserMax)} {asset}
            </b>
          </div>
        )}
        <label className="lg">申购数量（{asset}）</label>
        <input
          className="xin"
          inputMode="decimal"
          data-testid="earn-subscribe-amount"
          placeholder={min > 0 ? `最低 ${p.minAmount}` : "请输入数量"}
          value={amt}
          onChange={(e) => setAmt(e.target.value.replace(/[^\d.]/g, ""))}
        />
        {authed && (
          <div className="lrow">
            <span className="lg">现货可用</span>
            <span className="sp" />
            <b>
              {free == null ? "--" : fmtAmt(free)} {asset}{" "}
              {free != null && free > 0 && (
                <button type="button" className="cv-max" onClick={() => setAmt(trimFixed(free, 8))}>
                  最大
                </button>
              )}
            </b>
          </div>
        )}
        {y?.kind === "fixed" && (
          <div className="lrow">
            <span className="lg">预计到期收益</span>
            <span className="sp" />
            <b className="lime">
              {fmtAmt(y.total)} {asset}
            </b>
          </div>
        )}
        {y?.kind === "simple" && (
          <div className="lrow">
            <span className="lg">预计日收益</span>
            <span className="sp" />
            <b className="lime">
              {fmtAmt(y.daily)} {asset}
            </b>
          </div>
        )}
        <p className="lg">申购将从现货账户扣款；预计收益仅供参考，以实际到账为准。</p>
        {err && (
          <div className="fx-err" role="alert">
            {err}
          </div>
        )}
        <button className={"lb dark wide" + (busy ? " dis" : "")} disabled={busy} onClick={submit} data-testid="earn-subscribe-submit">
          {busy ? "提交中…" : authed ? "确认申购" : "登录后申购"}
        </button>
      </div>
    </div>
  );
}
