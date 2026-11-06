import SwiftUI

/// 合约 Tab 根：合约 / 事件合约 / TradFi / 模拟交易（对照图 #11）
struct FuturesRootView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var router = Router.shared
    @ObservedObject private var flags = AppFeatures.shared
    var body: some View {
        VStack(spacing: 0) {
            ScrollView(.horizontal, showsIndicators: false) {
                PrimaryTabs(items: ["合约", "事件合约", "TradFi", "模拟交易"], selected: $router.futuresTop, spacing: 16)
                    .padding(.horizontal, Spacing.pageH)
            }.frame(minHeight: 44)
            switch router.futuresTop {
            case 0: FuturesTradeView()
            case 1:
                if flags.isOn("eventContracts") { EventContractView() } else { ComingSoonView(title: "事件合约", note: "猜涨跌，到期按指数价结算，敬请期待") }
            case 2: TradFiView()
            default: ComingSoonView(title: "模拟交易", note: "使用模拟资金体验合约交易；OpenAPI 暂无独立模拟盘接口，待后端就绪后接入")
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
    // 截图路由在视图创建时就决定初始子 Tab：避免 m.start() 期间视图重建导致 @State 被重置、而 shotSheet 已被消费
    @State private var bottomTab = Router.shared.shotSheet == "ordersTpsl" || Router.shared.shotSheet == "ordersPlan" ? 1 : 0
    @State private var amendTarget: APIFuturesOrder?
    @State private var algoAmendTarget: APIFuturesAlgoOrder?
    @State private var showMode = false
    @State private var showLev = false
    @State private var showKind = false
    @State private var showCalc = false
    @State private var showTools = false
    @State private var showPairs = false
    @State private var showChart = false
    @State private var tpslTarget: APIFuturesPosition?
    @State private var showTpslForm = false
    @State private var marginTarget: APIFuturesPosition?
    @State private var confirm: ConfirmRequest?
    @State private var couponTab = 0
    @State private var orderFilter = Router.shared.shotSheet == "ordersTpsl" ? 1 : (Router.shared.shotSheet == "ordersPlan" ? 2 : 0)     // 委托：0 普通委托 1 止盈止损 2 计划委托
    @FocusState private var keyboardFocus: Bool

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
            .padding(.bottom, Metrics.tabBarH + 100)
        }
        .scrollDismissesKeyboard(.interactively)
        .lbKeyboardDone(focus: $keyboardFocus)
        .task {
            // 截图：把 K 线 / 委托区滚动到画面内
            let target: String? = router.shotSheet == "chart" ? "lb.chart" : (["ordersTpsl", "ordersPlan", "pnl", "pnlError", "pnlEmpty", "liq"].contains(router.shotSheet ?? "") ? "lb.bottom" : nil)
            guard let target else { return }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            proxy.scrollTo(target, anchor: .top)
        }
        }
        .refreshable { await m.refreshMarket(); await m.refreshAccount() }
        .background(p.bg)
        .onAppear { applyEarlyShot() }
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
        .lbSheet($showTools) { FuturesToolsSheet(onClose: { showTools = false }, onCalculator: { showCalc = true }) }
        .lbSheet($showCalc) { FuturesCalculatorSheet(entryHint: Fmt.priceAuto(m.mark), base: m.base, leverage: m.leverage, mmr: m.mmr) { showCalc = false } }
        .lbSheet($showPairs) { FuturesPairSheet(current: m.symbol) { s in showPairs = false; router.futuresSymbol = s } onClose: { showPairs = false } }
        .lbSheet($showTpslForm) { TpslSheet(m: m, position: tpslTarget) { showTpslForm = false } }
        .lbSheet(Binding(get: { marginTarget != nil }, set: { if !$0 { marginTarget = nil } })) {
            if let pos = marginTarget { AdjustMarginSheet(m: m, position: pos) { marginTarget = nil } }
        }
        .lbSheet(Binding(get: { algoAmendTarget != nil }, set: { if !$0 { algoAmendTarget = nil } })) {
            if let a = algoAmendTarget { AmendAlgoSheet(m: m, algo: a) { algoAmendTarget = nil } }
        }
        .lbSheet(Binding(get: { amendTarget != nil }, set: { if !$0 { amendTarget = nil } })) {
            if let o = amendTarget { AmendOrderSheet(m: m, order: o) { amendTarget = nil } }
        }
        .lbConfirm($confirm)
    }

    /// 只改本视图状态的截图路由在 onAppear 立即生效（不等网络），且不清空 shotSheet，保证重建后仍可恢复
    private func applyEarlyShot() {
        switch router.shotSheet {
        case "ordersTpsl": bottomTab = 1; orderFilter = 1
        case "ordersPlan": bottomTab = 1; orderFilter = 2
        case "plan": m.kind = .plan
        case "pnl", "pnlError", "pnlEmpty", "liq": applyPnlDemo(router.shotSheet ?? "")
        default: break
        }
    }

    /// 截图演示（CI 未登录）：注入示例仓位与今日盈亏三种状态，标记价仍取实时 WS
    private func applyPnlDemo(_ k: String) {
        m.shotDemo = true; bottomTab = 0
        let ref = m.mark > 0 ? m.mark : 78000
        let entry = Dec.plain(ref * Decimal(string: "0.985")!, m.pricePrec)
        m.positions = k == "pnlEmpty" ? [] : [APIFuturesPosition(symbol: m.symbol, side: .long, qty: "0.050", entryPrice: entry, markPrice: Dec.plain(ref, m.pricePrec),
                                                               marginMode: .cross, leverage: 20, margin: Dec.plain(Dec.parse(entry) * Decimal(string: "0.05")! / 20, 2))]
        if k == "liq" {
            let mark = Dec.plain(ref, m.pricePrec)
            m.positions = [
                APIFuturesPosition(symbol: m.symbol, side: .long, qty: "0.050", entryPrice: entry, markPrice: mark, marginMode: .cross, leverage: 2,
                                   margin: Dec.plain(Dec.parse(entry) * Decimal(string: "0.05")! / 2, 2), liquidationPrice: "0"),
                APIFuturesPosition(symbol: m.symbol, side: .short, qty: "0.020", entryPrice: entry, markPrice: mark, marginMode: .isolated, leverage: 1,
                                   margin: Dec.plain(Dec.parse(entry) * Decimal(string: "0.02")!, 2), liquidationPrice: Dec.plain(ref * 12, m.pricePrec)),
                APIFuturesPosition(symbol: m.symbol, side: .long, qty: "0.010", entryPrice: entry, markPrice: mark, marginMode: .isolated, leverage: 20,
                                   margin: Dec.plain(Dec.parse(entry) * Decimal(string: "0.01")! / 20, 2), liquidationPrice: Dec.plain(ref * Decimal(string: "0.955")!, m.pricePrec)),
            ]
        }
        switch k {
        case "pnlError": m.todayPnl = .failed("网络异常，请稍后重试")
        case "pnlEmpty": m.todayPnl = .loaded(APIFuturesTodayPnl200Response(asset: "USDT", since: 0, pnl: "0", realizedPnl: "0", funding: "0", fee: "0", liquidationFee: "0", insuranceCover: "0"))
        default: m.todayPnl = .loaded(APIFuturesTodayPnl200Response(asset: "USDT", since: 0, pnl: "37.4215", realizedPnl: "42.18", funding: "-1.2345", fee: "-3.524", liquidationFee: "0", insuranceCover: "0"))
        }
        m.accountLoaded = true
        m.syncMarkSubs()
    }

    private func applyShot() {
        switch router.shotSheet {
        case "leverage": showLev = true
        case "mode": showMode = true
        case "tpsl": showTpslForm = true
        case "kind": showKind = true
        case "tools": showTools = true
        case "chart": showChart = true
        case "calc": showCalc = true
        case "plan": m.kind = .plan
        case "ordersTpsl": bottomTab = 1; orderFilter = 1; return
        case "ordersPlan": bottomTab = 1; orderFilter = 2; return
        case "pnl", "pnlError", "pnlEmpty", "liq": applyPnlDemo(router.shotSheet ?? ""); return
        case "modeHedge": m.positionMode = .hedge; showMode = true
        case "tpslPartial": showTpslForm = true
        case "amend":
            amendTarget = m.openOrders.first ?? APIFuturesOrder(orderId: 0, symbol: m.symbol, side: "buy", type: "limit", timeInForce: "GTC",
                                                                 price: Dec.plain(m.last > 0 ? m.last * Decimal(string: "0.98")! : 0, m.pricePrec), qty: "0.010", filledQty: "0", status: "new", amendCount: 1)
        case "algoAmendPlan", "algoAmendTpsl":
            let plan = router.shotSheet == "algoAmendPlan"
            let ref = m.last > 0 ? m.last : 78000
            algoAmendTarget = (plan ? m.planAlgos.first : m.tpslAlgos.first) ?? APIFuturesAlgoOrder(
                algoId: 0, symbol: m.symbol, algoType: plan ? .plan : .tp, side: plan ? "buy" : "sell", positionSide: "BOTH",
                orderType: plan ? .limit : .market, price: plan ? Dec.plain(ref * Decimal(string: "0.97")!, m.pricePrec) : nil, qty: "0.010",
                triggerPrice: Dec.plain(ref * Decimal(string: plan ? "0.975" : "1.05")!, m.pricePrec), triggerPriceType: .mark,
                triggerDirection: plan ? .down : .up, reduceOnly: !plan, status: .active)
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
            ChipButton(title: m.hedge ? "分仓" : "合仓") { showMode = true }
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
                }.padding(.horizontal, 10).frame(minHeight: 34).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
            }.buttonStyle(PressableStyle(scale: 1))
            if m.kind == .plan {
                NumberBox(label: "触发价", unit: m.quote, text: $m.triggerText, onEdit: {})
                HStack(spacing: 6) {
                    Button { m.planTriggerType = m.planTriggerType == .last ? .mark : .last } label: {
                        HStack(spacing: 3) {
                            Text(m.planTriggerType == .mark ? "标记价格触发" : "最新价格触发")
                            Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6))
                        }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                    Spacer()
                    Checkbox(on: $m.planMarket, label: "市价")
                }
            }
            if m.kind == .market || (m.kind == .plan && m.planMarket) {
                Text("市价").font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
                    .frame(maxWidth: .infinity).frame(minHeight: 44).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
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
                    NumberBox(label: "止盈触发价(\(m.orderTpType == .last ? "最新" : "标记"))", unit: m.quote, text: $m.tpText, onEdit: {})
                    NumberBox(label: "止损触发价(\(m.orderSlType == .last ? "最新" : "标记"))", unit: m.quote, text: $m.slText, onEdit: {})
                }
            }
            VStack(spacing: 6) {
                HStack(spacing: 4) {
                    Text("可用").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Text("\(Fmt.amount(m.available, 2)) \(m.quote)").font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                    Button { Router.shared.open(.transfer) } label: { Image(systemName: "arrow.left.arrow.right.square.fill").font(.system(size: 12)).foregroundColor(p.textPrimary) }
                }
                KVRow(k: m.isOpen ? "可开" : "可平", v: "\(Fmt.amount(m.isOpen ? m.maxOpenQty : abs(Dec.parse(m.closeTarget?.qty)), m.qtyPrec)) \(m.base)")
            }
            if m.kind == .postOnly {
                Text("只做 Maker：若委托会立即成交则整单撤销").font(.lb(10)).foregroundColor(p.textTertiary)
            }
            // 未登录只显示一颗登录按钮（CI futures-light 曾出现双「登录/注册」叠放）
            if session.isLoggedIn {
                sideButton(long: true)
                sideButton(long: false)
            } else {
                LBButton(title: "登录 / 注册", kind: .primary, height: 40) {
                    session.present(.login)
                }
            }
        }
    }

    private func sideButton(long: Bool) -> some View {
        VStack(spacing: 6) {
            if m.isOpen {
                KVRow(k: "预估强平价", v: m.orderPrice > 0 ? Fmt.price(m.liqPrice(long: long), precision: m.pricePrec) : "--")
                KVRow(k: "成本", v: "\(Fmt.amount(m.cost, 2)) \(m.quote)")
            }
            LBButton(title: m.isOpen ? (long ? "开多" : "开空") : (long ? "平空" : "平多"),
                     kind: long ? .buy : .sell, height: 40, loading: m.placing) {
                session.requireLogin {
                    if let e = m.validation { ToastCenter.shared.show(e, .error); return }
                    let dir = m.isOpen ? (long ? "开多" : "开空") : (long ? "平空" : "平多")
                    confirm = .init(title: "\(dir) \(m.symbol)",
                                    message: "\(m.kind.title) · \(m.marginMode == .isolated ? "逐仓" : "全仓") \(m.leverage)x · \(m.hedge ? "分仓" : "合仓")\(m.kind == .plan ? "\n触发价 \(m.triggerText)（\(m.planTriggerType == .mark ? "标记价格" : "最新价格")）" : "")\n价格 \(m.kind == .market || (m.kind == .plan && m.planMarket) ? "市价" : m.priceText)  数量 \(Fmt.amount(m.qty, m.qtyPrec)) \(m.base)\n预估手续费 \(Fmt.amount(m.qty * m.orderPrice * m.takerFee, 4)) USDT",
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
                }.padding(.horizontal, Spacing.pageH).frame(minHeight: 36)
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
        if !session.isLoggedIn && !m.shotDemo {
            EmptyBlock(text: "登录后查看仓位", actions: [("登录 / 注册", { session.present(.login) })])
        } else {
            TodayPnlCard(state: m.todayPnl, unrealized: m.liveUnrealized, hasPositions: !m.positions.isEmpty) { Task { await m.loadTodayPnl() } }
            positionsBody
        }
    }

    @ViewBuilder private var positionsBody: some View {
        if !m.accountLoaded {
            SkeletonList(rows: 2)
        } else if m.shownPositions.isEmpty {
            // 对照图 #22：充值 / 模拟盘 / 跟单交易
            EmptyBlock(text: "暂无仓位", actions: [("充值", { Router.shared.open(.deposit("USDT")) }),
                                                 ("模拟盘", { Router.shared.futuresTop = 3 }),
                                                 ("跟单交易", { Router.shared.openFeature(.copyTrading) })])
        } else {
            LazyVStack(spacing: 0) {
                ForEach(m.shownPositions, id: \.self) { pos in
                    PositionCard(pos: pos, live: m.livePnl(pos), onTpsl: { tpslTarget = pos; showTpslForm = true },
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
                default: algoList(m.shownPlanAlgos, empty: "暂无计划委托")
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
                    FuturesOrderRow(o: o, onAmend: o.type == "market" ? nil : { amendTarget = o }) {
                        confirm = .init(title: "撤销委托", message: "确定撤销该笔委托吗？", confirmTitle: "撤销", destructive: true) { Task { await m.cancel(o) } }
                    }
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
            }
        }
    }

    /// 条件委托列表（止盈止损 / 计划委托，status=active）：加载 / 错误 / 空 / 列表 + 逐单撤销
    @ViewBuilder private func algoList(_ list: [APIFuturesAlgoOrder], empty: String) -> some View {
        if !m.algosLoaded {
            SkeletonList(rows: 2)
        } else if let e = m.algosError, list.isEmpty {
            EmptyBlock(text: "加载失败：\(e)", actions: [("重试", { Task { await m.refreshAccount() } })])
        } else if list.isEmpty {
            EmptyBlock(text: empty, actions: [])
        } else {
            LazyVStack(spacing: 0) {
                ForEach(list, id: \.self) { a in
                    AlgoOrderRow(a: a, onAmend: { algoAmendTarget = a }) {
                        confirm = .init(title: a.algoType == .plan ? "撤销计划委托" : "撤销止盈止损", message: "确定撤销该笔条件委托吗？",
                                        confirmTitle: "撤销", destructive: true) { Task { await m.cancelAlgo(a) } }
                    }
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
            }
        }
    }

    /// 止盈止损：tpsl-orders 委托（可部分数量）+ 仓位上整仓止盈止损
    @ViewBuilder private var tpslOrders: some View {
        let list = m.shownPositions.filter { !($0.tpPrice ?? "").isEmpty || !($0.slPrice ?? "").isEmpty }
        if !m.accountLoaded || !m.algosLoaded {
            SkeletonList(rows: 2)
        } else if !m.shownTpslAlgos.isEmpty || list.isEmpty {
            algoList(m.shownTpslAlgos, empty: "暂无止盈止损")
            if !list.isEmpty { legacyTpsl(list) }
        } else { legacyTpsl(list) }
    }
    @ViewBuilder private func legacyTpsl(_ list: [APIFuturesPosition]) -> some View {
        if list.isEmpty {
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
    /// 按 WS 实时标记价重算的盈亏 / 收益率 / 标记价
    var live: (pnl: Decimal, roe: Decimal, mark: Decimal)? = nil
    let onTpsl: () -> Void
    let onMargin: () -> Void
    let onClose: () -> Void
    var body: some View {
        let long = pos.side != .short
        let pnl = live?.pnl ?? Dec.parse(pos.unrealizedPnl), roe = live?.roe ?? Dec.parse(pos.roe) * 100
        let markV = (live?.mark ?? 0) > 0 ? live!.mark : Dec.parse(pos.markPrice)
        let pc = pnl < 0 ? p.down : p.up
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                Text(long ? "多" : "空").font(.lb(FontSize.small, .semibold)).foregroundColor(.white)
                    .frame(width: 20, height: 18).background(RoundedRectangle(cornerRadius: 3).fill(long ? p.up : p.down))
                Text(pos.symbol ?? "").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                if let ps = pos.positionSide, ps != .both {
                    Text("分仓").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        .padding(.horizontal, 4).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                }
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
                KVCell(k: "标记价格", v: Fmt.priceAuto(markV))
                KVCell(k: "强平价格", v: pos.liqText, align: .center)
                KVCell(k: "止盈/止损", v: lbTpslText(pos.tpPrice, pos.tpTriggerPriceType?.rawValue, pos.slPrice, pos.slTriggerPriceType?.rawValue), align: .trailing)
            }
            if let tip = pos.liqTip {
                Text(tip).font(.lb(FontSize.small)).foregroundColor(p.textTertiary).frame(maxWidth: .infinity, alignment: .leading)
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
    var onAmend: (() -> Void)? = nil
    let onCancel: () -> Void
    var body: some View {
        let buy = o.side == "buy"
        // 分仓：LONG+买 / SHORT+卖 为开仓；合仓看 reduceOnly
        let ps = o.positionSide ?? "BOTH"
        let open = ps == "BOTH" ? o.reduceOnly != true : (ps == "LONG") == buy
        let dir = open ? (buy ? "开多" : "开空") : (buy ? "平空" : "平多")
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(dir).font(.lb(FontSize.small, .semibold)).foregroundColor(buy ? p.up : p.down)
                Text(o.symbol ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(o.type == "market" ? "市价" : "限价").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                if o.timeInForce == "POST_ONLY" || o.timeInForce == "GTX" {
                    Text("只做Maker").font(.lb(10)).foregroundColor(p.textSecondary)
                }
                if let n = o.amendCount, n > 0 {
                    Text("已改\(n)次").font(.lb(10)).foregroundColor(p.brand)
                }
                Spacer()
                if let onAmend {
                    Button(action: onAmend) {
                        Text("改单").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                            .padding(.horizontal, 12).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                    }
                }
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
            Text("触发类型：止盈\(pos.tpTriggerPriceType == .last ? "最新价" : "标记价") / 止损\(pos.slTriggerPriceType == .last ? "最新价" : "标记价") · 触发后市价全部平仓").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private func small(_ t: String, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            Text(t).font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                .padding(.horizontal, 12).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
        }
    }
}

/// 条件委托行（计划委托 / 止盈止损委托）：方向、触发价（最新 / 标记）、委托价、数量、状态，撤销
struct AlgoOrderRow: View {
    @Environment(\.palette) private var p
    let a: APIFuturesAlgoOrder
    var onAmend: (() -> Void)? = nil
    var onCancel: (() -> Void)? = nil
    var body: some View {
        let buy = a.side == "buy"
        let ps = a.positionSide ?? "BOTH"
        let open = a.algoType == .plan && (ps == "BOTH" ? a.reduceOnly != true : (ps == "LONG") == buy)
        let dir = open ? (buy ? "开多" : "开空") : (buy ? "平空" : "平多")
        let typeText = a.algoType == .plan ? "计划委托" : (a.algoType == .tp ? "止盈" : "止损")
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(dir).font(.lb(FontSize.small, .semibold)).foregroundColor(buy ? p.up : p.down)
                Text(a.symbol ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(typeText).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                if ps != "BOTH" {
                    Text("分仓").font(.lb(10)).foregroundColor(p.textSecondary)
                        .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                }
                Spacer()
                if let onCancel, a.status == .active {
                    if let onAmend {
                        Button(action: onAmend) {
                            Text("修改").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                                .padding(.horizontal, 12).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                        }
                    }
                    Button(action: onCancel) {
                        Text("撤销").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                            .padding(.horizontal, 12).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                    }
                } else {
                    Text(Self.statusText(a.status)).font(.lb(FontSize.small)).foregroundColor(a.status == .failed ? p.down : p.textSecondary)
                }
            }
            HStack {
                KVCell(k: "触发价(\(a.triggerPriceType == .mark ? "标记" : "最新"))",
                       v: "\(a.triggerDirection == .down ? "≤" : "≥") \(Fmt.priceAuto(Dec.parse(a.triggerPrice)))")
                KVCell(k: "委托价格", v: a.orderType == .market || (a.price ?? "").isEmpty ? "市价" : Fmt.priceAuto(Dec.parse(a.price)), align: .center)
                KVCell(k: "数量", v: (a.qty ?? "").isEmpty ? "全部仓位" : (a.qty ?? ""), align: .trailing)
            }
            if let r = a.failReason, !r.isEmpty {
                Text("失败原因：\(ErrorText.map[r] ?? r)").font(.lb(FontSize.small)).foregroundColor(p.down)
            }
            if let d = a.createdAt { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    static func statusText(_ s: APIFuturesAlgoOrder.APIStatus?) -> String {
        switch s {
        case .active: return "等待触发"
        case .triggered: return "已触发"
        case .canceled: return "已撤销"
        case .failed: return "触发失败"
        case nil: return "--"
        }
    }
}

/// 今日盈亏卡片（UTC 00:00 起）：合计 / 已实现盈亏 / 资金费 / 手续费 + 实时未实现盈亏
struct TodayPnlCard: View {
    @Environment(\.palette) private var p
    let state: Loadable<APIFuturesTodayPnl200Response>
    let unrealized: Decimal
    let hasPositions: Bool
    let retry: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("今日盈亏(USDT)").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .underline(true, color: p.textTertiary.opacity(0.4))
                Text("UTC 00:00 起").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                Spacer()
                if hasPositions {
                    Text("未实现 ").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    + Text(signed(unrealized)).font(.lbNum(FontSize.small, .medium)).foregroundColor(color(unrealized))
                }
            }
            switch state {
            case .idle, .loading:
                HStack(spacing: 8) {
                    ForEach(0..<4, id: \.self) { _ in RoundedRectangle(cornerRadius: 3).fill(p.fill).frame(height: 30) }
                }.redacted(reason: .placeholder)
            case let .failed(msg):
                HStack {
                    Text("今日盈亏加载失败：\(msg)").font(.lb(FontSize.small)).foregroundColor(p.down).lineLimit(2)
                    Spacer()
                    Button(action: retry) {
                        Text("重试").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                            .padding(.horizontal, 10).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                    }.buttonStyle(PressableStyle())
                }
            case let .loaded(v):
                let total = Dec.parse(v.pnl), realized = Dec.parse(v.realizedPnl), funding = Dec.parse(v.funding)
                let fee = Dec.parse(v.fee) + Dec.parse(v.liquidationFee), other = Dec.parse(v.insuranceCover)
                if total == 0 && realized == 0 && funding == 0 && fee == 0 && other == 0 {
                    Text("今日暂无已实现盈亏、资金费或手续费记录").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                } else {
                    Text(raw(v.pnl, total)).font(.lbNum(FontSize.title, .semibold)).foregroundColor(color(total))
                    HStack {
                        KVCell(k: "已实现盈亏", v: raw(v.realizedPnl, realized), vColor: color(realized))
                        KVCell(k: "资金费", v: raw(v.funding, funding), align: .center, vColor: color(funding))
                        KVCell(k: "手续费", v: Dec.parse(v.liquidationFee) == 0 ? raw(v.fee, fee) : signed(fee), align: .trailing, vColor: color(fee))
                    }
                }
            }
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.bgElevated))
        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
        .padding(.horizontal, Spacing.pageH).padding(.top, 10)
    }
    /// 后端金额为固定 8 位小数字符串，原样展示（不截断、不去尾零），仅为正数补 "+"
    private func raw(_ s: String?, _ d: Decimal) -> String {
        guard let s, !s.isEmpty else { return signed(d) }
        return (d > 0 && !s.hasPrefix("+") ? "+" : "") + s
    }
    /// 仅在需要客户端求和时使用，保留 8 位小数
    private func signed(_ d: Decimal) -> String { (d > 0 ? "+" : "") + Fmt.amount(d, 8) }
    private func color(_ d: Decimal) -> Color { d > 0 ? p.up : (d < 0 ? p.down : p.textPrimary) }
}
