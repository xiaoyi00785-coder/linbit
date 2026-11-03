import Foundation

enum MarketKind: Int { case spot, futures, tradfi }

/// 行情数据中心：REST 拉全量 + WS ticker 增量，首页与行情页共用。
@MainActor
final class MarketStore: ObservableObject {
    static let shared = MarketStore()

    @Published private(set) var spot: [MarketRow] = []
    @Published private(set) var futures: [MarketRow] = []
    @Published private(set) var categories: [APIMarketCategory] = []
    @Published private(set) var loading = false
    @Published var error: String?

    private var subs: [WSSubscription] = []
    private var symbolMeta: [String: APISymbol] = [:]

    func rows(_ kind: MarketKind) -> [MarketRow] {
        switch kind { case .spot: spot; case .futures: futures; case .tradfi: [] }
    }

    func load() async {
        loading = true
        defer { loading = false }
        async let symbolsR = try? MarketAPI.symbols()
        async let spotR = try? MarketAPI.spotTickers()
        async let futR = try? MarketAPI.futuresTickers()
        async let catR = try? MarketAPI.categories()
        let (symbols, spotT, futT, cats) = await (symbolsR, spotR, futR, catR)
        symbolMeta = Dictionary((symbols ?? []).map { ($0.symbol, $0) }, uniquingKeysWith: { a, _ in a })
        if let spotT { spot = spotT.map { MarketRow(spot: $0, symbol: symbolMeta[$0.symbol]) } }
        if let futT { futures = futT.compactMap(MarketRow.init(futures:)) }
        if let cats { categories = cats }
        if spotT == nil && futT == nil { error = "行情加载失败，请检查网络" } else { error = nil }
        subscribeTickers()
    }

    private func subscribeTickers() {
        subs.removeAll()
        let socket = MarketSocket.shared
        for row in spot {
            subs.append(socket.subscribe("ticker:\(row.symbol)") { [weak self] frame in
                guard let t = try? JSONDecoder().decode(WSTicker.self, from: frame.data) else { return }
                self?.apply(t)
            })
        }
    }

    private func apply(_ t: WSTicker) {
        guard let i = spot.firstIndex(where: { $0.symbol == t.symbol }) else { return }
        var r = spot[i]
        r.last = Dec.parse(t.lastPrice)
        r.changePct = Dec.parse(t.priceChangePercent)
        if let q = t.quoteVolume { r.quoteVolume = Dec.parse(q) }
        spot[i] = r
    }

    func filtered(_ kind: MarketKind, categoryIndex: Int) -> [MarketRow] {
        let all = rows(kind)
        guard categoryIndex > 0, categoryIndex - 1 < categories.count else { return all }
        let set = Set(categories[categoryIndex - 1].symbols)
        return all.filter { set.contains($0.symbol) }
    }
}

/// 自选（本地；登录后可同步到服务端，接口待定）。
@MainActor
final class Watchlist: ObservableObject {
    static let shared = Watchlist()
    @Published private(set) var symbols: [String] = UserDefaults.standard.stringArray(forKey: "lb.watchlist") ?? []
    func contains(_ s: String) -> Bool { symbols.contains(s) }
    func toggle(_ s: String) { if contains(s) { symbols.removeAll { $0 == s } } else { symbols.append(s) }; persist() }
    func add(_ list: [String]) { for s in list where !contains(s) { symbols.append(s) }; persist() }
    private func persist() { UserDefaults.standard.set(symbols, forKey: "lb.watchlist") }
}

enum SortField { case volume, price, change }
enum SortDir { case none, desc, asc }

extension Array where Element == MarketRow {
    func sorted(by f: SortField, _ d: SortDir) -> [MarketRow] {
        guard d != .none else { return self }
        let key: (MarketRow) -> Decimal = { f == .volume ? $0.quoteVolume : f == .price ? $0.last : $0.changePct }
        return sorted { d == .desc ? key($0) > key($1) : key($0) < key($1) }
    }
}
