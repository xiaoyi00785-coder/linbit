import SwiftUI

/// Paper 订单 / 成交历史（生产页）：独立 `/paper/orders` · `/paper/trades`，带 generation。
/// 分页防重复；失败保留原列表同页重试；Decimal 字符串原样；不伪造真钱成交。
struct PaperHistoryView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var auth = Session.shared
    @StateObject private var model = PaperTradingSession()
    @State private var tab = 0 // 0 订单 1 成交
    var onBack: (() -> Void)? = nil

    private let note = "模拟标识 mode=paper · 价格源 INDEX · 与真钱现货/合约成交隔离。列表金额为十进制字符串原样展示；generation 对应当前虚拟账户代数。"

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "模拟历史", onBack: onBack)
            if !PaperTradingFeature.isFlagOn && !PaperTradingDemo.on {
                closed
            } else if !auth.isLoggedIn && !PaperTradingDemo.on {
                login
            } else {
                main
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await bootstrap() }
        .onChange(of: auth.isLoggedIn) { _ in
            model.invalidate()
            Task { await bootstrap() }
        }
        .onAppear { applyShotTab() }
    }

    private var closed: some View {
        ScrollView {
            VStack(spacing: 12) {
                Spacer().frame(height: 48)
                Text("模拟交易暂未开放").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                Text("功能开关 paperTrading 缺省关闭。").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Text(note).font(.lb(FontSize.small)).foregroundColor(p.textTertiary).multilineTextAlignment(.center)
            }.padding(Spacing.pageH)
        }
    }

    private var login: some View {
        VStack {
            Spacer()
            EmptyStateView(text: ErrorText.loginRequired)
            LBButton(title: "登录 / 注册") { auth.showAuth = true }
                .padding(.horizontal, Spacing.pageH)
            Spacer()
        }
    }

    private var main: some View {
        VStack(spacing: 0) {
            InfoNote(text: note).padding(.horizontal, Spacing.pageH).padding(.top, 8)
            if model.isRateLimited {
                InlineBanner(text: "请 \(model.rateLimitRemainingSec) 秒后再试")
            }
            // tabs
            HStack(spacing: 0) {
                tabBtn("订单", 0)
                tabBtn("成交", 1)
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.top, 8)

            if tab == 0 { ordersPane } else { tradesPane }
        }
    }

    private func tabBtn(_ title: String, _ i: Int) -> some View {
        Button {
            tab = i
            Task { await reloadCurrent() }
        } label: {
            Text(title)
                .font(.lb(FontSize.label, tab == i ? .semibold : .regular))
                .foregroundColor(tab == i ? p.textPrimary : p.textSecondary)
                .frame(maxWidth: .infinity).frame(height: 36)
                .overlay(alignment: .bottom) {
                    Rectangle().fill(tab == i ? p.primary : Color.clear).frame(height: 2)
                }
        }
        .buttonStyle(PressableStyle())
    }

    @ViewBuilder private var ordersPane: some View {
        if let e = model.ordersError {
            InlineBanner(text: e + " · 已保留当前列表，可重试")
            Button("同页重试") {
                guard !model.isRateLimited else {
                    ToastCenter.shared.show("请 \(model.rateLimitRemainingSec) 秒后再试", .error)
                    return
                }
                Task { await model.reloadOrders(generation: currentGeneration) }
            }
            .font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
            .padding(.horizontal, Spacing.pageH)
        }
        switch model.orders {
        case .idle, .loading:
            Spacer(); ProgressView(); Spacer()
        case let .failed(m):
            ErrorStateView(message: m) {
                guard !model.isRateLimited else { return }
                Task { await model.reloadOrders(generation: currentGeneration) }
            }
        case let .loaded(pg) where pg.items.isEmpty:
            Spacer()
            EmptyStateView(text: "暂无模拟订单")
            Text("generation \(pg.generation.map(String.init) ?? "—")")
                .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            Spacer()
        case let .loaded(pg):
            ScrollView {
                LazyVStack(spacing: 0) {
                    Text("generation \(pg.generation.map(String.init) ?? "—") · 共 \(pg.total ?? pg.items.count) 条")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
                    ForEach(pg.items) { o in
                        orderRow(o)
                        Divider().overlay(p.border).padding(.horizontal, Spacing.pageH)
                    }
                    footerMore(
                        can: model.canLoadMoreOrders,
                        loading: model.loadingMoreOrders
                    ) { Task { await model.loadMoreOrders() } }
                }
            }
            .refreshable { await model.reloadOrders(generation: currentGeneration) }
        }
    }

    @ViewBuilder private var tradesPane: some View {
        if let e = model.tradesError {
            InlineBanner(text: e + " · 已保留当前列表，可重试")
            Button("同页重试") {
                guard !model.isRateLimited else {
                    ToastCenter.shared.show("请 \(model.rateLimitRemainingSec) 秒后再试", .error)
                    return
                }
                Task { await model.reloadTrades(generation: currentGeneration) }
            }
            .font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
            .padding(.horizontal, Spacing.pageH)
        }
        switch model.trades {
        case .idle, .loading:
            Spacer(); ProgressView(); Spacer()
        case let .failed(m):
            ErrorStateView(message: m) {
                guard !model.isRateLimited else { return }
                Task { await model.reloadTrades(generation: currentGeneration) }
            }
        case let .loaded(pg) where pg.items.isEmpty:
            Spacer()
            EmptyStateView(text: "暂无模拟成交")
            Spacer()
        case let .loaded(pg):
            ScrollView {
                LazyVStack(spacing: 0) {
                    Text("generation \(pg.generation.map(String.init) ?? "—") · 共 \(pg.total ?? pg.items.count) 条 · fee=0")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
                    ForEach(pg.items) { tr in
                        tradeRow(tr)
                        Divider().overlay(p.border).padding(.horizontal, Spacing.pageH)
                    }
                    footerMore(
                        can: model.canLoadMoreTrades,
                        loading: model.loadingMoreTrades
                    ) { Task { await model.loadMoreTrades() } }
                }
            }
            .refreshable { await model.reloadTrades(generation: currentGeneration) }
        }
    }

    private func orderRow(_ o: PaperOrder) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(o.symbol).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text("paper").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .padding(.horizontal, 4).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                Text(o.side == "buy" ? "买" : "卖")
                    .font(.lb(FontSize.small, .medium))
                    .foregroundColor(o.side == "buy" ? p.up : p.down)
                Spacer()
                Text(o.status).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Text("qty \(o.quantity) · filled \(o.filledQuantity) · INDEX \(o.referencePrice)")
                .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            if let q = o.quoteOrderQty, !q.isEmpty {
                Text("quote \(q)").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
            Text("\(o.orderId) · gen \(o.generation)")
                .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
        }
        .padding(.horizontal, Spacing.pageH)
        .padding(.vertical, 12)
    }

    private func tradeRow(_ tr: PaperTrade) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(tr.symbol).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text("paper").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .padding(.horizontal, 4).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                Text(tr.side == "buy" ? "买" : "卖")
                    .font(.lb(FontSize.small, .medium))
                    .foregroundColor(tr.side == "buy" ? p.up : p.down)
                Spacer()
                Text(tr.priceSource).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Text("\(tr.price) × \(tr.quantity) · fee \(tr.fee) \(tr.feeAsset)")
                .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            Text(tr.tradeId).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
        }
        .padding(.horizontal, Spacing.pageH)
        .padding(.vertical, 12)
    }

    private func footerMore(can: Bool, loading: Bool, load: @escaping () -> Void) -> some View {
        Group {
            if can {
                HStack {
                    if loading { ProgressView().scaleEffect(0.8) }
                    Text(loading ? "加载中…" : "加载更多")
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                .frame(maxWidth: .infinity).frame(height: 44)
                .onAppear { if !loading { load() } }
            } else {
                Text("没有更多了").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .frame(maxWidth: .infinity).frame(height: 44)
            }
        }
    }

    private var currentGeneration: Int64? {
        if case let .loaded(a) = model.account { return a.generation }
        return nil
    }

    private func bootstrap() async {
        applyShotTab()
        await model.reloadAccount()
        await reloadCurrent()
    }

    private func reloadCurrent() async {
        if tab == 0 {
            await model.reloadOrders(generation: currentGeneration)
        } else {
            await model.reloadTrades(generation: currentGeneration)
        }
    }

    private func applyShotTab() {
        switch PaperTradingDemo.historyKind {
        case .trades: tab = 1
        case .orders: tab = 0
        case .none: break
        }
    }
}
