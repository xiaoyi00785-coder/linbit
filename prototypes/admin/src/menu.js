// 菜单树：key 即路由（#/key），perm 为所需权限码
export const MENU = [
  { g: '总览', items: [['dashboard', '数据看板', '◧', 'dash:view']] },
  { g: '用户与合规', items: [['users', '用户列表', '◉', 'user:view'], ['kyc', 'KYC 审核', '◎', 'kyc:review'], ['risk-users', '风险名单', '⚑', 'risk:view']] },
  { g: '资金', items: [['withdraw', '提现审核', '⇡', 'wd:review'], ['deposit', '充值记录', '⇣', 'fund:view'], ['ledger', '资产流水', '≡', 'fund:view'], ['wallets', '钱包管理', '▣', 'wallet:view']] },
  { g: '交易', items: [['spot-orders', '现货订单', '◇', 'trade:view'], ['positions', '合约持仓', '◆', 'trade:view'], ['grid', '网格策略', '▦', 'trade:view'], ['liquidation', '强平与风险', '⚠', 'risk:view']] },
  { g: '产品配置', items: [['coins', '币种与网络', '◈', 'coin:edit'], ['pairs', '现货交易对', '⇄', 'pair:edit'], ['futures', '合约参数', '⚙', 'fut:edit'], ['fees', '手续费配置', '%', 'fee:edit']] },
  { g: '运营增长', items: [['campaigns', '活动管理', '★', 'mkt:edit'], ['rewards', '奖励发放', '◐', 'mkt:edit'], ['points', '积分商城', '✦', 'mkt:edit'], ['rebate', '返佣配置', '%', 'mkt:edit'], ['partners', '合伙人管理', '♛', 'mkt:edit'], ['coupons', '卡券管理', '▤', 'mkt:edit'], ['copy', '跟单交易', '⧉', 'copy:edit'], ['content', '内容与公告', '✎', 'cms:edit'], ['notify', '消息推送', '✉', 'cms:edit']] },
  { g: '系统', items: [['admins', '管理员与角色', '⚿', 'sys:rbac'], ['audit', '审计日志', '☰', 'audit:view'], ['settings', '系统设置', '⚒', 'sys:edit']] },
]
export const FLAT = MENU.flatMap(m => m.items.map(i => ({ key: i[0], name: i[1], ic: i[2], perm: i[3], badge: i[4], group: m.g })))
