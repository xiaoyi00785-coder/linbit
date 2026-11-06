# adapt job xxxLarge 截图阅读记录（run 42151727198，head cec3a5b）

产物目录（本机下载）：`/workspace/ci-shots-adapt-104/linbit-ios-adapt-cec3a5b833220991fb00f14daa17f313ce45b8dd/`

| 文件 | 结论 |
|------|------|
| login-light-xxxl.png | 布局完整；禁用主按钮灰态正常；安全区正常 |
| password-register-light-xxxl.png | 红框校验与规则勾选清晰；大字下未裁切确认按钮区域（键盘未展开） |
| futures-light-xxxl.png | **已修点**：未登录仅一颗「登录/注册」。仍见顶栏「事件合约」在窄宽下易挤；行情失败横幅曾被固定行高裁切 → 本轮改 InlineBanner 多行；底栏标签加 minimumScaleFactor |
| spot-light-xxxl.png / assets-light-xxxl.png | 已下载备查；本记录未逐像素写满，不作虚假全绿 |

build job（含 Release compile + 全量截图）观察时仍 in_progress，未宣称通过。

## 补充阅读（同批产物）
| 文件 | 结论 |
|------|------|
| spot-light-xxxl.png | 未登录单登录钮；区域限制遮罩占盘口；底部委托区与悬浮 Tab 偏挤 → 加大 `.padding(.bottom, tabBarH+72)` |
| assets-light-xxxl.png | 未登录空态对齐正常；顶部分类 Tab 可横滑，大字下最右项易被裁属既有横滑预期 |

266630c 的 PR CI 与 cec3a5b build job 观察时仍 in_progress，无新 Release 截图可下。

## 本轮本地加固（未等新 CI 截图）
- WithdrawView：地址/地址簿纵向；确认提现 safeAreaInset 吸底；2FA/资金密码 scrollTo；键盘 Done resignFirstResponder；提交前收起键盘
- SpotTradeView：底栏 padding → tabBarH+72 + scrollDismissesKeyboard
- SupportTicketDetailView：composer FocusState + 键盘 Done
- FuturesTradeView / DepositView：键盘 Done
- Forms.lbKeyboardDone：完成键同时 resignFirstResponder；新增无参重载
