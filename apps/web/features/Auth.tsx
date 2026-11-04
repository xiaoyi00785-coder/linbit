"use client";
import { passkeyLogin, webauthnSupported } from "@/lib/security";
import { deviceId, deviceName } from "@/lib/account";
import { G } from "@/components/Glyph";
import { useEffect, useState } from "react";
import { useHome } from "@/lib/home";
import { AuthSkel } from "@/components/AuthSkel";
import { Link, useNavigate } from "@/lib/router";
import {
  COUNTRIES, displayAccount, isChallenge, login, loginErrMsg, loginResend, loginVerify, register, resetPassword, verifyResetCode,
  sendAuthCode, setMe, toE164, type AuthTokens, type LoginChallenge, type Target,
} from "@/lib/account";
import { useAuthStore } from "@/lib/store";
// @ts-ignore legacy 模块未加类型
import { setAuthed } from "@/legacy/auth";
import { CodeInput, Steps, isEmail, isPhone } from "./common";
import { asset } from '@/lib/asset';

function Shell({ title, sw, children }: { title: string; sw?: [string, string]; children: React.ReactNode }) {
  return (
    <main className="auth">
      <div className="aleft">
        <h1>
          在奖励中心最高可赚
          <br />
          <GiftAmt /> 新人奖励
        </h1>
        <div className="chest">
          <img src={asset("/img/auth.webp")} alt="" />
        </div>
      </div>
      <div className="aright">
        <div className="ah">
          <h2>{title}</h2>
          {sw && (
            <Link to={sw[0]} className="sw">
              <G n="swap" s={14} /> {sw[1]}
            </Link>
          )}
        </div>
        {children}
      </div>
    </main>
  );
}

/** 新人礼包金额：接口返回前显示骨架，不再写死金额 */
function GiftAmt() {
  const H = useHome();
  if (!H.ncReady) return <span className="sk-in" style={{ display: "inline-block", width: 140, height: "0.9em", verticalAlign: "middle" }} aria-label="加载中" />;
  return H.newcomer?.total ? <span className="lime">{Number(H.newcomer.total).toLocaleString("en-US")} {H.newcomer.asset || "USDT"}</span> : <span className="lime">专属</span>;
}

/** 已登录访问 /login、/register 时直接跳转资产页 */
function useGuestOnly() {
  const nav = useNavigate();
  const tok = useAuthStore((s) => s.accessToken);
  useEffect(() => { if (tok) nav("/assets", { replace: true } as any); }, [tok, nav]);
  return !!tok;
}

const PWD_MAX = 64;
const ACCT_MAX = 254;
/** 密码输入：显示/隐藏切换 + 显式长度上限提示 */
function Pwd({ value, onChange, placeholder, label }: { value: string; onChange: (v: string) => void; placeholder: string; label?: string }) {
  const [show, setShow] = useState(false);
  return (
    <>
      <div className="ai-pwd">
        <input className="ai" type={show ? "text" : "password"} aria-label={label ?? placeholder} placeholder={placeholder} maxLength={PWD_MAX} value={value} onChange={(e) => onChange(e.target.value)} />
        <button type="button" className="ai-eye" aria-label={show ? "隐藏密码" : "显示密码"} aria-pressed={show} onClick={() => setShow(!show)}>
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" />{!show && <path d="M3 3l18 18" />}</svg>
        </button>
      </div>
      {value.length >= PWD_MAX && <div className="fx-hint">密码最多 {PWD_MAX} 位</div>}
    </>
  );
}

function useFinish() {
  const nav = useNavigate();
  const store = useAuthStore();
  return (tokens: AuthTokens, t: Target) => {
    setMe(null);
    store.login({ accessToken: tokens.accessToken, refreshToken: tokens.refreshToken, email: t.email ?? null });
    setAuthed(true);
    nav("/assets");
  };
}

type Kind = "邮箱" | "手机";
/** 邮箱 / 手机（含国家区号选择）输入 */
function useAcct(initial: Kind = "邮箱") {
  const [kind, setKind] = useState<Kind>(initial);
  const [cc, setCc] = useState("86");
  const [val, setVal] = useState(() => {
    if (typeof window === "undefined") return "";
    const x = (new URLSearchParams(window.location.search).get("account") || "").trim().slice(0, ACCT_MAX);
    return initial === "邮箱" && x.includes("@") ? x : "";
  });
  const [touched, setTouched] = useState(false);
  const valid = kind === "邮箱" ? isEmail(val) : /^\d{5,14}$/.test(val);
  const target = (): Target => (kind === "邮箱" ? { email: val } : { phone: toE164(cc, val), countryCode: cc });
  const shown = kind === "邮箱" ? val : `+${cc} ${val}`;
  const label = kind === "邮箱" ? "邮箱" : "手机号";
  /** 区分空值与格式错误 */
  const msg = !val ? `请输入${label}` : !valid ? (kind === "邮箱" ? "邮箱格式不正确，请检查后重新输入" : "手机号格式不正确") : "";
  const ui = (
    <>
      <div className="pt aline" role="tablist" aria-label="账户类型">
        {(["邮箱", "手机"] as const).map((x) => (
          <b key={x} role="tab" tabIndex={0} aria-selected={kind === x} onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setKind(x); setVal(""); setTouched(false); } }} className={kind === x ? "on" : ""} onClick={() => (setKind(x), setVal(""), setTouched(false))}>
            {x}
          </b>
        ))}
      </div>
      {kind === "邮箱" ? (
        <input className="ai" type="email" aria-label="邮箱地址" placeholder="邮箱地址" maxLength={ACCT_MAX} autoComplete="email" aria-invalid={touched && !!msg} value={val} onBlur={() => setTouched(true)} onChange={(e) => setVal(e.target.value.trim())} />
      ) : (
        <div className="fx-phone">
          <select className="ai fx-cc" aria-label="国家/地区区号" value={cc} onChange={(e) => setCc(e.target.value)}>
            {COUNTRIES.map(([c, n]) => (
              <option key={c} value={c}>
                +{c} {n}
              </option>
            ))}
          </select>
          <input className="ai" inputMode="numeric" aria-label="手机号" placeholder="手机号" maxLength={14} aria-invalid={touched && !!msg} value={val} onBlur={() => setTouched(true)} onChange={(e) => setVal(e.target.value.replace(/\D/g, ""))} />
        </div>
      )}
      {touched && msg && <div className="fx-err" role="alert">{msg}</div>}
      {kind === "邮箱" && val.length >= ACCT_MAX && <div className="fx-hint">邮箱最多 {ACCT_MAX} 个字符</div>}
    </>
  );
  return { kind, valid, target, shown, ui, msg, touch: () => setTouched(true) };
}

/** 注册：账户+密码 → 验证码（POST /auth/code/send → POST /auth/register） */
export function Register() {
  const a = useAcct();
  const [step, setStep] = useState(0);
  const [pwd, setPwd] = useState("");
  const [invite, setInvite] = useState(() => (typeof window === "undefined" ? "" : (new URLSearchParams(window.location.search).get("inviteCode") || "").trim().slice(0, 32)));
  const [agree, setAgree] = useState(false);
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const finish = useFinish();
  const authed = useGuestOnly();
  const validPwd = pwd.length >= 8 && /[A-Za-z]/.test(pwd) && /\d/.test(pwd);
  const send = async () => {
    const r = await sendAuthCode("register", a.target());
    return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.error };
  };

  if (authed) return <AuthSkel />;
  return (
    <Shell title="创建账户" sw={["/login", "立即登录"]}>
      <Steps items={["填写信息", "安全验证"]} cur={step} />
      {step === 0 ? (
        <>
          {a.ui}
          <Pwd placeholder={`设置密码（8–${PWD_MAX} 位，含字母和数字）`} value={pwd} onChange={setPwd} />
          <input className="ai" aria-label="邀请码（选填）" maxLength={32} placeholder="邀请码（选填）" value={invite} onChange={(e) => setInvite(e.target.value.trim())} />
          <label className="agree" onClick={() => setAgree(!agree)}>
            <i className={agree ? "on" : ""}>{agree && "✓"}</i>
            <span>
              点击「下一步」即表示你同意 linbit 的 <a className="lime">服务条款</a> 和 <a className="lime">隐私政策</a>。
            </span>
          </label>
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (agree && a.valid && validPwd && !busy ? "" : " dis")}
            disabled={busy}
            aria-disabled={!(agree && a.valid && validPwd)}
            onClick={async () => {
              if (!a.valid) { a.touch(); return setErr(a.msg); }
              if (!pwd) return setErr("请输入密码");
              if (!validPwd) return setErr("密码需 8 位以上且包含字母和数字");
              if (!agree) return setErr("请先同意服务条款");
              setBusy(true);
              const r = await send();
              setBusy(false);
              if (!r.ok) return setErr(r.error ?? "验证码发送失败");
              setErr("");
              setStep(1);
            }}
          >
            {busy ? "发送验证码…" : "下一步"}
          </button>
        </>
      ) : (
        <>
          <p className="fx-hint">验证码已发送至 {a.shown}，请在有效期内输入。</p>
          <CodeInput value={code} onChange={setCode} send={send} autoSent />
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (code.length === 6 && !busy ? "" : " dis")}
            disabled={busy || code.length !== 6}
            onClick={async () => {
              if (code.length !== 6) return setErr("请输入 6 位验证码");
              setBusy(true);
              const t = a.target();
              const r = await register({ ...t, password: pwd, code, inviteCode: invite || null });
              setBusy(false);
              if (!r.ok || !r.data) return setErr(r.error ?? "注册失败");
              finish(r.data, t);
            }}
          >
            {busy ? "提交中…" : "创建账户"}
          </button>
          <a className="fg" onClick={() => (setStep(0), setErr(""))}>
            ← 返回修改
          </a>
        </>
      )}
    </Shell>
  );
}

const METHOD: Record<string, string> = { totp: "谷歌验证器", sms: "短信验证码", email: "邮箱验证码" };

/** 登录：账户+密码 →（新设备/已开 2FA 时）安全验证 → 完成 */
export function Login() {
  const a = useAcct();
  const [pwd, setPwd] = useState("");
  const [ch, setCh] = useState<LoginChallenge | null>(null);
  const [method, setMethod] = useState("totp");
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const finish = useFinish();
  const [tgt, setTgt] = useState<Target>({});
  const authed = useGuestOnly();
  // 429 RATE_LIMITED：按 Retry-After 倒计时，期间禁用登录按钮
  const [wait, setWait] = useState(0);
  useEffect(() => {
    if (wait <= 0) return;
    const id = setTimeout(() => setWait((w) => w - 1), 1000);
    return () => clearTimeout(id);
  }, [wait]);
  useEffect(() => { if (wait === 0) setErr((e) => (e.startsWith("登录尝试过于频繁") ? "" : e)); }, [wait]);

  if (authed) return <AuthSkel />;
  return (
    <Shell title="欢迎来到 linbit" sw={["/register", "立即注册"]}>
      <Steps items={["账户密码", "安全验证"]} cur={ch ? 1 : 0} />
      {!ch ? (
        <>
          {a.ui}
          <Pwd placeholder="请输入密码" value={pwd} onChange={(v) => (setPwd(v), setErr(""))} />
          <Link to="/forgot-password" className="fg">
            忘记密码？
          </Link>
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (a.valid && pwd && !busy && wait <= 0 ? "" : " dis")}
            disabled={busy || wait > 0}
            aria-disabled={!(a.valid && pwd)}
            onClick={async () => {
              if (busy || wait > 0) return;
              if (!a.valid) { a.touch(); return setErr(a.msg); }
              if (!pwd) return setErr("请输入密码");
              setBusy(true);
              const t = a.target();
              const r = await login({ ...t, password: pwd });
              setBusy(false);
              if (!r.ok || !r.data) {
                if (r.code === "RATE_LIMITED") setWait(r.retryAfter ?? 60);
                return setErr(loginErrMsg(r));
              }
              setErr("");
              setTgt(t);
              if (isChallenge(r.data)) {
                setCh(r.data);
                setMethod(r.data.methods[0] ?? "totp");
                setCode("");
              } else finish(r.data, t);
            }}
          >
            {busy ? "登录中…" : wait > 0 ? `请 ${wait} 秒后重试` : "登录"}
          </button>
          <button
            className={"lb wide lpk" + (busy ? " dis" : "")}
            onClick={async () => {
              if (!webauthnSupported()) return setErr("当前浏览器不支持通行密钥");
              setBusy(true);
              const r = await passkeyLogin({ deviceId: deviceId(), deviceName: deviceName() });
              setBusy(false);
              if (!r.ok || !r.data) return setErr(r.code === "PASSKEY_UNAVAILABLE" || r.status === 404 || r.status === undefined ? "通行密钥登录暂未启用" : r.error ?? "通行密钥登录失败");
              setErr("");
              finish(r.data as AuthTokens, {});
            }}
          >
            <G n="key" s={16} /> 使用通行密钥登录
          </button>
        </>
      ) : (
        <>
          {ch.methods.length > 1 && (
            <div className="pt aline">
              {ch.methods.map((m) => (
                <b key={m} className={method === m ? "on" : ""} onClick={() => (setMethod(m), setCode(""))}>
                  {METHOD[m] ?? m}
                </b>
              ))}
            </div>
          )}
          {method === "totp" ? (
            <>
              <p className="fx-hint">请输入谷歌验证器（Authenticator）中显示的 6 位动态码。</p>
              <input className="ai" inputMode="numeric" maxLength={6} placeholder="6 位 2FA 动态码" value={code} onChange={(e) => (setCode(e.target.value.replace(/\D/g, "")), setErr(""))} />
            </>
          ) : (
            <>
              <p className="fx-hint">检测到新设备登录，验证码已发送至 {displayAccount(tgt)}（{METHOD[method]}）。</p>
              <CodeInput
                value={code}
                onChange={setCode}
                autoSent
                send={async () => {
                  const r = await loginResend(ch.challengeToken);
                  return { ok: r.ok, error: r.error };
                }}
              />
            </>
          )}
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (code.length === 6 && !busy ? "" : " dis")}
            onClick={async () => {
              if (code.length !== 6) return setErr("请输入 6 位验证码");
              setBusy(true);
              const r = await loginVerify(ch.challengeToken, method, code);
              setBusy(false);
              if (!r.ok || !r.data) return setErr(r.code === "INVALID_TOTP" ? "动态码错误，请重新输入" : r.error ?? "验证失败");
              finish(r.data, tgt);
            }}
          >
            {busy ? "验证中…" : "确认"}
          </button>
          <a className="fg" onClick={() => (setCh(null), setErr(""))}>
            ← 返回
          </a>
        </>
      )}
    </Shell>
  );
}

/** 找回密码：账户（发码）→ 验证码 → 新密码（POST /auth/password/reset）→ 完成 */
export function ForgotPassword() {
  const a = useAcct();
  const [step, setStep] = useState(0);
  const [code, setCode] = useState("");
  const [pwd, setPwd] = useState("");
  const [pwd2, setPwd2] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [tok, setTok] = useState<{ t: string; exp: number } | null>(null);
  const nav = useNavigate();
  const send = async () => {
    const r = await sendAuthCode("reset_password", a.target());
    return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.error };
  };

  return (
    <Shell title="重置密码" sw={["/login", "返回登录"]}>
      <Steps items={["账户", "验证", "新密码"]} cur={Math.min(step, 2)} />
      {step === 0 && (
        <>
          {a.ui}
          <p className="fx-hint">为保障资金安全，重置密码后 24 小时内禁止提现。</p>
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (a.valid && !busy ? "" : " dis")}
            onClick={async () => {
              if (!a.valid) return setErr("请输入正确的邮箱或手机号");
              setBusy(true);
              const r = await send();
              setBusy(false);
              if (!r.ok) return setErr(r.error ?? "验证码发送失败");
              setErr("");
              setStep(1);
            }}
          >
            {busy ? "发送验证码…" : "下一步"}
          </button>
        </>
      )}
      {step === 1 && (
        <>
          <p className="fx-hint">验证码已发送至 {a.shown}</p>
          <CodeInput value={code} onChange={setCode} send={send} autoSent />
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (code.length === 6 && !busy ? "" : " dis")}
            onClick={async () => {
              if (code.length !== 6 || busy) return;
              setBusy(true);
              const r = await verifyResetCode({ ...a.target(), code });
              setBusy(false);
              if (!r.ok || !r.data?.resetToken) return setErr(r.error ?? "验证码错误或已过期");
              setTok({ t: r.data.resetToken, exp: Date.now() + (r.data.expiresIn ?? 600) * 1000 });
              setErr("");
              setStep(2);
            }}
          >
            {busy ? "校验中…" : "下一步"}
          </button>
        </>
      )}
      {step === 2 && (
        <>
          <input className="ai" type="password" placeholder="新密码（8 位以上，含字母和数字）" value={pwd} onChange={(e) => setPwd(e.target.value)} />
          <input className="ai" type="password" placeholder="确认新密码" value={pwd2} onChange={(e) => setPwd2(e.target.value)} />
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (busy ? " dis" : "")}
            onClick={async () => {
              if (pwd.length < 8 || !/[A-Za-z]/.test(pwd) || !/\d/.test(pwd)) return setErr("密码需 8 位以上且包含字母和数字");
              if (pwd !== pwd2) return setErr("两次输入的密码不一致");
              if (!tok || Date.now() > tok.exp) {
                setTok(null);
                setCode("");
                setStep(1);
                return setErr("重置令牌已过期，请重新获取验证码");
              }
              setBusy(true);
              const r = await resetPassword({ resetToken: tok.t, newPassword: pwd });
              setBusy(false);
              setTok(null); // 一次性令牌，无论成败都作废
              if (r.ok) return setStep(3);
              setCode("");
              setStep(1);
              setErr((r.error ?? "重置失败") + "，请重新获取验证码");
            }}
          >
            {busy ? "提交中…" : "确认重置"}
          </button>
        </>
      )}
      {step === 3 && (
        <>
          <p className="fx-ok">✓ 密码已重置，请使用新密码登录。</p>
          <button className="abtn" onClick={() => nav("/login")}>
            去登录
          </button>
        </>
      )}
    </Shell>
  );
}
