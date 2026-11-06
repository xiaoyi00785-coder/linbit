import SwiftUI

/// 合约指南（工具入口文案「新手教学」）：本地静态说明，**不是**完整「linbit 学院」。
/// 文案来源映射（只解释已上线能力与风险，不编造活动承诺）：
/// - apps/web/legacy/pagedata.ts → "仓位档位" / "标记价格历史" / "资金费率历史" / "保险基金" / "服务条款".风险提示
/// - 契约：`fundingIntervalHours` 按品种返回（OpenAPI FuturesContract / funding-history）；交易页用 nextFundingTime 倒计时
/// - 入口：FuturesToolsSheet「新手教学」；首页「学院」另有分类目录，本页仅标合约指南
struct FuturesBeginnerGuideView: View {
    @Environment(\.palette) private var p

    private struct Sec: Identifiable {
        var id: String { title }
        let title: String
        let paras: [String]
        let open: (() -> Void)?
        let cta: String?
    }

    private var sections: [Sec] {[
        Sec(title: "现货与合约", paras: [
            "现货以持有资产为主，成交后资产进入现货账户。",
            "U 本位永续合约用保证金做多或做空，可加杠杆；价格不利时可能触发强平，损失可至全部保证金。"
        ], open: nil, cta: nil),
        Sec(title: "杠杆、保证金与强平", paras: [
            "仓位价值越大，可用最高杠杆通常越低、维持保证金率越高；仓位价值落入的档位决定可用最高杠杆与维持保证金率（见「仓位档位」实时表）。",
            "维持保证金与标记价格参与强平判定；杠杆越高，价格小幅波动也可能触及强平，请按自身承受能力设置。"
        ], open: { Router.shared.openFeature(.futuresRiskTiers) }, cta: "查看仓位档位"),
        Sec(title: "标记价格", paras: [
            "标记价格用于计算未实现盈亏与强平，由指数价格与资金费基差加权得出，用于降低单一市场异常波动的影响。",
            "可在工具中按周期查看标记价格历史（公开行情接口）。"
        ], open: { Router.shared.openFeature(.futuresMarkHistory) }, cta: "查看历史标记价格"),
        Sec(title: "资金费率", paras: [
            "资金费按持仓价值在多空之间互相支付：费率为正时多头支付空头，为负则相反。资金费 = 仓位价值 × 资金费率。",
            "结算周期因品种而异，以该合约的 fundingIntervalHours、交易页资金费倒计时（nextFundingTime）及「历史资金费率」页展示为准，不保证所有品种均为固定 8 小时。"
        ], open: { Router.shared.openFeature(.futuresFunding) }, cta: "查看历史资金费率"),
        Sec(title: "风险准备金", paras: [
            "保险基金（风险准备金）用于承接强平时可能产生的穿仓亏损，降低自动减仓对盈利用户的影响。",
            "可在工具中查看当前余额与近期记录（公开行情接口，无需登录）。"
        ], open: { Router.shared.openFeature(.futuresInsurance) }, cta: "查看风险准备金"),
        Sec(title: "止盈止损与计算器", paras: [
            "合约下单与持仓支持止盈/止损等风险控制（以交易页实际可用选项为准）。",
            "工具中的「计算器」仅在本地估算开仓成本、强平价与盈亏，不含手续费与资金费，不构成下单或收益承诺。"
        ], open: nil, cta: nil),
        Sec(title: "风险提示", paras: [
            "数字资产价格波动剧烈，合约交易可能导致全部保证金损失，请谨慎评估自身风险承受能力。",
            "本页只说明 App 内已提供的合约相关功能与公开规则摘要；费率、档位、资金费周期与结算以交易页及接口返回为准。"
        ], open: nil, cta: nil),
    ]}

    var body: some View {
        FeatureShell(title: "合约指南") {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text("合约交易核心概念与风险摘要（非完整学院课程）。内容对齐 Web 静态说明与当前 OpenAPI 字段；具体数值以各品种接口与交易页为准。")
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    ForEach(sections) { s in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(s.title).font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                            ForEach(Array(s.paras.enumerated()), id: \.offset) { _, para in
                                Text(para).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true)
                            }
                            if let open = s.open, let cta = s.cta {
                                Button(cta) { open() }
                                    .font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                            }
                        }
                        .padding(14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                    }
                    Text("来源：apps/web/legacy/pagedata.ts；周期字段 fundingIntervalHours / nextFundingTime（docs/api/openapi.yaml）")
                        .font(.lb(10)).foregroundColor(p.textTertiary)
                }
                .padding(.horizontal, Spacing.pageH).padding(.vertical, 12).padding(.bottom, 24)
            }
        }
    }
}
