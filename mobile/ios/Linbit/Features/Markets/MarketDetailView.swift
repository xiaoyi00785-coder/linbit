import SwiftUI

/// 币种详情。K 线周期只用接口支持的 8 档。公开成交流没有接口，成交页显示本人成交。
struct MarketDetailView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    @ObservedObject private var watch = Watchlist.shared
    let symbol: String
    @State private var ticker: APITicker?
    @State private var book = OrderBook()
    @State private var interval = "15m"
    @State private var candles: [APIFuturesKlinesKlinesInner] = []
    @State private var candleLoading = false
    @State private var candleError: String?
    @State private var section = 0
    @State private var error: String?
    @State private var fills: [APITrade] = []
    @State private var fillsNote: String?
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
        .task { await load() }
        .task(id: interval) { await loadCandles() }
        .task(id: session.isLoggedIn) { await loadFills() }
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
            NavBar(title: pair, onBack: { Router.shared.marketDetail = nil }) {
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
                    Picker("", selection: $section) {
                        Text("订单表").tag(0)
                        Text("深度图").tag(1)
                        Text("我的成交").tag(2)
                    }.pickerStyle(.segmented)
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
                LBButton(title: "交易", kind: .buy) { openTrade() }
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
        let base = ticker.map { String($0.symbol.dropLast(4)) } ?? String(symbol.dropLast(4))
        let quote = symbol.hasSuffix("USDT") ? "USDT" : String(symbol.suffix(4))
        return "\(base)/\(quote)"
    }
    private var base: String { ticker.map { String($0.symbol.dropLast(4)) } ?? String(symbol.dropLast(4)) }
    private var last: Decimal { Dec.parse(ticker?.lastPrice) }
    private var change: Decimal { Dec.parse(ticker?.priceChangePercent) }

    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(last > 0 ? Fmt.priceAuto(last) : "--")
                    .font(.lbNum(28, .bold)).foregroundColor(change < 0 ? p.down : p.up)
                Text(last > 0 ? "≈ " + Fmt.fiat(last, rate: AppConfig.usdtCny) : "")
                    .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                Text(Fmt.percent(change)).font(.lbNum(FontSize.body, .medium)).foregroundColor(change < 0 ? p.down : p.up)
            }
            Spacer(minLength: 8)
            VStack(alignment: .leading, spacing: 6) {
                stat("24h最高价", Fmt.priceAuto(Dec.parse(ticker?.high)))
                stat("24h最低价", Fmt.priceAuto(Dec.parse(ticker?.low)))
                stat("24h成交量(\(base))", Fmt.compact(Dec.parse(ticker?.volume)))
                stat("24h成交额(USDT)", Fmt.compact(Dec.parse(ticker?.quoteVolume)))
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
        if candleLoading && candles.isEmpty {
            ProgressView().frame(maxWidth: .infinity).frame(height: 220)
        } else if let candleError, candles.isEmpty {
            Text(candleError).font(.lb(FontSize.small)).foregroundColor(p.down).frame(maxWidth: .infinity).frame(height: 120)
        } else if candles.isEmpty {
            EmptyStateView(text: "暂无 K 线").frame(height: 160)
        } else {
            CandleChart(candles: candles, pricePrec: 2).frame(height: 220)
        }
        if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
    }

    private var bookList: some View {
        VStack(spacing: 0) {
            ratioBar
            HStack {
                Text("价格 (USDT)").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                Text("数量 (USDT)").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            if book.asks.isEmpty && book.bids.isEmpty { EmptyStateView(text: "暂无盘口") }
            ForEach(Array(book.asks.prefix(8)).reversed()) { level($0, up: false) }
            Text(last > 0 ? Fmt.priceAuto(last) : "--").font(.lbNum(18, .bold)).foregroundColor(change < 0 ? p.down : p.up)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 6)
            ForEach(Array(book.bids.prefix(8))) { level($0, up: true) }
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
            Text("公开成交流没有接口，这里是你在 \(pair) 的成交。")
                .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            if let fillsNote { Text(fillsNote).font(.lb(FontSize.small)).foregroundColor(p.down) }
            if !session.isLoggedIn { EmptyStateView(text: "登录后查看成交") }
            else if fills.isEmpty { EmptyStateView(text: "暂无成交") }
            else {
                HStack {
                    Text("时间").frame(maxWidth: .infinity, alignment: .leading)
                    Text("价格").frame(width: 110, alignment: .trailing)
                    Text("数量").frame(width: 80, alignment: .trailing)
                }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                ForEach(fills, id: \.tradeId) { t in
                    HStack {
                        Text(Fmt.dateTime(t.createdAt)).frame(maxWidth: .infinity, alignment: .leading)
                        Text(t.price).foregroundColor(t.side == .buy ? p.up : p.down).frame(width: 110, alignment: .trailing)
                        Text(t.quantity).frame(width: 80, alignment: .trailing)
                    }.font(.lbNum(12)).foregroundColor(p.textPrimary).frame(height: 28)
                }
            }
        }
    }

    private func level(_ x: BookLevel, up: Bool) -> some View {
        HStack {
            Text(Fmt.priceAuto(x.price)).foregroundColor(up ? p.up : p.down)
            Spacer()
            Text(Fmt.compact(x.price * x.qty)).foregroundColor(p.textPrimary)
        }.font(.lbNum(12)).frame(height: 22)
    }

    private func openTrade() {
        let sym = symbol
        Router.shared.marketDetail = nil
        Router.shared.trade(sym)
    }

    private func plainPrice(_ d: Decimal) -> String {
        d > 0 ? NSDecimalNumber(decimal: d).stringValue : ""
    }

    private func load() async {
        do {
            let r: APIGetTicker200Response = try await APIClient.shared.get("/market/ticker", query: ["symbol": symbol])
            if case let .typeAPITicker(t) = r { ticker = t }
            book = try await SpotAPI.depth(symbol)
            error = nil
        } catch { self.error = error.lbMessage }
    }

    private func loadCandles() async {
        candleLoading = true
        candleError = nil
        defer { candleLoading = false }
        do {
            let rows: [SpotKline] = try await APIClient.shared.get("/market/klines", query: ["symbol": symbol, "interval": interval, "limit": "120"])
            candles = rows.map { APIFuturesKlinesKlinesInner(_open: $0.open, high: $0.high, low: $0.low, close: $0.close) }
        } catch {
            candles = []
            candleError = error.lbMessage
        }
    }

    private func loadFills() async {
        guard session.isLoggedIn else { fills = []; fillsNote = nil; return }
        do {
            fills = try await SpotAPI.trades(symbol: symbol, page: 1).items.filter { $0.symbol == symbol }
            fillsNote = nil
        } catch { fillsNote = error.lbMessage }
    }
}

/// 买卖累计深度。横轴是档位，纵轴是累计数量。
private struct DepthPlot: View {
    let book: OrderBook
    let up: Color
    let down: Color
    var body: some View {
        Canvas { ctx, size in
            let bids = Array(book.bids.prefix(20))
            let asks = Array(book.asks.prefix(20))
            func cum(_ rows: [BookLevel]) -> [Double] {
                var s = 0.0
                return rows.map { s += NSDecimalNumber(decimal: $0.qty).doubleValue; return s }
            }
            let b = cum(bids), a = cum(asks)
            let maxV = max(b.max() ?? 0, a.max() ?? 0, 1)
            func path(_ values: [Double], fromRight: Bool) -> Path {
                var p = Path()
                guard !values.isEmpty else { return p }
                let step = (size.width / 2) / CGFloat(values.count)
                for (i, v) in values.enumerated() {
                    let x = fromRight ? size.width - step * CGFloat(i) : step * CGFloat(i)
                    let y = size.height - CGFloat(v / maxV) * (size.height - 8)
                    if i == 0 { p.move(to: CGPoint(x: x, y: size.height)); p.addLine(to: CGPoint(x: x, y: y)) }
                    else { p.addLine(to: CGPoint(x: x, y: y)) }
                }
                let endX = fromRight ? size.width - step * CGFloat(values.count) : step * CGFloat(values.count)
                p.addLine(to: CGPoint(x: endX, y: size.height))
                p.closeSubpath()
                return p
            }
            ctx.fill(path(b, fromRight: false), with: .color(up.opacity(0.25)))
            ctx.stroke(path(b, fromRight: false), with: .color(up), lineWidth: 1.5)
            ctx.fill(path(a, fromRight: true), with: .color(down.opacity(0.25)))
            ctx.stroke(path(a, fromRight: true), with: .color(down), lineWidth: 1.5)
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

/// 现货价格预警。条件只有涨到 / 跌到，频率只有一次，和 /price-alerts 一致。
private struct SpotAlertPage: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    let symbol: String
    let suggested: String
    let onBack: () -> Void
    @State private var rise = true
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
                    Text("按现货指数价比较，只提醒一次，通知在站内。").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    HStack(spacing: 8) {
                        chip("价格涨到", rise) { rise = true }
                        chip("价格跌到", !rise) { rise = false }
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
                            Text(item.condition == "lte" ? "跌到" : "涨到").foregroundColor(p.textSecondary)
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
            try await PriceAlertAPI.create(symbol: symbol, condition: rise ? "gte" : "lte", threshold: threshold)
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
