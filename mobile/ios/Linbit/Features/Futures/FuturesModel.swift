import Foundation

/// 下单输入单位。数量是币的个数，成本是保证金，价值是仓位名义价值。
enum FuturesOrderUnit: String, CaseIterable, Identifiable {
    case qty, cost, value
    var id: String { rawValue }
    var fieldLabel: String {
        switch self {
        case .qty: return "数量"
        case .cost: return "成本"
        case .value: return "价值"
        }
    }
    func title(base: String, quote: String) -> String {
        switch self {
        case .qty: return "数量-\(base)"
        case .cost: return "成本-\(quote)"
        case .value: return "价值-\(quote)"
        }
    }
    func detail(base: String) -> String {
        switch self {
        case .qty: return "合约标的交易的仓位数量，单位为\(base)。"
        case .cost: return "交易实际投入的保证金数量，如：开仓时的初始保证金、平仓时结算的保证金。"
        case .value: return "标的资产的市场价值，基于保证金和杠杆倍数计算，并随市场价格波动而调整。"
        }
    }
}

enum FuturesKind: String, CaseIterable { case limit, market, plan, postOnly
    var title: String { ["limit": "限价单", "market": "市价单", "plan": "计划委托", "postOnly": "只做 Maker"][rawValue]! }
    var subtitle: String {
        ["limit": "以指定价格或更优价格成交", "market": "以当前最优价格立即成交", "plan": "价格到达触发价后自动下单",
         "postOnly": "只挂单不吃单，若会立即成交则自动撤销"][rawValue]!
    }
    /// 四种委托均已接通：限价/市价 → /futures/orders；只做 Maker → timeInForce POST_ONLY；计划委托 → /futures/plan-orders
    var available: Bool { true }
    var backendMissing: Bool { false }
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
    /// 输入框原文。含义由 orderUnit 决定：数量、成本或价值。
    @Published var costText = ""
    @Published var orderUnit: FuturesOrderUnit = .cost
    @Published var percent: Double = 0
    @Published var tpsl = false
    @Published var tpText = ""
    @Published var slText = ""
    /// 计划委托：触发价、触发价类型、触发后市价 / 限价
    @Published var triggerText = ""
    @Published var planTriggerType: APIFuturesPlaceOrderRequest.APITriggerPriceType = .last
    /// 下单附带止盈止损的触发价类型（默认标记价格）
    @Published var orderTpType: APIFuturesPlaceOrderRequest.APITpTriggerPriceType = .mark
    @Published var orderSlType: APIFuturesPlaceOrderRequest.APISlTriggerPriceType = .mark
    @Published var planMarket = false
    /// 持仓模式：ONE_WAY 合仓 / HEDGE 分仓（GET/POST /futures/position-mode）
    @Published var positionMode: APIFuturesPositionMode.APIPositionMode = .oneWay
    /// 进行中的止盈止损委托 / 计划委托（/futures/tpsl-orders、/futures/plan-orders，status=active）
    @Published var tpslAlgos: [APIFuturesAlgoOrder] = []
    @Published var planAlgos: [APIFuturesAlgoOrder] = []
    @Published var algosLoaded = false
    @Published var algosError: String?
    /// 风险限额档位（/futures/market/risk-tiers），用于杠杆弹窗的最大可持仓
    @Published var tiers: [APIFuturesRiskTiers200ResponseContractsInnerTiersInner] = []
    @Published var placing = false
    @Published var onlyCurrent = false
    /// 成交推送的最新价。盘口中间大字用它，不再等 3 秒轮询。
    @Published var tradeLast: Decimal = 0
    @Published var closingKeys: Set<String> = []
    @Published var closingAll = false
    private var quoteSubs: [WSSubscription] = []
    private var pendingLast: Decimal?
    private var quoteFlush: Task<Void, Never>?
    /// 今日盈亏（GET /futures/pnl/today）
    @Published var todayPnl: Loadable<APIFuturesTodayPnl200Response> = .idle
    /// WS mark:<symbol> 实时标记价格（symbol -> 价格）
    @Published var wsMarks: [String: Decimal] = [:]
    @Published var wsFunding: [String: (rate: Decimal, next: Int64?)] = [:]
    /// 截图演示：未登录 CI 下注入示例仓位 / 今日盈亏
    var shotDemo = false
    private var markSubs: [String: WSSubscription] = [:]

    private var loop: Task<Void, Never>?

    init(symbol: String) { self.symbol = symbol }

    var base: String { contract?.baseAsset ?? String(symbol.dropLast(4)) }
    var quote: String { contract?.quoteAsset ?? "USDT" }
    var pricePrec: Int { max(Dec.precision(of: contract?.tickSize), 1) }
    var qtyPrec: Int { Dec.precision(of: contract?.minQty) }
    var minQty: Decimal { Dec.parse(contract?.minQty) }
    private var spotLast: Decimal { MarketStore.shared.spot.first { $0.symbol == symbol }?.last ?? 0 }
    var last: Decimal {
        if tradeLast > 0 { return tradeLast }
        return ticker?.effectivePrice(spot: spotLast) ?? spotLast
    }
    var mark: Decimal {
        if let w = wsMarks[symbol], w > 0 { return w }
        let m = Dec.parse(ticker?.markPrice); return m > 0 ? m : last
    }
    /// 某合约的实时标记价：WS 优先，其次仓位 / 行情接口返回值
    func markPrice(for sym: String?, fallback: String? = nil) -> Decimal {
        if let s = sym, let w = wsMarks[s], w > 0 { return w }
        if sym == symbol { let m = Dec.parse(ticker?.markPrice); if m > 0 { return m } }
        return Dec.parse(fallback)
    }
    /// 按实时标记价重算未实现盈亏与收益率（多：(mark-entry)*qty；空：(entry-mark)*qty；收益率 = 盈亏 / 保证金）
    func livePnl(_ pos: APIFuturesPosition) -> (pnl: Decimal, roe: Decimal, mark: Decimal) {
        let mk = markPrice(for: pos.symbol, fallback: pos.markPrice)
        let entry = Dec.parse(pos.entryPrice), q = abs(Dec.parse(pos.qty))
        guard mk > 0, entry > 0, q > 0 else { return (Dec.parse(pos.unrealizedPnl), Dec.parse(pos.roe) * 100, mk) }
        let pnl = (pos.side == .short ? entry - mk : mk - entry) * q
        let margin = Dec.parse(pos.margin)
        let roe = margin > 0 ? pnl / margin * 100 : Dec.parse(pos.roe) * 100
        return (pnl, roe, mk)
    }
    /// 全部仓位的实时未实现盈亏合计
    var liveUnrealized: Decimal { positions.reduce(0) { $0 + livePnl($1).pnl } }
    /// 24h 涨跌幅。priceChangePercent 已是百分数（-0.6 表示 -0.6%）；priceChangePercent24h 是比率（-0.005969）。没有字段时是 nil，不当成 0。
    var changePct: Decimal? {
        if let raw = ticker?.priceChangePercent, !raw.isEmpty { return Dec.parse(raw) }
        if let raw = ticker?.priceChangePercent24h, !raw.isEmpty { return Dec.parse(raw) * 100 }
        return nil
    }
    var fundingRate: Decimal { if let f = wsFunding[symbol] { return f.rate * 100 }; return Dec.parse(ticker?.fundingRate) * 100 }
    var available: Decimal { Dec.parse(account?.availableBalance) }
    var mmr: Decimal { Dec.parse(contract?.maintenanceMarginRate ?? "0.004") }
    var takerFee: Decimal { Dec.parse(contract?.takerFeeRate ?? "0.0005") }

    var hedge: Bool { positionMode == .hedge }
    var orderPrice: Decimal {
        if kind == .market || (kind == .plan && planMarket) {
            let t = Dec.parse(triggerText)
            return kind == .plan && t > 0 ? t : (last > 0 ? last : mark)
        }
        return Dec.parse(priceText)
    }
    /// 输入框里的数
    var typed: Decimal { Dec.parse(costText) }
    private var lev: Decimal { Decimal(max(leverage, 1)) }
    /// 按当前单位换算出的下单数量，按最小数量步长向下取整
    var qty: Decimal {
        guard orderPrice > 0 else { return 0 }
        if !isOpen, let pos = closeTarget { return Dec.parse(Dec.plain(abs(Dec.parse(pos.qty)) * Decimal(percent > 0 ? percent : 1), qtyPrec)) }
        let raw: Decimal
        switch orderUnit {
        case .qty: raw = typed
        case .cost: raw = typed * lev / orderPrice
        case .value: raw = typed / orderPrice
        }
        return Dec.parse(Dec.plain(raw, qtyPrec))
    }
    /// 这笔单占用的保证金
    var margin: Decimal {
        guard orderPrice > 0 else { return 0 }
        switch orderUnit {
        case .qty: return qty * orderPrice / lev
        case .cost: return typed
        case .value: return typed / lev
        }
    }
    /// 仓位价值 = 数量 × 价格。成本模式下等于输入 × 杠杆。
    var orderValue: Decimal {
        switch orderUnit {
        case .qty: return qty * orderPrice
        case .cost: return typed * lev
        case .value: return typed
        }
    }
    var maxOpenQty: Decimal { orderPrice > 0 ? available * lev / orderPrice : 0 }
    /// 可开 / 可卖，用当前输入单位表示
    var openLimitText: String {
        switch orderUnit {
        case .qty: return "\(Fmt.amount(maxOpenQty, qtyPrec)) \(base)"
        case .cost: return "\(Fmt.amount(available, 2)) \(quote)"
        case .value: return "\(Fmt.amount(available * lev, 2)) \(quote)"
        }
    }
    func liqPrice(long: Bool) -> Decimal {
        let p = orderPrice; guard p > 0 else { return 0 }
        let inv = 1 / Decimal(leverage)
        return long ? p * (1 - inv + mmr) : p * (1 + inv - mmr)
    }
    var current: APIFuturesPosition? { positions.first { $0.symbol == symbol && Dec.parse($0.qty) != 0 } }
    /// 当前合约的多 / 空仓位（分仓模式下多空可同时存在）
    func position(long: Bool) -> APIFuturesPosition? {
        positions.first { $0.symbol == symbol && Dec.parse($0.qty) != 0 && ($0.side == .short) != long }
    }
    /// 平仓表单默认针对的仓位（合仓：唯一仓位；分仓：优先多仓）
    var closeTarget: APIFuturesPosition? { hedge ? (position(long: true) ?? position(long: false)) : current }
    var shownTpslAlgos: [APIFuturesAlgoOrder] { onlyCurrent ? tpslAlgos.filter { $0.symbol == symbol } : tpslAlgos }
    var shownPlanAlgos: [APIFuturesAlgoOrder] { onlyCurrent ? planAlgos.filter { $0.symbol == symbol } : planAlgos }
    /// 仓位的 positionSide（分仓必填；合仓 BOTH / nil）
    static func side(of pos: APIFuturesPosition) -> String? {
        if let ps = pos.positionSide, ps != .both { return ps.rawValue }
        return nil
    }
    var shownPositions: [APIFuturesPosition] { onlyCurrent ? positions.filter { $0.symbol == symbol } : positions }
    var shownOrders: [APIFuturesOrder] { onlyCurrent ? openOrders.filter { $0.symbol == symbol } : openOrders }

    func fundingText(at date: Date) -> String {
        guard let t = ticker?.nextFundingTime else { return "--:--:--" }
        let s = max(0, Int(Double(t) / 1000 - date.timeIntervalSince1970))
        return String(format: "%02d:%02d:%02d", s / 3600, s % 3600 / 60, s % 60)
    }
    static func closeKey(_ pos: APIFuturesPosition, fallback: String) -> String {
        "\(pos.symbol ?? fallback)|\(side(of: pos) ?? "BOTH")"
    }
    func isClosing(_ pos: APIFuturesPosition) -> Bool { closingKeys.contains(Self.closeKey(pos, fallback: symbol)) }

    /// 页面离开时调用方会取消本任务并 stop()。网络请求返回后要先看是否已取消，
    /// 否则会在 stop() 之后重新订阅、起轮询，页面不在屏幕上也一直刷新。
    func start() async {
        loop?.cancel()
        await loadContract()
        if Task.isCancelled { return }
        await refreshMarket()
        if Task.isCancelled { return }
        await refreshAccount()
        if Task.isCancelled { return }
        if priceText.isEmpty, last > 0 { priceText = Dec.plain(last, pricePrec) }
        syncMarkSubs()
        subscribeQuotes()
        loop?.cancel()
        loop = Task { [weak self] in
            var n = 0
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                guard let self else { return }
                n += 1
                if n % 3 == 0 { await self.refreshMarket() }
                if n % 6 == 0 { await self.refreshAccount(); self.syncMarkSubs() }
                if n % 30 == 0 { await self.loadTodayPnl(silent: true) }
            }
        }
        await loadTodayPnl()
    }
    /// 离开页面：停止轮询并退订全部 mark:<symbol>
    func stop() {
        loop?.cancel(); loop = nil
        quoteFlush?.cancel(); quoteFlush = nil
        for s in markSubs.values { s.cancel() }
        markSubs = [:]
        for s in quoteSubs { s.cancel() }
        quoteSubs = []
    }

    /// 最新价来自 futures-trades / futures-ticker。成交帧往往没有涨跌幅字段，只更新价格。
    func subscribeQuotes() {
        for s in quoteSubs { s.cancel() }
        quoteSubs = []
        let sym = symbol
        quoteSubs.append(MarketSocket.shared.subscribe("futures-trades:\(sym)") { [weak self] frame in
            guard let px = TapePrice.decode(frame.data)?.price, Dec.parse(px) > 0 else { return }
            Task { @MainActor in self?.noteTradeLast(Dec.parse(px)) }
        })
        quoteSubs.append(MarketSocket.shared.subscribe("futures-ticker:\(sym)") { [weak self] frame in
            guard let t = TapePrice.decode(frame.data) else { return }
            let px = Dec.parse(t.lastPrice ?? t.price)
            Task { @MainActor in
                guard let self, px > 0 else { return }
                self.noteTradeLast(px)
            }
        })
    }

    /// 成交帧可能一秒几十条。每条都发布会让整页重算，这里最多每 250ms 刷一次，价格没变不发布。
    func noteTradeLast(_ v: Decimal) {
        guard v > 0 else { return }
        pendingLast = v
        guard quoteFlush == nil else { return }
        quoteFlush = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 250_000_000)
            guard let self, !Task.isCancelled else { return }
            if let p = self.pendingLast, p != self.tradeLast { self.tradeLast = p }
            self.quoteFlush = nil
        }
    }

    /// 订阅当前合约 + 持仓合约的 mark:<symbol>，退订不再需要的
    func syncMarkSubs() {
        let want = Set([symbol] + positions.compactMap(\.symbol))
        for (s, sub) in markSubs where !want.contains(s) { sub.cancel(); markSubs[s] = nil; wsMarks[s] = nil }
        for s in want where markSubs[s] == nil {
            markSubs[s] = MarketSocket.shared.subscribe("mark:\(s)") { [weak self] frame in
                guard let mk = try? JSONDecoder().decode(WSMark.self, from: frame.data) else { return }
                let v = Dec.parse(mk.markPrice)
                Task { @MainActor in
                    guard let self, v > 0 else { return }
                    if self.wsMarks[mk.symbol] != v { self.wsMarks[mk.symbol] = v }
                    if let r = mk.fundingRate {
                        let rate = Dec.parse(r), old = self.wsFunding[mk.symbol]
                        if old?.rate != rate || old?.next != mk.nextFundingTime { self.wsFunding[mk.symbol] = (rate, mk.nextFundingTime) }
                    }
                }
            }
        }
    }

    func loadTodayPnl(silent: Bool = false) async {
        if shotDemo { return }
        guard TokenStore.shared.isLoggedIn else { todayPnl = .idle; return }
        if !silent || todayPnl.value == nil { if todayPnl.value == nil { todayPnl = .loading } }
        do { todayPnl = .loaded(try await FuturesAPI.todayPnl()) }
        catch { if !silent || todayPnl.value == nil { todayPnl = .failed(error.lbMessage) } }
    }

    /// restart 为 false 时（页面不在屏幕上）只换币种、清表单，等页面回到屏幕再拉行情。
    func switchSymbol(_ s: String, restart: Bool = true) async {
        stop(); symbol = s; priceText = ""; costText = ""; percent = 0; ticker = nil; book = OrderBook(); tradeLast = 0
        if restart { await start() }
    }

    private func loadContract() async {
        if let list = try? await FuturesAPI.contracts() {
            contract = list.first { $0.symbol == symbol }
            maxLeverage = contract?.maxLeverage ?? 100
        }
        tiers = (try? await FuturesAPI.riskTiers(symbol))?.first { $0.symbol == symbol }?.tiers ?? []
    }

    func refreshMarket() async {
        do {
            async let t = FuturesAPI.ticker(symbol)
            async let b = FuturesAPI.depth(symbol)
            let nt = try await t
            if nt != ticker { ticker = nt }
            if let nb = try? await b, nb != book { book = nb }
            if marketError != nil { marketError = nil }
        } catch { if ticker == nil { marketError = error.lbMessage } }
    }

    func refreshAccount() async {
        if shotDemo { accountLoaded = true; algosLoaded = true; return }
        guard TokenStore.shared.isLoggedIn else {
            account = nil; positions = []; openOrders = []; tpslAlgos = []; planAlgos = []
            accountLoaded = true; algosLoaded = true; return
        }
        async let pm = try? FuturesAPI.positionMode()
        async let ta = FuturesAPI.tpslOrders()
        async let pa = FuturesAPI.planOrders()
        async let a = try? FuturesAPI.account()
        async let o = try? FuturesAPI.openOrders()
        async let l = try? FuturesAPI.leverage(symbol)
        async let bn = try? FuturesAPI.bonus()
        // 每 6 秒轮询一次。只在值变了时赋值：每次 @Published 赋值都会让整个合约页重算。
        if let acc = await a {
            setIfChanged(\.account, acc)
            setIfChanged(\.positions, (acc.positions ?? []).filter { Dec.parse($0.qty) != 0 })
        }
        if let list = await o { setIfChanged(\.openOrders, list) }
        if let lv = await l {
            setIfChanged(\.leverage, lv.leverage ?? leverage)
            setIfChanged(\.maxLeverage, lv.maxLeverage ?? maxLeverage)
            if let mm = lv.marginMode { setIfChanged(\.marginMode, mm == .isolated ? .isolated : .cross) }
        }
        setIfChanged(\.bonus, await bn)
        if let mode = await pm { setIfChanged(\.positionMode, mode) }
        do {
            let (t, pl) = try await (ta, pa)
            setIfChanged(\.tpslAlgos, t.orders ?? []); setIfChanged(\.planAlgos, pl.orders ?? []); setIfChanged(\.algosError, nil)
        } catch { if !algosLoaded { algosError = error.lbMessage } }
        setIfChanged(\.algosLoaded, true)
        setIfChanged(\.accountLoaded, true)
    }

    func setPercent(_ v: Double) {
        percent = v
        guard isOpen else { return }
        if v == 0 { costText = ""; return }
        switch orderUnit {
        case .cost: costText = Dec.plain(available * Decimal(v), 2)
        case .value: costText = Dec.plain(available * lev * Decimal(v), 2)
        case .qty: costText = Dec.plain(maxOpenQty * Decimal(v), qtyPrec)
        }
    }

    /// 切换单位时按当前价格和杠杆换算，仓位大小保持不变。
    func setOrderUnit(_ next: FuturesOrderUnit) {
        guard next != orderUnit else { return }
        if isOpen, orderPrice > 0, typed > 0 {
            let q = qty
            switch next {
            case .qty: costText = Dec.plain(q, qtyPrec)
            case .cost: costText = Dec.plain(q * orderPrice / lev, 2)
            case .value: costText = Dec.plain(q * orderPrice, 2)
            }
        }
        orderUnit = next
        percent = 0
    }

    var validation: String? {
        if kind == .plan, Dec.parse(triggerText) <= 0 { return "请输入触发价" }
        if kind != .market, !(kind == .plan && planMarket), Dec.parse(priceText) <= 0 { return "请输入价格" }
        if isOpen && typed <= 0 { return "请输入\(orderUnit.fieldLabel)" }
        if qty <= 0 || (minQty > 0 && qty < minQty) { return "数量低于最小下单量 \(Dec.plain(minQty, qtyPrec)) \(base)" }
        if isOpen && margin > available { return "可用保证金不足" }
        if !isOpen && closeTarget == nil { return "当前无持仓可平" }
        return nil
    }

    /// long=true：开多 / 平空（买）；false：开空 / 平多（卖）
    /// 分仓（HEDGE）：开多/平多 → LONG，开空/平空 → SHORT；平仓数量按对应方向仓位计算
    func place(long: Bool) async -> Bool {
        placing = true; defer { placing = false }
        let side: APIFuturesPlaceOrderRequest.APISide = long ? .buy : .sell
        var posSide: APIFuturesPlaceOrderRequest.APIPositionSide? = nil
        var q = qty
        if hedge {
            let isLongPos = isOpen ? long : !long
            posSide = isLongPos ? .long : .short
            if !isOpen {
                guard let pos = position(long: isLongPos) else { ToastCenter.shared.show("当前无\(isLongPos ? "多" : "空")仓可平", .error); return false }
                q = Dec.parse(Dec.plain(abs(Dec.parse(pos.qty)) * Decimal(percent > 0 ? percent : 1), qtyPrec))
            }
        }
        let isPlan = kind == .plan
        let marketLike = kind == .market || (isPlan && planMarket)
        let req = APIFuturesPlaceOrderRequest(
            symbol: symbol, side: side, type: marketLike ? .market : .limit,
            timeInForce: kind == .postOnly ? .postOnly : (marketLike ? .ioc : .gtc),
            price: marketLike ? nil : Dec.plain(Dec.parse(priceText), pricePrec),
            qty: Dec.plain(q, qtyPrec),
            // 分仓模式由 positionSide 决定开平，不再传 reduceOnly
            reduceOnly: isOpen || hedge ? nil : true,
            tpPrice: isOpen && tpsl && !tpText.isEmpty && !isPlan ? tpText : nil,
            slPrice: isOpen && tpsl && !slText.isEmpty && !isPlan ? slText : nil,
            tpTriggerPriceType: isOpen && tpsl && !tpText.isEmpty && !isPlan ? orderTpType : nil,
            slTriggerPriceType: isOpen && tpsl && !slText.isEmpty && !isPlan ? orderSlType : nil,
            positionSide: posSide,
            triggerPrice: isPlan ? Dec.plain(Dec.parse(triggerText), pricePrec) : nil,
            triggerPriceType: isPlan ? planTriggerType : nil)
        let label = isOpen ? (long ? "开多" : "开空") : (long ? "平空" : "平多")
        do {
            if isPlan {
                _ = try await FuturesAPI.createPlan(req)
                ToastCenter.shared.show("\(label)计划委托已提交", .success)
                triggerText = ""
            } else {
                _ = try await FuturesAPI.place(req)
                ToastCenter.shared.show("\(label)委托已提交", .success)
            }
            costText = ""; percent = 0
            await refreshAccount()
            return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    /// 切换持仓模式前的本地拦截：有仓位 / 普通委托 / 计划委托 / 止盈止损委托都不可切换（后端同样返回 409）
    var positionModeBlockReason: String? {
        var r: [String] = []
        if !positions.isEmpty { r.append("\(positions.count) 个仓位") }
        if !openOrders.isEmpty { r.append("\(openOrders.count) 笔挂单") }
        if !planAlgos.isEmpty { r.append("\(planAlgos.count) 笔计划委托") }
        if !tpslAlgos.isEmpty { r.append("\(tpslAlgos.count) 笔止盈止损委托") }
        return r.isEmpty ? nil : "当前存在\(r.joined(separator: "、"))，请先平仓并撤销全部委托后再切换仓位模式"
    }
    func setPositionMode(_ mode: APIFuturesPositionMode.APIPositionMode) async -> Bool {
        guard mode != positionMode else { return true }
        if let reason = positionModeBlockReason { ToastCenter.shared.show(reason, .error); return false }
        do {
            positionMode = try await FuturesAPI.setPositionMode(mode)
            ToastCenter.shared.show(mode == .hedge ? "已切换为分仓模式" : "已切换为合仓模式", .success)
            await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    /// 改单：price / qty 为空表示不修改；qty 为新的总数量（需大于已成交数量）
    func amend(_ o: APIFuturesOrder, price: String?, qty: String?) async -> Bool {
        guard let id = o.orderId else { return false }
        do {
            _ = try await FuturesAPI.amend(id, price: price, qty: qty)
            ToastCenter.shared.show("改单成功", .success); await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    /// 部分 / 全部止盈止损委托（POST /futures/tpsl-orders）
    func createTpslOrder(_ pos: APIFuturesPosition, qty: String?, tp: String?, tpType: APIFuturesTpslOrderRequest.APITpTriggerPriceType,
                         sl: String?, slType: APIFuturesTpslOrderRequest.APISlTriggerPriceType) async -> Bool {
        let req = APIFuturesTpslOrderRequest(
            symbol: pos.symbol ?? symbol,
            positionSide: Self.side(of: pos).flatMap(APIFuturesTpslOrderRequest.APIPositionSide.init(rawValue:)),
            qty: qty, tpTriggerPrice: tp, tpTriggerPriceType: tp == nil ? nil : tpType,
            slTriggerPrice: sl, slTriggerPriceType: sl == nil ? nil : slType)
        do {
            _ = try await FuturesAPI.createTpsl(req)
            ToastCenter.shared.show(qty == nil ? "止盈止损委托已提交" : "部分止盈止损已提交", .success)
            await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    /// 修改计划委托 / 止盈止损委托
    func amendAlgo(_ a: APIFuturesAlgoOrder, _ r: APIFuturesAlgoAmendRequest) async -> String? {
        do {
            _ = try await FuturesAPI.amendAlgo(a, r)
            ToastCenter.shared.show(a.algoType == .plan ? "计划委托已修改" : "止盈止损已修改", .success)
            await refreshAccount(); return nil
        } catch { return error.lbMessage }
    }

    func cancelAlgo(_ a: APIFuturesAlgoOrder) async {
        guard let id = a.algoId else { return }
        do {
            if a.algoType == .plan { try await FuturesAPI.cancelPlan(id) } else { try await FuturesAPI.cancelTpsl(id) }
            ToastCenter.shared.show("已撤销", .success); await refreshAccount()
        } catch { ToastCenter.shared.error(error) }
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

    struct CloseOutcome {
        var symbol: String
        var text: String
        /// 持仓数量比提交前减少。全部平掉也是 true。
        var reduced: Bool
    }

    func close(_ pos: APIFuturesPosition) async -> CloseOutcome {
        let sym = pos.symbol ?? symbol
        let key = Self.closeKey(pos, fallback: symbol)
        if closingKeys.contains(key) {
            return CloseOutcome(symbol: sym, text: "\(sym) 平仓正在处理", reduced: false)
        }
        closingKeys.insert(key)
        defer { closingKeys.remove(key) }
        let before = abs(Dec.parse(pos.qty))
        do {
            let resp = try await FuturesAPI.close(sym, positionSide: Self.side(of: pos))
            await refreshAccount()
            return Self.outcome(sym, before: before, after: qtyLeft(sym, Self.side(of: pos)), order: resp.order)
        } catch {
            await refreshAccount()
            let after = qtyLeft(sym, Self.side(of: pos))
            if after < before {
                let left = after == 0 ? "已平仓" : "剩余 \(Dec.plain(after, qtyPrec))"
                return CloseOutcome(symbol: sym, text: "\(sym) \(left)。请求结果未确认：\(error.lbMessage)", reduced: true)
            }
            return CloseOutcome(symbol: sym, text: "\(sym) 平仓失败：\(error.lbMessage)", reduced: false)
        }
    }

    func closeAll() async -> [CloseOutcome] {
        if closingAll { return [] }
        closingAll = true
        defer { closingAll = false }
        var out: [CloseOutcome] = []
        for p in shownPositions { out.append(await close(p)) }
        return out
    }

    private func qtyLeft(_ sym: String, _ side: String?) -> Decimal {
        let row = positions.first { ($0.symbol ?? "") == sym && Self.side(of: $0) == side }
        return abs(Dec.parse(row?.qty))
    }

    /// 以刷新后的持仓数量判断成交，不用「接口返回 200」代替平仓完成。
    static func outcome(_ sym: String, before: Decimal, after: Decimal, order: APIFuturesOrder?) -> CloseOutcome {
        let status = order?.status ?? ""
        let filled = Dec.parse(order?.filledQty)
        let why = cancelText(order?.cancelReason)
        if after == 0 && before > 0 {
            return CloseOutcome(symbol: sym, text: "\(sym) 已全部成交", reduced: true)
        }
        if after < before {
            return CloseOutcome(symbol: sym, text: "\(sym) 部分成交，剩余 \(Dec.plain(after, 8))。\(status == "canceled" ? why : "委托状态 \(status)")", reduced: true)
        }
        switch status {
        case "new", "partially_filled":
            return CloseOutcome(symbol: sym, text: "\(sym) 委托已受理，持仓尚未减少", reduced: false)
        case "canceled":
            return CloseOutcome(symbol: sym, text: "\(sym) 平仓未成交：\(why)", reduced: false)
        case "filled":
            if filled > 0 { return CloseOutcome(symbol: sym, text: "\(sym) 回报已成交，持仓仍在，请再看一次仓位", reduced: false) }
            fallthrough
        default:
            return CloseOutcome(symbol: sym, text: "\(sym) 平仓没有减少持仓", reduced: false)
        }
    }

    static func cancelText(_ reason: String?) -> String {
        switch reason {
        case "no_liquidity": return "盘口没有足够对手单"
        case "price_band": return "对手价偏离标记价超过保护范围"
        case "reduce_only": return "没有可减的仓位"
        case "self_trade_prevention": return "对手单是自己的挂单"
        case nil, "": return "未成交"
        default: return reason ?? "未成交"
        }
    }
    func cancel(_ o: APIFuturesOrder) async {
        guard let id = o.orderId else { return }
        do { try await FuturesAPI.cancel(id); ToastCenter.shared.show("已撤销", .success); await refreshAccount() }
        catch { ToastCenter.shared.error(error) }
    }
    /// 设置 / 修改持仓止盈止损。spec：传 "" 表示清除该项，不传（nil）表示保持不变。
    func setTpsl(_ pos: APIFuturesPosition, tp: String?, sl: String?, tpType: APIFuturesSetTpslRequest.APITpTriggerPriceType? = nil,
                 slType: APIFuturesSetTpslRequest.APISlTriggerPriceType? = nil, toast: String = "止盈止损已设置") async -> Bool {
        do {
            try await FuturesAPI.setTpsl(pos.symbol ?? symbol, tp: tp, sl: sl, positionSide: Self.side(of: pos), tpType: tpType, slType: slType)
            ToastCenter.shared.show(toast, .success); await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }
    /// 撤销止盈 / 止损（which: 0 全部，1 仅止盈，2 仅止损）
    func clearTpsl(_ pos: APIFuturesPosition, which: Int = 0) async -> Bool {
        await setTpsl(pos, tp: which == 2 ? nil : "", sl: which == 1 ? nil : "", toast: "已撤销止盈止损")
    }
    func adjustMargin(_ pos: APIFuturesPosition, amount: Decimal) async -> Bool {
        do {
            try await FuturesAPI.adjustMargin(pos.symbol ?? symbol, amount: NSDecimalNumber(decimal: amount).stringValue, positionSide: Self.side(of: pos))
            ToastCenter.shared.show(amount > 0 ? "保证金已增加" : "保证金已减少", .success); await refreshAccount(); return true
        } catch { ToastCenter.shared.error(error); return false }
    }
}

extension FuturesModel {
    func setIfChanged<T: Equatable>(_ kp: ReferenceWritableKeyPath<FuturesModel, T>, _ v: T) {
        if self[keyPath: kp] != v { self[keyPath: kp] = v }
    }
    /// 指定杠杆下可持有的最大名义价值：取 maxLeverage ≥ lev 的最高档上限（Decimal）
    func maxNotional(forLeverage lev: Int) -> Decimal? {
        let ok = tiers.filter { ($0.maxLeverage ?? 0) >= lev }
        guard let t = ok.max(by: { Dec.parse($0.maxNotional) < Dec.parse($1.maxNotional) }) else { return nil }
        return Dec.parse(t.maxNotional)
    }
}
