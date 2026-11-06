import SwiftUI

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
    @State private var rankKind = 0
    @State private var newsTab = 0
    @State private var bannerPage = 0
    @State private var earnPage = 0
    @State private var moreSheet = false
    @Environment(\.displayScale) private var displayScale
    var onOpenMarkets: () -> Void = {}

    var body: some View {
        VStack(spacing: 0) {
            topBar
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    assetHeader.padding(.top, 14)
                    taskCard.padding(.top, 18)
                    redPacket.padding(.top, 12)
                    grid.padding(.top, 17)
                    hairline
                    cardPromo
                    hairline
                    earnStrip
                    hairline
                    ranking
                    sectionDivider
                    news
                }
                .padding(.bottom, Metrics.tabBarH + Metrics.tabBarInset + 120)
            }
            .refreshable { await vm.load(); await store.load() }
            .clipped()
        }
        .background(p.bg)
        .task { await NotifCenter.shared.refresh() }
        .task { await vm.load(); if store.spot.isEmpty { await store.load() } }
        .lbSheet($moreSheet) { HomeMoreSheet { moreSheet = false } }
    }

    // MARK: 顶栏：头像 / 搜索 / 扫码 / 客服 / 通知
    private var topBar: some View {
        HStack(spacing: 14) {
            Button { Session.shared.requireLogin { Router.shared.showUserCenter = true } } label: {
                if session.account != nil || SecShot.raw == "sec:home" {
                    AvatarView(url: session.account?.avatarUrl, name: session.account?.nickname ?? "lb_Trader", size: 28,
                               local: SecShot.raw == "sec:home" ? AvatarDemo.image(120) : nil)
                } else { IconView(icon: .avatar, size: 28) }
            }
                .buttonStyle(PressableStyle())
            HStack(spacing: 6) {
                IconView(icon: .search, size: 15, tint: p.textTertiary)
                Text(vm.searchHint ?? "BTC").font(.lb(FontSize.label)).foregroundColor(p.textTertiary)
                Spacer()
            }
            .padding(.horizontal, 10)
            .frame(height: Metrics.searchBarH)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
            IconView(icon: .scan, tint: p.textPrimary)
            IconView(icon: .headset, tint: p.textPrimary)
            Button { if session.isLoggedIn || NotifShot.state != nil { Router.shared.openFeature(.notifications) } else { session.showAuth = true } } label: {
                IconView(icon: .bell, tint: p.textPrimary)
                    .overlay(alignment: .topTrailing) { if notif.unread > 0 { Badge(n: notif.unread).offset(x: 8, y: -6) } }
            }.buttonStyle(.plain).accessibilityLabel("消息通知")
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
        // 对照 ref/1：主文案 + 可选倒计时（仅当 API 提供 deadline；无则不伪造）
        HStack(alignment: .center, spacing: 4) {
            VStack(alignment: .leading, spacing: 2) {
                (Text(newcomerLead).foregroundColor(p.textPrimary)
                 + Text("\(vm.newcomer?.total ?? "215") \(vm.newcomer?.asset ?? "USDT")").foregroundColor(p.brand).fontWeight(.semibold)
                 + Text(" 体验金").foregroundColor(p.textPrimary))
                    .font(.lb(FontSize.caption))
                    .lineLimit(2)
                    .minimumScaleFactor(0.85)
                if let d = vm.newcomer?.deadline {
                    Text(Self.newcomerCountdown(d))
                        .font(.lbNum(FontSize.small))
                        .foregroundColor(p.textTertiary)
                }
            }
            Spacer(minLength: 4)
            IconView(icon: .chevronRight, size: 14, tint: p.textSecondary)
        }
        .contentShape(Rectangle())
        .onTapGesture { Haptics.tap(); Router.shared.openFeature(.welfare) }
        .padding(.horizontal, 12)
        .padding(.vertical, vm.newcomer?.deadline == nil ? 0 : 6)
        .frame(minHeight: Metrics.taskCardH)
        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border, lineWidth: 1))
        .padding(.horizontal, Spacing.pageH)
    }

    /// 对照 ref/1「6D:23:46:26」形态；不足 1 天则 HH:MM:SS。
    private static func newcomerCountdown(_ deadline: Date) -> String {
        let s = max(0, Int(deadline.timeIntervalSinceNow))
        let d = s / 86_400, h = (s % 86_400) / 3600, m = (s % 3600) / 60, sec = s % 60
        if d > 0 { return String(format: "%dD:%02d:%02d:%02d", d, h, m, sec) }
        return String(format: "%02d:%02d:%02d", h, m, sec)
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
        .frame(minHeight: Metrics.bannerH)
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
                    Text(title).font(.lb(11)).foregroundColor(p.textPrimary).lineLimit(1).minimumScaleFactor(0.8).frame(minHeight: 14)
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
        case "理财": Router.shared.openFeature(.earn)
        case "TradFi": Router.shared.openFeature(.tradfi)
        case "跟单": Router.shared.openFeature(.copyTrading)
        case "策略交易": Router.shared.openFeature(.strategy)
        default: moreSheet = true
        }
    }

    private var sectionDivider: some View { Rectangle().fill(p.fill).frame(height: 8) }
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
                    .font(.lb(12)).foregroundColor(p.textSecondary).lineLimit(1).minimumScaleFactor(0.85).frame(minHeight: 17, alignment: .leading)
                Text(currentBanner?.title ?? "Linbit Card").font(.lb(14, .medium)).foregroundColor(p.textPrimary)
                    .lineLimit(1).minimumScaleFactor(0.85).frame(minHeight: 17, alignment: .leading).padding(.top, 5.5)
                HStack(spacing: 6) {
                    Text("查看详情").font(.lb(12)).foregroundColor(p.textPrimary)
                    IconView(icon: .arrowRight, size: 16, tint: p.textPrimary)
                }
                .frame(minHeight: 17, alignment: .leading).padding(.top, 39.5)
            }
            .padding(.top, 20)
            Spacer(minLength: 8)
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(minHeight: 138)
        .overlay(alignment: .bottomTrailing) { pager(bannerPage + 1, max(vm.banners.count, 1)).padding(.trailing, 23).padding(.bottom, 4) }
        .contentShape(Rectangle())
        .onTapGesture { HomeLink.open(currentBanner?.link, fallback: nil) }
        .onReceive(Timer.publish(every: 5, on: .main, in: .common).autoconnect()) { _ in
            // 证据：ios.yml 冒烟 sleep 4–7s，与 4s/5s 轮播重叠会拍到 withAnimation 中帧（home-bell 叠影）
            guard ShotGate.launchShot == nil else { return }
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
            ZStack {
                RoundedRectangle(cornerRadius: 4).fill(p.brand).frame(width: 54, height: 34)
                    .rotationEffect(.degrees(-8)).position(x: 27, y: 19)
                RoundedRectangle(cornerRadius: 4).fill(p.primary).frame(width: 62, height: 39)
                    .overlay(IconView(icon: .logo, size: 10, tint: p.bg).padding(5), alignment: .topLeading)
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
            Text(earnLine).font(.lb(FontSize.body)).foregroundColor(p.textPrimary).lineLimit(1).minimumScaleFactor(0.85).frame(minHeight: 18, alignment: .leading)
        }
        .padding(.top, 24).padding(.leading, Spacing.pageH)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: 87)
        .overlay(alignment: .bottomTrailing) { pager(earnPage + 1, max(vm.earn.count, 1)).padding(.trailing, 23).padding(.bottom, 23.5) }
        .contentShape(Rectangle())
        .onTapGesture { Router.shared.openFeature(.earn) }
        .onReceive(Timer.publish(every: 4, on: .main, in: .common).autoconnect()) { _ in
            guard ShotGate.launchShot == nil else { return }
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
            ScrollView(.horizontal, showsIndicators: false) {
                PrimaryTabs(items: ["自选", "热门", "涨幅榜", "跌幅榜", "成交额榜"], selected: $rankTab, size: 16, underline: true, spacing: 17)
                    .padding(.horizontal, Spacing.pageH)
            }
            .padding(.top, 20)
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
            } else if newsTab == 1 {
                EmptyStateView(text: "快讯即将上线").frame(height: 140)
            } else {
                AcademyHomeSection()
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

/// 运营位链接：linbit://feature/earn、/earn、/strategy、/swap、/chat、/rewards/newcomer… → 功能页；其他 http(s) → 浏览器
/// 只按路由第一段**精确**匹配（不再用 contains，避免 /partner-news、/copyright 之类误跳）。
@MainActor
enum HomeLink {
    private static let routes: [String: FeaturePage] = [
        "earn": .earn, "grid": .strategy, "strategy": .strategy, "swap": .swap, "convert": .swap,
        "chat": .chat, "reward": .welfare, "rewards": .welfare, "newcomer": .welfare, "welfare": .welfare,
        "sub-account": .subAccounts, "partner": .partner, "loans": .loans, "loan": .loans, "affiliate": .partner, "copy": .copyTrading, "tradfi": .tradfi,
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
            if let f = routes[first] { Router.shared.openFeature(f); return }
            if first == "futures" { Router.shared.tab = .futures; return }
            if first == "event" { Router.shared.futuresTop = 1; Router.shared.tab = .futures; return }
        }
        if link.lowercased().hasPrefix("http"), let u = URL(string: link) { UIApplication.shared.open(u); return }
        if let f = fallback { Router.shared.openFeature(f) }
    }
}

/// 首页“更多”：全部功能（未开放的显示即将上线）
struct HomeMoreSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject private var flags = AppFeatures.shared
    let onClose: () -> Void
    private let items: [(String, String, FeaturePage?, String?)] = [
        ("arrow.left.arrow.right.circle", "闪兑", .swap, "convert"), ("chart.bar.xaxis", "策略交易", .strategy, nil),
        ("banknote", "理财", .earn, nil), ("bubble.left.and.bubble.right", "聊天室", .chat, "chat"),
        ("person.2", "子账户", .subAccounts, "subAccounts"), ("gift", "福利中心", .welfare, nil), ("person.3.sequence", "合伙人", .partner, nil), ("banknote", "质押借币", .loans, nil),
        ("doc.on.doc", "跟单", .copyTrading, "copyTrading"), ("building.columns", "TradFi", .tradfi, "tokenizedStocks"),
        ("bell.badge", "价格预警", .priceAlerts, "priceAlerts"),
        ("rectangle.stack.person.crop", "模拟交易", .paperTrading, "paperTrading"),
        ("clock.arrow.circlepath", "事件合约", nil, "eventContracts"), ("person.3", "P2P", nil, "p2p"),
        ("creditcard", "快捷买币", nil, "fiatOnRamp"), ("function", "期权", nil, "options")]
    var body: some View {
        LBSheet(title: "全部功能", showClose: true, onClose: onClose) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 18) {
                ForEach(items, id: \.1) { icon, title, page, flag in
                    let on: Bool = {
                        if flag == PaperTradingFeature.flagKey { return PaperTradingFeature.isFlagOn }
                        return flag.map { flags.isOn($0) } ?? true
                    }()
                    Button {
                        onClose()
                        if !on { ToastCenter.shared.show("\(title) 即将上线"); return }
                        if let page { Router.shared.openFeature(page) }
                        else if title == "事件合约" { Router.shared.futuresTop = 1; Router.shared.tab = .futures }
                        else { ToastCenter.shared.show("\(title) 即将上线") }
                    } label: {
                        VStack(spacing: 6) {
                            Image(systemName: icon).font(.system(size: 18)).foregroundColor(on ? p.textPrimary : p.textTertiary)
                                .frame(width: 44, height: 44).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.iconTileBg))
                                .overlay(alignment: .topTrailing) {
                                    if !on { Text("即将上线").font(.lb(8)).foregroundColor(p.textOnPrimary).padding(.horizontal, 3)
                                        .background(Capsule().fill(p.brand)).offset(x: 10, y: -6) }
                                }
                            Text(title).font(.lb(FontSize.small)).foregroundColor(on ? p.textPrimary : p.textTertiary)
                        }
                    }.buttonStyle(PressableStyle())
                }
            }.padding(.horizontal, Spacing.pageH)
        }
    }
}
