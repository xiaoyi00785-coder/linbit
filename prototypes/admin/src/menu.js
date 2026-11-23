// 菜单树：key 即路由（#/key）；perm 为打开该页所需的后端权限码（services/go/admin/rbac.go），
// 数组表示满足其一即可。侧栏只显示当前管理员有权限的菜单，路由同样按此拦截。
export const MENU = [
  { g: '总览', items: [['dashboard', '数据看板', '◧', 'dash:view']] },
  { g: '用户与合规', items: [['users', '用户列表', '◉', 'user:view'], ['kyc', 'KYC 审核', '◎', 'kyc:view'], ['risk-users', '风险名单', '⚑', 'user:view'], ['risk-rules', '风控规则', '⛨', 'risk:view'], ['apikeys', 'API Key 管理', '⚿', 'user:view'],
    ['cs', '客服工作台', '☏', 'support:view'], ['support', '客服工单', '✆', 'support:view'], ['chat', '聊天审核', '✉', 'content:edit']] },
  { g: '资金', items: [['withdraw', '提现审核', '⇡', 'withdraw:view'], ['deposit', '充值记录', '⇣', 'ledger:view'], ['ledger', '资产流水', '≡', 'ledger:view'], ['adjust', '人工调账', '±', 'fund:adjust'],
    ['wallets', '钱包管理', '▣', 'withdraw:view'], ['solvency', '偿付能力', '⚖', 'mm:view'], ['por', '储备金证明', '◫', 'content:edit']] },
  { g: '交易', items: [['spot-orders', '现货订单', '◇', 'ledger:view'], ['positions', '合约持仓', '◆', 'ledger:view'], ['grid', '网格策略', '▦', 'ledger:view'], ['liquidation', '强平与风险', '⚠', 'ledger:view'], ['marketmaker', '做市管理', '◈', 'mm:view']] },
  { g: '产品配置', items: [['coins', '币种与网络', '◈', 'withdraw:view'], ['pairs', '现货交易对', '⇄', 'symbol:view'], ['futures', '合约参数', '⚙', 'symbol:view'], ['fees', '手续费配置', '%', 'fee:view']] },
  { g: '运营增长', items: [['campaigns', '活动管理', '★', 'reward:view'], ['rewards', '奖励发放', '◐', 'reward:view'], ['points', '积分商城', '✦', 'reward:view'], ['rebate', '返佣配置', '%', 'partner:view'], ['partners', '合伙人管理', '♛', 'partner:view'], ['coupons', '卡券管理', '▤', 'reward:view'], ['copy', '跟单交易', '⧉', 'copy:view'], ['content', '内容与公告', '✎', 'content:edit'], ['notify', '消息推送', '✉', 'content:edit']] },
  { g: '系统', items: [['admins', '管理员与角色', '⚿', 'admin:manage'], ['audit', '审计日志', '☰', 'audit:view'], ['settings', '系统设置', '⚒', ['user:edit', 'symbol:edit', 'content:edit']]] },
]
export const FLAT = MENU.flatMap(m => m.items.map(i => ({ key: i[0], name: i[1], ic: i[2], perm: i[3], badge: i[4], group: m.g })))
/** allowed(perms, need): need 为字符串或数组（满足其一）；空 = 登录即可 */
export const allowed = (perms, need) => !need || (Array.isArray(need) ? need : [need]).some(p => (perms || []).includes(p))
export const menuFor = perms => MENU.map(m => ({ ...m, items: m.items.filter(i => allowed(perms, i[3])) })).filter(m => m.items.length)
