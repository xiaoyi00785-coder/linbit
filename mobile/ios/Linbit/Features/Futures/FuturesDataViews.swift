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
        if candles.value == nil { candles = .loading }
        do { candles = .loaded(try await FuturesAPI.klines(symbol, interval: interval)) } catch { candles = .failed(error.lbMessage) }
    }
    private func loadTrades() async {
        if trades.value == nil { trades = .loading }
        do { trades = .loaded(try await FuturesAPI.marketTrades(symbol)) } catch { trades = .failed(error.lbMessage) }
    }
}

/// 一根 K 线。time 一律是开盘时间的秒。volume 是成交量，不是成交额。
struct ChartBar: Identifiable, Hashable {
    var time: Int64
    var open, high, low, close, volume: Decimal
    var id: Int64 { time }
}

struct ChartStudies: Equatable {
    var ma = true
    var boll = false
    var ema = false
    var vol = true
    var macd = false
}

enum ChartMath {
    static func sma(_ n: Int, _ v: [Double]) -> [Double?] {
        guard n > 0 else { return Array(repeating: nil, count: v.count) }
        var out = [Double?](repeating: nil, count: v.count)
        var sum = 0.0
        for i in v.indices {
            sum += v[i]
            if i >= n { sum -= v[i - n] }
            if i >= n - 1 { out[i] = sum / Double(n) }
        }
        return out
    }
    static func ema(_ n: Int, _ v: [Double]) -> [Double?] {
        guard n > 1, v.count >= n else { return Array(repeating: nil, count: v.count) }
        var out = [Double?](repeating: nil, count: v.count)
        let seed = v.prefix(n).reduce(0, +) / Double(n)
        out[n - 1] = seed
        let k = 2.0 / Double(n + 1)
        var prev = seed
        if n < v.count {
            for i in n..<v.count {
                prev = v[i] * k + prev * (1 - k)
                out[i] = prev
            }
        }
        return out
    }
    static func boll(_ n: Int, _ v: [Double], mult: Double = 2) -> (mid: [Double?], up: [Double?], lo: [Double?]) {
        let mid = sma(n, v)
        var up = [Double?](repeating: nil, count: v.count)
        var lo = [Double?](repeating: nil, count: v.count)
        guard n > 1 else { return (mid, up, lo) }
        for i in v.indices where i >= n - 1 {
            guard let m = mid[i] else { continue }
            let slice = v[(i - n + 1)...i]
            let variance = slice.reduce(0) { $0 + ($1 - m) * ($1 - m) } / Double(n)
            let sd = variance.squareRoot()
            up[i] = m + mult * sd
            lo[i] = m - mult * sd
        }
        return (mid, up, lo)
    }
    static func last(_ s: [Double?]) -> Double? { s.compactMap { $0 }.last }
}

/// 蜡烛图。MA / BOLL / EMA / VOL / MACD 都从这组 K 线算，不另拉数据。
struct CandleChart: View {
    @Environment(\.palette) private var p
    let bars: [ChartBar]
    let pricePrec: Int
    var studies = ChartStudies()
    /// 右侧被藏起来的根数。0 表示贴着最新一根，实时更新时跟着走。
    @State private var right = 0
    @State private var candleW: CGFloat = 8
    @State private var follow = true
    @State private var seen = 0
    @State private var cross: Int? = nil
    init(bars: [ChartBar], pricePrec: Int, studies: ChartStudies = ChartStudies()) {
        self.bars = bars; self.pricePrec = pricePrec; self.studies = studies
    }
    init(candles: [APIFuturesKlinesKlinesInner], pricePrec: Int, studies: ChartStudies = ChartStudies()) {
        self.pricePrec = pricePrec
        self.studies = studies
        self.bars = candles.compactMap { c in
            guard let t = c.openTime else { return nil }
            return ChartBar(time: t > 1_000_000_000_000 ? t / 1000 : t, open: Dec.parse(c._open), high: Dec.parse(c.high), low: Dec.parse(c.low), close: Dec.parse(c.close), volume: Dec.parse(c.volume))
        }
    }
    var body: some View {
        let closes = bars.map { Dec.double($0.close) }
        let vols = bars.map { Dec.double($0.volume) }
        let ma5 = studies.ma ? ChartMath.sma(5, closes) : []
        let ma10 = studies.ma ? ChartMath.sma(10, closes) : []
        let ma20 = studies.ma ? ChartMath.sma(20, closes) : []
        let boll = studies.boll ? ChartMath.boll(20, closes) : (mid: [Double?](), up: [Double?](), lo: [Double?]())
        let ema7 = studies.ema ? ChartMath.ema(7, closes) : []
        let ema30 = studies.ema ? ChartMath.ema(30, closes) : []
        let ema12 = studies.macd ? ChartMath.ema(12, closes) : []
        let ema26 = studies.macd ? ChartMath.ema(26, closes) : []
        let vma5 = studies.vol ? ChartMath.sma(5, vols) : []
        let vma10 = studies.vol ? ChartMath.sma(10, vols) : []
        let hiAll = bars.map(\.high).max() ?? 0
        let loAll = bars.map(\.low).min() ?? 0
        let maxVAll = bars.map(\.volume).max() ?? 0
        VStack(alignment: .leading, spacing: 2) {
            legend(ma5: ma5, ma10: ma10, ma20: ma20, boll: boll, ema7: ema7, ema30: ema30)
            GeometryReader { geo in
                let width = max(geo.size.width, 1)
                let count = max(20, min(bars.count, Int(width / candleW)))
                let end = min(bars.count, max(count, bars.count - right))
                let start = max(0, end - count)
                let shown = Array(bars[start..<end])
                let hi = shown.map(\.high).max() ?? hiAll
                let lo = shown.map(\.low).min() ?? loAll
                let maxV = shown.map(\.volume).max() ?? maxVAll
                ZStack(alignment: .topLeading) {
                Canvas { ctx, size in
                    let n = max(shown.count, 1)
                    let step = size.width / CGFloat(n)
                    let bands = (studies.vol ? 1 : 0) + (studies.macd ? 1 : 0)
                    let priceH = size.height * (bands == 0 ? 1 : bands == 1 ? 0.72 : 0.58)
                    let range = Dec.double(hi - lo)
                    func y(_ d: Decimal) -> CGFloat { range <= 0 ? priceH / 2 : CGFloat(Dec.double(hi - d) / range) * (priceH - 8) + 4 }
                    func yD(_ d: Double) -> CGFloat { y(Decimal(d)) }
                    for gy in stride(from: 0.0, through: 1.0, by: 0.25) {
                        var path = Path(); path.move(to: CGPoint(x: 0, y: priceH * gy)); path.addLine(to: CGPoint(x: size.width, y: priceH * gy))
                        ctx.stroke(path, with: .color(p.divider), lineWidth: 0.5)
                    }
                    if studies.boll {
                        var band = Path()
                        var started = false
                        let upS = Array(boll.up[start..<min(end, boll.up.count)])
                        let loS = Array(boll.lo[start..<min(end, boll.lo.count)])
                        for i in upS.indices {
                            guard let u = upS[i] else { continue }
                            let pt = CGPoint(x: step * (CGFloat(i) + 0.5), y: yD(u))
                            if started { band.addLine(to: pt) } else { band.move(to: pt); started = true }
                        }
                        for i in loS.indices.reversed() {
                            guard let l = loS[i] else { continue }
                            band.addLine(to: CGPoint(x: step * (CGFloat(i) + 0.5), y: yD(l)))
                        }
                        band.closeSubpath()
                        ctx.fill(band, with: .color(Color.purple.opacity(0.12)))
                        stroke(ctx, Array(boll.mid[start..<min(end, boll.mid.count)]), step: step, y: yD, color: .purple, width: 1)
                    }
                    let volTop = priceH + 4
                    let strip = bands == 0 ? 0 : (size.height - volTop) / CGFloat(bands)
                    let vmax = Dec.double(maxV)
                    for (i, c) in shown.enumerated() {
                        let color = c.close >= c.open ? p.up : p.down
                        let x = step * (CGFloat(i) + 0.5)
                        var wick = Path(); wick.move(to: CGPoint(x: x, y: y(c.high))); wick.addLine(to: CGPoint(x: x, y: y(c.low)))
                        ctx.stroke(wick, with: .color(color), lineWidth: 1)
                        let top = y(max(c.open, c.close)), bot = y(min(c.open, c.close))
                        let w = max(step * 0.62, 3)
                        ctx.fill(Path(CGRect(x: x - w / 2, y: top, width: w, height: max(bot - top, 1))), with: .color(color))
                        if studies.vol {
                            let vh = vmax <= 0 ? 1 : CGFloat(Dec.double(c.volume) / vmax) * (strip - 2)
                            ctx.fill(Path(CGRect(x: x - w / 2, y: volTop + strip - vh, width: w, height: max(vh, 1))), with: .color(color.opacity(0.45)))
                        }
                    }
                    if studies.ma {
                        stroke(ctx, Array(ma5[start..<min(end, ma5.count)]), step: step, y: yD, color: Color(red: 0.93, green: 0.62, blue: 0.12), width: 1)
                        stroke(ctx, Array(ma10[start..<min(end, ma10.count)]), step: step, y: yD, color: Color(red: 0.55, green: 0.36, blue: 0.86), width: 1)
                        stroke(ctx, Array(ma20[start..<min(end, ma20.count)]), step: step, y: yD, color: Color(red: 0.95, green: 0.45, blue: 0.2), width: 1)
                    }
                    if studies.ema {
                        stroke(ctx, Array(ema7[start..<min(end, ema7.count)]), step: step, y: yD, color: .blue, width: 1)
                        stroke(ctx, Array(ema30[start..<min(end, ema30.count)]), step: step, y: yD, color: .cyan, width: 1)
                    }
                    if studies.vol {
                        stroke(ctx, Array(vma5[start..<min(end, vma5.count)]), step: step, y: { v in volTop + strip - (vmax <= 0 ? 0 : CGFloat(v / vmax) * (strip - 2)) }, color: Color(red: 0.93, green: 0.62, blue: 0.12), width: 1)
                        stroke(ctx, Array(vma10[start..<min(end, vma10.count)]), step: step, y: { v in volTop + strip - (vmax <= 0 ? 0 : CGFloat(v / vmax) * (strip - 2)) }, color: Color(red: 0.55, green: 0.36, blue: 0.86), width: 1)
                    }
                    if studies.macd {
                        let diffAll = zip(ema12, ema26).map { a, b -> Double? in
                            guard let a, let b else { return nil }
                            return a - b
                        }
                        let diff = Array(diffAll[start..<min(end, diffAll.count)])
                        let signal = Array(ChartMath.ema(9, diffAll.map { $0 ?? 0 })[start..<min(end, diffAll.count)])
                        let top = studies.vol ? volTop + strip : volTop
                        let vals = diff.compactMap { $0 }
                        let peak = max(vals.map(abs).max() ?? 1, 0.0001)
                        for (i, d) in diff.enumerated() {
                            guard let d else { continue }
                            let h = CGFloat(abs(d) / peak) * (strip / 2 - 1)
                            let mid = top + strip / 2
                            let y0 = d >= 0 ? mid - h : mid
                            let x = step * (CGFloat(i) + 0.5)
                            let w = max(step * 0.6, 1)
                            ctx.fill(Path(CGRect(x: x - w / 2, y: y0, width: w, height: max(h, 1))), with: .color(d >= 0 ? p.up : p.down))
                        }
                        stroke(ctx, diff, step: step, y: { v in top + strip / 2 - CGFloat(v / peak) * (strip / 2 - 1) }, color: .orange, width: 1)
                        stroke(ctx, signal, step: step, y: { v in top + strip / 2 - CGFloat(v / peak) * (strip / 2 - 1) }, color: .blue, width: 1)
                    }
                    if let mark = cross, shown.indices.contains(mark) {
                        let x = step * (CGFloat(mark) + 0.5)
                        var v = Path(); v.move(to: CGPoint(x: x, y: 0)); v.addLine(to: CGPoint(x: x, y: size.height))
                        ctx.stroke(v, with: .color(p.textSecondary), style: StrokeStyle(lineWidth: 0.6, dash: [3, 3]))
                        let py = y(shown[mark].close)
                        var hline = Path(); hline.move(to: CGPoint(x: 0, y: py)); hline.addLine(to: CGPoint(x: size.width, y: py))
                        ctx.stroke(hline, with: .color(p.textSecondary), style: StrokeStyle(lineWidth: 0.6, dash: [3, 3]))
                    }
                }
                .frame(height: 248)
                ChartTouch(right: right, candleW: candleW,
                    onDrag: { proposed in
                        let next = min(max(0, bars.count - count), max(0, proposed))
                        right = next
                        follow = next == 0
                        cross = nil
                    },
                    onPinch: { width in candleW = min(16, max(5, width)) },
                    onPress: { x, active in
                        guard active, count > 0 else { return }
                        cross = min(count - 1, max(0, Int(x / width * CGFloat(count))))
                    })
                if let bar = cross.flatMap({ shown.indices.contains($0) ? shown[$0] : nil }) {
                    crossCard(bar).padding(4)
                }
                if follow, let last = shown.last {
                    VStack {
                        Spacer()
                        HStack {
                            Spacer()
                            Text(Fmt.price(last.close, precision: pricePrec))
                                .font(.lbNum(10, .medium)).foregroundColor(.white)
                                .padding(.horizontal, 4).padding(.vertical, 2)
                                .background(RoundedRectangle(cornerRadius: 2).fill(last.close >= last.open ? p.up : p.down))
                        }
                    }.padding(.bottom, studies.vol || studies.macd ? 64 : 8).allowsHitTesting(false)
                }
                }
                .onChange(of: bars.count) { n in
                    if !follow { right += max(0, n - seen) }
                    seen = n
                    if right > max(0, n - 20) { right = max(0, n - 20) }
                    if right == 0 { follow = true }
                }
                .onChange(of: bars.first?.time) { _ in follow = true; right = 0; cross = nil; seen = bars.count }
            }
            .frame(height: 248)
            Text(axisLabel).font(.lbNum(10)).foregroundColor(p.textTertiary).frame(maxWidth: .infinity)
            if studies.vol, let a = ChartMath.last(vma5), let b = ChartMath.last(vma10) {
                Text("MA-5: \(Fmt.amount(Decimal(a), 2))  MA-10: \(Fmt.amount(Decimal(b), 2))")
                    .font(.lbNum(10)).foregroundColor(p.textSecondary)
            }
        }
    }

    private var axisLabel: String {
        let width = UIScreen.main.bounds.width - 32
        let count = max(20, min(bars.count, Int(width / max(candleW, 1))))
        let end = min(bars.count, max(count, bars.count - right))
        let start = max(0, end - count)
        guard bars.indices.contains(start), bars.indices.contains(end - 1) else { return "" }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "MM-dd HH:mm"
        let a = Date(timeIntervalSince1970: Double(bars[start].time))
        let b = Date(timeIntervalSince1970: Double(bars[end - 1].time))
        return f.string(from: a) + "  –  " + f.string(from: b)
    }

    private func crossCard(_ bar: ChartBar) -> some View {
        let chg = bar.close - bar.open
        let pct = bar.open == 0 ? Decimal(0) : chg / bar.open * 100
        return VStack(alignment: .leading, spacing: 2) {
            Text(axisClock(bar.time))
            Text("开 \(Fmt.price(bar.open, precision: pricePrec))  高 \(Fmt.price(bar.high, precision: pricePrec))")
            Text("低 \(Fmt.price(bar.low, precision: pricePrec))  收 \(Fmt.price(bar.close, precision: pricePrec))")
            Text("涨跌 \(Fmt.price(chg, precision: pricePrec))  \(Fmt.percent(pct))")
        }
        .font(.lbNum(10)).foregroundColor(p.textPrimary)
        .padding(6)
        .background(RoundedRectangle(cornerRadius: 4).fill(p.bg.opacity(0.92)))
        .overlay(RoundedRectangle(cornerRadius: 4).stroke(p.border))
        .allowsHitTesting(false)
    }

    private func axisClock(_ t: Int64) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy/MM/dd HH:mm"
        return f.string(from: Date(timeIntervalSince1970: Double(t)))
    }

    private func legend(ma5: [Double?], ma10: [Double?], ma20: [Double?], boll: (mid: [Double?], up: [Double?], lo: [Double?]), ema7: [Double?], ema30: [Double?]) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            if studies.ma {
                Text(line([("MA-5", ChartMath.last(ma5)), ("MA-10", ChartMath.last(ma10)), ("MA-20", ChartMath.last(ma20))]))
                    .font(.lbNum(10)).foregroundColor(p.textSecondary)
            }
            if studies.boll, let mid = ChartMath.last(boll.mid), let up = ChartMath.last(boll.up), let lo = ChartMath.last(boll.lo) {
                Text("BOLL(20, 2) MID: \(Fmt.price(Decimal(up), precision: pricePrec))  \(Fmt.price(Decimal(mid), precision: pricePrec))  \(Fmt.price(Decimal(lo), precision: pricePrec))")
                    .font(.lbNum(10)).foregroundColor(p.textSecondary)
            }
            if studies.ema {
                Text(line([("EMA-7", ChartMath.last(ema7)), ("EMA-30", ChartMath.last(ema30))]))
                    .font(.lbNum(10)).foregroundColor(p.textSecondary)
            }
        }
    }

    private func line(_ items: [(String, Double?)]) -> String {
        items.compactMap { name, v in v.map { "\(name): \(Fmt.price(Decimal($0), precision: pricePrec))" } }.joined(separator: "  ")
    }

    private func stroke(_ ctx: GraphicsContext, _ series: [Double?], step: CGFloat, y: (Double) -> CGFloat, color: Color, width: CGFloat) {
        var path = Path()
        var started = false
        for (i, v) in series.enumerated() {
            guard let v else { continue }
            let pt = CGPoint(x: step * (CGFloat(i) + 0.5), y: y(v))
            if started { path.addLine(to: pt) } else { path.move(to: pt); started = true }
        }
        ctx.stroke(path, with: .color(color), lineWidth: width)
    }
}


/// 图表手势。横向拖动留给图表，纵向仍交给外层滚动。
struct ChartTouch: UIViewRepresentable {
    var right: Int
    var candleW: CGFloat
    var onDrag: (Int) -> Void
    var onPinch: (CGFloat) -> Void
    var onPress: (CGFloat, Bool) -> Void
    func makeUIView(context: Context) -> ChartTouchView {
        let v = ChartTouchView()
        v.right = right; v.candleW = candleW
        v.onDrag = onDrag; v.onPinch = onPinch; v.onPress = onPress
        return v
    }
    func updateUIView(_ v: ChartTouchView, context: Context) {
        v.right = right; v.candleW = candleW
        v.onDrag = onDrag; v.onPinch = onPinch; v.onPress = onPress
    }
}

final class ChartTouchView: UIView, UIGestureRecognizerDelegate {
    var right = 0
    var candleW: CGFloat = 8
    var onDrag: ((Int) -> Void)?
    var onPinch: ((CGFloat) -> Void)?
    var onPress: ((CGFloat, Bool) -> Void)?
    private var dragOrigin = 0
    private var pinchOrigin: CGFloat = 8
    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .clear
        let pan = UIPanGestureRecognizer(target: self, action: #selector(panned))
        pan.delegate = self
        let pinch = UIPinchGestureRecognizer(target: self, action: #selector(pinched))
        let press = UILongPressGestureRecognizer(target: self, action: #selector(pressed))
        press.minimumPressDuration = 0.35
        addGestureRecognizer(pan)
        addGestureRecognizer(pinch)
        addGestureRecognizer(press)
    }
    required init?(coder: NSCoder) { return nil }
    override func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
        guard let pan = g as? UIPanGestureRecognizer else { return true }
        let v = pan.velocity(in: self)
        return abs(v.x) > abs(v.y)
    }
    @objc private func panned(_ g: UIPanGestureRecognizer) {
        if g.state == .began { dragOrigin = right }
        let shift = Int((g.translation(in: self).x / max(candleW, 1)).rounded())
        onDrag?(dragOrigin + shift)
    }
    @objc private func pinched(_ g: UIPinchGestureRecognizer) {
        if g.state == .began { pinchOrigin = candleW }
        onPinch?(pinchOrigin * g.scale)
    }
    @objc private func pressed(_ g: UILongPressGestureRecognizer) {
        let active = g.state == .began || g.state == .changed
        onPress?(g.location(in: self).x, active)
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
