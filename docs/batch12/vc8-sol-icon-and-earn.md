# vc8 / 0.3.5 — SOL 图标 + 理财批次

## t82u SOL 图标

| 项 | 内容 |
|---|---|
| 问题 | 浅绿圆白三杠（cryptocurrency-icons `#66F9A1`），与主流交易所黑底紫青渐变三斜杠不符 |
| 修复 | `coin_sol.xml` 手写 VectorDrawable（黑底 + aapt 线性渐变三斜杠） |
| 来源 | [0xSequence/web3icons](https://github.com/0xSequence/web3icons) `icons/tokens/solana.svg`，与 `apps/web/public/coins/sol.svg` 同源 |
| 许可 | MIT（见 `mobile/coin-icons/LICENSE-WEB3ICONS.md` / web `LICENSE-ICONS.md`） |
| 防护 | `gen_coin_icons.py` 的 `HANDMADE_ANDROID` 跳过 `sol`/`dot`/`doge`，避免回写错误扁平标 |
| 对比图 | `docs/batch12/sol-icon-compare-vc8.png` |

## 其他主流币审查

| 币 | 动作 |
|---|---|
| BTC/ETH/USDT/USDC/… | 与 web CC0 同源，保持 |
| DOT | 替换过时字母标 → 官方白点环 PNG（drawable-nodpi） |
| DOGE | 替换通用 D → 官方柴犬金币 PNG |
| ARB/APT/… 未打包 | 仍走 CoinCap CDN → 首字母圆，**不**编造映射 |

## 理财同批（延续）

- Simple Earn = simple + flexible + fixed；On-chain 独立；Crypto Loans 只读 `GET /loans/market`
- `EarnApr` 档位语义 + `EarnCatalogue` 逐条 soft-decode；未知 kind → UNKNOWN
- Wealth Hub / Flash Sale 仍为契约缺口（见 `mobile/android/docs/batch12-earn-contract-gaps.md`）

## 产物

- APK: `docs/batch12/linbit-vc8-0.3.5-earn-sol-icon.apk`（versionCode 8 / 0.3.5；debug cert SHA-256 `747ec0f1…c0111a`；apk sha256 `e0101439…9c8241dd`）
- 单测: CoinIconsTest / EarnAprTest / EarnProductKindDecodeTest 通过
