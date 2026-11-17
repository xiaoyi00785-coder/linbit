# 外部指数价格 / 做市服务 / 偿付对账

## 外部指数价格（market-data）
- 来源：Binance 公共行情（主），OKX、Bybit、Gate.io 依次备用；`INDEX_USD_FALLBACK_SYMBOLS`（默认 TONUSDT）再接 Coinbase / Kraken 的 BASE/USD，按同所实时 USDT/USD 换算为 USDT（主备切换，生效来源权重为 1）。`INDEX_FEED=binance,okx,bybit,gateio,coinbase,kraken`，顺序即优先级。
- 只采用交易所标记为“交易中”的交易对：Binance `exchangeInfo` status=TRADING、OKX instruments state=live、Bybit instruments-info status=Trading、Gate currency_pairs trade_status=tradable、Coinbase product online、Kraken AssetPairs status=online；状态列表每 `INDEX_STATUS_REFRESH_SECONDS=900` 秒刷新（首次加载失败则该来源不报价）。暂停/下架的交易对（例如 Binance TONUSDT 为 BREAK）不再从该所产生指数。
- USD 源：`INDEX_USDT_USD_MIN/MAX`（默认 0.97..1.03）约束同所 USDT/USD；汇率缺失、不可解析或越界则整所本轮失败（fail-closed）。不变价守卫看原始 USD 价，避免汇率抖动掩盖冻结的 BASE/USD。
- 价格连续 `INDEX_UNCHANGED_STALE_SECONDS=1800` 秒完全不变视为不新鲜（0=关闭）；锚定币对在 `INDEX_UNCHANGED_EXEMPT` 中豁免。
- 所有来源都不新鲜时 `/market/index` 返回 503 `INDEX_UNAVAILABLE`（body 含 `fresh:false` 与各来源 components）。
- 实现：**REST 轮询**（`INDEX_POLL_MS=2000`），不是 WebSocket 订阅。
- 超过 `INDEX_STALE_SECONDS=10` 秒没有新价格即视为不可用：合约标记价、KYC/后台折算继续 fail-closed；做市暂停。
- `/market/index?symbol=BTCUSDT` 供 futures / wallet / admin / marketmaker 使用。生产环境禁止 `INDEX_FEED=off`。

## 做市服务 marketmaker（端口 8093）
- 报价模式是 **跟盘**：每个现货、合约交易对复制 Binance 深度（OKX 备用），挂到本所撮合引擎。现货 PostOnly，合约 GTC（全仓）。用户成交仍是与做市账户过户，不会打到对方交易所。
- 现货跟 Binance spot `/api/v3/depth`；合约跟 Binance USDT-M `/fapi/v1/depth`；TradFi（股票/金属/原油）跟对应参考合约（如 PAXG→XAUUSDT，WTI→CLUSDT）。`MM_BOOK_FEED=binance,okx,bybit`（OKX SWAP 没有则退 spot，Binance 空盘口继续试下一家），快照缓存 `MM_BOOK_TTL_MS`（默认 1000）。
- 档位数 `levels` 决定复制前几档；数量按外部档位同比缩放，每档名义金额夹在 100–1,000,000 USDT（合约再按杠杆压到仓位限额内）。`MM_BOOK_SCALE`（默认 1）再乘一层。外部盘口为空（如下架的 TON）时退回指数梯子，状态 `bookSource=index-ladder`。
- 库存平衡：达到 `maxPosition` 停止加仓一侧；`maxSkewBps` 把整本复制盘口平移。TradFi 参考偏离时用 `spreadBps × (widen−1)` 把买卖再推开。指数仍用于标记价/库存估值；指数或盘口不可用都撤单暂停。
- **没有止损**（Yaeko 决定）。
- 默认参数（每对可在后台修改，需 `mm:edit` + 2FA，写审计 module=mm）：
  价差 10 bps（仅 TradFi 加宽仍用）、5 档、档间距 5 bps、每档 1000 USDT（跟盘后不再用来铺梯子）、逐档 ×1.2、刷新 3000 ms、偏离 2 bps 才重挂、
  现货目标基础币占比 0.5、最大持仓 50,000 USDT、最大偏移 20 bps、合约杠杆 5x。
- 默认 **不运行**；后台 `/admin/v1/mm/start|stop`（停止时同步撤销全部挂单）。
- 指数或外部盘口不可用：撤单、暂停，并发 Telegram「做市暂停」；恢复后发「做市恢复」。

## 资金规则
- 后台可给做市账户直接入账（`mm_testnet_seed` / `POST /admin/v1/mm/credit`，2FA + 审计，ref 幂等，上限 `MM_SEED_MAX`，默认 10,000,000）。主网可用。
- 入账记在 `system:mm_testnet_virtual`，对账（solvency）把该金额从应付中剔除并单列 `virtualSeed`。每次入账以 `!!!` 打印日志。

## 每日偿付对账（admin）
- `SOLVENCY_HOUR_UTC=1`（每天 01:00 UTC；-1 关闭）。也可 `/admin/v1/solvency/run` 手动触发。
- 每个币种：链上持有 ≥ 全部用户余额 + 做市余额（现货 + 合约保证金）− 测试网虚拟入账，否则 Telegram 告警「偿付对账失败」。
  任一数据源出错也视为失败。结果写入 `solvency_reports`。
- Telegram：`TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` 从环境变量读取，token 不写日志。

## 测试网基础币虚拟注资（2026-11-03）

`POST /admin/v1/mm/credit` 与 `POST /admin/v1/mm/testnet-seed`（mm:edit + 2FA + 审计；主网可用）。

- `{"ref":"r1"}`：注入 `MM_SEED_AMOUNT` USDT（原行为）。
- `{"ref":"base-1103","assets":["BTC","ETH","SOL"],"usdtValue":"1000000"}`：每个币按当时
  `/market/index?symbol=<A>USDT` 折算数量（截断 8 位），usdtValue 默认 `MM_SEED_BASE_USDT=1000000`；
  也可用 `"amounts":{"BTC":"10"}` 指定固定数量。允许的币：`MM_SEED_BASE_ASSETS`（默认 BTC,ETH,SOL,BNB,TRX）。
- 幂等键按币：`mm_testnet_seed:<ref>:<ASSET>`，重放返回 `already_applied`。
- 返回 `{txType, usdtValue, items:[{asset, amount, indexPrice, status: ok|already_applied|failed, error}]}`。
- asset 的 `MM_SEED_MAX`（默认 10,000,000）按**数量**限制单笔，TRX 如需注资可能需调高。
- 偿付能力：`/internal/solvency/liabilities` 每币 `virtualSeed` = 该币全部 mm_testnet_seed 之和，已从 required 中扣除。

合约账户资金：`POST /admin/v1/mm/futures-transfer {"direction":"in","amount":"3000000","reason":"…","totpCode":"…"}`
把做市账户现货 USDT 转入合约账户（out 反向）。

## 现货 SOLUSDT / BNBUSDT / TRXUSDT

order 迁移 00003 新增三个交易对（TRX 价格 5 位小数、数量整数），marketmaker 迁移 00002 预置各自参数，
撮合引擎 `SYMBOLS` 追加 `SOLUSDT:3,BNBUSDT:3,TRXUSDT:1`（只能追加，顺序决定成交 id 命名空间）。
指数价自动来自 Binance `SOLUSDT` 等 / OKX `SOL-USDT` 等全量 ticker，无需映射配置。

## Ticker / kline fallback when there are no trades
- Ticker `lastPrice` = last trade → mid of best bid/ask → index price (futures: → mark) → `"0"`. `priceSource` tells which (`trade|mid|index|mark|none`). Without trades, 24h change is 0, high/low equal lastPrice, volume 0. No trades are fabricated.
- Klines and the public trade tape are copied from Binance (spot, then USDⓈ-M; OKX backup), including volume. They are not marked `source:"index"`, so the chart draws them as normal candles. Local fills still update the ticker. If every external venue fails, the old index fallback (gray, volume 0) is used.
- `/futures/market/contracts` items carry `tags` (ids from `market_categories`, same as spot).
