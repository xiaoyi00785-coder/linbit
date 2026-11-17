import SwiftUI
import AVFoundation

/// 首页（对照图 #1–#3）
struct HomeView: View {
    @Environment(\.palette) private var p
    @StateObject private var vm = HomeViewModel()
    @StateObject private var store = MarketStore.shared
    @StateObject private var watch = Watchlist.shared
    @ObservedObject private var session = Session.shared
    @State private var hideAssets = false
    @ObservedObject private var notif = NotifCenter.shared
    @State private var rankTab = 1
    @State private var rankKind = 1  // Android ref/2：默认选中「合约」
    @State private var bannerPage = 0
    @State private var earnPage = 0
    @State private var moreSheet = false
    @State private var scanning = false
    @State private var article: Article?
    @State private var showAllAnnouncements = false
    @State private var annTab = 0
    @Environment(\.displayScale) private var displayScale
    var onOpenMarkets: () -> Void = {}

    var body: some View {
        VStack(spacing: 0) {
            topBar
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    if session.isLoggedIn {
                        assetHeader.padding(.top, 14)
                        taskCard.padding(.top, 18)
                        redPacket.padding(.top, 12)
                    } else {
                        guestHero.padding(.top, 28)
                    }
                    grid.padding(.top, 17)
                    hairline
                    cardPromo
                    hairline
                    earnStrip
                    hairline
                    ranking
                    announcements
                }
                .padding(.bottom, Metrics.tabBarH + 40)
            }
            .refreshable { await vm.load(); await store.load() }
        }
        .background(p.bg)
        .task { await NotifCenter.shared.refresh() }
        .task { await vm.load(); if store.spot.isEmpty { await store.load() } }
        .lbSheet($moreSheet) { HomeMoreSheet { moreSheet = false } }
        .fullScreenCover(isPresented: $scanning) {
            CodeScanner(onCode: { code in scanning = false; handleScan(code) }, onClose: { scanning = false })
        }
        .sheet(item: $article) { item in AnnouncementBody(item: item.item) }
        .sheet(isPresented: $showAllAnnouncements) { announcementList }
    }

    // MARK: 顶栏：头像 / 搜索 / 扫码 / 客服 / 通知
    private var topBar: some View {
        HStack(spacing: 14) {
            Button { Session.shared.requireLogin { Router.shared.showUserCenter = true } } label: {
                if session.account != nil || SecShot.raw == "sec:home" {
                    AvatarView(url: session.account?.avatarUrl, name: session.account?.nickname ?? "lb_Trader", size: 28,
                               local: SecShot.raw == "sec:home" ? AvatarDemo.image(120) : nil)
                } else { IconView(icon: .logo, size: 28) }
            }
                .buttonStyle(PressableStyle())
            Button { Router.shared.marketSearch = true; Router.shared.tab = .markets } label: {
                HStack(spacing: 6) {
                    IconView(icon: .search, size: 15, tint: p.textTertiary)
                    Text(vm.searchHint ?? "BTC").font(.lb(FontSize.label)).foregroundColor(p.textTertiary)
                    Spacer()
                }
                .padding(.horizontal, 10)
                .frame(height: Metrics.searchBarH)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
            }.buttonStyle(PressableStyle())
            Button { scanning = true } label: { IconView(icon: .scan, tint: p.textPrimary) }.buttonStyle(PressableStyle()).accessibilityLabel("扫一扫")
            Button { Session.shared.requireLogin { Router.shared.openFeature(.support) } } label: { IconView(icon: .headset, tint: p.textPrimary) }.buttonStyle(PressableStyle())
            if session.isLoggedIn || NotifShot.state != nil {
            Button { Router.shared.openFeature(.notifications) } label: {
                IconView(icon: .bell, tint: p.textPrimary)
                    .overlay(alignment: .topTrailing) { if notif.unread > 0 { Badge(n: notif.unread).offset(x: 8, y: -6) } }
            }.buttonStyle(.plain).accessibilityLabel("消息通知")
            }
        }
        .padding(.horizontal, Spacing.pageH)
        .frame(height: 44)
    }

    /// 未登录首页：注册引导，不展示资产和充值（对照 EasiCoin 退出登录后的首页）。
    private var guestHero: some View {
        VStack(spacing: 22) {
            GuestCoinField()
                .frame(height: 188)
                .padding(.bottom, 4)
            Text("立即注册，最高可获得 15,200 USDT")
                .font(.lb(28, .bold)).foregroundColor(p.textPrimary)
                .multilineTextAlignment(.center).frame(maxWidth: .infinity)
            Button { session.present(.login) } label: {
                Text("登录/注册").font(.lb(FontSize.label, .medium)).foregroundColor(p.textOnPrimary)
                    .frame(maxWidth: .infinity).frame(height: 48)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.primary))
            }.buttonStyle(PressableStyle())
        }
        .padding(.horizontal, Spacing.pageH)
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
                Button { Router.shared.open(.deposit(nil)) } label: {
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
            VStack(alignment: .leading, spacing: 2) {
                (Text(newcomerLead).foregroundColor(p.textPrimary)
                 + Text("\(vm.newcomer?.total ?? "215") \(vm.newcomer?.asset ?? "USDT")").foregroundColor(p.brand).fontWeight(.semibold)
                 + Text(" 体验金").foregroundColor(p.textPrimary))
                    .font(.lb(FontSize.caption))
                if let deadline = vm.newcomer?.deadline {
                    CountdownText(deadline: deadline).font(.lbNum(10)).foregroundColor(p.textTertiary)
                }
            }
            Spacer()
            IconView(icon: .chevronRight, size: 14, tint: p.textSecondary)
        }
        .contentShape(Rectangle())
        .onTapGesture { Haptics.tap(); Router.shared.openFeature(.welfare) }
        .padding(.horizontal, 12)
        .frame(height: Metrics.taskCardH)
        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border, lineWidth: 1))
        .padding(.horizontal, Spacing.pageH)
    }

    private var newcomerLead: String {
        guard let n = vm.newcomer, n.available == true else { return "完成新手任务，可获得 " }
        if let c = n.completed, let all = n.tasks?.count, all > 0, c > 0 { return "新手任务 \(c)/\(all)，可获得 " }
        return "完成新手任务，可获得 "
    }

    private var redPacket: some View {
        HStack {
            VStack(alignment: .leading, spacing: 3) {
                Text(vm.promo?.title ?? "全员红包雨火爆来袭").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text(vm.promo?.subtitle ?? "每日登入领红包，各种福利领不停").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Spacer()
            IconView(icon: .gGift, size: 40)
        }
        .padding(.horizontal, 14)
        .frame(height: 62)
        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
        .contentShape(Rectangle())
        .onTapGesture { Haptics.tap(); HomeLink.open(vm.promo?.linkUrl, fallback: .welfare) }
        .padding(.horizontal, Spacing.pageH)
    }

    // MARK: 功能宫格
    private let gridItems: [(LBIcon, String)] = [(.gChat, "聊天室"), (.gGift, "福利中心"), (.gEarn, "理财"), (.gTradfi, "TradFi"),
                                                  (.gCopy, "跟单"), (.gBot, "策略交易"), (.gMore, "更多")]
    private var grid: some View {
        // 对照图 #1：4 列等分整屏宽（列距 97.5pt，无左右内边距），图标块 40pt，标签 11pt，两行图标块顶对顶 88pt
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 0), count: 4), spacing: 27) {
            ForEach(gridItems, id: \.1) { icon, title in
                VStack(spacing: 7) {
                    IconView(icon: icon, size: Metrics.gridIconGlyph)
                        .frame(width: Metrics.gridIcon, height: Metrics.gridIcon)
                        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.iconTileBg))
                    Text(title).font(.lb(11)).foregroundColor(p.textPrimary).frame(height: 14)
                }
                .contentShape(Rectangle())
                .onTapGesture { Haptics.tap(); gridTap(title) }
            }
        }
        .padding(.bottom, 19)
    }

    private func gridTap(_ title: String) {
        switch title {
        case "聊天室": Router.shared.openFeature(.chat)
        case "福利中心": Router.shared.openFeature(.welfare)
        case "理财": Router.shared.openSpotTab(2)
        case "TradFi": Router.shared.openFeature(.tradfi)
        case "跟单": Router.shared.openFeature(.copyTrading)
        case "策略交易": Router.shared.openSpotTab(1)
        default: moreSheet = true
        }
    }

    /// 对照图 #1 的分隔线：1 物理像素（@3x = 0.33pt）通栏浅灰线
    private var hairline: some View { Rectangle().fill(p.border).frame(height: 1 / max(displayScale, 1)) }

    // MARK: 推广轮播（Linbit Card）
    /// 对照图 #1：整块高 138pt；卡面在左（左距 24，顶距 32，76×70），文字列左缘 118pt（副标题 12 / 标题 14 / 「查看详情 →」12），页码「4/8」右下
    private var cardPromo: some View {
        let showArt = bannerImageURL != nil || currentBanner == nil
        return HStack(alignment: .top, spacing: 0) {
            if showArt {
                promoVisual.frame(width: 76, height: 70).padding(.leading, 24).padding(.top, 32).padding(.trailing, 18)
            } else {
                Spacer().frame(width: Spacing.pageH)
            }
            VStack(alignment: .leading, spacing: 0) {
                Text(currentBanner == nil ? "数字资产 · 全球消费" : (currentBanner?.subtitle ?? ""))
                    .font(.lb(12)).foregroundColor(p.textSecondary).lineLimit(1).frame(height: 17, alignment: .leading)
                Text(currentBanner?.title ?? "Linbit Card").font(.lb(14, .medium)).foregroundColor(p.textPrimary)
                    .lineLimit(1).minimumScaleFactor(0.85).frame(height: 17, alignment: .leading).padding(.top, 5.5)
                HStack(spacing: 6) {
                    Text("查看详情").font(.lb(12)).foregroundColor(p.textPrimary)
                    IconView(icon: .arrowRight, size: 16, tint: p.textPrimary)
                }
                .frame(height: 17, alignment: .leading).padding(.top, 39.5)
            }
            .padding(.top, 20)
            Spacer(minLength: 8)
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: 138)
        .overlay(alignment: .bottomTrailing) { pager(bannerPage + 1, max(vm.banners.count, 1)).padding(.trailing, 23).padding(.bottom, 4) }
        .contentShape(Rectangle())
        .onTapGesture { Router.shared.openFeature(.coupons) }
        .onReceive(Timer.publish(every: 5, on: .main, in: .common).autoconnect()) { _ in
            if vm.banners.count > 1 { withAnimation { bannerPage = (bannerPage + 1) % vm.banners.count } }
        }
    }
    /// 页码「4/8」：当前页深色，总数灰色，12pt
    private func pager(_ cur: Int, _ total: Int) -> some View {
        (Text("\(cur)").foregroundColor(p.textPrimary) + Text("/\(total)").foregroundColor(p.textTertiary))
            .font(.lbNum(12)).frame(height: 14)
    }
    private var currentBanner: HomeBanner? { vm.banners[safe: bannerPage] }
    private var bannerImageURL: URL? {
        guard let s = currentBanner?.image?.trimmingCharacters(in: .whitespaces), !s.isEmpty else { return nil }
        return URL(string: s)
    }
    /// 轮播卡面（左侧 76×70）：有图片 URL 用远程图；运营位无图时为纯文字卡片（不套用无关本地配图）；无运营数据时展示默认 Linbit Card 卡面（绿卡在后、黑卡在前）。
    @ViewBuilder private var promoVisual: some View {
        if let url = bannerImageURL {
            AsyncImage(url: url) { img in img.resizable().scaledToFit() } placeholder: { RoundedRectangle(cornerRadius: 8).fill(p.fill) }
                .frame(width: 76, height: 70)
        } else if currentBanner == nil {
            // 对齐 Android：绿卡 #ABE127 在后旋转 -9°，黑卡 #070808 在前旋转 +5°，右下角白色 VISA 文字
            ZStack {
                RoundedRectangle(cornerRadius: 4)
                    .fill(Color(red: 0xAB/255.0, green: 0xE1/255.0, blue: 0x27/255.0))
                    .frame(width: 54, height: 34)
                    .rotationEffect(.degrees(-9))
                    .position(x: 27, y: 19)
                RoundedRectangle(cornerRadius: 4)
                    .fill(Color(red: 0x07/255.0, green: 0x08/255.0, blue: 0x08/255.0))
                    .frame(width: 62, height: 39)
                    .overlay(
                        ZStack(alignment: .bottomTrailing) {
                            IconView(icon: .logo, size: 10, tint: .white).padding(5).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                            Text("VISA").font(.system(size: 7, weight: .bold)).foregroundColor(.white).padding(.trailing, 5).padding(.bottom, 3)
                        }
                    )
                    .rotationEffect(.degrees(5))
                    .position(x: 44.5, y: 41)
            }
            .frame(width: 76, height: 70)
        }
    }

    // MARK: 理财宝
    /// 对照图 #1：整块高 87pt，两行（第 1 行「理财宝」11pt + 18pt 币图标，第 2 行 15pt 产品名与 APR），左距 16，顶距 24，页码「1/5」右下
    private var earnStrip: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 5) {
                Text("理财宝").font(.lb(11)).foregroundColor(p.textPrimary)
                if let e = vm.earn[safe: earnPage] { CoinIcon(base: e.asset ?? "USDT", size: 18) }
            }
            .frame(height: 18)
            Text(earnLine).font(.lb(FontSize.body)).foregroundColor(p.textPrimary).lineLimit(1).frame(height: 18, alignment: .leading)
        }
        .padding(.top, 24).padding(.leading, Spacing.pageH)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: 87)
        .overlay(alignment: .bottomTrailing) { pager(earnPage + 1, max(vm.earn.count, 1)).padding(.trailing, 23).padding(.bottom, 23.5) }
        .contentShape(Rectangle())
        .onTapGesture { Router.shared.openSpotTab(2) }
        .onReceive(Timer.publish(every: 4, on: .main, in: .common).autoconnect()) { _ in
            if !vm.earn.isEmpty { earnPage = (earnPage + 1) % vm.earn.count }
        }
    }
    private var earnLine: String {
        guard let e = vm.earn[safe: earnPage] else { return "" }
        return "\(e.title ?? e.asset ?? "")，\(Fmt.percent(Dec.parse(e.aprMax ?? e.apr) * 100).trimmingCharacters(in: CharacterSet(charactersIn: "+")))APR"
    }

    // MARK: 榜单
    private var ranking: some View {
        VStack(alignment: .leading, spacing: 0) {
            // 对照图 #2：分隔线到 Tab 文字顶 23pt；Tab 间距 18pt、选中项下 16×3 横线；横线到筛选块 19pt
            PrimaryTabs(items: ["自选", "热门", "涨幅榜", "跌幅榜", "成交额榜"], selected: $rankTab, size: 16, underline: true, spacing: 17)
                .padding(.horizontal, Spacing.pageH).padding(.top, 16)
            ChipBar(items: ["现货", "合约"], selected: $rankKind).padding(.top, 19).padding(.bottom, 12)
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

    private var announcementBuckets: [(String, [APIAnnouncement])] {
        let groups = Dictionary(grouping: vm.announcements, by: { Self.bucket($0.category) })
        return ["公告", "快讯", "学院"].compactMap { key in groups[key].map { (key, $0) } }
    }

    private static func bucket(_ category: String?) -> String {
        let s = (category ?? "").lowercased()
        if s.contains("news") || s.contains("flash") || s.contains("快讯") { return "快讯" }
        if s.contains("academy") || s.contains("learn") || s.contains("学院") || s.contains("school") { return "学院" }
        return "公告"
    }

    @ViewBuilder private var announcements: some View {
        let buckets = announcementBuckets
        if !buckets.isEmpty || vm.announcementNote != nil {
            VStack(alignment: .leading, spacing: 0) {
                if buckets.count > 1 {
                    PrimaryTabs(items: buckets.map(\.0), selected: $annTab, size: 16, underline: true, spacing: 17)
                        .padding(.horizontal, Spacing.pageH).padding(.top, 8)
                } else {
                    Text(buckets.first?.0 ?? "公告").font(.lb(16, .semibold)).foregroundColor(p.textPrimary)
                        .padding(.horizontal, Spacing.pageH).padding(.top, 16)
                }
                if let note = vm.announcementNote, buckets.isEmpty {
                    Text(note).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                        .padding(.horizontal, Spacing.pageH).padding(.vertical, 16)
                }
                let rows = buckets.isEmpty ? [] : buckets[min(annTab, buckets.count - 1)].1
                ForEach(Array(rows.prefix(5).enumerated()), id: \.offset) { _, item in
                    Button { article = Article(item: item) } label: {
                        HStack {
                            Text(item.title ?? "公告").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary).lineLimit(1)
                            Spacer(minLength: 8)
                            Text(item.date ?? "").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                        }.padding(.horizontal, Spacing.pageH).frame(height: 44)
                    }.buttonStyle(.plain)
                }
                if rows.count > 5 {
                    Button { showAllAnnouncements = true } label: {
                        HStack(spacing: 2) {
                            Text("查看更多").font(.lb(FontSize.caption))
                            IconView(icon: .arrowRight, size: 12, tint: p.textSecondary)
                        }.foregroundColor(p.textSecondary).frame(maxWidth: .infinity).frame(height: 44)
                    }
                }
            }
        }
    }

    private var announcementList: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(Array(vm.announcements.enumerated()), id: \.offset) { _, item in
                        Button { article = Article(item: item) } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(item.title ?? "公告").font(.lb(FontSize.body)).foregroundColor(p.textPrimary).frame(maxWidth: .infinity, alignment: .leading)
                                Text(item.date ?? "").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                            }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                        }.buttonStyle(.plain)
                        Divider()
                    }
                }
            }
            .navigationTitle("公告")
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private func handleScan(_ raw: String) {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.lowercased().hasPrefix("http://") || text.lowercased().hasPrefix("https://") {
            HomeLink.open(text, fallback: nil)
            return
        }
        let key = text.uppercased().replacingOccurrences(of: "/", with: "").replacingOccurrences(of: " ", with: "")
        let rows = MarketStore.shared.spot + MarketStore.shared.futures
        if let row = rows.first(where: { $0.symbol.uppercased() == key || $0.base.uppercased() == key || ($0.base + $0.quote).uppercased() == key }) {
            Router.shared.marketDetail = row.symbol
        } else {
            ToastCenter.shared.show("没有对应的交易对")
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

/// 运营位链接：linbit://feature/earn、/earn、/strategy、/swap、/chat、/rewards/newcomer… → 功能页；其他 http(s) → 浏览器
/// 只按路由第一段**精确**匹配（不再用 contains，避免 /partner-news、/copyright 之类误跳）。
@MainActor
enum HomeLink {
    private static let routes: [String: FeaturePage] = [
        "reward": .welfare, "rewards": .welfare, "newcomer": .welfare, "welfare": .welfare,
        "sub-account": .subAccounts, "partner": .partner, "affiliate": .partner, "copy": .copyTrading, "tradfi": .tradfi,
    ]
    private static let webHosts: Set<String> = ["cowa.click", "www.cowa.click"]

    /// 链接里的路由段：linbit://feature/earn → ["earn"]；/rewards/newcomer → ["rewards", "newcomer"]；其他域名的 http(s) → nil
    private static func segments(_ link: String) -> [String]? {
        var s = link.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if s.hasPrefix("http://") || s.hasPrefix("https://") {
            guard let c = URLComponents(string: s), let host = c.host, webHosts.contains(host) else { return nil }
            s = c.path
        } else {
            if let i = s.firstIndex(where: { $0 == "?" || $0 == "#" }) { s = String(s[..<i]) }
            for scheme in ["linbit://", "cowa://", "linbit:", "cowa:"] where s.hasPrefix(scheme) { s.removeFirst(scheme.count); break }
        }
        var segs = s.split(separator: "/").map(String.init)
        if segs.first == "feature" { segs.removeFirst() }
        return segs
    }

    static func open(_ link: String?, fallback: FeaturePage?) {
        guard let link, !link.isEmpty else { if let f = fallback { Router.shared.openFeature(f) }; return }
        if let first = segments(link)?.first {
            switch first {
            case "earn": Router.shared.openSpotTab(2); return
            case "grid", "strategy": Router.shared.openSpotTab(1); return
            case "swap", "convert": Router.shared.openSpotTab(3); return
            case "chat": Router.shared.openFeature(.chat); return
            case "futures": Router.shared.tab = .futures; return
            case "event": Router.shared.futuresTop = 1; Router.shared.tab = .futures; return
            default: if let f = routes[first] { Router.shared.openFeature(f); return }
            }
        }
        if link.lowercased().hasPrefix("http"), let u = URL(string: link) { UIApplication.shared.open(u); return }
        if let f = fallback { Router.shared.openFeature(f) }
    }
}

/// 首页“更多”：与安卓全部功能一致的 7 项。
struct HomeMoreSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    private let items: [(String, FeaturePage)] = [
        ("福利中心", .welfare), ("跟单交易", .copyTrading), ("消息通知", .notifications),
        ("卡券中心", .coupons), ("合伙人计划", .partner), ("TradFi", .tradfi), ("我的工单", .support),
    ]
    var body: some View {
        LBSheet(title: "全部功能", showClose: true, onClose: onClose) {
            VStack(spacing: 10) {
                ForEach(items, id: \.0) { title, page in
                    Button {
                        onClose()
                        Router.shared.openFeature(page)
                    } label: {
                        HStack {
                            Text(title).font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                            Spacer()
                            Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textTertiary)
                        }
                        .padding(16)
                        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                    }.buttonStyle(PressableStyle())
                }
            }.padding(.horizontal, Spacing.pageH)
        }
    }
}

/// 未登录首页的币面插画。只有形状，没有文字和他人品牌标记。
private struct GuestCoinField: View {
    var body: some View {
        GeometryReader { g in
            let w = g.size.width
            let h = g.size.height
            ZStack {
                coin(72, Color(red: 0.62, green: 0.80, blue: 0.18), tilt: -18)
                    .position(x: w * 0.18, y: h * 0.62)
                coin(46, Color(red: 0.55, green: 0.58, blue: 0.52), tilt: 24)
                    .position(x: w * 0.34, y: h * 0.22)
                coin(58, Color(red: 0.78, green: 0.86, blue: 0.28), tilt: 12)
                    .position(x: w * 0.52, y: h * 0.48)
                coin(40, Color(red: 0.45, green: 0.62, blue: 0.16), tilt: -32)
                    .position(x: w * 0.70, y: h * 0.18)
                coin(64, Color(red: 0.86, green: 0.90, blue: 0.42), tilt: 8)
                    .position(x: w * 0.84, y: h * 0.58)
                coin(34, Color(red: 0.70, green: 0.74, blue: 0.62), tilt: -8)
                    .position(x: w * 0.62, y: h * 0.78)
            }
        }
    }

    private func coin(_ size: CGFloat, _ face: Color, tilt: Double) -> some View {
        ZStack {
            Circle().fill(face.opacity(0.35)).frame(width: size * 1.15, height: size * 0.42).offset(y: size * 0.28).blur(radius: 6)
            Circle().fill(LinearGradient(colors: [face.opacity(0.95), face.opacity(0.55)], startPoint: .topLeading, endPoint: .bottomTrailing))
            Circle().stroke(Color.white.opacity(0.55), lineWidth: max(size * 0.06, 2)).padding(size * 0.08)
            Circle().stroke(face.opacity(0.9), lineWidth: 1.5).padding(size * 0.16)
            Capsule().fill(Color.white.opacity(0.28)).frame(width: size * 0.34, height: size * 0.08).rotationEffect(.degrees(-28))
        }
        .frame(width: size, height: size)
        .rotationEffect(.degrees(tilt))
        .shadow(color: face.opacity(0.25), radius: 8, y: 6)
    }
}

/// 倒计时文本（与 Android 一致：「6D:23:46:26」），每秒刷新。
private struct CountdownText: View {
    let deadline: Date
    @State private var text: String = ""
    private let timer = Timer.publish(every: 1, on: .main, in: .common).autoconnect()
    var body: some View {
        Text(text).onAppear { update() }.onReceive(timer) { _ in update() }
    }
    private func update() {
        let left = max(deadline.timeIntervalSinceNow, 0)
        if left == 0 { text = ""; return }
        let d = Int(left) / 86400
        let h = Int(left) % 86400 / 3600
        let m = Int(left) % 3600 / 60
        let s = Int(left) % 60
        text = String(format: "%dD:%02d:%02d:%02d", d, h, m, s)
    }
}

private struct Article: Identifiable {
    let id = UUID()
    let item: APIAnnouncement
}

private struct AnnouncementBody: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let item: APIAnnouncement
    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text(item.title ?? "公告").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    Text(item.date ?? "").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                    Text(Self.plain(item.body ?? "")).font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }.padding(Spacing.pageH)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("关闭") { dismiss() } } }
        }
    }

    private static func plain(_ s: String) -> String {
        var t = s.replacingOccurrences(of: "<br\\s*/?>", with: "\n", options: .regularExpression)
        t = t.replacingOccurrences(of: "</p>", with: "\n", options: .caseInsensitive)
        t = t.replacingOccurrences(of: "<[^>]+>", with: "", options: .regularExpression)
        for pair in [("&nbsp;", " "), ("&amp;", "&"), ("&lt;", "<"), ("&gt;", ">"), ("&quot;", "\"")] {
            t = t.replacingOccurrences(of: pair.0, with: pair.1)
        }
        let trimmed = t.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? "这条公告没有正文" : trimmed
    }
}

/// 扫描二维码。只识别交易对和链接，不发起充值或转账。
private struct CodeScanner: UIViewControllerRepresentable {
    var onCode: (String) -> Void
    var onClose: () -> Void
    func makeUIViewController(context: Context) -> ScannerController {
        let c = ScannerController()
        c.onCode = onCode
        c.onClose = onClose
        return c
    }
    func updateUIViewController(_ uiViewController: ScannerController, context: Context) {}
}

private final class ScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCode: ((String) -> Void)?
    var onClose: (() -> Void)?
    private let session = AVCaptureSession()
    private var preview: AVCaptureVideoPreviewLayer?
    private var finished = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        let close = UIButton(type: .system)
        close.setTitle("关闭", for: .normal)
        close.setTitleColor(.white, for: .normal)
        close.addTarget(self, action: #selector(closeTapped), for: .touchUpInside)
        close.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(close)
        NSLayoutConstraint.activate([
            close.leadingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.leadingAnchor, constant: 16),
            close.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 8),
        ])
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: start()
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { [weak self] ok in
                DispatchQueue.main.async { ok ? self?.start() : self?.showDenied() }
            }
        default: showDenied()
        }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        if session.isRunning { DispatchQueue.global(qos: .userInitiated).async { [weak self] in self?.session.stopRunning() } }
    }

    private func start() {
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else { showDenied(); return }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.videoGravity = .resizeAspectFill
        layer.frame = view.bounds
        view.layer.insertSublayer(layer, at: 0)
        preview = layer
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in self?.session.startRunning() }
    }

    private func showDenied() {
        let label = UILabel()
        label.text = "没有相机权限，无法扫码"
        label.textColor = .white
        label.numberOfLines = 0
        label.textAlignment = .center
        label.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(label)
        NSLayoutConstraint.activate([
            label.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            label.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            label.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            label.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24),
        ])
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard !finished, let code = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
        finished = true
        session.stopRunning()
        onCode?(code)
    }

    @objc private func closeTapped() { onClose?() }
}
