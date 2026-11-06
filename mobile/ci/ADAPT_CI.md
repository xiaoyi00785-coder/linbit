# iOS 适配 / Release 编译 CI 增补说明

**状态（#104）**：已将本说明对应改动写入 `.github/workflows/ios.yml`（同分支普通 push）。
原 `ios-workflow-adapt.patch` 保留作历史对照；以仓库内实际 `ios.yml` 为准。

## 行为

1. **主 job `build`**：Debug 模拟器构建与既有截图之后，增加 **Release compile check**（独立 `${DERIVED}-release`，无截图、无签名），校验 `#if DEBUG` / ShotGate 剥离后 Release 可编。
2. **`workflow_dispatch` 输入** `adapt_layout`（boolean，默认 false）。
3. **可选 job `adapt`**：仅 `workflow_dispatch && adapt_layout == true`；使用 `mobile/ci/screens-adapt.txt`；优先小屏 iPhone；`simctl ui content_size extra-extra-extra-large`；产物 `shots-adapt/`。

## 安全边界

- `permissions.contents: read`；无部署、无 TestFlight / App Store / 资金相关步骤。
- `adapt` 默认不跑，避免每个 PR 重复全量截图。
- 全栈部署冻结期：即使 CI 绿也不合 main（draft #104）。
