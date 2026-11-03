# Linbit iOS（SwiftUI）

生成工程（需在 macOS 上执行）：
```bash
brew install xcodegen
cd ios && xcodegen generate && open Linbit.xcodeproj
```
- iOS 16+，纯 SwiftUI，无第三方依赖。
- 接口根地址、WS 地址写在 Info.plist 的 LBAPIBase / LBWSURL 中（测试网 api.cowa.click）。
- 目录：App（入口、Tab）、DesignSystem（令牌、主题、组件、图标）、Core（网络、WS、Decimal 格式化）、Features（各页面）。
- 图标是重绘的 SVG（../icons），已放入 Assets.xcassets，日间和夜间各一版。
