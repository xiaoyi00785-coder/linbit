/** 社区链接：仅读取配置（NEXT_PUBLIC_SOCIAL_X / _TELEGRAM / _FACEBOOK / _DISCORD / _INSTAGRAM / _YOUTUBE），未配置的渠道不展示。 */
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
export const socialLinks = (): [string, string, string][] =>
  Object.entries(ENV).filter(([, u]) => u && /^https?:\/\//.test(u)).map(([k, u]) => [k, NAME[k], u as string]);
/** 页脚社区：固定展示全部渠道；未配置的渠道 url 为 null（点击提示即将开放，不生成链接） */
export const PRIMARY_SOCIAL = ["x", "telegram", "facebook", "discord", "instagram"];
export const MORE_SOCIAL = ["youtube", "tiktok", "reddit", "medium"];
export const socialItem = (k: string): { k: string; name: string; url: string | null } => {
  const u = ENV[k]; return { k, name: NAME[k] ?? k, url: u && /^https?:\/\//.test(u) ? u : null };
};
