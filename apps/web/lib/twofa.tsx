"use client";
import { openM } from "@/features/Kit";
import { goto } from "@/lib/ui";
import { rget } from "@/lib/rest";
import { useSecurity, type SecurityOverview } from "@/lib/security";
/** 2FA 拆分错误码（后端 APIError.code）：
 *  TWOFA_REQUIRED → 未绑定：提示 + 去绑定；已绑定但未填动态码：提示输入验证码；
 *  INVALID_TOTP   → 动态码错误：在输入框下方提示，弹窗保持打开。 */
export const TWOFA_BIND = "/setting/security#2fa";
export const MSG_TWOFA_REQUIRED = "请先绑定谷歌验证";
export const MSG_TOTP_MISSING = "请输入谷歌验证码";
export const MSG_INVALID_TOTP = "动态码错误，请重新输入";
export type TwofaKind = "required" | "invalid" | null;
export const twofaKind = (r: { code?: string; error?: string }): TwofaKind =>
  r.code === "TWOFA_REQUIRED" ? "required" : r.code === "INVALID_TOTP" ? "invalid" : null;
/** 当前账户是否已绑定谷歌验证器（读 /account/security）。 */
export const twofaBound = async () => { const r = await rget<SecurityOverview>("/account/security"); return !!(r.ok && r.data?.twoFactor?.enabled); };
export function TwofaRequired({ onGo }: { onGo?: () => void }) {
  const sec = useSecurity();
  if (sec.loading) return null;
  if (sec.data?.twoFactor?.enabled) return <div className="fx-err tfa-inv">{MSG_TOTP_MISSING}</div>;
  return (
    <div className="tfa-req">
      <span><b>!</b>{MSG_TWOFA_REQUIRED}，绑定后才能进行该操作。</span>
      <button className="lb sm lime" onClick={() => { onGo?.(); if (location.pathname.startsWith("/setting/security")) openM("security", { kind: "totp" }); else goto(TWOFA_BIND); }}>去绑定</button>
    </div>
  );
}
export const TotpErr = ({ show }: { show: boolean }) => (show ? <div className="fx-err tfa-inv">{MSG_INVALID_TOTP}</div> : null);
