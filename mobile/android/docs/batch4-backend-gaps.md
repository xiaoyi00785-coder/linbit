# Batch 4 后端缺口

## 闪兑（convert）
- `GET /convert/pairs` 仅返回 `minUsdt`/`maxUsdt`（USDT 计价）。参考 App 对每个币种显示单笔限额（如 BTC `0.000003~10.5`），后端缺少按币种的 `minAmount`/`maxAmount` 字段。当前：兑出币为 USDT 时显示 min~max，其他币种显示「请输入数量」。需要后端在 pair 中补 `fromMin`/`fromMax`。
- 未登录参考汇率来自公开现货行情 last 价推算（仅展示），后端无公开 indicative rate 接口。
- 闪兑记录页无筛选（参考有筛选图标）；`GET /convert/orders` 无 asset/时间筛选参数。

## 首页
- 参考登录态榜单下方有「公告 / 快讯 / 学院」。后端无公告、快讯接口 → 前两个 Tab 显示真实空态；学院取 `/academy/articles?limit=5`。
- 榜单 TradFi 分组用 `/tradfi/assets` 的 symbol 过滤合约行情。
- 首页轮播运营位为空时本地兜底为 福利中心 / 跟单 / 策略交易 三张（自绘矢量配图，指向 App 内真实页面）；有后端 carousel 图片时用后端图片。

## 现货交易页
- 生产 `GET /market/depth?symbol=BTCUSDT` 返回 `bids:[] asks:[]`（2026-11-08 13:00 EST 实测），盘口显示真实空态「--」；参考盘口有 5 档深度与 B/S 比例。需撮合/做市深度上线。
- 盘口「≈ $」依赖 `/market/usd-rate`，生产未部署时显示 `$--`。
