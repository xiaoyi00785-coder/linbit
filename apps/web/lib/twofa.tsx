"use client";
import { goto } from "@/lib/ui";
/** 2FA 拆分错误码（后端 APIError.code）：
 *  TWOFA_REQUIRED → 未绑定谷歌验证器：提示 + 跳转安全中心绑定；
 *  INVALID_TOTP   → 动态码错误：在输入框下方提示，弹窗保持打开。 */
export const TWOFA_BIND = "/setting/security#2fa";
export const MSG_TWOFA_REQUIRED = "请先绑定谷歌验证";
export const MSG_INVALID_TOTP = "动态码错误，请重新输入";
export type TwofaKind = "required" | "invalid" | null;
export const twofaKind = (r: { code?: string; error?: string }): TwofaKind =>
  r.code === "TWOFA_REQUIRED" ? "required" : r.code === "INVALID_TOTP" ? "invalid" : null;
export function TwofaRequired({ onGo }: { onGo?: () => void }) {
  return (
    <div className="tfa-req">
      <span><b>!</b>{MSG_TWOFA_REQUIRED}，绑定后才能进行该操作。</span>
      <button className="lb sm lime" onClick={() => { onGo?.(); goto(TWOFA_BIND); }}>去绑定</button>
    </div>
  );
}
export const TotpErr = ({ show }: { show: boolean }) => (show ? <div className="fx-err tfa-inv">{MSG_INVALID_TOTP}</div> : null);
