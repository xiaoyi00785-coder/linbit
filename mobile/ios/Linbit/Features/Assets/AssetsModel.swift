import Foundation

@MainActor
final class AssetsModel: ObservableObject {
    static let shared = AssetsModel()
    @Published var balances: Loadable<[APIBalance]> = .idle
    @Published var funding: [APIBalance] = []
    @Published var spot: [APIBalance] = []
    @Published var fundingError: String?
    @Published var spotError: String?
    @Published var futures: APIFuturesAccount?
    @Published var hideAmount = UserDefaults.standard.bool(forKey: "lb.assets.hide") {
        didSet { UserDefaults.standard.set(hideAmount, forKey: "lb.assets.hide") }
    }
    @Published var hideSmall = false
    @Published var query = ""

    var list: [APIBalance] { balances.value ?? [] }
    /// 单价：优先用服务端实时指数价 priceUsd；缺失时才回退（USDT=1 / 行情最新价）。不得用 valuationUsd ÷ 数量反推。
    static func unitPrice(asset: String?, priceUsd: String?) -> Decimal? {
        if let p = priceUsd, !p.isEmpty { return Dec.parse(p) }
        guard let asset else { return nil }
        if asset == "USDT" { return 1 }
        return MarketStore.shared.spot.first { $0.symbol == asset + "USDT" }?.last
    }
    func unitPrice(_ b: APIBalance) -> Decimal? { Self.unitPrice(asset: b.asset, priceUsd: b.priceUsd) }
    func usd(_ b: APIBalance) -> Decimal {
        if let v = b.valuationUsd { return Dec.parse(v) }
        if b.asset == "USDT" { return Dec.parse(b.total) }
        let px = MarketStore.shared.spot.first { $0.symbol == b.asset + "USDT" }?.last ?? 0
        return Dec.parse(b.total) * px
    }
    var spotTotal: Decimal { spot.reduce(0) { $0 + usd($1) } }
    var fundingTotal: Decimal { funding.reduce(0) { $0 + usd($1) } }
    var spotAvailUSDT: Decimal { Dec.parse(spot.first { $0.asset == "USDT" }?.free) }
    var fundingAvailUSDT: Decimal { Dec.parse(funding.first { $0.asset == "USDT" }?.free) }
    var futuresTotal: Decimal { Dec.parse(futures?.marginBalance ?? futures?.walletBalance) }
    var futuresAvail: Decimal { Dec.parse(futures?.availableBalance) }
    var total: Decimal { fundingTotal + spotTotal + futuresTotal }

    /// 资产列表：USDT 置顶，余额降序，可搜索 / 隐藏小额（< 1 USDT）
    var rows: [APIBalance] { filtered(list) }
    func filtered(_ src: [APIBalance]) -> [APIBalance] {
        src.filter { (query.isEmpty || $0.asset.localizedCaseInsensitiveContains(query)) && (!hideSmall || usd($0) >= 1) }
            .sorted { usd($0) == usd($1) ? $0.asset < $1.asset : usd($0) > usd($1) }
    }

    func load(silent: Bool = false) async {
        guard Session.shared.isLoggedIn else { balances = .idle; funding = []; spot = []; futures = nil; return }
        if !silent || balances.value == nil { balances = .loading }
        async let fundingTask = Self.loadWallet("funding")
        async let spotTask = Self.loadWallet("spot")
        async let futuresTask = try? AssetsAPI.futuresAccount()
        let fundingResult = await fundingTask
        let spotResult = await spotTask
        futures = await futuresTask
        switch fundingResult {
        case let .success(rows): funding = Self.collapseExactDuplicates(rows); fundingError = nil
        case let .failure(error): fundingError = error.lbMessage
        }
        switch spotResult {
        case let .success(rows): spot = Self.collapseExactDuplicates(rows); spotError = nil
        case let .failure(error): spotError = error.lbMessage
        }
        if fundingError != nil && spotError != nil && funding.isEmpty && spot.isEmpty {
            balances = .failed(fundingError ?? spotError ?? "余额加载失败")
        } else {
            balances = .loaded(funding + spot)
        }
    }

    /// 同一账户里资产、可用、冻结、总额都相同的重复行只留一条，不把两份余额加在一起。
    private static func collapseExactDuplicates(_ rows: [APIBalance]) -> [APIBalance] {
        var seen = Set<String>()
        return rows.filter { row in
            let key = "\(row.asset)|\(row.free ?? "")|\(row.locked ?? "")|\(row.total ?? "")"
            return seen.insert(key).inserted
        }
    }

    private static func loadWallet(_ account: String) async -> Result<[APIBalance], Error> {
        do { return .success(try await AssetsAPI.balances(account: account)) }
        catch { return .failure(error) }
    }
}
