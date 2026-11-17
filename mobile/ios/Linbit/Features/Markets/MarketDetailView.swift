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
            NavBar(title: futures ? "\(symbol) 永续" : pair, onBack: { Router.shared.marketDetail = nil }) {
                Button { watch.toggle(symbol) } label: {
                    Image(systemName: watch.contains(symbol) ? "star.fill" : "star")
                        .foregroundColor(watch.contains(symbol) ? p.brand : p.textPrimary)
                }.buttonStyle(.plain).accessibilityLabel(watch.contains(symbol) ? "移出自选" : "加入自选")
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
                    .font(.lbNum(28, .bold)).foregroundColor(change < 0 ? p.down : p.up)
                Text(last > 0 ? "≈ " + Fmt.fiat(last, rate: AppConfig.usdtCny) : "")
                    .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                Text(Fmt.percent(change)).font(.lbNum(FontSize.body, .medium)).foregroundColor(change < 0 ? p.down : p.up)
            }
            Spacer(minLength: 8)
            VStack(alignment: .leading, spacing: 6) {
                stat("24h最高价", live.high > 0 ? Fmt.price(live.high, precision: live.pricePrec) : "--")
                stat("24h最低价", live.low > 0 ? Fmt.price(live.low, precision: live.pricePrec) : "--")
                stat("24h成交量(\(base))", Fmt.compact(live.volume))
                stat("24h成交额(USDT)", Fmt.compact(live.quoteVolume))
            }.frame(width: 150, alignment: .leading)
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
            Button { showIntervals = true } label: {
                Text("更多").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }.buttonStyle(.plain)
        }
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
                studyChip("VOL", studies.vol) { studies.vol.toggle() }
                studyChip("MACD", studies.macd) { studies.macd.toggle() }
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
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 16) {
                Label("买入", systemImage: "circle.fill").foregroundColor(p.up)
                Label("卖出", systemImage: "circle.fill").foregroundColor(p.down)
            }.font(.lb(10))
            if book.bids.isEmpty && book.asks.isEmpty { EmptyStateView(text: "暂无深度") }
            else { DepthPlot(book: book, up: p.up, down: p.down).frame(height: 180) }
        }
    }

    private var fillsList: some View {
        VStack(alignment: .leading, spacing: 8) {
            if live.trades.isEmpty { EmptyStateView(text: "暂无公开成交") }
            else {
                HStack {
                    Text("时间").frame(maxWidth: .infinity, alignment: .leading)
                    Text("价格(USDT)").frame(width: 110, alignment: .trailing)
                    Text("数量(\(base))").frame(width: 90, alignment: .trailing)
                }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                ForEach(live.trades) { t in
                    HStack {
                        Text(Self.clock.string(from: t.time)).frame(maxWidth: .infinity, alignment: .leading)
                        Text(Fmt.price(Dec.parse(t.price), precision: live.pricePrec)).foregroundColor(t.side == "buy" ? p.up : p.down).frame(width: 110, alignment: .trailing)
                        Text(Fmt.amount(Dec.parse(t.quantity), 5)).frame(width: 90, alignment: .trailing)
                    }.font(.lbNum(12)).foregroundColor(p.textPrimary).frame(height: 28)
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
                for row in rows { path.addLine(to: CGPoint(x: x(row.0), y: size.height - CGFloat(row.1 / maxV) * (size.height - 8))) }
                if let last = rows.last { path.addLine(to: CGPoint(x: x(last.0), y: size.height)) }
                path.closeSubpath()
                ctx.fill(path, with: .color(color.opacity(0.25)))
                ctx.stroke(path, with: .color(color), lineWidth: 1.5)
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

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "价格预警", onBack: onBack)
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    Text(symbol).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    Text("按现货指数价比较，通知在站内。").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    HStack(spacing: 8) {
                        chip("价格涨到", rise) { rise = true }
                        chip("价格跌到", !rise) { rise = false }
                    }
                    HStack(spacing: 8) {
                        chip("只提醒一次", mode == "once") { mode = "once" }
                        chip("每天一次", mode == "daily") { mode = "daily" }
                        chip("持续提醒", mode == "repeat") { mode = "repeat" }
                    }
                    LBTextField(placeholder: "价格", text: $threshold, keyboard: .decimalPad)
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    LBButton(title: "创建", enabled: PriceAlertAPI.threshold(threshold) != nil && !saving, loading: saving) {
                        Task { await create() }
                    }
                    HStack {
                        Text("当前提醒").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                        Spacer()
                        if !items.isEmpty {
                            Button("删除全部") { confirm = ConfirmRequest(title: "删除全部提醒", message: "删除 \(symbol) 的 \(items.count) 条提醒。", confirmTitle: "删除", destructive: true) { Task { await deleteAll() } } }
                                .font(.lb(FontSize.small)).foregroundColor(p.down)
                        }
                    }.padding(.top, 8)
                    if items.isEmpty { EmptyStateView(text: "暂无提醒") }
                    ForEach(items) { item in
                        HStack {
                            Text(alertLabel(item)).foregroundColor(p.textSecondary)
                            Text(item.threshold).foregroundColor(p.textPrimary)
                            Spacer()
                            Text(item.status).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        }.font(.lbNum(FontSize.label)).frame(height: 36)
                    }
                }.padding(Spacing.pageH)
            }
        }
        .onAppear { if threshold.isEmpty { threshold = suggested } }
        .task(id: session.isLoggedIn) { await reload() }
        .lbConfirm($confirm)
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
