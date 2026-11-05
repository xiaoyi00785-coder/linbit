# 币种图标来源

本目录 SVG 来自 [spothq/cryptocurrency-icons](https://github.com/spothq/cryptocurrency-icons) 的 `svg/color/`，
采用 **CC0 1.0 Universal**（完整文本见 `LICENSE.md`）。

- 源版本：`1a63530be6e374711a8554f31b17e4cb92c25fa5`（2026-11-05 获取）
- 项目作者与贡献者：Christopher Downer、spothq 及 cryptocurrency-icons contributors
- 本地 SVG 保持上游内容不变；Android VectorDrawable 由 `mobile/tools/gen_coin_icons.py` 转换。
- 上架币种的公开行情接口不包含资产账户中的全部币种，因此同时打包主流币种。
- 图标集未覆盖的币种不映射其他币种图标，继续走远程图标，最后回退到首字母圆。

后续 iOS 接入：运行 `python3 mobile/tools/gen_coin_icons.py --ios`，生成本目录下
`CoinSymbols.swift` 以及 iOS `Assets.xcassets` 的 `coin-*.imageset`。iOS 的 `CoinIcon`
后续应优先 `Image("coin-" + normalizedBase)`，再使用现有远程图标与字母兜底。
