# Batch12 阶段报告：理财页实质重做（先交一页）

## 对照来源
- 参考（真机/已存）：`/workspace/ref-capture/auth/028_earn_list.png`、`029_earn_btc_detail.png`
- 我方：Robolectric 离屏夹具 `asset-earn-normal-products.png` 等（非真机同状态实拍）
- 左右图：`compare-earn-list-ref028-vs-ours-offscreen.png`（左参考 / 右离屏夹具）

## 结构对齐（相对旧扁平 Kv 列表）
1. 理财资产头：总额、昨日收益、隐藏、我的持仓入口
2. 活期/定期分类（仅 API `kind`；无链上/借贷假入口）
3. 精选推荐横滑
4. 搜索 + 全部产品按币种展开 APR 区间
5. 申购/赎回弹层保留复核与失败态（测试只验证 UI，不写成功资金）

## 同批资产 bug（真机反馈）
- ≈¥--：改用已部署 `GET /market/fiat-rates?base=USDT`（rates 为每 1 USDT 法币）；不再因未部署 `/market/usd-rate` 整页无金额。缺币种时显示 `--` + 原因 + 重试。不本地硬锚定 USDT=USD。
- USDT 三角：估值单位 USDT/BTC/ETH 可点选并持久化；BTC/ETH 用现货 ticker 折算。
- 文档图标：打开「历史」sheet → 充值/提现记录或现货/合约历史（只读）。

## API 缺口（需后端）
- `/market/usd-rate` 与 ticker.`usd` 仍未部署（404）；现用 fiat-rates base=USDT 可显示法币≈。
- 理财：链上赚币 / Wealth Hub / 借贷、闪购倒计时等无 OpenAPI 字段 → 未造假入口。
- 资金账户「流水总账」若参考还有独立 ledger，当前接 `wallet/deposit|withdraw/records` + 交易历史；缺统一 account-ledger 请补契约。

## 未做（下批）
- 模拟交易页实质重做（缺独立参考屏；batch3/of 为自有包名截图不可当参考）
