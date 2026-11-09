# Batch 12 · 模拟交易（Demo Trading）对齐与契约缺口（vc9 / 0.3.6）

参考：EasiCoin `io.easiex.app` Futures → Demo Trading，**未登录态**，Ramus 云端实拍 2026-11-09（`linbit-verify/batch12/ref-demo-ramus/01–21`）。用户 t89u 明确以未登录页为参考。

## 已对齐（合约 Tab →「模拟交易」）
| 参考 | Linbit | 数据来源 |
|---|---|---|
| 顶栏 `Demo Trading` + 黑底 `Go back`（04/06） | 「模拟交易」+ 黑底「返回实盘」；进入模拟后不再显示实盘四个页签 | — |
| Go back →「Welcome to Live Trading」过渡（21） | 「欢迎来到 / 实盘交易」过渡 0.9s 后回到「合约」；进入模拟时同样显示「欢迎来到 / 模拟交易」 | — |
| 交易对头 `BTCUSDX ▾ • / Perpetual ±%`、☀ / K 线 / ⋯ | 同布局，交易对为真实永续合约 | `/futures/market/contracts`、`/ticker` |
| `Cross / Merge` + `--` `--`（未登录） | 「全仓 / 合仓」+ 绿 `--` + 红 `--`，未登录点击跳登录 | — |
| Funding Rate(8H) / Countdown | 资金费率(8H) / 倒计时 | 真实 ticker + WS |
| Open / Close、Market ▾、Cost USDX ▾、滑杆、TP/SL | 开仓 / 平仓、市价单 ▾、成本 USDT ▾、滑杆、止盈/止损（未登录点击跳登录） | — |
| Available + `⊞ Funds`、Max Buy / Max Sell | 可用 + 「⊞ 资金」、可买 / 可卖；未登录 `--`，「资金」跳登录 | 登录后 `/paper/balances` |
| 绿 / 红 `Login` + Est. Liq. Price / Cost | 绿 / 红「登录」+ 预估强平价 / 成本（未登录 `--`） | — |
| 右侧盘口、中间价、≈标记价、B/S 比例条、布局按钮、精度下拉（13/14） | 复用 OrderBookView / BookModeSheet / PrecisionSheet | 真实 `/futures/market/depth` |
| 交易对列表（07） | 复用 PairPickerSheet（搜索、最新价/涨跌幅排序、币种图标） | 真实合约行情 |
| 主题弹层（08） | 复用 ThemeModeSheet（跟随系统 / 日间 / 夜间） | 本地偏好 |
| ⋯ Popular（10） | 复用 ToolsSheet，去掉嵌套的「模拟交易」入口；计算器 / 价格预警 / 档位 / 资金费率历史 / 标记价格历史 / 风险准备金 / 添加自选 / 新手教学 | 既有真实接口 |
| Order Type（11） | 限价 / 市价 / 计划 / 只做 Maker 四行；按契约只有「市价」可选，其余置灰并标「模拟盘暂不支持」 | `PaperAccount.orderTypes` |
| Futures Unit Settings（12） | 数量-BTC / 成本-USDT / 价值-USDT 三张说明卡；「价值」置灰（需杠杆） | — |
| 底部 `BTCUSDX Chart ▴` 展开 K 线（15） | 「BTCUSDT 图表 ▴」展开 TradeChartPanel | 真实 `/futures/market/klines` |
| Positions / Orders + 时钟、Show Current / Close All（16/17） | 仓位 / 委托 + 时钟、只看当前 / 一键平仓（未登录跳登录） | — |
| 时钟、Funds 未登录跳登录（19/20） | 同；登录后时钟打开虚拟订单/成交分页，资金打开虚拟账户（余额、重置、历史） | `/paper/orders`、`/paper/trades`、`/paper/account` |

## 登录后行为（只走真实 /paper/* 契约）
- 开仓 = 虚拟**市价买入**：成本 → `quoteOrderQty`，数量 → `quantity`；平仓 = 虚拟**市价卖出**（只能按数量）。交易对取当前合约交易对（如 BTCUSDT），服务端按外部 INDEX 全成或全拒；提交前有最终复核弹层。
- 仓位 = 虚拟非 USDT 余额（`/paper/balances`）；委托恒为 0，因为契约只有市价、全成或全拒，不会挂单。
- 绝不调用 `/futures/orders` 等真实合约写接口；`paperTrading` feature 关闭时按钮显示「模拟服务未启用」且不可点。

## 缺契约，未做（不伪造）
1. **模拟合约账户**：参考是 USDX 计价的模拟永续（BTC/ETH/BNB/ARB/XRP/SOL·USDX）。后端 `/paper/*` 只有现货（`market=spot`、`quoteAsset=USDT`），README 写明「不支持合约」。因此没有 USDX 交易对、杠杆、全仓/逐仓、合仓/分仓、预估强平价、合约仓位。
2. **限价 / 计划 / 只做 Maker**：`orderTypes` enum 仅 `market`。
3. **止盈止损**、**一键平仓 / 全部撤销**：无对应 paper 接口。
4. **Copy 页签（模拟跟单）**：无 paper 跟单接口，页签未放。
5. **领取 / 补充模拟资金**（参考 Funds）：只有每 UTC 日一次 `reset`（恢复 10000 USDT），已在虚拟账户页提供；无「领取」接口。
6. **Vouchers**：参考实盘有，Demo 页没有，未放。
7. **图表详情页（09，K 线 + 指标 + Order Book/Depth/Trades/Futures 页签 + 底部 Trade）**：Linbit 合约尚无独立行情详情页；K 线图标沿用展开底部图表。属实盘与模拟共同缺口。
8. **⋯ 菜单图标**：沿用既有通用文档图标，参考为每项独立图标（实盘合约同样存在）。
9. 盘口精度档位为 tick×1/10/100/1000（参考 BTC 为 0.1/1/10/50），沿用实盘既有逻辑。

## 证据
- 单测：`DemoTradeLogicTest`（契约映射、余额、滑杆、可买）、`DemoTradingScreenTest`（Robolectric：未登录 5 个入口全部跳登录且不下单；返回实盘过渡；委托类型/单位置灰不可选；⋯ 无嵌套入口；登录后市价买入只发 `/paper/orders` 且 symbol/quoteOrderQty 正确；历史不限交易对）。
- 对比图：`linbit-verify/batch12/demo-compare/`，左参考（Ramus 实拍未登录）、右我方（Robolectric 离屏渲染 + 夹具行情），不是真机截图。
