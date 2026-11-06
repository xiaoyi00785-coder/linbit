import SwiftUI

private func msDate(_ ms: Int64?) -> Date? { ms.map { Date(timeIntervalSince1970: TimeInterval($0) / 1000) } }

// MARK: - K 线 / 最新成交（合约页「图表」展开区，/futures/market/klines、/futures/market/trades）
struct FuturesChartPanel: View {
    @Environment(\.palette) private var p
    let symbol: String
    let pricePrec: Int
    @State private var tab = 0
    @State private var interval = "15m"
    @State private var candles: Loadable<[APIFuturesKlinesKlinesInner]> = .idle
    @State private var trades: Loadable<[APIFuturesPublicTrade]> = .idle
    @State private var candleSeq = 0
    @State private var tradeSeq = 0
    private let intervals: [(String, String)] = [("15m", "15分"), ("1h", "1小时"), ("4h", "4小时"), ("1d", "1天"), ("1w", "1周")]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 16) {
                tabText("K线", 0); tabText("成交", 1)
                Spacer()
            }
            if tab == 0 {
                HStack(spacing: 14) {
                    ForEach(intervals, id: \.0) { k, t in
                        Text(t).font(.lb(FontSize.small, interval == k ? .semibold : .regular))
                            .foregroundColor(interval == k ? p.textPrimary : p.textSecondary)
                            .onTapGesture { interval = k; Task { await loadCandles() } }
                    }
                    Spacer()
                }
                switch candles {
                case .idle, .loading: RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill).frame(height: 180).overlay(ProgressView())
                case let .failed(msg): ErrorStateView(message: msg) { Task { await loadCandles() } }.frame(height: 180)
                case let .loaded(list):
                    if list.isEmpty { EmptyStateView(text: "暂无 K 线数据").frame(height: 180) }
                    else { CandleChart(candles: list, pricePrec: pricePrec).frame(height: 180) }
                }
            } else {
                tradesList
            }
        }
        .task(id: symbol) { await loadCandles(); await loadTrades() }
    }
    private func tabText(_ t: String, _ i: Int) -> some View {
        Text(t).font(.lb(FontSize.label, tab == i ? .semibold : .regular)).foregroundColor(tab == i ? p.textPrimary : p.textSecondary)
            .onTapGesture { tab = i; if i == 1 { Task { await loadTrades() } } }
    }
    @ViewBuilder private var tradesList: some View {
        switch trades {
        case .idle, .loading: SkeletonList(rows: 4)
        case let .failed(msg): ErrorStateView(message: msg) { Task { await loadTrades() } }
        case let .loaded(list):
            if list.isEmpty { EmptyStateView(text: "暂无成交") } else {
                VStack(spacing: 6) {
                    HStack {
                        Text("时间").frame(maxWidth: .infinity, alignment: .leading)
                        Text("价格(USDT)").frame(maxWidth: .infinity, alignment: .center)
                        Text("数量").frame(maxWidth: .infinity, alignment: .trailing)
                    }.font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    ForEach(list.prefix(20), id: \.self) { t in
                        HStack {
                            Text(t.time.map(Self.hms) ?? "--").foregroundColor(p.textSecondary).frame(maxWidth: .infinity, alignment: .leading)
                            Text(Fmt.price(Dec.parse(t.price), precision: pricePrec)).foregroundColor(t.takerSide == .sell ? p.down : p.up)
                                .frame(maxWidth: .infinity, alignment: .center)
                            Text(Fmt.compact(Dec.parse(t.qty))).foregroundColor(p.textPrimary).frame(maxWidth: .infinity, alignment: .trailing)
                        }.font(.lbNum(FontSize.small))
                    }
                }
            }
        }
    }
    static func hms(_ d: Date) -> String {
        let f = DateFormatter(); f.locale = Locale(identifier: "en_US_POSIX"); f.dateFormat = "HH:mm:ss"; return f.string(from: d)
    }
    private func loadCandles() async {
        candleSeq &+= 1
        let seq = candleSeq, sym = symbol, iv = interval
        // 切交易对/周期时立即离开旧图，避免旧响应覆盖前短暂展示错币种
        candles = .loading
        do {
            let list = try await FuturesAPI.klines(sym, interval: iv)
            guard seq == candleSeq, sym == symbol, iv == interval else { return }
            candles = .loaded(list)
        } catch {
            guard seq == candleSeq, sym == symbol, iv == interval else { return }
            candles = .failed(error.lbMessage)
        }
    }
    private func loadTrades() async {
        tradeSeq &+= 1
        let seq = tradeSeq, sym = symbol
        trades = .loading
        do {
            let list = try await FuturesAPI.marketTrades(sym)
            guard seq == tradeSeq, sym == symbol else { return }
            trades = .loaded(list)
        } catch {
            guard seq == tradeSeq, sym == symbol else { return }
            trades = .failed(error.lbMessage)
        }
    }
}

/// 统一蜡烛数据（现货 APIKline / 合约 FuturesKline 共用绘制）
struct CandlePoint: Hashable {
    var open: String, high: String, low: String, close: String
    init(open: String, high: String, low: String, close: String) {
        self.open = open; self.high = high; self.low = low; self.close = close
    }
    init(_ c: APIFuturesKlinesKlinesInner) { self.init(open: c._open ?? "0", high: c.high ?? "0", low: c.low ?? "0", close: c.close ?? "0") }
    init(_ c: APIKline) { self.init(open: c._open, high: c.high, low: c.low, close: c.close) }
}

/// 蜡烛图：价格以 Decimal 解析，仅在绘制坐标时转 Double
struct CandleChart: View {
    @Environment(\.palette) private var p
    let candles: [CandlePoint]
    let pricePrec: Int
    init(candles: [CandlePoint], pricePrec: Int) { self.candles = candles; self.pricePrec = pricePrec }
    init(candles: [APIFuturesKlinesKlinesInner], pricePrec: Int) { self.init(candles: candles.map(CandlePoint.init), pricePrec: pricePrec) }
    init(spot candles: [APIKline], pricePrec: Int) { self.init(candles: candles.map(CandlePoint.init), pricePrec: pricePrec) }
    var body: some View {
        let hi = candles.map { Dec.parse($0.high) }.max() ?? 0
        let lo = candles.map { Dec.parse($0.low) }.min() ?? 0
        ZStack(alignment: .topLeading) {
            Canvas { ctx, size in
                let n = max(candles.count, 1)
                let step = size.width / CGFloat(n)
                let range = Dec.double(hi - lo)
                func y(_ d: Decimal) -> CGFloat { range <= 0 ? size.height / 2 : CGFloat(Dec.double(hi - d) / range) * (size.height - 8) + 4 }
                for gy in stride(from: 0.0, through: 1.0, by: 0.25) {
                    var path = Path(); path.move(to: CGPoint(x: 0, y: size.height * gy)); path.addLine(to: CGPoint(x: size.width, y: size.height * gy))
                    ctx.stroke(path, with: .color(p.divider), lineWidth: 0.5)
                }
                for (i, c) in candles.enumerated() {
                    let o = Dec.parse(c.open), cl = Dec.parse(c.close), h = Dec.parse(c.high), l = Dec.parse(c.low)
                    let color = cl >= o ? p.up : p.down
                    let x = step * (CGFloat(i) + 0.5)
                    var wick = Path(); wick.move(to: CGPoint(x: x, y: y(h))); wick.addLine(to: CGPoint(x: x, y: y(l)))
                    ctx.stroke(wick, with: .color(color), lineWidth: 1)
                    let top = y(max(o, cl)), bot = y(min(o, cl))
                    ctx.fill(Path(CGRect(x: x - max(step * 0.35, 0.5), y: top, width: max(step * 0.7, 1), height: max(bot - top, 1))), with: .color(color))
                }
            }
            VStack(alignment: .trailing) {
                Text(Fmt.price(hi, precision: pricePrec)); Spacer(); Text(Fmt.price(lo, precision: pricePrec))
            }.font(.lbNum(10)).foregroundColor(p.textTertiary).frame(maxWidth: .infinity, alignment: .trailing)
        }
    }
}

// MARK: - 仓位档位介绍 / 风险限额（/futures/market/risk-tiers）
struct FuturesRiskTiersView: View {
    @Environment(\.palette) private var p
    @State var symbol: String
    @State private var state: Loadable<[APIFuturesRiskTiers200ResponseContractsInner]> = .idle
    @State private var picking = false
    var body: some View {
        FeatureShell(title: "仓位档位介绍") {
            VStack(spacing: 0) {
                HStack {
                    ChipButton(title: symbol) { picking = true }
                    Spacer()
                    Text("名义价值越大，最高杠杆越低").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
                HStack {
                    Text("档位").frame(width: 36, alignment: .leading)
                    Text("名义价值(USDT)").frame(maxWidth: .infinity, alignment: .leading)
                    Text("最高杠杆").frame(width: 60, alignment: .trailing)
                    Text("维持保证金率").frame(width: 84, alignment: .trailing)
                }.font(.lb(FontSize.small)).foregroundColor(p.textTertiary).padding(.horizontal, Spacing.pageH).padding(.bottom, 6)
                Divider().overlay(p.divider)
                ScrollView {
                    LoadableView(state: state, emptyText: "暂无档位数据", isEmpty: { $0.first(where: { $0.symbol == symbol })?.tiers?.isEmpty ?? true },
                                 retry: { Task { await load() } }) { list in
                        let tiers = list.first { $0.symbol == symbol }?.tiers ?? []
                        LazyVStack(spacing: 0) {
                            ForEach(tiers, id: \.self) { t in
                                HStack {
                                    Text("\(t.tier ?? 0)").frame(width: 36, alignment: .leading)
                                    Text("\(Fmt.amount(Dec.parse(t.minNotional), 0)) - \(Fmt.amount(Dec.parse(t.maxNotional), 0))").frame(maxWidth: .infinity, alignment: .leading)
                                    Text("\(t.maxLeverage ?? 0)x").frame(width: 60, alignment: .trailing)
                                    Text("\(Fmt.amount(Dec.parse(t.maintenanceMarginRate) * 100, 2))%").frame(width: 84, alignment: .trailing)
                                }
                                .font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                                .padding(.horizontal, Spacing.pageH).frame(height: 40)
                                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                            }
                        }
                    }
                }
            }
        }
        .task { await load() }
        .lbSheet($picking) { FuturesSymbolFilterSheet(selected: symbol, allowAll: false) { s in picking = false; if let s { symbol = s } } onClose: { picking = false } }
    }
    private func load() async {
        state = .loading
        do { state = .loaded(try await FuturesAPI.riskTiers(symbol)) } catch { state = .failed(error.lbMessage) }
    }
}

// MARK: - 历史资金费率（/futures/market/funding-history，nextEndTime 翻页）
struct FuturesFundingHistoryView: View {
    @Environment(\.palette) private var p
    @State var symbol: String
    @State private var state: Loadable<[APIFuturesFundingHistory200ResponseHistoryInner]> = .idle
    @State private var next: Int64?
    @State private var more = false
    @State private var moreError: String?
    @State private var picking = false
    var body: some View {
        FeatureShell(title: "历史资金费率") {
            VStack(spacing: 0) {
                HStack { ChipButton(title: symbol) { picking = true }; Spacer() }
                    .padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
                HStack {
                    Text("时间").frame(maxWidth: .infinity, alignment: .leading)
                    Text("资金费率").frame(width: 90, alignment: .trailing)
                    Text("标记价格").frame(width: 100, alignment: .trailing)
                }.font(.lb(FontSize.small)).foregroundColor(p.textTertiary).padding(.horizontal, Spacing.pageH).padding(.bottom, 6)
                Divider().overlay(p.divider)
                ScrollView {
                    LoadableView(state: state, emptyText: "暂无资金费率记录", isEmpty: { $0.isEmpty }, retry: { Task { await load(reset: true) } }) { list in
                        LazyVStack(spacing: 0) {
                            ForEach(list, id: \.self) { h in
                                let r = Dec.parse(h.fundingRate) * 100
                                HStack {
                                    Text(msDate(h.fundingTime).map(Fmt.dateTime) ?? "--").foregroundColor(p.textSecondary).frame(maxWidth: .infinity, alignment: .leading)
                                    Text("\(Fmt.amount(r, 4))%").foregroundColor(r < 0 ? p.down : p.up).frame(width: 90, alignment: .trailing)
                                    Text(Fmt.priceAuto(Dec.parse(h.markPrice))).foregroundColor(p.textPrimary).frame(width: 100, alignment: .trailing)
                                }.font(.lbNum(FontSize.small)).padding(.horizontal, Spacing.pageH).frame(height: 40)
                            }
                            if let e = moreError {
                                Button("加载失败：\(e)，点击重试") { Task { await load(reset: false) } }.font(.lb(FontSize.small)).foregroundColor(p.down).padding(12)
                            } else if next != nil {
                                Button(more ? "加载中…" : "加载更多") { Task { await load(reset: false) } }
                                    .font(.lb(FontSize.small)).foregroundColor(p.textSecondary).padding(12).disabled(more)
                            }
                        }
                    }
                }
            }
        }
        .task { await load(reset: true) }
        .lbSheet($picking) { FuturesSymbolFilterSheet(selected: symbol, allowAll: false) { s in
            picking = false; if let s { symbol = s; Task { await load(reset: true) } }
        } onClose: { picking = false } }
    }
    private func load(reset: Bool) async {
        if reset { state = .loading; next = nil } else { more = true }
        moreError = nil
        defer { more = false }
        do {
            let r = try await FuturesAPI.fundingHistory(symbol, endTime: reset ? nil : next)
            let items = r.history ?? []
            state = .loaded(reset ? items : (state.value ?? []) + items)
            next = r.nextEndTime
        } catch {
            if reset { state = .failed(error.lbMessage) } else { moreError = error.lbMessage }
        }
    }
}

// MARK: - 对照图 #24：币对筛选（搜索 + 全部币对 / 自选币对）
struct FuturesSymbolFilterSheet: View {
    @Environment(\.palette) private var p
    let selected: String?
    var allowAll = true
    let onPick: (String?) -> Void
    let onClose: () -> Void
    @StateObject private var store = MarketStore.shared
    @State private var q = ""
    @State private var tab = 0
    var body: some View {
        let rows = store.rows(.futures).map(\.symbol).filter { q.isEmpty || $0.localizedCaseInsensitiveContains(q) }
        LBSheet(onClose: onClose) {
            VStack(alignment: .leading, spacing: 10) {
                SearchField(text: $q, placeholder: "搜索").padding(.horizontal, Spacing.pageH).padding(.top, 12)
                HStack(spacing: 16) {
                    ForEach(["全部币对", "自选币对"].indices, id: \.self) { i in
                        Text(["全部币对", "自选币对"][i]).font(.lb(FontSize.label, tab == i ? .semibold : .regular))
                            .foregroundColor(tab == i ? p.textPrimary : p.textSecondary).onTapGesture { tab = i }
                    }
                }.padding(.horizontal, Spacing.pageH)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if tab == 1 { EmptyStateView(text: "暂无自选币对") }
                        else if store.futures.isEmpty { SkeletonList(rows: 5) }
                        else {
                            if allowAll && q.isEmpty { row(nil) }
                            if rows.isEmpty { EmptyStateView(text: "无匹配币对") }
                            ForEach(rows, id: \.self) { row($0) }
                        }
                    }
                }.frame(height: 400)
            }
        }
        .task { if store.futures.isEmpty { await store.load() } }
    }
    private func row(_ s: String?) -> some View {
        Button { onPick(s) } label: {
            HStack {
                Text(s ?? "全部").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                Spacer()
                if s == selected { Image(systemName: "checkmark.circle.fill").foregroundColor(p.textPrimary) }
            }.padding(.horizontal, Spacing.pageH).frame(height: 46)
        }.buttonStyle(RowPressStyle())
    }
}


// MARK: - 现货 K 线（GET /market/klines，对齐安卓 SpotTradeViewModel）
struct SpotChartPanel: View {
    @Environment(\.palette) private var p
    let symbol: String
    let pricePrec: Int
    @State private var interval = "15m"
    @State private var candles: Loadable<[APIKline]> = .idle
    @State private var loadSeq = 0
    private let intervals: [(String, String)] = [("15m", "15分"), ("1h", "1小时"), ("4h", "4小时"), ("1d", "1天"), ("1w", "1周")]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("K线").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
            HStack(spacing: 14) {
                ForEach(intervals, id: \.0) { k, title in
                    Text(title).font(.lb(FontSize.small, interval == k ? .semibold : .regular))
                        .foregroundColor(interval == k ? p.textPrimary : p.textSecondary)
                        .onTapGesture { interval = k; Task { await load() } }
                }
                Spacer()
            }
            switch candles {
            case .idle, .loading: RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill).frame(height: 180).overlay(ProgressView())
            case let .failed(msg): ErrorStateView(message: msg) { Task { await load() } }.frame(height: 180)
            case let .loaded(list):
                if list.isEmpty { EmptyStateView(text: "暂无 K 线数据").frame(height: 180) }
                else { CandleChart(spot: list, pricePrec: pricePrec).frame(height: 180) }
            }
        }
        .task(id: symbol) { await load() }
    }
    private func load() async {
        loadSeq &+= 1
        let seq = loadSeq, sym = symbol, iv = interval
        candles = .loading
        do {
            let list = try await MarketAPI.klines(sym, interval: iv)
            guard seq == loadSeq, sym == symbol, iv == interval else { return }
            candles = .loaded(list)
        } catch {
            guard seq == loadSeq, sym == symbol, iv == interval else { return }
            candles = .failed(error.lbMessage)
        }
    }
}

// MARK: - 历史标记价格（GET /futures/market/mark-history）
struct FuturesMarkHistoryView: View {
    @Environment(\.palette) private var p
    let symbol: String
    @State private var state: Loadable<[APIFuturesMarkHistory200ResponseHistoryInner]> = .idle
    var body: some View {
        FeatureShell(title: "历史标记价格") {
            LoadableView(state: state, emptyText: "暂无标记价历史", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { rows in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        HStack {
                            Text("时间").frame(maxWidth: .infinity, alignment: .leading)
                            Text("标记价").frame(maxWidth: .infinity, alignment: .trailing)
                            Text("指数价").frame(maxWidth: .infinity, alignment: .trailing)
                        }.font(.lb(FontSize.small)).foregroundColor(p.textTertiary).padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
                        ForEach(Array(rows.prefix(100).enumerated()), id: \.offset) { _, r in
                            HStack {
                                Text(fmt(r.openTime ?? r.time)).frame(maxWidth: .infinity, alignment: .leading)
                                Text(Fmt.priceAuto(Dec.parse(r.close ?? r.markPrice))).frame(maxWidth: .infinity, alignment: .trailing)
                                Text(Fmt.priceAuto(Dec.parse(r.indexClose ?? r.indexPrice))).frame(maxWidth: .infinity, alignment: .trailing)
                            }.font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                            .padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                    }
                }
            }
        }
        .task(id: symbol) { await load() }
    }
    private func load() async {
        state = .loading
        let sym = symbol
        do {
            let r = try await FuturesAPI.markHistory(sym)
            guard sym == symbol else { return }
            state = .loaded(r.history ?? [])
        } catch {
            guard sym == symbol else { return }
            state = .failed(error.lbMessage)
        }
    }
    private func fmt(_ ms: Int64?) -> String {
        guard let ms else { return "--" }
        let f = DateFormatter(); f.locale = Locale(identifier: "en_US_POSIX"); f.dateFormat = "MM-dd HH:mm"
        return f.string(from: Date(timeIntervalSince1970: TimeInterval(ms) / 1000))
    }
}

// MARK: - 风险准备金（GET /futures/market/insurance-fund）
struct FuturesInsuranceFundView: View {
    @Environment(\.palette) private var p
    @State private var state: Loadable<APIFuturesInsuranceFund200Response> = .idle
    var body: some View {
        FeatureShell(title: "风险准备金") {
            LoadableView(state: state, retry: { Task { await load() } }) { v in
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        KVRow(k: "资产", v: v.asset ?? "USDT")
                        KVRow(k: "余额", v: Fmt.amount(Dec.parse(v.balance), 4))
                        KVRow(k: "权益", v: Fmt.amount(Dec.parse(v.equity), 4))
                        Text("最近强平").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary).padding(.top, 8)
                        let liqs = v.recentLiquidations ?? []
                        if liqs.isEmpty { Text("暂无记录").font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                        else {
                            ForEach(Array(liqs.prefix(20).enumerated()), id: \.offset) { _, x in
                                HStack {
                                    Text(x.symbol ?? "--")
                                    Spacer()
                                    Text(x.qty ?? "--")
                                    Text(Fmt.priceAuto(Dec.parse(x.markPrice))).foregroundColor(p.textSecondary)
                                }.font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                            }
                        }
                    }.padding(Spacing.pageH)
                }
            }
        }
        .task { await load() }
    }
    private func load() async {
        if state.value == nil { state = .loading }
        do { state = .loaded(try await FuturesAPI.insuranceFund()) } catch { state = .failed(error.lbMessage) }
    }
}

// MARK: - 合约计算器（本地推算，对齐安卓 TradeCalculator；不调用资金接口）
struct FuturesCalculatorSheet: View {
    @Environment(\.palette) private var p
    let entryHint: String
    let base: String
    let leverage: Int
    let mmr: Decimal
    let onClose: () -> Void
    @State private var long = true
    @State private var entry = ""
    @State private var qty = ""
    @State private var exitP = ""
    @State private var target = ""
    var body: some View {
        LBSheet(title: "计算器", onClose: onClose) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    HStack(spacing: 8) {
                        chip("开多", long) { long = true }
                        chip("开空", !long) { long = false }
                    }
                    NumberBox(label: "开仓价", unit: "USDT", text: $entry, onEdit: {})
                    NumberBox(label: "数量", unit: base, text: $qty, onEdit: {})
                    NumberBox(label: "平仓价（可选）", unit: "USDT", text: $exitP, onEdit: {})
                    NumberBox(label: "目标盈亏 USDT（可选）", unit: "USDT", text: $target, onEdit: {})
                    let e = TradeCalculator.parseInput(entry.isEmpty ? entryHint : entry)
                    let q = TradeCalculator.parseInput(qty)
                    let lev = max(leverage, 1)
                    let cost = TradeCalculator.cost(qty: q, price: e, leverage: lev)
                    let liq = TradeCalculator.liqPrice(entry: e, leverage: lev, long: long, mmr: mmr)
                    KVRow(k: "开仓成本", v: (e > 0 && q > 0) ? Fmt.amount(cost, 4) + " USDT" : "--")
                    KVRow(k: "预估强平价", v: e > 0 && lev > 0 ? Fmt.priceAuto(liq) : "--")
                    if TradeCalculator.parseInput(exitP) > 0, q > 0, e > 0 {
                        KVRow(k: "预估盈亏", v: Fmt.amount(TradeCalculator.pnl(entry: e, exit: TradeCalculator.parseInput(exitP), qty: q, long: long), 4) + " USDT")
                    }
                    if TradeCalculator.parseInput(target) != 0, q > 0, e > 0 {
                        KVRow(k: "目标价", v: Fmt.priceAuto(TradeCalculator.targetPrice(entry: e, qty: q, targetPnl: TradeCalculator.parseInput(target), long: long)))
                    }
                    Text("结果为本地估算，未含手续费与资金费率，不构成下单依据。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 20)
            }
            .onAppear { if entry.isEmpty { entry = entryHint } }
        }
    }
    private func chip(_ t: String, _ on: Bool, _ act: @escaping () -> Void) -> some View {
        Text(t).font(.lb(FontSize.label, on ? .semibold : .regular)).foregroundColor(on ? p.textOnPrimary : p.textPrimary)
            .padding(.horizontal, 14).frame(height: 32)
            .background(RoundedRectangle(cornerRadius: Radius.sm).fill(on ? p.primary : p.fill))
            .onTapGesture(perform: act)
    }
}

enum TradeCalculator {
    /// 剥千分位后按 en_US_POSIX 解析；空/非法 → 0
    static func parseInput(_ s: String) -> Decimal {
        Dec.parse(s.replacingOccurrences(of: ",", with: ""))
    }
    static func cost(qty: Decimal, price: Decimal, leverage: Int) -> Decimal {
        guard price > 0, leverage > 0, qty != 0 else { return 0 }
        return abs(qty) * price / Decimal(leverage)
    }
    static func liqPrice(entry: Decimal, leverage: Int, long: Bool, mmr: Decimal) -> Decimal {
        guard entry > 0, leverage > 0 else { return 0 }
        let inv = Decimal(1) / Decimal(leverage)
        return long ? entry * (1 - inv + mmr) : entry * (1 + inv - mmr)
    }
    static func pnl(entry: Decimal, exit: Decimal, qty: Decimal, long: Bool) -> Decimal {
        let q = abs(qty)
        return long ? (exit - entry) * q : (entry - exit) * q
    }
    static func targetPrice(entry: Decimal, qty: Decimal, targetPnl: Decimal, long: Bool) -> Decimal {
        guard qty != 0 else { return 0 }
        let delta = targetPnl / abs(qty)
        return long ? entry + delta : entry - delta
    }
}
