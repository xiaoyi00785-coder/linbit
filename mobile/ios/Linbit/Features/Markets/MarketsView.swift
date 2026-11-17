import SwiftUI

/// 行情页（对照图 #7 自选空态推荐、#8/#9 市场-现货）
struct MarketsView: View {
    @Environment(\.palette) private var p
    @StateObject private var store = MarketStore.shared
    @StateObject private var watch = Watchlist.shared
    @State private var top = 1          // 0 自选 1 市场 2 聊天室
    @State private var kind = 0         // 现货 / 合约 / TradFi
    @State private var category = 0
    @State private var field: SortField = .volume
    @State private var dir: SortDir = .none
    @State private var searching = false
    @State private var query = ""
    @State private var showMore = false
    @State private var morePage = 0
    @State private var showWidget = false
    @AppStorage("lb.market.logo") private var showLogo = true
    @AppStorage("lb.market.moreSeen") private var moreSeen = false
    @ObservedObject private var router = Router.shared

    var body: some View {
        VStack(spacing: 0) {
            if searching {
                MarketSearchPage(query: $query) { searching = false; query = "" }
            } else {
                header
                Divider().overlay(p.divider)
                switch top {
                case 0: WatchlistTab(query: query)
                case 1: marketTab
                default: ChatDirectoryView(embedded: true)
                }
            }
        }
        .background(p.bg)
        .task { if store.spot.isEmpty { await store.load() } }
        .onChange(of: router.marketSearch) { on in
            if on { searching = true; router.marketSearch = false }
        }
        .lbSheet($showMore) {
            if morePage == 0 {
                LBSheet(title: "更多", showClose: true, onClose: { showMore = false }) {
                    VStack(spacing: 0) {
                        SheetOptionRow(title: "图标设置", selected: false) { morePage = 1 }
                        SheetOptionRow(title: "小组件", selected: false) { showMore = false; showWidget = true }
                    }.padding(.bottom, 12)
                }
            } else {
                LBSheet(title: "图标设置", showClose: true, onClose: { morePage = 0 }) {
                    VStack(spacing: 0) {
                        SheetOptionRow(title: "有 Logo", selected: showLogo) { showLogo = true }
                        SheetOptionRow(title: "无 Logo", selected: !showLogo) { showLogo = false }
                    }.padding(.bottom, 12)
                }
            }
        }
        .onChange(of: showMore) { if !$0 { morePage = 0 } }
        .fullScreenCover(isPresented: $showWidget) { WidgetBoardView() }
    }

    private var header: some View {
        HStack {
            PrimaryTabs(items: ["自选", "市场", "聊天室"], selected: $top)
            Spacer()
            Button { searching = true } label: {
                IconView(icon: .search, size: 22, tint: p.textPrimary).frame(width: 32, height: 32)
            }.buttonStyle(.plain).accessibilityLabel("搜索")
            Button { moreSeen = true; showMore = true } label: {
                ZStack(alignment: .topTrailing) {
                    IconView(icon: .more, size: 22, tint: p.textPrimary)
                    if !moreSeen { Circle().fill(p.badgeDot).frame(width: 6, height: 6).offset(x: 3, y: -4) }
                }.frame(width: 32, height: 32)
            }.buttonStyle(.plain).accessibilityLabel("图标设置").padding(.leading, 8)
        }
        .padding(.horizontal, Spacing.pageH)
        .frame(height: 44)
    }

    private var chipItems: [String] {
        ["全部"] + (store.categories.isEmpty ? ["公链", "股票代币", "Meme", "DeFi", "Solana 生态", "Layer 2"] : store.categories.map(\.name))
    }

    private var marketTab: some View {
        VStack(spacing: 0) {
            HStack {
                PrimaryTabs(items: ["现货", "合约", "TradFi"], selected: $kind, size: FontSize.tabSecondary, underline: true)
                Spacer()
            }
            .padding(.horizontal, Spacing.pageH).padding(.top, 10)
            Divider().overlay(p.divider)
            if kind == 2 {
                TradFiView()
            } else {
                ChipBar(items: chipItems, selected: $category).padding(.vertical, 10)
                MarketListHeader(field: $field, dir: $dir)
                let rows = store.filtered(kind == 0 ? .spot : .futures, categoryIndex: category)
                    .filter { MarketQuery.match($0, query) }
                    .sorted(by: field, dir)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if rows.isEmpty {
                            if store.loading { ProgressView().padding(40) } else { EmptyStateView() }
                        }
                        ForEach(rows) { MarketRowView(row: $0, marketFutures: kind == 1) }
                    }
                    .padding(.bottom, Metrics.tabBarH + 30)
                }
                .refreshable { await store.load() }
            }
        }
    }
}

/// 自选：空时显示推荐（#7：两列卡片 + 勾选 + 黑色「加入自选」）
enum MarketQuery {
    static func match(_ row: MarketRow, _ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else { return true }
        return row.symbol.localizedCaseInsensitiveContains(q) || row.base.localizedCaseInsensitiveContains(q)
    }
}

struct WatchlistTab: View {
    @Environment(\.palette) private var p
    @StateObject private var store = MarketStore.shared
    @StateObject private var watch = Watchlist.shared
    var query = ""
    @State private var kind = 0
    @State private var picked: Set<String> = []
    @State private var field: SortField = .volume
    @State private var dir: SortDir = .none

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                PrimaryTabs(items: ["全部", "现货", "合约", "TradFi"], selected: $kind, size: FontSize.label, underline: true)
                Spacer()
            }
            .padding(.horizontal, Spacing.pageH).padding(.top, 10)
            let pool = filteredPool
            let all = pool.filter { watch.contains($0.symbol) && MarketQuery.match($0, query) }
            if watch.symbols.isEmpty && kind == 0 { recommend } else {
                MarketListHeader(field: $field, dir: $dir).padding(.top, 8)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if all.isEmpty { EmptyStateView(text: "暂无自选") }
                        ForEach(all.sorted(by: field, dir)) { row in
                            MarketRowView(row: row, marketFutures: kind == 2 || ((kind == 0 || kind == 3) && !store.spot.contains { $0.symbol == row.symbol } && store.futures.contains { $0.symbol == row.symbol }))
                        }
                    }
                }
            }
        }
        .task { await loadTradFi() }
    }

    @State private var tradfiBases: Set<String> = []

    private var filteredPool: [MarketRow] {
        switch kind {
        case 1: return store.spot
        case 2: return store.futures
        case 3: return (store.spot + store.futures).filter { tradfiBases.contains($0.base) }
        default: return store.spot + store.futures
        }
    }

    private func loadTradFi() async {
        guard let items = try? await TradFiAPI.assets(nil).items else { return }
        tradfiBases = Set(items.compactMap { $0["asset"].s })
    }

    private var recommend: some View {
        let recs = Array((store.spot + store.futures).prefix(10))
        return VStack(spacing: Spacing.lg) {
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible())], spacing: 10) {
                ForEach(recs, id: \.symbol) { r in
                    HStack {
                        Text(r.symbol).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                        Spacer()
                        Image(systemName: picked.contains(r.symbol) ? "checkmark.square.fill" : "square")
                            .foregroundColor(p.textPrimary)
                    }
                    .padding(.horizontal, 10).frame(height: 52)
                    .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
                    .onTapGesture { if picked.contains(r.symbol) { picked.remove(r.symbol) } else { picked.insert(r.symbol) } }
                }
            }
            Button { watch.add(Array(picked)) } label: {
                Text("加入自选").font(.lb(FontSize.body, .medium)).foregroundColor(p.textOnPrimary)
                    .frame(maxWidth: .infinity).frame(height: 44)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.primary))
            }
            Spacer()
        }
        .padding(Spacing.pageH)
        .onAppear { picked = Set(recs.map(\.symbol)) }
    }
}

/// 行情搜索：热门来自当前成交额，结果是真实行情。取消回到列表。
struct MarketSearchPage: View {
    @Environment(\.palette) private var p
    @StateObject private var store = MarketStore.shared
    @Binding var query: String
    let onCancel: () -> Void
    @State private var kind = 0
    @State private var tradfiBases: Set<String> = []
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass").font(.system(size: 14)).foregroundColor(p.textSecondary)
                    TextField("搜索交易对", text: $query)
                        .font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                        .focused($focused).autocorrectionDisabled().textInputAutocapitalization(.never)
                    if !query.isEmpty {
                        Button { query = "" } label: { Image(systemName: "xmark.circle.fill").foregroundColor(p.textTertiary) }
                    }
                }
                .padding(.horizontal, 12).frame(height: 36).background(Capsule().fill(p.fill))
                Button("取消", action: onCancel).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
            }
            .padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
            ChipBar(items: ["现货", "合约", "TradFi"], selected: $kind).padding(.vertical, 8)
            ScrollView {
                LazyVStack(spacing: 0) {
                    if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        Text("热门搜索").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, Spacing.pageH).padding(.bottom, 4)
                    }
                    if rows.isEmpty { EmptyStateView(text: store.loading ? "加载中" : "无匹配交易对") }
                    ForEach(rows) { MarketRowView(row: $0, showsStar: true, marketFutures: kind == 1) }
                }.padding(.bottom, Metrics.tabBarH + 30)
            }
        }
        .onAppear { focused = true }
        .task { await loadTradFi() }
    }

    private var pool: [MarketRow] {
        switch kind {
        case 1: return store.futures
        case 2: return (store.spot + store.futures).filter { tradfiBases.contains($0.base) }
        default: return store.spot
        }
    }

    private var rows: [MarketRow] {
        let matched = pool.filter { MarketQuery.match($0, query) }.sorted(by: .volume, .desc)
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return Array(matched.prefix(8)) }
        return matched
    }

    private func loadTradFi() async {
        guard let items = try? await TradFiAPI.assets(nil).items else { return }
        tradfiBases = Set(items.compactMap { $0["asset"].s })
    }
}

/// 行情小组件。卡片数据来自 GET /market/widget，所选交易对保存在本机并作为 symbols 传给接口。
struct WidgetBoardView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @StateObject private var store = MarketStore.shared
    @AppStorage("lb.widget.symbols") private var saved = "BTCUSDT,ETHUSDT"
    @State private var items: [WidgetQuote] = []
    @State private var error: String?

    private var symbols: [String] {
        saved.split(separator: ",").map { String($0) }.filter { !$0.isEmpty }
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "小组件", onBack: { dismiss() })
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    VStack(spacing: 0) {
                        if items.isEmpty { Text("暂无行情").font(.lb(FontSize.small)).foregroundColor(p.textSecondary).frame(maxWidth: .infinity).padding(20) }
                        ForEach(items) { q in
                            HStack {
                                Text(q.symbol).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                                Spacer()
                                Text(q.lastPrice).font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                                Text(Fmt.percent(Dec.parse(q.priceChangePercent))).font(.lbNum(FontSize.small))
                                    .foregroundColor(Dec.parse(q.priceChangePercent) < 0 ? p.down : p.up)
                                    .frame(width: 64, alignment: .trailing)
                            }.padding(.horizontal, 16).frame(height: 48)
                        }
                    }
                    .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                    Text("最多 4 个交易对。下面的选择会请求 /market/widget。")
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    let choices = Array(store.spot.prefix(16))
                    LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                        ForEach(choices) { row in
                            let on = symbols.contains(row.symbol)
                            Text(row.base).font(.lb(FontSize.small, on ? .semibold : .regular)).foregroundColor(p.textPrimary)
                                .frame(maxWidth: .infinity).frame(height: 36)
                                .background(RoundedRectangle(cornerRadius: Radius.md).stroke(on ? p.textPrimary : p.border))
                                .onTapGesture { toggle(row.symbol) }
                        }
                    }
                }.padding(Spacing.pageH)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { if store.spot.isEmpty { await store.load() }; await load() }
        .onChange(of: saved) { _ in Task { await load() } }
    }

    private func toggle(_ symbol: String) {
        var next = symbols
        if let i = next.firstIndex(of: symbol) { next.remove(at: i) }
        else if next.count >= 4 { ToastCenter.shared.show("最多 4 个"); return }
        else { next.append(symbol) }
        saved = next.joined(separator: ",")
    }

    private func load() async {
        let q = symbols.joined(separator: ",")
        do {
            let r: WidgetResp = try await APIClient.shared.get("/market/widget", query: ["symbols": q.isEmpty ? nil : q])
            items = r.items
            error = nil
        } catch { self.error = error.lbMessage }
    }
}

private struct WidgetQuote: Decodable, Identifiable {
    var symbol: String
    var lastPrice: String
    var priceChangePercent: String
    var id: String { symbol }
}
private struct WidgetResp: Decodable { var items: [WidgetQuote] }
