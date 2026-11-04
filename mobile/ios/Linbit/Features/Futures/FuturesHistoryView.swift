import SwiftUI

/// 对照图 #23/#24：合约历史记录
/// 当前委托 / 历史委托 / 历史仓位 / 成交明细 / 平仓盈亏 / 资金流水
struct FuturesHistoryView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    let symbol: String
    @State private var tab = Router.shared.shotSheet == "posHistory" ? 2 : (Router.shared.shotSheet == "algoHistory" ? 6 : 0)
    // 历史仓位：cursor 分页 + 平仓时间筛选
    @State private var posHist: Loadable<[APIFuturesPositionHistory]> = .idle
    @State private var posCursor: Int64?
    @State private var loadingMore = false
    @State private var timeFilter = 1           // 0 近7天 1 近30天 2 近90天 3 全部
    @State private var pickingTime = false
    // 条件委托：计划委托 / 止盈止损 × 进行中 / 历史
    @State private var algoKind = 0
    @State private var algoHistory = Router.shared.shotSheet == "algoHistory"
    @State private var algos: Loadable<[APIFuturesAlgoOrder]> = .idle
    @State private var algoCursor: Int64?
    @State private var filterSymbol: String?
    @State private var typeFilter = 0          // 0 全部 1 限价 2 市价 3 止盈止损
    @State private var billKind: String?
    @State private var picking = false
    @State private var pickingType = false
    @State private var confirm: ConfirmRequest?
    @State private var orders: Loadable<[APIFuturesOrder]> = .idle
    @State private var trades: Loadable<[APIFuturesMyTrades200ResponseTradesInner]> = .idle
    @State private var bills: Loadable<[APIFuturesBills200ResponseBillsInner]> = .idle

    private let tabs = ["当前委托", "历史委托", "历史仓位", "成交明细", "平仓盈亏", "资金流水", "条件委托"]
    private let timeTitles = ["近 7 天", "近 30 天", "近 90 天", "全部"]
    private let typeTitles = ["全部", "限价单", "市价单", "止盈止损"]
    static let billKinds: [(String?, String)] = [(nil, "全部"), ("transfer_in", "转入"), ("transfer_out", "转出"), ("fee", "手续费"),
                                                 ("realized_pnl", "已实现盈亏"), ("funding", "资金费用"), ("liquidation_fee", "强平费"),
                                                 ("insurance_cover", "风险准备金"), ("copy_transfer_in", "跟单转入"), ("copy_transfer_out", "跟单转出")]

    var body: some View {
        FeatureShell(title: "历史记录") {
            VStack(spacing: 0) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 18) {
                        ForEach(tabs.indices, id: \.self) { i in
                            VStack(spacing: 6) {
                                Text(tabs[i]).font(.lb(FontSize.label, tab == i ? .semibold : .regular))
                                    .foregroundColor(tab == i ? p.textPrimary : p.textSecondary)
                                Rectangle().fill(tab == i ? p.textPrimary : .clear).frame(width: 16, height: 2)
                            }.onTapGesture { tab = i; Task { await load() } }
                        }
                    }.padding(.horizontal, Spacing.pageH)
                }.frame(height: 40)
                filterBar
                Divider().overlay(p.divider)
                ScrollView { content }.refreshable { await load() }
            }
        }
        .task { await load() }
        .onChange(of: session.isLoggedIn) { _ in Task { await load() } }
        .lbSheet($picking) { FuturesSymbolFilterSheet(selected: filterSymbol) { s in picking = false; filterSymbol = s; Task { await load() } } onClose: { picking = false } }
        .lbSheet($pickingType) {
            LBSheet(title: tab == 5 ? "流水类型" : "委托类型", showClose: true, onClose: { pickingType = false }) {
                ScrollView {
                    VStack(spacing: 0) {
                        if tab == 5 {
                            ForEach(Self.billKinds.indices, id: \.self) { i in
                                SheetOptionRow(title: Self.billKinds[i].1, selected: billKind == Self.billKinds[i].0) {
                                    billKind = Self.billKinds[i].0; pickingType = false; Task { await load() }
                                }
                            }
                        } else {
                            ForEach(typeTitles.indices, id: \.self) { i in
                                SheetOptionRow(title: typeTitles[i], selected: typeFilter == i) { typeFilter = i; pickingType = false }
                            }
                        }
                    }
                }.frame(maxHeight: 420)
            }
        }
        .lbSheet($pickingTime) {
            LBSheet(title: "平仓时间", showClose: true, onClose: { pickingTime = false }) {
                VStack(spacing: 0) {
                    ForEach(timeTitles.indices, id: \.self) { i in
                        SheetOptionRow(title: timeTitles[i], selected: timeFilter == i) { timeFilter = i; pickingTime = false; Task { await load() } }
                    }
                }
            }
        }
        .lbConfirm($confirm)
        .onAppear { if Router.shared.shotSheet == "posHistory" || Router.shared.shotSheet == "algoHistory" { Router.shared.shotSheet = nil } }
    }

    private var startTime: Int64? {
        let days = [7, 30, 90][safe: timeFilter]
        return days.map { Int64((Date().timeIntervalSince1970 - Double($0) * 86400) * 1000) }
    }

    private var filterBar: some View {
        HStack(spacing: 14) {
            dropdown(filterSymbol ?? "币对") { picking = true }
            if tab <= 1 { dropdown(typeFilter == 0 ? "委托类型" : typeTitles[typeFilter]) { pickingType = true } }
            if tab == 2 { dropdown(timeTitles[timeFilter]) { pickingTime = true } }
            if tab == 6 {
                dropdown(algoKind == 0 ? "计划委托" : "止盈止损") { algoKind = algoKind == 0 ? 1 : 0; Task { await load() } }
                dropdown(algoHistory ? "历史" : "进行中") { algoHistory.toggle(); Task { await load() } }
            }
            if tab == 5 { dropdown(Self.billKinds.first { $0.0 == billKind }?.1 ?? "类型") { pickingType = true } }
            Spacer()
            if tab == 0 {
                let open = filteredOrders(orders.value ?? [])
                Button {
                    guard !open.isEmpty else { return }
                    confirm = .init(title: "全部撤销", message: "确定撤销 \(open.count) 笔委托吗？", confirmTitle: "全部撤销", destructive: true) {
                        Task {
                            for o in open { if let id = o.orderId { try? await FuturesAPI.cancel(id) } }
                            ToastCenter.shared.show("已提交撤销", .success); await load()
                        }
                    }
                } label: {
                    Text("全部撤销").font(.lb(FontSize.small)).foregroundColor(open.isEmpty ? p.textTertiary : p.textPrimary)
                        .padding(.horizontal, 10).frame(height: 24).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                }
            }
        }.padding(.horizontal, Spacing.pageH).frame(height: 36)
    }
    private func dropdown(_ t: String, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            HStack(spacing: 3) {
                Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)).foregroundColor(p.textSecondary)
            }
        }
    }

    private func filteredOrders(_ list: [APIFuturesOrder]) -> [APIFuturesOrder] {
        list.filter { o in
            switch typeFilter {
            case 1: return o.type == "limit" && o.source != "tpsl"
            case 2: return o.type == "market" && o.source != "tpsl"
            case 3: return o.source == "tpsl"
            default: return true
            }
        }
    }

    @ViewBuilder private var content: some View {
        if !session.isLoggedIn {
            EmptyBlock(text: "登录后查看历史记录", actions: [("登录 / 注册", { session.present(.login) })])
        } else {
            switch tab {
            case 0, 1:
                LoadableView(state: orders, emptyText: "暂无数据", isEmpty: { filteredOrders($0).isEmpty }, retry: { Task { await load() } }) { list in
                    LazyVStack(spacing: 0) {
                        ForEach(filteredOrders(list), id: \.self) { o in
                            if tab == 0 {
                                FuturesOrderRow(o: o) {
                                    confirm = .init(title: "撤销委托", message: "确定撤销该笔委托吗？", confirmTitle: "撤销", destructive: true) {
                                        Task {
                                            do { if let id = o.orderId { try await FuturesAPI.cancel(id) }; ToastCenter.shared.show("已撤销", .success); await load() }
                                            catch { ToastCenter.shared.error(error) }
                                        }
                                    }
                                }
                            } else { FuturesHistoryOrderRow(o: o) }
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                    }
                }
            case 2:
                LoadableView(state: posHist, emptyText: "暂无历史仓位", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { list in
                    LazyVStack(spacing: 0) {
                        ForEach(Array(list.enumerated()), id: \.offset) { _, h in
                            PositionHistoryRow(h: h)
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                        moreButton(hasMore: posCursor != nil) { await loadMorePositions() }
                    }
                }
            case 6:
                LoadableView(state: algos, emptyText: algoHistory ? "暂无历史条件委托" : "暂无进行中的条件委托", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { list in
                    LazyVStack(spacing: 0) {
                        ForEach(list, id: \.self) { a in
                            AlgoOrderRow(a: a, onCancel: algoHistory ? nil : {
                                confirm = .init(title: "撤销条件委托", message: "确定撤销该笔条件委托吗？", confirmTitle: "撤销", destructive: true) {
                                    Task {
                                        do {
                                            guard let id = a.algoId else { return }
                                            if a.algoType == .plan { try await FuturesAPI.cancelPlan(id) } else { try await FuturesAPI.cancelTpsl(id) }
                                            ToastCenter.shared.show("已撤销", .success); await load()
                                        } catch { ToastCenter.shared.error(error) }
                                    }
                                }
                            })
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                        moreButton(hasMore: algoCursor != nil) { await loadMoreAlgos() }
                    }
                }
            case 3:
                LoadableView(state: trades, emptyText: "暂无成交", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { list in
                    LazyVStack(spacing: 0) {
                        ForEach(list, id: \.self) { t in
                            FuturesTradeRow(t: t)
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                    }
                }
            default:
                LoadableView(state: bills, emptyText: "暂无数据", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { list in
                    LazyVStack(spacing: 0) {
                        ForEach(list, id: \.self) { b in
                            FuturesBillRow(b: b)
                            Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                        }
                    }
                }
            }
        }
    }

    private func load() async {
        guard TokenStore.shared.isLoggedIn else { return }
        do {
            switch tab {
            case 0:
                if orders.value == nil { orders = .loading }
                orders = .loaded(try await FuturesAPI.openOrders(symbol: filterSymbol))
            case 1:
                orders = .loading
                let all = try await FuturesAPI.orders(symbol: filterSymbol)
                orders = .loaded(all.filter { !["new", "partially_filled", "open"].contains($0.status ?? "") })
            case 2:
                posHist = .loading
                let r = try await FuturesAPI.positionHistory(symbol: filterSymbol, startTime: startTime, endTime: nil, cursor: nil)
                posHist = .loaded(r.positions ?? []); posCursor = r.nextCursor
            case 6:
                algos = .loading
                let r: FuturesAPI.AlgoList
                if algoKind == 0 { r = try await FuturesAPI.planOrders(symbol: filterSymbol, history: algoHistory) }
                else { r = try await FuturesAPI.tpslOrders(symbol: filterSymbol, history: algoHistory) }
                algos = .loaded(r.orders ?? []); algoCursor = r.nextCursor
            case 3:
                trades = .loading
                trades = .loaded(try await FuturesAPI.myTrades(symbol: filterSymbol))
            case 4:
                bills = .loading
                bills = .loaded(try await FuturesAPI.bills(kind: "realized_pnl").filter { filterSymbol == nil || $0.symbol == filterSymbol })
            default:
                bills = .loading
                bills = .loaded(try await FuturesAPI.bills(kind: billKind).filter { filterSymbol == nil || $0.symbol == filterSymbol })
            }
        } catch {
            switch tab {
            case 0, 1: orders = .failed(error.lbMessage)
            case 2: posHist = .failed(error.lbMessage)
            case 3: trades = .failed(error.lbMessage)
            case 6: algos = .failed(error.lbMessage)
            default: bills = .failed(error.lbMessage)
            }
        }
    }

    @ViewBuilder private func moreButton(hasMore: Bool, _ more: @escaping () async -> Void) -> some View {
        if hasMore {
            Button { Task { await more() } } label: {
                HStack(spacing: 6) {
                    if loadingMore { ProgressView().scaleEffect(0.7) }
                    Text(loadingMore ? "加载中…" : "加载更多").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }.frame(maxWidth: .infinity).frame(height: 44)
            }.disabled(loadingMore)
        } else {
            Text("没有更多了").font(.lb(FontSize.small)).foregroundColor(p.textTertiary).frame(maxWidth: .infinity).frame(height: 44)
        }
    }
    private func loadMorePositions() async {
        guard let c = posCursor, !loadingMore else { return }
        loadingMore = true; defer { loadingMore = false }
        do {
            let r = try await FuturesAPI.positionHistory(symbol: filterSymbol, startTime: startTime, endTime: nil, cursor: c)
            posHist = .loaded((posHist.value ?? []) + (r.positions ?? [])); posCursor = r.nextCursor
        } catch { ToastCenter.shared.error(error) }
    }
    private func loadMoreAlgos() async {
        guard let c = algoCursor, !loadingMore else { return }
        loadingMore = true; defer { loadingMore = false }
        do {
            let r: FuturesAPI.AlgoList
            if algoKind == 0 { r = try await FuturesAPI.planOrders(symbol: filterSymbol, history: algoHistory, cursor: c) }
            else { r = try await FuturesAPI.tpslOrders(symbol: filterSymbol, history: algoHistory, cursor: c) }
            algos = .loaded((algos.value ?? []) + (r.orders ?? [])); algoCursor = r.nextCursor
        } catch { ToastCenter.shared.error(error) }
    }
}

struct FuturesHistoryOrderRow: View {
    @Environment(\.palette) private var p
    let o: APIFuturesOrder
    var body: some View {
        let buy = o.side == "buy"
        let open = o.reduceOnly != true
        let dir = open ? (buy ? "开多" : "开空") : (buy ? "平空" : "平多")
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(dir).font(.lb(FontSize.small, .semibold)).foregroundColor(buy ? p.up : p.down)
                Text(o.symbol ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(o.source == "tpsl" ? "止盈止损" : (o.type == "market" ? "市价" : "限价")).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                Text(Self.statusText(o.status)).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            HStack {
                KVCell(k: "委托价格", v: o.price.map { Fmt.priceAuto(Dec.parse($0)) } ?? "市价")
                KVCell(k: "成交均价", v: o.avgPrice.map { Fmt.priceAuto(Dec.parse($0)) } ?? "--", align: .center)
                KVCell(k: "已成交/数量", v: "\(o.filledQty ?? "0")/\(o.qty ?? "--")", align: .trailing)
            }
            HStack {
                KVCell(k: "手续费(USDT)", v: o.fee.map { Fmt.amount(Dec.parse($0), 4) } ?? "--")
                KVCell(k: "止盈/止损", v: lbTpslText(o.tpPrice, o.tpTriggerPriceType?.rawValue, o.slPrice, o.slTriggerPriceType?.rawValue), align: .trailing)
            }
            if let d = o.createdAt { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    static func statusText(_ s: String?) -> String {
        ["filled": "已成交", "canceled": "已撤销", "cancelled": "已撤销", "rejected": "已拒绝", "expired": "已过期",
         "partially_filled": "部分成交", "new": "未成交", "open": "未成交"][s ?? ""] ?? (s ?? "--")
    }
}

struct FuturesTradeRow: View {
    @Environment(\.palette) private var p
    let t: APIFuturesMyTrades200ResponseTradesInner
    var body: some View {
        let buy = t.side == .buy
        let price = Dec.parse(t.price), qty = Dec.parse(t.qty)
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(buy ? "买入" : "卖出").font(.lb(FontSize.small, .semibold)).foregroundColor(buy ? p.up : p.down)
                Text(t.symbol ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(t.role == .maker ? "Maker" : "Taker").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                if let d = t.time { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
            }
            HStack {
                KVCell(k: "成交价格", v: Fmt.priceAuto(price))
                KVCell(k: "成交数量", v: t.qty ?? "--", align: .center)
                KVCell(k: "成交额(USDT)", v: Fmt.amount(price * qty, 2), align: .trailing)
            }
            KVRow(k: "手续费", v: "\(Fmt.amount(Dec.parse(t.fee), 6)) USDT")
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
}

struct FuturesBillRow: View {
    @Environment(\.palette) private var p
    let b: APIFuturesBills200ResponseBillsInner
    var body: some View {
        let amt = Dec.parse(b.amount)
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(FuturesHistoryView.billKinds.first { $0.0 == b.kind }?.1 ?? (b.kind ?? "--"))
                    .font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                Text([b.symbol, b.time.map(Fmt.dateTime)].compactMap { $0 }.joined(separator: " · "))
                    .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
            Spacer()
            Text((amt > 0 ? "+" : "") + Fmt.amount(amt, 4) + " USDT").font(.lbNum(FontSize.label, .medium))
                .foregroundColor(amt < 0 ? p.down : p.up)
        }.padding(.horizontal, Spacing.pageH).frame(height: 60)
    }
}

/// 历史仓位行：方向 / 杠杆 / 状态，开平仓均价、最大持仓、已平数量、已实现 / 手续费 / 资金费 / 净盈亏，开平仓时间
struct PositionHistoryRow: View {
    @Environment(\.palette) private var p
    let h: APIFuturesPositionHistory
    var body: some View {
        let long = h.side != .short
        let net = Dec.parse(h.netPnl)
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Text(long ? "多" : "空").font(.lb(FontSize.small, .semibold)).foregroundColor(.white)
                    .frame(width: 20, height: 18).background(RoundedRectangle(cornerRadius: 3).fill(long ? p.up : p.down))
                Text(h.symbol ?? "").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text("\(h.marginMode == "isolated" ? "逐仓" : "全仓") \(h.leverage ?? 0)x").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    .padding(.horizontal, 4).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                if let ps = h.positionSide, ps != "BOTH" {
                    Text("分仓").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                Spacer()
                Text(statusText).font(.lb(FontSize.small)).foregroundColor(h.status == .closed ? p.textSecondary : p.down)
            }
            HStack {
                KVCell(k: "净盈亏(USDT)", v: Fmt.amount(net, 4), vColor: net < 0 ? p.down : p.up)
                KVCell(k: "已实现盈亏", v: Fmt.amount(Dec.parse(h.realizedPnl), 4), align: .trailing)
            }
            HStack {
                KVCell(k: "开仓均价", v: Fmt.priceAuto(Dec.parse(h.entryPrice)))
                KVCell(k: "平仓均价", v: Fmt.priceAuto(Dec.parse(h.closePrice)), align: .center)
                KVCell(k: "最大持仓", v: h.maxQty ?? "--", align: .trailing)
            }
            HStack {
                KVCell(k: "已平数量", v: h.closedQty ?? "--")
                KVCell(k: "手续费", v: Fmt.amount(Dec.parse(h.fee), 4), align: .center)
                KVCell(k: "资金费用", v: Fmt.amount(Dec.parse(h.fundingFee), 4), align: .trailing)
            }
            HStack {
                Text("开仓 \(h.openTime.map(Fmt.dateTime) ?? "--")")
                Spacer()
                Text("平仓 \(h.closeTime.map(Fmt.dateTime) ?? "--")")
            }.font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private var statusText: String {
        switch h.status {
        case .liquidated: return "强制平仓"
        case .adl: return "自动减仓"
        default: return "已平仓"
        }
    }
}
