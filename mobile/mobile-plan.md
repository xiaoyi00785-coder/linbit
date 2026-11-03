# Linbit iOS 计划

## 已完成（feat/ios-phase3，PR #12）
- U本位合约：下单 / 持仓 / 平仓 / 杠杆 / 全仓逐仓 / 止盈止损 / 逐仓保证金
- 事件合约（自行设计内页）、闪兑（/convert/*）、理财、网格策略
- 首页：/home/placements、/home/newcomer、/account/favorites、/market/fiat-rates、/app/features
- 子账户 + 切换账号、聊天室

## 待办 / 已知差距
- 新手任务无领取 / 报名接口，CTA 仅跳转
- 部分止盈止损、分仓、计划委托未做
- 聊天室、子账户、事件合约、跟单依赖后端开关

## 下一轮接入的新接口（后端已就绪，本轮未接）
| 分支 @ 提交 | 内容 | 计划用途 |
|---|---|---|
| feat/spot-asset-gaps @2039887 | `GET /assets/overview` | 首页 / 资产页总资产估值（替换本地汇总） |
| | `GET /wallet/networks` | 充值 / 提现选择网络 |
| | 现货 `postOnly` / `GTX` | 现货下单“只做 Maker”选项 |
| | `/spot/orders/conditional` | 现货条件单（止盈止损 / 计划委托）下单、列表、撤单 |
| | `GET /account/fee-rates` | 下单页手续费展示、VIP 费率 |
| | 地址簿 `PATCH` | 编辑提现地址备注 / 标签 |
| | `POST /wallet/internal-transfer` | 站内转账（UID / 邮箱） |
| feat/tradfi @3613d60 | `GET /tradfi/assets`，股票与贵金属交易对 | 合约 Tab「TradFi」页与首页 TradFi 宫格 |
| feat/security-settings @e92f9b3 | 安全设置、资金密码 | 用户中心 → 账户安全；提现 / 划转前校验资金密码 |
| | 错误码 `TWOFA_REQUIRED`、`INVALID_TOTP`、`WITHDRAW_LOCKED` | Feedback.swift 错误文案 + 弹出 2FA 输入 / 提现锁定提示 |

接入前先以各分支的 `docs/api/openapi.yaml` 为准；合入 main 后同步 `mobile/openapi.yaml`（CI 会校验一致性）并重新生成模型。
