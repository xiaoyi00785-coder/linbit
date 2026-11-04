import SwiftUI

/// 右侧盘口（对照图 #28）：卖 5 档 / 最新价 + ≈¥ / 买 5 档 / 买卖比 / 精度 & 显示模式
struct OrderBookView: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: SpotModel
    @Binding var showPrecision: Bool
    @AppStorage("lb.book.mode") private var mode = 0   // 0 双边 1 只看买 2 只看卖

    private func agg(_ l: [BookLevel], up: Bool) -> [BookLevel] {
        let step = m.bookPrecision
        var out: [Decimal: Decimal] = [:]
        for x in l {
            var q = x.price / step, r = Decimal(); NSDecimalRound(&r, &q, 0, up ? .up : .down)
            out[r * step, default: 0] += x.qty
        }
        return out.map { BookLevel(price: $0.key, qty: $0.value) }.sorted { up ? $0.price < $1.price : $0.price > $1.price }
    }
    private var rows: Int { mode == 0 ? 5 : 10 }

    var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .top) {
                Text("价格\n(\(m.quote))"); Spacer(); Text("数量\n(\(m.base))").multilineTextAlignment(.trailing)
            }.font(.lb(11)).foregroundColor(p.textSecondary).padding(.bottom, 6)

            switch m.bookState {
            case .idle, .loading: SkeletonList(rows: 8).frame(height: 300)
            case .failed(let e): ErrorStateView(message: e) { Task { await m.loadBook() } }.frame(height: 300)
            case .loaded(_):
                let asks = Array(agg(m.book.asks, up: true).prefix(rows)).reversed()
                let bids = Array(agg(m.book.bids, up: false).prefix(rows))
                let maxQ = (Array(asks) + bids).map(\.qty).max() ?? 1
                if mode != 1 { side(Array(asks), up: false, maxQ: maxQ, pad: true) }
                mid
                if mode != 2 { side(bids, up: true, maxQ: maxQ, pad: false) }
            }
            ratioBar.padding(.top, 8)
            HStack(spacing: 6) {
                Button { mode = (mode + 1) % 3; Haptics.tap() } label: { BookModeIcon(mode: mode) }
                Button { showPrecision = true } label: {
                    HStack {
                        Text(NSDecimalNumber(decimal: m.bookPrecision).stringValue).font(.lbNum(11))
                        Spacer()
                        Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6))
                    }.foregroundColor(p.textPrimary).padding(.horizontal, 8).frame(height: 22)
                        .background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                }.buttonStyle(PressableStyle())
            }.padding(.top, 8)
        }
    }

    private func side(_ l: [BookLevel], up: Bool, maxQ: Decimal, pad: Bool) -> some View {
        VStack(spacing: 0) {
            if pad && l.count < rows { ForEach(0..<(rows - l.count), id: \.self) { _ in row(nil, up: up, maxQ: maxQ) } }
            ForEach(l, id: \.price) { row($0, up: up, maxQ: maxQ) }
            if !pad && l.count < rows { ForEach(0..<(rows - l.count), id: \.self) { _ in row(nil, up: up, maxQ: maxQ) } }
        }
    }
    private func row(_ x: BookLevel?, up: Bool, maxQ: Decimal) -> some View {
        Button { if let x { m.pickBookPrice(x.price) } } label: {
            HStack {
                Text(x.map { Fmt.price($0.price, precision: m.pricePrec) } ?? "--").foregroundColor(up ? p.up : p.down)
                Spacer()
                Text(x.map { Fmt.compact($0.qty) } ?? "--").foregroundColor(p.textPrimary)
            }
            .font(.lbNum(12)).frame(height: 22)
            .background(alignment: .trailing) {
                GeometryReader { g in
                    let r = x.map { maxQ > 0 ? NSDecimalNumber(decimal: $0.qty / maxQ).doubleValue : 0 } ?? 0
                    Rectangle().fill(up ? p.upTint : p.downTint).frame(width: g.size.width * min(r, 1))
                        .frame(maxWidth: .infinity, alignment: .trailing)
                }
            }
        }.buttonStyle(RowPressStyle())
    }
    private var mid: some View {
        Button { m.pickBookPrice(m.last) } label: {
            VStack(alignment: .leading, spacing: 1) {
                Text(m.last > 0 ? Fmt.price(m.last, precision: m.pricePrec) : "--")
                    .font(.lbNum(17, .bold)).foregroundColor(m.changePct < 0 ? p.down : p.textPrimary)
                Text("≈ " + Fmt.fiat(m.last, rate: AppConfig.usdtCny)).font(.lbNum(11)).foregroundColor(p.textSecondary)
            }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 6)
        }.buttonStyle(RowPressStyle())
    }
    private var ratioBar: some View {
        let b = m.book.buyRatio
        return HStack(spacing: 4) {
            Text("B \(Int((b * 100).rounded()))%").foregroundColor(p.up)
            GeometryReader { g in
                HStack(spacing: 2) {
                    Capsule().fill(p.up).frame(width: max(g.size.width * b - 1, 0))
                    Capsule().fill(p.down)
                }
            }.frame(height: 3)
            Text("\(Int(((1 - b) * 100).rounded()))% S").foregroundColor(p.down)
        }.font(.lbNum(10))
    }
}

struct BookModeIcon: View {
    @Environment(\.palette) private var p
    let mode: Int
    var body: some View {
        VStack(spacing: 1.5) {
            ForEach(0..<2, id: \.self) { i in
                HStack(spacing: 1.5) {
                    RoundedRectangle(cornerRadius: 1).fill(color(i)).frame(width: 6, height: 6)
                    VStack(spacing: 1) { ForEach(0..<2, id: \.self) { _ in Rectangle().fill(p.textTertiary).frame(width: 6, height: 2) } }
                }
            }
        }.frame(width: 22, height: 22).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
    }
    private func color(_ i: Int) -> Color { mode == 0 ? (i == 0 ? p.down : p.up) : mode == 1 ? p.up : p.down }
}

/// 委托类型 Sheet（对照图 #19：图标 + 名称，选中黑底对勾）
struct OrderKindSheet: View {
    @Environment(\.palette) private var p
    let selected: SpotOrderKind
    let onPick: (SpotOrderKind) -> Void
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "委托类型", onClose: onClose) {
            VStack(spacing: 0) {
                ForEach(SpotOrderKind.allCases) { k in
                    Button { onPick(k) } label: {
                        HStack(spacing: 14) {
                            Image(systemName: k.icon).font(.system(size: 15)).frame(width: 20).foregroundColor(p.textPrimary)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(k.title).font(.lb(FontSize.body)).foregroundColor(k.available ? p.textPrimary : p.textTertiary)
                                Text(k.desc).font(.lb(11)).foregroundColor(p.textTertiary)
                            }
                            Spacer()
                            if k == selected { Image(systemName: "checkmark.circle.fill").font(.system(size: 18)).foregroundColor(p.textPrimary) }
                        }.padding(.vertical, 12).contentShape(Rectangle())
                    }.buttonStyle(RowPressStyle())
                }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 20)
        }
    }
}

/// 下单确认 Sheet
struct OrderConfirmSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: SpotModel
    @Binding var skip: Bool
    let onConfirm: () -> Void
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "\(m.isBuy ? "买入" : "卖出") \(m.base)/\(m.quote)", onClose: onClose) {
            VStack(spacing: 12) {
                kv("委托类型", m.kind.title)
                kv("方向", m.isBuy ? "买入" : "卖出", color: m.isBuy ? p.up : p.down)
                if m.isPlan { kv("触发价", "\(m.triggerText) \(m.quote)") }
                kv("委托价格", m.marketLike ? "市价" : "\(m.priceText) \(m.quote)")
                if m.byAmount { kv("下单方式", "按金额") } else { kv("委托数量", "\(SpotModel.fmt(m.qty, m.qtyPrec)) \(m.base)") }
                kv(m.marketLike && !m.byAmount ? "预估交易额" : "交易额", "\(Fmt.amount(m.amount, 2)) \(m.quote)")
                kv("手续费率", SpotCond.pct(m.feeRate))
                kv("预估手续费", "\(Fmt.amount(m.fee, 4)) \(m.quote)")
                if m.tpsl && !m.isPlan {
                    if Dec.parse(m.tpText) > 0 { kv("止盈触发价", "\(m.tpText) \(m.quote)") }
                    if Dec.parse(m.slText) > 0 { kv("止损触发价", "\(m.slText) \(m.quote)") }
                }
                if m.isPlan {
                    InlineBanner(text: "最新价到达触发价后按\(m.planMarket ? "市价" : "委托价格")下单，触发前将冻结相应资金")
                } else if m.kind == .market {
                    InlineBanner(text: "市价单将以盘口最优价格立即成交，实际成交价可能与预估存在偏差")
                }
                HStack { Checkbox(on: $skip, label: "不再提示（可在偏好设置中恢复）"); Spacer() }.padding(.top, 4)
                LBButton(title: "确认\(m.isBuy ? "买入" : "卖出")", kind: m.isBuy ? .buy : .sell, loading: m.placing, action: onConfirm)
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 20)
        }
    }
    private func kv(_ k: String, _ v: String, color: Color? = nil) -> some View {
        HStack {
            Text(k).font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v).font(.lbNum(FontSize.label, .medium)).foregroundColor(color ?? p.textPrimary)
        }
    }
}

/// 盘口精度
struct PrecisionSheet: View {
    @ObservedObject var m: SpotModel
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "价格精度", onClose: onClose) {
            VStack(spacing: 0) {
                ForEach(options, id: \.self) { d in
                    SheetOptionRow(title: NSDecimalNumber(decimal: d).stringValue, selected: d == m.bookPrecision) {
                        m.bookPrecision = d; onClose()
                    }
                }
            }.padding(.bottom, 20)
        }
    }
    private var options: [Decimal] {
        var base = Decimal(1); for _ in 0..<m.pricePrec { base /= 10 }
        return [base, base * 10, base * 100, base * 1000]
    }
}

/// 切换交易对
struct PairPickerSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject private var store = MarketStore.shared
    let current: String
    let onPick: (String) -> Void
    let onClose: () -> Void
    @State private var q = ""
    var body: some View {
        LBSheet(title: "选择交易对", onClose: onClose) {
            VStack(spacing: 8) {
                SearchField(text: $q, placeholder: "搜索币种").padding(.horizontal, Spacing.pageH)
                let list = store.spot.filter { q.isEmpty || $0.symbol.localizedCaseInsensitiveContains(q) }
                if list.isEmpty { EmptyStateView(text: "暂无相关币种").frame(height: 240) }
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(list, id: \.symbol) { r in
                            Button { onPick(r.symbol) } label: {
                                HStack {
                                    Text(r.symbol.replacingOccurrences(of: "USDT", with: "/USDT")).font(.lb(FontSize.body, .medium))
                                        .foregroundColor(r.symbol == current ? p.brand : p.textPrimary)
                                    Spacer()
                                    Text(Fmt.priceAuto(r.last)).font(.lbNum(FontSize.label)).foregroundColor(p.textPrimary)
                                    Text(Fmt.percent(r.changePct)).font(.lbNum(FontSize.small)).foregroundColor(r.changePct < 0 ? p.down : p.up).frame(width: 64, alignment: .trailing)
                                }.padding(.horizontal, Spacing.pageH).frame(height: 48).contentShape(Rectangle())
                            }.buttonStyle(RowPressStyle())
                        }
                    }
                }.frame(height: 420)
            }
        }
        .task { if store.spot.isEmpty { await store.load() } }
    }
}
