import Foundation

enum SpotOrderKind: Int, CaseIterable, Identifiable {
    case limit, market, plan, makerOnly
    var id: Int { rawValue }
    var title: String { ["限价单", "市价单", "计划委托", "只做 Maker"][rawValue] }
    var icon: String { ["slider.horizontal.3", "bolt", "clock.arrow.circlepath", "m.square"][rawValue] }
    var desc: String { ["以指定价格或更优价格成交", "以当前市场最优价格立即成交", "价格达到触发价后按设定价格下单", "仅作为挂单方成交，否则自动撤销"][rawValue] }
    /// 后端目前只支持 limit / market；计划委托暂未开放，只做 Maker 用 limit + timeInForce=GTC 且客户端检查价格不吃单
    var available: Bool { self != .plan }
}

@MainActor
final class SpotModel: ObservableObject {
    @Published var symbol: String
    @Published var meta: APISymbol?
    @Published var book = OrderBook()
    @Published var bookState: Loadable<Void> = .loading
    @Published var last: Decimal = 0
    @Published var changePct: Decimal = 0

    @Published var isBuy = true
    @Published var kind: SpotOrderKind = .limit
    @Published var priceText = ""
    @Published var qtyText = ""
    @Published var amountText = ""
    @Published var percent: Double = 0
    @Published var tpsl = false
    @Published var tpText = ""
    @Published var slText = ""

    @Published var balances: [String: APIBalance] = [:]
    @Published var openOrders: [APIOrder] = []
    @Published var onlyCurrent = false
    @Published var placing = false
    @Published var bookPrecision: Decimal = 0.01

    private var sub: WSSubscription?
    private var poll: Task<Void, Never>?
    static let feeRate: Decimal = 0.001   // 0.1%，后端未出费率接口前固定

    init(symbol: String) { self.symbol = symbol }

    var base: String { meta?.base ?? String(symbol.dropLast(4)) }
    var quote: String { meta?.quote ?? "USDT" }
    var pricePrec: Int { meta?.pricePrecision ?? 2 }
    var qtyPrec: Int { meta?.quantityPrecision ?? 5 }

    var price: Decimal { kind == .market ? (isBuy ? book.bestAsk ?? last : book.bestBid ?? last) : Dec.parse(priceText) }
    var qty: Decimal { Dec.parse(qtyText) }
    var amount: Decimal { price * qty }
    var availQuote: Decimal { Dec.parse(balances[quote]?.free) }
    var availBase: Decimal { Dec.parse(balances[base]?.free) }
    var maxQty: Decimal { isBuy ? (price > 0 ? availQuote / price : 0) : availBase }
    var fee: Decimal { amount * Self.feeRate }

    /// 下单前校验，返回错误文案
    var validation: String? {
        if !Session.shared.isLoggedIn { return nil }
        if kind != .market && price <= 0 { return "请输入价格" }
        if qty <= 0 { return "请输入数量" }
        if let m = meta?.minQuantity, qty < Dec.parse(m) { return "最小下单数量 \(m) \(base)" }
        if isBuy && amount > availQuote { return "可用 \(quote) 不足" }
        if !isBuy && qty > availBase { return "可用 \(base) 不足" }
        if kind == .makerOnly {
            if isBuy, let a = book.bestAsk, price >= a { return "只做 Maker：买入价需低于卖一价" }
            if !isBuy, let b = book.bestBid, price <= b { return "只做 Maker：卖出价需高于买一价" }
        }
        return nil
    }

    func start() async {
        async let s: [APISymbol]? = try? MarketAPI.symbols()
        if let list = await s { meta = list.first { $0.symbol == symbol } }
        if let row = MarketStore.shared.spot.first(where: { $0.symbol == symbol }) { last = row.last; changePct = row.changePct }
        await loadBook()
        sub = MarketSocket.shared.subscribe("depth:\(symbol)") { [weak self] f in
            guard let d = try? CodableHelper.jsonDecoder.decode(APIDepth.self, from: f.data) else { return }
            Task { @MainActor in self?.book = OrderBook(d) }
        }
        // WS 不可用时 3 秒轮询兜底
        poll = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 3_000_000_000)
                await self?.loadBook(silent: true)
            }
        }
        await refreshAccount()
    }
    func stop() { sub = nil; poll?.cancel() }

    func loadBook(silent: Bool = false) async {
        if !silent { bookState = .loading }
        do {
            book = try await SpotAPI.depth(symbol, limit: 20)
            if last == 0 { last = book.bestBid ?? book.bestAsk ?? 0 }
            if priceText.isEmpty, last > 0 { priceText = Fmt.price(last, precision: pricePrec).replacingOccurrences(of: ",", with: "") }
            bookState = .loaded(())
        } catch { if !silent { bookState = .failed(error.lbMessage) } }
    }

    func refreshAccount() async {
        guard Session.shared.isLoggedIn else { balances = [:]; openOrders = []; return }
        async let b = try? AssetsAPI.balances()
        async let o = try? SpotAPI.open(symbol: nil)
        if let list = await b { balances = Dictionary(list.map { ($0.asset, $0) }, uniquingKeysWith: { a, _ in a }) }
        openOrders = await o ?? []
    }

    var shownOrders: [APIOrder] { onlyCurrent ? openOrders.filter { $0.symbol == symbol } : openOrders }

    // 数量 / 交易额 / 滑杆 三者联动
    func setPercent(_ v: Double) {
        percent = v
        let q = maxQty * Decimal(v)
        qtyText = q > 0 ? Self.fmt(q, qtyPrec) : ""
        amountText = q > 0 ? Self.fmt(q * price, pricePrec) : ""
    }
    func qtyEdited() {
        amountText = qty > 0 && price > 0 ? Self.fmt(amount, pricePrec) : ""
        percent = maxQty > 0 ? min(NSDecimalNumber(decimal: qty / maxQty).doubleValue, 1) : 0
    }
    func amountEdited() {
        let a = Dec.parse(amountText)
        qtyText = a > 0 && price > 0 ? Self.fmt(a / price, qtyPrec) : ""
        percent = maxQty > 0 ? min(NSDecimalNumber(decimal: qty / maxQty).doubleValue, 1) : 0
    }
    func stepPrice(_ up: Bool) {
        var step = Decimal(1); for _ in 0..<pricePrec { step /= 10 }
        let p = max(Dec.parse(priceText) + (up ? step : -step), 0)
        priceText = Self.fmt(p, pricePrec); qtyEdited()
    }
    func pickBookPrice(_ p: Decimal) { if kind != .market { priceText = Self.fmt(p, pricePrec); qtyEdited(); Haptics.tap() } }

    static func fmt(_ d: Decimal, _ prec: Int) -> String {
        var v = d, r = Decimal(); NSDecimalRound(&r, &v, prec, .down)
        return NSDecimalNumber(decimal: r).stringValue
    }

    func place() async -> Bool {
        placing = true; defer { placing = false }
        let req = APIPlaceOrderRequest(symbol: symbol, side: isBuy ? .buy : .sell, type: kind == .market ? .market : .limit,
                                       price: kind == .market ? nil : Self.fmt(price, pricePrec), quantity: Self.fmt(qty, qtyPrec),
                                       timeInForce: kind == .market ? nil : .gtc, clientOrderId: "ios-\(UUID().uuidString.prefix(12))")
        do {
            let o = try await SpotAPI.place(req)
            ToastCenter.shared.show(o.status == .filled ? "委托已成交" : "下单成功", .success)
            qtyText = ""; amountText = ""; percent = 0
            await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }
    func cancel(_ o: APIOrder) async {
        do { _ = try await SpotAPI.cancel(o.orderId); ToastCenter.shared.show("撤单成功", .success); await refreshAccount() }
        catch { ToastCenter.shared.error(error) }
    }
    func cancelAll() async {
        let list = shownOrders
        var fails = 0
        await withTaskGroup(of: Bool.self) { g in
            for o in list { g.addTask { (try? await SpotAPI.cancel(o.orderId)) != nil } }
            for await ok in g where !ok { fails += 1 }
        }
        ToastCenter.shared.show(fails == 0 ? "已全部撤销" : "\(list.count - fails) 笔撤销成功，\(fails) 笔失败", fails == 0 ? .success : .error)
        await refreshAccount()
    }
}
