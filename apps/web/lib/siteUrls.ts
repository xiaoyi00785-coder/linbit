import { siteUrl } from "./site";
/** 对外站点地址（二维码 / 分享用）：统一取 siteUrl()（NEXT_PUBLIC_SITE_URL，未配置时为当前 origin），不写死域名。 */
export const siteOrigin = () => siteUrl();
/** App 下载落地页 */
export const downloadUrl = () => `${siteUrl()}/sim`;
