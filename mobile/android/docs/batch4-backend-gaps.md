# Batch 4 后端缺口

## 闪兑（convert）
- `GET /convert/pairs` 仅返回 `minUsdt`/`maxUsdt`（USDT 计价）。参考 App 对每个币种显示单笔限额（如 BTC `0.000003~10.5`），后端缺少按币种的 `minAmount`/`maxAmount` 字段。当前：兑出币为 USDT 时显示 min~max，其他币种显示「请输入数量」。需要后端在 pair 中补 `fromMin`/`fromMax`。
- 未登录参考汇率来自公开现货行情 last 价推算（仅展示），后端无公开 indicative rate 接口。
- 闪兑记录页无筛选（参考有筛选图标）；`GET /convert/orders` 无 asset/时间筛选参数。
