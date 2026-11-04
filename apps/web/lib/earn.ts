/** 理财 APR 单一口径：一律来自 GET /earn/products。
 *  产品最高年化 = aprMax（后端 = max(apr, bonusApr)，bonusApr 为首段 bonusQuota 内的阶梯利率，非叠加）；仅统计在售产品。 */
export type EarnAprFields = { apr?: string; bonusApr?: string; aprMax?: string; status?: string };
export const productMaxApr = (p: EarnAprFields) => Number(p.aprMax || p.apr);
export const onSale = (p: EarnAprFields) => !p.status || p.status === "online" || p.status === "active";
export const maxApr = (items: EarnAprFields[]) => {
  const l = items.filter(onSale).map(productMaxApr).filter((x) => Number.isFinite(x) && x > 0);
  return l.length ? Math.max(...l) : null;
};
