"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Button, Nav } from "@linbit/ui";
import { useAuthStore } from "@/lib/store";

const NAV_ITEMS = [
  { label: "首页", href: "/" },
  { label: "行情", href: "/markets" },
  { label: "交易", href: "/trade/BTCUSDT" },
  { label: "资产", href: "/assets" },
];

export function SiteNav() {
  const pathname = usePathname();
  const router = useRouter();
  const { isAuthed, logout } = useAuthStore();

  // 避免服务端/客户端认证态不一致导致的 hydration 警告
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const right =
    mounted && isAuthed ? (
      <>
        <Link href="/assets">
          <Button variant="secondary" size="sm">
            我的资产
          </Button>
        </Link>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            logout();
            router.push("/");
          }}
        >
          登出
        </Button>
      </>
    ) : (
      <>
        <Link href="/login">
          <Button variant="ghost" size="sm">
            登录
          </Button>
        </Link>
        <Link href="/register">
          <Button variant="primary" size="sm">
            注册
          </Button>
        </Link>
      </>
    );

  return (
    <Nav
      items={NAV_ITEMS}
      activeHref={pathname}
      right={right}
      renderLink={(href, children, className) => (
        <Link href={href} className={className}>
          {children}
        </Link>
      )}
    />
  );
}
