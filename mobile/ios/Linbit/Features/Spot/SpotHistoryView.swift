import SwiftUI
import UIKit

/// 历史记录（对照图 #23）：当前委托 / 历史委托 / 成交明细；筛选「币对 ▾」「委托类型 ▾」；全部撤销
struct SpotHistoryView: View {
    @Environment(\.palette) private var p
    @State private var tab = Router.shared.shotSheet == "spotHistoryCond" ? 3 : (Router.shared.shotSheet == "spotHistoryAmount" ? 1 : 0)
    @State private var symbol: String? = nil
    @State private var type: APIOrderType? = nil
    @State private var open: Loadable<[APIOrder]> = .loading
    @State private var orders: [APIOrder] = []
    @State private var trades: [APITrade] = []
    @State private var page = 1
    @State private var hasMore = true
    @State private var state: Loadable<Void> = .loading
    @State private var loadingMore = false
    @State private var showPair = false
    @State private var showType = false
    @State private var confirm: ConfirmRequest?
    @State private var detail: APIOrder?

    private let tabs = ["当前委托", "历史委托", "成交明细", "条件单"]

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "历史记录") { EmptyView() }
            PrimaryTabs(items: tabs, selected: $tab, size: FontSize.tabSecondary, underline: true)
                .padding(.horizontal, Spacing.pageH).frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 16) {
                chip(symbol.map { $0.replacingOccurrences(of: "USDT", with: "/USDT") } ?? "币对") { showPair = true }
                if tab < 2 { chip(type.map { $0 == .limit ? "限价单" : "市价单" } ?? "委托类型") { showType = true } }
                Spacer()
                if tab == 0 {
                    let n = filteredOpen.count
                    Button {
                        guard n > 0 else { return }
                        confirm = .init(title: "全部撤销", message: "确定撤销当前筛选下的 \(n) 笔委托吗？", confirmTitle: "全部撤销", destructive: true) { Task { await cancelAll() } }
                    } label: {
                        Text("全部撤销").font(.lb(FontSize.small)).foregroundColor(n == 0 ? p.textTertiary : p.textPrimary)
                            .padding(.horizontal, 10).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                    }.buttonStyle(PressableStyle())
                }
            }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
            if tab == 3 { SpotConditionalList(symbol: symbol).id(symbol ?? "all") } else { content }
        }
        .background(p.bg.ignoresSafeArea())
        .navigationBarHidden(true)
        .task(id: "\(tab)-\(symbol ?? "")-\(type?.rawValue ?? "")") { await reload() }
        .lbSheet($showPair) { PairPickerSheet(current: symbol ?? "") { s in symbol = s; showPair = false } onClose: { showPair = false } }
        .lbSheet($showType) {
            LBSheet(title: "委托类型", onClose: { showType = false }) {
                VStack(spacing: 0) {
                    SheetOptionRow(title: "全部", selected: type == nil) { type = nil; showType = false }
                    SheetOptionRow(title: "限价单", selected: type == .limit) { type = .limit; showType = false }
                    SheetOptionRow(title: "市价单", selected: type == .market) { type = .market; showType = false }
                }.padding(.bottom, 20)
            }
        }
        .lbSheet(Binding(get: { detail != nil }, set: { if !$0 { detail = nil } })) {
            if let o = detail { OrderDetailSheet(o: o) { detail = nil } }
        }
        .lbConfirm($confirm)
    }

    private func chip(_ t: String, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            HStack(spacing: 3) { Text(t); Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)) }
                .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
        }
    }

    private var filteredOpen: [APIOrder] {
        (open.value ?? []).filter { (symbol == nil || $0.symbol == symbol) && (type == nil || $0.type == type) }
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .idle, .loading: SkeletonList().padding(.horizontal, Spacing.pageH)
        case .failed(let e): ErrorStateView(message: e) { Task { await reload() } }.frame(maxHeight: .infinity)
        case .loaded(_):
            let empty = tab == 0 ? filteredOpen.isEmpty : tab == 1 ? orders.isEmpty : trades.isEmpty
            if empty {
                VStack(spacing: 10) { EmptyIllustration(); Text("暂无数据").font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                    .frame(maxHeight: .infinity).offset(y: -60)
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if tab == 0 {
                            ForEach(filteredOpen, id: \.orderId) { o in
                                OpenOrderRow(o: o) {
                                    confirm = .init(title: "撤销委托", message: "确定撤销该笔委托吗？", confirmTitle: "撤销", destructive: true) {
                                        Task { do { _ = try await SpotAPI.cancel(o.orderId); ToastCenter.shared.show("撤单成功", .success); await reload() } catch { ToastCenter.shared.error(error) } }
                                    }
                                }
                                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                            }
                        } else if tab == 1 {
                            ForEach(orders, id: \.orderId) { o in
                                Button { detail = o } label: { HistoryOrderRow(o: o) }.buttonStyle(RowPressStyle())
                                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                            }
                        } else {
                            ForEach(trades, id: \.tradeId) { t in
                                TradeRow(t: t)
                                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                            }
                        }
                        if tab != 0 {
                            Group {
                                if hasMore { ProgressView().onAppear { Task { await more() } } }
                                else { Text("没有更多了").font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                            }.frame(height: 44)
                        }
                    }
                }.refreshable { await reload() }
            }
        }
    }

    private func reload() async {
        guard tab != 3 else { return }
        state = .loading; page = 1; hasMore = true
        if Router.shared.shotSheet == "spotHistoryAmount", tab == 1 {
            orders = APIOrder.amountDemo; hasMore = false; state = .loaded(()); return
        }
        do {
            switch tab {
            case 0: open = .loaded(try await SpotAPI.open(symbol: symbol))
            case 1:
                let r = try await SpotAPI.history(symbol: symbol, status: nil, page: 1)
                orders = r.items.filter { type == nil || $0.type == type }; hasMore = r.page * r.pageSize < r.total
            default:
                let r = try await SpotAPI.trades(symbol: symbol, page: 1)
                trades = r.items; hasMore = r.page * r.pageSize < r.total
            }
            state = .loaded(())
        } catch { state = .failed(error.lbMessage) }
    }
    private func more() async {
        guard !loadingMore, hasMore else { return }
        loadingMore = true; defer { loadingMore = false }
        do {
            if tab == 1 {
                let r = try await SpotAPI.history(symbol: symbol, status: nil, page: page + 1)
                orders += r.items.filter { type == nil || $0.type == type }; page = r.page; hasMore = r.page * r.pageSize < r.total
            } else {
                let r = try await SpotAPI.trades(symbol: symbol, page: page + 1)
                trades += r.items; page = r.page; hasMore = r.page * r.pageSize < r.total
            }
        } catch { hasMore = false; ToastCenter.shared.error(error) }
    }
    private func cancelAll() async {
        for o in filteredOpen { _ = try? await SpotAPI.cancel(o.orderId) }
        ToastCenter.shared.show("已全部撤销", .success); await reload()
    }
}

extension APIOrderStatus {
    var title: String {
        switch self { case .new: "未成交"; case .partiallyFilled: "部分成交"; case .filled: "已成交"; case .canceled: "已撤销"; case .rejected: "已拒绝" }
    }
}

struct HistoryOrderRow: View {
    @Environment(\.palette) private var p
    let o: APIOrder
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(o.side == .buy ? "买入" : "卖出").font(.lb(FontSize.small, .medium)).foregroundColor(o.side == .buy ? p.up : p.down)
                Text(o.symbol.replacingOccurrences(of: "USDT", with: "/USDT")).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                Text(o.type == .limit ? "限价" : "市价").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                Text(o.status.title).font(.lb(FontSize.small)).foregroundColor(o.status == .filled ? p.textPrimary : p.textSecondary)
                Image(systemName: "chevron.right").font(.system(size: 10)).foregroundColor(p.textTertiary)
            }
            Text(Fmt.dateTime(o.createdAt)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            HStack {
                kv("委托价", o.price ?? "市价"); kv("委托数量", o.qtyText); kv("已成交", o.filledQuantity, end: true)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12).contentShape(Rectangle())
    }
    private func kv(_ k: String, _ v: String, end: Bool = false) -> some View {
        VStack(alignment: end ? .trailing : .leading, spacing: 3) {
            Text(k).font(.lb(11)).foregroundColor(p.textSecondary)
            Text(v).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
        }.frame(maxWidth: .infinity, alignment: end ? .trailing : .leading)
    }
}

struct TradeRow: View {
    @Environment(\.palette) private var p
    let t: APITrade
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(t.side == .buy ? "买入" : "卖出").font(.lb(FontSize.small, .medium)).foregroundColor(t.side == .buy ? p.up : p.down)
                Text(t.symbol.replacingOccurrences(of: "USDT", with: "/USDT")).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                if let r = t.role { Text(r.rawValue == "maker" ? "Maker" : "Taker").font(.lb(11)).foregroundColor(p.textSecondary) }
                Spacer()
                Text(Fmt.dateTime(t.createdAt)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
            HStack {
                kv("成交价", t.price); kv("成交量", t.quantity)
                kv("手续费", "\(t.fee ?? "0") \(t.feeAsset ?? "")", end: true)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private func kv(_ k: String, _ v: String, end: Bool = false) -> some View {
        VStack(alignment: end ? .trailing : .leading, spacing: 3) {
            Text(k).font(.lb(11)).foregroundColor(p.textSecondary)
            Text(v).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
        }.frame(maxWidth: .infinity, alignment: end ? .trailing : .leading)
    }
}

struct OrderDetailSheet: View {
    @Environment(\.palette) private var p
    let o: APIOrder
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "委托详情", onClose: onClose) {
            VStack(spacing: 12) {
                kv("交易对", o.symbol.replacingOccurrences(of: "USDT", with: "/USDT"))
                kv("方向", o.side == .buy ? "买入" : "卖出", c: o.side == .buy ? p.up : p.down)
                kv("类型", o.type == .limit ? "限价单" : "市价单")
                kv("委托价", o.price ?? "市价"); kv("委托数量", o.qtyText); kv("已成交", o.filledQuantity)
                kv("状态", o.status.title); kv("时间", Fmt.dateTime(o.createdAt))
                HStack {
                    Text("订单号").font(.lb(FontSize.label)).foregroundColor(p.textSecondary); Spacer()
                    Text(o.orderId).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary).lineLimit(1)
                    Button { UIPasteboard.general.string = o.orderId; ToastCenter.shared.show("已复制", .success) } label: {
                        Image(systemName: "doc.on.doc").font(.system(size: 12)).foregroundColor(p.textSecondary)
                    }
                }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
        }
    }
    private func kv(_ k: String, _ v: String, c: Color? = nil) -> some View {
        HStack { Text(k).font(.lb(FontSize.label)).foregroundColor(p.textSecondary); Spacer(); Text(v).font(.lbNum(FontSize.label)).foregroundColor(c ?? p.textPrimary) }
    }
}
