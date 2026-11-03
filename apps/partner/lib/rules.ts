/**
 * 合伙人返佣规则（2026-11-02 产品确认）。
 * 上线后应由后台「返佣配置」下发（GET /partner/rules，接口待定），此处为默认值 / mock。
 */
export interface Tier {
  key: "normal" | "silver" | "gold" | "diamond";
  name: string;
  /** 直属用户近 30 日合约交易量门槛（USDT），normal 需审核 */
  minVolume30d: number;
  futuresRate: number;
  spotRate: number;
}
export const TIERS: Tier[] = [
  { key: "normal", name: "普通合伙人", minVolume30d: 0, futuresRate: 0.3, spotRate: 0.2 },
  { key: "silver", name: "白银合伙人", minVolume30d: 5_000_000, futuresRate: 0.4, spotRate: 0.25 },
  { key: "gold", name: "黄金合伙人", minVolume30d: 20_000_000, futuresRate: 0.45, spotRate: 0.3 },
  { key: "diamond", name: "钻石合伙人", minVolume30d: 50_000_000, futuresRate: 0.5, spotRate: 0.35 },
];
export const OVERRIDE = { rate: 0.1, maxLevels: 2, fundedBy: "平台" };
export const RULE_NOTES = [
  "普通合伙人需提交申请并经平台审核；之后按直属用户近 30 日合约交易量自动升降档。",
  "返佣基数为直属用户实际支付的交易手续费（体验金、抵扣金产生的手续费不计返佣）。",
  "二级奖励：上级合伙人额外获得其下级合伙人佣金的 10%，由平台出资，不从下级佣金中扣除；最多 2 级。",
  "佣金每日结算，发放至合伙人资金账户；被邀请用户终身有效。",
  "合伙人返佣与普通邀请返佣（10%、1 年）不叠加；跟单分润不计入返佣。",
];
export const pct = (n: number) => `${Math.round(n * 100)}%`;
export const fmtU = (n: number) => n.toLocaleString("en-US");

/* ---- mock 数据（全部虚构） ---- */
export const ME = { name: "lb_Demo8K2", uid: "80000001", email: "demo***@example.com", invite: "LB8DEMO", tier: "silver" as Tier["key"], volume30d: 7_820_000 };
export const STATS = { today: 128.42, month: 3_915.6, total: 41_208.77, directs: 186, subPartners: 4 };
export const DAILY = [
  { date: "2026-11-01", futures: 96.3, spot: 18.2, override: 13.92, total: 128.42 },
  { date: "2026-10-31", futures: 110.9, spot: 22.4, override: 9.1, total: 142.4 },
  { date: "2026-10-30", futures: 88.0, spot: 15.6, override: 11.3, total: 114.9 },
];
export const TEAM = [
  { uid: "80000123", type: "直属用户", joined: "2026-10-12", vol30d: 1_200_000, fee30d: 1200 },
  { uid: "80000456", type: "直属用户", joined: "2026-09-03", vol30d: 3_400_000, fee30d: 3400 },
  { uid: "80000789", type: "下级合伙人", joined: "2026-08-21", vol30d: 9_100_000, fee30d: 9100 },
];
