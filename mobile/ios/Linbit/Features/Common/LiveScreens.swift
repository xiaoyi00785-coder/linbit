import SwiftUI

struct SpotKline: Decodable {
    var open, high, low, close: String?
}

/// 现货页内展开的 K 线，数据来自 /market/klines。
struct SpotChartPanel: View {
    @Environment(\.palette) private var p
    let symbol: String
    let pricePrec: Int
    @State private var interval = "15m"
    @State private var candles: [APIFuturesKlinesKlinesInner] = []
    @State private var loaded = false
    @State private var error: String?
    private let intervals = ["15m", "1h", "4h", "1d"]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 14) {
                ForEach(intervals, id: \.self) { iv in
                    Text(iv).font(.lb(FontSize.small, interval == iv ? .semibold : .regular))
                        .foregroundColor(interval == iv ? p.textPrimary : p.textSecondary)
                        .onTapGesture { interval = iv; Task { await load() } }
                }
                Spacer()
            }
            if let error {
                ErrorStateView(message: error) { Task { await load() } }.frame(height: 180)
            } else if !loaded {
                ProgressView().frame(maxWidth: .infinity).frame(height: 180)
            } else if candles.isEmpty {
                EmptyStateView(text: "暂无 K 线").frame(height: 180)
            } else {
                CandleChart(candles: candles, pricePrec: pricePrec).frame(height: 180)
            }
        }
        .task(id: "\(symbol)-\(interval)") { await load() }
    }

    private func load() async {
        error = nil
        do {
            let rows: [SpotKline] = try await APIClient.shared.get("/market/klines", query: ["symbol": symbol, "interval": interval, "limit": "120"])
            candles = rows.map { APIFuturesKlinesKlinesInner(_open: $0.open, high: $0.high, low: $0.low, close: $0.close) }
            loaded = true
            error = nil
        } catch {
            self.error = error.lbMessage
        }
    }
}

struct SpotKlineSheet: View {
    @Environment(\.palette) private var p
    let symbol: String
    let onClose: () -> Void
    @State private var interval = "15m"
    @State private var candles: [APIFuturesKlinesKlinesInner] = []
    @State private var error: String?
    private let intervals = ["15m", "1h", "4h", "1d"]

    var body: some View {
        LBSheet(title: "\(symbol) K 线", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    ForEach(intervals, id: \.self) { iv in
                        Text(iv).font(.lb(FontSize.small, interval == iv ? .semibold : .regular))
                            .foregroundColor(interval == iv ? p.textPrimary : p.textSecondary)
                            .onTapGesture { interval = iv; Task { await load() } }
                    }
                }
                if let error {
                    ErrorStateView(message: error) { Task { await load() } }.frame(height: 180)
                } else if candles.isEmpty {
                    ProgressView().frame(height: 180)
                } else {
                    CandleChart(candles: candles, pricePrec: 2).frame(height: 220)
                }
            }.padding(.horizontal, Spacing.pageH)
        }
        .task { await load() }
    }

    private func load() async {
        do {
            let rows: [SpotKline] = try await APIClient.shared.get("/market/klines", query: ["symbol": symbol, "interval": interval, "limit": "120"])
            candles = rows.map { APIFuturesKlinesKlinesInner(_open: $0.open, high: $0.high, low: $0.low, close: $0.close) }
            error = nil
        } catch {
            self.error = error.lbMessage
        }
    }
}

struct PaperAccountDTO: Decodable {
    var balance, available, unrealizedPnl, positionMode: String?
}

private struct PaperRow: Decodable, Identifiable {
    var symbol, positionSide, quantity, entryPrice, unrealizedPnl, status, side, type: String?
    var id: String { (symbol ?? "") + (positionSide ?? "") + (status ?? "") + (side ?? "") + (entryPrice ?? "") }
}
private struct PaperList: Decodable { var items: [PaperRow]? }

/// 模拟盘。布局对照参考 App：BTCUSDX 永续、成本用 USDX、右侧是对应实盘盘口，退出回到合约。
struct PaperFuturesScreen: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @StateObject private var live = MarketTape(symbol: "BTCUSDT", futures: true)
    @State private var account: PaperAccountDTO?
    @State private var positions: [PaperRow] = []
    @State private var orders: [PaperRow] = []
    @State private var cost = ""
    @State private var price = ""
    @State private var opening = true
    @State private var market = true
    @State private var showChart = false
    @State private var tab = 0
    @State private var error: String?
    @State private var pending: String?

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("模拟交易").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                Spacer()
                Button("退出") { Router.shared.futuresTop = 0 }
                    .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
            }.padding(.horizontal, Spacing.pageH).frame(height: 44)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("BTCUSDX").font(.lb(FontSize.symbol, .semibold))
                            Text("永续  \(Fmt.percent(live.change))").font(.lbNum(FontSize.small)).foregroundColor(live.change < 0 ? p.down : p.up)
                        }
                        Spacer()
                        VStack(alignment: .trailing, spacing: 2) {
                            Text("资金费率 / 倒计时").font(.lb(10)).foregroundColor(p.textTertiary)
                            Text(live.last > 0 ? Fmt.price(live.last, precision: live.pricePrec) : "--").font(.lbNum(FontSize.small))
                        }
                    }
                    HStack(spacing: 8) {
                        Text(account?.positionMode == "hedge" ? "全仓 / 分仓" : "全仓 / 合仓")
                            .font(.lb(FontSize.small)).padding(.horizontal, 8).frame(height: 28)
                            .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                        Text("100x").font(.lbNum(FontSize.small)).padding(.horizontal, 8).frame(height: 28)
                            .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                    }
                    HStack(alignment: .top, spacing: 10) {
                        VStack(alignment: .leading, spacing: 8) {
                            Picker("", selection: $opening) {
                                Text("开仓").tag(true)
                                Text("平仓").tag(false)
                            }.pickerStyle(.segmented)
                            Button { market.toggle() } label: {
                                HStack { Text(market ? "市价单" : "限价单"); Spacer(); Image(systemName: "chevron.down").font(.system(size: 9)) }
                                    .font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                                    .padding(.horizontal, 10).frame(height: 36)
                                    .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                            }.buttonStyle(.plain)
                            if !market { NumberBox(label: "价格", unit: "USDX", text: $price, onEdit: {}) }
                            NumberBox(label: opening ? "成本" : "数量", unit: opening ? "USDX" : "BTC", text: $cost, onEdit: {})
                            if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                            HStack { Text("可用").foregroundColor(p.textSecondary); Spacer(); Text("\(account?.available ?? "--") USDX") }
                            HStack { Text("可买").foregroundColor(p.textSecondary); Spacer(); Text(canOpen) }
                            HStack { Text("可卖").foregroundColor(p.textSecondary); Spacer(); Text(canOpen) }
                            Button { pending = opening ? "buy" : "close-long" } label: {
                                Text(opening ? "开多\n-- USDX" : "平多").font(.lb(FontSize.body, .semibold)).multilineTextAlignment(.center)
                                    .frame(maxWidth: .infinity).frame(height: 44).foregroundColor(.white)
                                    .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.up))
                            }.buttonStyle(.plain)
                            Button { pending = opening ? "sell" : "close-short" } label: {
                                Text(opening ? "开空\n-- USDX" : "平空").font(.lb(FontSize.body, .semibold)).multilineTextAlignment(.center)
                                    .frame(maxWidth: .infinity).frame(height: 44).foregroundColor(.white)
                                    .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.down))
                            }.buttonStyle(.plain)
                            Text("预估强平价  --").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }.font(.lb(FontSize.small))
                        VStack(spacing: 2) {
                            HStack { Text("价格 (USDX)"); Spacer(); Text("数量 (USDX)") }.font(.lb(10)).foregroundColor(p.textSecondary)
                            ForEach(live.book.asks.prefix(12).reversed()) { row in
                                HStack { Text(Fmt.price(row.price, precision: live.pricePrec)).foregroundColor(p.down); Spacer(); Text(Fmt.compact(row.price * row.qty)) }
                            }
                            Text(live.last > 0 ? Fmt.price(live.last, precision: live.pricePrec) : "--").font(.lbNum(16, .bold))
                            ForEach(live.book.bids.prefix(12)) { row in
                                HStack { Text(Fmt.price(row.price, precision: live.pricePrec)).foregroundColor(p.up); Spacer(); Text(Fmt.compact(row.price * row.qty)) }
                            }
                        }.font(.lbNum(11)).frame(width: 148)
                    }
                    Button { showChart.toggle() } label: {
                        HStack { Text("BTCUSDX 图表"); Spacer(); Image(systemName: showChart ? "chevron.up" : "chevron.down") }
                            .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }.buttonStyle(.plain)
                    if showChart { CandleChart(bars: live.bars, pricePrec: live.pricePrec) }
                    Picker("", selection: $tab) {
                        Text("仓位 (\(positions.count))").tag(0)
                        Text("委托 (\(orders.count))").tag(1)
                        Text("跟单 (0)").tag(2)
                    }.pickerStyle(.segmented)
                    if tab == 0 {
                        if positions.isEmpty { EmptyStateView(text: session.isLoggedIn ? "暂无仓位" : "登录后查看模拟仓位") }
                        ForEach(positions) { r in
                            HStack {
                                Text(r.symbol ?? "BTCUSDX"); Spacer()
                                Text(r.quantity ?? "--"); Text(r.unrealizedPnl ?? "--").foregroundColor((r.unrealizedPnl ?? "").hasPrefix("-") ? p.down : p.up)
                            }.font(.lbNum(12))
                        }
                    } else if tab == 1 {
                        if orders.isEmpty { EmptyStateView(text: "暂无委托") }
                        ForEach(orders) { r in
                            HStack { Text(r.symbol ?? ""); Spacer(); Text(r.side ?? ""); Text(r.quantity ?? "") }.font(.lbNum(12))
                        }
                    } else {
                        EmptyStateView(text: "模拟盘不提供跟单")
                    }
                    if !positions.isEmpty {
                        Button { pending = "close-all" } label: { Text("一键平仓").font(.lb(FontSize.small)).foregroundColor(p.down) }.buttonStyle(.plain)
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, Metrics.tabBarH + 24)
            }
        }
        .task { await reload() }
        .liveWhileVisible(.futures, start: { await live.start() }, stop: { live.stop() })
        .lbSheet(Binding(get: { pending != nil }, set: { if !$0 { pending = nil } })) {
            LBSheet(title: pending == "close-all" ? "一键平仓" : (pending ?? "").hasPrefix("close") ? "确认平仓" : (pending == "sell" ? "确认开空" : "确认开多"), onClose: { pending = nil }) {
                VStack(alignment: .leading, spacing: 12) {
                    Text("BTCUSDX · \(market ? "市价" : "限价 \(price)") · \(opening ? "成本" : "数量") \(cost.isEmpty ? "--" : cost)。只动用模拟 USDX。")
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    LBButton(title: "确认", enabled: session.isLoggedIn && (pending == "close-all" || Dec.parse(cost) > 0)) {
                        let action = pending ?? ""
                        pending = nil
                        Task { await submit(action) }
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 16)
            }
        }
    }

    private var canOpen: String {
        guard live.last > 0, Dec.parse(cost) > 0 else { return "-- USDX" }
        return Fmt.amount(Dec.parse(cost), 2) + " USDX"
    }

    private func reload() async {
        guard session.isLoggedIn else { account = nil; positions = []; orders = []; return }
        do {
            account = try await APIClient.shared.get("/paper/futures/account", auth: true)
            let pos: PaperList = try await APIClient.shared.get("/paper/futures/positions", auth: true)
            let ord: PaperList = try await APIClient.shared.get("/paper/futures/orders", query: ["status": "open"], auth: true)
            positions = pos.items ?? []
            orders = ord.items ?? []
            error = nil
        } catch { self.error = error.lbMessage }
    }

    private func submit(_ action: String) async {
        struct Body: Encodable { let symbol, side, type, action, quantity, price, sizeMode: String }
        if action == "close-all" {
            struct Empty: Encodable {}
            do { let _: JSONValue = try await APIClient.shared.post("/paper/futures/positions/close-all", body: Empty(), auth: true); await reload() }
            catch { self.error = error.lbMessage }
            return
        }
        let px = market ? live.last : Dec.parse(price)
        let qty = px > 0 && opening ? Dec.parse(cost) / px : Dec.parse(cost)
        let side = action == "sell" || action == "close-long" ? (action == "close-long" ? "sell" : "sell") : "buy"
        let act = action.hasPrefix("close") ? "close" : "open"
        do {
            let _: JSONValue = try await APIClient.shared.post("/paper/futures/orders", body: Body(symbol: "BTCUSDX", side: side, type: market ? "market" : "limit", action: act, quantity: NSDecimalNumber(decimal: qty).stringValue, price: market ? "" : price, sizeMode: opening ? "quantity" : "quantity"), auth: true)
            await reload()
        } catch { self.error = error.lbMessage }
    }
}

/// 价格预警。接口只接受现货、INDEX、一次、站内，条件只有 gte / lte。
enum PriceAlertAPI {
    struct Item: Decodable, Identifiable {
        var alertId: String
        var symbol: String
        var condition: String
        var threshold: String
        var triggerMode: String?
        var status: String
        var revision: Int64
        var id: String { alertId }
    }
    struct List: Decodable { var items: [Item] }
    struct Create: Encodable {
        let clientAlertId, market, symbol, condition, threshold, priceSource, triggerMode: String
        let channels: [String]
    }

    static func list(symbol: String?) async throws -> [Item] {
        let r: List = try await APIClient.shared.get("/price-alerts", query: ["symbol": symbol, "pageSize": "50"], auth: true)
        return r.items
    }

    /// 去掉千分位后的十进制字符串。不是数字时返回 nil，不发给接口。
    static func threshold(_ text: String) -> String? {
        let raw = text.replacingOccurrences(of: ",", with: "").trimmingCharacters(in: .whitespaces)
        guard !raw.isEmpty, Decimal(string: raw, locale: Locale(identifier: "en_US_POSIX")) != nil else { return nil }
        return raw
    }

    static func create(symbol: String, condition: String, threshold: String, mode: String = "once") async throws {
        guard let raw = Self.threshold(threshold) else { return }
        let chosen = ["once", "daily", "repeat"].contains(mode) ? mode : "once"
        let _: JSONValue = try await APIClient.shared.post("/price-alerts", body: Create(
            clientAlertId: UUID().uuidString, market: "spot", symbol: symbol, condition: condition,
            threshold: raw, priceSource: "INDEX", triggerMode: chosen, channels: ["in_app"]))
    }

    static func delete(_ id: String) async throws {
        try await APIClient.shared.deleteAuth("/price-alerts/\(id)")
    }
}

struct PriceAlertSheet: View {
    @Environment(\.palette) private var p
    let symbol: String
    let onClose: () -> Void
    @State private var price = ""
    @State private var error: String?

    var body: some View {
        LBSheet(title: "价格预警", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                Text("\(symbol) 现货指数价上穿后，站内提醒一次").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                LBTextField(placeholder: "触发价格", text: $price, keyboard: .decimalPad)
                if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                LBButton(title: "创建预警", enabled: !price.trimmingCharacters(in: .whitespaces).isEmpty) { Task { await create() } }
            }.padding(.horizontal, Spacing.pageH)
        }
    }

    private func create() async {
        guard PriceAlertAPI.threshold(price) != nil else { error = "请输入数字价格"; return }
        do {
            try await PriceAlertAPI.create(symbol: symbol, condition: "gte", threshold: price)
            onClose()
        } catch { self.error = error.lbMessage }
    }
}

struct LiveTrade: Identifiable, Hashable {
    var tradeId: String
    var price: String
    var quantity: String
    var side: String
    var time: Date
    var id: String { tradeId }
}

/// 币种详情行情。现货用 /market 与 kline、depth、trades；合约用 futures-kline、futures-depth、futures-trades。
@MainActor
final class MarketTape: ObservableObject {
    let symbol: String
    let futures: Bool
    @Published var last: Decimal = 0
    @Published var change: Decimal = 0
    @Published var high: Decimal = 0
    @Published var low: Decimal = 0
    @Published var volume: Decimal = 0
    @Published var quoteVolume: Decimal = 0
    @Published var bars: [ChartBar] = []
    @Published var book = OrderBook()
    @Published var trades: [LiveTrade] = []
    @Published var barError: String?
    @Published var loadingBars = false
    @Published var lastEvent: Date?
    @Published private(set) var interval = "15m"
    var pricePrec: Int { MarketStore.shared.meta(symbol)?.pricePrecision ?? (futures ? 1 : 2) }
    var qtyPrec: Int { MarketStore.shared.meta(symbol)?.quantityPrecision ?? (futures ? 3 : 5) }
    private static var barCache: [String: [ChartBar]] = [:]
    private var cacheKey: String { "\(futures ? "f" : "s")|\(symbol)|\(interval)" }

    private var subs: [WSSubscription] = []
    private var klineSub: WSSubscription?
    private var barToken = UUID()
    /// 盘口走 BookStream（固定 0.5 秒节拍）；成交和 ticker 帧先攒着，同样按节拍合并发布一次。
    /// 之前每来一帧就改一次 @Published，一阵帧到达时整页（含 K 线）要连着重画十几次。
    private let bookStream = BookStream()
    private var pendingTrades: [LiveTrade] = []
    private var pendingTicker: WSTicker?
    private var pendingBars: [Int64: ChartBar] = [:]
    private var flush: Task<Void, Never>?

    init(symbol: String, futures: Bool, interval: String = "15m") {
        self.symbol = symbol
        self.futures = futures
        self.interval = interval
    }

    func start() async {
        stop()
        let rows = futures ? MarketStore.shared.futures : MarketStore.shared.spot
        if let row = rows.first(where: { $0.symbol == symbol }) {
            last = row.last
            change = row.changePct
        }
        if bars.isEmpty, let cached = Self.barCache[cacheKey] { bars = cached }
        bookStream.onPublish = { [weak self] b in self?.book = b }
        bookStream.start(symbol: symbol, futures: futures)
        async let barsTask: Void = reloadBars()
        async let tickerTask: Void = loadTicker()
        async let tradeTask: Void = loadTrades()
        _ = await (barsTask, tickerTask, tradeTask)
        if Task.isCancelled { return }
        subscribeStreams()
    }

    func stop() {
        barToken = UUID()
        klineSub?.cancel(); klineSub = nil
        subs.forEach { $0.cancel() }
        subs = []
        bookStream.stop()
        flush?.cancel(); flush = nil
        pendingTrades = []; pendingTicker = nil; pendingBars = [:]
    }

    private func scheduleFlush() {
        guard flush == nil else { return }
        flush = Task { [weak self] in
            try? await Task.sleep(nanoseconds: BookStream.tickNanos)
            guard let self, !Task.isCancelled else { return }
            self.flush = nil
            if !self.pendingBars.isEmpty {
                let bars = self.pendingBars.values.sorted { $0.time < $1.time }
                self.pendingBars = [:]
                for b in bars { self.upsert(b) }
            }
            if let t = self.pendingTicker {
                self.pendingTicker = nil
                self.applyTicker(last: Dec.parse(t.lastPrice), change: Dec.parse(t.priceChangePercent), high: Dec.parse(t.high), low: Dec.parse(t.low), volume: Dec.parse(t.volume), quote: Dec.parse(t.quoteVolume))
            }
            if !self.pendingTrades.isEmpty {
                let batch = self.pendingTrades
                self.pendingTrades = []
                var next = self.trades
                for t in batch where !next.contains(where: { $0.tradeId == t.tradeId }) { next.insert(t, at: 0) }
                if next.count > 40 { next.removeLast(next.count - 40) }
                if next != self.trades { self.trades = next }
                // 详情页大字最新价跟着成交走（合约 ticker 帧不带涨跌幅，解不出来）
                if let px = batch.last.map({ Dec.parse($0.price) }), px > 0, px != self.last { self.last = px }
            }
        }
    }

    func setInterval(_ next: String) {
        guard next != interval else { return }
        interval = next
        bars = []
        pendingBars = [:]
        Task { await reloadBars() }
    }

    private func reloadBars() async {
        let token = UUID()
        barToken = token
        klineSub?.cancel(); klineSub = nil
        loadingBars = bars.isEmpty
        let iv = interval
        do {
            let loaded: [ChartBar]
            if futures {
                loaded = try await FuturesAPI.klines(symbol, interval: iv, limit: 500).compactMap(Self.futuresBar).sorted { $0.time < $1.time }
            } else {
                let rows: [APIKline] = try await APIClient.shared.get("/market/klines", query: ["symbol": symbol, "interval": iv, "limit": "500"])
                loaded = rows.map { ChartBar(time: Self.secs(Int64($0.time)), open: Dec.parse($0._open), high: Dec.parse($0.high), low: Dec.parse($0.low), close: Dec.parse($0.close), volume: Dec.parse($0.volume)) }.sorted { $0.time < $1.time }
            }
            guard token == barToken else { return }
            bars = loaded
            Self.barCache[cacheKey] = loaded
            barError = nil
        } catch {
            guard token == barToken else { return }
            barError = error.lbMessage
        }
        loadingBars = false
        guard token == barToken else { return }
        let channel = futures ? "futures-kline:\(symbol):\(iv)" : "kline:\(symbol):\(iv)"
        klineSub = MarketSocket.shared.subscribe(channel) { [weak self] frame in
            guard let bar = Self.parseBar(frame.data) else { return }
            Task { @MainActor in
                guard let self, token == self.barToken else { return }
                self.pendingBars[bar.time] = bar
                self.scheduleFlush()
            }
        }
    }

    private func loadTicker() async {
        if futures, let t = try? await FuturesAPI.ticker(symbol) {
            let pct = t.priceChangePercent.map(Dec.parse) ?? Dec.parse(t.priceChangePercent24h) * 100
            applyTicker(last: t.effectivePrice(), change: pct, high: Dec.parse(t.high ?? t.highPrice24h), low: Dec.parse(t.low ?? t.lowPrice24h), volume: Dec.parse(t.volume ?? t.volume24h), quote: Dec.parse(t.quoteVolume ?? t.turnover24h))
        } else if let r: APIGetTicker200Response = try? await APIClient.shared.get("/market/ticker", query: ["symbol": symbol]), case let .typeAPITicker(t) = r {
            applyTicker(last: Dec.parse(t.lastPrice), change: Dec.parse(t.priceChangePercent), high: Dec.parse(t.high), low: Dec.parse(t.low), volume: Dec.parse(t.volume), quote: Dec.parse(t.quoteVolume))
        }
    }

    private func loadTrades() async {
        if futures {
            let rows = (try? await FuturesAPI.marketTrades(symbol, limit: 40)) ?? []
            trades = rows.compactMap { t in
                guard let id = t.tradeId else { return nil }
                return LiveTrade(tradeId: String(id), price: t.price ?? "0", quantity: t.qty ?? "0", side: t.takerSide?.rawValue ?? "buy", time: t.time ?? Date())
            }
        } else if let rows: [RawTape] = try? await APIClient.shared.get("/market/trades", query: ["symbol": symbol, "limit": "40"]) {
            trades = rows.map(\.live)
        }
    }

    private func subscribeStreams() {
        let tickerCh = futures ? "futures-ticker:\(symbol)" : "ticker:\(symbol)"
        let tradeCh = futures ? "futures-trades:\(symbol)" : "trades:\(symbol)"
        subs.append(MarketSocket.shared.subscribe(tickerCh) { [weak self] frame in
            guard let t = try? CodableHelper.jsonDecoder.decode(WSTicker.self, from: frame.data) else { return }
            Task { @MainActor in self?.pendingTicker = t; self?.scheduleFlush() }
        })
        subs.append(MarketSocket.shared.subscribe(tradeCh) { [weak self] frame in
            guard let raw = try? CodableHelper.jsonDecoder.decode(RawTape.self, from: frame.data) else { return }
            Task { @MainActor in
                guard let self else { return }
                self.pendingTrades.append(raw.live)
                if self.pendingTrades.count > 80 { self.pendingTrades.removeFirst(self.pendingTrades.count - 80) }
                self.scheduleFlush()
            }
        })
    }

    private func applyTicker(last: Decimal, change: Decimal, high: Decimal, low: Decimal, volume: Decimal, quote: Decimal) {
        if last > 0 { self.last = last }
        self.change = change
        if high > 0 { self.high = high }
        if low > 0 { self.low = low }
        if volume > 0 { self.volume = volume }
        if quote > 0 { self.quoteVolume = quote }
    }

    private func upsert(_ b: ChartBar) {
        if let i = bars.lastIndex(where: { $0.time == b.time }) {
            var cur = bars[i]
            cur.high = max(cur.high, b.high)
            cur.low = min(cur.low, b.low)
            cur.close = b.close
            cur.open = b.open
            cur.volume = b.volume
            bars[i] = cur
        } else if bars.last.map({ $0.time < b.time }) ?? true {
            bars.append(b)
        }
    }

    private static func secs(_ t: Int64) -> Int64 { t > 1_000_000_000_000 ? t / 1000 : t }
    private static func futuresBar(_ row: APIFuturesKlinesKlinesInner) -> ChartBar? {
        guard let t = row.openTime else { return nil }
        return ChartBar(time: secs(t), open: Dec.parse(row._open), high: Dec.parse(row.high), low: Dec.parse(row.low), close: Dec.parse(row.close), volume: Dec.parse(row.volume))
    }
    private static func parseBar(_ data: Data) -> ChartBar? {
        guard let row = try? JSONDecoder().decode(FlexKline.self, from: data), row.time > 0, Dec.parse(row.close) > 0 else { return nil }
        return ChartBar(time: secs(row.time), open: Dec.parse(row.open), high: Dec.parse(row.high), low: Dec.parse(row.low), close: Dec.parse(row.close), volume: Dec.parse(row.volume))
    }
}

private struct FlexKline: Decodable {
    var time: Int64
    var open, high, low, close, volume: String
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        time = (try? c.decode(Int64.self, forKey: .time)) ?? (try? c.decode(Int64.self, forKey: .openTime)) ?? 0
        open = (try? c.decode(String.self, forKey: .open)) ?? "0"
        high = (try? c.decode(String.self, forKey: .high)) ?? open
        low = (try? c.decode(String.self, forKey: .low)) ?? open
        close = (try? c.decode(String.self, forKey: .close)) ?? open
        volume = (try? c.decode(String.self, forKey: .volume)) ?? "0"
    }
    private enum CodingKeys: String, CodingKey { case time, openTime, open, high, low, close, volume }
}


private struct RawTape: Decodable {
    var tradeId: String
    var price: String
    var quantity: String
    var side: String
    var time: Int64
    var live: LiveTrade {
        LiveTrade(tradeId: tradeId, price: price, quantity: quantity, side: side, time: time > 10_000_000_000 ? Date(timeIntervalSince1970: Double(time) / 1000) : Date(timeIntervalSince1970: Double(time)))
    }
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        if let s = try? c.decode(String.self, forKey: .tradeId) { tradeId = s }
        else if let n = try? c.decode(Int64.self, forKey: .tradeId) { tradeId = String(n) }
        else { tradeId = UUID().uuidString }
        price = (try? c.decode(String.self, forKey: .price)) ?? "0"
        quantity = (try? c.decode(String.self, forKey: .quantity)) ?? (try? c.decode(String.self, forKey: .qty)) ?? "0"
        side = (try? c.decode(String.self, forKey: .side)) ?? (try? c.decode(String.self, forKey: .takerSide)) ?? "buy"
        time = (try? c.decode(Int64.self, forKey: .time)) ?? 0
    }
    private enum CodingKeys: String, CodingKey { case tradeId, price, quantity, qty, side, takerSide, time }
}
