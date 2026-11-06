import SwiftUI

/// 价格预警列表 + 创建/编辑/删除入口（正式 FeaturePage）。
/// DEBUG+lb.shot 写走本地 mock；Release 走正式 API（ShotGate 关闭，夹具不误封）。
struct PriceAlertsListView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    @ObservedObject private var flags = AppFeatures.shared
    @StateObject private var model = PriceAlertsSession()
    @State private var detail: PriceAlert?
    @State private var editor: PriceAlertEditorSheet.Mode? = nil
    @State private var deleting: PriceAlert? = nil
    var onBack: (() -> Void)? = nil

    /// 契约文案：once + INDEX + in_app；上限取 OpenAPI enum=50（若列表项带回 limit 则用之）
    private var activeCap: Int {
        if case let .loaded(pg) = model.page, let c = pg.items.compactMap(\.limit?.activePerUser).first {
            return c
        }
        return PriceAlertsFeature.activePerUserCap
    }

    private var limitNote: String {
        "仅支持现货 INDEX 一次性跨价预警，触发后只发站内信；每账户最多 \(activeCap) 条有效预警（契约 activePerUser）。编辑/删除使用 revision 乐观锁。"
    }

    private var loadedActiveCount: Int {
        guard case let .loaded(pg) = model.page else { return 0 }
        return pg.items.filter { $0.status == "active" || ($0.enabled && $0.status != "triggered") }.count
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "价格预警", onBack: onBack) {
                if flags.isOn(PriceAlertsFeature.flagKey) || PriceAlertDemo.on {
                    if session.isLoggedIn || PriceAlertDemo.on {
                        Button {
                            editor = .create
                        } label: {
                            Image(systemName: "plus").font(.system(size: 18, weight: .medium)).foregroundColor(p.textPrimary)
                                .frame(width: 44, height: 44, alignment: .trailing)
                        }
                        .buttonStyle(PressableStyle())
                    }
                }
            }
            if !flags.isOn(PriceAlertsFeature.flagKey) && !PriceAlertDemo.on {
                featureClosed
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
        .task { await model.reload(); openShotSheetsIfNeeded() }
        .onChange(of: session.isLoggedIn) { _ in
            model.invalidate()
            Task { await model.reload() }
        }
        .lbSheet(Binding(get: { detail != nil }, set: { if !$0 { detail = nil } })) {
            if let a = detail {
                PriceAlertDetailSheet(
                    alert: a,
                    onEdit: { detail = nil; editor = .edit(a) },
                    onDelete: { detail = nil; deleting = a },
                    onClose: { detail = nil }
                )
            }
        }
        .lbSheet(Binding(get: { editor != nil }, set: { if !$0 { editor = nil } })) {
            if let mode = editor {
                PriceAlertEditorSheet(
                    mode: mode,
                    session: model,
                    loadedActiveCount: loadedActiveCount,
                    activeCap: activeCap
                ) { ok in
                    editor = nil
                    if ok { Task { await model.reload() } }
                }
            }
        }
        .lbSheet(Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } })) {
            if let a = deleting {
                PriceAlertDeleteSheet(alert: a, session: model) { ok in
                    deleting = nil
                    if ok { Task { await model.reload() } }
                }
            }
        }
    }

    /// flag 关闭：准确说明，不冒充 ComingSoon / 不假装已实现
    private var featureClosed: some View {
        VStack(spacing: 12) {
            Spacer()
            Image(systemName: "bell.slash").font(.system(size: 36)).foregroundColor(p.textTertiary)
            Text("价格预警暂未开放").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            Text("客户端功能开关 priceAlerts 已关闭，或服务端返回 FEATURE_DISABLED。开通后可创建现货 INDEX 一次性站内预警（每账户最多 \(PriceAlertsFeature.activePerUserCap) 条有效）。")
                .font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, Spacing.pageH)
            Spacer()
        }
    }

    @ViewBuilder private var listBody: some View {
        InfoNote(text: limitNote).padding(.horizontal, Spacing.pageH).padding(.top, 8)
        if model.isRateLimited {
            InlineBanner(text: "操作过于频繁，请 \(model.rateLimitRemainingSec) 秒后再试")
        }
        if model.featureDisabled {
            VStack(spacing: 8) {
                Spacer()
                Text("价格预警暂不可用").font(.lb(FontSize.title, .medium)).foregroundColor(p.textPrimary)
                Text(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                    .font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Button("重试") { Task { await model.reload() } }
                    .font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
                Spacer()
            }
        } else {
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
                LBButton(title: "创建预警", enabled: loadedActiveCount < activeCap) { editor = .create }
                    .padding(.horizontal, Spacing.pageH).padding(.top, 16)
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

    private func openShotSheetsIfNeeded() {
        guard PriceAlertDemo.on else { return }
        if PriceAlertDemo.openCreate { editor = .create }
        else if PriceAlertDemo.openEdit { editor = .edit(PriceAlertDemo.example(active: true)) }
        else if PriceAlertDemo.openDelete { deleting = PriceAlertDemo.example(active: true) }
    }
}

struct PriceAlertDetailSheet: View {
    @Environment(\.palette) private var p
    let alert: PriceAlert
    var onEdit: (() -> Void)? = nil
    var onDelete: (() -> Void)? = nil
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
                HStack(spacing: 10) {
                    if let onEdit {
                        LBButton(title: "编辑", kind: .secondary) { onEdit() }
                    }
                    if let onDelete {
                        LBButton(title: "删除", kind: .sell) { onDelete() }
                    }
                }
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
