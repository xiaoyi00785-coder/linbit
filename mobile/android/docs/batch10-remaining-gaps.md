# 剩余前端差异精简清单（Batch10 后，有代码/参考依据）

> 设备：`/dev/kvm` 属主 `root:pulse-access`，权限 `crw-rw----`；当前用户 `box`（uid/gid 1000）不在 `pulse-access`/`kvm` 组。最小修复动作（需用户/宿主机）：将 `box` 加入 `pulse-access`（或等价 kvm 组），或 `setfacl -m u:box:rw /dev/kvm`；**不要**全局 chmod、不要提权脚本。未完成前不得声称真机/模拟器验收；离屏 Robolectric ≠ 真机。勿用旧 ANR 图代替当次验证。

## 本批已改（有参考）

| 项 | 参考 | 代码依据 |
|---|---|---|
| 偏好「颜色偏好」行尾 K 线对、「主题设置」行尾色块；充值账户上分隔线 | `ref-capture/auth/043_preferences.png`、`batch3/color.png` | `PreferencesContent.kt` + `ProfileRow.trailing` |
| 颜色偏好弹层：左蜡烛+文案+对勾 | 同上 color | `ColorCandles` / `MiniCandle` |
| 语言弹层：仅真正支持的简体中文（已回退假 14 语列表/toast） | `auth/044_language.png`（全量 14 语未实现，不算完成） | `PreferencesContent` 语言 sheet |
| 修复 LbSheet 内再套 `verticalScroll`（嵌套无限高） | — | `PreferencesContent` sheet Column |

## 可继续（前端可定，有参考或不造假数据）

1. **福利中心头图**：参考 3D 箱/币/券（`ref-welfare-1`）；现 `ic_gift_big` 矢量。缺合法 3D 素材则只能布局微调，不能冒充参考渲染。
2. **用户中心引导遮罩**：`ProfileLandingScreen`「偏好设置已移至用户中心」已有；对照 `mock-b4-profile-guide` / 参考是否需高亮锚点——需再截同状态参考。
3. **主题弹层三卡**：已有 ThemeCards；与参考细对比间距/AUTO 标。
4. **事件合约「⋯」菜单**：仅交易规则+新手指引；历史数据/区间限额/FAQ 缺后端内容，前端不可编造。

## 缺参考

- 币种详情页参考截图（现点币种进充值）。
- 画线工具完整菜单（仅入口可证）。
- Supertrend 参数参考确认（现 Wilder ATR10×3）。

## 缺正式规则 / 后端 / 部署

- `/market/usd-rate`、ticker `usd` 未部署 → `$--`。
- `/assets/deposit-account` 未部署 →「暂未开放」；**合约直充不得开放**（`DepositTarget.FUTURES` 仅当 supported 含 futures）。
- 深度盘口空、币种全称 `name`、策略账户接口、每日任务接口、新人 7/30/90 分档。
- 通知迁移 17 / 生产 link 数据（客户端 Batch9 已接契约）。
- placements 轮播空 → 自有三张兜底；储备金 `available=false` 空态已如实。

## 用户延后（勿擅自开工）

- Google 登录、法币买币、Linbit 卡产品。

## 占位/未开放（非改措辞即可）

- 每日任务「暂无每日任务」、API Key / 卡片 / 法币 unconfigured 空态、聊天 FEATURE_DISABLED 文案——需后端能力，前端保持真实空态。


### 语言（更正）
- **全量 14 语言国际化尚未实现**；仅列表展示或点选 toast 不算完成，已回退。
- 当前仅简体中文；参考语言页布局可日后复用，但不得冒充已支持。

### 设备解阻（Batch11 research）
- 宿主机 KVM：`dmesg` `kernel BUG ... kvm_arch_vcpu_create`（见 `linbit-verify/batch4/batch11-kvm-dmesg.txt`）。
- 官方/论坛公共办法：`emulator ... -accel off`（`emulator -help-accel`；Cursor 论坛 staff 建议 Bot computer 用无加速）。
- 本机验证：`-accel off` 可冷启原 AVD（保留 userdata，约 191s boot_completed），ADB device；但 framebuffer 花屏/绿屏，随后 **System UI isn't responding**，无法完成偏好/聊天/画线同状态实拍。
- **全量 14 语国际化仍未实现**；仅简体中文 ≠ 参考 14 语完成。
- 恢复路径（debugging-the-box）：请用户在桌面端点 [Update Bot's Computer](grokbot://app/v1/settings?id=update-computer)（重建实例，保留文件/登录；apt 等需重装）。不要 Reset。不自行改内核/全局 chmod。
