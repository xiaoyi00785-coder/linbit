# Adapt 设备/字体证据（实测，非宣称全过）

## 像素与逻辑宽（已下载产物）

| 产物目录 | PNG 数 | 像素 | 推定逻辑点 | 设备偏好 |
|---|---|---|---|---|
| `ci-shots-104-2300fb6-adapt/…2300fb6…` | **5** | **750×1334** 全部 | **375×667 @2x**（SE 类） | workflow prefer `iPhone SE (3rd generation)` |
| `ci-shots-adapt-104/…cec3a5b…` | 5 | 同 750×1334 | 同 375pt | 同 |

`screens-adapt.txt` 现列 **8** 键（含 assets-deposit / assets-withdraw / support-detail-open），但上述 tip 产物 **仅 5 张**（充提/客服详情缺失）——不能用 5 图冒充 adapt 清单全覆盖。

## 动态字体

- CI：`simctl ui content_size extra-extra-extra-large`，跑完复位 `medium`。
- App：`Font.lb` / `lbNum` 使用 `UIFontMetrics`，**封顶 1.35×**（`Theme.swift`），本批**未扩大**封顶。
- 目视：xxxl 截图相对默认字号增大有限（封顶生效）；不能当作「无封顶系统 xxxLarge」全貌。

## 未覆盖代表宽度（明确缺口）

| 逻辑宽 (pt) | 代表机型 | 本批 adapt 证据 |
|---|---|---|
| 320 | 极窄 / 旧 SE | **无** |
| **375** | SE 3rd / 8 | **有**（仅此） |
| 390 | iPhone 14/15 | **无** |
| 430 | Pro Max | **无** |

PR 冒烟图（`2300fb6-pr`）28 PNG 均为 **1179×2556**（约 393×852 @3x，iPhone 16 类），**不是**多宽度矩阵，且非 xxxLarge。

## 结论

**不以 5 张 xxxl 图当作双端/全尺寸适配全过。** 当前仅证明：单窄屏 SE 类 + 封顶动态字体下，5 个主 tab 类页面可出图。缺 320/390/430 与 adapt 清单中 3 个 fixture 页产物。
