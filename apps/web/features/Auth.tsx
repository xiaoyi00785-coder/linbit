"use client";
import { useState } from "react";
import { Link, useNavigate } from "@/lib/router";
import {
  COUNTRIES, displayAccount, isChallenge, login, loginResend, loginVerify, register, resetPassword,
  sendAuthCode, setMe, toE164, type AuthTokens, type LoginChallenge, type Target,
} from "@/lib/account";
import { useAuthStore } from "@/lib/store";
// @ts-ignore legacy 模块未加类型
import { setAuthed } from "@/legacy/auth";
import { CodeInput, Steps, isEmail, isPhone } from "./common";

function Shell({ title, sw, children }: { title: string; sw?: [string, string]; children: React.ReactNode }) {
  return (
    <main className="auth">
      <div className="aleft">
        <h1>
          在奖励中心最高可赚
          <br />
          <span className="lime">215 USDT</span> 新人奖励
        </h1>
        <div className="chest">
          <img src="/img/auth.webp" alt="" />
        </div>
      </div>
      <div className="aright">
        <div className="ah">
          <h2>{title}</h2>
          {sw && (
            <Link to={sw[0]} className="sw">
              ⇄ {sw[1]}
            </Link>
          )}
        </div>
        {children}
      </div>
    </main>
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
  const [val, setVal] = useState("");
  const valid = kind === "邮箱" ? isEmail(val) : /^\d{5,14}$/.test(val);
  const target = (): Target => (kind === "邮箱" ? { email: val } : { phone: toE164(cc, val), countryCode: cc });
  const shown = kind === "邮箱" ? val : `+${cc} ${val}`;
  const ui = (
    <>
      <div className="pt aline">
        {(["邮箱", "手机"] as const).map((x) => (
          <b key={x} className={kind === x ? "on" : ""} onClick={() => (setKind(x), setVal(""))}>
            {x}
          </b>
        ))}
      </div>
      {kind === "邮箱" ? (
        <input className="ai" placeholder="邮箱地址" value={val} onChange={(e) => setVal(e.target.value.trim())} />
      ) : (
        <div className="fx-phone">
          <select className="ai fx-cc" value={cc} onChange={(e) => setCc(e.target.value)}>
            {COUNTRIES.map(([c, n]) => (
              <option key={c} value={c}>
                +{c} {n}
              </option>
            ))}
          </select>
          <input className="ai" inputMode="numeric" placeholder="手机号" value={val} onChange={(e) => setVal(e.target.value.replace(/\D/g, ""))} />
        </div>
      )}
    </>
  );
  return { kind, valid, target, shown, ui };
}

/** 注册：账户+密码 → 验证码（POST /auth/code/send → POST /auth/register） */
export function Register() {
  const a = useAcct();
  const [step, setStep] = useState(0);
  const [pwd, setPwd] = useState("");
  const [invite, setInvite] = useState("");
  const [agree, setAgree] = useState(false);
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const finish = useFinish();
  const validPwd = pwd.length >= 8 && /[A-Za-z]/.test(pwd) && /\d/.test(pwd);
  const send = async () => {
    const r = await sendAuthCode("register", a.target());
    return { ok: r.ok, ttl: r.data?.ttlSeconds, error: r.error };
  };

  return (
    <Shell title="创建账户" sw={["/login", "立即登录"]}>
      <Steps items={["填写信息", "安全验证"]} cur={step} />
      {step === 0 ? (
        <>
          {a.ui}
          <input className="ai" type="password" placeholder="设置密码（8 位以上，含字母和数字）" value={pwd} onChange={(e) => setPwd(e.target.value)} />
          <input className="ai" placeholder="邀请码（选填）" value={invite} onChange={(e) => setInvite(e.target.value.trim())} />
          <label className="agree" onClick={() => setAgree(!agree)}>
            <i className={agree ? "on" : ""}>{agree && "✓"}</i>
            <span>
              点击「下一步」即表示你同意 linbit 的 <a className="lime">服务条款</a> 和 <a className="lime">隐私政策</a>。
            </span>
          </label>
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (agree && a.valid && validPwd && !busy ? "" : " dis")}
            onClick={async () => {
              if (!a.valid) return setErr(`请输入正确的${a.kind === "邮箱" ? "邮箱" : "手机号"}`);
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

  return (
    <Shell title="欢迎来到 linbit" sw={["/register", "立即注册"]}>
      <Steps items={["账户密码", "安全验证"]} cur={ch ? 1 : 0} />
      {!ch ? (
        <>
          {a.ui}
          <input className="ai" type="password" placeholder="请输入密码" value={pwd} onChange={(e) => setPwd(e.target.value)} />
          <Link to="/forgot-password" className="fg">
            忘记密码？
          </Link>
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (a.valid && pwd && !busy ? "" : " dis")}
            onClick={async () => {
              if (!a.valid || !pwd) return setErr("请输入正确的账户和密码");
              setBusy(true);
              const t = a.target();
              const r = await login({ ...t, password: pwd });
              setBusy(false);
              if (!r.ok || !r.data) return setErr(r.error ?? "登录失败");
              setErr("");
              setTgt(t);
              if (isChallenge(r.data)) {
                setCh(r.data);
                setMethod(r.data.methods[0] ?? "totp");
                setCode("");
              } else finish(r.data, t);
            }}
          >
            {busy ? "登录中…" : "登录"}
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
              if (!r.ok || !r.data) return setErr(r.error ?? "验证失败");
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
          <button className={"abtn" + (code.length === 6 ? "" : " dis")} onClick={() => code.length === 6 && (setErr(""), setStep(2))}>
            下一步
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
              setBusy(true);
              const r = await resetPassword({ ...a.target(), code, newPassword: pwd });
              setBusy(false);
              if (r.ok) return setStep(3);
              setErr(r.error ?? "重置失败");
              if (/code|验证码/i.test(r.error ?? "")) setStep(1);
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
