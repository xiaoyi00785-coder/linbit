import SwiftUI

/// 价格预警列表垂直切片（正式 FeaturePage；入口：合约工具「价格预警」、首页更多）。
/// 创建/编辑/删除 UI 下批；本页动作在 DEBUG/ShotGate 下不远端写。
struct PriceAlertsListView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    @ObservedObject private var flags = AppFeatures.shared
    @StateObject private var model = PriceAlertsSession()
    @State private var detail: PriceAlert?
    var onBack: (() -> Void)? = nil

    /// 契约限制文案（OpenAPI：once + INDEX + in_app；activePerUser=50）
    private let limitNote = "仅支持现货 INDEX 一次性跨价预警，触发后只发站内信；每账户最多 50 条有效预警。编辑/删除使用 revision 乐观锁。"

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "价格预警", onBack: onBack)
            if !flags.isOn(PriceAlertsFeature.flagKey) && !PriceAlertDemo.on {
                // flag 关闭时仍展示 ComingSoon；不能把它当唯一安全闸——列表加载仍可能 403
                ComingSoonView(title: "价格预警")
            } else if !session.isLoggedIn && !PriceAlertDemo.on {
                Spacer()
                EmptyStateView(text: ErrorText.loginRequired)
                Button {
                    session.showAuth = true
                } label: {
                    Text("登录 / 注册").font(.lb(FontSize.label, .medium)).foregroundColor(p.textOnPrimary)
                        .frame(maxWidth: .infinity).frame(height: 44)
                        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.primary))
                }
                .padding(.horizontal, Spacing.pageH)
                Spacer()
            } else {
                listBody
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await model.reload() }
        .onChange(of: session.isLoggedIn) { _ in
            model.invalidate()
            Task { await model.reload() }
        }
        .lbSheet(Binding(get: { detail != nil }, set: { if !$0 { detail = nil } })) {
            if let a = detail { PriceAlertDetailSheet(alert: a, onClose: { detail = nil }) }
        }
    }

    @ViewBuilder private var listBody: some View {
        InfoNote(text: limitNote).padding(.horizontal, Spacing.pageH).padding(.top, 8)
        if model.isRateLimited {
            InlineBanner(text: "操作过于频繁，请 \(model.rateLimitRemainingSec) 秒后再试")
                .padding(.horizontal, Spacing.pageH).padding(.top, 8)
        }
        switch model.page {
        case .idle, .loading:
            Spacer(); ProgressView(); Spacer()
        case let .failed(m):
            ErrorStateView(message: m) {
                Task { await model.reload() }
            }
        case let .loaded(pg) where pg.items.isEmpty:
            Spacer()
            EmptyStateView(text: "暂无价格预警")
            Text("可在行情到达目标价时收到站内提醒")
                .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                .padding(.top, 6)
            Spacer()
        case let .loaded(pg):
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(pg.items) { a in
                        row(a).onTapGesture { detail = a }
                        Divider().overlay(p.border)
                    }
                    if model.canLoadMore {
                        HStack {
                            if model.loadingMore { ProgressView().scaleEffect(0.8) }
                            Text(model.loadingMore ? "加载中…" : "加载更多")
                                .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        .frame(maxWidth: .infinity).frame(height: 44)
                        .onAppear { Task { await model.loadMore() } }
                    } else if (pg.total ?? pg.items.count) > 0 {
                        Text("没有更多了").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                            .frame(maxWidth: .infinity).frame(height: 44)
                    }
                }
                .padding(.horizontal, Spacing.pageH)
            }
        }
    }

    private func row(_ a: PriceAlert) -> some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(a.symbol).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                    Text(a.market.uppercased()).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        .padding(.horizontal, 4).frame(height: 18)
                        .background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                    statusTag(a.status)
                }
                Text("\(conditionText(a.condition)) \(a.threshold) · \(a.priceSource) · \(a.triggerMode)")
                    .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                if let note = a.note, !note.isEmpty, !note.hasPrefix("DEBUG") {
                    Text(note).font(.lb(FontSize.small)).foregroundColor(p.textTertiary).lineLimit(1)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textTertiary)
        }
        .padding(.vertical, 14)
        .contentShape(Rectangle())
    }

    private func conditionText(_ c: String) -> String {
        switch c {
        case "gte": return "≥"
        case "lte": return "≤"
        default: return c
        }
    }

    private func statusTag(_ s: String) -> some View {
        let (t, c): (String, Color) = {
            switch s {
            case "active": return ("进行中", p.up)
            case "triggered": return ("已触发", p.brand)
            case "disabled": return ("已停用", p.textTertiary)
            default: return (s, p.textTertiary)
            }
        }()
        return Text(t).font(.lb(FontSize.small)).foregroundColor(c)
            .padding(.horizontal, 6).frame(height: 18)
            .background(RoundedRectangle(cornerRadius: 4).fill(c.opacity(0.12)))
    }
}

/// 只读详情（创建/编辑/删除 UI 下批；本页无写按钮）
struct PriceAlertDetailSheet: View {
    @Environment(\.palette) private var p
    let alert: PriceAlert
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "预警详情", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                kv("交易对", alert.symbol)
                kv("市场", alert.market)
                kv("条件", "\(alert.condition) \(alert.threshold)")
                kv("价格源", alert.priceSource)
                kv("触发", alert.triggerMode)
                kv("渠道", alert.channels.joined(separator: ", "))
                kv("状态", alert.status)
                kv("revision", "\(alert.revision)")
                if let o = alert.lastObservedPrice { kv("最近观测价", o) }
                if let t = alert.triggeredPrice { kv("触发价", t) }
                if let n = alert.note, !n.isEmpty { kv("备注", n) }
                Text("本页只读。创建 / 编辑 / 删除下一批实现；DEBUG 夹具下不会请求写接口。")
                    .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .padding(.top, 8)
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.bottom, 24)
        }
    }
    private func kv(_ k: String, _ v: String) -> some View {
        HStack {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v).font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
        }
    }
}
