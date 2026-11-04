/** 页面标题与深色作用域：按路由前缀匹配（越长越优先）。 */
const T: [string, string][] = [
  ["/markets", "行情"], ["/spot", "现货交易"], ["/futures", "合约交易"], ["/trade", "交易"],
  ["/convert", "闪兑"], ["/buy", "买币"], ["/deposit", "充值"], ["/withdraw", "提现"],
  ["/orders/futures", "合约订单"], ["/orders/spot", "现货订单"], ["/orders/buy", "买币订单"], ["/orders/earn", "理财订单"],
  ["/orders/convert", "闪兑记录"], ["/orders/tradfi", "传统金融订单"], ["/orders/loan", "借贷订单"], ["/orders", "订单"],
  ["/assets/overview", "资产总览"], ["/assets/funding-account", "资金账户"], ["/assets/trading-account", "合约账户"],
  ["/assets/spot-account", "现货账户"], ["/assets/earn-account", "理财账户"], ["/assets/tradfi-account", "传统金融账户"],
  ["/assets/bots-account", "策略账户"], ["/assets", "资产"],
  ["/setting/api", "API 管理"], ["/setting/kyc", "身份认证"], ["/setting/address", "提现地址管理"], ["/setting/devices", "设备管理"], ["/setting/dashboard", "个人中心"], ["/setting/security", "安全设置"], ["/setting", "账户设置"],
  ["/earn/vip", "VIP 专享"], ["/earn/loan", "质押借币"], ["/earn", "理财"],
  ["/notifications", "消息通知"], ["/announcements", "公告中心"], ["/login", "登录"], ["/register", "注册"],
  ["/forgot-password", "找回密码"], ["/invite", "邀请返佣"], ["/rewards", "奖励中心"], ["/coupons", "卡券中心"],
  ["/points", "积分中心"], ["/events", "活动中心"], ["/copy", "跟单交易"], ["/strategy", "策略交易"],
  ["/sub-accounts", "子账户"], ["/tradfi", "传统金融"], ["/sim", "模拟交易"], ["/api/docs", "API 文档"],
  ["/account", "账户"], ["/chat", "聊天室"], ["/coming-soon", "即将上线"],
];
const hit = (p: string, pre: string) => p === pre || p.startsWith(pre + "/");
export function pageTitle(p: string): string {
  if (p === "/") return "linbit - 数字资产交易平台";
  const pg = /^\/p\/([^/]+)/.exec(p)?.[1];
  if (pg) { let t = pg; try { t = decodeURIComponent(pg); } catch {} return `${t} | linbit`; }
  const m = [...T].sort((a, b) => b[0].length - a[0].length).find(([k]) => hit(p, k));
  const sym = /^\/(spot|futures)\/([A-Z0-9_-]+)/i.exec(p)?.[2];
  return m ? `${sym ? sym.toUpperCase() + " " : ""}${m[1]} | linbit` : "linbit - 数字资产交易平台";
}
