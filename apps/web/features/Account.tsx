"use client";
import { fmtLocal } from "@/lib/time";
import { TotpErr, TwofaRequired, twofaKind, type TwofaKind } from "@/lib/twofa";
import { useEffect, useState } from "react";
import { Link } from "@/lib/router";
import { Shell, Side, ACC } from "./layout";
import { Footer } from "./layout";
import { toast } from "@/lib/ui";
import {
  activateCoupon, couponSummary, listAnnouncements, listCoupons, listNotifications, readNotifications,
  type Announcement, type Coupon, type Notice,
} from "@/lib/services";
import {
  addAddress, deleteAddress, deviceId, getWhitelist, listAddresses, listDevices, removeDevice, sendAccountCode, setWhitelist, useMe,
  type AddressItem, type DeviceItem,
} from "@/lib/account";
import { CodeInput, Src } from "./common";
import { Sessions } from "./Security";

function useLoad<T>(fn: () => Promise<{ data?: T; source: "api" | "mock"; ok?: boolean; error?: string }>, init: T) {
  const [d, setD] = useState<T>(init);
  const [src, setSrc] = useState<"api" | "mock">();
  useEffect(() => { fn().then((r) => { setD(r.data ?? init); setSrc(r.source); if (r.ok === false && r.error) toast(r.error, "err"); }); }, []); // eslint-disable-line
  return [d, setD, src] as const;
}
/** 骨架屏：数据加载中占位 */
export function Skel({ n = 3, h = 64 }: { n?: number; h?: number }) {
  return <div className="lb-skel-list">{Array.from({ length: n }, (_, i) => <div key={i} className="lb-skel" style={{ height: h }} />)}</div>;
}

function Tabs<T extends string>({ items, cur, set }: { items: T[]; cur: T; set: (v: T) => void }) {
  return <div className="fx-tabs">{items.map((x) => <b key={x} className={cur === x ? "on" : ""} onClick={() => set(x)}>{x}</b>)}</div>;
}

const CP_TAB = { 未使用: "unused", 使用中: "active", "已使用/过期": "used" } as const;
type CpTab = keyof typeof CP_TAB;

export function Coupons() {
  const [list, setList, src] = useLoad<Coupon[]>(listCoupons, []);
  const [tab, setTab] = useState<CpTab>("未使用");
  const [sum, setSum] = useState<{ available: number; active: number }>();
  const [busy, setBusy] = useState("");
  useEffect(() => { couponSummary().then(setSum); }, []);
  const activate = async (id: string) => {
    setBusy(id);
    const r = await activateCoupon(id);
    setBusy("");
    if (!r.ok) return toast(r.error || "激活失败", "err");
    toast("体验金已激活，可前往合约交易使用");
    setList(list.map((x) => (x.id === id ? { ...x, status: "active" } : x)));
    couponSummary().then(setSum);
  };
  const shown = list.filter((c) => (tab === "已使用/过期" ? c.status === "used" || c.status === "expired" : c.status === CP_TAB[tab]));
  return (
    <main className="light"><div className="lwrap"><section className="lmain" style={{ width: "100%" }}>
      <div className="crumb lg"><Link to="/rewards">福利中心</Link> / 卡券中心</div>
      <h2 className="lh2">卡券中心 <Src s={src} /></h2>
      {sum && <div className="fx-cpsum"><span>可用 <b>{sum.available}</b></span><span>使用中 <b>{sum.active}</b></span></div>}
      <Tabs items={Object.keys(CP_TAB) as CpTab[]} cur={tab} set={setTab} />
      <p className="lg">体验金激活时，合约账户自有资金不得少于体验金金额；体验金仅限合约交易，可抵扣手续费与资金费，本金不可提现，盈利可提现。</p>
      <div className="fx-coupons">
        {!src && <Skel n={2} h={110} />}
        {src && shown.length === 0 && <div className="lg fx-empty">暂无卡券</div>}
        {shown.map((c) => (
          <div key={c.id} className={"fx-cp" + (c.status === "used" || c.status === "expired" ? " dim" : "")}>
            <div className="v"><b>{c.amount}</b><small>USDT</small></div>
            <div className="b"><b>{c.title}</b><span className="lg">{c.desc}</span><span className="lg">有效期至 {c.expireAt}</span>
              {c.status === "unused" && c.type === "trial" && <button className="lb lime sm" disabled={busy === c.id} onClick={() => activate(c.id)}>{busy === c.id ? "激活中…" : "激活"}</button>}
              {c.status === "unused" && c.type === "fee" && <span className="lime2">交易时自动抵扣</span>}
              {c.status === "active" && <Link to="/futures" className="lime2">去合约交易 ›</Link>}
            </div>
          </div>
        ))}
      </div>
    </section></div><Footer light /></main>
  );
}

const KIND: Record<Notice["kind"], string> = { system: "系统", trade: "交易", activity: "活动", security: "安全" };
export function Notifications() {
  const [list, setList, src] = useLoad<Notice[]>(listNotifications, []);
  const [tab, setTab] = useState<string>("全部");
  const shown = list.filter((n) => tab === "全部" || KIND[n.kind] === tab);
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="lcard">
        <div className="lrow"><h3>消息通知 <Src s={src} /></h3><a className="lime2" onClick={async () => { const r = await readNotifications({ all: true }); if (r.source === "api" && !r.ok) return toast(r.error || "操作失败", "err"); setList(list.map((n) => ({ ...n, read: true }))); toast("已全部标记为已读"); window.dispatchEvent(new Event("lb_unread")); }}>全部已读</a></div>
        <Tabs items={["全部", ...Object.values(KIND)]} cur={tab} set={setTab} />
        <div className="fx-list">
          {!src && <Skel n={4} h={52} />}
          {src && shown.length === 0 && <div className="lg fx-empty">暂无消息</div>}
          {shown.map((n) => (
            <div key={n.id} className={"fx-item" + (n.read ? "" : " unread")} onClick={() => { if (!n.read) { readNotifications({ ids: [n.id] }); window.dispatchEvent(new Event("lb_unread")); } setList(list.map((x) => (x.id === n.id ? { ...x, read: true } : x))); }}>
              <span className={"fx-dot" + (n.read ? " off" : "")} />
              <div className="sp"><b>[{KIND[n.kind]}] {n.title}</b><small>{n.body}</small></div>
              <span className="lg">{n.time}</span>
            </div>
          ))}
        </div>
      </div>
    </Shell>
  );
}

export function Announcements() {
  const [list, , src] = useLoad<Announcement[]>(listAnnouncements, []);
  const [cat, setCat] = useState("全部");
  const cats = ["全部", ...Array.from(new Set(list.map((a) => a.category)))];
  return (
    <main className="light"><div className="lwrap"><section className="lmain" style={{ width: "100%" }}>
      <h2 className="lh2">公告中心 <Src s={src} /></h2>
      <Tabs items={cats} cur={cat} set={setCat} />
      <div className="fx-list">
        {!src ? <Skel n={4} h={52} /> : list.filter((a) => cat === "全部" || a.category === cat).length === 0 ? (
          <div className="fx-empty" role="status"><svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><path d="M4 5h16v11H8l-4 4z" /><path d="M8 9h8M8 12h5" /></svg><p>暂无公告</p><small>平台最新公告发布后将显示在这里</small></div>
        ) : list.filter((a) => cat === "全部" || a.category === cat).map((a) => (
          <div key={a.id} className="fx-item"><div className="sp">{a.pinned && <span className="fx-pin">置顶</span>}<b>{a.title}</b><small>{a.category}</small></div><span className="lg">{a.date}</span></div>
        ))}
      </div>
    </section></div><Footer light /></main>
  );
}

const fmtTime = (t?: string) => fmtLocal(t);

export function Devices() {
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="crumb lg">个人中心 / 安全中心 / 设备管理</div>
      <Sessions />
    </Shell>
  );
}

type Pending = { kind: "add" } | { kind: "del"; id: string } | { kind: "wl"; enabled: boolean };

/** 安全验证字段：已开 2FA → 谷歌动态码；否则 → 账户验证码（purpose=address） */
function VerifyField({ totp, value, onChange }: { totp: boolean; value: string; onChange: (v: string) => void }) {
  return totp ? (
    <label className="fx-field">2FA 动态码<input inputMode="numeric" maxLength={6} value={value} onChange={(e) => onChange(e.target.value.replace(/\D/g, ""))} /></label>
  ) : (
    <div className="fx-field">安全验证码
      <CodeInput value={value} onChange={onChange} send={async () => { const r = await sendAccountCode("address"); return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.error }; }} />
    </div>
  );
}

export function AddressBook() {
  const me = useMe();
  const totp = !!me?.twoFactorEnabled;
  const [list, setList, src] = useLoad<AddressItem[]>(listAddresses, []);
  const [wl, setWl] = useState(false);
  const [pend, setPend] = useState<Pending | null>(null);
  const [f, setF] = useState({ asset: "USDT", network: "TRC20", address: "", memo: "", label: "" });
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => { getWhitelist().then((r) => r.ok && r.data && setWl(r.data.enabled)); }, []);
  const open = (p: Pending) => { setPend(p); setCode(""); setErr(""); setTfa(null); };
  const cred = () => (totp ? { totpCode: code } : { code });
  const [tfa, setTfa] = useState<TwofaKind>(null);
  /** 2FA 拆分错误码：动态码错误 → 输入框下提示并保持弹窗；未绑定 → 提示去绑定 */
  const tfaFail = (r: { code?: string; error?: string }) => { const k = twofaKind(r); if (!k) return false; setBusy(false); setTfa(k); setErr(""); if (k === "invalid") setCode(""); return true; };

  const submit = async () => {
    if (!pend) return;
    if (pend.kind === "add" && (!f.label || f.address.length < 20)) return setErr("请填写备注名和正确的地址");
    if (!/^\d{6}$/.test(code)) return setErr(totp ? "请输入 6 位 2FA 动态码" : "请输入 6 位验证码");
    setBusy(true);
    if (pend.kind === "add") {
      const r = await addAddress({ ...f, memo: f.memo || null, whitelisted: true, ...cred() });
      if (tfaFail(r)) return;
      setBusy(false);
      if (!r.ok || !r.data) return setErr(r.error ?? "添加失败");
      setList([...list, r.data]);
      setF({ ...f, address: "", memo: "", label: "" });
    } else if (pend.kind === "del") {
      const id = pend.id;
      const r = await deleteAddress(id, cred());
      if (tfaFail(r)) return;
      setBusy(false);
      if (!r.ok) return setErr(r.error ?? "删除失败");
      setList(list.filter((x) => x.id !== id));
    } else {
      const r = await setWhitelist({ enabled: pend.enabled, ...cred() });
      if (tfaFail(r)) return;
      setBusy(false);
      if (!r.ok) return setErr(r.error ?? "设置失败");
      setWl(pend.enabled);
    }
    setPend(null);
  };

  const title = pend?.kind === "add" ? "添加提现地址" : pend?.kind === "del" ? "删除提现地址" : pend?.kind === "wl" ? (pend.enabled ? "开启白名单" : "关闭白名单") : "";
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="crumb lg">个人中心 / 安全中心 / 提现地址管理</div>
      <div className="lcard">
        <div className="lrow"><h3>提现地址簿 <Src s={src} /></h3><button className="lb lime sm" onClick={() => open({ kind: "add" })}>+ 添加地址</button></div>
        <label className="agree dark" onClick={() => open({ kind: "wl", enabled: !wl })}>
          <i className={wl ? "on" : ""}>{wl && "✓"}</i>
          <span>提现白名单{wl ? "已开启" : "未开启"}：开启后仅能向地址簿中的白名单地址提现（切换需{totp ? " 2FA " : "验证码"}验证）</span>
        </label>
        <div className="fx-list">
          {list.length === 0 && <div className="lg">暂无地址</div>}
          {list.map((a) => (
            <div key={a.id} className="fx-item">
              <div className="sp">
                <b>{a.label || `${a.asset} 地址`}</b>
                {a.whitelisted && <span className="fx-pin" style={{ marginLeft: 8 }}>白名单</span>}
                <small>{a.asset} · {a.network} · {a.address}{a.memo ? ` · Memo ${a.memo}` : ""}</small>
              </div>
              <button className="lb sm" onClick={() => open({ kind: "del", id: a.id })}>删除</button>
            </div>
          ))}
        </div>
      </div>
      {pend && (
        <div className="fx-modal" data-dismissible="false" aria-busy={busy} onClick={() => setPend(null)}>
          <div onClick={(e) => e.stopPropagation()}>
            <span className="fx-x" role="button" aria-label="关闭" onClick={() => setPend(null)}>✕</span>
            <h3>{title}</h3>
            {pend.kind === "add" && ([["label", "备注名"], ["asset", "币种"], ["network", "网络"], ["address", "地址"], ["memo", "Memo / Tag（选填）"]] as const).map(([k, l]) => (
              <label key={k} className="fx-field">{l}<input value={f[k]} onChange={(e) => setF({ ...f, [k]: k === "asset" || k === "network" ? e.target.value.trim().toUpperCase() : e.target.value.trim() })} /></label>
            ))}
            {pend.kind === "del" && <p className="lg">删除后该地址将从地址簿及白名单中移除。</p>}
            <VerifyField totp={totp} value={code} onChange={(v: string) => { setCode(v); if (tfa === "invalid") setTfa(null); }} />
            <TotpErr show={tfa === "invalid"} />
            {tfa === "required" && <TwofaRequired onGo={() => setPend(null)} />}
            {err && <div className="fx-err">{err}</div>}
            <button className={"lb lime wide" + (busy ? " dis" : "")} onClick={submit}>{busy ? "提交中…" : "确认"}</button>
          </div>
        </div>
      )}
    </Shell>
  );
}
