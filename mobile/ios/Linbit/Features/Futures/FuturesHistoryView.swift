import SwiftUI

/// 对照图 #23/#24：合约历史记录
/// 当前委托 / 历史委托 / 历史仓位 / 成交明细 / 平仓盈亏 / 资金流水
struct FuturesHistoryView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    let symbol: String
    @State private var tab = 0
    @State private var filterSymbol: String?
    @State private var typeFilter = 0          // 0 全部 1 限价 2 市价 3 止盈止损
    @State private var billKind: String?
    @State private var picking = false
    @State private var pickingType = false
    @State private var confirm: ConfirmRequest?
    @State private var orders: Loadable<[APIFuturesOrder]> = .idle
    @State private var trades: Loadable<[APIFuturesMyTrades200ResponseTradesInner]> = .idle
    @State private var bills: Loadable<[APIFuturesBills200ResponseBillsInner]> = .idle

    private let tabs = ["当前委托", "历史委托", "历史仓位", "成交明细", "平仓盈亏", "资金流水"]
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
        .lbConfirm($confirm)
    }

    private var filterBar: some View {
        HStack(spacing: 14) {
            dropdown(filterSymbol ?? "币对") { picking = true }
            if tab <= 1 { dropdown(typeFilter == 0 ? "委托类型" : typeTitles[typeFilter]) { pickingType = true } }
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
                VStack(spacing: 0) {
                    InlineBanner(text: "历史仓位需后端提供已平仓位查询接口，当前可在「平仓盈亏」查看已实现盈亏").padding(Spacing.pageH)
                    EmptyBlock(text: "暂无数据", actions: [])
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
            case 2: break
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
            case 3: trades = .failed(error.lbMessage)
            default: bills = .failed(error.lbMessage)
            }
        }
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
                KVCell(k: "止盈/止损", v: "\(o.tpPrice ?? "--")/\(o.slPrice ?? "--")", align: .trailing)
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
