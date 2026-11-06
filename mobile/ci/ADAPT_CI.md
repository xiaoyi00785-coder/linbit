# iOS 适配 / Release 编译 CI 增补说明

本 PR 应用代码已含 ShotGate 与布局改动。因推送 `.github/workflows/ios.yml` 需要 PAT 的 `workflow` scope，工作流改动未直接推送。

请有 workflow 权限的维护者应用同目录 `ios-workflow-adapt.patch`，或手工加入：

1. **主 job**：Debug 构建后增加 `Release compile check`（独立 `${DERIVED}-release`，无截图），校验 `#if DEBUG` 剥离后 Release 可编。
2. **`workflow_dispatch` 输入** `adapt_layout`（boolean，默认 false）。
3. **可选 job `adapt`**：仅 `workflow_dispatch && adapt_layout`；精简 `screens-adapt.txt`；优先小屏模拟器；`simctl ui content_size extra-extra-extra-large`。

在应用 patch 前请先对最新 `ios.yml` 做三方合并。
