import SwiftUI

/// 首页（对照图 #1–#3）
struct HomeView: View {
    @Environment(\.palette) private var p
    @StateObject private var vm = HomeViewModel()
    @StateObject private var store = MarketStore.shared
    @StateObject private var watch = Watchlist.shared
    @State private var hideAssets = false
    @State private var rankTab = 1
    @State private var rankKind = 0
    @State private var newsTab = 0
    @State private var bannerPage = 0
    @State private var earnPage = 0
    var onOpenMarkets: () -> Void = {}

    var body: some View {
        VStack(spacing: 0) {
            topBar
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    assetHeader.padding(.top, 14)
                    taskCard.padding(.top, 18)
                    redPacket.padding(.top, 12)
                    grid.padding(.top, 22)
                    sectionDivider
                    cardPromo
                    sectionDivider
                    earnStrip
                    sectionDivider
                    ranking
                    sectionDivider
                    news
                }
                .padding(.bottom, Metrics.tabBarH + 40)
            }
            .refreshable { await vm.load(); await store.load() }
        }
        .background(p.bg)
        .task { await vm.load(); if store.spot.isEmpty { await store.load() } }
    }

    // MARK: 顶栏：头像 / 搜索 / 扫码 / 客服 / 通知
    private var topBar: some View {
        HStack(spacing: 14) {
            Button { Session.shared.requireLogin { Router.shared.showUserCenter = true } } label: { IconView(icon: .avatar, size: 28) }
                .buttonStyle(PressableStyle())
            HStack(spacing: 6) {
                IconView(icon: .search, size: 15, tint: p.textTertiary)
                Text("BTC").font(.lb(FontSize.label)).foregroundColor(p.textTertiary)
                Spacer()
            }
            .padding(.horizontal, 10)
            .frame(height: Metrics.searchBarH)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
            IconView(icon: .scan, tint: p.textPrimary)
            IconView(icon: .headset, tint: p.textPrimary)
            IconView(icon: .bell, tint: p.textPrimary)
        }
        .padding(.horizontal, Spacing.pageH)
        .frame(height: 44)
    }

    // MARK: 资产
    private var assetHeader: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 4) {
                Text("总资产估值").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                IconView(icon: .eye, size: 14, tint: p.textSecondary).onTapGesture { hideAssets.toggle() }
            }
            HStack(alignment: .firstTextBaseline) {
                Text(hideAssets ? "****" : Fmt.price(vm.totalUSDT, precision: 2))
                    .font(.lbNum(FontSize.assetHero, .bold)).foregroundColor(p.textPrimary)
                HStack(spacing: 2) {
                    Text("USDT").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    IconView(icon: .caretDown, size: 10, tint: p.textPrimary)
                }
                Spacer()
                Button {} label: {
                    Text("充值").font(.lb(FontSize.label, .medium)).foregroundColor(p.textOnPrimary)
                        .frame(width: Metrics.heroButtonW, height: Metrics.heroButtonH)
                        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.primary))
                }
            }
            Text(hideAssets ? "≈ ****" : "≈ " + Fmt.fiat(vm.totalUSDT, rate: AppConfig.usdtCny))
                .font(.lbNum(FontSize.caption)).foregroundColor(p.textSecondary)
        }
        .padding(.horizontal, Spacing.pageH)
    }

    private var taskCard: some View {
        HStack(spacing: 4) {
            (Text("完成新手任务，可获得 ").foregroundColor(p.textPrimary)
             + Text("215 USDT").foregroundColor(p.brand).fontWeight(.semibold)
             + Text(" 体验金").foregroundColor(p.textPrimary))
                .font(.lb(FontSize.caption))
            Spacer()
            IconView(icon: .chevronRight, size: 14, tint: p.textSecondary)
        }
        .padding(.horizontal, 12)
        .frame(height: Metrics.taskCardH)
        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border, lineWidth: 1))
        .padding(.horizontal, Spacing.pageH)
    }

    private var redPacket: some View {
        HStack {
            VStack(alignment: .leading, spacing: 3) {
                Text("全员红包雨火爆来袭").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text("惊喜好礼等你拿").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Spacer()
            IconView(icon: .gGift, size: 40)
        }
        .padding(.horizontal, 14)
        .frame(height: Metrics.bannerH)
        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
        .padding(.horizontal, Spacing.pageH)
    }

    // MARK: 功能宫格
    private let gridItems: [(LBIcon, String)] = [(.gChat, "聊天室"), (.gGift, "福利中心"), (.gEarn, "理财"), (.gTradfi, "TradFi"),
                                                  (.gCopy, "跟单"), (.gBot, "策略交易"), (.gMore, "更多")]
    private var grid: some View {
        LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 18) {
            ForEach(gridItems, id: \.1) { icon, title in
                VStack(spacing: 6) {
                    IconView(icon: icon, size: Metrics.gridIconGlyph)
                        .frame(width: Metrics.gridIcon, height: Metrics.gridIcon)
                        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.iconTileBg))
                    Text(title).font(.lb(FontSize.caption)).foregroundColor(p.textPrimary)
                }
            }
        }
        .padding(.horizontal, Spacing.pageH)
        .padding(.bottom, 20)
    }

    private var sectionDivider: some View { Rectangle().fill(p.fill).frame(height: 8) }

    // MARK: 推广轮播（Linbit Card）
    private var cardPromo: some View {
        HStack {
            VStack(alignment: .leading, spacing: 6) {
                Text("数字资产 · 全球消费").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Text("Linbit Card").font(.lb(20, .bold)).foregroundColor(p.textPrimary)
                HStack(spacing: 2) {
                    Text("查看详情").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                    IconView(icon: .arrowRight, size: 12, tint: p.textPrimary)
                }
            }
            Spacer()
            ZStack {
                RoundedRectangle(cornerRadius: 6).fill(p.brand).frame(width: 74, height: 46).rotationEffect(.degrees(-12)).offset(x: -10, y: 4)
                RoundedRectangle(cornerRadius: 6).fill(p.primary).frame(width: 74, height: 46)
                    .overlay(IconView(icon: .logo, size: 16).padding(6), alignment: .topLeading)
                    .rotationEffect(.degrees(8)).offset(x: 8)
            }
            .frame(width: 110, height: 70)
        }
        .overlay(Text("\(bannerPage + 1)/\(max(vm.banners.count, 1))").font(.lbNum(10)).foregroundColor(p.textTertiary), alignment: .bottomTrailing)
        .padding(Spacing.pageH)
        .frame(height: 104)
    }

    // MARK: 理财宝
    private var earnStrip: some View {
        HStack(spacing: 8) {
            Text("理财宝").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
            if let e = vm.earn[safe: earnPage] {
                CoinIcon(base: e.asset ?? "USDT", size: 16)
                Text("\(e.title ?? e.asset ?? "")，\(Fmt.percent(Dec.parse(e.aprMax ?? e.apr) * 100).trimmingCharacters(in: CharacterSet(charactersIn: "+")))APR")
                    .font(.lb(FontSize.caption)).foregroundColor(p.textSecondary).lineLimit(1)
            }
            Spacer()
            Text("\(earnPage + 1)/\(max(vm.earn.count, 1))").font(.lbNum(10)).foregroundColor(p.textTertiary)
        }
        .padding(.horizontal, Spacing.pageH).frame(height: 48)
        .onReceive(Timer.publish(every: 4, on: .main, in: .common).autoconnect()) { _ in
            if !vm.earn.isEmpty { earnPage = (earnPage + 1) % vm.earn.count }
        }
    }

    // MARK: 榜单
    private var ranking: some View {
        VStack(alignment: .leading, spacing: 0) {
            PrimaryTabs(items: ["自选", "热门", "涨幅榜", "跌幅榜", "成交额榜"], selected: $rankTab, size: 16)
                .padding(.horizontal, Spacing.pageH).padding(.top, 16)
            ChipBar(items: ["现货", "合约"], selected: $rankKind).padding(.vertical, 12)
            HStack {
                Text("交易对"); Spacer(); Text("最新价")
                Text("涨跌幅").frame(width: Metrics.changeBadgeW + 12, alignment: .trailing)
            }
            .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            .padding(.horizontal, Spacing.pageH).frame(height: 24)
            let rows = vm.ranking(rankTab, kind: rankKind == 0 ? .spot : .futures, store: store, watch: watch)
            if rows.isEmpty { EmptyStateView(text: rankTab == 0 ? "暂无自选" : "暂无数据").frame(height: 160) }
            ForEach(rows) { MarketRowView(row: $0, showIcon: false, compactPair: true) }
            moreLink(onOpenMarkets)
        }
    }

    // MARK: 公告 / 快讯 / 学院
    private var news: some View {
        VStack(alignment: .leading, spacing: 0) {
            PrimaryTabs(items: ["公告", "快讯", "学院"], selected: $newsTab, size: 16)
                .padding(.horizontal, Spacing.pageH).padding(.vertical, 16)
            if newsTab == 0 {
                if vm.announcements.isEmpty {
                    EmptyStateView(text: vm.announcementNote ?? "暂无公告").frame(height: 140)
                }
                ForEach(Array(vm.announcements.prefix(5).enumerated()), id: \.offset) { _, a in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(a.title ?? "").font(.lb(FontSize.body)).foregroundColor(p.textPrimary).lineLimit(2)
                        Text(a.publishAt.map { Fmt.dateTime($0) } ?? "")
                            .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                    }
                    .padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
            } else {
                EmptyStateView(text: newsTab == 1 ? "快讯即将上线" : "学院即将上线").frame(height: 140)
            }
            moreLink {}
        }
    }

    private func moreLink(_ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 2) {
                Text("查看更多").font(.lb(FontSize.caption))
                IconView(icon: .arrowRight, size: 12, tint: p.textSecondary)
            }
            .foregroundColor(p.textSecondary).frame(maxWidth: .infinity).frame(height: 44)
        }
    }
}

extension Array {
    subscript(safe i: Int) -> Element? { indices.contains(i) ? self[i] : nil }
}
