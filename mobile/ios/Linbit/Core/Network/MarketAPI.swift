import Foundation

/// 首页 banner / 热门榜（feat/home-transfer 分支开发中，尚未进入 openapi，先手写模型）。
struct HomeBanner: Decodable, Identifiable, Hashable {
    let id: String
    let title: String?
    let subtitle: String?
    let image: String?
    let link: String?
}

enum HotType: String, CaseIterable { case hot, gainers, losers, new, volume }

struct HotItem: Decodable, Hashable {
    let symbol: String
    let lastPrice: String
    let priceChangePercent: String
    let quoteVolume: String?
}

/// 行情统一行模型（现货 Ticker / 合约 FuturesTicker 归一）。
struct MarketRow: Identifiable, Hashable {
    var id: String { symbol }
    let symbol: String
    let base: String
    let quote: String
    var last: Decimal
    var changePct: Decimal
    var quoteVolume: Decimal
    var precision: Int?

    var displayPair: String { symbol }
}

enum MarketAPI {
    static func symbols() async throws -> [APISymbol] { try await APIClient.shared.get("/market/symbols") }

    static func categories() async throws -> [APIMarketCategory] { try await APIClient.shared.get("/market/categories") }

    static func spotTickers() async throws -> [APITicker] {
        let r: APIGetTicker200Response = try await APIClient.shared.get("/market/ticker")
        switch r {
        case let .typeArrayOfAPITicker(list): return list
        case let .typeAPITicker(one): return [one]
        }
    }

    static func futuresTickers() async throws -> [APIFuturesTicker] {
        let r: APIFuturesTicker200ResponseOneOf = try await APIClient.shared.get("/futures/market/ticker")
        return r.tickers ?? []
    }

    static func announcements() async throws -> [APIAnnouncement] {
        let r: APIListAnnouncements200Response = try await APIClient.shared.get("/announcements")
        return r.items ?? []
    }

    static func banners(locale: String = "zh-CN") async throws -> [HomeBanner] {
        try await APIClient.shared.get("/home/banners", query: ["platform": "app", "locale": locale])
    }

    static func hot(_ type: HotType, category: String) async throws -> [HotItem] {
        try await APIClient.shared.get("/home/hot", query: ["type": type.rawValue, "category": category])
    }
}

extension MarketRow {
    init(spot t: APITicker, symbol s: APISymbol?) {
        let base = s?.base ?? String(t.symbol.dropLast(4))
        self.init(symbol: t.symbol, base: base, quote: s?.quote ?? "USDT", last: Dec.parse(t.lastPrice),
                  changePct: Dec.parse(t.priceChangePercent), quoteVolume: Dec.parse(t.quoteVolume),
                  precision: s?.pricePrecision)
    }

    /// 合约：无成交时 lastPrice 为 0，用标记价格兜底显示（priceAvailable=true）。
    init?(futures t: APIFuturesTicker) {
        guard let sym = t.symbol else { return nil }
        var last = Dec.parse(t.lastPrice)
        if last == 0 { last = Dec.parse(t.markPrice) }
        self.init(symbol: sym, base: String(sym.dropLast(4)), quote: "USDT", last: last,
                  changePct: Dec.parse(t.priceChangePercent24h), quoteVolume: Dec.parse(t.turnover24h), precision: nil)
    }
}
