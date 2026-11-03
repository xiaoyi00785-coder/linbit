import Foundation

enum FuturesKind: String, CaseIterable { case limit, market, plan, postOnly
    var title: String { ["limit": "限价单", "market": "市价单", "plan": "计划委托", "postOnly": "只做 Maker"][rawValue]! }
    var subtitle: String {
        ["limit": "以指定价格或更优价格成交", "market": "以当前最优价格立即成交", "plan": "价格到达触发价后自动下单",
         "postOnly": "只挂单不吃单，若会立即成交则自动撤销"][rawValue]!
    }
    var available: Bool { self == .limit || self == .market }
}

@MainActor
final class FuturesModel: ObservableObject {
    @Published var symbol: String
    @Published var contract: APIFuturesContract?
    @Published var ticker: APIFuturesTicker?
    @Published var book = OrderBook()
    @Published var account: APIFuturesAccount?
    @Published var positions: [APIFuturesPosition] = []
    @Published var openOrders: [APIFuturesOrder] = []
    @Published var bonus: APIFuturesBonus200Response?
    @Published var marketError: String?
    @Published var accountLoaded = false

    @Published var leverage = 20
    @Published var maxLeverage = 100
    @Published var marginMode: APIFuturesPosition.APIMarginMode = .cross

    // 表单
    @Published var isOpen = true
    @Published var kind: FuturesKind = .limit
    @Published var priceText = ""
    @Published var costText = ""
    @Published var percent: Double = 0
    @Published var tpsl = false
    @Published var tpText = ""
    @Published var slText = ""
    @Published var placing = false
    @Published var onlyCurrent = false
    @Published var now = Date()

    private var loop: Task<Void, Never>?

    init(symbol: String) { self.symbol = symbol }

    var base: String { contract?.baseAsset ?? String(symbol.dropLast(4)) }
    var quote: String { contract?.quoteAsset ?? "USDT" }
    var pricePrec: Int { max(Dec.precision(of: contract?.tickSize), 1) }
    var qtyPrec: Int { Dec.precision(of: contract?.minQty) }
    var minQty: Decimal { Dec.parse(contract?.minQty) }
    private var spotLast: Decimal { MarketStore.shared.spot.first { $0.symbol == symbol }?.last ?? 0 }
    var last: Decimal { ticker?.effectivePrice(spot: spotLast) ?? spotLast }
    var mark: Decimal { let m = Dec.parse(ticker?.markPrice); return m > 0 ? m : last }
    var changePct: Decimal { Dec.parse(ticker?.priceChangePercent24h) }
    var fundingRate: Decimal { Dec.parse(ticker?.fundingRate) * 100 }
    var available: Decimal { Dec.parse(account?.availableBalance) }
    var mmr: Decimal { Dec.parse(contract?.maintenanceMarginRate ?? "0.004") }
    var takerFee: Decimal { Dec.parse(contract?.takerFeeRate ?? "0.0005") }

    var orderPrice: Decimal { kind == .market ? (last > 0 ? last : mark) : Dec.parse(priceText) }
    var cost: Decimal { Dec.parse(costText) }
    /// 成本（保证金）× 杠杆 / 价格 = 数量，按最小数量步长向下取整
    var qty: Decimal {
        guard orderPrice > 0 else { return 0 }
        if !isOpen, let pos = current { return Dec.parse(Dec.plain(abs(Dec.parse(pos.qty)) * Decimal(percent > 0 ? percent : 1), qtyPrec)) }
        return Dec.parse(Dec.plain(cost * Decimal(leverage) / orderPrice, qtyPrec))
    }
    var maxOpenQty: Decimal { orderPrice > 0 ? available * Decimal(leverage) / orderPrice : 0 }
    func liqPrice(long: Bool) -> Decimal {
        let p = orderPrice; guard p > 0 else { return 0 }
        let inv = 1 / Decimal(leverage)
        return long ? p * (1 - inv + mmr) : p * (1 + inv - mmr)
    }
    var current: APIFuturesPosition? { positions.first { $0.symbol == symbol && Dec.parse($0.qty) != 0 } }
    var shownPositions: [APIFuturesPosition] { onlyCurrent ? positions.filter { $0.symbol == symbol } : positions }
    var shownOrders: [APIFuturesOrder] { onlyCurrent ? openOrders.filter { $0.symbol == symbol } : openOrders }

    var fundingCountdown: String {
        guard let t = ticker?.nextFundingTime else { return "--:--:--" }
        let s = max(0, Int(Double(t) / 1000 - now.timeIntervalSince1970))
        return String(format: "%02d:%02d:%02d", s / 3600, s % 3600 / 60, s % 60)
    }

    func start() async {
        loop?.cancel()
        await loadContract()
        await refreshMarket()
        await refreshAccount()
        if priceText.isEmpty, last > 0 { priceText = Dec.plain(last, pricePrec) }
        loop = Task { [weak self] in
            var n = 0
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                guard let self else { return }
                self.now = Date(); n += 1
                if n % 3 == 0 { await self.refreshMarket() }
                if n % 6 == 0 { await self.refreshAccount() }
            }
        }
    }
    func stop() { loop?.cancel(); loop = nil }

    func switchSymbol(_ s: String) async {
        stop(); symbol = s; priceText = ""; costText = ""; percent = 0; ticker = nil; book = OrderBook()
        await start()
    }

    private func loadContract() async {
        if let list = try? await FuturesAPI.contracts() {
            contract = list.first { $0.symbol == symbol }
            maxLeverage = contract?.maxLeverage ?? 100
        }
    }

    func refreshMarket() async {
        do {
            async let t = FuturesAPI.ticker(symbol)
            async let b = FuturesAPI.depth(symbol)
            ticker = try await t
            book = (try? await b) ?? book
            marketError = nil
        } catch { if ticker == nil { marketError = error.lbMessage } }
    }

    func refreshAccount() async {
        guard TokenStore.shared.isLoggedIn else { account = nil; positions = []; openOrders = []; accountLoaded = true; return }
        async let a = try? FuturesAPI.account()
        async let o = try? FuturesAPI.openOrders()
        async let l = try? FuturesAPI.leverage(symbol)
        async let bn = try? FuturesAPI.bonus()
        if let acc = await a { account = acc; positions = (acc.positions ?? []).filter { Dec.parse($0.qty) != 0 } }
        if let list = await o { openOrders = list }
        if let lv = await l {
            leverage = lv.leverage ?? leverage
            maxLeverage = lv.maxLeverage ?? maxLeverage
            if let mm = lv.marginMode { marginMode = mm == .isolated ? .isolated : .cross }
        }
        bonus = await bn
        accountLoaded = true
    }

    func setPercent(_ v: Double) {
        percent = v
        guard isOpen else { return }
        costText = v == 0 ? "" : Dec.plain(available * Decimal(v), 2)
    }

    var validation: String? {
        if kind != .market, Dec.parse(priceText) <= 0 { return "请输入价格" }
        if isOpen && cost <= 0 { return "请输入成本" }
        if qty <= 0 || (minQty > 0 && qty < minQty) { return "数量低于最小下单量 \(Dec.plain(minQty, qtyPrec)) \(base)" }
        if isOpen && cost > available { return "可用保证金不足" }
        if !isOpen && current == nil { return "当前无持仓可平" }
        return nil
    }

    /// long=true：开多 / 平空（买）；false：开空 / 平多（卖）
    func place(long: Bool) async -> Bool {
        placing = true; defer { placing = false }
        let side: APIFuturesPlaceOrderRequest.APISide = long ? .buy : .sell
        let req = APIFuturesPlaceOrderRequest(
            symbol: symbol, side: side, type: kind == .market ? .market : .limit,
            timeInForce: kind == .market ? .ioc : .gtc,
            price: kind == .market ? nil : Dec.plain(Dec.parse(priceText), pricePrec),
            qty: Dec.plain(qty, qtyPrec), reduceOnly: isOpen ? nil : true,
            tpPrice: isOpen && tpsl && !tpText.isEmpty ? tpText : nil,
            slPrice: isOpen && tpsl && !slText.isEmpty ? slText : nil)
        do {
            _ = try await FuturesAPI.place(req)
            ToastCenter.shared.show(isOpen ? (long ? "开多委托已提交" : "开空委托已提交") : "平仓委托已提交", .success)
            costText = ""; percent = 0
            await refreshAccount()
            return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    func saveLeverage(_ v: Int, mode: APIFuturesPosition.APIMarginMode) async -> Bool {
        do {
            let r = try await FuturesAPI.setLeverage(.init(symbol: symbol, leverage: v, marginMode: mode == .isolated ? .isolated : .cross))
            leverage = r.leverage ?? v
            if let mm = r.marginMode { marginMode = mm == .isolated ? .isolated : .cross }
            ToastCenter.shared.show("已保存", .success)
            return true
        } catch {
            if !TokenStore.shared.isLoggedIn { leverage = v; marginMode = mode; return true }
            ToastCenter.shared.error(error); return false
        }
    }

    func close(_ pos: APIFuturesPosition) async {
        do { try await FuturesAPI.close(pos.symbol ?? symbol); ToastCenter.shared.show("已提交市价平仓", .success); await refreshAccount() }
        catch { ToastCenter.shared.error(error) }
    }
    func closeAll() async {
        for p in shownPositions { await close(p) }
    }
    func cancel(_ o: APIFuturesOrder) async {
        guard let id = o.orderId else { return }
        do { try await FuturesAPI.cancel(id); ToastCenter.shared.show("已撤销", .success); await refreshAccount() }
        catch { ToastCenter.shared.error(error) }
    }
    func setTpsl(_ pos: APIFuturesPosition, tp: String, sl: String) async -> Bool {
        do {
            try await FuturesAPI.setTpsl(pos.symbol ?? symbol, tp: tp.isEmpty ? nil : tp, sl: sl.isEmpty ? nil : sl)
            ToastCenter.shared.show("止盈止损已设置", .success); await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }
    func adjustMargin(_ pos: APIFuturesPosition, amount: Decimal) async -> Bool {
        do {
            try await FuturesAPI.adjustMargin(pos.symbol ?? symbol, amount: NSDecimalNumber(decimal: amount).stringValue)
            ToastCenter.shared.show(amount > 0 ? "保证金已增加" : "保证金已减少", .success); await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }
}
