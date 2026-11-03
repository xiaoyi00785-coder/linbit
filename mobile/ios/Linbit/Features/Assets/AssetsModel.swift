import Foundation

@MainActor
final class AssetsModel: ObservableObject {
    static let shared = AssetsModel()
    @Published var balances: Loadable<[APIBalance]> = .idle
    @Published var futures: APIFuturesAccount?
    @Published var hideAmount = UserDefaults.standard.bool(forKey: "lb.assets.hide") {
        didSet { UserDefaults.standard.set(hideAmount, forKey: "lb.assets.hide") }
    }
    @Published var hideSmall = false
    @Published var query = ""

    var list: [APIBalance] { balances.value ?? [] }
    func usd(_ b: APIBalance) -> Decimal {
        if let v = b.valuationUsd { return Dec.parse(v) }
        if b.asset == "USDT" { return Dec.parse(b.total) }
        let px = MarketStore.shared.spot.first { $0.symbol == b.asset + "USDT" }?.last ?? 0
        return Dec.parse(b.total) * px
    }
    var spotTotal: Decimal { list.reduce(0) { $0 + usd($1) } }
    var spotAvailUSDT: Decimal { Dec.parse(list.first { $0.asset == "USDT" }?.free) }
    var futuresTotal: Decimal { Dec.parse(futures?.marginBalance ?? futures?.walletBalance) }
    var futuresAvail: Decimal { Dec.parse(futures?.availableBalance) }
    var total: Decimal { spotTotal + futuresTotal }

    /// 资产列表：USDT 置顶，余额降序，可搜索 / 隐藏小额（< 1 USDT）
    var rows: [APIBalance] {
        list.filter { (query.isEmpty || $0.asset.localizedCaseInsensitiveContains(query)) && (!hideSmall || usd($0) >= 1) }
            .sorted { usd($0) == usd($1) ? $0.asset < $1.asset : usd($0) > usd($1) }
    }

    func load(silent: Bool = false) async {
        guard Session.shared.isLoggedIn else { balances = .idle; futures = nil; return }
        if !silent || balances.value == nil { balances = .loading }
        do {
            async let b = AssetsAPI.balances()
            async let f = try? AssetsAPI.futuresAccount()
            balances = .loaded(try await b); futures = await f
        } catch { balances = .failed(error.lbMessage) }
    }
}
