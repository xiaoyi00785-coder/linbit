import { partnerUrl } from "./partnerUrl.mjs";

/** 合伙人站点（NEXT_PUBLIC_PARTNER_URL，未设置时 https://partner.cowa.click）。 */
export const PARTNER_HOME = partnerUrl(process.env.NEXT_PUBLIC_PARTNER_URL, "/");
/** 合伙人申请页 */
export const PARTNER_APPLY = partnerUrl(process.env.NEXT_PUBLIC_PARTNER_URL, "/apply");
