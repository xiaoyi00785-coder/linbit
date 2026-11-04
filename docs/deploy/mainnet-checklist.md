# Linbit 主网上线检查清单

> 唯一开关：`NETWORK=mainnet`（或 `APP_ENV=mainnet`）。模板见 `deploy/mainnet/.env.mainnet.example`，
> 启动：`docker compose -f docker-compose.yml -f deploy/mainnet/docker-compose.mainnet.yml --env-file deploy/mainnet/.env.mainnet up -d`。
> 任一必填变量缺失或开启了虚拟/测试开关，服务会打印原因并拒绝启动。

## 0. 切换方式（已定）：复用当前服务器与域名，测试网下线
- [ ] 停服后先完整备份数据库（pg_dump）+ KYC 卷 + 引擎数据卷，校验可恢复。
- [ ] 清理测试网数据：虚拟做市注资（mm:main 余额/流水）、测试余额与充提记录、测试网赠金/积分、测试网订单/成交/持仓、测试链充值地址。保留数据须逐表确认。
- [ ] 清理后余额与负债核对为 0（或仅剩真实数据），再设置 `NETWORK=mainnet` 启动。

## 1. 域名与 TLS
- [ ] `www.cowa.click` DNS 指向 Cloudflare/LB，证书有效（自动续期）。
- [ ] `GROWTH_INVITE_BASE_URL=https://www.cowa.click`，`WEBAUTHN_RP_ID/ORIGINS`、`CORS_ALLOWED_ORIGINS` 一致。
- [ ] `TRUSTED_PROXY_CIDRS` 仅包含 CF/LB 出口网段（否则 `CF-IPCountry` 可伪造，地域限制失效）。

## 2. 密钥轮换（不得沿用测试网）
- [ ] `JWT_SECRET`、`INTERNAL_TOKEN` 重新生成（`openssl rand -hex 32`）。
- [ ] 数据库、Redis 新密码；DB 开 TLS；仅内网可达。
- [ ] 所有密钥经 Secret Manager 注入，仓库内只有占位符。

## 3. 冷热钱包（由 Yaeko 操作）
- [ ] Yaeko 线下生成助记词/私钥，离线备份；**仓库、日志、聊天中不得出现任何密钥**。
- [ ] 热钱包签名机（wallet-signer）口令文件以 secret 挂载；`WALLET_HOT_SECRET` 非 mock 值。
- [ ] 冷钱包只配置地址 `WALLET_COLD_ADDRESS_*`；小额转账验证每条链。

## 4. 链节点 / RPC
- [ ] 需要主网 RPC 的链：BTC、ETH/ERC20、TRON、BSC、Solana（`WALLET_RPC_BTC/ERC20/TRC20/BEP20/SOL`），至少两家备份。
- [ ] RPC Key 等密钥仅写入服务器运维 .env，绝不通过聊天传递。
- [ ] `WALLET_CHAIN=mainnet`，`WALLET_NETWORKS_JSON` 主网合约地址、确认数核对。

## 5. 充提限额与人工审核（已定）
- [ ] 充值：不设上限。
- [ ] 提现：单笔 > 1000 USDT 等值进入人工审核（主网默认，按实时价折算；无价格时转人工）。阈值可通过 `WALLET_RISK_REVIEW_MIN_AMOUNT` 调整。
- [ ] 二次复核阈值 `ADMIN_WITHDRAW_SECOND_REVIEW_USDT`（默认 5000）。
- [ ] 审核值班人员与 SLA 确定。

## 6. KYC / 邮件 / 短信 / 告警
- [ ] KYC：图片存本机磁盘（`STORAGE_BACKEND=local`，卷 `kycdocs`），不使用 S3/R2；确保持久卷、权限 700、每日加密异地备份。
- [ ] 邮件：继续使用 Resend，`RESEND_API_KEY`、发件域 SPF/DKIM/DMARC。
- [ ] 短信：不使用，`SMS_ENABLED=false`，手机号注册关闭。
- [ ] Telegram：`TELEGRAM_BOT_TOKEN/CHAT_ID`（主网必填），确认能收到测试告警。

## 7. 备份与监控
- [ ] KYC 卷 `kycdocs` 纳入每日备份。
- [ ] Postgres 每日全量 + WAL/PITR，异地存储，做一次恢复演练。
- [ ] 撮合引擎数据卷备份。
- [ ] 服务健康检查、错误率、延迟、链同步高度、热钱包余额告警。
- [ ] 每日偿付能力检查 `SOLVENCY_HOUR_UTC`（主网必须 0–23），差额即 Telegram 告警。

## 8. 首次储备金证明（PoR）
- [ ] 上线前对冷热钱包链上余额与用户负债做首次快照并留档/公示。

## 9. 地域限制
- [ ] 仅限制美国：`TRADFI_RESTRICTED_COUNTRIES=US`（首次启动写入，之后在后台修改即生效）。
- [ ] 注意：迁移默认还包含美属领地及 OFAC 制裁国（PR,GU,VI,AS,MP,UM,CU,IR,KP,SY），主网按决定仅保留 US——请法务确认制裁国是否需保留。
- [ ] 应用内地域限制作用于代币化股票/金属交易；如需全站封禁美国，在 Cloudflare WAF 配置。

## 10. 法务
- [x] 代币化股票与事件合约已通过法务审核，主网默认开启。
- [ ] 代币化现货必须有真实持有的代币背书；无虚拟库存（主网禁止 `TRADFI_VIRTUAL_INVENTORY`）。

## 11. 做市
- [ ] 主网 `MM_ENABLED=false`，虚拟注资全部禁用；启动接口返回 409 `MM_DISABLED`。
- [ ] **上线初期订单簿将为空**，需提前告知运营/用户。若日后开启，做市账户只能使用真实充值余额，受每日偿付检查约束。

## 12. 费率与前端
- [ ] 手续费沿用：现货 0.1%，合约 0.05%。
- [ ] 前端构建 `NEXT_PUBLIC_NETWORK=mainnet`；`NEXT_PUBLIC_ENABLE_MOCK`/`NEXT_PUBLIC_DEMO` 不设置（设置则构建失败）。
- [ ] 不设置 `NEXT_PUBLIC_SOCIAL_*`（无社群链接）。
- [ ] 管理后台（独立仓库 exchange-admin-ui）同样确认关闭 mock。
