# Batch12 Earn stage (vc6)

## 对照依据
- 参考：`/workspace/ref-capture/auth/028_earn_list.png` + `028_earn_list.xml`，package=`io.easiex.app`（现货底栏 + 顶栏 Spot/Bots/Earn/Convert）
- 我方：Robolectric 离屏 `asset-earn-normal-holding-vc6.png`（AssetFeatureHost earn + Spot 顶栏；非真机）
- 左右图：`/workspace/linbit-verify/batch12/compare-earn-list-ref028-vs-ours-vc6-offscreen.png`

## 本轮已对齐（真实已支持）
- 导航：资产/更多「理财」深链到现货顶栏 index=2；Host 内嵌 PrimaryTabs；无独立返回标题栏（onBack=null）
- 金额与 USDT 同一行
- 分类图标：仅渲染已支持的活期/定期，紧凑 78dp 格，不拉成全宽大卡；链上/财富/借贷无 API → 不造入口
- 精选推荐：灰底 126×100 横滑卡；APR 来自产品字段，不假 APR
- 全部产品：标题→搜索→折叠资产行（密度对照 028）；点开再申购/赎回

## 无法对齐 / 缺参考
- 四宫格后两格（On-chain / Wealth Hub / Loans）：无后端，不造无效入口
- 理财头 USDT 下拉换计价：summary 接口为 USDT，未接估值切换（资产页估值另计）
- 参考 Flash Sale / 营销角标文案：无对应字段时仅用 hot/newcomer/bonusApr→「热销/新手」
- 模拟交易：穷尽 `/workspace/ref-capture` + stores；仅 More 列表文案与合约页「Demo Trading」角标提及，**缺 `io.easiex.app` Demo Trading 主界面整页截图/XML**；不把自有 paper 图当参考、不盲造

## 资产页（保留已交付）
- ≈ 法币：`GET /market/fiat-rates?base=USDT`
- USDT 估值下拉：DisplayPrefs USDT/BTC/ETH
- 右上角历史：sheet 分类 → 充值/提现/现货/合约独立流水页（非单一合并页；对照无独立「资产总历史」页可证）

## 用户附件
- 声称路径 `.../attachments/f2f4c73….png`：本机全盘扫描无此文件名/SHA；无法 Read。资产 ≈/下拉/历史修复仍保留自 8a55392。
