"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Button, Card, Input } from "@linbit/ui";
import { api } from "@/lib/api";
import { useAuthStore } from "@/lib/store";

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const router = useRouter();
  const login = useAuthStore((s) => s.login);

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isRegister = mode === "register";

  const submit = async () => {
    if (!email || !password) {
      setError("请输入邮箱与密码");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      if (isRegister) {
        const { data, error } = await api.POST("/auth/register", {
          body: {
            email,
            password,
            ...(inviteCode ? { inviteCode } : {}),
          },
        });
        if (error || !data) throw error ?? new Error("register failed");
        login({
          accessToken: data.accessToken,
          refreshToken: data.refreshToken,
          email,
        });
      } else {
        const { data, error } = await api.POST("/auth/login", {
          body: {
            email,
            password,
            ...(totpCode ? { totpCode } : {}),
          },
        });
        if (error || !data) throw error ?? new Error("login failed");
        login({
          accessToken: data.accessToken,
          refreshToken: data.refreshToken,
          email,
        });
      }
      router.push("/assets");
    } catch {
      setError(isRegister ? "注册失败，请稍后重试" : "登录失败，请检查账号或验证码");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-md flex-col px-5 py-16">
      <Card>
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-xl font-semibold">
            {isRegister ? "创建账户" : "欢迎回到 Linbit"}
          </h1>
          <Link
            href={isRegister ? "/login" : "/register"}
            className="text-sm text-lb-primary"
          >
            {isRegister ? "已有账户？登录" : "没有账户？注册"}
          </Link>
        </div>

        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Input
            label="邮箱"
            type="email"
            name="email"
            autoComplete="email"
            placeholder="trader@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <Input
            label="密码"
            type="password"
            name="password"
            autoComplete={isRegister ? "new-password" : "current-password"}
            placeholder={isRegister ? "至少 8 位" : "请输入密码"}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {!isRegister && (
            <Input
              label="2FA 验证码（如已开启）"
              inputMode="numeric"
              placeholder="6 位动态码，可留空"
              value={totpCode}
              onChange={(e) => setTotpCode(e.target.value)}
            />
          )}
          {isRegister && (
            <Input
              label="邀请码（选填）"
              placeholder="如 LB8DEMO"
              value={inviteCode}
              onChange={(e) => setInviteCode(e.target.value)}
            />
          )}

          {error && <p className="text-sm text-lb-down">{error}</p>}

          <Button type="submit" block disabled={submitting}>
            {submitting
              ? "处理中…"
              : isRegister
                ? "创建账户"
                : "登录"}
          </Button>
        </form>

        <p className="mt-4 text-center text-xs text-lb-text-3">
          点击即表示你同意 Linbit 服务条款与隐私政策。
        </p>
      </Card>
    </div>
  );
}
