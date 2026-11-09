# 策略 API：现货网格（`services/go/strategy`，:8090）

仅现货（无 DCA / 定投）。每笔单都经订单服务以该用户 `POST /spot/orders`，余额、手续费、撮合与手动单相同。Gateway 代理 `/strategy/*`。`/strategy/config` 和 `/strategy/grid/square` 公开；其余要用户 token。

## 网格模型

- `mode`：`arithmetic`（等差价格）或 `geometric`（等比）。`lowerPrice` 与 `upperPrice` 之间 L0…Ln，`gridCount` = n。价格按交易对 tick 取整。
- 每格数量 q = investment / (Σ P 以下买档 + P 以上卖档数 × P × (1 + `GRID_BUY_BUFFER`))，再按步长向下取整。每笔须满足 minQty / minNotional，否则 `GRID_TOO_DENSE` / `INVESTMENT_TOO_LOW`。
- 启动：先市价买入卖侧所需的基础币，再在 P 以上挂限价卖、P 以下挂限价买（最靠近 P 的一档留空）。
- 成交处理（轮询，`GRID_POLL_MS`）：Li 买单成交 → 在 Li+1 挂卖；Lj 卖单成交 → 在 Lj−1 挂买。每笔完成的卖单记网格利润 = 卖出所得 − 对应买单成本 − 手续费。
- 停止：`stopLossPrice`（最新价 ≤ SL）/ `takeProfitPrice`（最新价 ≥ TP）、手动停止、机器人订单被外部撤销（`order_canceled`）、或挂单失败（`place_failed`）。停止时撤销未完成订单、记部分成交，若 `sellBaseOnStop`（默认 `GRID_SELL_ON_STOP`）则市价卖掉基础币。重启后可从 `stopping` 继续。启动失败且尚未挂出任何单时，机器人丢弃。
- 统计：value、gridProfit、unrealizedPnl、totalProfit、profitRate、gridApr、cycles、runtimeSeconds、maxDrawdown、maxDrawdown7d、fees。每 `GRID_SNAPSHOT_SECONDS` 打快照，供回撤和曲线。

## 接口

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | /strategy/config | public（限额与默认值） |
| GET | /strategy/grid/square?sort=default\|roi\|runtime&page=&pageSize= | public（运行中的机器人，匿名，运行时长 ≥ `GRID_SQUARE_MIN_RUN_SECONDS`） |
| GET | /strategy/grid/preview?symbol=&mode=&lowerPrice=&upperPrice=&gridCount=&investment= | user |
| POST | /strategy/grid `{symbol, mode, lowerPrice, upperPrice, gridCount, investment, stopLossPrice?, takeProfitPrice?, sellBaseOnStop?}` | user |
| GET | /strategy/grid?status=running\|stopped\|all | user |
| GET | /strategy/grid/{id} | user（别人的 404） |
| GET | /strategy/grid/{id}/orders?status= | user |
| POST | /strategy/grid/{id}/stop `{sellBase?}` | user |
| POST | /internal/dev/grid/tick | dev（DEV_MODE + internal token） |

订单服务内部接口（X-Internal-Token）：`GET /internal/orders/{id}?userId=`、`GET /internal/symbols/{symbol}`。

错误：INVALID_MODE、SYMBOL_NOT_FOUND、SYMBOL_NOT_TRADING、INVALID_RANGE、INVALID_GRID_COUNT、INVALID_INVESTMENT、INVESTMENT_TOO_LOW、GRID_TOO_DENSE、NO_PRICE、PRICE_OUT_OF_RANGE、INVALID_STOP_LOSS、INVALID_TAKE_PROFIT、TOO_MANY_BOTS、INSUFFICIENT_BALANCE、ORDER_REJECTED、NO_LIQUIDITY、BOT_NOT_FOUND（404）、BOT_NOT_RUNNING。

## 可配置默认值（环境变量）

| 变量 | 默认 | 含义 |
|---|---|---|
| PORT | 8090 | 端口 |
| GRID_POLL_MS | 1000 | 订单/价格轮询间隔 |
| GRID_MAX_BOTS_PER_USER | 10 | 每用户运行中的机器人数 |
| GRID_MIN_GRIDS / GRID_MAX_GRIDS | 2 / 200 | 网格数范围 |
| GRID_MIN_INVESTMENT | 10 | 最小投入（计价币） |
| GRID_FEE_RATE | 0.001 | 仅预览用的费率（真实费率来自成交） |
| GRID_BUY_BUFFER | 0.003 | 初始市价买入的滑点缓冲 |
| GRID_MAX_RANGE_RATIO | 100 | 上下价最大倍数 |
| GRID_SNAPSHOT_SECONDS | 60 | 快照间隔 |
| GRID_SQUARE_MIN_RUN_SECONDS | 3600 | 上广场的最短运行时间 |
| GRID_SELL_ON_STOP | true | sellBaseOnStop 的默认值 |

## 策略广场一键复制

- `GET /strategy/grid/square` 每项新增 `copyCount`（被复制次数）；`sort` 新增 `copies`（按复制数降序）。
- `POST /strategy/grid/square/{id}/copy`（需登录）`{"investment":"300","stopLossPrice"?,"takeProfitPrice"?,"sellBaseOnStop"?}`
  - 复制源策略的交易对、模式、价格区间与网格数，投入金额与止盈止损由复制者设置；返回新策略详情及 `copiedFrom`。
  - 只能复制广场上展示的策略（运行中且运行时长 ≥ 广场门槛），否则 404；复制自己的策略返回 `SELF_COPY`。
  - 其它校验（余额、最多运行数、价格区间）与创建网格一致。成功后源策略 `copyCount` +1。
