"use client";
import { useEffect, useState } from "react";

/** 验证码输入 + 发送按钮（倒计时）。send 返回 ttl 秒数或错误文案 */
export function CodeInput({
  value,
  onChange,
  send,
  placeholder = "请输入 6 位验证码",
  autoSent = false,
}: {
  value: string;
  onChange: (v: string) => void;
  send: () => Promise<{ ok: boolean; ttl?: number; error?: string }>;
  placeholder?: string;
  autoSent?: boolean;
}) {
  const [left, setLeft] = useState(autoSent ? 60 : 0);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (left <= 0) return;
    const t = setTimeout(() => setLeft(left - 1), 1000);
    return () => clearTimeout(t);
  }, [left]);
  return (
    <>
      <div className="fx-code">
        <input
          className="ai"
          inputMode="numeric"
          maxLength={6}
          placeholder={placeholder}
          value={value}
          onChange={(e) => onChange(e.target.value.replace(/\D/g, ""))}
        />
        <button
          type="button"
          className="fx-send"
          disabled={left > 0 || busy}
          onClick={async () => {
            setBusy(true);
            const r = await send();
            setBusy(false);
            if (r.ok) {
              setErr("");
              setLeft(Math.min(r.ttl ?? 60, 60));
            } else setErr(r.error ?? "发送失败");
          }}
        >
          {busy ? "发送中…" : left > 0 ? `${left}s 后重发` : "获取验证码"}
        </button>
      </div>
      {err && <div className="fx-err">{err}</div>}
    </>
  );
}

export function Steps({ items, cur }: { items: string[]; cur: number }) {
  return (
    <div className="fx-steps">
      {items.map((x, i) => (
        <b key={x} className={i <= cur ? "on" : ""}>
          {i + 1}. {x}
        </b>
      ))}
    </div>
  );
}

export function Src({ s }: { s?: "api" | "mock" }) {
  return s ? <span className="fx-src">{s === "api" ? "API" : "Mock"}</span> : null;
}

export const isEmail = (v: string) => /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(v);
export const isPhone = (v: string) => /^\+?\d{6,15}$/.test(v);
