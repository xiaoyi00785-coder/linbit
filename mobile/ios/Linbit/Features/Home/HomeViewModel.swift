import Foundation

@MainActor
final class HomeViewModel: ObservableObject {
    @Published var banners: [HomeBanner] = HomeViewModel.localBanners
    @Published var earn: [APIEarnProduct] = []
    @Published var announcements: [APIAnnouncement] = []
    @Published var announcementNote: String?
    @Published var hotFromServer: [HotType: [MarketRow]] = [:]

    /// 登录后从 /assets 汇总；未登录显示 0.00
    @Published var totalUSDT: Decimal = 0

    static let localBanners: [HomeBanner] = [
        .init(id: "card", title: "Linbit Card", subtitle: "数字资产 · 全球消费", image: nil, link: nil),
        .init(id: "redpacket", title: "全员红包雨火爆来袭", subtitle: "惊喜好礼等你拿", image: nil, link: nil),
    ]

    /// 运营位：弹窗 / 搜索占位词（/home/placements 上线后生效）
    @Published var popup: Placement?
    @Published var searchHint: String?

    func load() async {
        async let p = try? PlacementAPI.fetch()
        async let e: APIEarnProducts200Response? = try? APIClient.shared.get("/earn/products")
        async let a = loadAnnouncements()
        async let r: Void = FiatRates.refresh()
        // 优先 /home/placements → 回退 /home/banners → 本地
        if let items = await p, !items.isEmpty {
            let b = items.filter { $0.slot == "home_banner" }
                .map { HomeBanner(id: $0.id, title: $0.title, subtitle: $0.subtitle, image: $0.imageUrl, link: $0.action?.target) }
            if !b.isEmpty { banners = b }
            searchHint = items.first { $0.slot == "search_hint" }?.title
            popup = items.first { $0.slot == "popup" && PopupFrequency.shouldShow($0) }
        } else if let list = try? await MarketAPI.banners(), !list.isEmpty {
            banners = list
        }
        _ = await r
        earn = (await e)?.items?.filter { ($0.apr.map(Dec.parse) ?? 0) > 0 } ?? []
        await a
    }

    private func loadAnnouncements() async {
        do {
            announcements = try await MarketAPI.announcements()
            announcementNote = nil
        } catch {
            // 测试网 /announcements 目前返回 AUTH_UNAUTHORIZED（与文档「公开」不一致），优雅降级
            announcements = []
            announcementNote = (error as? APIFailure)?.code == "AUTH_UNAUTHORIZED" ? "登录后查看公告" : "公告加载失败"
        }
    }

    /// 榜单：优先 /home/hot（开发中），否则用 ticker 本地计算。
    func ranking(_ tab: Int, kind: MarketKind, store: MarketStore, watch: Watchlist) -> [MarketRow] {
        let rows = store.rows(kind)
        switch tab {
        case 0: return rows.filter { watch.contains($0.symbol) }
        case 1: return Array(rows.sorted(by: .volume, .desc).prefix(5))            // 热门：按成交额
        case 2: return Array(rows.sorted(by: .change, .desc).prefix(5))
        case 3: return Array(rows.sorted(by: .change, .asc).prefix(5))
        default: return Array(rows.sorted(by: .volume, .desc).prefix(5))
        }
    }
}

/// 弹窗 / 开屏频次本地去重：once | daily | every_launch
enum PopupFrequency {
    static func shouldShow(_ p: Placement) -> Bool {
        let key = "lb.popup.\(p.id)"
        let last = UserDefaults.standard.object(forKey: key) as? Date
        switch p.frequency ?? "once" {
        case "every_launch": return true
        case "daily": return last.map { !Calendar.current.isDateInToday($0) } ?? true
        default: return last == nil
        }
    }
    static func markShown(_ p: Placement) { UserDefaults.standard.set(Date(), forKey: "lb.popup.\(p.id)") }
}
