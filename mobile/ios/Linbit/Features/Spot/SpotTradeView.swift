import SwiftUI

/// 现货交易页（对照图 #28 / #29 / #19；下单确认与撤单确认为同风格自绘）
struct SpotTradeView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @ObservedObject private var router = Router.shared
    @StateObject private var m = SpotModel(symbol: Router.shared.spotSymbol)
    @State private var topTab = 0
    @State private var bottomTab = 0
    @State private var botTab = 0
    @State private var showKind = false
    @State private var showConfirm = false
    @State private var showPrecision = false
    @State private var showPairs = false
    @State private var showFee = false
    @State private var confirm: ConfirmRequest?
    @AppStorage("lb.spot.skipConfirm") private var skipConfirm = false

    var body: some View {
        VStack(spacing: 0) {
            topBar
            ScrollView {
                VStack(spacing: 0) {
                    pairHeader
                    HStack(alignment: .top, spacing: 12) {
                        form.frame(maxWidth: .infinity)
                        OrderBookView(m: m, showPrecision: $showPrecision).frame(width: 138)
                    }
                    .padding(.horizontal, Spacing.pageH).padding(.top, 8)
                    ordersSection.padding(.top, 20)
                }
                .padding(.bottom, 100)
            }
            .refreshable { await m.loadBook(); await m.refreshAccount() }
        }
        .background(p.bg.ignoresSafeArea())
        .task {
            switch router.shotSheet {
            case "spotPlan": m.kind = .plan; m.triggerText = "65000"; m.priceText = "64900"; m.qtyText = "0.01"; showConfirm = true
            case "spotMarketAmount": m.kind = .market; m.marketBuyByAmount = true; m.amountText = "100"
            case "spotFee": showFee = true
            case "spotConditional": bottomTab = 2
            default: break
            }
            if ["spotPlan", "spotMarketAmount", "spotFee", "spotConditional"].contains(router.shotSheet ?? "") { router.shotSheet = nil }
            await m.start()
        }
        .onDisappear { m.stop() }
        .onChange(of: session.isLoggedIn) { _ in Task { await m.refreshAccount() } }
        .onChange(of: router.spotSymbol) { s in
            m.stop(); m.symbol = s; m.priceText = ""; m.qtyText = ""; m.amountText = ""; m.percent = 0; m.last = 0
            Task { await m.start() }
        }
        .lbSheet($showKind) { OrderKindSheet(selected: m.kind) { k in
            if k.available { m.kind = k; m.qtyEdited() } else { ToastCenter.shared.show("该委托类型暂未开放") }
            showKind = false
        } onClose: { showKind = false } }
        .lbSheet($showConfirm) { OrderConfirmSheet(m: m, skip: $skipConfirm) {
            showConfirm = false; Task { _ = await m.place() }
        } onClose: { showConfirm = false } }
        .lbSheet($showFee) { SpotFeeSheet(m: m) { showFee = false } }
        .lbSheet($showPrecision) { PrecisionSheet(m: m) { showPrecision = false } }
        .lbSheet($showPairs) { PairPickerSheet(current: m.symbol) { s in showPairs = false; router.spotSymbol = s } onClose: { showPairs = false } }
        .lbConfirm($confirm)
    }

    // 一级 Tab：现货 / 策略 / 理财 / 闪兑
    private var topBar: some View {
        HStack {
            PrimaryTabs(items: ["现货", "策略", "理财", "闪兑"], selected: Binding(get: { topTab }, set: { i in
                if i == 0 { topTab = 0 } else { Haptics.tap(); Router.shared.openFeature([FeaturePage.strategy, .strategy, .earn, .swap][i]) }
            }))
            Spacer()
        }.padding(.horizontal, Spacing.pageH).frame(height: 44)
    }

    private var pairHeader: some View {
        HStack(alignment: .top) {
            Button { showPairs = true } label: {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text("\(m.base)/\(m.quote)").font(.lb(18, .semibold)).foregroundColor(p.textPrimary)
                        Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                        Circle().fill(p.up).frame(width: 5)
                    }
                    Text(Fmt.percent(m.changePct)).font(.lbNum(FontSize.small)).foregroundColor(m.changePct < 0 ? p.down : p.up)
                }
            }.buttonStyle(PressableStyle())
            Spacer()
            HStack(spacing: 18) {
                ThemeToggleButton()
                Button { ToastCenter.shared.show("K 线页即将开放") } label: { Image(systemName: "chart.bar.xaxis").foregroundColor(p.textPrimary) }
                Button { ToastCenter.shared.show("更多功能即将开放") } label: { Image(systemName: "ellipsis").foregroundColor(p.textPrimary) }
            }.font(.system(size: 17)).padding(.top, 4)
        }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
    }

    // 左侧下单表单
    private var form: some View {
        VStack(alignment: .leading, spacing: 10) {
            SideSegment(isBuy: Binding(get: { m.isBuy }, set: { m.isBuy = $0; m.setPercent(0) }))
            Button { showKind = true } label: {
                HStack(spacing: 6) {
                    Image(systemName: "info.circle.fill").font(.system(size: 12)).foregroundColor(p.textSecondary)
                    Text(m.kind.title).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    Spacer()
                    Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                }.padding(.horizontal, 10).frame(height: 34).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
            }.buttonStyle(PressableStyle(scale: 1))

            if m.isPlan {
                NumberBox(label: "触发价", unit: m.quote, text: $m.triggerText, onEdit: m.qtyEdited)
                PillSegment(items: ["限价", "市价"], selected: Binding(get: { m.planMarket ? 1 : 0 }, set: { m.planMarket = $0 == 1; m.qtyEdited() }), height: 30)
            }
            if m.marketLike {
                Text(m.isPlan ? "触发后以市价成交" : "以当前最优价格成交").font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
                    .frame(maxWidth: .infinity).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                if m.isBuy {
                    PillSegment(items: ["按金额", "按数量"], selected: Binding(get: { m.marketBuyByAmount ? 0 : 1 }, set: { m.marketBuyByAmount = $0 == 0 }), height: 30)
                        .accessibilityIdentifier("spot.marketBuyMode")
                }
            } else {
                NumberBox(label: m.isPlan ? "委托价格" : "价格", unit: m.quote, text: $m.priceText, onEdit: m.qtyEdited)
            }
            if m.byAmount {
                NumberBox(label: "交易额", unit: m.quote, text: $m.amountText, onEdit: {})
                PercentSlider(value: Binding(get: { m.percent }, set: { v in m.percent = v; m.amountText = SpotModel.fmt(m.availQuote * Decimal(v), 2) }), tint: p.up)
                    .padding(.horizontal, 2)
            } else {
                NumberBox(label: "数量", unit: m.base, text: $m.qtyText, onEdit: m.qtyEdited)
                PercentSlider(value: Binding(get: { m.percent }, set: { m.setPercent($0) }), tint: m.isBuy ? p.up : p.down)
                    .padding(.horizontal, 2)
                NumberBox(label: "交易额", unit: m.quote, text: $m.amountText, onEdit: m.amountEdited)
            }
            if !m.isPlan { Checkbox(on: $m.tpsl, label: "止盈/止损") }
            if m.tpsl && !m.isPlan {
                NumberBox(label: "止盈触发价", unit: m.quote, text: $m.tpText, onEdit: {})
                NumberBox(label: "止损触发价", unit: m.quote, text: $m.slText, onEdit: {})
            }
            VStack(spacing: 6) {
                info("可用", "\(Fmt.amount(m.isBuy ? m.availQuote : m.availBase, 2)) \(m.isBuy ? m.quote : m.base)", plus: true)
                info(m.isBuy ? "可买" : "可卖", "\(Fmt.amount(m.isBuy ? m.maxQty : m.availBase * m.price, m.isBuy ? m.qtyPrec : 2)) \(m.isBuy ? m.base : m.quote)")
                Button { showFee = true } label: {
                    HStack(spacing: 4) {
                        Text("手续费率").font(.lb(FontSize.small)).foregroundColor(p.textSecondary).underline(pattern: .dot)
                        Spacer()
                        Text("\(SpotCond.pct(m.feeRate)) ≈ \(Fmt.amount(m.fee, 4))").font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                    }
                }.buttonStyle(.plain).accessibilityIdentifier("spot.feeRate")
            }
            LBButton(title: session.isLoggedIn ? "\(m.isBuy ? "买入" : "卖出") \(m.base)" : "登录 / 注册",
                     kind: session.isLoggedIn ? (m.isBuy ? .buy : .sell) : .primary, height: 40, loading: m.placing) { submit() }
        }
    }

    private func info(_ k: String, _ v: String, plus: Bool = false) -> some View {
        HStack(spacing: 4) {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
            if plus {
                Button { Router.shared.open(.transfer) } label: {
                    Image(systemName: "plus.square.fill").font(.system(size: 12)).foregroundColor(p.textPrimary)
                }
            }
        }
    }

    private func submit() {
        session.requireLogin {
            if let e = m.validation { ToastCenter.shared.show(e, .error); return }
            if skipConfirm { Task { _ = await m.place() } } else { showConfirm = true }
        }
    }

    // 底部：委托(n) / 交易机器人(0)
    private var ordersSection: some View {
        VStack(spacing: 0) {
            HStack(spacing: 18) {
                tabText("委托(\(m.openOrders.count))", 0, arrow: true)
                tabText("条件单(\(m.conditional.count))", 2)
                tabText("交易机器人(0)", 1)
                Spacer()
                NavigationLink(value: SpotRoute.history) {
                    Image(systemName: "clock.fill").font(.system(size: 15)).foregroundColor(p.textSecondary)
                }
            }.padding(.horizontal, Spacing.pageH)
            if bottomTab == 0 {
                HStack {
                    Checkbox(on: $m.onlyCurrent, label: "只看当前")
                    Spacer()
                    Button {
                        guard !m.shownOrders.isEmpty else { return }
                        confirm = .init(title: "全部撤销", message: "确定撤销\(m.onlyCurrent ? "当前交易对" : "全部")的 \(m.shownOrders.count) 笔委托吗？",
                                        confirmTitle: "全部撤销", destructive: true) { Task { await m.cancelAll() } }
                    } label: {
                        Text("全部撤销").font(.lb(FontSize.small)).foregroundColor(m.shownOrders.isEmpty ? p.textTertiary : p.textPrimary)
                            .padding(.horizontal, 10).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                    }.buttonStyle(PressableStyle())
                }.padding(.horizontal, Spacing.pageH).padding(.top, 14)
                if m.shownOrders.isEmpty {
                    SpotEmpty()
                } else {
                    LazyVStack(spacing: 0) {
                        ForEach(m.shownOrders, id: \.orderId) { o in
                            OpenOrderRow(o: o) {
                                confirm = .init(title: "撤销委托", message: "确定撤销该笔 \(o.side == .buy ? "买入" : "卖出") \(o.symbol) 委托吗？",
                                                confirmTitle: "撤销", destructive: true) { Task { await m.cancel(o) } }
                            }
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                    }
                }
            } else if bottomTab == 2 {
                condSection
            } else {
                HStack(spacing: 18) {
                    ForEach(["现货网格", "现货定投"].indices, id: \.self) { i in
                        Text(["现货网格", "现货定投"][i]).font(.lb(FontSize.caption, botTab == i ? .medium : .regular))
                            .foregroundColor(botTab == i ? p.textPrimary : p.textSecondary).onTapGesture { botTab = i }
                    }
                    Spacer()
                    Button { ToastCenter.shared.show("交易机器人即将开放") } label: {
                        Text("创建交易机器人").font(.lb(FontSize.small, .medium)).foregroundColor(p.textOnPrimary)
                            .padding(.horizontal, 10).frame(height: 26).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.primary))
                    }.buttonStyle(PressableStyle())
                }.padding(.horizontal, Spacing.pageH).padding(.top, 14)
                EmptyStateView(text: "暂无交易机器人")
            }
        }
    }
    @ViewBuilder private var condSection: some View {
        switch m.condState {
        case .idle, .loading: SkeletonList(rows: 3).padding(.top, 14)
        case .failed(let e): ErrorStateView(message: e) { Task { await m.loadConditional() } }.padding(.top, 14)
        case .loaded:
            if m.shownConditional.isEmpty {
                EmptyStateView(text: session.isLoggedIn ? "暂无未触发的条件单" : "登录后查看条件单")
            } else {
                LazyVStack(spacing: 0) {
                    ForEach(m.shownConditional) { o in
                        ConditionalOrderRow(o: o) {
                            confirm = .init(title: "撤销条件单", message: "撤销后将释放冻结资金，确定撤销吗？", confirmTitle: "撤销", destructive: true) {
                                Task { await m.cancelConditional(o) }
                            }
                        }
                        Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                    }
                }.padding(.top, 8)
            }
        }
    }
    private func tabText(_ t: String, _ i: Int, arrow: Bool = false) -> some View {
        HStack(spacing: 3) {
            Text(t).font(.lb(FontSize.label, bottomTab == i ? .semibold : .regular))
            if arrow { Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)) }
        }
        .foregroundColor(bottomTab == i ? p.textPrimary : p.textSecondary)
        .onTapGesture { bottomTab = i }
    }
}

enum SpotRoute: Hashable { case history }

/// 输入盒：左上小标签 + 数值，右侧单位（对照图 #28 灰底圆角）
struct NumberBox: View {
    @Environment(\.palette) private var p
    let label: String
    let unit: String
    @Binding var text: String
    let onEdit: () -> Void
    @FocusState private var f: Bool
    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 1) {
                if !text.isEmpty || f { Text(label).font(.lb(11)).foregroundColor(p.textSecondary) }
                TextField("", text: Binding(get: { text }, set: { v in
                    let clean = v.filter { $0.isNumber || $0 == "." }
                    if clean.filter({ $0 == "." }).count <= 1 { text = clean; onEdit() }
                }))
                .placeholder(when: text.isEmpty && !f) { Text(label).foregroundColor(p.textSecondary) }
                .font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary).keyboardType(.decimalPad).focused($f)
            }
            Text(unit).font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
        }
        .padding(.horizontal, 10).frame(height: 44)
        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
        .overlay(RoundedRectangle(cornerRadius: Radius.sm).stroke(f ? p.textPrimary : .clear))
    }
}

/// 委托空状态：插画 + 「划转资金，开始现货交易」+ 充值 / 划转
struct SpotEmpty: View {
    @Environment(\.palette) private var p
    var body: some View {
        VStack(spacing: 12) {
            EmptyIllustration()
            Text("划转资金，开始现货交易").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary)
            HStack(spacing: 10) {
                small("充值") { Router.shared.open(.deposit(nil)) }
                small("划转") { Router.shared.open(.transfer) }
            }
        }.frame(maxWidth: .infinity).padding(.vertical, 24)
    }
    private func small(_ t: String, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            Text(t).font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                .frame(width: 78, height: 30).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
        }.buttonStyle(PressableStyle())
    }
}

/// 灰色文件盒插画（对照图空状态通用）
struct EmptyIllustration: View {
    @Environment(\.palette) private var p
    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 4).fill(p.fillStrong).frame(width: 40, height: 46).offset(y: -6)
            RoundedRectangle(cornerRadius: 2).fill(p.brand).frame(width: 18, height: 3).offset(x: -4, y: -18)
            RoundedRectangle(cornerRadius: 2).fill(p.brand.opacity(0.6)).frame(width: 14, height: 3).offset(x: -6, y: -12)
            Path { pa in pa.move(to: CGPoint(x: 0, y: 22)); pa.addLine(to: CGPoint(x: 56, y: 22)); pa.addLine(to: CGPoint(x: 50, y: 46)); pa.addLine(to: CGPoint(x: 6, y: 46)); pa.closeSubpath() }
                .fill(LinearGradient(colors: [p.textTertiary.opacity(0.5), p.fillStrong], startPoint: .top, endPoint: .bottom))
                .frame(width: 56, height: 46).offset(y: 6)
        }.frame(width: 64, height: 60)
    }
}

/// 当前委托行
struct OpenOrderRow: View {
    @Environment(\.palette) private var p
    let o: APIOrder
    let onCancel: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(o.side == .buy ? "买入" : "卖出").font(.lb(FontSize.small, .medium)).foregroundColor(o.side == .buy ? p.up : p.down)
                    .padding(.horizontal, 4).padding(.vertical, 1).background(RoundedRectangle(cornerRadius: 3).fill(o.side == .buy ? p.upTint : p.downTint))
                Text(o.symbol.replacingOccurrences(of: "USDT", with: "/USDT")).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                Text(o.type == .limit ? "限价" : "市价").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                Button(action: onCancel) {
                    Text("撤单").font(.lb(FontSize.small)).foregroundColor(p.textPrimary).padding(.horizontal, 12).frame(height: 24)
                        .background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                }.buttonStyle(PressableStyle())
            }
            Text(Fmt.dateTime(o.createdAt)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            HStack {
                kv("委托价", o.price ?? "市价")
                kv("已成交/数量", "\(o.filledQuantity)/\(o.qtyText)")
                kv("状态", o.status == .partiallyFilled ? "部分成交" : "未成交", alignEnd: true)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private func kv(_ k: String, _ v: String, alignEnd: Bool = false) -> some View {
        VStack(alignment: alignEnd ? .trailing : .leading, spacing: 3) {
            Text(k).font(.lb(11)).foregroundColor(p.textSecondary)
            Text(v).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
        }.frame(maxWidth: .infinity, alignment: alignEnd ? .trailing : .leading)
    }
}

/// 太阳/月亮：切换日夜（对照图 #28 右上）
struct ThemeToggleButton: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var theme: ThemeStore
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        Button { theme.mode = scheme == .dark ? .light : .dark; Haptics.tap() } label: {
            Image(systemName: scheme == .dark ? "moon.fill" : "sun.max.fill").foregroundColor(p.textPrimary)
        }
    }
}
