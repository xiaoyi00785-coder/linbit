# Android EasiCoin 还原：第三批实况

不是 100% 验收。本地编写（/workspace/linbit-android），未动 /workspace/linbit，未用云端。参考 App 只读浏览补截（/workspace/ref-capture/batch3），未下单/划转/提现/领奖/改安全/退出。

## 已做
- 我的：热门宫格按参考顺序：邀请好友、充值返现、TradFi、交易抽奖、合约、现货、模拟交易、理财、快捷买币、聊天室、帮助中心、客服、API Key。
- 邀请好友（深色活动页）：数据总览（全部/昨日/本月）、受邀人列表、我的返佣、邀请步骤、分享链接/复制邀请码，复用 partner 接口。
- 充值返现、交易抽奖：按参考结构（头图、活动时间、档位报名/奖池、待开奖/累计抽奖、规则折叠、悬浮邀请入口）；接口 404 时显示「暂无活动」。
- API Key：空态 + 创建只读 Key（备注、IP 白名单）、Secret 只显示一次、列表/删除确认；接口 404 时显示未开放。
- 偏好设置：颜色偏好三选一（含红涨蓝跌）、主题设置子页（新/经典主题，涨跌色全局生效）、默认充值账户（/assets/deposit-account，含跟随平台）。
- Google 登录：Credential Manager + /auth/providers 取 clientId + /auth/google；未配置时如实提示。
- 游客客服：/support/guest，mode=external 时打开 entryUrl，否则提示未开通。
- 法币买币：/fiat/buy 状态页；不调用报价/下单。
- 盈亏分析：7/30/90 天合约累计盈亏曲线、交易统计、现货成交统计（现货已实现盈亏标为暂不提供）。
- 学院：分类 Tab、文章列表、详情、搜索（/market/search 的 articles）。
- 卡片：/cards/* 页面（卡面、产品/发行方、申请/状态），未配置与 409 如实显示；入口在「全部功能」。

## 未完成 / 差异
- 活动页颜色/插画只用了 App 内已有素材，没有参考的 3D 插画；活动 H5 页脚（About/社区）未做。
- 登录态页面只有 Robolectric loopback 渲染（mock-*.png），没有 Linbit 测试账号做真机截图；弹层（Sheet/Dialog）因在独立窗口没截进 mock 图。
- batch2 里其余差异（现货/合约逐状态、资产非空状态、我的引导遮罩、TradFi 多屏宽、福利/事件合约）本批**没有处理**。
- 聊天室、首页运营位、储备证明、USD 折合尚未接入新契约。
- 卡片入口位置参考 App 里没找到，放在「全部功能」。
