import SwiftUI

/// 策略交易：网格广场 / 我的策略；创建网格 → GridCreateView
struct StrategyView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var tab = 0
    @State private var square: Loadable<[APIGridSquareItem]> = .idle
    @State private var mine: Loadable<[APIGridBot]> = .idle
    @State private var stopping: APIGridBot?
    @State private var sellBase = false
    @State private var busy = false
    @State private var create = false
    @State private var copyFrom: APIGridSquareItem?

    var body: some View {
        FeatureShell(title: "策略交易") {
            VStack(spacing: 0) {
                PrimaryTabs(items: ["网格广场", "我的策略"], selected: $tab)
                ScrollView {
                    VStack(spacing: 0) { if tab == 0 { squareList } else { mineList } }.padding(.bottom, 90)
                }
                .refreshable { await load() }
                LBButton(title: "创建现货网格") { session.requireLogin { copyFrom = nil; create = true } }
                    .padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
            }
        }
        .task { await load(); if Router.shared.shotSheet == "gridCreate" { Router.shared.shotSheet = nil; create = true } }
        .onChange(of: tab) { _ in Task { await load() } }
        .fullScreenCover(isPresented: $create) {
            ThemedRoot { GridCreateView(copy: copyFrom) { created in create = false; if created { tab = 1; Task { await load() } } } }
                .environmentObject(session).overlay(alignment: .top) { ToastOverlay() }
        }
        .lbSheet(Binding(get: { stopping != nil }, set: { if !$0 { stopping = nil } })) {
            if let b = stopping {
                LBSheet(title: "停止网格", subtitle: b.symbol, showClose: true, onClose: { stopping = nil }) {
                    VStack(alignment: .leading, spacing: 10) {
                        KVRow(k: "总收益", v: "\(Fmt.amount(Dec.parse(b.totalProfit), 4)) \(b.quoteAsset ?? "USDT")", vColor: Dec.parse(b.totalProfit) < 0 ? p.down : p.up)
                        KVRow(k: "持有 \(b.baseAsset ?? "")", v: b.baseHeld ?? "0")
                        Checkbox(on: $sellBase, label: "停止时以市价卖出持有的 \(b.baseAsset ?? "币种")")
                        Text("停止后将撤销所有网格挂单，资金返还现货账户。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        LBButton(title: "确认停止", kind: .sell, loading: busy) { Task { await stop(b) } }
                    }.padding(.horizontal, Spacing.pageH)
                }
            }
        }
    }

    private func load() async {
        if tab == 0 {
            if square.value == nil { square = .loading }
            do { square = .loaded(try await GridAPI.square()) } catch { if square.value == nil { square = .failed(error.lbMessage) } }
        } else if TokenStore.shared.isLoggedIn {
            if mine.value == nil { mine = .loading }
            do { mine = .loaded(try await GridAPI.list()) } catch { if mine.value == nil { mine = .failed(error.lbMessage) } }
        }
    }
    private func stop(_ b: APIGridBot) async {
        busy = true; defer { busy = false }
        do { _ = try await GridAPI.stop(b.id ?? "", sellBase: sellBase); ToastCenter.shared.show("网格已停止", .success); stopping = nil; mine = .idle; await load() }
        catch { ToastCenter.shared.error(error) }
    }

    @ViewBuilder private var squareList: some View {
        switch square {
        case .idle, .loading: SkeletonList(rows: 5)
        case let .failed(e): ErrorStateView(message: e) { Task { await load() } }.frame(height: 320)
        case let .loaded(items):
            if items.isEmpty { EmptyStateView(text: "暂无公开策略").frame(height: 300) }
            ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text(it.symbol ?? "").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                        Text("现货网格").font(.lb(10)).foregroundColor(p.textSecondary).padding(.horizontal, 4).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                        Spacer()
                        Button("复制") { session.requireLogin { copyFrom = it; create = true } }
                            .font(.lb(FontSize.caption, .medium)).foregroundColor(p.textOnPrimary).padding(.horizontal, 14).frame(height: 28).background(Capsule().fill(p.primary))
                    }
                    HStack {
                        KVCell(k: "收益率", v: Fmt.percent(Dec.parse(it.profitRate) * 100), vColor: Dec.parse(it.profitRate) < 0 ? p.down : p.up)
                        KVCell(k: "运行时长", v: runtime(it.runtimeSeconds), align: .center)
                        KVCell(k: "7日最大回撤", v: Fmt.percent(Dec.parse(it.maxDrawdown7d) * 100), align: .trailing)
                    }
                    Text("区间 \(it.lowerPrice ?? "--") - \(it.upperPrice ?? "--") · \(it.gridCount ?? 0) 格 · \(it.copyCount ?? 0) 人复制")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
            }
        }
    }

    @ViewBuilder private var mineList: some View {
        if !session.isLoggedIn {
            EmptyBlock(text: "登录后查看我的策略", actions: [("登录 / 注册", { session.present(.login) })])
        } else {
            switch mine {
            case .idle, .loading: SkeletonList(rows: 4)
            case let .failed(e): ErrorStateView(message: e) { Task { await load() } }.frame(height: 320)
            case let .loaded(bots):
                if bots.isEmpty { EmptyBlock(text: "暂无运行中的策略", actions: [("创建网格", { create = true })]) }
                ForEach(Array(bots.enumerated()), id: \.offset) { _, b in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text(b.symbol ?? "").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                            Text(statusText(b.status)).font(.lb(10)).foregroundColor(b.status == .running ? p.up : p.textSecondary)
                            Spacer()
                            if b.status == .running {
                                Button("停止") { sellBase = b.sellBaseOnStop ?? false; stopping = b }
                                    .font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary).padding(.horizontal, 14).frame(height: 28).background(Capsule().fill(p.fill))
                            }
                        }
                        HStack {
                            KVCell(k: "总收益(\(b.quoteAsset ?? "USDT"))", v: Fmt.amount(Dec.parse(b.totalProfit), 4), vColor: Dec.parse(b.totalProfit) < 0 ? p.down : p.up)
                            KVCell(k: "网格收益", v: Fmt.amount(Dec.parse(b.gridProfit), 4), align: .center)
                            KVCell(k: "投入", v: Fmt.amount(Dec.parse(b.investment), 2), align: .trailing)
                        }
                        HStack {
                            KVCell(k: "区间", v: "\(b.lowerPrice ?? "--")-\(b.upperPrice ?? "--")")
                            KVCell(k: "成交次数", v: "\(b.cycles ?? 0)", align: .center)
                            KVCell(k: "运行时长", v: runtime(b.runtimeSeconds), align: .trailing)
                        }
                        if let r = b.stopReason, b.status != .running { Text("停止原因：\(r)").font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                    }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
            }
        }
    }
    private func statusText(_ s: APIGridBot.APIStatus?) -> String {
        switch s { case .running: return "运行中"; case .stopping: return "停止中"; case .stopped: return "已停止"; default: return "--" }
    }
    private func runtime(_ s: Int?) -> String {
        let s = s ?? 0
        return s >= 86400 ? "\(s / 86400)天\(s % 86400 / 3600)时" : "\(s / 3600)时\(s % 3600 / 60)分"
    }
}

struct GridCreateView: View {
    @Environment(\.palette) private var p
    let copy: APIGridSquareItem?
    let onDone: (Bool) -> Void
    @StateObject private var store = MarketStore.shared
    @State private var symbol = "BTCUSDT"
    @State private var mode = 0
    @State private var lower = ""
    @State private var upper = ""
    @State private var grids = "20"
    @State private var invest = ""
    @State private var tp = ""
    @State private var sl = ""
    @State private var advanced = false
    @State private var preview: APIStrategyPreview200Response?
    @State private var previewErr: String?
    @State private var confirm = false
    @State private var pairs = false
    @State private var busy = false
    @State private var avail: Decimal = 0

    @ObservedObject private var session = Session.shared
    @State private var refPrice: Decimal = 0
    /// 当前价：现货最新价（MarketStore 已做标记价/指数价兜底）→ 合约 标记价/指数价
    private var price: Decimal { let s = store.spot.first { $0.symbol == symbol }?.last ?? 0; return s > 0 ? s : refPrice }
    private func refreshRefPrice() async {
        if let t = try? await FuturesAPI.ticker(symbol) { refPrice = t.effectivePrice() }
    }
    /// 本地估算每格利润率（等差：步长/价格），未拿到服务端预览时展示
    private var estProfit: String {
        let lo = Dec.parse(lower), hi = Dec.parse(upper), n = Decimal(Int(grids) ?? 0)
        guard lo > 0, hi > lo, n > 0 else { return "--" }
        let step = (hi - lo) / n
        return "≈ \(Fmt.amount(step / hi * 100, 2))% ~ \(Fmt.amount(step / lo * 100, 2))%"
    }
    private var req: APIGridCreateRequest {
        APIGridCreateRequest(symbol: symbol, mode: mode == 0 ? .arithmetic : .geometric, lowerPrice: lower, upperPrice: upper,
                             gridCount: Int(grids) ?? 0, investment: invest, stopLossPrice: sl.isEmpty ? nil : sl,
                             takeProfitPrice: tp.isEmpty ? nil : tp, sellBaseOnStop: nil)
    }
    private var error: String? {
        let lo = Dec.parse(lower), hi = Dec.parse(upper), n = Int(grids) ?? 0
        if lower.isEmpty || upper.isEmpty || invest.isEmpty { return nil }
        if lo <= 0 || hi <= lo { return "最高价需大于最低价" }
        if n < 2 || n > 200 { return "网格数量 2-200" }
        if session.isLoggedIn, Dec.parse(invest) > avail { return "可用 USDT 不足" }
        if !sl.isEmpty, Dec.parse(sl) >= lo { return "止损价需低于最低价" }
        if !tp.isEmpty, Dec.parse(tp) <= hi { return "止盈价需高于最高价" }
        return nil
    }

    var body: some View {
        FeatureShell(title: "创建现货网格", onBack: { onDone(false) }) { VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Button { pairs = true } label: {
                        HStack(spacing: 6) {
                            Text(symbol).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                            Image(systemName: "chevron.down").font(.system(size: 11)).foregroundColor(p.textSecondary)
                            Spacer()
                            Text(price > 0 ? Fmt.priceAuto(price) : "--").font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                        }
                    }
                    HStack {
                        PillSegment(items: ["等差", "等比"], selected: $mode, height: 34)
                        Button("AI 参数") { aiFill() }.font(.lb(FontSize.caption, .medium)).foregroundColor(p.brand).padding(.leading, 8)
                    }
                    HStack(spacing: 8) {
                        NumberBox(label: "最低价", unit: "USDT", text: $lower, onEdit: {})
                        NumberBox(label: "最高价", unit: "USDT", text: $upper, onEdit: {})
                    }
                    NumberBox(label: "网格数量 (2-200)", unit: "格", text: $grids, onEdit: {})
                    NumberBox(label: "投入金额", unit: "USDT", text: $invest, onEdit: {})
                    HStack {
                        Text("可用 \(Fmt.amount(avail, 2)) USDT").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        Spacer()
                        Button("全部") { invest = Dec.plain(avail, 2) }.font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                    }
                    Button { withAnimation { advanced.toggle() } } label: {
                        HStack { Text("高级设置（止盈/止损）"); Image(systemName: advanced ? "chevron.up" : "chevron.down") }
                            .font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    }
                    if advanced {
                        HStack(spacing: 8) {
                            NumberBox(label: "止盈价", unit: "USDT", text: $tp, onEdit: {})
                            NumberBox(label: "止损价", unit: "USDT", text: $sl, onEdit: {})
                        }
                    }
                    if let e = error ?? previewErr { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    if let pv = preview {
                        VStack(spacing: 8) {
                            KVRow(k: "每格利润率", v: "\(Fmt.amount(Dec.parse(pv.profitPerGridMin) * 100, 2))% ~ \(Fmt.amount(Dec.parse(pv.profitPerGridMax) * 100, 2))%", vColor: p.up)
                            KVRow(k: "每格数量", v: pv.qtyPerGrid ?? "--")
                            KVRow(k: "买单 / 卖单", v: "\(pv.buyOrders ?? 0) / \(pv.sellOrders ?? 0)")
                            KVRow(k: "初始买入", v: "\(pv.initialBuyQuote ?? "0") USDT")
                        }.padding(12).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.top, 8)
            }
            Group { if session.isLoggedIn {
                LBButton(title: "创建", enabled: error == nil && !lower.isEmpty && !upper.isEmpty && Dec.parse(invest) > 0, loading: busy) {
                    Task { await doPreview(thenConfirm: true) }
                }
            } else {
                LBButton(title: "登录后创建") { session.requireLogin {} }
            } }.padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
        } }
        .task {
            if store.spot.isEmpty { await store.load() }
            if let c = copy { symbol = c.symbol ?? symbol; lower = c.lowerPrice ?? ""; upper = c.upperPrice ?? ""; grids = "\(c.gridCount ?? 20)"; mode = c.mode == "geometric" ? 1 : 0 }
            else { aiFill() }
            if session.isLoggedIn { avail = (try? await AssetsAPI.spotBalance("USDT")) ?? 0 }
            if price == 0 { await refreshRefPrice() }
            if lower.isEmpty || upper.isEmpty { aiFill() }
            if Router.shared.shotSheet == "gridConfirm" {
                Router.shared.shotSheet = nil; invest = "100"
                if session.isLoggedIn { await doPreview(thenConfirm: true) } else { confirm = true }
            }
        }
        .onChange(of: lower) { _ in preview = nil }
        .onChange(of: upper) { _ in preview = nil }
        .onChange(of: grids) { _ in preview = nil }
        .lbSheet($pairs) {
            SwapAssetSheet(current: String(symbol.dropLast(4))) { b in symbol = b + "USDT"; pairs = false; refPrice = 0; Task { if price == 0 { await refreshRefPrice() }; aiFill() } } onClose: { pairs = false }
        }
        .lbSheet($confirm) {
            LBSheet(title: "确认创建", subtitle: symbol, showClose: true, onClose: { confirm = false }) {
                VStack(spacing: 10) {
                    KVRow(k: "价格区间", v: "\(lower) - \(upper)")
                    KVRow(k: "网格", v: "\(grids) 格 · \(mode == 0 ? "等差" : "等比")")
                    KVRow(k: "投入", v: "\(invest) USDT")
                    if !tp.isEmpty { KVRow(k: "止盈价", v: tp) }
                    if !sl.isEmpty { KVRow(k: "止损价", v: sl) }
                    KVRow(k: "当前价格", v: price > 0 ? Fmt.priceAuto(price) : "--")
                    KVRow(k: "每格利润率", v: preview.map { "\(Fmt.amount(Dec.parse($0.profitPerGridMin) * 100, 2))% ~ \(Fmt.amount(Dec.parse($0.profitPerGridMax) * 100, 2))%" } ?? estProfit, vColor: p.up)
                    if session.isLoggedIn {
                        LBButton(title: "确认创建", loading: busy) { Task { await submit() } }.padding(.top, 6)
                    } else {
                        LBButton(title: "登录后创建") { confirm = false; session.requireLogin {} }.padding(.top, 6)
                    }
                }.padding(.horizontal, Spacing.pageH)
            }
        }
    }

    private func aiFill() {
        guard price > 0 else { return }
        let prec = max(store.meta(symbol)?.pricePrecision ?? 2, 0)
        lower = Dec.plain(price * Decimal(string: "0.9")!, prec)
        upper = Dec.plain(price * Decimal(string: "1.1")!, prec)
        grids = "20"; preview = nil
    }
    private func doPreview(thenConfirm: Bool) async {
        busy = true; defer { busy = false }
        do { preview = try await GridAPI.preview(req); previewErr = nil; if thenConfirm { confirm = true } }
        catch { previewErr = error.lbMessage }
    }
    private func submit() async {
        busy = true; defer { busy = false }
        do {
            _ = try await GridAPI.create(req)
            ToastCenter.shared.show("网格已创建", .success)
            confirm = false; onDone(true)
        } catch { ToastCenter.shared.error(error) }
    }
}
