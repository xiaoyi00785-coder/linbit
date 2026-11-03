"use client";
import { useEffect, useState } from "react";
import { sendVerificationCode } from "@/lib/services";

/** 验证码输入 + 发送按钮（60s 倒计时） */
export function CodeInput({
  value,
  onChange,
  target,
  scene,
  placeholder = "请输入 6 位验证码",
}: {
  value: string;
  onChange: (v: string) => void;
  target: string;
  scene: "register" | "reset_password" | "withdraw";
  placeholder?: string;
}) {
  const [left, setLeft] = useState(0);
  useEffect(() => {
    if (left <= 0) return;
    const t = setTimeout(() => setLeft(left - 1), 1000);
    return () => clearTimeout(t);
  }, [left]);
  return (
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
        disabled={left > 0}
        onClick={async () => {
          const r = await sendVerificationCode(target, scene);
          if (r.ok) setLeft(r.data?.ttl ?? 60);
        }}
      >
        {left > 0 ? `${left}s 后重发` : "获取验证码"}
      </button>
    </div>
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
