"use client";
import { openM } from "./Kit";
/** 安全中心：对接 GET /account/security 及各修改接口（feat/security-settings） */
import { useEffect, useState, type ReactNode } from "react";
import { G } from "@/components/Glyph";
import { Shell, Side, ACC } from "./layout";
import { CodeInput } from "./common";
import { Skel } from "./Account";
import { toast, goto } from "@/lib/ui";
import { maskEmail, sendAccountCode } from "@/lib/account";
import { TotpErr, TwofaRequired, twofaKind, type TwofaKind } from "@/lib/twofa";
import {
  changeContact, changePassword, createPasskey, deletePasskey, fmtUntil, fundPassword, listPasskeys, listSessions, revokeOthers, revokeSession,
  secChanged, secMsg, sendContactCode, setAntiPhishing, useSecurity, webauthnSupported, type PasskeyItem, type SecurityOverview, type SessionItem,
} from "@/lib/security";

type Kind = "password" | "email" | "phone" | "fund-set" | "fund-change" | "fund-reset" | "phishing" | "passkey";
const TITLE: Record<Kind, string> = { password: "修改登录密码", email: "更换邮箱", phone: "绑定 / 更换手机", "fund-set": "设置资金密码", "fund-change": "修改资金密码", "fund-reset": "重置资金密码", phishing: "设置防钓鱼码", passkey: "添加通行密钥" };
const digits6 = (v: string) => v.replace(/\D/g, "").slice(0, 6);

/** 二次验证 + 提现锁定提示 + 2FA 拆分错误码的通用弹窗 */
function SecModal({ kind, sec, onClose }: { kind: Kind; sec: SecurityOverview; onClose: () => void }) {
  const totp = !!sec.twoFactor?.enabled;
  const hours = sec.policy?.withdrawLockHours ?? 24;
  const [f, setF] = useState<Record<string, string>>({ cc: "+86" });
  const [code, setCode] = useState("");
  const [tfa, setTfa] = useState<TwofaKind>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const v = (k: string) => f[k] ?? "";
  const set = (k: string, d = false) => (e: { target: { value: string } }) => setF({ ...f, [k]: d ? digits6(e.target.value) : e.target.value });
  const verify = totp ? { totpCode: code } : { code };
  const contact = kind === "email" ? { email: v("email").trim() } : { phone: v("phone").trim(), countryCode: v("cc") };

  async function submit() {
    setErr("");
    if (code.length !== 6) return setErr(totp ? "请输入 6 位谷歌动态码" : "请输入 6 位安全验证码");
    let r;
    if (kind === "password") {
      if (!v("old")) return setErr("请输入原登录密码");
      if (v("new").length < 8) return setErr("新密码至少 8 位");
      if (v("new") !== v("new2")) return setErr("两次输入的新密码不一致");
      setBusy(true); r = await changePassword({ oldPassword: v("old"), newPassword: v("new"), ...verify });
    } else if (kind === "email" || kind === "phone") {
      if (kind === "email" ? !/^\S+@\S+\.\S+$/.test(v("email")) : !/^\d{5,15}$/.test(v("phone"))) return setErr(kind === "email" ? "请输入正确的新邮箱" : "请输入正确的手机号");
      if (v("newCode").length !== 6 || v("oldCode").length !== 6) return setErr("请填写新、旧联系方式收到的验证码");
      setBusy(true); r = await changeContact(kind, { ...contact, newCode: v("newCode"), oldCode: v("oldCode"), ...(totp ? { totpCode: code } : {}) });
    } else if (kind.startsWith("fund")) {
      if (kind === "fund-change" && v("oldFp").length !== 6) return setErr("请输入 6 位原资金密码");
      if (v("fp").length !== 6) return setErr("资金密码须为 6 位数字");
      if (v("fp") !== v("fp2")) return setErr("两次输入的资金密码不一致");
      setBusy(true); r = await fundPassword(kind === "fund-set" ? "set" : kind === "fund-change" ? "change" : "reset", { fundPassword: v("fp"), ...(kind === "fund-change" ? { oldFundPassword: v("oldFp") } : {}), ...verify });
    } else if (kind === "phishing") {
      if (!/^[A-Za-z0-9]{4,20}$/.test(v("ap"))) return setErr("防钓鱼码为 4–20 位字母或数字");
      setBusy(true); r = await setAntiPhishing({ antiPhishingCode: v("ap"), ...verify });
    } else {
      if (!webauthnSupported()) return setErr("当前浏览器不支持通行密钥");
      setBusy(true); r = await createPasskey(verify, v("name") || "我的设备");
    }
    setBusy(false);
    const k = twofaKind(r);
    if (k) { setTfa(k); if (k === "invalid") setCode(""); return; }
    if (!r.ok) return setErr(r.code === "FUND_PASSWORD_LOCKED" ? `${secMsg(r)}，请于 ${fmtUntil((r as { data?: { lockedUntil?: string } }).data?.lockedUntil) || "稍后"} 再试或重置资金密码` : secMsg(r));
    toast(kind === "password" ? "登录密码已修改，其他设备已退出登录" : "设置成功");
    secChanged(); onClose();
  }

  const pw = (k: string, ph: string, d = false) => <input type="password" inputMode={d ? "numeric" : undefined} maxLength={d ? 6 : 128} placeholder={ph} value={v(k)} onChange={set(k, d)} />;
  const oldLabel = kind === "email" ? (sec.email ? `原邮箱验证码（${maskEmail(sec.email)}）` : "原联系方式验证码") : (sec.phone ? `原手机验证码（${sec.phone}）` : "原联系方式验证码");
  return (
    <div className="fx-modal" onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()}>
        <h3>{TITLE[kind]}</h3>
        {kind !== "phishing" && kind !== "passkey" && <div className="sec-lock"><b>!</b>为保障资产安全，修改后 {hours} 小时内将禁止提现和站内转出。</div>}
        {kind === "password" && <>
          <label className="fx-field">原登录密码{pw("old", "请输入原密码")}</label>
          <label className="fx-field">新登录密码{pw("new", "8–128 位，建议包含字母、数字和符号")}</label>
          <label className="fx-field">确认新密码{pw("new2", "再次输入新密码")}</label>
          <p className="lg sm">修改成功后，除当前设备外的其他登录会话将全部退出。</p>
        </>}
        {(kind === "email" || kind === "phone") && <>
          {kind === "email" ? <label className="fx-field">新邮箱<input value={v("email")} onChange={set("email")} placeholder="name@example.com" /></label>
            : <label className="fx-field">新手机号<div className="sec-ph"><select value={v("cc")} onChange={set("cc")}>{["+86", "+852", "+853", "+886", "+1", "+65", "+81", "+82", "+44"].map((c) => <option key={c}>{c}</option>)}</select><input inputMode="numeric" value={v("phone")} onChange={(e) => setF({ ...f, phone: e.target.value.replace(/\D/g, "") })} placeholder="手机号" /></div></label>}
          <div className="fx-field">新{kind === "email" ? "邮箱" : "手机"}验证码<CodeInput value={v("newCode")} onChange={(x) => setF({ ...f, newCode: x })} send={async () => { const r = await sendContactCode(contact); return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.ok ? undefined : secMsg(r) }; }} /></div>
          <div className="fx-field">{oldLabel}<CodeInput value={v("oldCode")} onChange={(x) => setF({ ...f, oldCode: x })} send={async () => { const r = await sendAccountCode("security"); return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.error }; }} /></div>
        </>}
        {kind === "fund-change" && <label className="fx-field">原资金密码{pw("oldFp", "6 位数字", true)}</label>}
        {kind.startsWith("fund") && <>
          <label className="fx-field">{kind === "fund-change" ? "新资金密码" : "资金密码"}{pw("fp", "6 位数字，用于提现与站内转账", true)}</label>
          <label className="fx-field">确认资金密码{pw("fp2", "再次输入", true)}</label>
          {kind === "fund-reset" && <p className="lg sm">重置后将清除错误次数锁定。</p>}
        </>}
        {kind === "phishing" && <><label className="fx-field">防钓鱼码<input maxLength={20} value={v("ap")} onChange={set("ap")} placeholder="4–20 位字母或数字" /></label><p className="lg sm">设置后，平台发出的所有邮件开头都会显示该防钓鱼码，帮助你识别仿冒邮件。</p></>}
        {kind === "passkey" && <><label className="fx-field">密钥名称<input maxLength={64} value={v("name")} onChange={set("name")} placeholder="如：MacBook 指纹" /></label><p className="lg sm">验证通过后，浏览器将调用设备的指纹、面容或安全密钥完成创建。</p></>}
        {totp ? (
          <label className="fx-field">谷歌验证码<input inputMode="numeric" maxLength={6} value={code} placeholder="6 位动态码" onChange={(e) => { setCode(digits6(e.target.value)); if (tfa === "invalid") setTfa(null); }} /></label>
        ) : (
          <div className="fx-field">安全验证码<CodeInput value={code} onChange={(x) => { setCode(x); if (tfa === "invalid") setTfa(null); }} send={async () => { const r = await sendAccountCode("security"); return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.error }; }} /></div>
        )}
        <TotpErr show={tfa === "invalid"} />
        {tfa === "required" && <TwofaRequired onGo={onClose} />}
        {err && <div className="fx-err">{err}</div>}
        <button className={"lb lime wide" + (busy ? " dis" : "")} disabled={busy} onClick={submit}>{busy ? "提交中…" : "确认"}</button>
      </div>
    </div>
  );
}

function Row({ icon, t, d, ok, s, children, id }: { icon: ReactNode; t: ReactNode; d: string; ok: boolean; s: string; children: ReactNode; id?: string }) {
  return <div className="srow" id={id}><span className="sic">{icon}</span><div><b>{t}</b><div className="lg">{d}</div></div><span className="sp" /><span className={ok ? "sok" : "sno"}><i className="sdot" />{s}</span><span className="sec-btns">{children}</span></div>;
}
const fmt = (s?: string | null) => (s ? new Date(s).toLocaleString("zh-CN", { hour12: false }) : "—");

export function Sessions({ compact }: { compact?: boolean }) {
  const [list, setList] = useState<SessionItem[] | null>(null);
  const [err, setErr] = useState("");
  const load = async () => { const r = await listSessions(); if (r.ok) { setList(r.data?.items ?? []); setErr(""); } else { setList([]); setErr(secMsg(r)); } };
  useEffect(() => { load(); }, []);
  return (
    <div className="lcard">
      <h3 className="sec-h">登录设备与会话<span className="sp" />{list && list.filter((x) => !x.current).length > 0 && <button className="lb sm" onClick={async () => { const r = await revokeOthers(); if (!r.ok) return toast(secMsg(r), "err"); toast(`已退出 ${r.data?.revoked ?? 0} 个其他会话`); load(); }}>退出其他所有设备</button>}</h3>
      {!compact && <p className="lg">以下为当前有效的登录会话。如发现陌生设备，请立即退出并修改登录密码。</p>}
      {err && <div className="fx-err">{err}</div>}
      {list === null ? <Skel n={2} h={52} /> : (
        <div className="fx-list">
          {list.length === 0 && !err && <div className="lg">暂无会话</div>}
          {list.map((x) => (
            <div key={x.id} className="fx-item">
              <div className="sp"><b>{x.name || x.userAgent || x.deviceId || "未知设备"}</b>{x.current && <span className="fx-pin" style={{ marginLeft: 8 }}>当前设备</span>}<small>{x.ip ? `IP ${x.ip} · ` : ""}登录于 {fmt(x.createdAt)} · 有效至 {fmt(x.expiresAt)}</small></div>
              {x.current ? <button className="lb sm dis" disabled>当前设备</button> : <button className="lb sm" onClick={async () => { const r = await revokeSession(x.id); if (!r.ok) return toast(secMsg(r), "err"); setList(list.filter((y) => y.id !== x.id)); toast("已退出该设备"); }}>退出</button>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function Passkeys({ onAdd, n }: { onAdd: () => void; n: number }) {
  const [d, setD] = useState<{ enabled: boolean; items: PasskeyItem[] } | null>(null);
  const load = async () => { const r = await listPasskeys(); setD(r.ok && r.data ? { enabled: r.data.enabled, items: r.data.items ?? [] } : { enabled: false, items: [] }); };
  useEffect(() => { load(); const h = () => load(); window.addEventListener("lb:security", h); return () => window.removeEventListener("lb:security", h); }, [n]);
  return (
    <div className="lcard" id="passkey">
      <h3 className="sec-h">通行密钥<span className="sp" />{d?.enabled && <button className="lb sm lime" onClick={onAdd}>添加通行密钥</button>}</h3>
      <p className="lg">使用设备指纹、面容或安全密钥免密登录，无需输入密码。</p>
      {d === null ? <Skel n={1} h={48} /> : !d.enabled ? <div className="lg">通行密钥服务暂未启用</div> : (
        <div className="fx-list">
          {d.items.length === 0 && <div className="lg">尚未添加通行密钥</div>}
          {d.items.map((p) => (
            <div key={p.id} className="fx-item"><div className="sp"><b>{p.name || "通行密钥"}</b><small>添加于 {fmt(p.createdAt)} · 最近使用 {fmt(p.lastUsedAt)}</small></div>
              <button className="lb sm" onClick={async () => { if (!confirm("确认删除该通行密钥？")) return; const r = await deletePasskey(p.id); if (!r.ok) return toast(secMsg(r), "err"); toast("已删除"); secChanged(); }}>删除</button></div>
          ))}
        </div>
      )}
    </div>
  );
}

export function SecurityCenter() {
  const { loading, data, res } = useSecurity();
  const [m, setM] = useState<Kind | null>(null);
  useEffect(() => { if (location.hash) setTimeout(() => document.querySelector(location.hash)?.scrollIntoView({ block: "center" }), 300); }, [loading]);
  const s: SecurityOverview = data ?? {};
  const checks: [string, boolean][] = [["邮箱验证", !!s.emailVerified], ["手机验证", !!s.phoneVerified], ["谷歌验证器", !!s.twoFactor?.enabled], ["资金密码", !!s.fundPasswordSet], ["通行密钥", (s.passkeysCount ?? 0) > 0]];
  const score = checks.filter((c) => c[1]).length;
  const lvl = score >= 4 ? ["高", "up2"] : score >= 2 ? ["中", ""] : ["低", "down2"];
  const lockedFp = s.fundPasswordLockedUntil && new Date(s.fundPasswordLockedUntil) > new Date();
  const wLock = s.withdrawLockedUntil && new Date(s.withdrawLockedUntil) > new Date();
  return (
    <Shell side={<Side items={ACC} />} foot>
      {loading ? <div className="lcard"><Skel n={4} h={56} /></div> : !data ? (
        <div className="lcard"><h3>安全中心</h3><div className="fx-err">{res ? secMsg(res) : "加载失败"}</div></div>
      ) : <>
        {wLock && <div className="sec-lock big"><b>!</b>安全设置变更后 {s.policy?.withdrawLockHours ?? 24} 小时内禁止提现，预计 {fmtUntil(s.withdrawLockedUntil)} 解除{s.withdrawLockReason ? `（${s.withdrawLockReason}）` : ""}。</div>}
        <div className="lcard"><div className="slv"><div className="sring"><b>{score}/{checks.length}</b></div><div><b className={lvl[1]}>{lvl[0]}</b><div className="lg">安全等级</div></div><div className="schk">{checks.map(([x, ok]) => <span key={x} className={ok ? "ok" : ""}><i>✓</i>{x}</span>)}</div></div></div>
        <div className="lcard"><h3>基础安全设置</h3>
          <Row icon={<G n="lock" s={18} />} t="登录密码" d="用于登录账户，修改后其他设备将退出登录" ok s="已设置"><button className="lb sm" onClick={() => setM("password")}>修改</button></Row>
          <Row icon={<G n="mail" s={18} />} t="邮箱验证" d="用于登录、提币、修改安全设置" ok={!!s.emailVerified} s={s.email ? maskEmail(s.email) : "未绑定"}><button className="lb sm" onClick={() => setM("email")}>{s.email ? "更换" : "绑定"}</button></Row>
          <Row icon={<G n="phone" s={18} />} t="手机验证" d="用于登录、提币、修改安全设置" ok={!!s.phoneVerified} s={s.phone || "未绑定"}><button className="lb sm" onClick={() => setM("phone")}>{s.phone ? "更换" : "绑定"}</button></Row>
          <Row icon={<G n="key" s={18} />} t="通行密钥" d="使用设备生物识别安全登录" ok={(s.passkeysCount ?? 0) > 0} s={(s.passkeysCount ?? 0) > 0 ? `已添加 ${s.passkeysCount} 个` : "未开启"}><button className="lb sm" onClick={() => document.getElementById("passkey")?.scrollIntoView({ behavior: "smooth", block: "center" })}>管理</button></Row>
        </div>
        <div className="lcard"><h3>高级安全设置</h3>
          <Row id="2fa" icon="G" t={<>谷歌验证器 <em className="srec">推荐</em></>} d="用于登录、提币等敏感操作" ok={!!s.twoFactor?.enabled} s={s.twoFactor?.enabled ? "已开启" : "未设置"}>{s.twoFactor?.enabled ? <button className="lb sm dis" disabled>已绑定</button> : <button className="lb sm lime" onClick={() => openM("security", { kind: "totp" })}>去绑定</button>}</Row>
          <Row id="fund" icon={<G n="shield" s={18} />} t="资金密码" d={s.policy?.fundPasswordForWithdraw ? "提现、站内转账时需输入的 6 位数字密码" : "6 位数字密码，用于提现与站内转账"} ok={!!s.fundPasswordSet && !lockedFp} s={lockedFp ? `已锁定至 ${fmtUntil(s.fundPasswordLockedUntil)}` : s.fundPasswordSet ? "已设置" : "未设置"}>
            {s.fundPasswordSet ? <><button className="lb sm" onClick={() => setM("fund-change")}>修改</button><button className="lb sm" onClick={() => setM("fund-reset")}>忘记 / 重置</button></> : <button className="lb sm lime" onClick={() => setM("fund-set")}>设置</button>}
          </Row>
          <Row icon={<G n="flag" s={18} />} t="防钓鱼码" d="在官方邮件中显示，帮你识别钓鱼邮件" ok={!!s.antiPhishingSet} s={s.antiPhishingSet ? `已设置 ${s.antiPhishingHint ?? ""}` : "未设置"}><button className="lb sm" onClick={() => setM("phishing")}>{s.antiPhishingSet ? "修改" : "设置"}</button></Row>
        </div>
        <Passkeys n={s.passkeysCount ?? 0} onAdd={() => setM("passkey")} />
        <Sessions compact />
        {m && <SecModal kind={m} sec={s} onClose={() => setM(null)} />}
      </>}
    </Shell>
  );
}
