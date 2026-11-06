# iOS adapt 截图（layout / 动态字体 / 多宽度）

## 触发
- 仅 `workflow_dispatch` 且 `adapt_layout=true`（**不会**在每个 PR push 自动跑，避免取消正在进行的冒烟截图）。
- 可选 `adapt_device_profile`：
  - `se375_xxxl`（默认）：375pt SE 类 + xxxLarge
  - `mid393_xxxl`：393pt（iPhone 16 类）+ xxxLarge
  - `max430_xxxl`：430pt（Plus/Pro Max 类）+ xxxLarge
  - `matrix_se_mid_max`：上述三档**同一 adapt job 顺序**拍摄；**一次** `generic/platform=iOS Simulator` Debug build

## 清单与分母
- 页面键：`mobile/ci/screens-adapt.txt`（只含 screen + light/dark/both）
- 设备/字体档：`mobile/ci/adapt-device-matrix.json`
- 精确计数：`python3 mobile/ci/tests/test_ios_screens_matrix.py`

## 已验证 vs 未验证（勿混）
| 宽度 | 证据 | 状态 |
|---|---|---|
| 375pt @2x + xxxl | adapt 产物 750×1334 | 有（单档） |
| ~393pt @3x + medium | PR 冒烟 1179×2556 | 有（**不是** xxxl，也**不是** 430） |
| 320pt | 无 bootable CI runtime（见 matrix `w320_se1_preview_only`） | **未验**；DEBUG Preview only |
| 430pt + xxxl | 无已下载产物 | **未验**（可选 profile） |

## 320pt
Xcode 16.4 **可能**列出 `iPhone SE (1st generation)` device type，但 macos-15 / iOS 18.x 镜像通常不提供可启动的 320pt runtime（iOS 18.5 默认设备以 iPhone 16/16e 系为主）。**不伪造已实机/已 CI**；本地用 `LayoutSizeProbe` `#Preview("320 preview-only")`（`#if DEBUG`）。
