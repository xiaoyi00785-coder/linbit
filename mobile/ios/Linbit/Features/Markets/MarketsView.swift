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

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider().overlay(p.divider)
            switch top {
            case 0: WatchlistTab()
            case 1: marketTab
            default: PlaceholderView(title: "聊天室", note: "聊天室后端暂未提供，敬请期待")
            }
        }
        .background(p.bg)
        .task { if store.spot.isEmpty { await store.load() } }
    }

    private var header: some View {
        HStack {
            PrimaryTabs(items: ["自选", "市场", "聊天室"], selected: $top)
            Spacer()
            IconView(icon: .search, size: 22, tint: p.textPrimary)
            ZStack(alignment: .topTrailing) {
                IconView(icon: .more, size: 22, tint: p.textPrimary)
                Circle().fill(p.badgeDot).frame(width: 6, height: 6).offset(x: 3, y: -4)
            }
            .padding(.leading, 18)
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
                PlaceholderView(title: "TradFi", note: "TradFi 后端暂未提供")
            } else {
                ChipBar(items: chipItems, selected: $category).padding(.vertical, 10)
                MarketListHeader(field: $field, dir: $dir)
                let rows = store.filtered(kind == 0 ? .spot : .futures, categoryIndex: category).sorted(by: field, dir)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if rows.isEmpty {
                            if store.loading { ProgressView().padding(40) } else { EmptyStateView() }
                        }
                        ForEach(rows) { MarketRowView(row: $0) }
                    }
                    .padding(.bottom, Metrics.tabBarH + 30)
                }
                .refreshable { await store.load() }
            }
        }
    }
}

/// 自选：空时显示推荐（#7：两列卡片 + 勾选 + 黑色「加入自选」）
struct WatchlistTab: View {
    @Environment(\.palette) private var p
    @StateObject private var store = MarketStore.shared
    @StateObject private var watch = Watchlist.shared
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
            let all = (store.spot + store.futures).filter { watch.contains($0.symbol) }
            if watch.symbols.isEmpty { recommend } else {
                MarketListHeader(field: $field, dir: $dir).padding(.top, 8)
                ScrollView { LazyVStack(spacing: 0) { ForEach(all.sorted(by: field, dir)) { MarketRowView(row: $0) } } }
            }
        }
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
