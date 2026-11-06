import Foundation

enum MarketKind: Int { case spot, futures, tradfi }

/// 行情数据中心：REST 拉全量 + WS ticker 增量，首页与行情页共用。
@MainActor
final class MarketStore: ObservableObject {
    static let shared = MarketStore()

    @Published private(set) var spot: [MarketRow] = []
    @Published private(set) var futures: [MarketRow] = []
    private var refPrice: [String: Decimal] = [:]
    @Published private(set) var categories: [APIMarketCategory] = []
    @Published private(set) var loading = false
    @Published var error: String?

    private var subs: [WSSubscription] = []
    private var symbolMeta: [String: APISymbol] = [:]

    func meta(_ symbol: String) -> APISymbol? { symbolMeta[symbol] }

    func rows(_ kind: MarketKind) -> [MarketRow] {
        switch kind { case .spot: spot; case .futures: futures; case .tradfi: [] }
    }

    func load() async {
        loading = true
        defer { loading = false }
        // ShotGate 行情列表夹具：MarketDemo.covers("markets") 已声明；地区拒绝时避免「暂无数据」空页。
        if MarketDemo.on {
            switch MarketDemo.mode {
            case .error:
                spot = []; futures = []; categories = []; error = MarketDemo.errorMessage
            case .empty:
                spot = []; futures = []; categories = MarketDemo.categories(); error = nil
            case .ok:
                let symbols = MarketDemo.symbols()
                symbolMeta = Dictionary(symbols.map { ($0.symbol, $0) }, uniquingKeysWith: { a, _ in a })
                let futT = MarketDemo.listFuturesTickers()
                refPrice = Dictionary(futT.compactMap { t in t.symbol.map { ($0, t.effectivePrice()) } }, uniquingKeysWith: { a, _ in a })
                spot = MarketDemo.spotTickers().map { var r = MarketRow(spot: $0, symbol: symbolMeta[$0.symbol]); if r.last == 0 { r.last = refPrice[r.symbol] ?? 0 }; return r }
                let spotMap = Dictionary(spot.map { ($0.symbol, $0.last) }, uniquingKeysWith: { a, _ in a })
                futures = futT.compactMap { t in
                    guard var r = MarketRow(futures: t) else { return nil }
                    if r.last == 0 { r.last = spotMap[r.symbol] ?? 0 }
                    return r
                }
                categories = MarketDemo.categories()
                error = nil
            }
            // 截图夹具不订 WS
            subs.removeAll()
            return
        }
        async let symbolsR = try? MarketAPI.symbols()
        async let spotR = try? MarketAPI.spotTickers()
        async let futR = try? MarketAPI.futuresTickers()
        async let catR = try? MarketAPI.categories()
        let (symbols, spotT, futT, cats) = await (symbolsR, spotR, futR, catR)
        symbolMeta = Dictionary((symbols ?? []).map { ($0.symbol, $0) }, uniquingKeysWith: { a, _ in a })
        // 现货无成交（lastPrice=0）时用同名合约的 标记价 → 指数价 兜底；合约再兜底现货价
        if let futT { refPrice = Dictionary(futT.compactMap { t in t.symbol.map { ($0, t.effectivePrice()) } }, uniquingKeysWith: { a, _ in a }) }
        if let spotT { spot = spotT.map { var r = MarketRow(spot: $0, symbol: symbolMeta[$0.symbol]); if r.last == 0 { r.last = refPrice[r.symbol] ?? 0 }; return r } }
        if let futT {
            let spotMap = Dictionary(spot.map { ($0.symbol, $0.last) }, uniquingKeysWith: { a, _ in a })
            futures = futT.compactMap { t in
                guard var r = MarketRow(futures: t) else { return nil }
                if r.last == 0 { r.last = spotMap[r.symbol] ?? 0 }
                return r
            }
        }
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
        let v = Dec.parse(t.lastPrice)
        r.last = v > 0 ? v : (refPrice[t.symbol] ?? r.last)
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

/// 自选：本地持久化；登录后与 /account/favorites 合并同步（PUT 整体替换）。
@MainActor
final class Watchlist: ObservableObject {
    static let shared = Watchlist()
    @Published private(set) var symbols: [String] = UserDefaults.standard.stringArray(forKey: "lb.watchlist") ?? []
    func contains(_ s: String) -> Bool { symbols.contains(s) }
    func toggle(_ s: String) { if contains(s) { symbols.removeAll { $0 == s } } else { symbols.append(s) }; persist() }
    func add(_ list: [String]) { for s in list where !contains(s) { symbols.append(s) }; persist() }
    private func persist() {
        UserDefaults.standard.set(symbols, forKey: "lb.watchlist")
        guard TokenStore.shared.isLoggedIn else { return }
        let snapshot = symbols
        pushTask?.cancel()
        pushTask = Task { try? await Task.sleep(nanoseconds: 600_000_000); guard !Task.isCancelled else { return }; _ = try? await FavoritesAPI.replace(snapshot) }
    }
    private var pushTask: Task<Void, Never>?
    /// 登录后调用：服务端 ∪ 本地，回写服务端
    func sync() async {
        guard TokenStore.shared.isLoggedIn, let remote = try? await FavoritesAPI.list() else { return }
        let merged = remote + symbols.filter { !remote.contains($0) }
        symbols = merged
        UserDefaults.standard.set(symbols, forKey: "lb.watchlist")
        if merged != remote { _ = try? await FavoritesAPI.replace(merged) }
    }
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
