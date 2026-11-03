# 外部指数价格 / 做市服务 / 偿付对账

## 外部指数价格（market-data）
- 来源：Binance 公共行情（主），OKX（备）。`INDEX_FEED=binance,okx`。
- 实现：**REST 轮询**（`INDEX_POLL_MS=2000`），不是 WebSocket 订阅。
- 超过 `INDEX_STALE_SECONDS=10` 秒没有新价格即视为不可用：合约标记价、KYC/后台折算继续 fail-closed；做市暂停。
- `/market/index?symbol=BTCUSDT` 供 futures / wallet / admin / marketmaker 使用。生产环境禁止 `INDEX_FEED=off`。

## 做市服务 marketmaker（端口 8093）
- 每个现货、合约交易对按指数价双边多档报价；现货 PostOnly，合约 GTC（全仓）。
- 库存平衡：现货按目标基础币占比、合约按持仓名义值偏移中间价（最多 `maxSkewBps`），达到 `maxPosition` 停止加仓一侧。
- **没有止损**（Yaeko 决定）。
- 默认参数（每对可在后台修改，需 `mm:edit` + 2FA，写审计 module=mm）：
  价差 10 bps（0.1%）、5 档、档间距 5 bps、每档 1000 USDT、逐档 ×1.2、刷新 3000 ms、偏离 2 bps 才重挂、
  现货目标基础币占比 0.5、最大持仓 50,000 USDT、最大偏移 20 bps、合约杠杆 5x。
- 默认 **不运行**；后台 `/admin/v1/mm/start|stop`（停止时同步撤销全部挂单）。
- 指数不可用：撤单、暂停，并发 Telegram「做市暂停」；恢复后发「做市恢复」。

## 资金规则（严格）
- `WALLET_CHAIN=testnet` 时（即使 `LINBIT_ENV=production`）或 `WALLET_CHAIN=mock` 且非 production 时，后台可给做市账户虚拟入账
  10,000,000 USDT（`mm_testnet_seed`，2FA + 审计，ref 幂等，上限 `MM_SEED_MAX`）。
- `WALLET_CHAIN=mainnet`（或任何非 testnet/mock 值）：无论 LINBIT_ENV 如何，admin / marketmaker / asset 三层都直接拒绝，做市账户只能使用真实充值资产。每次入账/拒绝都会以 `!!!` 打印日志；对账（solvency）把虚拟入账从应付中剔除并单列 `virtualSeed`。

## 每日偿付对账（admin）
- `SOLVENCY_HOUR_UTC=1`（每天 01:00 UTC；-1 关闭）。也可 `/admin/v1/solvency/run` 手动触发。
- 每个币种：链上持有 ≥ 全部用户余额 + 做市余额（现货 + 合约保证金）− 测试网虚拟入账，否则 Telegram 告警「偿付对账失败」。
  任一数据源出错也视为失败。结果写入 `solvency_reports`。
- Telegram：`TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` 从环境变量读取，token 不写日志。
