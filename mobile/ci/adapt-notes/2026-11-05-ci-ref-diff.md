# CI 截图 vs 对照图差异（已实际打开阅读）

来源：
- CI artifact：`gh run download 42142418722`（main `ca421fa`，目录 `linbit-ios-ca421fa…`）
- 对照：`/workspace/ref2/3.jpg`（登录）、`/workspace/ref/28.jpg`（现货盘口；合约页无同编号完整对照时以交易页共性为准）

## 已阅读的 CI 图
| 文件 | 观察 |
|------|------|
| `login-light.png` | 有 Linbit 词标；主按钮空态灰底（禁用）正确；与 ref2/3 结构一致，ref 主按钮为可点黑底（有内容态） |
| `password-register-light.png` | 红框校验 + 规则勾选可见；底部留白大，键盘场景靠本 PR ScrollView 修复 |
| `futures-light.png` | **确定缺陷**：未登录时双「登录 / 注册」叠放；行情区国家/地区不可用横幅；底栏与图表折叠条拥挤 |
| `home-light.png` | 中心像素为品牌绿 `(155,227,0)`，实为 **启动页残留**，不能当首页布局验收（截图时机/splash 问题） |
| `spot-light.png` / `assets-light.png` | 已下载；本轮未逐像素写结论时不声称已审完 |

## 对照差异（登录）
- CI 多出词标 `LinbitWordmark`；ref2/3 描述以标题「登录」起头。属实现有意品牌强化，非错位。
- 禁用主按钮对比度低：符合「未输入不可点」，保留。

## 本 PR 针对证据的修复
- 合约未登录：单颗「登录 / 注册」（消除 futures-light 双按钮）
- 客服详情输入条：`safeAreaInset(edge: .bottom)`
- 动态字体：`Font.lb` 上限 1.35×，降低密排溢出
- SupportShot → `ShotRaw.support`（#68 stack 依赖，**尚未合 main**）

## 明确未完成
- 链上提币入口仍 Toast「将在提现阶段开放」（`WithdrawSheet`），**无完整提现表单可验**
- 无 Xcode：未跑 Release 编译 / adapt 截图；workflow 仍仅 patch
- #68/#78 未合 main；home CI 图不可用，#78 视觉需其 CI 产物或本地模拟器
