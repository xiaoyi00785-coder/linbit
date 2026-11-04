# 主网切换：测试网数据清理计划

> 复用当前服务器与数据库。**先备份，后清理**；脚本 `deploy/mainnet/wipe-testnet.sh` 只能人工执行，默认只打印计划（dry-run），不会被任何服务或 CI 自动调用。

执行顺序：
1. 停止所有服务（保留 postgres）。
2. `pg_dump -Fc` 全量备份 + 备份 `kycdocs`、`enginedata` 卷；校验备份可恢复。
3. `BACKUP_FILE=<备份文件> deploy/mainnet/wipe-testnet.sh`（dry-run）检查计划。
4. 确认后：`BACKUP_FILE=<备份文件> CONFIRM_WIPE=WIPE-TESTNET-<数据库名> deploy/mainnet/wipe-testnet.sh --execute`。
5. 清空撮合引擎数据卷 `enginedata`（引擎内仍有测试网挂单）、`FLUSHALL` Redis、删除 `kycdocs` 卷内测试网证件文件。
6. 核对：所有余额表为空，`mm_state.running=false`，再以 `NETWORK=mainnet` 启动。

说明：迁移表 `goose_db_version*` 保留。默认清空全部测试网用户；如需保留测试网注册用户，须将 account 段改为 keep 并仍清空所有余额/流水（需 Yaeko 决定）。

| 服务 | db.table | 内容 | 处理 |
|---|---|---|---|
| account | linbit.users | 用户账户（测试网注册用户） | 清空 wipe |
| account | linbit.account_settings | 用户偏好 | 清空 wipe |
| account | linbit.devices | 登录设备 | 清空 wipe |
| account | linbit.login_challenges | 登录挑战 | 清空 wipe |
| account | linbit.notification_prefs | 通知偏好 | 清空 wipe |
| account | linbit.notifications | 站内通知 | 清空 wipe |
| account | linbit.passkeys | 通行密钥 | 清空 wipe |
| account | linbit.password_reset_tokens | 重置密码令牌 | 清空 wipe |
| account | linbit.refresh_tokens | 刷新令牌 | 清空 wipe |
| account | linbit.sub_account_settings | 子账户设置 | 清空 wipe |
| account | linbit.sub_accounts | 子账户 | 清空 wipe |
| account | linbit.sub_api_keys | 子账户 API Key | 清空 wipe |
| account | linbit.sub_transfers | 子账户划转 | 清空 wipe |
| account | linbit.user_favorites | 自选 | 清空 wipe |
| account | linbit.verification_codes | 验证码 | 清空 wipe |
| account | linbit.vip_applications | VIP 申请 | 清空 wipe |
| account | linbit.webauthn_sessions | WebAuthn 会话 | 清空 wipe |
| account | linbit.withdraw_addresses | 提现地址簿（测试链地址） | 清空 wipe |
| admin | linbit.admin_users | 后台管理员 | 保留 keep |
| admin | linbit.admin_sessions | 后台会话 | 清空 wipe |
| admin | linbit.announcements | 公告 | 保留 keep |
| admin | linbit.audit_logs | 审计日志（合规留存） | 保留 keep |
| admin | linbit.change_approvals | 变更审批记录 | 保留 keep |
| admin | linbit.change_requests | 变更申请 | 保留 keep |
| admin | linbit.coupon_templates | 卡券模板（配置） | 保留 keep |
| admin | linbit.reward_grants | 已发放奖励（测试网赠金） | 清空 wipe |
| admin | linbit.solvency_reports | 偿付能力报告（测试网） | 清空 wipe |
| admin | linbit.withdrawal_reviews | 提现审核记录 | 清空 wipe |
| asset | linbit.asset_settings | 资产服务配置 | 保留 keep |
| asset | linbit.convert_assets | 闪兑币种配置 | 保留 keep |
| asset | linbit.convert_config | 闪兑配置 | 保留 keep |
| asset | linbit.convert_pair_state | 闪兑币对运行状态 | 清空 wipe |
| asset | linbit.asset_accounts | 用户资产账户 | 清空 wipe |
| asset | linbit.balances | 余额（含做市虚拟注资、测试余额） | 清空 wipe |
| asset | linbit.funding_balances | 资金账户余额 | 清空 wipe |
| asset | linbit.freeze_accounts | 冻结 | 清空 wipe |
| asset | linbit.ledger_entries | 账本分录 | 清空 wipe |
| asset | linbit.ledger_transactions | 账本交易 | 清空 wipe |
| asset | linbit.settlements | 结算 | 清空 wipe |
| asset | linbit.wallet_ops | 充提入账操作 | 清空 wipe |
| asset | linbit.convert_events | 闪兑事件 | 清空 wipe |
| asset | linbit.convert_hedges | 闪兑对冲 | 清空 wipe |
| asset | linbit.convert_orders | 闪兑订单 | 清空 wipe |
| asset | linbit.convert_quotes | 闪兑报价 | 清空 wipe |
| earn | linbit.e_products | 理财产品（配置） | 保留 keep |
| earn | linbit.e_loan_terms | 借贷期限（配置） | 保留 keep |
| earn | linbit.e_earn_events | 理财事件 | 清空 wipe |
| earn | linbit.e_fixed_orders | 定期订单 | 清空 wipe |
| earn | linbit.e_holdings | 理财持仓 | 清空 wipe |
| earn | linbit.e_loan_collaterals | 借贷质押 | 清空 wipe |
| earn | linbit.e_loan_events | 借贷事件 | 清空 wipe |
| earn | linbit.e_loans | 借贷订单 | 清空 wipe |
| earn | linbit.e_unlocks | 解锁记录 | 清空 wipe |
| futures | linbit.ev_config | 事件合约配置 | 保留 keep |
| futures | linbit.ev_symbols | 事件合约标的 | 保留 keep |
| futures | linbit.fut_contracts | 合约定义 | 保留 keep |
| futures | linbit.fut_order_config | 下单配置 | 保留 keep |
| futures | linbit.fut_risk_tiers | 风险档位 | 保留 keep |
| futures | linbit.fut_settings | 合约全局设置 | 保留 keep |
| futures | linbit.ev_index_samples | 事件指数采样 | 清空 wipe |
| futures | linbit.ev_orders | 事件合约订单 | 清空 wipe |
| futures | linbit.fut_accounts | 合约账户 | 清空 wipe |
| futures | linbit.fut_adl | ADL 记录 | 清空 wipe |
| futures | linbit.fut_algo_orders | 计划委托 | 清空 wipe |
| futures | linbit.fut_bills | 合约账单 | 清空 wipe |
| futures | linbit.fut_bonus | 体验金（测试网赠金） | 清空 wipe |
| futures | linbit.fut_copy_events | 跟单事件 | 清空 wipe |
| futures | linbit.fut_copy_follows | 跟单关系 | 清空 wipe |
| futures | linbit.fut_copy_settlements | 跟单分润 | 清空 wipe |
| futures | linbit.fut_copy_traders | 带单员 | 清空 wipe |
| futures | linbit.fut_copy_trades | 跟单成交 | 清空 wipe |
| futures | linbit.fut_fee_overrides | 用户费率覆盖 | 清空 wipe |
| futures | linbit.fut_funding_history | 资金费历史 | 清空 wipe |
| futures | linbit.fut_insurance_snapshots | 保险基金快照 | 清空 wipe |
| futures | linbit.fut_liquidations | 强平记录 | 清空 wipe |
| futures | linbit.fut_mark_history | 标记价格历史 | 清空 wipe |
| futures | linbit.fut_orders | 合约订单 | 清空 wipe |
| futures | linbit.fut_position_history | 历史仓位 | 清空 wipe |
| futures | linbit.fut_position_mode | 用户持仓模式 | 清空 wipe |
| futures | linbit.fut_positions | 持仓 | 清空 wipe |
| futures | linbit.fut_trades | 合约成交 | 清空 wipe |
| futures | linbit.fut_transfers | 合约划转 | 清空 wipe |
| growth | linbit.g_campaigns | 活动配置 | 保留 keep |
| growth | linbit.g_commissions | 返佣 | 清空 wipe |
| growth | linbit.g_coupons | 用户卡券（测试网赠金） | 清空 wipe |
| growth | linbit.g_cursors | 消费游标 | 清空 wipe |
| growth | linbit.g_enrollments | 活动报名 | 清空 wipe |
| growth | linbit.g_fee_events | 手续费事件 | 清空 wipe |
| growth | linbit.g_newcomer | 新人任务 | 清空 wipe |
| growth | linbit.g_newcomer_claims | 新人奖励领取（测试网赠金） | 清空 wipe |
| growth | linbit.g_partners | 合伙人 | 清空 wipe |
| growth | linbit.g_payouts | 返佣发放 | 清空 wipe |
| growth | linbit.g_points_daily | 积分（测试网积分） | 清空 wipe |
| growth | linbit.g_redemptions | 积分兑换 | 清空 wipe |
| growth | linbit.g_settlements | 返佣结算 | 清空 wipe |
| growth | linbit.g_users | 增长用户档案 | 清空 wipe |
| growth | linbit.g_wallet_ops | 增长入账操作 | 清空 wipe |
| kyc | linbit.kyc_applications | KYC 申请 | 清空 wipe |
| kyc | linbit.kyc_documents | KYC 证件元数据（另需清理 kycdocs 卷文件） | 清空 wipe |
| kyc | linbit.kyc_level_sync | KYC 等级同步 | 清空 wipe |
| market-data | linbit.app_features | 功能开关覆盖 | 保留 keep |
| market-data | linbit.chat_config | 聊天配置 | 保留 keep |
| market-data | linbit.home_hot_pins | 首页热门置顶 | 保留 keep |
| market-data | linbit.home_placements | 首页运营位 | 保留 keep |
| market-data | linbit.market_categories | 行情分类 | 保留 keep |
| market-data | linbit.tradfi_assets | TradFi 标的 | 保留 keep |
| market-data | linbit.tradfi_tokens | 代币化资产 | 保留 keep |
| market-data | linbit.tradfi_settings | TradFi/地域设置（保留受限国家列表） | 保留 keep |
| market-data | linbit.chat_messages | 聊天消息 | 清空 wipe |
| market-data | linbit.chat_reports | 聊天举报 | 清空 wipe |
| market-data | linbit.chat_sanctions | 聊天处罚 | 清空 wipe |
| market-data | linbit.klines | K 线（测试网成交生成） | 清空 wipe |
| market-data | linbit.por_snapshots | 储备金快照（测试网） | 清空 wipe |
| marketmaker | linbit.mm_pairs | 做市交易对配置 | 保留 keep |
| marketmaker | linbit.mm_inventory_baseline | 做市虚拟注资基线 | 清空 wipe |
| marketmaker | linbit.mm_state | 做市运行状态 → running=false | 重置 reset |
| order | linbit.fee_rates | 费率（现货 0.1% / 合约 0.05%） | 保留 keep |
| order | linbit.symbols | 交易对 | 保留 keep |
| order | linbit.orders | 现货订单 | 清空 wipe |
| order | linbit.trades | 现货成交 | 清空 wipe |
| order | linbit.spot_conditional_orders | 条件单 | 清空 wipe |
| order | linbit.spot_fee_overrides | 用户费率覆盖 | 清空 wipe |
| strategy | linbit.grid_bots | 网格策略 | 清空 wipe |
| strategy | linbit.grid_orders | 网格订单 | 清空 wipe |
| strategy | linbit.grid_snapshots | 网格快照 | 清空 wipe |
| wallet | linbit.wallet_settings | 钱包设置 | 保留 keep |
| wallet | linbit.wallet_network_settings | 网络开关/最小额/手续费（需按主网复核） | 保留 keep |
| wallet | linbit.chain_cursors | 链扫描游标（测试链高度） | 清空 wipe |
| wallet | linbit.chain_state | 链状态（测试链） | 清空 wipe |
| wallet | linbit.chain_txs | 链上交易 | 清空 wipe |
| wallet | linbit.deposit_addresses | 充值地址（测试链） | 清空 wipe |
| wallet | linbit.deposits | 充值记录 | 清空 wipe |
| wallet | linbit.internal_transfers | 站内转账 | 清空 wipe |
| wallet | linbit.sweeps | 归集 | 清空 wipe |
| wallet | linbit.withdrawals | 提现记录 | 清空 wipe |
