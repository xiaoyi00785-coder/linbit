import SwiftUI

extension Dec {
    /// 提交给服务端的十进制字符串：向下截断、无千分位
    static func plain(_ d: Decimal, _ prec: Int) -> String {
        var v = d, r = Decimal(); NSDecimalRound(&r, &v, prec, .down)
        return NSDecimalNumber(decimal: r).stringValue
    }
    /// 步长字符串（如 "0.001"）→ 小数位数
    static func precision(of step: String?) -> Int {
        guard let s = step, let dot = s.firstIndex(of: ".") else { return 0 }
        let frac = s[s.index(after: dot)...]
        let trimmed = frac.reversed().drop { $0 == "0" }
        return trimmed.count
    }
    static func double(_ d: Decimal) -> Double { NSDecimalNumber(decimal: d).doubleValue }
}

/// 灰底胶囊分段（开仓/平仓、开多/开空、全仓/逐仓 等）
struct PillSegment: View {
    @Environment(\.palette) private var p
    let items: [String]
    @Binding var selected: Int
    var tints: [Color]? = nil
    var height: CGFloat = 32
    var body: some View {
        HStack(spacing: 0) {
            ForEach(items.indices, id: \.self) { i in
                let on = i == selected
                Text(items[i]).font(.lb(FontSize.label, on ? .semibold : .regular))
                    .foregroundColor(on ? (tints != nil ? Color.white : p.textPrimary) : p.textSecondary)
                    .frame(maxWidth: .infinity).frame(height: height - 4)
                    .background(RoundedRectangle(cornerRadius: Radius.sm - 2).fill(on ? (tints?[i] ?? p.bgElevated) : Color.clear))
                    .contentShape(Rectangle())
                    .onTapGesture { Haptics.tap(); selected = i }
            }
        }
        .padding(2)
        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
    }
}

/// 小号灰底标签按钮（全仓 / 100x）
struct ChipButton: View {
    @Environment(\.palette) private var p
    let title: String
    var arrow = true
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack(spacing: 4) {
                Text(title).font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                if arrow { Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)).foregroundColor(p.textSecondary) }
            }
            .padding(.horizontal, 10).frame(height: 24)
            .background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
        }.buttonStyle(PressableStyle())
    }
}

struct KVRow: View {
    @Environment(\.palette) private var p
    let k: String
    let v: String
    var vColor: Color? = nil
    var body: some View {
        HStack {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v).font(.lbNum(FontSize.small)).foregroundColor(vColor ?? p.textPrimary)
        }
    }
}

/// 竖排键值（仓位卡片里的三列）
struct KVCell: View {
    @Environment(\.palette) private var p
    let k: String
    let v: String
    var align: HorizontalAlignment = .leading
    var vColor: Color? = nil
    var body: some View {
        VStack(alignment: align, spacing: 4) {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textTertiary).underline(true, color: p.textTertiary.opacity(0.4))
            Text(v).font(.lbNum(FontSize.caption, .medium)).foregroundColor(vColor ?? p.textPrimary)
        }
        .frame(maxWidth: .infinity, alignment: align == .leading ? .leading : align == .trailing ? .trailing : .center)
    }
}

/// 功能开关关闭时的「即将上线」页
struct ComingSoonView: View {
    @Environment(\.palette) private var p
    let title: String
    var note = "该功能即将上线，敬请期待"
    var body: some View {
        VStack(spacing: 12) {
            Spacer()
            EmptyIllustration()
            Text("\(title) 即将上线").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            Text(note).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary).multilineTextAlignment(.center)
            Spacer(); Spacer()
        }
        .padding(.horizontal, 40).frame(maxWidth: .infinity).background(p.bg)
    }
}

/// 二级全屏页统一外壳：返回 + 标题 + 右侧
struct FeatureShell<Content: View, Trailing: View>: View {
    @Environment(\.palette) private var p
    let title: String
    var onBack: () -> Void = { Task { @MainActor in Router.shared.feature = nil } }
    @ViewBuilder var trailing: Trailing
    @ViewBuilder var content: Content
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: title, onBack: onBack) { trailing }
            content.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        }
        .background(p.bg.ignoresSafeArea())
    }
}
extension FeatureShell where Trailing == EmptyView {
    init(title: String, onBack: @escaping () -> Void = { Task { @MainActor in Router.shared.feature = nil } }, @ViewBuilder content: () -> Content) {
        self.init(title: title, onBack: onBack, trailing: { EmptyView() }, content: content)
    }
}

/// 迷你折线（策略卡片收益曲线）
struct Sparkline: View {
    let values: [Double]
    let color: Color
    var body: some View {
        GeometryReader { g in
            let lo = values.min() ?? 0, hi = values.max() ?? 1, span = max(hi - lo, 0.0001)
            Path { pa in
                for (i, v) in values.enumerated() {
                    let x = g.size.width * CGFloat(i) / CGFloat(max(values.count - 1, 1))
                    let y = g.size.height * (1 - CGFloat((v - lo) / span))
                    if i == 0 { pa.move(to: CGPoint(x: x, y: y)) } else { pa.addLine(to: CGPoint(x: x, y: y)) }
                }
            }.stroke(color, lineWidth: 1.5)
        }
    }
}

/// 全屏二级功能页（从首页宫格、现货顶栏进入）
enum FeaturePage: String, Identifiable {
    case earn, strategy, gridCreate, swap, chat, subAccounts, welfare, copyTrading, tradfi, futuresHistory, futuresRiskTiers, futuresFunding, changePassword, security, kyc, notifications, coupons, partner
    var id: String { rawValue }
}

struct FeatureHost: View {
    let page: FeaturePage
    @ObservedObject private var flags = AppFeatures.shared
    var body: some View {
        switch page {
        case .earn: EarnView()
        case .strategy: StrategyView()
        case .gridCreate: GridCreateView(copy: nil) { _ in Task { @MainActor in Router.shared.feature = nil } }
        case .swap: if flags.isOn("convert") { SwapView() } else { FeatureShell(title: "闪兑") { ComingSoonView(title: "闪兑") } }
        case .chat: if flags.isOn("chat") { ChatRoomView() } else { FeatureShell(title: "聊天室") { ComingSoonView(title: "聊天室") } }
        case .subAccounts: if flags.isOn("subAccounts") { SubAccountsView() } else { FeatureShell(title: "子账户") { ComingSoonView(title: "子账户") } }
        case .welfare: FeatureShell(title: "福利中心") { NewcomerView() }
        case .copyTrading: CopyTradingView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .futuresHistory: FuturesHistoryView(symbol: Router.shared.futuresSymbol)
        case .futuresRiskTiers: FuturesRiskTiersView(symbol: Router.shared.futuresSymbol)
        case .futuresFunding: FuturesFundingHistoryView(symbol: Router.shared.futuresSymbol)
        case .security: SecurityCenterView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .notifications: NotificationsView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .coupons: CouponsView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .partner: PartnerView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .kyc: KycView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .changePassword: ChangePasswordView(onBack: { Task { @MainActor in Router.shared.feature = nil } })
        case .tradfi: FeatureShell(title: "TradFi") { ComingSoonView(title: "TradFi", note: "股票、贵金属等传统金融资产交易即将上线") }
        }
    }
}
