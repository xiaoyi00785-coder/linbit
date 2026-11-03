import SwiftUI

/// 合约 Tab 根：合约 / 事件合约 / TradFi / 模拟交易（对照图 #11）
struct FuturesRootView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var router = Router.shared
    @ObservedObject private var flags = AppFeatures.shared
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                PrimaryTabs(items: ["合约", "事件合约", "TradFi", "模拟交易"], selected: $router.futuresTop)
                Spacer()
            }.padding(.horizontal, Spacing.pageH).frame(height: 44)
            switch router.futuresTop {
            case 0: FuturesTradeView()
            case 1:
                if flags.isOn("eventContracts") { EventContractView() } else { ComingSoonView(title: "事件合约", note: "猜涨跌，到期按指数价结算，敬请期待") }
            case 2: ComingSoonView(title: "TradFi", note: "股票、贵金属等传统金融资产交易即将上线")
            default: ComingSoonView(title: "模拟交易", note: "使用模拟资金体验合约交易，即将上线")
            }
        }
        .background(p.bg.ignoresSafeArea())
    }
}

/// U 本位永续合约交易页（对照图 #11/#12/#13/#17–#22/#25）
struct FuturesTradeView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @ObservedObject private var router = Router.shared
    @StateObject private var m = FuturesModel(symbol: Router.shared.futuresSymbol)
    @State private var bottomTab = 0
    @State private var showMode = false
    @State private var showLev = false
    @State private var showKind = false
    @State private var showTools = false
    @State private var showPairs = false
    @State private var showChart = false
    @State private var tpslTarget: APIFuturesPosition?
    @State private var showTpslForm = false
    @State private var marginTarget: APIFuturesPosition?
    @State private var confirm: ConfirmRequest?
    @State private var couponTab = 0
    @State private var orderFilter = 0     // 委托：0 普通委托 1 止盈止损 2 计划委托

    var body: some View {
        ScrollViewReader { proxy in
        ScrollView {
            VStack(spacing: 0) {
                pairHeader
                chipsRow.padding(.top, 10)
                if let e = m.marketError, m.ticker == nil {
                    InlineBanner(text: "行情加载失败：\(e)").padding(.horizontal, Spacing.pageH).padding(.top, 8)
                }
                HStack(alignment: .top, spacing: 12) {
                    form.frame(maxWidth: .infinity)
                    FuturesBookView(m: m).frame(width: 138)
                }
                .padding(.horizontal, Spacing.pageH).padding(.top, 10)
                chartToggle.padding(.top, 14).id("lb.chart")
                bottomSection.padding(.top, 8).id("lb.bottom")
            }
            .padding(.bottom, Metrics.tabBarH + 60)
        }
        .task {
            // 截图：把 K 线 / 委托区滚动到画面内
            let target: String? = router.shotSheet == "chart" ? "lb.chart" : (router.shotSheet == "ordersTpsl" ? "lb.bottom" : nil)
            guard let target else { return }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            proxy.scrollTo(target, anchor: .top)
        }
        }
        .refreshable { await m.refreshMarket(); await m.refreshAccount() }
        .background(p.bg)
        .task { await m.start(); applyShot() }
        .onDisappear { m.stop() }
        .onChange(of: session.isLoggedIn) { _ in Task { await m.refreshAccount() } }
        .onChange(of: router.futuresSymbol) { s in Task { await m.switchSymbol(s) } }
        .lbSheet($showMode) { MarginModeSheet(m: m) { showMode = false } }
        .lbSheet($showLev) { LeverageSheet(m: m) { showLev = false } }
        .lbSheet($showKind) { FuturesKindSheet(selected: m.kind) { k in
            m.kind = k
            showKind = false
        } onClose: { showKind = false } }
        .lbSheet($showTools) { FuturesToolsSheet { showTools = false } }
        .lbSheet($showPairs) { FuturesPairSheet(current: m.symbol) { s in showPairs = false; router.futuresSymbol = s } onClose: { showPairs = false } }
        .lbSheet($showTpslForm) { TpslSheet(m: m, position: tpslTarget) { showTpslForm = false } }
        .lbSheet(Binding(get: { marginTarget != nil }, set: { if !$0 { marginTarget = nil } })) {
            if let pos = marginTarget { AdjustMarginSheet(m: m, position: pos) { marginTarget = nil } }
        }
        .lbConfirm($confirm)
    }

    private func applyShot() {
        switch router.shotSheet {
        case "leverage": showLev = true
        case "mode": showMode = true
        case "tpsl": showTpslForm = true
        case "kind": showKind = true
        case "tools": showTools = true
        case "chart": showChart = true
        case "plan": m.kind = .plan
        case "ordersTpsl": bottomTab = 1; orderFilter = 1
        default: break
        }
        if router.shotSheet != nil { router.shotSheet = nil }
    }

    // BTCUSDT 永续 +0.66%    图表 / 常用功能
    private var pairHeader: some View {
        HStack(alignment: .center) {
            Button { showPairs = true } label: {
                HStack(spacing: 6) {
                    Text(m.symbol).font(.lb(18, .semibold)).foregroundColor(p.textPrimary)
                    Text("永续").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        .padding(.horizontal, 4).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                    Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                    Text(Fmt.percent(m.changePct)).font(.lbNum(FontSize.small)).foregroundColor(m.changePct < 0 ? p.down : p.up)
                }
            }.buttonStyle(PressableStyle())
            Spacer()
            HStack(spacing: 18) {
                ThemeToggleButton()
                Button { withAnimation { showChart.toggle() } } label: { Image(systemName: "chart.bar.xaxis").foregroundColor(p.textPrimary) }
                Button { showTools = true } label: { Image(systemName: "ellipsis").foregroundColor(p.textPrimary) }
            }.font(.system(size: 17))
        }.padding(.horizontal, Spacing.pageH).padding(.top, 4)
    }

    // 全仓 · 20x       资金费率 / 倒计时
    private var chipsRow: some View {
        HStack(spacing: 8) {
            ChipButton(title: m.marginMode == .isolated ? "逐仓" : "全仓") { showMode = true }
            ChipButton(title: "\(m.leverage)x") { showLev = true }
            Spacer()
            VStack(alignment: .trailing, spacing: 2) {
                Text("资金费率 / 倒计时").font(.lb(10)).foregroundColor(p.textTertiary)
                Text("\(Fmt.amount(m.fundingRate, 4))% / \(m.fundingCountdown)").font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
            }
        }.padding(.horizontal, Spacing.pageH)
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 10) {
            PillSegment(items: ["开仓", "平仓"], selected: Binding(get: { m.isOpen ? 0 : 1 }, set: { m.isOpen = $0 == 0; m.setPercent(0) }))
            Button { showKind = true } label: {
                HStack(spacing: 6) {
                    Image(systemName: "info.circle.fill").font(.system(size: 12)).foregroundColor(p.textSecondary)
                    Text(m.kind.title).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    Spacer()
                    Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                }.padding(.horizontal, 10).frame(height: 34).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
            }.buttonStyle(PressableStyle(scale: 1))
            if m.kind == .plan {
                NumberBox(label: "触发价", unit: m.quote, text: $m.triggerText, onEdit: {})
            }
            if m.kind == .market {
                Text("市价").font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
                    .frame(maxWidth: .infinity).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
            } else {
                NumberBox(label: "价格", unit: m.quote, text: $m.priceText, onEdit: {})
            }
            if m.isOpen {
                NumberBox(label: "成本", unit: m.quote, text: $m.costText, onEdit: { m.percent = 0 })
            } else {
                HStack {
                    Text("数量").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Text("\(Fmt.amount(m.qty, m.qtyPrec)) \(m.base)").font(.lbNum(FontSize.label)).foregroundColor(p.textPrimary)
                }.padding(.horizontal, 10).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
            }
            PercentSlider(value: Binding(get: { m.percent }, set: { m.setPercent($0) }), tint: p.textPrimary).padding(.horizontal, 2)
            if m.isOpen {
                HStack {
                    Checkbox(on: $m.tpsl, label: "止盈/止损")
                    Spacer()
                    if m.tpsl { Button("高级设置") { tpslTarget = nil; showTpslForm = true }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                }
                if m.tpsl {
                    NumberBox(label: "止盈触发价", unit: m.quote, text: $m.tpText, onEdit: {})
                    NumberBox(label: "止损触发价", unit: m.quote, text: $m.slText, onEdit: {})
                }
            }
            VStack(spacing: 6) {
                HStack(spacing: 4) {
                    Text("可用").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Text("\(Fmt.amount(m.available, 2)) \(m.quote)").font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                    Button { Router.shared.open(.transfer) } label: { Image(systemName: "arrow.left.arrow.right.square.fill").font(.system(size: 12)).foregroundColor(p.textPrimary) }
                }
                KVRow(k: m.isOpen ? "可开" : "可平", v: "\(Fmt.amount(m.isOpen ? m.maxOpenQty : abs(Dec.parse(m.current?.qty)), m.qtyPrec)) \(m.base)")
            }
            if m.kind.backendMissing {
                InlineBanner(text: "\(m.kind.title)需后端提供下单接口，暂不可提交")
            }
            sideButton(long: true)
            sideButton(long: false)
        }
    }

    private func sideButton(long: Bool) -> some View {
        VStack(spacing: 6) {
            if m.isOpen {
                KVRow(k: "预估强平价", v: m.orderPrice > 0 ? Fmt.price(m.liqPrice(long: long), precision: m.pricePrec) : "--")
                KVRow(k: "成本", v: "\(Fmt.amount(m.cost, 2)) \(m.quote)")
            }
            LBButton(title: session.isLoggedIn ? (m.isOpen ? (long ? "开多" : "开空") : (long ? "平空" : "平多")) : "登录 / 注册",
                     kind: session.isLoggedIn ? (long ? .buy : .sell) : .primary, height: 40, loading: m.placing) {
                session.requireLogin {
                    if let e = m.validation { ToastCenter.shared.show(e, .error); return }
                    let dir = m.isOpen ? (long ? "开多" : "开空") : (long ? "平空" : "平多")
                    confirm = .init(title: "\(dir) \(m.symbol)",
                                    message: "\(m.kind.title) · \(m.marginMode == .isolated ? "逐仓" : "全仓") \(m.leverage)x\n价格 \(m.kind == .market ? "市价" : m.priceText)  数量 \(Fmt.amount(m.qty, m.qtyPrec)) \(m.base)\n预估手续费 \(Fmt.amount(m.qty * m.orderPrice * m.takerFee, 4)) USDT",
                                    confirmTitle: "确认\(dir)") { Task { _ = await m.place(long: long) } }
                }
            }
        }
    }

    private var chartToggle: some View {
        VStack(spacing: 0) {
            Button { withAnimation { showChart.toggle() } } label: {
                HStack {
                    Text("\(m.symbol) 图表").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    Spacer()
                    Image(systemName: showChart ? "chevron.up" : "chevron.down").font(.system(size: 11)).foregroundColor(p.textSecondary)
                }.padding(.horizontal, Spacing.pageH).frame(height: 36)
            }
            if showChart {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 16) {
                        KVCell(k: "标记价格", v: Fmt.priceAuto(m.mark))
                        KVCell(k: "指数价格", v: Fmt.priceAuto(Dec.parse(m.ticker?.indexPrice)))
                        KVCell(k: "24h 成交额", v: Fmt.volumeCN(Dec.parse(m.ticker?.turnover24h)), align: .trailing)
                    }
                    FuturesChartPanel(symbol: m.symbol, pricePrec: m.pricePrec)
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
            }
            Rectangle().fill(p.fill).frame(height: 8)
        }
    }

    // 仓位 / 委托 / 跟单 / 卡券
    private var bottomSection: some View {
        VStack(spacing: 0) {
            HStack(spacing: 18) {
                tab("仓位(\(m.positions.count))", 0)
                tab("委托(\(m.openOrders.count))", 1)
                tab("跟单", 2)
                tab("卡券", 3)
                Spacer()
                Button { Router.shared.openFeature(.futuresHistory) } label: {
                    Image(systemName: "doc.text").font(.system(size: 15)).foregroundColor(p.textSecondary)
                }
            }.padding(.horizontal, Spacing.pageH).frame(height: 40)
            Divider().overlay(p.divider)
            if bottomTab < 2 {
                HStack {
                    Checkbox(on: $m.onlyCurrent, label: "只看当前")
                    Spacer()
                    if bottomTab == 0 {
                        smallAction("一键平仓", enabled: !m.shownPositions.isEmpty) {
                            confirm = .init(title: "一键平仓", message: "将以市价平掉\(m.onlyCurrent ? "当前合约" : "全部")的 \(m.shownPositions.count) 个仓位，确定吗？",
                                            confirmTitle: "全部平仓", destructive: true) { Task { await m.closeAll() } }
                        }
                    } else {
                        smallAction("全部撤销", enabled: !m.shownOrders.isEmpty) {
                            confirm = .init(title: "全部撤销", message: "确定撤销 \(m.shownOrders.count) 笔委托吗？", confirmTitle: "全部撤销", destructive: true) {
                                Task { for o in m.shownOrders { await m.cancel(o) } }
                            }
                        }
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.top, 12)
            }
            switch bottomTab {
            case 0: positionsList
            case 1: ordersList
            case 2: EmptyBlock(text: "暂无跟单", actions: [("跟单交易", { Router.shared.openFeature(.copyTrading) })])
            default: couponsList
            }
        }
    }
    private func tab(_ t: String, _ i: Int) -> some View {
        Text(t).font(.lb(FontSize.label, bottomTab == i ? .semibold : .regular))
            .foregroundColor(bottomTab == i ? p.textPrimary : p.textSecondary).onTapGesture { bottomTab = i }
    }
    private func smallAction(_ t: String, enabled: Bool, _ a: @escaping () -> Void) -> some View {
        Button(action: { if enabled { a() } }) {
            Text(t).font(.lb(FontSize.small)).foregroundColor(enabled ? p.textPrimary : p.textTertiary)
                .padding(.horizontal, 10).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
        }.buttonStyle(PressableStyle())
    }

    @ViewBuilder private var positionsList: some View {
        if !session.isLoggedIn {
            EmptyBlock(text: "登录后查看仓位", actions: [("登录 / 注册", { session.present(.login) })])
        } else if !m.accountLoaded {
            SkeletonList(rows: 2)
        } else if m.shownPositions.isEmpty {
            // 对照图 #22：充值 / 模拟盘 / 跟单交易
            EmptyBlock(text: "暂无仓位", actions: [("充值", { Router.shared.open(.deposit("USDT")) }),
                                                 ("模拟盘", { Router.shared.futuresTop = 3 }),
                                                 ("跟单交易", { Router.shared.openFeature(.copyTrading) })])
        } else {
            LazyVStack(spacing: 0) {
                ForEach(m.shownPositions, id: \.self) { pos in
                    PositionCard(pos: pos, onTpsl: { tpslTarget = pos; showTpslForm = true },
                                 onMargin: { marginTarget = pos },
                                 onClose: {
                        confirm = .init(title: "市价平仓", message: "确定以市价平掉 \(pos.symbol ?? "") \(pos.side == .short ? "空" : "多")仓 \(pos.qty ?? "") 吗？",
                                        confirmTitle: "平仓", destructive: true) { Task { await m.close(pos) } }
                    })
                    Rectangle().fill(p.fill).frame(height: 8)
                }
            }
        }
    }

    @ViewBuilder private var ordersList: some View {
        if !session.isLoggedIn {
            EmptyBlock(text: "登录后查看委托", actions: [("登录 / 注册", { session.present(.login) })])
        } else {
            VStack(spacing: 0) {
                HStack(spacing: 8) {
                    ForEach(["普通委托", "止盈止损", "计划委托"].indices, id: \.self) { i in
                        Text(["普通委托", "止盈止损", "计划委托"][i]).font(.lb(FontSize.small, orderFilter == i ? .medium : .regular))
                            .foregroundColor(orderFilter == i ? p.textPrimary : p.textSecondary)
                            .padding(.horizontal, 10).frame(height: 24)
                            .background(RoundedRectangle(cornerRadius: Radius.xs).fill(orderFilter == i ? p.fill : Color.clear))
                            .onTapGesture { orderFilter = i }
                    }
                    Spacer()
                }.padding(.horizontal, Spacing.pageH).padding(.top, 10)
                switch orderFilter {
                case 0: normalOrders
                case 1: tpslOrders
                default:
                    VStack(spacing: 0) {
                        InlineBanner(text: "计划委托需后端提供创建 / 查询 / 撤销接口，暂无数据").padding(.horizontal, Spacing.pageH).padding(.top, 10)
                        EmptyBlock(text: "暂无计划委托", actions: [])
                    }
                }
            }
        }
    }

    @ViewBuilder private var normalOrders: some View {
        let list = m.shownOrders.filter { $0.source != "tpsl" }
        if !m.accountLoaded {
            SkeletonList(rows: 2)
        } else if list.isEmpty {
            EmptyBlock(text: "暂无委托", actions: [])
        } else {
            LazyVStack(spacing: 0) {
                ForEach(list, id: \.self) { o in
                    FuturesOrderRow(o: o) {
                        confirm = .init(title: "撤销委托", message: "确定撤销该笔委托吗？", confirmTitle: "撤销", destructive: true) { Task { await m.cancel(o) } }
                    }
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
            }
        }
    }

    /// 持仓止盈止损：查看 / 修改 / 撤销（整仓）
    @ViewBuilder private var tpslOrders: some View {
        let list = m.shownPositions.filter { !($0.tpPrice ?? "").isEmpty || !($0.slPrice ?? "").isEmpty }
        if !m.accountLoaded {
            SkeletonList(rows: 2)
        } else if list.isEmpty {
            EmptyBlock(text: "暂无止盈止损", actions: [])
        } else {
            LazyVStack(spacing: 0) {
                ForEach(list, id: \.self) { pos in
                    TpslOrderRow(pos: pos, onEdit: { tpslTarget = pos; showTpslForm = true }, onCancel: {
                        confirm = .init(title: "撤销止盈止损", message: "确定撤销 \(pos.symbol ?? "") 的止盈止损吗？", confirmTitle: "撤销", destructive: true) {
                            Task { _ = await m.clearTpsl(pos) }
                        }
                    })
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
            }
        }
    }

    // 对照图 #25：体验金 / 抵扣金
    private var couponsList: some View {
        let items = (m.bonus?.items ?? []).filter { couponTab == 0 ? ($0.kind ?? "trial") != "deduction" : $0.kind == "deduction" }
        return VStack(spacing: 0) {
            HStack(spacing: 8) {
                ForEach(["体验金", "抵扣金"].indices, id: \.self) { i in
                    Text(["体验金", "抵扣金"][i]).font(.lb(FontSize.small, couponTab == i ? .medium : .regular))
                        .foregroundColor(couponTab == i ? p.textPrimary : p.textSecondary)
                        .padding(.horizontal, 10).frame(height: 24)
                        .background(RoundedRectangle(cornerRadius: Radius.xs).fill(couponTab == i ? p.fill : Color.clear))
                        .onTapGesture { couponTab = i }
                }
                Spacer()
            }.padding(.horizontal, Spacing.pageH).padding(.top, 12)
            if items.isEmpty {
                EmptyBlock(text: "暂无卡券", actions: [("前往卡券中心", { Router.shared.openFeature(.welfare) })])
            } else {
                ForEach(items, id: \.self) { b in
                    HStack {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("\(b.remaining ?? b.amount ?? "0") USDT").font(.lbNum(FontSize.body, .semibold)).foregroundColor(p.brand)
                            Text(b.source ?? "合约体验金").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        Spacer()
                        Text(b.status == "active" ? "可使用" : (b.status ?? "")).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                    .padding(12).background(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
                    .padding(.horizontal, Spacing.pageH).padding(.top, 10)
                }
            }
        }
    }
}

/// 空态块：插画 + 文案 + 若干小按钮
struct EmptyBlock: View {
    @Environment(\.palette) private var p
    let text: String
    let actions: [(String, () -> Void)]
    var body: some View {
        VStack(spacing: 12) {
            EmptyIllustration()
            Text(text).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
            if !actions.isEmpty {
                HStack(spacing: 10) {
                    ForEach(actions.indices, id: \.self) { i in
                        Button(action: actions[i].1) {
                            Text(actions[i].0).font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                                .padding(.horizontal, 14).frame(height: 28).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                        }.buttonStyle(PressableStyle())
                    }
                }
            }
        }.frame(maxWidth: .infinity).padding(.vertical, 28)
    }
}

/// 仓位卡片：多/空 · 全仓 20x · 未实现盈亏 / 收益率 / 持仓量 / 保证金 / 开仓价 / 标记价 / 强平价 + 操作
struct PositionCard: View {
    @Environment(\.palette) private var p
    let pos: APIFuturesPosition
    let onTpsl: () -> Void
    let onMargin: () -> Void
    let onClose: () -> Void
    var body: some View {
        let long = pos.side != .short
        let pnl = Dec.parse(pos.unrealizedPnl), roe = Dec.parse(pos.roe) * 100
        let pc = pnl < 0 ? p.down : p.up
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                Text(long ? "多" : "空").font(.lb(FontSize.small, .semibold)).foregroundColor(.white)
                    .frame(width: 20, height: 18).background(RoundedRectangle(cornerRadius: 3).fill(long ? p.up : p.down))
                Text(pos.symbol ?? "").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text("\(pos.marginMode == .isolated ? "逐仓" : "全仓") \(pos.leverage ?? 0)x").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    .padding(.horizontal, 4).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                Spacer()
            }
            HStack {
                KVCell(k: "未实现盈亏(USDT)", v: Fmt.amount(pnl, 2), vColor: pc)
                KVCell(k: "收益率", v: Fmt.percent(roe), align: .trailing, vColor: pc)
            }
            HStack {
                KVCell(k: "持仓量", v: pos.qty ?? "--")
                KVCell(k: "保证金(USDT)", v: Fmt.amount(Dec.parse(pos.margin), 2), align: .center)
                KVCell(k: "开仓价格", v: Fmt.priceAuto(Dec.parse(pos.entryPrice)), align: .trailing)
            }
            HStack {
                KVCell(k: "标记价格", v: Fmt.priceAuto(Dec.parse(pos.markPrice)))
                KVCell(k: "强平价格", v: pos.liquidationPrice.map { Fmt.priceAuto(Dec.parse($0)) } ?? "--", align: .center)
                KVCell(k: "止盈/止损", v: "\(pos.tpPrice ?? "--")/\(pos.slPrice ?? "--")", align: .trailing)
            }
            HStack(spacing: 8) {
                btn(pos.marginMode == .isolated ? "调整保证金" : "保证金", onMargin)
                btn("止盈止损", onTpsl)
                btn("市价平仓", onClose)
            }
        }
        .padding(.horizontal, Spacing.pageH).padding(.vertical, 14)
    }
    private func btn(_ t: String, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            Text(t).font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                .frame(maxWidth: .infinity).frame(height: 30).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
        }.buttonStyle(PressableStyle())
    }
}

struct FuturesOrderRow: View {
    @Environment(\.palette) private var p
    let o: APIFuturesOrder
    let onCancel: () -> Void
    var body: some View {
        let buy = o.side == "buy"
        let open = o.reduceOnly != true
        let dir = open ? (buy ? "开多" : "开空") : (buy ? "平空" : "平多")
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(dir).font(.lb(FontSize.small, .semibold)).foregroundColor(buy ? p.up : p.down)
                Text(o.symbol ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(o.type == "market" ? "市价" : "限价").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                Button(action: onCancel) {
                    Text("撤单").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                        .padding(.horizontal, 12).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                }
            }
            HStack {
                KVCell(k: "委托价格", v: o.price ?? "市价")
                KVCell(k: "数量(\(String((o.symbol ?? "").dropLast(4))))", v: o.qty ?? "--", align: .center)
                KVCell(k: "已成交", v: o.filledQty ?? "0", align: .trailing)
            }
            if let d = o.createdAt { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
}

/// 合约盘口（价格 / 数量，中间标记价格）
struct FuturesBookView: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: FuturesModel
    var body: some View {
        let asks = Array(m.book.asks.prefix(7)).reversed()
        let bids = Array(m.book.bids.prefix(7))
        let maxQ = max((m.book.asks.prefix(7) + m.book.bids.prefix(7)).map(\.qty).max() ?? 1, 0.0000001)
        VStack(spacing: 2) {
            HStack {
                Text("价格\n(USDT)").multilineTextAlignment(.leading)
                Spacer()
                Text("数量\n(\(m.base))").multilineTextAlignment(.trailing)
            }.font(.lb(10)).foregroundColor(p.textTertiary).padding(.bottom, 4)
            if m.book.asks.isEmpty && m.book.bids.isEmpty {
                ForEach(0..<14, id: \.self) { _ in RoundedRectangle(cornerRadius: 2).fill(p.fill).frame(height: 18) }
            } else {
                ForEach(Array(asks), id: \.price) { l in level(l, up: false, maxQ: maxQ) }
                VStack(spacing: 1) {
                    Text(Fmt.price(m.last, precision: m.pricePrec)).font(.lbNum(16, .semibold)).foregroundColor(m.changePct < 0 ? p.down : p.up)
                    Text(Fmt.price(m.mark, precision: m.pricePrec)).font(.lbNum(10)).foregroundColor(p.textSecondary).underline()
                }.padding(.vertical, 6)
                ForEach(bids, id: \.price) { l in level(l, up: true, maxQ: maxQ) }
            }
        }
    }
    private func level(_ l: BookLevel, up: Bool, maxQ: Decimal) -> some View {
        let ratio = CGFloat(Dec.double(l.qty / maxQ))
        return HStack {
            Text(Fmt.price(l.price, precision: m.pricePrec)).foregroundColor(up ? p.up : p.down)
            Spacer()
            Text(Fmt.compact(l.qty)).foregroundColor(p.textPrimary)
        }
        .font(.lbNum(11)).frame(height: 18)
        .background(alignment: .trailing) {
            GeometryReader { g in Rectangle().fill(up ? p.upTint : p.downTint).frame(width: g.size.width * min(ratio, 1)).frame(maxWidth: .infinity, alignment: .trailing) }
        }
        .contentShape(Rectangle())
        .onTapGesture { m.priceText = Dec.plain(l.price, m.pricePrec); if m.kind == .market { m.kind = .limit } }
    }
}

/// 止盈止损行：触发价 + 预计收益，修改 / 撤销
struct TpslOrderRow: View {
    @Environment(\.palette) private var p
    let pos: APIFuturesPosition
    let onEdit: () -> Void
    let onCancel: () -> Void
    var body: some View {
        let long = pos.side != .short
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(long ? "平多" : "平空").font(.lb(FontSize.small, .semibold)).foregroundColor(long ? p.down : p.up)
                Text(pos.symbol ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text("整仓止盈止损").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                small("修改", onEdit)
                small("撤销", onCancel)
            }
            HStack {
                KVCell(k: "止盈触发价", v: pos.tpPrice.flatMap { $0.isEmpty ? nil : Fmt.priceAuto(Dec.parse($0)) } ?? "--", vColor: p.up)
                KVCell(k: "止损触发价", v: pos.slPrice.flatMap { $0.isEmpty ? nil : Fmt.priceAuto(Dec.parse($0)) } ?? "--", align: .center, vColor: p.down)
                KVCell(k: "数量", v: pos.qty ?? "--", align: .trailing)
            }
            Text("触发类型：最新价 · 触发后市价全部平仓").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private func small(_ t: String, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            Text(t).font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                .padding(.horizontal, 12).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
        }
    }
}
