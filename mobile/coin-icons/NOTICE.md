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

## 全量 PNG 资源（batch1）
- `android/app/src/main/assets/coins/*.png` 为同一上游版本 `128/color/` 目录的 483 个 PNG，原样拷贝、未修改。
- 加载顺序：本地矢量 → 本地全量 PNG → CoinCap 公开 CDN → 首字母圆兜底。

## Solana (SOL) 例外

`sol.svg` / Android `coin_sol.xml` **不**使用 cryptocurrency-icons 的浅绿三杠版（与主流交易所不符）。
改用 [0xSequence/web3icons](https://github.com/0xSequence/web3icons) `icons/tokens/solana.svg` 衍生资源
（与 `apps/web/public/coins/sol.svg` 同源）：黑底圆 + 紫青渐变三斜杠，**MIT License**。
完整许可见 `apps/web/public/coins/LICENSE-ICONS.md`。`gen_coin_icons.py` 不得覆盖 `coin_sol.xml`。

## DOT / DOGE 例外

- `dot`：官方 Polkadot 白点环 + 品牌粉 `#E6007A`（源自 cryptologos / web `public/coins/dot.svg`），替换 cryptocurrency-icons 过时字母标。Android 为 PNG（含 transform，不可用 gen 转矢量）。
- `doge`：官方 Dogecoin 柴犬金币标（社区商标，仅用于标识资产；同源 web `public/coins/doge.svg`），替换通用字母 D 标。Android 为 PNG。

