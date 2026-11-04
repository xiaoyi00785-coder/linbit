# Linbit 主网上线检查清单

> 唯一开关：`NETWORK=mainnet`（或 `APP_ENV=mainnet`）。模板见 `deploy/mainnet/.env.mainnet.example`，
> 启动：`docker compose -f docker-compose.yml -f deploy/mainnet/docker-compose.mainnet.yml --env-file deploy/mainnet/.env.mainnet up -d`。
> 任一必填变量缺失或开启了虚拟/测试开关，服务会打印原因并拒绝启动。

## 0. 切换方式（已定）：复用当前服务器与域名，测试网下线
- [ ] 停服后先完整备份数据库（pg_dump）+ KYC 卷 + 引擎数据卷，校验可恢复。
- [ ] 清理测试网数据：虚拟做市注资（mm:main 余额/流水）、测试余额与充提记录、测试网赠金/积分、测试网订单/成交/持仓、测试链充值地址。保留数据须逐表确认。
- [ ] 逐表清单与脚本：`docs/deploy/mainnet-wipe-plan.md`、`deploy/mainnet/wipe-testnet.sh`（默认 dry-run，需 BACKUP_FILE + CONFIRM_WIPE + 手动输入库名，永不自动执行）。
- [ ] 清理后余额与负债核对为 0（或仅剩真实数据），再设置 `NETWORK=mainnet` 启动。

## 1. 域名与 TLS
- [ ] `www.cowa.click` DNS 指向 Cloudflare/LB，证书有效（自动续期）。
- [ ] `GROWTH_INVITE_BASE_URL=https://www.cowa.click`，`WEBAUTHN_RP_ID/ORIGINS`、`CORS_ALLOWED_ORIGINS` 一致。
- [ ] `TRUSTED_PROXY_CIDRS` 仅包含 CF/LB 出口网段（否则 `CF-IPCountry` 可伪造，地域限制失效）。

## 2. 密钥轮换（不得沿用测试网）
- [ ] `JWT_SECRET`、`INTERNAL_TOKEN` 重新生成（`openssl rand -hex 32`）。
- [ ] 数据库、Redis 新密码；DB 仅内网可达。
- [ ] `DATABASE_URL` TLS：公网/域名主机必须 `sslmode=require`（或 `verify-full`），否则启动预检拒绝；
      仅当 DB 主机为内网/容器地址（compose 服务名、localhost、10/8、172.16/12、192.168/16、`*.internal`、`*.local`）时允许 `sslmode=disable`。
- [ ] 所有密钥经 Secret Manager 注入，仓库内只有占位符。

## 3. 冷热钱包（由 Yaeko 操作）
- [ ] Yaeko 线下生成助记词/私钥，离线备份；**仓库、日志、聊天中不得出现任何密钥**。
- [ ] 热钱包签名机（wallet-signer）口令文件以 secret 挂载；`WALLET_HOT_SECRET` 非 mock 值。
- [ ] 冷钱包只配置地址：`WALLET_COLD_ADDRESS_TRC20/ERC20/BEP20/BTC/SOL`（开启 ARBITRUM 时加 `_ARBITRUM`）。
      主网缺任一已开启链的冷地址 → 拒绝启动；主网不再从 HD 种子派生冷地址（`WALLET_COLD_FROM_HD` 在主网无效）。
- [ ] 小额转账验证每条链（充值→热钱包→超额归集到冷地址）。

## 4. 链节点 / RPC / 代币合约
- [ ] 主网绝不使用测试网默认 RPC / chainId / 测试代币（代码在 `NETWORK=mainnet` 下强制关闭）。
- [ ] `WALLET_NETWORKS_JSON` 只列出要开启的链；列出的每条链必须有 `WALLET_RPC_<NET>`（BTC、ERC20、TRC20、BEP20、SOL），否则拒绝启动。JSON 无效同样拒绝启动。
- [ ] ARBITRUM：仅在 `WALLET_NETWORKS_JSON` 列出时才需要 `WALLET_RPC_ARBITRUM` / 冷地址 / 合约。
- [ ] 建议每条链准备第二家 RPC 供应商作为人工切换备用（推荐，非必需）。
- [ ] RPC Key 等密钥仅写入服务器运维 .env，绝不通过聊天传递。
- [ ] 官方主网合约（`WALLET_TOKEN_<资产>_<网络>=合约:精度`，已写入模板，上线前在浏览器再核对一次）：

| 资产/网络 | 合约 | 精度 | 来源 |
|---|---|---|---|
| USDT TRC20 | `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t` | 6 | Tether 官方 https://tether.to/supported-protocols/ |
| USDT ERC20 | `0xdAC17F958D2ee523a2206206994597C13D831ec7` | 6 | Tether 官方（同上） |
| USDT SOL (SPL) | `Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB` | 6 | Tether 官方（同上） |
| USDT BEP20 | `0x55d398326f99059fF775485246999027B3197955` | **18** | **非 Tether 官方**：Tether 官网不列 BSC 地址，此为 Binance-Peg USDT（Binance 发行/托管），各大交易所通用；风险自担，核对 https://bscscan.com/token/0x55d398326f99059fF775485246999027B3197955 |
| USDC ERC20 | `0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48` | 6 | Circle 官方 https://developers.circle.com/stablecoins/usdc-contract-addresses |
| USDC SOL (SPL) | `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v` | 6 | Circle 官方（同上） |
| USDC ARBITRUM（可选） | `0xaf88d065e77c8cC2239327C5EDb3A432268e5831` | 6 | Circle 官方（同上） |

- [ ] **不要开启** USDC/TRC20（Circle 已停止 TRON USDC，官方列表无 TRON）与 USDC/BEP20（无 Circle 官方合约；Binance-Peg USDC 精度 18，如需须单独决定）。
- [ ] `tradfi_tokens`（清空时保留）：逐条核对主网合约地址与精度，测试网地址必须改为主网或停用。

## 闪兑对冲
- [ ] 清空后新建真实对冲账户：`psql "$DATABASE_URL" -c "INSERT INTO asset_accounts(user_id) VALUES ('system:hedge-main') ON CONFLICT DO NOTHING"`，
      在 .env 设 `CONVERT_HEDGE_USER_ID=system:hedge-main`，重启 asset（启动时自动补齐余额行）；按需从冷/热钱包为其充值对冲资金。
- [ ] 主网若未设置或账户不存在：对冲停用、闪兑照常、Telegram 告警（`/convert` 风控接口返回 `hedgeBlocked` 原因），不会自动建账户。
- [ ] 后台对冲开关在主网默认关闭，确认资金到位后再开启。

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

## 9. 地域限制（已定：全站屏蔽）
- [ ] 受限列表保持默认：US + 美属领地 + OFAC 制裁国（US,PR,GU,VI,AS,MP,UM,CU,IR,KP,SY），后台 TradFi 设置可改，网关 30s 内生效。
- [ ] 网关全站屏蔽：`GEO_BLOCK_ENABLED=true`（主网默认），国家取自受信代理（`TRUSTED_PROXY_CIDRS`）的 `CF-IPCountry`，命中返回 451 `REGION_BLOCKED`（中英文提示）；`/healthz` 不受影响。前端收到 451 跳转 `/blocked`。
- [ ] 代币化股票/金属下单仍额外校验（国家未知时拒绝 + KYC 国籍）。
- [ ] Cloudflare WAF 规则（第一道防线）：`(ip.geoip.country in {"US" "PR" "GU" "VI" "AS" "MP" "UM" "CU" "IR" "KP" "SY"}) and not http.request.uri.path eq "/healthz"` → Block（自定义响应 451）。
- [ ] 源站只允许 Cloudflare IP 访问（防绕过）。

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
