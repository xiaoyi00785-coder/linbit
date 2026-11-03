"use client";
import { useEffect, useState } from "react";
import { Link } from "@/lib/router";
import { Shell, Side, ACC } from "./layout";
import { Footer } from "./layout";
import {
  activateCoupon, listAnnouncements, listCoupons, listNotifications,
  type Announcement, type Coupon, type Notice,
} from "@/lib/services";
import {
  addAddress, deleteAddress, deviceId, getWhitelist, listAddresses, listDevices, removeDevice, sendAccountCode, setWhitelist, useMe,
  type AddressItem, type DeviceItem,
} from "@/lib/account";
import { CodeInput, Src } from "./common";

function useLoad<T>(fn: () => Promise<{ data?: T; source: "api" | "mock" }>, init: T) {
  const [d, setD] = useState<T>(init);
  const [src, setSrc] = useState<"api" | "mock">();
  useEffect(() => { fn().then((r) => { setD(r.data ?? init); setSrc(r.source); }); }, []); // eslint-disable-line
  return [d, setD, src] as const;
}

function Tabs<T extends string>({ items, cur, set }: { items: T[]; cur: T; set: (v: T) => void }) {
  return <div className="fx-tabs">{items.map((x) => <b key={x} className={cur === x ? "on" : ""} onClick={() => set(x)}>{x}</b>)}</div>;
}

const CP_TAB = { 未使用: "unused", 使用中: "active", "已使用/过期": "used" } as const;
type CpTab = keyof typeof CP_TAB;

export function Coupons() {
  const [list, setList, src] = useLoad<Coupon[]>(listCoupons, []);
  const [tab, setTab] = useState<CpTab>("未使用");
  const shown = list.filter((c) => (tab === "已使用/过期" ? c.status === "used" || c.status === "expired" : c.status === CP_TAB[tab]));
  return (
    <main className="light"><div className="lwrap"><section className="lmain" style={{ width: "100%" }}>
      <div className="crumb lg"><Link to="/rewards">福利中心</Link> / 卡券中心</div>
      <h2 className="lh2">卡券中心 <Src s={src} /></h2>
      <Tabs items={Object.keys(CP_TAB) as CpTab[]} cur={tab} set={setTab} />
      <p className="lg">体验金激活时，合约账户自有资金不得少于体验金金额；体验金仅限合约交易，可抵扣手续费与资金费，本金不可提现，盈利可提现。</p>
      <div className="fx-coupons">
        {shown.length === 0 && <div className="lg">暂无卡券</div>}
        {shown.map((c) => (
          <div key={c.id} className={"fx-cp" + (c.status === "used" || c.status === "expired" ? " dim" : "")}>
            <div className="v"><b>{c.amount}</b><small>USDT</small></div>
            <div className="b"><b>{c.title}</b><span className="lg">{c.desc}</span><span className="lg">有效期至 {c.expireAt}</span>
              {c.status === "unused" && c.type === "trial" && <button className="lb lime sm" onClick={async () => { await activateCoupon(c.id); setList(list.map((x) => (x.id === c.id ? { ...x, status: "active" } : x))); }}>激活</button>}
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
        <div className="lrow"><h3>消息通知 <Src s={src} /></h3><a className="lime2" onClick={() => setList(list.map((n) => ({ ...n, read: true })))}>全部已读</a></div>
        <Tabs items={["全部", ...Object.values(KIND)]} cur={tab} set={setTab} />
        <div className="fx-list">
          {shown.map((n) => (
            <div key={n.id} className="fx-item" onClick={() => setList(list.map((x) => (x.id === n.id ? { ...x, read: true } : x)))}>
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
        {list.filter((a) => cat === "全部" || a.category === cat).map((a) => (
          <div key={a.id} className="fx-item"><div className="sp">{a.pinned && <span className="fx-pin">置顶</span>}<b>{a.title}</b><small>{a.category}</small></div><span className="lg">{a.date}</span></div>
        ))}
      </div>
    </section></div><Footer light /></main>
  );
}

const fmtTime = (t?: string) => (t ? new Date(t).toLocaleString("zh-CN", { hour12: false }) : "-");

export function Devices() {
  const [list, setList, src] = useLoad<DeviceItem[]>(listDevices, []);
  const [err, setErr] = useState("");
  const mine = typeof window !== "undefined" ? `cid:${deviceId()}` : "";
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="crumb lg">个人中心 / 安全中心 / 设备管理</div>
      <div className="lcard">
        <h3>设备管理 <Src s={src} /></h3>
        <p className="lg">以下为近期登录过你账户的设备。如发现陌生设备，请立即移除并修改登录密码。移除后该设备再次登录需重新验证。</p>
        {err && <div className="fx-err">{err}</div>}
        <div className="fx-list">
          {list.length === 0 && <div className="lg">暂无设备</div>}
          {list.map((d) => {
            const cur = d.deviceId === mine;
            return (
              <div key={d.deviceId} className="fx-item">
                <div className="sp">
                  <b>{d.name || d.userAgent || d.deviceId}</b>
                  {cur && <span className="fx-pin" style={{ marginLeft: 8 }}>本机</span>}
                  <small>{d.ip ? `IP ${d.ip} · ` : ""}首次登录 {fmtTime(d.firstSeen)}</small>
                </div>
                <span className="lg">最近活跃 {fmtTime(d.lastSeen)}</span>
                {!cur && (
                  <button className="lb sm" onClick={async () => {
                    const r = await removeDevice(d.deviceId);
                    if (!r.ok) return setErr(r.error ?? "移除失败");
                    setErr("");
                    setList(list.filter((x) => x.deviceId !== d.deviceId));
                  }}>移除</button>
                )}
              </div>
            );
          })}
        </div>
      </div>
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
  const open = (p: Pending) => { setPend(p); setCode(""); setErr(""); };
  const cred = () => (totp ? { totpCode: code } : { code });

  const submit = async () => {
    if (!pend) return;
    if (pend.kind === "add" && (!f.label || f.address.length < 20)) return setErr("请填写备注名和正确的地址");
    if (!/^\d{6}$/.test(code)) return setErr(totp ? "请输入 6 位 2FA 动态码" : "请输入 6 位验证码");
    setBusy(true);
    if (pend.kind === "add") {
      const r = await addAddress({ ...f, memo: f.memo || null, whitelisted: true, ...cred() });
      setBusy(false);
      if (!r.ok || !r.data) return setErr(r.error ?? "添加失败");
      setList([...list, r.data]);
      setF({ ...f, address: "", memo: "", label: "" });
    } else if (pend.kind === "del") {
      const id = pend.id;
      const r = await deleteAddress(id, cred());
      setBusy(false);
      if (!r.ok) return setErr(r.error ?? "删除失败");
      setList(list.filter((x) => x.id !== id));
    } else {
      const r = await setWhitelist({ enabled: pend.enabled, ...cred() });
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
        <div className="fx-modal" onClick={() => setPend(null)}>
          <div onClick={(e) => e.stopPropagation()}>
            <h3>{title}</h3>
            {pend.kind === "add" && ([["label", "备注名"], ["asset", "币种"], ["network", "网络"], ["address", "地址"], ["memo", "Memo / Tag（选填）"]] as const).map(([k, l]) => (
              <label key={k} className="fx-field">{l}<input value={f[k]} onChange={(e) => setF({ ...f, [k]: k === "asset" || k === "network" ? e.target.value.trim().toUpperCase() : e.target.value.trim() })} /></label>
            ))}
            {pend.kind === "del" && <p className="lg">删除后该地址将从地址簿及白名单中移除。</p>}
            <VerifyField totp={totp} value={code} onChange={setCode} />
            {err && <div className="fx-err">{err}</div>}
            <button className={"lb lime wide" + (busy ? " dis" : "")} onClick={submit}>{busy ? "提交中…" : "确认"}</button>
          </div>
        </div>
      )}
    </Shell>
  );
}
