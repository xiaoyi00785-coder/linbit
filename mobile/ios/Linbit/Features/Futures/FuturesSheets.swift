import SwiftUI

/// 对照图 #17：调整交易模式（保证金模式 全仓/逐仓；仓位模式 合仓/分仓）
struct MarginModeSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let onClose: () -> Void
    @State private var mode = 0
    @State private var posMode = 0
    @State private var saving = false
    var body: some View {
        LBSheet(title: "调整交易模式", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 14) {
                Text("保证金模式").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                PillSegment(items: ["全仓", "逐仓"], selected: $mode, height: 40)
                Text(mode == 0 ? "全仓模式：所有全仓仓位共享账户可用余额作为保证金，盈亏可相互抵消。"
                               : "逐仓模式：每个仓位独立分配保证金，最大亏损为该仓位保证金。")
                    .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Text("仓位模式").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary).padding(.top, 4)
                PillSegment(items: ["合仓", "分仓"], selected: Binding(get: { posMode }, set: { v in
                    if v == 1 { ToastCenter.shared.show("分仓模式即将开放") } else { posMode = 0 }
                }), height: 40)
                Text("合仓模式：同一合约同方向仅持有一个仓位。").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                if !m.positions.filter({ $0.symbol == m.symbol }).isEmpty || !m.openOrders.filter({ $0.symbol == m.symbol }).isEmpty {
                    InlineBanner(text: "当前合约存在仓位或委托，切换保证金模式可能失败")
                }
                LBButton(title: "确认", loading: saving) {
                    saving = true
                    Task {
                        let ok = await m.saveLeverage(m.leverage, mode: mode == 1 ? .isolated : .cross)
                        saving = false
                        if ok { onClose() }
                    }
                }.padding(.top, 6)
            }.padding(.horizontal, Spacing.pageH)
        }
        .onAppear { mode = m.marginMode == .isolated ? 1 : 0 }
    }
}

/// 对照图 #18：调整杠杆（− 100x +，刻度 1x–100x，最大可开）
struct LeverageSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let onClose: () -> Void
    @State private var value: Double = 20
    @State private var saving = false
    private var maxL: Double { Double(max(m.maxLeverage, 1)) }
    private var ticks: [Int] {
        let mx = max(m.maxLeverage, 1)
        return [1] + (1...4).map { mx * $0 / 5 } + [mx]
    }
    var body: some View {
        let lev = Int(value.rounded())
        LBSheet(title: "调整杠杆", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    stepBtn("minus") { value = max(1, value - 1) }
                    Spacer()
                    Text("\(lev)x").font(.lbNum(22, .semibold)).foregroundColor(p.textPrimary)
                    Spacer()
                    stepBtn("plus") { value = min(maxL, value + 1) }
                }
                .padding(.horizontal, 6).frame(height: 48)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                VStack(spacing: 6) {
                    Slider(value: $value, in: 1...maxL, step: 1).tint(p.textPrimary)
                    HStack {
                        ForEach(ticks.indices, id: \.self) { i in
                            Text("\(ticks[i])x").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                                .onTapGesture { value = Double(ticks[i]) }
                            if i < ticks.count - 1 { Spacer() }
                        }
                    }
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text("· 当前杠杆倍数最高可持有头寸：\(maxPositionText(lev)) USDT").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Text("· 调整杠杆对当前合约的全部仓位与委托生效").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    if lev >= 50 {
                        Text("· 选择超过 50x 的杠杆交易会增加强行平仓的风险，请注意相关风险").font(.lb(FontSize.small)).foregroundColor(p.down)
                    }
                }
                LBButton(title: "保存", loading: saving) {
                    saving = true
                    Task {
                        let ok = await m.saveLeverage(lev, mode: m.marginMode)
                        saving = false
                        if ok { onClose() }
                    }
                }
            }.padding(.horizontal, Spacing.pageH)
        }
        .onAppear { value = Double(m.leverage) }
    }
    private func maxPositionText(_ lev: Int) -> String {
        let cap: Decimal = lev <= 20 ? 5_000_000 : lev <= 50 ? 1_000_000 : lev <= 75 ? 250_000 : 50_000
        return Fmt.amount(cap, 0)
    }
    private func stepBtn(_ icon: String, _ a: @escaping () -> Void) -> some View {
        Button(action: { Haptics.tap(); a() }) {
            Image(systemName: icon).font(.system(size: 15, weight: .semibold)).foregroundColor(p.textPrimary).frame(width: 40, height: 40)
        }
    }
}

/// 对照图 #19：委托类型
struct FuturesKindSheet: View {
    let selected: FuturesKind
    let onPick: (FuturesKind) -> Void
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "委托类型", showClose: true, onClose: onClose) {
            VStack(spacing: 0) {
                ForEach(FuturesKind.allCases, id: \.self) { k in
                    SheetOptionRow(title: k.title, subtitle: k.subtitle, selected: k == selected) { onPick(k) }
                }
            }
        }
    }
}

/// 对照图 #13：常用功能（8 宫格）
struct FuturesToolsSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    private let items: [(String, String)] = [("arrow.left.arrow.right", "资金划转"), ("doc.text", "合约历史"), ("function", "计算器"),
                                             ("percent", "资金费率"), ("book", "合约指南"), ("gearshape", "偏好设置"),
                                             ("ticket", "卡券中心"), ("chart.line.uptrend.xyaxis", "模拟交易")]
    var body: some View {
        LBSheet(title: "常用功能", showClose: true, onClose: onClose) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 20) {
                ForEach(items, id: \.1) { icon, title in
                    Button {
                        onClose()
                        switch title {
                        case "资金划转": Router.shared.open(.transfer)
                        case "卡券中心": Router.shared.openFeature(.welfare)
                        case "模拟交易": Router.shared.futuresTop = 3
                        default: ToastCenter.shared.show("\(title)即将开放")
                        }
                    } label: {
                        VStack(spacing: 8) {
                            Image(systemName: icon).font(.system(size: 18)).foregroundColor(p.textPrimary)
                                .frame(width: 44, height: 44).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                            Text(title).font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                        }
                    }.buttonStyle(PressableStyle())
                }
            }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
        }
    }
}

/// 合约切换（永续合约列表 + 搜索）
struct FuturesPairSheet: View {
    @Environment(\.palette) private var p
    let current: String
    let onPick: (String) -> Void
    let onClose: () -> Void
    @StateObject private var store = MarketStore.shared
    @State private var q = ""
    var body: some View {
        let rows = store.rows(.futures).filter { q.isEmpty || $0.symbol.localizedCaseInsensitiveContains(q) }
        LBSheet(title: "永续合约", showClose: true, onClose: onClose) {
            VStack(spacing: 8) {
                SearchField(text: $q, placeholder: "搜索合约").padding(.horizontal, Spacing.pageH)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if rows.isEmpty { EmptyStateView(text: store.futures.isEmpty ? "加载中…" : "无匹配合约") }
                        ForEach(rows) { r in
                            Button { onPick(r.symbol) } label: {
                                HStack {
                                    Text(r.symbol).font(.lb(FontSize.body, r.symbol == current ? .semibold : .regular)).foregroundColor(p.textPrimary)
                                    Text("永续").font(.lb(10)).foregroundColor(p.textSecondary)
                                    Spacer()
                                    Text(Fmt.priceAuto(r.last)).font(.lbNum(FontSize.label)).foregroundColor(p.textPrimary)
                                    Text(Fmt.percent(r.changePct)).font(.lbNum(FontSize.small)).foregroundColor(r.changePct < 0 ? p.down : p.up).frame(width: 70, alignment: .trailing)
                                }.padding(.horizontal, Spacing.pageH).frame(height: 48)
                            }.buttonStyle(RowPressStyle())
                        }
                    }
                }.frame(height: 380)
            }
        }
        .task { if store.futures.isEmpty { await store.load() } }
    }
}

/// 对照图 #21：止盈止损（开多/开空 Tab、触发价、盈利率、部分止盈止损）
struct TpslSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let position: APIFuturesPosition?
    let onClose: () -> Void
    @State private var side = 0
    @State private var tp = ""
    @State private var sl = ""
    @State private var tpPct: Double = 0
    @State private var slPct: Double = 0
    @State private var partial = false
    @State private var saving = false

    private var entry: Decimal { position.map { Dec.parse($0.entryPrice) } ?? m.orderPrice }
    private var long: Bool { position.map { $0.side != .short } ?? (side == 0) }
    private func roi(_ trigger: String) -> Decimal {
        let t = Dec.parse(trigger); guard t > 0, entry > 0 else { return 0 }
        let r = (t - entry) / entry * Decimal(position?.leverage ?? m.leverage) * 100
        return long ? r : -r
    }
    var body: some View {
        LBSheet(title: "止盈/止损", subtitle: position.map { "\($0.symbol ?? "") · 开仓价 \($0.entryPrice ?? "--")" }, showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                if position == nil {
                    PillSegment(items: ["开多", "开空"], selected: $side, tints: [p.up, p.down])
                }
                KVRow(k: "最新价格", v: Fmt.price(m.last, precision: m.pricePrec))
                trigger(title: "止盈触发价", text: $tp, pct: $tpPct, positive: true)
                trigger(title: "止损触发价", text: $sl, pct: $slPct, positive: false)
                Checkbox(on: Binding(get: { partial }, set: { v in
                    if v { ToastCenter.shared.show("部分止盈止损即将开放") }
                }), label: "部分止盈止损")
                Text("触发后将以市价全部平仓。价格剧烈波动时实际成交价可能偏离触发价。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                LBButton(title: "确定", loading: saving) { submit() }
            }.padding(.horizontal, Spacing.pageH)
        }
        .onAppear { tp = position?.tpPrice ?? m.tpText; sl = position?.slPrice ?? m.slText }
    }
    private func trigger(title: String, text: Binding<String>, pct: Binding<Double>, positive: Bool) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                NumberBox(label: title, unit: "USDT", text: text, onEdit: {})
                Text("\(positive ? "盈利率" : "亏损率") \(Fmt.amount(abs(roi(text.wrappedValue)), 2))%")
                    .font(.lbNum(FontSize.small)).foregroundColor(positive ? p.up : p.down).frame(width: 96, alignment: .trailing)
            }
            PercentSlider(value: Binding(get: { pct.wrappedValue }, set: { v in
                pct.wrappedValue = v
                let lev = Decimal(position?.leverage ?? m.leverage)
                let move = Decimal(v) * (positive ? 3 : 1) / lev     // 盈利率 0–300% / 亏损率 0–100%
                let up = positive == long
                let price = up ? entry * (1 + move) : entry * (1 - move)
                text.wrappedValue = v == 0 ? "" : Dec.plain(price, m.pricePrec)
            }), tint: positive ? p.up : p.down)
        }
    }
    private func submit() {
        let t = Dec.parse(tp), s = Dec.parse(sl)
        if !tp.isEmpty, (long ? t <= entry : t >= entry) { ToastCenter.shared.show("止盈价需\(long ? "高于" : "低于")开仓价", .error); return }
        if !sl.isEmpty, (long ? s >= entry : s <= entry) { ToastCenter.shared.show("止损价需\(long ? "低于" : "高于")开仓价", .error); return }
        if let pos = position {
            saving = true
            Task { let ok = await m.setTpsl(pos, tp: tp, sl: sl); saving = false; if ok { onClose() } }
        } else {
            m.tpsl = true; m.tpText = tp; m.slText = sl; onClose()
        }
    }
}

/// 调整保证金（增加 / 减少，逐仓）
struct AdjustMarginSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let position: APIFuturesPosition
    let onClose: () -> Void
    @State private var add = 0
    @State private var amount = ""
    @State private var saving = false
    var body: some View {
        let isolated = position.marginMode == .isolated
        LBSheet(title: "调整保证金", subtitle: "\(position.symbol ?? "") · \(isolated ? "逐仓" : "全仓") \(position.leverage ?? 0)x", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                if !isolated {
                    InlineBanner(text: "全仓仓位共享账户余额作为保证金，无需单独调整。切换为逐仓后可调整。")
                } else {
                    PillSegment(items: ["增加保证金", "减少保证金"], selected: $add)
                    NumberBox(label: "数量", unit: "USDT", text: $amount, onEdit: {})
                    KVRow(k: add == 0 ? "可增加" : "可减少", v: "\(Fmt.amount(add == 0 ? m.available : Dec.parse(position.margin), 2)) USDT")
                    KVRow(k: "当前保证金", v: "\(Fmt.amount(Dec.parse(position.margin), 2)) USDT")
                    KVRow(k: "当前强平价", v: position.liquidationPrice ?? "--")
                }
                LBButton(title: isolated ? "确认" : "知道了", enabled: !isolated || Dec.parse(amount) > 0, loading: saving) {
                    guard isolated else { onClose(); return }
                    let v = Dec.parse(amount)
                    saving = true
                    Task { let ok = await m.adjustMargin(position, amount: add == 0 ? v : -v); saving = false; if ok { onClose() } }
                }
            }.padding(.horizontal, Spacing.pageH)
        }
    }
}
