import { useEffect, useState } from "react";
import { API_BASE } from "./api";

const ENV: Record<string, string | undefined> = {
  x: process.env.NEXT_PUBLIC_SOCIAL_X,
  telegram: process.env.NEXT_PUBLIC_SOCIAL_TELEGRAM,
  facebook: process.env.NEXT_PUBLIC_SOCIAL_FACEBOOK,
  discord: process.env.NEXT_PUBLIC_SOCIAL_DISCORD,
  instagram: process.env.NEXT_PUBLIC_SOCIAL_INSTAGRAM,
  youtube: process.env.NEXT_PUBLIC_SOCIAL_YOUTUBE,
  tiktok: process.env.NEXT_PUBLIC_SOCIAL_TIKTOK,
  reddit: process.env.NEXT_PUBLIC_SOCIAL_REDDIT,
  medium: process.env.NEXT_PUBLIC_SOCIAL_MEDIUM,
};
const NAME: Record<string, string> = { x: "X", telegram: "Telegram", facebook: "Facebook", discord: "Discord", instagram: "Instagram", youtube: "YouTube", tiktok: "TikTok", reddit: "Reddit", medium: "Medium" };

function validHttp(raw: unknown): raw is string {
  if (typeof raw !== "string" || !raw.trim() || raw.length > 2048) return false;
  try {
    const u = new URL(raw.trim());
    return (u.protocol === "https:" || u.protocol === "http:") && !!u.host && !u.username && !u.password;
  } catch { return false; }
}
function fallback(k: string): string {
  const u = ENV[k];
  return validHttp(u) ? u.trim() : "";
}

let saved: Record<string, string> = {};
let loaded = false;
let request: Promise<void> | null = null;
const subscriptions = new Set<() => void>();

/** 仅获取匿名公开配置；不附带用户登录态，失败时继续使用构建期环境变量。 */
async function fetchSocial(): Promise<void> {
  try {
    const r = await fetch(API_BASE + "/site/social");
    if (r.ok) {
      const body: unknown = await r.json();
      const links = typeof body === "object" && body && typeof (body as { links?: unknown }).links === "object" ? (body as { links: Record<string, unknown> }).links : {};
      const next: Record<string, string> = {};
      for (const k of Object.keys(ENV)) {
        const u = links[k];
        next[k] = validHttp(u) && new URL(u).protocol === "https:" ? u.trim() : "";
      }
      saved = next;
      loaded = true;
    }
  } catch { /* 回退 NEXT_PUBLIC_SOCIAL_* */ }
}

export function useSocialLinks(): void {
  const [, render] = useState(0);
  useEffect(() => {
    const update = () => render((n) => n + 1);
    subscriptions.add(update);
    if (!request) request = fetchSocial().finally(() => subscriptions.forEach((fn) => fn()));
    return () => { subscriptions.delete(update); };
  }, []);
}

export const socialLinks = (): [string, string, string][] =>
  Object.keys(ENV).map((k) => [k, NAME[k], loaded ? saved[k] : fallback(k)] as [string, string, string]).filter(([, , u]) => !!u);
/** 页脚社区：固定展示全部渠道；未配置的渠道 url 为 null（点击提示即将开放，不生成链接）。 */
export const PRIMARY_SOCIAL = ["x", "telegram", "facebook", "discord", "instagram"];
export const MORE_SOCIAL = ["youtube", "tiktok", "reddit", "medium"];
export const socialItem = (k: string): { k: string; name: string; url: string | null } => {
  const u = loaded ? saved[k] : fallback(k);
  return { k, name: NAME[k] ?? k, url: u || null };
};
