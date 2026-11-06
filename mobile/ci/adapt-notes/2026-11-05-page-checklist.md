# iOS 逐页视觉清单（#104 / 本批）

> 规则：旧 tip 截图不冒充新 tip 验收；Release 需真实构建证据；不以脚本绿代替视觉。
> 对照：`/workspace/ref`、`/workspace/ref2`。本机无 Xcode，视觉以 CI 截图 + Read 打开为准。

## 产物与 tip 绑定

| 来源 | SHA | 路径 / run | 可否当新 tip 验收 |
|------|-----|------------|-------------------|
| adapt xxxl（旧） | `cec3a5b` | `/workspace/ci-shots-adapt-104/linbit-ios-adapt-cec3a5b…` | **否**（仅对照缺口） |
| push iOS `42153888929` | `4e326fc` | build 进行中；adapt 因非 workflow_dispatch 跳过 | 待 build 产物 |
| dispatch adapt `42154944871` | `4e326fc` 分支 tip | 手动 `adapt_layout=true` | 待产物；成功后才可当本 tip 适配证据 |
| 本批代码 tip | push 后新 SHA | PR #104 draft | — |

## 后端依赖（有需求、缺接口，不阻塞其他独立项）

| 功能 | 客户端现状 | OpenAPI | 处理 |
|------|------------|---------|------|
| 模拟交易 | Tab + ComingSoonView | 无 paper/demo 交易 path | 文案标明待后端；不造假盘 |
| 价格预警 | 合约工具入口 | 无独立 alert CRUD（仅他处 `alertCooldownSec`） | Toast 标明需后端；不造假表单 |
| 交易机器人 | 现货底栏入口 | 策略/网格另议 | Toast「即将开放」保留 |

## 已实际打开阅读（cec3a5b xxxl，对照用）

| 页 | 文件 | 对照 | 结论 | 状态 |
|----|------|------|------|------|
| 登录 | login-light-xxxl | ref2/3.jpg | 结构对齐；CI 多词标；空态主钮灰 vs ref 可点黑=输入态差 | 已看，差异可接受 |
| 设密 | password-register-light-xxxl | （注册流程） | 红框+规则勾选清晰；确认禁用正确；键盘未出时底空白大 | 已看 |
| 合约 | futures-light-xxxl | ref/11.jpg | 单「登录/注册」OK；顶栏「事件…」易裁；横幅右裁（旧 tip，266630c 后 InlineBanner 多行待新图验）；底栏近登录钮 | 已看；顶栏本批 PrimaryTabs fixedSize |
| 现货 | spot-light-xxxl | ref/28.jpg | 底栏「交易机器人」大字裁切；文案「手续费率」vs ref「预估手续费」；区域限制遮罩占盘口属环境 | 已看；本批横滑+文案 |
| 资产 | assets-light-xxxl | — | 未登录空态居中正常；顶部分类横滑最右易裁属预期 | 已看 |
| 首页 | （本批无新 home 图） | — | 旧 home 曾为开屏绿，不可用 | **未看新图** |

## 未看 / 待新 tip 产物

screens-pr / screens-adapt 外：home 日夜、markets、充提表单页、客服列表/详情、KYC、通知、理财/闪兑/跟单、合约杠杆/TPSL 弹层等。等 `4e326fc`+ 后续 tip 的 build/adapt 产物再逐张 Read。

## 本批已修（代码，待新截图复验）

- Spot 委托/条件单/机器人底栏横滑 + scale；「预估手续费」对齐 ref#28
- PrimaryTabs `fixedSize`，减轻「事件合约」被裁成「事件…」
- 价格预警 / 模拟交易：明确后端依赖文案
- 上一 tip 已含：提现吸底与 2FA、客服/合约/充值键盘 Done、Spot 底 padding

## 构建 / 契约

- 本环境无 Xcode：`xcodebuild` 不可用；以 GitHub Actions iOS `build` job 为 Release/编译证据。
- OpenAPI 契约：提现已按既有 `/wallet/withdraw`；价格预警/模拟盘无 path 可对。

## 4e326fc PR 冒烟截图（run 42153888929，merge SHA `803080f`，已实际 Read）

路径：`/workspace/ci-shots-104-4e326fc/linbit-ios-803080f…/`  
**Release/编译证据**：该 run `build` conclusion=`success`（event=pull_request）。非 xxxLarge。

| 页 | 文件 | 结论 | 状态 |
|----|------|------|------|
| 首页 | home-light.png | **真实首页**（非开屏绿）；资产/充值/宫格/卡片可见；底内容与悬浮 Tab 仍偏近 | 已看；本批加大 home padding |
| 合约 | futures-light.png | 顶栏「事件合约」完整；横幅全文多行；单登录钮；**仓位区登录钮被悬浮 Tab 挡住一截** | 已看；本批加大 futures padding |
| 现货 | spot-light.png | 结构近 ref#28；仍见「手续费率」（790d6f5 已改文案，本图早于该 tip） | 已看 |
| 登录 | login-light.png | 与 ref2/3 结构一致 | 已看 |
| 资产 | assets-light.png | 未登录空态正常 | 已看 |
| 客服列表 | support-list-light.png | 列表/状态徽标清晰，安全区正常 | 已看 |

未在本批逐张打开但仍在同产物内：markets、futures-chart/leverage/tpsl、earn、swap、kyc、notif、security、loan、tradfi、copy、coupon、home-bell、assets-overview/networks、日夜间等——标为**未看**，不冒充已验。

## tip `790d6f5`+

含现货底栏横滑、预估手续费、PrimaryTabs fixedSize、预警/模拟盘依赖文案；以及本段 home/futures 底栏留白。新 CI：iOS `42155738788` 等。旧图不冒充该 tip 验收。
