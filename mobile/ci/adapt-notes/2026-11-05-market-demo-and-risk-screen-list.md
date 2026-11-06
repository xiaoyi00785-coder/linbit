# MarketDemo + screens-risk 截图范围设计

## 根因
CI 跑在地区受限网络：`/market/*` 与 `/futures/market/*` 返回 REGION_RESTRICTED。
既有 ShotGate 只路由页面/弹层（`futures-chart` 打开面板），**不注入** ticker/depth/klines，
故截图多为地区拒绝横幅 / 空盘口 / K 线错误态，**不等于**正常业务态验收通过。

## MarketDemo（已实现，本地 tip）
- 文件：`Features/Common/MarketDemo.swift`
- 闸门：`ShotGate.enabled`（`#if DEBUG` true / Release false）且 `lb.shot` 命中 `futures*` / `spot*` / `markets*`
- 数据：OpenAPI 同形 `APIDepth` / `APIFuturesTicker` / `APIKline` / `APIFuturesKlinesKlinesInner` 合成 example
- 模式：默认 `ok`；键后缀 `-empty` / `-error` → 空/错误 UI
- 接线：`FuturesModel.refreshMarket` / `loadContract`、`SpotModel.loadBook`、`FuturesChartPanel` / `SpotChartPanel`
- 安全：夹具态 `place` toast return；**不**写生产启动路径；**不**调用 KYC/提现 API
- 明确：example fixture ≠ 实盘行情 ≠ 虚构资金入账

## screens-risk 可选截图（workflow 设计，避免重复 full 成本）
目标：高风险 12+ 页可单独拍，**不扩大 permissions / 不部署**。

| 方案 | 做法 | 构建成本 | 选用 |
|------|------|----------|------|
| A | `workflow_dispatch` 增加 `screen_list`: screens-pr \| screens-risk；build job 只改 `SCREEN_LIST` 路径 | 仍 1× Debug build + 较短截图环 | **采用**（最小改动） |
| B | 拆独立 risk job 再编一次 | 2× build | 否 |
| C | 跨 run 复用 artifact 装包 | 需写 permission/artifact 传递，易扩权 | 否（本批不做） |

PR 事件忽略 `screen_list`，固定 `screens-pr.txt`，避免误拍 risk 打断主验收。
adapt job 不变（已单 build 多档）。risk 清单比 pr 短 → 截图阶段更省，但无法省掉当次 Debug compile。

## 未验清单（本批后仍待新 tip 图）
- 现货/合约**正常盘口+K线**（需 push 后 CI 新图；旧 35 图仍是地区拒绝）
- screens-risk：kyc-form / passkeys / devices / 2fa-disable / copy-detail / follow / loan-quote / loans / withdraw-ok|paused|records
