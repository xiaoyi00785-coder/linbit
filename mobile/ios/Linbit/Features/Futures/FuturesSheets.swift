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
                Text("仓位模式").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary).padding(.top, 4)
                PillSegment(items: ["合仓", "分仓"], selected: $posMode, height: 40)
                // 对照 EasiCoin：按两项组合给一段说明
                Text(comboText).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true).padding(.top, 4)
                let modeChanged = (posMode == 1) != m.hedge
                if modeChanged, let reason = m.positionModeBlockReason {
                    InlineBanner(text: reason)
                }
                if !m.positions.filter({ $0.symbol == m.symbol }).isEmpty || !m.openOrders.filter({ $0.symbol == m.symbol }).isEmpty {
                    InlineBanner(text: "当前合约存在仓位或委托，切换保证金模式可能失败")
                }
                LBButton(title: "确认", enabled: !(modeChanged && m.positionModeBlockReason != nil), loading: saving) {
                    saving = true
                    Task {
                        var ok = true
                        if modeChanged { ok = await m.setPositionMode(posMode == 1 ? .hedge : .oneWay) }
                        let marginChanged = (mode == 1) != (m.marginMode == .isolated)
                        if ok, marginChanged { ok = await m.saveLeverage(m.leverage, mode: mode == 1 ? .isolated : .cross) }
                        saving = false
                        if ok { onClose() }
                    }
                }.padding(.top, 6)
            }.padding(.horizontal, Spacing.pageH)
        }
        .onAppear { mode = m.marginMode == .isolated ? 1 : 0; posMode = m.hedge ? 1 : 0 }
    }
    private var comboText: String {
        switch (mode, posMode) {
        case (0, 0): return "全仓合仓：同一合约同一方向的开仓合并为一个仓位，最多同时持有多仓和空仓各一个。杠杆统一调整，所有全仓仓位共享账户保证金，盈亏可以互相抵消。"
        case (0, _): return "全仓分仓：同一合约同一方向可以开多个仓位，每个仓位可单独设置杠杆、止盈止损和平仓。新开仓不会并入已有仓位，所有全仓仓位共享账户保证金。"
        case (_, 0): return "逐仓合仓：同一合约同一方向的开仓合并为一个仓位。每个仓位单独分配保证金，最大亏损不超过该仓位的保证金。"
        default: return "逐仓分仓：同一合约同一方向可以开多个仓位，每个仓位可单独设置杠杆。新开仓不会并入已有仓位，各仓位保证金互相独立。"
        }
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
        return mx <= 5 ? Array(1...mx) : [1] + (1...4).map { mx * $0 / 5 } + [mx]
    }
    var body: some View {
        let lev = Int(value.rounded())
        LBSheet(title: "调整杠杆", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 16) {
                HStack(spacing: 6) {
                    Text(m.symbol).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                    Text(m.hedge ? "多/空" : "多").font(.lb(FontSize.small)).foregroundColor(p.up)
                        .padding(.horizontal, 4).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(p.upTint))
                    Text("\(lev)x").font(.lbNum(FontSize.small)).foregroundColor(p.up)
                        .padding(.horizontal, 4).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(p.upTint))
                    Text(m.marginMode == .isolated ? "逐仓" : "全仓").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                HStack {
                    stepBtn("minus") { value = max(1, value - 1) }
                    Spacer()
                    Text("\(lev)x").font(.lbNum(22, .semibold)).foregroundColor(p.textPrimary)
                    Spacer()
                    stepBtn("plus") { value = min(maxL, value + 1) }
                }
                .padding(.horizontal, 6).frame(height: 48)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                // 菱形滑块 + 刻度（对照 EasiCoin 1x / 40x … 200x）
                TickSlider(value: Binding(get: { maxL > 1 ? (value - 1) / (maxL - 1) : 0 },
                                          set: { value = max(1, (1 + $0 * (maxL - 1)).rounded()) }),
                           labels: ticks.map { "\($0)x" }, tint: p.textPrimary)
                VStack(alignment: .leading, spacing: 8) {
                    infoLine("当前杠杆倍数最大可开", "\(Fmt.amount(m.available * Decimal(lev), 2)) USDT")
                    infoLine("当前仓位占用保证金", "\(Fmt.amount(currentMargin, 2)) USDT")
                    infoLine("该杠杆最高可持有头寸", "\(maxPositionText(lev)) USDT")
                    Button { onClose(); Router.shared.openFeature(.futuresRiskTiers) } label: {
                        Text("查看仓位档位介绍 ›").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
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
    private var currentMargin: Decimal {
        m.positions.filter { $0.symbol == m.symbol }.reduce(0) { $0 + Dec.parse($1.margin) }
    }
    private func infoLine(_ k: String, _ v: String) -> some View {
        HStack(spacing: 4) {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Text(v).font(.lbNum(FontSize.small, .medium)).foregroundColor(p.textPrimary)
        }
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
        LBSheet(title: "委托类型", onClose: onClose) {
            VStack(spacing: 0) {
                ForEach(FuturesKind.allCases, id: \.self) { k in
                    SheetOptionRow(icon: k.icon, title: k.title, selected: k == selected) { onPick(k) }
                }
            }.padding(.bottom, 12)
        }
    }
}

/// 对照图 #13：常用功能
struct FuturesToolsSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    @State private var alert = false
    private let items: [(String, String)] = [("gearshape.fill", "交易偏好"), ("plusminus", "计算器"), ("bell.badge.fill", "价格预警"), ("rectangle.stack.person.crop.fill", "模拟交易"),
                                             ("chart.bar.fill", "仓位档位介绍"), ("percent", "历史资金费率"), ("chart.xyaxis.line", "历史标记价格"), ("dollarsign.circle.fill", "风险准备金"),
                                             ("star.fill", "添加自选"), ("safari.fill", "新手教学")]
    var body: some View {
        LBSheet(title: "常用功能", onClose: onClose) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 20) {
                ForEach(items.map { $0.1 == "添加自选" && Watchlist.shared.contains(Router.shared.futuresSymbol) ? ("star.slash.fill", "取消自选") : $0 }, id: \.1) { icon, title in
                    Button {
                        if title == "价格预警" { alert = true; return }
                        onClose()
                        switch title {
                        case "模拟交易": Router.shared.futuresTop = 3
                        case "仓位档位介绍": Router.shared.openFeature(.futuresRiskTiers)
                        case "历史资金费率": Router.shared.openFeature(.futuresFunding)
                        case "价格预警": alert = true
                        case "添加自选", "取消自选":
                            let s = Router.shared.futuresSymbol
                            Watchlist.shared.toggle(s)
                            ToastCenter.shared.show(Watchlist.shared.contains(s) ? "已添加到自选" : "已取消自选", .success)
                        case "交易偏好": Router.shared.openFeature(.security)
                        default: Router.shared.openFeature(.futuresHistory)
                        }
                    } label: {
                        VStack(spacing: 8) {
                            Image(systemName: icon).font(.system(size: 18)).foregroundColor(p.textPrimary)
                                .frame(width: 48, height: 48)
                                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.bgElevated))
                                .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
                                .overlay(alignment: .topTrailing) {
                                    if title == "新手教学" {
                                        Text("New").font(.lb(9, .semibold)).foregroundColor(.white).padding(.horizontal, 4).frame(height: 13)
                                            .background(Capsule().fill(p.brand)).offset(x: 8, y: -6)
                                    }
                                }
                            Text(title).font(.lb(FontSize.small)).foregroundColor(p.textPrimary).lineLimit(1).minimumScaleFactor(0.8)
                        }
                    }.buttonStyle(PressableStyle())
                }
            }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
        }
        .lbSheet($alert) { PriceAlertSheet(symbol: Router.shared.futuresSymbol) { alert = false } }
    }
}

/// 合约选币。和现货不是同一套标签：自选，以及 USDT 合约下的分类。
struct FuturesPairSheet: View {
    let current: String
    let onPick: (String) -> Void
    let onClose: () -> Void
    var body: some View {
        TradePairSheet(futures: true, current: current, onPick: onPick, onClose: onClose)
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
    /// 触发价类型，默认标记价格
    @State private var tpType: APIFuturesTpslOrderRequest.APITpTriggerPriceType = .mark
    @State private var slType: APIFuturesTpslOrderRequest.APISlTriggerPriceType = .mark
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
            if let pos = position {
                if pos.tpTriggerPriceType == .last { tpType = .last }
                if pos.slTriggerPriceType == .last { slType = .last }
            } else {
                tpType = m.orderTpType == .last ? .last : .mark; slType = m.orderSlType == .last ? .last : .mark
            }
            if Router.shared.shotSheet == "tpslPartial" { partial = true }
            recomputeRoi()
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(position?.symbol ?? m.symbol).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text(long ? "多" : "空").font(.lb(10, .semibold)).foregroundColor(long ? p.up : p.down)
                    .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill((long ? p.up : p.down).opacity(0.12)))
                Text(m.hedge ? "分仓" : "合仓").font(.lb(10)).foregroundColor(p.textSecondary)
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
                do {
                Menu {
                    Button("标记价格") { setType(positive, mark: true) }
                    Button("最新价格") { setType(positive, mark: false) }
                } label: {
                    HStack(spacing: 3) { Text(isMark(positive) ? "标记" : "最新"); Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)) }
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
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

    private func isMark(_ tpSide: Bool) -> Bool { tpSide ? tpType == .mark : slType == .mark }
    private func setType(_ tpSide: Bool, mark: Bool) {
        if tpSide { tpType = mark ? .mark : .last } else { slType = mark ? .mark : .last }
    }
    private var maxQty: Decimal { abs(Dec.parse(position?.qty)) }

    private var partialRow: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text("类型").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Button {
                    if position == nil { ToastCenter.shared.show("部分止盈止损仅适用于已有仓位"); return }
                    partial.toggle()
                } label: {
                    HStack(spacing: 4) {
                        Text(partial ? "部分止盈止损" : "全部止盈止损").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                        Image(systemName: "arrow.left.arrow.right").font(.system(size: 9)).foregroundColor(p.textSecondary)
                    }
                }
                Spacer()
            }
            if partial {
                NumberBox(label: "平仓数量", unit: m.base, text: $partialQty, onEdit: {})
                HStack(spacing: 6) {
                    ForEach([25, 50, 75, 100], id: \.self) { pct in
                        Button { partialQty = Dec.plain(maxQty * Decimal(pct) / 100, m.qtyPrec) } label: {
                            Text("\(pct)%").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                                .frame(maxWidth: .infinity).frame(height: 26).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                        }
                    }
                }
                Text("可平 \(Fmt.amount(maxQty, m.qtyPrec)) \(m.base)，触发后以市价平掉所填数量").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
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
                LBButton(title: partial ? "提交" : "修改", loading: saving) { submit() }
            }
        } else {
            LBButton(title: "确定", loading: saving) { submit() }
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
        if let pos = position, partial {
            // 部分数量或指定触发价类型 → /futures/tpsl-orders（qty 为空即全部仓位）
            var q: String? = nil
            if partial {
                let v = Dec.parse(partialQty)
                if v <= 0 { ToastCenter.shared.show("请输入平仓数量", .error); return }
                if v > maxQty { ToastCenter.shared.show("平仓数量不能超过持仓数量", .error); return }
                q = Dec.plain(v, m.qtyPrec)
            }
            if tp.isEmpty && sl.isEmpty { ToastCenter.shared.show("请至少填写止盈或止损触发价", .error); return }
            saving = true
            Task {
                let ok = await m.createTpslOrder(pos, qty: q, tp: tp.isEmpty ? nil : tp, tpType: tpType, sl: sl.isEmpty ? nil : sl, slType: slType)
                saving = false; if ok { onClose() }
            }
        } else if let pos = position {
            // 整仓：/positions/{symbol}/tpsl，带触发价类型；修改：空字符串表示清除该项（spec："" 清除）
            saving = true
            Task { let ok = await m.setTpsl(pos, tp: tp, sl: sl, tpType: tpType == .mark ? .mark : .last, slType: slType == .mark ? .mark : .last, toast: hasExisting ? "止盈止损已修改" : "止盈止损已设置"); saving = false; if ok { onClose() } }
        } else {
            m.tpsl = !(tp.isEmpty && sl.isEmpty); m.tpText = tp; m.slText = sl
            m.orderTpType = tpType == .mark ? .mark : .last; m.orderSlType = slType == .mark ? .mark : .last; onClose()
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
                    KVRow(k: "当前强平价", v: position.liqText)
                    if let tip = position.liqTip { InlineBanner(text: tip) }
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

/// 改单：修改未成交限价单的价格 / 数量（PUT /futures/orders/{id}），展示已改单次数
struct AmendOrderSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let order: APIFuturesOrder
    let onClose: () -> Void
    @State private var price = ""
    @State private var qty = ""
    @State private var saving = false
    @State private var error: String?

    private var filled: Decimal { Dec.parse(order.filledQty) }
    private var buy: Bool { order.side == "buy" }
    var body: some View {
        LBSheet(title: "改单", subtitle: "\(order.symbol ?? m.symbol) · \(order.type == "market" ? "市价" : "限价")", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    KVCell(k: "原委托价", v: Fmt.priceAuto(Dec.parse(order.price)))
                    KVCell(k: "原数量", v: order.qty ?? "--", align: .center)
                    KVCell(k: "已成交", v: order.filledQty ?? "0", align: .trailing)
                }
                NumberBox(label: "新价格", unit: m.quote, text: $price, onEdit: { error = nil })
                NumberBox(label: "新数量（总量）", unit: m.base, text: $qty, onEdit: { error = nil })
                HStack {
                    Text("已改单次数").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Text("\(order.amendCount ?? 0) 次").font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                }
                if order.timeInForce == "POST_ONLY" || order.timeInForce == "GTX" {
                    Text("只做 Maker 委托：若新价格会立即成交，改单将被拒绝").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }
                if let error { InlineBanner(text: error) }
                LBButton(title: "确认修改", kind: buy ? .buy : .sell, loading: saving) { submit() }.padding(.top, 4)
            }.padding(.horizontal, Spacing.pageH)
        }
        .onAppear { price = order.price ?? ""; qty = order.qty ?? "" }
    }
    private func submit() {
        let np = Dec.parse(price), nq = Dec.parse(qty)
        let priceChanged = !price.isEmpty && np != Dec.parse(order.price)
        let qtyChanged = !qty.isEmpty && nq != Dec.parse(order.qty)
        if !priceChanged && !qtyChanged { error = "价格与数量均未变化"; return }
        if priceChanged && np <= 0 { error = "请输入有效价格"; return }
        if qtyChanged && nq <= filled { error = "新数量需大于已成交数量 \(order.filledQty ?? "0")"; return }
        saving = true
        Task {
            let ok = await m.amend(order, price: priceChanged ? Dec.plain(np, m.pricePrec) : nil, qty: qtyChanged ? Dec.plain(nq, m.qtyPrec) : nil)
            saving = false
            if ok { onClose() }
        }
    }
}

/// 修改条件委托（计划委托 / 止盈止损委托）：与改单弹窗同风格。仅提交有变化的字段，至少一项。
struct AmendAlgoSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let algo: APIFuturesAlgoOrder
    let onClose: () -> Void
    @State private var trigger = ""
    @State private var mark = true
    @State private var market = false
    @State private var price = ""
    @State private var qty = ""
    @State private var saving = false
    @State private var error: String?

    private var isPlan: Bool { algo.algoType == .plan }
    private var buy: Bool { algo.side == "buy" }
    private var title: String { isPlan ? "修改计划委托" : (algo.algoType == .tp ? "修改止盈委托" : "修改止损委托") }
    /// 止盈止损数量上限：对应仓位数量
    private var maxQty: Decimal? {
        guard !isPlan else { return nil }
        let ps = algo.positionSide ?? "BOTH"
        let pos = m.positions.first { ($0.symbol ?? "") == (algo.symbol ?? m.symbol) && (ps == "BOTH" || FuturesModel.side(of: $0) == ps) }
        return pos.map { abs(Dec.parse($0.qty)) }
    }

    var body: some View {
        LBSheet(title: title, subtitle: "\(algo.symbol ?? m.symbol) · \(isPlan ? "计划委托" : "止盈止损")", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    KVCell(k: "原触发价(\(algo.triggerPriceType == .last ? "最新" : "标记"))", v: Fmt.priceAuto(Dec.parse(algo.triggerPrice)))
                    KVCell(k: "原委托价", v: algo.orderType == .market || (algo.price ?? "").isEmpty ? "市价" : Fmt.priceAuto(Dec.parse(algo.price)), align: .center)
                    KVCell(k: "原数量", v: (algo.qty ?? "").isEmpty ? "全部仓位" : (algo.qty ?? ""), align: .trailing)
                }
                HStack(spacing: 8) {
                    Text("触发价类型").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    chip("标记价格", mark) { mark = true; error = nil }
                    chip("最新价格", !mark) { mark = false; error = nil }
                }
                NumberBox(label: "新触发价", unit: m.quote, text: $trigger, onEdit: { error = nil })
                if isPlan {
                    HStack(spacing: 8) {
                        Text("委托类型").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        Spacer()
                        chip("限价", !market) { market = false; error = nil }
                        chip("市价", market) { market = true; error = nil }
                    }
                    if !market { NumberBox(label: "新委托价", unit: m.quote, text: $price, onEdit: { error = nil }) }
                }
                NumberBox(label: isPlan ? "新数量" : "新数量（留空则保持原数量）", unit: m.base, text: $qty, onEdit: { error = nil })
                if let mq = maxQty { Text("最多 \(Dec.plain(mq, m.qtyPrec)) \(m.base)（当前持仓）").font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                Text("未修改的项将保持原值").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                if let error { InlineBanner(text: error) }
                LBButton(title: "确认修改", kind: buy ? .buy : .sell, loading: saving) { submit() }.padding(.top, 4)
            }.padding(.horizontal, Spacing.pageH)
        }
        .onAppear {
            trigger = algo.triggerPrice ?? ""; mark = algo.triggerPriceType != .last
            market = algo.orderType == .market || (algo.price ?? "").isEmpty
            price = algo.price ?? ""; qty = algo.qty ?? ""
        }
    }

    private func chip(_ t: String, _ on: Bool, _ tap: @escaping () -> Void) -> some View {
        Button(action: tap) {
            Text(t).font(.lb(FontSize.small, on ? .medium : .regular)).foregroundColor(on ? p.textPrimary : p.textSecondary)
                .padding(.horizontal, 10).frame(height: 26)
                .background(RoundedRectangle(cornerRadius: Radius.xs).fill(on ? p.fillStrong : p.fill))
        }
    }

    private func submit() {
        var r = APIFuturesAlgoAmendRequest()
        let nt = Dec.parse(trigger)
        if !trigger.isEmpty && nt != Dec.parse(algo.triggerPrice) {
            if nt <= 0 { error = "请输入有效触发价"; return }
            r.triggerPrice = Dec.plain(nt, m.pricePrec)
        }
        let oldMark = algo.triggerPriceType != .last
        if mark != oldMark { r.triggerPriceType = mark ? .mark : .last }
        if isPlan {
            let oldMarket = algo.orderType == .market || (algo.price ?? "").isEmpty
            if market {
                if !oldMarket { r.orderType = .market }
            } else {
                let np = Dec.parse(price)
                if np <= 0 { error = "请输入有效委托价"; return }
                if oldMarket || np != Dec.parse(algo.price) { r.orderType = .limit; r.price = Dec.plain(np, m.pricePrec) }
            }
        }
        if !qty.isEmpty && Dec.parse(qty) != Dec.parse(algo.qty) {
            let nq = Dec.parse(qty)
            if nq <= 0 { error = "请输入有效数量"; return }
            if let mq = maxQty, nq > mq { error = "数量不能超过持仓数量"; return }
            r.qty = Dec.plain(nq, m.qtyPrec)
        }
        if r.triggerPrice == nil && r.triggerPriceType == nil && r.orderType == nil && r.price == nil && r.qty == nil {
            error = "请至少修改一项"; return
        }
        saving = true
        Task {
            let e = await m.amendAlgo(algo, r)
            saving = false
            if let e { error = e } else { onClose() }
        }
    }
}

/// 合约单位：数量 / 成本 / 价值
struct FuturesUnitSheet: View {
    @Environment(\.palette) private var p
    let base: String
    let quote: String
    let selected: FuturesOrderUnit
    let onPick: (FuturesOrderUnit) -> Void
    var onClose: () -> Void = {}

    // 必须包在 LBSheet 里：它提供弹窗底色。之前直接返回 VStack，弹窗是透明的，下面的下单表单会透出来。
    var body: some View {
        LBSheet(title: "合约单位设置", onClose: onClose) {
            VStack(alignment: .leading, spacing: 14) {
                ForEach(FuturesOrderUnit.allCases) { unit in
                    Button { Haptics.tap(); onPick(unit) } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(unit.title(base: base, quote: quote)).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(unit.detail(base: base)).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(14)
                        .background(RoundedRectangle(cornerRadius: Radius.md).stroke(unit == selected ? p.textPrimary : p.border, lineWidth: unit == selected ? 1.5 : 1))
                        .contentShape(RoundedRectangle(cornerRadius: Radius.md))
                    }.buttonStyle(.plain)
                }
            }
            .padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
        }
    }
}

// MARK: - 对照 EasiCoin 合约页补充的小组件与说明弹窗

/// 行情连接状态点：WebSocket 连着为绿，否则灰
struct LiveDot: View {
    @Environment(\.palette) private var p
    @ObservedObject private var socket = MarketSocket.shared
    var body: some View {
        Circle().fill(socket.state == .open ? p.up : p.textTertiary).frame(width: 5, height: 5)
    }
}

/// 带虚线下划线的标签：可点开说明（资金费率、预估强平价、成本等）
struct DottedText: View {
    let text: String
    var size: CGFloat = FontSize.small
    var color: Color
    var body: some View {
        Text(text).font(.lb(size)).foregroundColor(color)
            .padding(.bottom, 2)
            .overlay(alignment: .bottom) {
                Line().stroke(color.opacity(0.6), style: StrokeStyle(lineWidth: 0.8, dash: [2, 2])).frame(height: 1)
            }
    }
    private struct Line: Shape {
        func path(in r: CGRect) -> Path { var p = Path(); p.move(to: CGPoint(x: 0, y: r.midY)); p.addLine(to: CGPoint(x: r.maxX, y: r.midY)); return p }
    }
}

struct InfoSheetItem: Identifiable {
    let id = UUID()
    var title: String
    var text: String
}

/// 字段说明：标题 + 正文 +「我知道了」
struct InfoSheet: View {
    @Environment(\.palette) private var p
    let item: InfoSheetItem
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: item.title, onClose: onClose) {
            VStack(alignment: .leading, spacing: 20) {
                Text(item.text).font(.lb(FontSize.body)).foregroundColor(p.textPrimary).fixedSize(horizontal: false, vertical: true)
                LBButton(title: "我知道了", action: onClose)
            }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
        }
    }
}

/// 资金费率说明：周期 / 方向 / 当前费率 + 计算说明
struct FundingInfoSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let onClose: () -> Void
    var body: some View {
        let rate = m.fundingRate
        LBSheet(title: "资金费率", onClose: onClose) {
            VStack(alignment: .leading, spacing: 14) {
                VStack(spacing: 12) {
                    row("资金费率周期") { Text("8 小时").foregroundColor(p.textPrimary) }
                    row("资金费率方向") {
                        if rate >= 0 { Text("多头").foregroundColor(p.up) + Text("支付").foregroundColor(p.textPrimary) + Text("空头").foregroundColor(p.down) }
                        else { Text("空头").foregroundColor(p.down) + Text("支付").foregroundColor(p.textPrimary) + Text("多头").foregroundColor(p.up) }
                    }
                    row("当前资金费率") { Text("\(Fmt.amount(rate, 4))%").foregroundColor(p.textPrimary) }
                    row("下次结算倒计时") {
                        TimelineView(.periodic(from: .now, by: 1)) { ctx in Text(m.fundingText(at: ctx.date)).foregroundColor(p.textPrimary) }
                    }
                }
                .font(.lbNum(FontSize.label))
                .padding(14)
                .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
                Text("资金费用每 8 小时在多空双方之间直接交换一次，平台不收取。费率为正时多仓支付给空仓，为负时空仓支付给多仓。\n资金费用 = 仓位价值 × 资金费率。只有结算时刻持有仓位才会收付。")
                    .font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true)
                Button { onClose(); Router.shared.openFeature(.futuresFunding) } label: {
                    Text("查看历史资金费率 ›").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                }.buttonStyle(.plain)
            }.padding(.horizontal, Spacing.pageH)
        }
    }
    private func row<V: View>(_ k: String, @ViewBuilder _ v: () -> V) -> some View {
        HStack { Text(k).foregroundColor(p.textSecondary); Spacer(); v() }
    }
}

/// 委托类型介绍（点「ⓘ」）：四种类型分页，每页一段说明 + 示意图 + 例子
struct OrderKindIntroSheet: View {
    @Environment(\.palette) private var p
    let initial: FuturesKind
    let onClose: () -> Void
    @State private var kind: FuturesKind = .limit
    @State private var buy = true
    var body: some View {
        LBSheet(onClose: onClose) {
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 18) {
                    ForEach(FuturesKind.allCases, id: \.self) { k in
                        VStack(spacing: 6) {
                            Text(k.title).font(.lb(FontSize.label, k == kind ? .semibold : .regular)).foregroundColor(k == kind ? p.textPrimary : p.textSecondary)
                            Capsule().fill(k == kind ? p.textPrimary : .clear).frame(width: 16, height: 3)
                        }.onTapGesture { kind = k }
                    }
                }
                Divider().overlay(p.divider)
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        Text(summary).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary).fixedSize(horizontal: false, vertical: true)
                        HStack {
                            Text("示意图").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                            Spacer()
                            Checkbox(on: Binding(get: { buy }, set: { if $0 { buy = true } }), label: "买入")
                            Checkbox(on: Binding(get: { !buy }, set: { if $0 { buy = false } }), label: "卖出")
                        }
                        KindDiagram(kind: kind, buy: buy).frame(height: 190)
                        Text(detail).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true)
                    }.padding(.bottom, 12)
                }.frame(maxHeight: 480)
            }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
        }
        .onAppear { kind = initial }
    }
    private var summary: String {
        switch kind {
        case .limit: return "限价单按你设定的价格或更优价格成交，不保证一定成交。"
        case .market: return "市价单按当前盘口最优价格立即成交，成交价可能和看到的价格有差距。"
        case .plan: return "计划委托先设一个触发价，价格到达后才按设定的价格（或市价）下单。"
        case .postOnly: return "只做 Maker 的委托只挂在盘口等待成交；如果下单时会立即成交，整单撤销。"
        }
    }
    private var detail: String {
        switch (kind, buy) {
        case (.limit, true): return "图中当前价为 A。买入限价设在 C（低于 A）时，委托挂在盘口，价格跌到 C 或更低才成交；如果买入限价设在 B（高于 A），会按当前盘口卖一附近的价格立即成交，而不是按 B 成交。"
        case (.limit, false): return "图中当前价为 A。卖出限价设在 C（高于 A）时，价格涨到 C 或更高才成交；如果卖出限价设在 B（低于 A），会按当前盘口买一附近的价格立即成交。"
        case (.market, _): return "市价单没有价格框，只填数量（或成本 / 价值）。盘口深度不足时会分多档成交，平均成交价可能偏离最新价，深度极差时剩余部分会被撤销。"
        case (.plan, true): return "设触发价 T。最新价（或标记价）涨到 / 跌到 T 时，系统按设定的委托价 C 下买单；选「市价」则触发后直接市价成交。触发前不占用保证金，触发后行情剧烈波动时可能下单失败。"
        case (.plan, false): return "设触发价 T。价格到达 T 时系统按设定的委托价 C 下卖单，常用于突破追单或止损。触发前不占用保证金。"
        case (.postOnly, true): return "只做 Maker 的买单价格必须低于卖一价（图中 A），否则会立即成交，系统会把整单撤销，保证你只付挂单手续费。"
        case (.postOnly, false): return "只做 Maker 的卖单价格必须高于买一价（图中 A），否则整单撤销。"
        }
    }
}

/// 委托类型示意图：折线表示价格走势，标出当前价 A、委托价 B / C、触发价 T
private struct KindDiagram: View {
    @Environment(\.palette) private var p
    let kind: FuturesKind
    let buy: Bool
    var body: some View {
        Canvas { ctx, size in
            let w = size.width, h = size.height - 20
            func pt(_ x: Double, _ y: Double) -> CGPoint { CGPoint(x: w * x, y: h * (1 - y)) }
            let path: [(Double, Double)] = buy
                ? [(0.02, 0.1), (0.25, 0.55), (0.34, 0.42), (0.45, 0.62), (0.58, 0.9), (0.8, 0.25), (0.92, 0.32)]
                : [(0.02, 0.9), (0.25, 0.45), (0.34, 0.58), (0.45, 0.38), (0.58, 0.1), (0.8, 0.75), (0.92, 0.68)]
            var solid = Path(); solid.move(to: pt(path[0].0, path[0].1))
            for q in path[1...3] { solid.addLine(to: pt(q.0, q.1)) }
            ctx.stroke(solid, with: .color(p.textPrimary), lineWidth: 1.5)
            var dash = Path(); dash.move(to: pt(path[3].0, path[3].1))
            for q in path[4...] { dash.addLine(to: pt(q.0, q.1)) }
            ctx.stroke(dash, with: .color(p.textSecondary), style: StrokeStyle(lineWidth: 1.2, dash: [3, 3]))
            let line = buy ? 0.25 : 0.75
            // 买入：限价线以下是会成交的区域；卖出：限价线以上
            let zone = buy ? CGRect(x: 0, y: h * (1 - line), width: w, height: h * line) : CGRect(x: 0, y: 0, width: w, height: h * (1 - line))
            ctx.fill(Path(zone), with: .color(p.brand.opacity(0.08)))
            var level = Path(); level.move(to: pt(0, line)); level.addLine(to: pt(1, line))
            ctx.stroke(level, with: .color(p.brand.opacity(0.7)), lineWidth: 1)
            func dot(_ c: CGPoint, _ color: Color, _ label: String) {
                ctx.fill(Path(ellipseIn: CGRect(x: c.x - 4, y: c.y - 4, width: 8, height: 8)), with: .color(color))
                ctx.draw(Text(label).font(.lb(11, .semibold)).foregroundColor(p.textPrimary), at: CGPoint(x: c.x + 12, y: c.y - 8))
            }
            dot(pt(path[3].0, path[3].1), p.textPrimary, "A")
            switch kind {
            case .limit, .postOnly:
                dot(pt(0.8, line), p.brand, buy ? "≤C" : "≥C")
                if kind == .limit { dot(pt(0.45, buy ? 0.9 : 0.1), p.brand, "B") }
            case .market:
                dot(pt(0.5, path[3].1), p.brand, "成交")
            case .plan:
                dot(pt(0.58, path[4].1), p.brand, "T")
                dot(pt(0.8, line), p.brand, "C")
            }
            let legend = Text("● A 当前价格    ● \(kind == .plan ? "T 触发价  C 委托价" : kind == .market ? "立即成交" : "B / C 委托价")").font(.lb(10)).foregroundColor(p.textSecondary)
            ctx.draw(legend, at: CGPoint(x: w / 2, y: size.height - 6))
        }
    }
}

/// 盘口显示模式：默认 / 只看买 / 只看卖
struct BookModeSheet: View {
    @Binding var mode: Int
    let onClose: () -> Void
    var body: some View {
        LBSheet(onClose: onClose) {
            VStack(spacing: 0) {
                ForEach(0..<3, id: \.self) { i in
                    Button { mode = i; Haptics.tap(); onClose() } label: {
                        HStack(spacing: 12) {
                            BookModeIcon(mode: i)
                            Text(["默认", "买", "卖"][i]).font(.lb(FontSize.body)).foregroundColor(.primary)
                            Spacer()
                            if mode == i { Image(systemName: "checkmark.circle.fill").font(.system(size: 18)) }
                        }.padding(.horizontal, Spacing.pageH).frame(height: 52).contentShape(Rectangle())
                    }.buttonStyle(RowPressStyle())
                }
            }.padding(.bottom, 12)
        }
    }
}

/// 合约盘口价格精度
struct FuturesPrecisionSheet: View {
    @ObservedObject var m: FuturesModel
    let onClose: () -> Void
    var body: some View {
        LBSheet(onClose: onClose) {
            VStack(spacing: 0) {
                ForEach(m.bookSteps, id: \.self) { d in
                    SheetOptionRow(title: NSDecimalNumber(decimal: d).stringValue, selected: d == m.bookStep) {
                        m.bookPrecision = d; onClose()
                    }
                }
            }.padding(.bottom, 12)
        }
    }
}

/// 数量低于最小下单量：提示可按最小数量下单。可用不足时只能取消。
struct MinQtySheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    let onCancel: () -> Void
    let onConfirm: () -> Void
    var body: some View {
        let px = m.orderPrice
        let minValue = m.minQty * px
        let enough = px > 0 && minValue / Decimal(max(m.leverage, 1)) <= m.available
        LBSheet(onClose: onCancel) {
            VStack(alignment: .leading, spacing: 20) {
                Text("订单数量未达到最低要求。请确认是否按最低数量 \(Dec.plain(m.minQty, m.qtyPrec)) \(m.base) ≈ \(Fmt.amount(minValue, 1)) \(m.quote) 下单。")
                    .font(.lb(FontSize.body)).foregroundColor(p.textPrimary).fixedSize(horizontal: false, vertical: true)
                if enough {
                    HStack(spacing: 10) {
                        LBButton(title: "取消", kind: .secondary, action: onCancel)
                        LBButton(title: "确认", action: onConfirm)
                    }
                } else {
                    LBButton(title: "取消", action: onCancel)
                }
            }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
        }
    }
}
