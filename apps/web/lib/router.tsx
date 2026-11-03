"use client";
/** react-router 兼容层：让原型组件在 Next.js App Router 下复用 Link/NavLink/useNavigate 等 API。 */
import NextLink from "next/link";
import { usePathname, useRouter, useParams as useNextParams } from "next/navigation";
import { useEffect, type ReactNode, type CSSProperties } from "react";

type LinkProps = { to: string; children?: ReactNode; className?: string; style?: CSSProperties; onClick?: (e: any) => void; [k: string]: any };
export function Link({ to, ...rest }: LinkProps) {
  return <NextLink href={to} {...rest} />;
}
type NavLinkProps = Omit<LinkProps, "className" | "style"> & {
  className?: string | ((s: { isActive: boolean }) => string);
  style?: CSSProperties | ((s: { isActive: boolean }) => CSSProperties);
  end?: boolean;
};
export function NavLink({ to, className, style, end, children, ...rest }: NavLinkProps) {
  const p = usePathname() || "/";
  const path = to.split(/[?#]/)[0];
  const isActive = end || path === "/" ? p === path : p === path || p.startsWith(path + "/");
  const cn = typeof className === "function" ? className({ isActive }) : [className, isActive ? "active" : ""].filter(Boolean).join(" ");
  const st = typeof style === "function" ? style({ isActive }) : style;
  return <NextLink href={to} className={cn || undefined} style={st} {...rest}>{children}</NextLink>;
}
export function useNavigate() {
  const r = useRouter();
  return (to: string | number, o?: { replace?: boolean }) => {
    if (typeof to === "number") return to < 0 ? r.back() : r.forward();
    return o?.replace ? r.replace(to) : r.push(to);
  };
}
export function useLocation() {
  const pathname = usePathname() || "/";
  return { pathname, search: typeof window === "undefined" ? "" : window.location.search, hash: "" };
}
export function useParams<T extends Record<string, string> = Record<string, string>>() {
  return (useNextParams() || {}) as unknown as T;
}
export function Navigate({ to, replace }: { to: string; replace?: boolean }) {
  const r = useRouter();
  useEffect(() => { replace ? r.replace(to) : r.push(to); }, [to, replace, r]);
  return null;
}
