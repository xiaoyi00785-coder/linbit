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
                PillSegment(items: ["合仓", "分仓"], selected: $posMode, height: 40)
                Text(posMode == 0 ? "合仓模式：同一合约（开多与开空）仅持有一个仓位，反向开仓会先减少已有仓位。"
                                  : "分仓模式：同一合约可同时持有多个独立仓位，每笔开仓单独计算保证金与盈亏，可分别止盈止损、平仓。")
                    .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                if posMode == 1 {
                    InlineBanner(text: "分仓模式需后端提供仓位模式切换接口（当前仅支持合仓 one_way），暂不可切换")
                }
                if !m.positions.filter({ $0.symbol == m.symbol }).isEmpty || !m.openOrders.filter({ $0.symbol == m.symbol }).isEmpty {
                    InlineBanner(text: "当前合约存在仓位或委托，切换保证金模式可能失败")
                }
                LBButton(title: posMode == 1 ? "分仓模式暂未开放" : "确认", enabled: posMode == 0, loading: saving) {
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
                    Button { onClose(); Router.shared.openFeature(.futuresRiskTiers) } label: {
                        Text("· 查看仓位档位介绍 ›").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                    }
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
        guard let cap = m.maxNotional(forLeverage: lev) else { return "--" }
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

/// 对照图 #13：常用功能
struct FuturesToolsSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    private let items: [(String, String)] = [("gearshape.fill", "交易偏好"), ("plusminus", "计算器"), ("bell.badge.fill", "价格预警"), ("rectangle.stack.person.crop.fill", "模拟交易"),
                                             ("chart.bar.fill", "仓位档位介绍"), ("percent", "历史资金费率"), ("chart.xyaxis.line", "历史标记价格"), ("dollarsign.circle.fill", "风险准备金"),
                                             ("star.fill", "添加自选"), ("safari.fill", "新手教学")]
    var body: some View {
        LBSheet(title: "常用功能", onClose: onClose) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 20) {
                ForEach(items, id: \.1) { icon, title in
                    Button {
                        onClose()
                        switch title {
                        case "模拟交易": Router.shared.futuresTop = 3
                        case "仓位档位介绍": Router.shared.openFeature(.futuresRiskTiers)
                        case "历史资金费率": Router.shared.openFeature(.futuresFunding)
                        case "添加自选": ToastCenter.shared.show("已添加到自选", .success)
                        default: ToastCenter.shared.show("\(title)即将开放")
                        }
                    } label: {
                        VStack(spacing: 8) {
                            Image(systemName: icon).font(.system(size: 17)).foregroundColor(p.textPrimary)
                                .frame(width: 44, height: 44).background(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border))
                            Text(title).font(.lb(FontSize.small)).foregroundColor(p.textPrimary).lineLimit(1).minimumScaleFactor(0.8)
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

/// 对照图 #21：止盈/止损（开多/开空、市场/标记价格、触发价 + 盈利率、刻度滑杆、清空、部分止盈止损）
/// position == nil：下单时设置；否则为持仓止盈止损的设置 / 修改 / 撤销。
struct TpslSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let position: APIFuturesPosition?
    let onClose: () -> Void
    @State private var side = 0
    @State private var tp = ""
    @State private var sl = ""
    @State private var tpRoi = ""
    @State private var slRoi = ""
    @State private var partial = false
    @State private var partialQty = ""
    @State private var saving = false
    @State private var confirm: ConfirmRequest?

    private var entry: Decimal {
        if let pos = position { return Dec.parse(pos.entryPrice) }
        return m.orderPrice > 0 ? m.orderPrice : m.last
    }
    private var long: Bool { position.map { $0.side != .short } ?? (side == 0) }
    private var lev: Decimal { Decimal(position?.leverage ?? m.leverage) }
    private var hasExisting: Bool { !(position?.tpPrice ?? "").isEmpty || !(position?.slPrice ?? "").isEmpty }

    /// 触发价 → 收益率（%），按杠杆放大，Decimal 计算
    private func roi(_ trigger: String) -> Decimal? {
        let t = Dec.parse(trigger); guard t > 0, entry > 0 else { return nil }
        let r = (t - entry) / entry * lev * 100
        return long ? r : -r
    }
    /// 收益率（%）→ 触发价
    private func price(roi pct: Decimal) -> String {
        guard entry > 0, lev > 0 else { return "" }
        let move = pct / 100 / lev
        return Dec.plain(long ? entry * (1 + move) : entry * (1 - move), m.pricePrec)
    }

    var body: some View {
        LBSheet(title: "止盈/止损", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 0) {
                if position == nil {
                    PillSegment(items: ["开多", "开空"], selected: Binding(get: { side }, set: { side = $0; recomputeRoi() }),
                                tints: [p.up, p.down], height: 36)
                        .padding(.horizontal, Spacing.pageH).padding(.bottom, 14)
                }
                header.padding(.horizontal, Spacing.pageH)
                Rectangle().fill(p.fill).frame(height: 6).padding(.top, 12)
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        block(title: "止盈", text: $tp, roiText: $tpRoi, positive: true, ticks: [0, 40, 80, 120, 160, 200])
                        block(title: "止损", text: $sl, roiText: $slRoi, positive: false, ticks: [0, 20, 40, 60, 80, 100])
                        partialRow
                        Text(position == nil ? "当前止盈/止损设置仅应用于当前委托" : "触发后将以市价平掉该仓位全部数量，行情剧烈波动时成交价可能偏离触发价")
                            .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    }.padding(.horizontal, Spacing.pageH).padding(.top, 14)
                }.frame(maxHeight: 400)
                buttons.padding(.horizontal, Spacing.pageH).padding(.top, 12)
            }
        }
        .lbConfirm($confirm)
        .onAppear {
            tp = position?.tpPrice ?? m.tpText; sl = position?.slPrice ?? m.slText
            recomputeRoi()
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(position?.symbol ?? m.symbol).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text(long ? "多" : "空").font(.lb(10, .semibold)).foregroundColor(long ? p.up : p.down)
                    .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill((long ? p.up : p.down).opacity(0.12)))
                Text("合仓").font(.lb(10)).foregroundColor(p.textSecondary)
                Spacer()
            }
            KVRow(k: "市场价格", v: "\(Fmt.price(m.last, precision: m.pricePrec)) USDT")
            KVRow(k: "标记价格", v: "\(Fmt.price(m.mark, precision: m.pricePrec)) USDT")
            if let pos = position {
                KVRow(k: "开仓价格", v: "\(Fmt.priceAuto(Dec.parse(pos.entryPrice))) USDT")
                KVRow(k: "持仓数量", v: "\(Fmt.amount(abs(Dec.parse(pos.qty)), m.qtyPrec)) \(m.base)")
            } else {
                KVRow(k: "委托数量", v: "\(Fmt.amount(m.qty, m.qtyPrec)) \(m.base)")
            }
        }
    }

    private func block(title: String, text: Binding<String>, roiText: Binding<String>, positive: Bool, ticks: [Int]) -> some View {
        let maxT = Decimal(ticks.last ?? 100)
        let cur = abs(Dec.parse(roiText.wrappedValue))
        return VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                Text(title).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                Spacer()
                Button { text.wrappedValue = ""; roiText.wrappedValue = "" } label: {
                    HStack(spacing: 3) { Image(systemName: "trash").font(.system(size: 11)); Text("清空") }
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                Button { ToastCenter.shared.show("当前仅支持按最新价触发") } label: {
                    HStack(spacing: 3) { Text("最新"); Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)) }
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }
            HStack(spacing: 8) {
                NumberBox(label: "\(title)触发价(USDT)", unit: "", text: Binding(get: { text.wrappedValue }, set: { v in
                    text.wrappedValue = v
                    roiText.wrappedValue = roi(v).map { Dec.plain(positive ? $0 : -$0, 2) } ?? ""
                }), onEdit: {})
                NumberBox(label: positive ? "盈利率(%)" : "亏损率(%)", unit: "", text: Binding(get: { roiText.wrappedValue }, set: { v in
                    roiText.wrappedValue = v
                    let pct = Dec.parse(v)
                    text.wrappedValue = v.isEmpty ? "" : price(roi: positive ? pct : -pct)
                }), onEdit: {}).frame(width: 110)
            }
            TickSlider(value: Binding(get: { maxT > 0 ? Dec.double(min(cur, maxT) / maxT) : 0 }, set: { v in
                let pct = Decimal(Int((v * Dec.double(maxT)).rounded()))
                roiText.wrappedValue = pct == 0 ? "" : Dec.plain(pct, 0)
                text.wrappedValue = pct == 0 ? "" : price(roi: positive ? pct : -pct)
            }), labels: ticks.map { "\($0)%" }, tint: positive ? p.up : p.down)
        }
    }

    private var partialRow: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text("类型").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Button { partial.toggle() } label: {
                    HStack(spacing: 4) {
                        Text(partial ? "部分止盈止损" : "全部止盈止损").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                        Image(systemName: "arrow.left.arrow.right").font(.system(size: 9)).foregroundColor(p.textSecondary)
                    }
                }
                Spacer()
            }
            if partial {
                NumberBox(label: "平仓数量", unit: m.base, text: $partialQty, onEdit: {})
                InlineBanner(text: "部分止盈止损需后端支持按数量设置（当前接口仅支持整仓），暂不可提交")
            }
        }
    }

    @ViewBuilder private var buttons: some View {
        if let pos = position, hasExisting {
            HStack(spacing: 10) {
                LBButton(title: "撤销止盈止损", kind: .secondary) {
                    confirm = .init(title: "撤销止盈止损", message: "确定撤销 \(pos.symbol ?? "") 的止盈止损吗？", confirmTitle: "撤销", destructive: true) {
                        saving = true
                        Task { let ok = await m.clearTpsl(pos); saving = false; if ok { onClose() } }
                    }
                }
                LBButton(title: "修改", enabled: !partial, loading: saving) { submit() }
            }
        } else {
            LBButton(title: "确定", enabled: !partial, loading: saving) { submit() }
        }
    }

    private func recomputeRoi() {
        tpRoi = roi(tp).map { Dec.plain($0, 2) } ?? ""
        slRoi = roi(sl).map { Dec.plain(-$0, 2) } ?? ""
    }

    private func submit() {
        let t = Dec.parse(tp), s = Dec.parse(sl)
        if !tp.isEmpty, (long ? t <= entry : t >= entry) { ToastCenter.shared.show("止盈价需\(long ? "高于" : "低于")\(position == nil ? "委托价" : "开仓价")", .error); return }
        if !sl.isEmpty, (long ? s >= entry : s <= entry) { ToastCenter.shared.show("止损价需\(long ? "低于" : "高于")\(position == nil ? "委托价" : "开仓价")", .error); return }
        if let pos = position {
            // 修改：空字符串表示清除该项（spec："" 清除）
            saving = true
            Task { let ok = await m.setTpsl(pos, tp: tp, sl: sl, toast: hasExisting ? "止盈止损已修改" : "止盈止损已设置"); saving = false; if ok { onClose() } }
        } else {
            m.tpsl = !(tp.isEmpty && sl.isEmpty); m.tpText = tp; m.slText = sl; onClose()
        }
    }
}

/// 对照图 #21 刻度滑杆：菱形节点 + 下方百分比刻度
struct TickSlider: View {
    @Environment(\.palette) private var p
    @Binding var value: Double   // 0...1
    let labels: [String]
    var tint: Color
    var body: some View {
        let n = max(labels.count - 1, 1)
        VStack(spacing: 4) {
            GeometryReader { g in
                let w = g.size.width
                ZStack(alignment: .leading) {
                    Rectangle().fill(p.fillStrong).frame(height: 2)
                    Rectangle().fill(tint).frame(width: w * value, height: 2)
                    ForEach(0...n, id: \.self) { i in
                        let on = Double(i) / Double(n) <= value
                        Circle().fill(on ? tint : p.fillStrong).frame(width: 5, height: 5)
                            .position(x: min(max(w * Double(i) / Double(n), 3), w - 3), y: 10)
                    }
                    Rectangle().fill(p.textPrimary).frame(width: 10, height: 10).rotationEffect(.degrees(45))
                        .position(x: min(max(w * value, 6), w - 6), y: 10)
                }
                .frame(height: 20).contentShape(Rectangle())
                .gesture(DragGesture(minimumDistance: 0).onChanged { v in
                    var nv = min(max(v.location.x / w, 0), 1)
                    for i in 0...n where abs(nv - Double(i) / Double(n)) < 0.025 { nv = Double(i) / Double(n) }
                    if nv != value { value = nv }
                })
            }.frame(height: 20)
            HStack {
                ForEach(labels.indices, id: \.self) { i in
                    Text(labels[i]).font(.lbNum(10)).foregroundColor(p.textTertiary)
                    if i < labels.count - 1 { Spacer() }
                }
            }
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
