import SwiftUI

/// 币种详情。K 线周期只用接口支持的 8 档。最新成交来自 /market/trades。
struct MarketDetailView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var watch = Watchlist.shared
    let symbol: String
    let futures: Bool
    @StateObject private var live: MarketTape
    @State private var interval = "15m"
    @State private var section = 0
    @State private var studies = ChartStudies()
    @State private var bookGroup = 1
    init(symbol: String, futures: Bool = false) {
        self.symbol = symbol
        self.futures = futures
        _live = StateObject(wrappedValue: MarketTape(symbol: symbol, futures: futures))
    }
    @State private var showIntervals = false
    @State private var showChartSettings = false
    @State private var editingPins = false
    @State private var showAlert = false
    @AppStorage("lb.kline.pins") private var pinsRaw = "1m,15m,1h,4h,1d"

    private let allIntervals = [("1m", "1分"), ("5m", "5分"), ("15m", "15分"), ("30m", "30分"), ("1h", "1小时"), ("4h", "4小时"), ("1d", "1天"), ("1w", "1周")]
    private let defaultPins = ["1m", "15m", "1h", "4h", "1d"]

    var body: some View {
        Group {
            if showAlert {
                SpotAlertPage(symbol: symbol, suggested: plainPrice(last), onBack: { showAlert = false })
            } else {
                detail
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await live.start() }
        .onDisappear { live.stop() }
        .onChange(of: interval) { live.setInterval($0) }
        .lbSheet($showChartSettings) { chartSettingsSheet }
        .lbSheet($showIntervals) {
            if editingPins {
                PinEditor(pins: pinList, all: allIntervals, onConfirm: { savePins($0); editingPins = false; showIntervals = false }, onBack: { editingPins = false })
            } else {
                intervalPicker
            }
        }
        .onChange(of: showIntervals) { if !$0 { editingPins = false } }
    }

    private var detail: some View {
        VStack(spacing: 0) {
            NavBar(title: "", onBack: { Router.shared.marketDetail = nil }) {
                // 参考 App：标题左对齐「← BTC/USDT ▾ •」，右侧星标
                EmptyView()
            }
            .overlay(alignment: .leading) {
                HStack(spacing: 4) {
                    Text(futures ? "\(symbol) 永续" : pair).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    if !futures {
                        Image(systemName: "triangle.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                            .rotationEffect(.degrees(180))
                    }
                    Circle().fill(p.up).frame(width: 6, height: 6)
                }
                .padding(.leading, 56)
                .allowsHitTesting(false)
            }
            .background(alignment: .trailing) {
                Button { watch.toggle(symbol) } label: {
                    Image(systemName: watch.contains(symbol) ? "star.fill" : "star")
                        .font(.system(size: 19))
                        .foregroundColor(watch.contains(symbol) ? p.brand : p.textTertiary)
                }.buttonStyle(.plain).padding(.trailing, Spacing.pageH)
                    .accessibilityLabel(watch.contains(symbol) ? "移出自选" : "加入自选")
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    header
                    intervalBar
                    chart
                    // 对照 EasiCoin：文字标签 + 下划线，不用系统分段控件
                    VStack(spacing: 0) {
                        HStack { PrimaryTabs(items: ["订单表", "深度图", "最新成交"], selected: $section, size: FontSize.tabSecondary, underline: true); Spacer() }
                        Divider().overlay(p.divider)
                    }
                    if section == 0 { bookList }
                    else if section == 1 { depthChart }
                    else { fillsList }
                }
                .padding(.horizontal, Spacing.pageH)
                .padding(.bottom, 24)
            }
            HStack(spacing: 12) {
                Button { Session.shared.requireLogin { showAlert = true } } label: {
                    VStack(spacing: 2) {
                        Image(systemName: "bell")
                        Text("预警").font(.lb(10))
                    }.foregroundColor(p.textPrimary).frame(width: 52, height: 44)
                }.buttonStyle(.plain)
                LBButton(title: "交易") { openTrade() }
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.bottom, 12)
        }
    }

    private var pinList: [String] {
        let known = Set(allIntervals.map(\.0))
        let saved = pinsRaw.split(separator: ",").map(String.init).filter { known.contains($0) }
        return saved.isEmpty ? defaultPins : saved
    }

    private func savePins(_ value: [String]) { pinsRaw = value.joined(separator: ",") }

    private var barItems: [(String, String)] {
        var ids = pinList
        if !ids.contains(interval) { ids.append(interval) }
        return ids.compactMap { id in allIntervals.first { $0.0 == id } }
    }

    private var pair: String {
        let quote = symbol.hasSuffix("USDT") ? "USDT" : String(symbol.suffix(4))
        return "\(base)/\(quote)"
    }
    private var base: String { symbol.hasSuffix("USDT") ? String(symbol.dropLast(4)) : symbol }
    private var book: OrderBook { live.book }
    private var last: Decimal { live.last }
    private var change: Decimal { live.change }

    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(last > 0 ? Fmt.price(last, precision: live.pricePrec) : "--")
                    .font(.lbNum(30, .bold)).foregroundColor(change < 0 ? p.down : p.up)
                    .lineLimit(1).minimumScaleFactor(0.7)
                HStack(spacing: 6) {
                    Text(last > 0 ? "≈ " + Fmt.fiat(last, rate: AppConfig.usdtCny) : "")
                        .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                    if last > 0 {
                        Text(Fmt.percent(change)).font(.lbNum(FontSize.small)).foregroundColor(change < 0 ? p.down : p.up)
                    }
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .leading, spacing: 5) {
                stat("24h最高价", live.high > 0 ? Fmt.price(live.high, precision: live.pricePrec) : "--")
                stat("24h最低价", live.low > 0 ? Fmt.price(live.low, precision: live.pricePrec) : "--")
                stat("24h成交量(\(base))", Fmt.compact(live.volume))
                stat("24h成交额(USDT)", Fmt.compact(live.quoteVolume))
            }
        }
    }

    private func stat(_ k: String, _ v: String) -> some View {
        HStack {
            Text(k).font(.lb(10)).foregroundColor(p.textSecondary).lineLimit(1)
            Spacer(minLength: 4)
            Text(v).font(.lbNum(10)).foregroundColor(p.textPrimary).lineLimit(1)
        }
    }

    private var intervalBar: some View {
        HStack(spacing: 14) {
            ForEach(barItems, id: \.0) { iv, title in
                Text(title).font(.lb(FontSize.small, interval == iv ? .semibold : .regular))
                    .foregroundColor(interval == iv ? p.textPrimary : p.textSecondary)
                    .onTapGesture { interval = iv }
            }
            Spacer()
            // 参考 App 右侧三个图标：分时/指标设置/图表设置
            HStack(spacing: 16) {
                Image(systemName: "chart.xyaxis.line").font(.system(size: 15)).foregroundColor(p.textPrimary)
                Image(systemName: "slider.horizontal.3").font(.system(size: 15)).foregroundColor(p.textPrimary)
                    .onTapGesture { showIntervals = true }
                Image(systemName: "gearshape").font(.system(size: 15)).foregroundColor(p.textPrimary)
                    .onTapGesture { showChartSettings = true }
            }
        }
    }

    /// 参考 App「图表设置」：指标 / 画图 / 横屏 / 更多，图表显示勾选项与高度滑杆。
    private var chartSettingsSheet: some View {
        @AppStorage("lb.chart.lastPrice") var showLast = true
        @AppStorage("lb.chart.markPrice") var showMark = false
        @AppStorage("lb.chart.high") var showHigh = true
        @AppStorage("lb.chart.low") var showLow = true
        @AppStorage("lb.chart.countdown") var showCountdown = false
        @AppStorage("lb.chart.flashOrder") var showFlash = true
        @AppStorage("lb.chart.orders") var showOrders = false
        @AppStorage("lb.chart.cost") var showCost = false
        @AppStorage("lb.chart.height") var chartH = 320.0
        return LBSheet(title: "图表设置", showClose: true, onClose: { showChartSettings = false }) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 10) {
                    settingsAction("指标", "chart.bar.xaxis") { showChartSettings = false; showIntervals = true }
                    settingsAction("画图", "pencil.tip") {}
                    settingsAction("横屏", "rectangle.expand.vertical") {}
                }
                settingsAction("更多", "square.grid.2x2") {}.padding(.top, 10).frame(maxWidth: .infinity, alignment: .leading)
                Text("图表显示").font(.lb(FontSize.body)).foregroundColor(p.textSecondary).padding(.top, 26)
                VStack(spacing: 18) {
                    HStack {
                        checkRow("最新价格", $showLast, new: false)
                        checkRow("标记价格", $showMark, new: false)
                    }
                    HStack {
                        checkRow("最高价格", $showHigh, new: false)
                        checkRow("最低价格", $showLow, new: false)
                    }
                    HStack {
                        checkRow("倒计时", $showCountdown, new: true)
                        checkRow("闪电下单", $showFlash, new: true)
                    }
                    HStack {
                        checkRow("当前委托", $showOrders, new: true)
                        checkRow("累计成本", $showCost, new: true)
                    }
                }.padding(.top, 16)
                Text("图表高度").font(.lb(FontSize.body)).foregroundColor(p.textSecondary).padding(.top, 26)
                Slider(value: $chartH, in: 220...520).tint(p.textPrimary).padding(.top, 10)
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
        }
    }

    private func settingsAction(_ title: String, _ icon: String, _ tap: @escaping () -> Void) -> some View {
        Button(action: tap) {
            HStack(spacing: 6) {
                Image(systemName: icon).font(.system(size: 13))
                Text(title).font(.lb(FontSize.label))
            }.foregroundColor(p.textPrimary)
                .frame(maxWidth: .infinity).frame(height: 38)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
        }.buttonStyle(.plain)
    }

    private func checkRow(_ title: String, _ on: Binding<Bool>, new: Bool) -> some View {
        Button { on.wrappedValue.toggle() } label: {
            HStack(spacing: 8) {
                Image(systemName: on.wrappedValue ? "checkmark.square.fill" : "square")
                    .font(.system(size: 16)).foregroundColor(on.wrappedValue ? p.textPrimary : p.textTertiary)
                Text(title).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                if new {
                    Text("New").font(.lb(9)).foregroundColor(p.textPrimary)
                        .padding(.horizontal, 4).frame(height: 14).background(Capsule().fill(p.brand))
                }
                Spacer(minLength: 0)
            }
        }.buttonStyle(.plain).frame(maxWidth: .infinity)
    }

    private var intervalPicker: some View {
        LBSheet(title: "周期", showClose: true, onClose: { showIntervals = false }) {
            VStack(alignment: .leading, spacing: 16) {
                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: 4), spacing: 8) {
                    ForEach(allIntervals, id: \.0) { iv, title in
                        Text(title).font(.lb(FontSize.small, interval == iv ? .semibold : .regular))
                            .foregroundColor(p.textPrimary)
                            .frame(maxWidth: .infinity).frame(height: 36)
                            .background(RoundedRectangle(cornerRadius: Radius.md).stroke(interval == iv ? p.textPrimary : p.border))
                            .onTapGesture { interval = iv; showIntervals = false }
                    }
                }
                Button { editingPins = true } label: {
                    HStack {
                        Text("选择时间周期偏好").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        Spacer()
                        Text("\(pinList.count)/5").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textTertiary)
                    }
                }.buttonStyle(.plain)
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 20)
        }
    }

    @ViewBuilder private var chart: some View {
        if live.loadingBars && live.bars.isEmpty {
            ProgressView().frame(maxWidth: .infinity).frame(height: 220)
        } else if let candleError = live.barError, live.bars.isEmpty {
            Text(candleError).font(.lb(FontSize.small)).foregroundColor(p.down).frame(maxWidth: .infinity).frame(height: 120)
        } else if live.bars.isEmpty {
            EmptyStateView(text: "暂无 K 线").frame(height: 160)
        } else {
            CandleChart(bars: live.bars, pricePrec: live.pricePrec, studies: studies)
            studyBar
        }
    }

    private var studyBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 18) {
                studyChip("MA", studies.ma) { studies.ma.toggle() }
                studyChip("BOLL", studies.boll) { studies.boll.toggle() }
                studyChip("EMA", studies.ema) { studies.ema.toggle() }
                studyChip("撑压线", studies.supportResistance) { studies.supportResistance.toggle() }
                studyChip("超级趋势", studies.superTrend) { studies.superTrend.toggle() }
                studyChip("VOL", studies.vol) { studies.vol.toggle() }
            }
        }
    }

    private func studyChip(_ title: String, _ on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title).font(.lb(FontSize.small, on ? .semibold : .regular))
                .foregroundColor(on ? p.textPrimary : p.textTertiary)
        }.buttonStyle(.plain)
    }

    private var bookList: some View {
        VStack(spacing: 0) {
            ratioBar
            HStack {
                Text("买").frame(maxWidth: .infinity, alignment: .leading)
                Text("卖").frame(maxWidth: .infinity, alignment: .trailing)
                Menu {
                    ForEach([1, 10, 100], id: \.self) { n in
                        Button(tickText(n)) { bookGroup = n }
                    }
                } label: {
                    HStack(spacing: 2) {
                        Text(tickText(bookGroup))
                        Image(systemName: "chevron.down").font(.system(size: 8))
                    }
                }
            }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            if book.asks.isEmpty && book.bids.isEmpty { EmptyStateView(text: "暂无盘口") }
            // 固定 20 行；底色条是从中间向两侧的累计深度（越往下越宽），和参考 App 的阶梯形一致
            let bids = Array(grouped(book.bids, up: false).prefix(20))
            let asks = Array(grouped(book.asks, up: true).prefix(20))
            let bidSum = Self.running(bids), askSum = Self.running(asks)
            let total = max(bidSum.last ?? 0, askSum.last ?? 0, 0.00000001)
            let rows = 20
            GeometryReader { g in
                let half = g.size.width / 2
                VStack(spacing: 0) {
                    ForEach(0..<rows, id: \.self) { i in
                        HStack(spacing: 0) {
                            bookSide(bids[safe: i], up: true, depth: bidSum[safe: i] ?? 0, total: total, width: half, qtyOnLeft: true)
                            bookSide(asks[safe: i], up: false, depth: askSum[safe: i] ?? 0, total: total, width: half, qtyOnLeft: false)
                        }.frame(height: 30)
                    }
                }
            }
            .frame(height: CGFloat(rows) * 30)
        }
    }

    private var ratioBar: some View {
        let buy = book.buyRatio
        return HStack(spacing: 6) {
            Text("B \(Int((buy * 100).rounded()))%").font(.lbNum(10)).foregroundColor(p.up)
            GeometryReader { g in
                HStack(spacing: 2) {
                    Rectangle().fill(p.up).frame(width: max(g.size.width * buy, 0))
                    Rectangle().fill(p.down)
                }
            }.frame(height: 4)
            Text("\(Int(((1 - buy) * 100).rounded()))% S").font(.lbNum(10)).foregroundColor(p.down)
        }.frame(height: 16).padding(.bottom, 6)
    }

    private var depthChart: some View {
        let plotHeight: CGFloat = {
            let saved = UserDefaults.standard.double(forKey: "lb.chart.height")
            return saved > 0 ? CGFloat(min(max(saved, 220), 520)) : 300
        }()
        return VStack(alignment: .trailing, spacing: 0) {
            // 参考 App：右上角图例（方块），深度曲线从底部往上叠
            HStack(spacing: 14) {
                Spacer()
                HStack(spacing: 4) {
                    Rectangle().fill(p.up).frame(width: 8, height: 8)
                    Text("买入").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                HStack(spacing: 4) {
                    Rectangle().fill(p.down).frame(width: 8, height: 8)
                    Text("卖出").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }.padding(.bottom, 6)
            if book.bids.isEmpty && book.asks.isEmpty { EmptyStateView(text: "暂无深度").frame(height: 260) }
            else {
                GeometryReader { geo in
                    DepthPlot(book: book, up: p.up, down: p.down)
                        .overlay(alignment: .bottomLeading) {
                            HStack {
                                Text(depthLow).font(.lbNum(10)).foregroundColor(p.textTertiary)
                                Spacer()
                                Text(depthMid).font(.lbNum(10)).foregroundColor(p.textTertiary)
                                Spacer()
                                Text(depthHigh).font(.lbNum(10)).foregroundColor(p.textTertiary)
                            }
                        }
                        .overlay(alignment: .trailing) {
                            VStack(alignment: .trailing, spacing: 0) {
                                ForEach(0..<4, id: \.self) { _ in
                                    Spacer()
                                    Rectangle().fill(p.divider).frame(width: geo.size.width, height: 0.5)
                                }
                                Spacer()
                            }.allowsHitTesting(false)
                        }
                }.frame(height: plotHeight)
            }
        }
    }

    private var depthLow: String {
        guard let v = book.bids.last?.price ?? book.asks.first?.price else { return "--" }
        return Fmt.price(v, precision: live.pricePrec)
    }
    private var depthHigh: String {
        guard let v = book.asks.last?.price ?? book.bids.first?.price else { return "--" }
        return Fmt.price(v, precision: live.pricePrec)
    }
    private var depthMid: String {
        guard let v = book.asks.first?.price ?? book.bids.first?.price else { return "--" }
        return Fmt.price(v, precision: live.pricePrec)
    }

    private var fillsList: some View {
        VStack(alignment: .leading, spacing: 8) {
            if live.trades.isEmpty { EmptyStateView(text: "暂无公开成交") }
            else {
                HStack {
                    Text("时间").frame(maxWidth: .infinity, alignment: .leading)
                    Text("价格(USDT)").frame(width: 110, alignment: .leading)
                    HStack(spacing: 3) {
                        Text("数量(\(base))")
                        Image(systemName: "triangle.fill").font(.system(size: 7)).rotationEffect(.degrees(180)).foregroundColor(p.textTertiary)
                    }.frame(width: 100, alignment: .trailing)
                }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                ForEach(live.trades) { t in
                    HStack {
                        Text(Self.clock.string(from: t.time)).frame(maxWidth: .infinity, alignment: .leading)
                        Text(Fmt.price(Dec.parse(t.price), precision: live.pricePrec)).foregroundColor(t.side == "buy" ? p.up : p.down).frame(width: 110, alignment: .leading)
                        Text(String(format: "%.5f", NSDecimalNumber(decimal: Dec.parse(t.quantity)).doubleValue)).frame(width: 100, alignment: .trailing)
                    }.font(.lbNum(12)).foregroundColor(p.textPrimary).frame(height: 30)
                }
            }
        }
    }

    private func tickText(_ group: Int) -> String {
        let value = tickUnit * Decimal(group)
        return Fmt.price(value, precision: min(live.pricePrec + (group == 1 ? 0 : 0), 8)).replacingOccurrences(of: ",", with: "")
    }

    private var tickUnit: Decimal {
        var step = Decimal(1)
        let n = max(live.pricePrec, 0)
        if n > 0 { for _ in 0..<n { step /= 10 } }
        return step
    }

    private func grouped(_ rows: [BookLevel], up: Bool) -> [BookLevel] {
        let step = tickUnit * Decimal(bookGroup)
        guard step > 0 else { return Array(rows.prefix(50)) }
        var map: [Decimal: Decimal] = [:]
        for row in rows where row.price > 0 && row.qty > 0 {
            var q = row.price / step
            var rounded = Decimal()
            NSDecimalRound(&rounded, &q, 0, up ? .up : .down)
            let bucket = rounded * step
            map[bucket, default: 0] += row.qty
        }
        let sorted = map.map { BookLevel(price: $0.key, qty: $0.value) }.sorted { up ? $0.price < $1.price : $0.price > $1.price }
        return Array(sorted.prefix(50))
    }

    private static func running(_ rows: [BookLevel]) -> [Decimal] {
        var sum = Decimal(0)
        return rows.map { sum += $0.qty; return sum }
    }

    private func bookSide(_ level: BookLevel?, up: Bool, depth: Decimal, total: Decimal, width: CGFloat, qtyOnLeft: Bool) -> some View {
        let frac: CGFloat = {
            guard level != nil, total > 0 else { return 0 }
            return CGFloat(min(1, max(0, NSDecimalNumber(decimal: depth / total).doubleValue)))
        }()
        // 买盘的条贴着中线向左长，卖盘贴着中线向右长
        return ZStack(alignment: qtyOnLeft ? .trailing : .leading) {
            Rectangle().fill((up ? p.up : p.down).opacity(0.14))
                .frame(width: width * frac)
            HStack(spacing: 4) {
                if qtyOnLeft {
                    Text(level.map { Self.qtyText($0.qty, live.qtyPrec) } ?? "")
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Text(level.map { Fmt.price($0.price, precision: live.pricePrec) } ?? "")
                        .foregroundColor(p.up)
                } else {
                    Text(level.map { Fmt.price($0.price, precision: live.pricePrec) } ?? "")
                        .foregroundColor(p.down)
                    Text(level.map { Self.qtyText($0.qty, live.qtyPrec) } ?? "")
                        .frame(maxWidth: .infinity, alignment: .trailing)
                }
            }
            .font(.lbNum(13))
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .padding(.horizontal, 4)
        }
        .frame(width: width, height: 30)
        .clipped()
    }

    private static func qtyText(_ q: Decimal, _ prec: Int) -> String {
        // 固定小数位（不去尾零）：数字宽度稳定，刷新时不左右抖
        Fmt.amount(q, prec)
    }

    private static let clock: DateFormatter = {
        let f = DateFormatter(); f.locale = Locale(identifier: "en_US_POSIX"); f.dateFormat = "HH:mm:ss"; return f
    }()

    private func openTrade() {
        let sym = symbol
        let fut = futures
        Router.shared.marketDetail = nil
        if fut {
            if Router.shared.tab == .futures && Router.shared.futuresSymbol == sym { Router.shared.tab = .futures }
            else { Router.shared.tradeFutures(sym) }
        } else if Router.shared.tab == .spot && Router.shared.spotSymbol == sym {
            Router.shared.tab = .spot
        } else {
            Router.shared.trade(sym)
        }
    }

    private func plainPrice(_ d: Decimal) -> String {
        d > 0 ? NSDecimalNumber(decimal: d).stringValue : ""
    }
}

/// 买卖累计深度。横轴是档位，纵轴是累计数量。
private struct DepthPlot: View {
    let book: OrderBook
    let up: Color
    let down: Color
    var body: some View {
        Canvas { ctx, size in
            let bids = Array(book.bids.prefix(40))
            let asks = Array(book.asks.prefix(40))
            guard let hi = asks.last?.price ?? bids.first?.price, let lo = bids.last?.price ?? asks.first?.price, hi > lo else { return }
            func cum(_ rows: [BookLevel]) -> [(Decimal, Double)] {
                var s = 0.0
                return rows.map { s += NSDecimalNumber(decimal: $0.qty).doubleValue; return ($0.price, s) }
            }
            let b = cum(bids), a = cum(asks)
            let maxV = max(b.map(\.1).max() ?? 0, a.map(\.1).max() ?? 0, 1)
            let span = NSDecimalNumber(decimal: hi - lo).doubleValue
            func x(_ price: Decimal) -> CGFloat { CGFloat((NSDecimalNumber(decimal: price - lo).doubleValue / span)) * size.width }
            func area(_ rows: [(Decimal, Double)], color: Color) {
                guard let first = rows.first else { return }
                var path = Path()
                path.move(to: CGPoint(x: x(first.0), y: size.height))
                // 参考 App 是阶梯线：横向走到下一档价位再竖向抬升
                var prevY = size.height
                for row in rows {
                    let ny = size.height - CGFloat(row.1 / maxV) * (size.height - 10)
                    path.addLine(to: CGPoint(x: x(row.0), y: prevY))
                    path.addLine(to: CGPoint(x: x(row.0), y: ny))
                    prevY = ny
                }
                if let last = rows.last { path.addLine(to: CGPoint(x: x(last.0), y: size.height)) }
                path.closeSubpath()
                ctx.fill(path, with: .color(color.opacity(0.12)))
                ctx.stroke(path, with: .color(color), lineWidth: 1)
            }
            area(b.reversed(), color: up)
            area(a, color: down)
        }
    }
}

private struct PinEditor: View {
    @Environment(\.palette) private var p
    let pins: [String]
    let all: [(String, String)]
    let onConfirm: ([String]) -> Void
    let onBack: () -> Void
    @State private var draft: [String] = []

    var body: some View {
        LBSheet(title: "选择周期偏好 \(draft.count)/5", showClose: true, onClose: onBack) {
            VStack(alignment: .leading, spacing: 16) {
                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: 4), spacing: 8) {
                    ForEach(all, id: \.0) { iv, title in
                        let on = draft.contains(iv)
                        Text(title).font(.lb(FontSize.small, on ? .semibold : .regular))
                            .foregroundColor(p.textPrimary)
                            .frame(maxWidth: .infinity).frame(height: 36)
                            .background(RoundedRectangle(cornerRadius: Radius.md).stroke(on ? p.textPrimary : p.border))
                            .onTapGesture { toggle(iv) }
                    }
                }
                HStack(spacing: 12) {
                    LBButton(title: "重置", kind: .outline) { draft = ["1m", "15m", "1h", "4h", "1d"] }
                    LBButton(title: "确认", enabled: !draft.isEmpty) { onConfirm(draft) }
                }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 20)
        }
        .onAppear { draft = pins }
    }

    private func toggle(_ iv: String) {
        if let i = draft.firstIndex(of: iv) { draft.remove(at: i) }
        else if draft.count >= 5 { ToastCenter.shared.show("最多钉 5 个周期") }
        else { draft.append(iv) }
    }
}

/// 现货价格预警。条件是涨到 / 跌到。频率 once、daily、repeat 对应接口 triggerMode。
private struct SpotAlertPage: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    let symbol: String
    let suggested: String
    let onBack: () -> Void
    @State private var rise = true
    @State private var mode = "once"
    @State private var threshold = ""
    @State private var items: [PriceAlertAPI.Item] = []
    @State private var error: String?
    @State private var saving = false
    @State private var confirm: ConfirmRequest?

    @State private var showKind = false
    @State private var showAll = false

    var body: some View {
        VStack(spacing: 0) {
            // 参考 App：标题居中「BTC/USDT ▾ / 现货」，右侧「全部提醒」
            ZStack {
                HStack(spacing: 4) {
                    Image(systemName: "triangle.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                        .rotationEffect(.degrees(180))
                    Circle().fill(p.up).frame(width: 6, height: 6)
                }.offset(x: 54, y: -6)
                VStack(spacing: 2) {
                    Text(symbol).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    Text("现货").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }
            .frame(maxWidth: .infinity).frame(height: 44)
            .overlay(alignment: .trailing) {
                Text("全部提醒").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                    .padding(.trailing, Spacing.pageH)
                    .onTapGesture { showAll = true }
            }
            .overlay(alignment: .leading) {
                Button(action: onBack) {
                    Image(systemName: "arrow.left").font(.system(size: 18, weight: .medium)).foregroundColor(p.textPrimary)
                }.buttonStyle(.plain).padding(.leading, Spacing.pageH)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text("市场价格").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                        .frame(maxWidth: .infinity).padding(.top, 18)
                    Text(suggested).font(.lbNum(28, .bold)).foregroundColor(p.down)
                        .frame(maxWidth: .infinity).padding(.top, 4)
                    Text("提醒类型").font(.lb(FontSize.body)).foregroundColor(p.textPrimary).padding(.top, 26)
                    HStack {
                        Text(rise ? "价格涨到" : "价格跌到").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        Spacer()
                        Image(systemName: "triangle.fill").font(.system(size: 8)).foregroundColor(p.textSecondary)
                            .rotationEffect(.degrees(180))
                    }
                    .padding(.horizontal, 14).frame(height: 44)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                    .padding(.top, 10)
                    .contentShape(Rectangle())
                    .onTapGesture { showKind = true }
                    HStack {
                        Text("数值").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        Spacer()
                        Text("≈ ¥" + cnyText).font(.lbNum(FontSize.caption)).foregroundColor(p.textSecondary)
                    }.padding(.top, 18)
                    HStack {
                        TextField("价格", text: $threshold, prompt: Text("请输入"))
                            .font(.lbNum(FontSize.body)).keyboardType(.decimalPad)
                        Text("USDT").font(.lb(FontSize.body)).foregroundColor(p.textSecondary)
                    }
                    .padding(.horizontal, 14).frame(height: 44)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                    .padding(.top, 8)
                    HStack {
                        Text("-25%").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        Spacer()
                        Text("25%").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                    }.padding(.top, 8)
                    Text("提醒频率").font(.lb(FontSize.body)).foregroundColor(p.textPrimary).padding(.top, 18)
                    HStack(spacing: 10) {
                        chip("只提醒一次", mode == "once") { mode = "once" }
                        chip("每天提醒一次", mode == "daily") { mode = "daily" }
                        chip("持续提醒", mode == "repeat") { mode = "repeat" }
                    }.padding(.top, 10)
                    Text("生效时间").font(.lb(FontSize.body)).foregroundColor(p.textPrimary).padding(.top, 18)
                    HStack {
                        Text("一直有效").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        Spacer()
                        Image(systemName: "triangle.fill").font(.system(size: 8)).foregroundColor(p.textSecondary)
                            .rotationEffect(.degrees(180))
                    }
                    .padding(.horizontal, 14).frame(height: 44)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                    .padding(.top, 10)
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down).padding(.top, 8) }
                    LBButton(title: "创建", enabled: PriceAlertAPI.threshold(threshold) != nil && !saving, loading: saving) {
                        Task { await create() }
                    }.padding(.top, 22)
                    HStack {
                        Text("当前提醒").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                        Spacer()
                        if !items.isEmpty {
                            Text("删除所有").font(.lb(FontSize.body)).foregroundColor(p.textTertiary)
                                .onTapGesture { confirm = ConfirmRequest(title: "删除全部提醒", message: "删除 \(symbol) 的 \(items.count) 条提醒。", confirmTitle: "删除", destructive: true) { Task { await deleteAll() } } }
                        }
                    }.padding(.top, 30)
                    if items.isEmpty { EmptyStateView(text: "暂无数据").padding(.top, 20) }
                    ForEach(items) { item in
                        HStack {
                            Text(alertLabel(item)).foregroundColor(p.textSecondary)
                            Text(item.threshold).foregroundColor(p.textPrimary)
                            Spacer()
                            Text(item.status).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        }.font(.lbNum(FontSize.label)).frame(height: 36)
                        Divider().overlay(p.divider)
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }
        }
        .lbSheet($showKind) {
            LBSheet(title: "提醒类型", showClose: true, onClose: { showKind = false }) {
                SheetOptionRow(title: "价格涨到", selected: rise) { rise = true; showKind = false }
                SheetOptionRow(title: "价格跌到", selected: !rise) { rise = false; showKind = false }
            }
        }
        .onAppear { if threshold.isEmpty { threshold = suggested } }
        .task(id: session.isLoggedIn) { await reload() }
        .lbConfirm($confirm)
    }

    private var cnyText: String {
        guard let raw = PriceAlertAPI.threshold(threshold),
              let v = Decimal(string: raw, locale: Dec.posix) else { return "--" }
        return Fmt.fiat(v, rate: AppConfig.usdtCny)
    }

    private func alertLabel(_ item: PriceAlertAPI.Item) -> String {
        let side = item.condition == "lte" ? "跌到" : "涨到"
        switch item.triggerMode ?? "once" {
        case "daily": return "每天·\(side)"
        case "repeat": return "持续·\(side)"
        default: return side
        }
    }

    private func chip(_ title: String, _ on: Bool, _ tap: @escaping () -> Void) -> some View {
        Text(title).font(.lb(FontSize.small, on ? .semibold : .regular)).foregroundColor(p.textPrimary)
            .frame(maxWidth: .infinity).frame(height: 36)
            .background(RoundedRectangle(cornerRadius: Radius.md).stroke(on ? p.textPrimary : p.border))
            .onTapGesture(perform: tap)
    }

    private func reload() async {
        guard session.isLoggedIn else { items = []; return }
        do { items = try await PriceAlertAPI.list(symbol: symbol).filter { $0.symbol == symbol } }
        catch { self.error = error.lbMessage }
    }

    private func create() async {
        guard PriceAlertAPI.threshold(threshold) != nil else { error = "请输入数字价格"; return }
        saving = true
        defer { saving = false }
        do {
            try await PriceAlertAPI.create(symbol: symbol, condition: rise ? "gte" : "lte", threshold: threshold, mode: mode)
            error = nil
            ToastCenter.shared.show("已创建提醒")
            await reload()
        } catch { self.error = error.lbMessage }
    }

    private func deleteAll() async {
        for item in items {
            do { try await PriceAlertAPI.delete(item.alertId) } catch { self.error = error.lbMessage; break }
        }
        await reload()
    }
}
