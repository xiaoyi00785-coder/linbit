"use client";
import { useState } from "react";
import { Link, useNavigate } from "@/lib/router";
import { login, register, resetPassword } from "@/lib/services";
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
  return (tokens: { accessToken: string; refreshToken: string }, email: string) => {
    store.login({ ...tokens, email });
    setAuthed(true);
    nav("/assets");
  };
}

/** 注册：账户+密码 → 验证码 → 完成 */
export function Register() {
  const [kind, setKind] = useState<"邮箱" | "手机">("邮箱");
  const [step, setStep] = useState(0);
  const [acct, setAcct] = useState("");
  const [pwd, setPwd] = useState("");
  const [invite, setInvite] = useState("");
  const [agree, setAgree] = useState(false);
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const finish = useFinish();
  const validAcct = kind === "邮箱" ? isEmail(acct) : isPhone(acct);
  const validPwd = pwd.length >= 8 && /[A-Za-z]/.test(pwd) && /\d/.test(pwd);

  return (
    <Shell title="创建账户" sw={["/login", "立即登录"]}>
      <Steps items={["填写信息", "安全验证"]} cur={step} />
      {step === 0 ? (
        <>
          <div className="pt aline">
            {(["邮箱", "手机"] as const).map((x) => (
              <b key={x} className={kind === x ? "on" : ""} onClick={() => setKind(x)}>
                {x}
              </b>
            ))}
          </div>
          <input className="ai" placeholder={kind === "邮箱" ? "邮箱地址" : "手机号（含国家区号）"} value={acct} onChange={(e) => setAcct(e.target.value.trim())} />
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
            className={"abtn" + (agree && validAcct && validPwd ? "" : " dis")}
            onClick={() => {
              if (!validAcct) return setErr(`请输入正确的${kind}`);
              if (!validPwd) return setErr("密码需 8 位以上且包含字母和数字");
              if (!agree) return setErr("请先同意服务条款");
              setErr("");
              setStep(1);
            }}
          >
            下一步
          </button>
        </>
      ) : (
        <>
          <p className="fx-hint">验证码将发送至 {acct}，10 分钟内有效。</p>
          <CodeInput value={code} onChange={setCode} target={acct} scene="register" />
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (code.length === 6 && !busy ? "" : " dis")}
            onClick={async () => {
              if (code.length !== 6) return setErr("请输入 6 位验证码");
              setBusy(true);
              const r = await register({ email: acct, password: pwd, inviteCode: invite || null });
              setBusy(false);
              if (!r.ok || !r.data) return setErr(r.error ?? "注册失败");
              finish(r.data, acct);
            }}
          >
            {busy ? "提交中…" : "创建账户"}
          </button>
          <a className="fg" onClick={() => setStep(0)}>
            ← 返回修改
          </a>
        </>
      )}
    </Shell>
  );
}

/** 登录：账户+密码 → 2FA（谷歌验证器） → 完成 */
export function Login() {
  const [step, setStep] = useState(0);
  const [acct, setAcct] = useState("");
  const [pwd, setPwd] = useState("");
  const [totp, setTotp] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const finish = useFinish();

  return (
    <Shell title="欢迎来到 linbit" sw={["/register", "立即注册"]}>
      <Steps items={["账户密码", "双重验证 (2FA)"]} cur={step} />
      {step === 0 ? (
        <>
          <input className="ai" placeholder="邮箱 / 手机号" value={acct} onChange={(e) => setAcct(e.target.value.trim())} />
          <input className="ai" type="password" placeholder="请输入密码" value={pwd} onChange={(e) => setPwd(e.target.value)} />
          <Link to="/forgot-password" className="fg">
            忘记密码？
          </Link>
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (acct && pwd ? "" : " dis")}
            onClick={() => {
              if (!acct || !pwd) return setErr("请输入账户和密码");
              setErr("");
              // 后端约定：已开启 2FA 的账户 /auth/login 需携带 totpCode；原型统一进入 2FA 步骤
              setStep(1);
            }}
          >
            下一步
          </button>
        </>
      ) : (
        <>
          <p className="fx-hint">请输入谷歌验证器（Authenticator）中显示的 6 位动态码。</p>
          <input className="ai" inputMode="numeric" maxLength={6} placeholder="6 位 2FA 动态码" value={totp} onChange={(e) => setTotp(e.target.value.replace(/\D/g, ""))} />
          {err && <div className="fx-err">{err}</div>}
          <button
            className={"abtn" + (totp.length === 6 && !busy ? "" : " dis")}
            onClick={async () => {
              if (totp.length !== 6) return setErr("请输入 6 位动态码");
              setBusy(true);
              const r = await login({ email: acct, password: pwd, totpCode: totp });
              setBusy(false);
              if (!r.ok || !r.data) return setErr(r.error ?? "登录失败");
              finish(r.data, acct);
            }}
          >
            {busy ? "验证中…" : "登录"}
          </button>
          <a className="fg" onClick={() => setStep(0)}>
            ← 返回
          </a>
        </>
      )}
    </Shell>
  );
}

/** 找回密码：账户 → 验证码 → 新密码 → 完成 */
export function ForgotPassword() {
  const [step, setStep] = useState(0);
  const [acct, setAcct] = useState("");
  const [code, setCode] = useState("");
  const [pwd, setPwd] = useState("");
  const [pwd2, setPwd2] = useState("");
  const [err, setErr] = useState("");
  const nav = useNavigate();

  return (
    <Shell title="重置密码" sw={["/login", "返回登录"]}>
      <Steps items={["账户", "验证", "新密码"]} cur={step} />
      {step === 0 && (
        <>
          <input className="ai" placeholder="邮箱 / 手机号" value={acct} onChange={(e) => setAcct(e.target.value.trim())} />
          <p className="fx-hint">为保障资金安全，重置密码后 24 小时内禁止提现。</p>
          {err && <div className="fx-err">{err}</div>}
          <button className={"abtn" + (acct ? "" : " dis")} onClick={() => (isEmail(acct) || isPhone(acct) ? (setErr(""), setStep(1)) : setErr("请输入正确的邮箱或手机号"))}>
            下一步
          </button>
        </>
      )}
      {step === 1 && (
        <>
          <p className="fx-hint">验证码将发送至 {acct}</p>
          <CodeInput value={code} onChange={setCode} target={acct} scene="reset_password" />
          <button className={"abtn" + (code.length === 6 ? "" : " dis")} onClick={() => code.length === 6 && setStep(2)}>
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
            className="abtn"
            onClick={async () => {
              if (pwd.length < 8 || !/[A-Za-z]/.test(pwd) || !/\d/.test(pwd)) return setErr("密码需 8 位以上且包含字母和数字");
              if (pwd !== pwd2) return setErr("两次输入的密码不一致");
              const r = await resetPassword({ account: acct, code, password: pwd });
              if (r.ok) setStep(3);
              else setErr(r.error ?? "重置失败");
            }}
          >
            确认重置
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
