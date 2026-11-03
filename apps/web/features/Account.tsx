"use client";
import { useEffect, useState } from "react";
import { Link } from "@/lib/router";
import { Shell, Side, ACC } from "./layout";
import { Footer } from "./layout";
import {
  activateCoupon, addAddress, listAddresses, listAnnouncements, listCoupons, listDevices, listNotifications, removeDevice,
  type Announcement, type Coupon, type Device, type Notice, type WhitelistAddress,
} from "@/lib/services";
import { Src } from "./common";

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

export function Devices() {
  const [list, setList, src] = useLoad<Device[]>(listDevices, []);
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="crumb lg">个人中心 / 安全中心 / 设备管理</div>
      <div className="lcard">
        <h3>设备管理 <Src s={src} /></h3>
        <p className="lg">以下为近期登录过你账户的设备。如发现陌生设备，请立即移除并修改登录密码。</p>
        <div className="fx-list">
          {list.map((d) => (
            <div key={d.id} className="fx-item">
              <div className="sp"><b>{d.name}</b>{d.current && <span className="fx-pin" style={{ marginLeft: 8 }}>本机</span>}<small>IP {d.ip} · {d.location}</small></div>
              <span className="lg">{d.lastActive}</span>
              {!d.current && <button className="lb sm" onClick={async () => { await removeDevice(d.id); setList(list.filter((x) => x.id !== d.id)); }}>移除</button>}
            </div>
          ))}
        </div>
      </div>
    </Shell>
  );
}

export function AddressBook() {
  const [list, setList, src] = useLoad<WhitelistAddress[]>(listAddresses, []);
  const [on, setOn] = useState(false);
  const [open, setOpen] = useState(false);
  const [f, setF] = useState({ asset: "USDT", network: "TRC20", address: "", memo: "", label: "", totpCode: "" });
  const [err, setErr] = useState("");
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="crumb lg">个人中心 / 安全中心 / 提现地址管理</div>
      <div className="lcard">
        <div className="lrow"><h3>提现地址白名单 <Src s={src} /></h3><button className="lb lime sm" onClick={() => setOpen(true)}>+ 添加地址</button></div>
        <label className="agree" onClick={() => setOn(!on)}><i className={on ? "on" : ""}>{on && "✓"}</i><span>开启白名单：开启后仅能向白名单内地址提现（开关需 2FA 验证）</span></label>
        <div className="fx-list">
          {list.length === 0 && <div className="lg">暂无地址</div>}
          {list.map((a) => (
            <div key={a.id} className="fx-item">
              <div className="sp"><b>{a.label}</b><small>{a.asset} · {a.network} · {a.address}{a.memo ? ` · Memo ${a.memo}` : ""}</small></div>
              <button className="lb sm" onClick={() => setList(list.filter((x) => x.id !== a.id))}>删除</button>
            </div>
          ))}
        </div>
      </div>
      {open && (
        <div className="fx-modal" onClick={() => setOpen(false)}>
          <div onClick={(e) => e.stopPropagation()}>
            <h3>添加提现地址</h3>
            {([["label", "备注名"], ["asset", "币种"], ["network", "网络"], ["address", "地址"], ["memo", "Memo / Tag（选填）"], ["totpCode", "2FA 动态码"]] as const).map(([k, l]) => (
              <label key={k} className="fx-field">{l}<input value={f[k]} onChange={(e) => setF({ ...f, [k]: e.target.value.trim() })} /></label>
            ))}
            {err && <div className="fx-err">{err}</div>}
            <button className="lb lime wide" onClick={async () => {
              if (!f.label || f.address.length < 20) return setErr("请填写备注名和正确的地址");
              if (!/^\d{6}$/.test(f.totpCode)) return setErr("请输入 6 位 2FA 动态码");
              const r = await addAddress(f);
              if (r.ok && r.data) { setList([...list, r.data]); setOpen(false); setErr(""); }
            }}>确认添加</button>
          </div>
        </div>
      )}
    </Shell>
  );
}
