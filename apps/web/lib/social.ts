/** 社区链接：仅读取配置（NEXT_PUBLIC_SOCIAL_X / _TELEGRAM / _FACEBOOK / _DISCORD / _INSTAGRAM / _YOUTUBE），未配置的渠道不展示。 */
const ENV: Record<string, string | undefined> = {
  x: process.env.NEXT_PUBLIC_SOCIAL_X,
  telegram: process.env.NEXT_PUBLIC_SOCIAL_TELEGRAM,
  facebook: process.env.NEXT_PUBLIC_SOCIAL_FACEBOOK,
  discord: process.env.NEXT_PUBLIC_SOCIAL_DISCORD,
  instagram: process.env.NEXT_PUBLIC_SOCIAL_INSTAGRAM,
  youtube: process.env.NEXT_PUBLIC_SOCIAL_YOUTUBE,
};
const NAME: Record<string, string> = { x: "X", telegram: "Telegram", facebook: "Facebook", discord: "Discord", instagram: "Instagram", youtube: "YouTube" };
export const socialLinks = (): [string, string, string][] =>
  Object.entries(ENV).filter(([, u]) => u && /^https?:\/\//.test(u)).map(([k, u]) => [k, NAME[k], u as string]);
