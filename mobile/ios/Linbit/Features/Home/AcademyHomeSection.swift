import SwiftUI

/// 首页「学院」Tab：按 Web 信息架构展示 `pagedata.ts`「linbit 学院」分类目录。
/// 不伪称完整课程；合约细则跳转「合约指南」（非把学院整页等同合约指南）。
/// 现货：仅收录学院「新手入门」中已有的「现货与合约区别」摘要，不自创宣传页、不要求后端。
struct AcademyHomeSection: View {
    @Environment(\.palette) private var p

    private struct Cat: Identifiable {
        var id: String { title }
        let title: String
        let topics: [String]
        let footer: String?
        let actionTitle: String?
        let action: (() -> Void)?
    }

    /// 来源：apps/web/legacy/pagedata.ts → "linbit 学院"
    private var cats: [Cat] {[
        Cat(title: "新手入门", topics: [
            "什么是区块链与数字资产",
            "如何完成注册、身份认证与首次充值",
            "现货交易与合约交易的区别：现货成交后资产进现货账户；U 本位合约用保证金做多/做空，可能强平至全部保证金"
        ], footer: nil, actionTitle: "去身份认证", action: { Router.shared.openFeature(.kyc) }),
        Cat(title: "合约进阶", topics: [
            "杠杆、保证金与强平价格",
            "资金费率与标记价格（周期以品种接口为准）",
            "止盈止损与风险控制"
        ], footer: "详情见「合约指南」（工具→新手教学）；非学院全文。",
           actionTitle: "打开合约指南", action: { Router.shared.openFeature(.futuresGuide) }),
        Cat(title: "帮助入口", topics: [
            "账户与安全、充值与提现、交易划转等说明见帮助中心同源文案；App 内可通过客服工单联系支持"
        ], footer: "来源：pagedata「帮助中心」分类；无独立帮助 CMS API。",
           actionTitle: "打开客服工单", action: { Router.shared.openFeature(.support) }),
    ]}

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("目录摘自 Web「linbit 学院」，仅列已有分类主题；具体规则以交易页与接口为准。")
                .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                .padding(.horizontal, Spacing.pageH)
            ForEach(cats) { c in
                VStack(alignment: .leading, spacing: 8) {
                    Text(c.title).font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                    ForEach(Array(c.topics.enumerated()), id: \.offset) { _, t in
                        HStack(alignment: .top, spacing: 6) {
                            Text("·").foregroundColor(p.textTertiary)
                            Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    if let f = c.footer {
                        Text(f).font(.lb(10)).foregroundColor(p.textTertiary)
                    }
                    if let title = c.actionTitle, let action = c.action {
                        Button(title, action: action)
                            .font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                    }
                }
                .padding(14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                .padding(.horizontal, Spacing.pageH)
            }
            Text("来源：apps/web/legacy/pagedata.ts（linbit 学院 / 帮助中心）")
                .font(.lb(10)).foregroundColor(p.textTertiary)
                .padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
        }
    }
}
