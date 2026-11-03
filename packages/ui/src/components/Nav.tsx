import * as React from "react";
import { cn } from "../cn";
import { TextLogo } from "./TextLogo";

export interface NavItem {
  label: React.ReactNode;
  href: string;
}

export interface NavProps {
  items: NavItem[];
  /** 当前激活的 href，用于高亮 */
  activeHref?: string;
  /** 右侧操作区（登录/注册按钮、头像等） */
  right?: React.ReactNode;
  /**
   * 链接渲染器，默认使用 <a>。在 Next.js 中传入基于 next/link 的渲染器：
   * renderLink={(href, children, className) => <Link href={href} className={className}>{children}</Link>}
   */
  renderLink?: (
    href: string,
    children: React.ReactNode,
    className?: string,
  ) => React.ReactNode;
  className?: string;
}

const defaultRenderLink: NonNullable<NavProps["renderLink"]> = (
  href,
  children,
  className,
) => (
  <a href={href} className={className}>
    {children}
  </a>
);

export function Nav({
  items,
  activeHref,
  right,
  renderLink = defaultRenderLink,
  className,
}: NavProps) {
  return (
    <header
      className={cn(
        "sticky top-0 z-20 flex h-16 items-center gap-8 border-b border-lb-border bg-lb-bg px-6",
        className,
      )}
    >
      {renderLink(
        "/",
        <TextLogo size={22} />,
        "mr-4 flex items-center",
      )}
      <nav className="flex items-center gap-7">
        {items.map((item) => {
          const active =
            activeHref === item.href ||
            (item.href !== "/" && activeHref?.startsWith(item.href));
          return (
            <React.Fragment key={item.href + String(item.label)}>
              {renderLink(
                item.href,
                item.label,
                cn(
                  "text-[15px] font-medium transition-colors",
                  active
                    ? "text-lb-primary"
                    : "text-lb-text hover:text-lb-primary",
                ),
              )}
            </React.Fragment>
          );
        })}
      </nav>
      <div className="flex-1" />
      {right && <div className="flex items-center gap-2">{right}</div>}
    </header>
  );
}
