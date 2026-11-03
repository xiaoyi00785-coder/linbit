import { API_BASE } from "./api";
/** 功能开关：未上线的功能入口统一跳转 /coming-soon/[feature]。环境变量 NEXT_PUBLIC_FEATURE_<KEY>=1 开启。 */
const on = (v?: string) => v === "1" || v === "true";
export const FEATURES = {
  tokenizedStocks: on(process.env.NEXT_PUBLIC_FEATURE_STOCKS),
  metals: on(process.env.NEXT_PUBLIC_FEATURE_METALS),
  tradfi: on(process.env.NEXT_PUBLIC_FEATURE_TRADFI),
  eventContracts: on(process.env.NEXT_PUBLIC_FEATURE_EVENTS),
  chatRoom: on(process.env.NEXT_PUBLIC_FEATURE_CHAT),
  subAccounts: on(process.env.NEXT_PUBLIC_FEATURE_SUBACCOUNTS),
};
export type FeatureKey = keyof typeof FEATURES;
/** 服务端开关 GET /app/features（公开，缓存 30s）。请求失败 ⇒ 全部视为关闭；false ⇒ /coming-soon；true ⇒ 正常渲染。
 *  后端 key 与前端 key 的映射（chat ⇒ chatRoom）。环境变量仅作本地覆盖。 */
const API_KEY: Record<string, FeatureKey> = { tokenizedStocks: "tokenizedStocks", metals: "metals", tradfi: "tradfi", eventContracts: "eventContracts", chat: "chatRoom", chatRoom: "chatRoom", subAccounts: "subAccounts" };
export const RUNTIME: Record<string, boolean> = {};
const CK = "lb_features";
try { if (typeof localStorage !== "undefined") Object.assign(RUNTIME, JSON.parse(localStorage.getItem(CK) || "{}").features || {}); } catch {}
export const isOn = (k: FeatureKey | string) => FEATURES[k as FeatureKey] || RUNTIME[k] === true;
let loading: Promise<void> | null = null;
export function loadFeatures(base = API_BASE): Promise<void> {
  if (loading) return loading;
  loading = fetch(base + "/app/features", { credentials: "include" }).then((r) => (r.ok ? r.json() : Promise.reject(r.status))).then((j) => {
    const f = (j && j.features) || {}; const next: Record<string, boolean> = {};
    for (const [k, v] of Object.entries(f)) { next[API_KEY[k] || k] = v === true; }
    const changed = JSON.stringify(next) !== JSON.stringify(RUNTIME);
    for (const k of Object.keys(RUNTIME)) delete RUNTIME[k]; Object.assign(RUNTIME, next);
    try { localStorage.setItem(CK, JSON.stringify({ features: next, updatedAt: j.updatedAt || null })); } catch {}
    if (changed) window.dispatchEvent(new Event("lb-features"));
  }).catch(() => { const had = Object.values(RUNTIME).some(Boolean); for (const k of Object.keys(RUNTIME)) delete RUNTIME[k]; if (had) window.dispatchEvent(new Event("lb-features")); });
  return loading;
}
export const SOON: Record<FeatureKey, { slug: string; name: string; intro: string; icon: string }> = {
  tokenizedStocks: { slug: "stocks", name: "股票合约 · 代币化股票", intro: "用 USDT 交易美股等真实资产的永续合约与代币化股票现货，即将上线。", icon: "▤" },
  metals: { slug: "metals", name: "贵金属", intro: "黄金、白银等贵金属合约与代币化资产，即将上线。", icon: "◆" },
  tradfi: { slug: "tradfi", name: "传统金融", intro: "用 USDT 连接全球股票、指数与大宗商品，即将上线。", icon: "◉" },
  eventContracts: { slug: "events", name: "事件合约", intro: "预测市场方向，以固定收益结构轻松交易，即将上线。", icon: "◎" },
  chatRoom: { slug: "chat", name: "聊天室", intro: "与全球交易者实时交流行情观点，即将上线。", icon: "✉" },
  subAccounts: { slug: "sub-accounts", name: "子账户", intro: "为不同策略或团队成员创建独立子账户，统一管理资产，即将上线。", icon: "◫" },
};
const LABELS: [RegExp, FeatureKey][] = [[/股票|美股|代币化股票/, "tokenizedStocks"], [/贵金属|黄金|白银/, "metals"], [/传统金融|TradFi/i, "tradfi"], [/事件合约/, "eventContracts"], [/聊天室/, "chatRoom"], [/子账户/, "subAccounts"]];
/** 入口文案 → 未上线功能 key（已开启的功能返回 null） */
export function soonOf(label: string): FeatureKey | null {
  const k = LABELS.find(([r]) => r.test(label))?.[1];
  return k && !isOn(k) ? k : null;
}
export const soonPath = (label: string) => { const k = soonOf(label); return k ? "/coming-soon/" + SOON[k].slug : null; };
export const bySlug = (slug: string) => (Object.keys(SOON) as FeatureKey[]).find((k) => SOON[k].slug === slug);
