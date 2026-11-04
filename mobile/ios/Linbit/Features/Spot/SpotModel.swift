import Foundation

enum SpotOrderKind: Int, CaseIterable, Identifiable {
    case limit, market, plan, makerOnly
    var id: Int { rawValue }
    var title: String { ["限价单", "市价单", "计划委托", "只做 Maker"][rawValue] }
    var icon: String { ["slider.horizontal.3", "bolt", "clock.arrow.circlepath", "m.square"][rawValue] }
    var desc: String { ["以指定价格或更优价格成交", "以当前市场最优价格立即成交", "价格达到触发价后按设定价格下单", "仅作为挂单方成交，否则自动撤销"][rawValue] }
    /// 计划委托走 /spot/orders/conditional（kind=trigger）；只做 Maker 走 postOnly
    var available: Bool { true }
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

    // 计划委托
    @Published var triggerText = ""
    @Published var planMarket = false
    /// 市价买入：true = 按金额（quoteOrderQty），false = 按数量
    @Published var marketBuyByAmount = true
    @Published var conditional: [APIConditionalOrder] = []
    @Published var condState: Loadable<Void> = .idle

    // 手续费率：登录读 /account/fee-rates（含个人覆盖），否则 /spot/fee-schedule
    @Published var makerRate: Decimal? = nil
    @Published var takerRate: Decimal? = nil
    @Published var futuresFee: APIFeePair?
    @Published var feeOverridden = false
    @Published var feeNote: String?
    @Published var feeState: Loadable<Void> = .idle

    private var sub: WSSubscription?
    private var poll: Task<Void, Never>?
    /// 费率未加载时用于估算的兜底值
    static let fallbackFee: Decimal = 0.001

    init(symbol: String) { self.symbol = symbol }

    var base: String { meta?.base ?? String(symbol.dropLast(4)) }
    var quote: String { meta?.quote ?? "USDT" }
    var pricePrec: Int { meta?.pricePrecision ?? 2 }
    var qtyPrec: Int { meta?.quantityPrecision ?? 5 }

    var price: Decimal { kind == .market ? (isBuy ? book.bestAsk ?? last : book.bestBid ?? last) : Dec.parse(priceText) }
    var qty: Decimal { Dec.parse(qtyText) }
    var isPlan: Bool { kind == .plan }
    /// 市价（或计划委托市价）买入按金额
    var marketLike: Bool { kind == .market || (isPlan && planMarket) }
    var byAmount: Bool { marketLike && isBuy && marketBuyByAmount }
    /// 计划委托限价单以委托价估算，市价以触发价估算
    var refPrice: Decimal { isPlan ? (planMarket ? Dec.parse(triggerText) : Dec.parse(priceText)) : price }
    var amount: Decimal { byAmount ? Dec.parse(amountText) : refPrice * qty }
    /// 只做 Maker 用 maker 费率，其余按 taker 估算
    var feeRate: Decimal { (kind == .makerOnly ? makerRate : takerRate) ?? Self.fallbackFee }
    var availQuote: Decimal { Dec.parse(balances[quote]?.free) }
    var availBase: Decimal { Dec.parse(balances[base]?.free) }
    var maxQty: Decimal { isBuy ? (price > 0 ? availQuote / price : 0) : availBase }
    var fee: Decimal { amount * feeRate }

    /// 下单前校验，返回错误文案
    var validation: String? {
        if !TokenStore.shared.isLoggedIn { return nil }
        if isPlan {
            let t = Dec.parse(triggerText)
            if t <= 0 { return "请输入触发价" }
            if !planMarket && Dec.parse(priceText) <= 0 { return "请输入委托价格" }
        } else if kind != .market && price <= 0 { return "请输入价格" }
        if byAmount {
            if Dec.parse(amountText) <= 0 { return "请输入交易额" }
            if Dec.parse(amountText) > availQuote { return "可用 \(quote) 不足" }
            return nil
        }
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
        await loadFees()
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
        guard TokenStore.shared.isLoggedIn else { balances = [:]; openOrders = []; return }
        async let b = try? AssetsAPI.balances()
        async let o = try? SpotAPI.open(symbol: nil)
        if let list = await b { balances = Dictionary(list.map { ($0.asset, $0) }, uniquingKeysWith: { a, _ in a }) }
        openOrders = await o ?? []
        await loadConditional()
    }

    func loadConditional() async {
        guard TokenStore.shared.isLoggedIn else { conditional = []; condState = .loaded(()); return }
        if condState.value == nil { condState = .loading }
        do { conditional = try await SpotAPI.conditional(status: "open", pageSize: 100).items ?? []; condState = .loaded(()) }
        catch { condState = .failed(error.lbMessage) }
    }
    var shownConditional: [APIConditionalOrder] { onlyCurrent ? conditional.filter { $0.symbol == symbol } : conditional }

    func loadFees() async {
        if feeState.value == nil { feeState = .loading }
        do {
            if TokenStore.shared.isLoggedIn, let r = try? await SpotAPI.feeRates(symbol: symbol) {
                makerRate = r.spot?.maker.map(Dec.parse); takerRate = r.spot?.taker.map(Dec.parse)
                futuresFee = r.futures; feeOverridden = r.overridden == true || r.source == ._override; feeNote = r.note
            } else {
                let r = try await SpotAPI.feeSchedule()
                makerRate = r.spot.maker.map(Dec.parse); takerRate = r.spot.taker.map(Dec.parse)
                futuresFee = r.futures; feeOverridden = false; feeNote = nil
            }
            feeState = .loaded(())
        } catch { feeState = .failed(error.lbMessage) }
    }

    func cancelConditional(_ o: APIConditionalOrder) async {
        do { _ = try await SpotAPI.cancelConditional(o.id ?? ""); ToastCenter.shared.show("已撤销条件单", .success); await refreshAccount() }
        catch { ToastCenter.shared.error(error) }
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
        let cid = "ios-\(UUID().uuidString.prefix(12))"
        let quoteQty = byAmount ? Self.fmt(Dec.parse(amountText), pricePrec) : nil
        let baseQty = byAmount ? nil : Self.fmt(qty, qtyPrec)
        do {
            if isPlan {
                let r = APIConditionalOrderRequest(symbol: symbol, side: isBuy ? .buy : .sell, type: planMarket ? .market : .limit, kind: .trigger,
                                                   triggerPrice: Self.fmt(Dec.parse(triggerText), pricePrec),
                                                   price: planMarket ? nil : Self.fmt(Dec.parse(priceText), pricePrec),
                                                   quantity: baseQty, quoteOrderQty: quoteQty,
                                                   timeInForce: planMarket ? nil : .gtc, clientOrderId: cid)
                _ = try await SpotAPI.createConditional(r)
                ToastCenter.shared.show("计划委托已提交", .success)
                triggerText = ""
            } else {
                let req = APIPlaceOrderRequest(symbol: symbol, side: isBuy ? .buy : .sell, type: kind == .market ? .market : .limit,
                                               price: kind == .market ? nil : Self.fmt(price, pricePrec), quantity: baseQty, quoteOrderQty: quoteQty,
                                               timeInForce: kind == .market ? nil : .gtc, postOnly: kind == .makerOnly ? true : nil, clientOrderId: cid)
                let o = try await SpotAPI.place(req)
                ToastCenter.shared.show(o.status == .filled ? "委托已成交" : "下单成功", .success)
                if tpsl { await placeTpsl(filledQty: Dec.parse(o.filledQuantity), orderQty: Dec.parse(o.quantity)) }
            }
            qtyText = ""; amountText = ""; percent = 0
            await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    /// 附带止盈止损：以反向市价条件单提交（止盈 take_profit / 止损 stop），数量取主单数量
    private func placeTpsl(filledQty: Decimal, orderQty: Decimal) async {
        let q = orderQty > 0 ? orderQty : filledQty
        guard q > 0 else { return }
        let side: APIOrderSide = isBuy ? .sell : .buy
        var fails: [String] = []
        for (text, kind) in [(tpText, APIConditionalOrderRequest.APIKind.takeProfit), (slText, .stop)] where Dec.parse(text) > 0 {
            let r = APIConditionalOrderRequest(symbol: symbol, side: side, type: .market, kind: kind,
                                               triggerPrice: Self.fmt(Dec.parse(text), pricePrec), quantity: Self.fmt(q, qtyPrec),
                                               clientOrderId: "ios-\(UUID().uuidString.prefix(12))")
            do { _ = try await SpotAPI.createConditional(r) } catch { fails.append("\(kind == .stop ? "止损" : "止盈")：\(error.lbMessage)") }
        }
        if fails.isEmpty { ToastCenter.shared.show("止盈止损条件单已提交", .success) }
        else { ToastCenter.shared.show(fails.joined(separator: "；"), .error) }
        tpText = ""; slText = "" 
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
