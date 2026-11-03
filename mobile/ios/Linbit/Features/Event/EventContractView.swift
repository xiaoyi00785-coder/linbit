import SwiftUI

/// 事件合约（无内页对照图，沿用合约页视觉：灰底卡片 / 绿涨红跌 / 底部弹窗确认）
/// 猜指定周期结束时指数价格相对开仓价的涨跌；猜中按 payoutRatio 赔付，平局退还本金。
@MainActor
final class EventModel: ObservableObject {
    @Published var state: Loadable<EventProducts> = .idle
    @Published var disabled = false
    @Published var symbol = "BTCUSDT"
    @Published var period = ""
    @Published var stakeText = ""
    @Published var active: [EventOrder] = []
    @Published var history: [EventOrder] = []
    @Published var historyCursor: String?
    @Published var dailyUsed: Decimal = 0
    @Published var placing = false
    @Published var now = Date()
    @Published var priceTrail: [Double] = []
    private var loop: Task<Void, Never>?
    private var sub: WSSubscription?

    var cfg: EventProducts? { state.value }
    var product: EventProductItem? { cfg?.products?.first { $0.symbol == symbol } }
    /// 产品指数价缺失/为 0（如未登录拿不到产品配置）时的参考价：合约 标记价 → 指数价 → 现货价
    @Published var refIndex: Decimal = 0
    var index: Decimal { let i = Dec.parse(product?.indexPrice); return i > 0 ? i : refIndex }
    var pairTitle: String { let b = product?.baseAsset ?? String(symbol.dropLast(4)); return "\(b)/\(symbol.hasSuffix("USDT") ? "USDT" : "USD")" }
    var sheetSubtitle: String { [pairTitle, periodTitle(period)].filter { !$0.isEmpty && $0 != "-" }.joined(separator: " · ") }
    func loadRefIndex() async {
        guard Dec.parse(product?.indexPrice) <= 0 else { return }
        let spot = MarketStore.shared.spot.first { $0.symbol == symbol }?.last ?? 0
        if let t = try? await FuturesAPI.ticker(symbol) { refIndex = t.effectivePrice(spot: spot) } else { refIndex = spot }
    }
    var ratio: Decimal { Dec.parse(cfg?.payoutRatio ?? "0.8") }
    var stake: Decimal { Dec.parse(stakeText) }
    var minStake: Decimal { Dec.parse(cfg?.minStake) }
    var maxStake: Decimal { Dec.parse(cfg?.maxStake) }
    var dailyLimit: Decimal { Dec.parse(cfg?.dailyLimit) }
    var asset: String { cfg?.stakeAsset ?? "USDT" }

    func start() async {
        await load()
        await refreshOrders()
        sub = MarketSocket.shared.subscribe("event:orders") { [weak self] f in
            guard let o = try? CodableHelper.jsonDecoder.decode(EventOrder.self, from: f.data) else { return }
            Task { @MainActor in self?.apply(o) }
        }
        loop?.cancel()
        loop = Task { [weak self] in
            var n = 0
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                guard let self else { return }
                self.now = Date(); n += 1
                if n % 2 == 0 { await self.load(silent: true) }
                if n % 5 == 0, self.active.contains(where: { $0.isActive }) { await self.refreshOrders() }
            }
        }
    }
    func stop() { loop?.cancel(); loop = nil; sub?.cancel(); sub = nil }

    func load(silent: Bool = false) async {
        if !silent, state.value == nil { state = .loading }
        do {
            let r = try await EventAPI.products()
            disabled = r.enabled == false
            state = .loaded(r)
            if period.isEmpty { period = r.periods?.first?.period ?? "" }
            if r.products?.contains(where: { $0.symbol == symbol }) == false, let f = r.products?.first { symbol = f.symbol }
            if stakeText.isEmpty, let m = r.minStake { stakeText = m }
            await loadRefIndex()
            let v = Dec.double(index)
            if v > 0 { priceTrail.append(v); if priceTrail.count > 60 { priceTrail.removeFirst() } }
        } catch {
            if (error as? APIFailure)?.code == "FEATURE_DISABLED" { disabled = true; state = .loaded(EventProducts(enabled: false, stakeAsset: nil, payoutRatio: nil, minStake: nil, maxStake: nil, dailyLimit: nil, tieRule: nil, periods: nil, products: nil)); return }
            if !silent || state.value == nil { state = .failed(error.lbMessage) }
            await loadRefIndex()
        }
    }

    func refreshOrders() async {
        guard TokenStore.shared.isLoggedIn else { active = []; history = []; return }
        if let a = try? await EventAPI.orders(status: "open", before: nil, limit: 50) {
            active = (a.orders ?? []).filter { $0.isActive }
            dailyUsed = Dec.parse(a.dailyUsed)
        }
        if let h = try? await EventAPI.orders(status: "settled", before: nil) {
            history = h.orders ?? []; historyCursor = h.nextBefore
        }
    }
    func moreHistory() async {
        guard let c = historyCursor, let h = try? await EventAPI.orders(status: "settled", before: c) else { return }
        history += h.orders ?? []; historyCursor = h.nextBefore
    }

    private func apply(_ o: EventOrder) {
        if o.isActive {
            if let i = active.firstIndex(where: { $0.id == o.id }) { active[i] = o } else { active.insert(o, at: 0) }
        } else {
            active.removeAll { $0.id == o.id }
            history.removeAll { $0.id == o.id }
            history.insert(o, at: 0)
            if o.status == "settled" {
                let r = ["won": "猜中，获得 \(o.payout ?? "") \(asset)", "lost": "未猜中", "tie": "平局，本金已退还", "refunded": "已退款"][o.result ?? ""] ?? "已结算"
                ToastCenter.shared.show("\(o.symbol) \(r)", o.result == "won" ? .success : .info)
            }
        }
    }

    var validation: String? {
        if stake <= 0 { return "请输入投注金额" }
        if minStake > 0, stake < minStake { return "最小投注 \(cfg?.minStake ?? "") \(asset)" }
        if maxStake > 0, stake > maxStake { return "最大投注 \(cfg?.maxStake ?? "") \(asset)" }
        if dailyLimit > 0, dailyUsed + stake > dailyLimit { return "超过今日投注上限 \(cfg?.dailyLimit ?? "")" }
        if product?.paused == true { return "该产品暂停交易" }
        if product?.priceAvailable == false { return "指数价格暂不可用" }
        return nil
    }

    func place(up: Bool) async -> Bool {
        placing = true; defer { placing = false }
        do {
            let o = try await EventAPI.place(.init(symbol: symbol, period: period, direction: up ? "up" : "down",
                                                   stake: Dec.plain(stake, 2), clientId: "ios-\(UUID().uuidString)"))
            if o.status == "rejected" { ToastCenter.shared.show(o.rejectReason ?? "下单被拒绝", .error); return false }
            apply(o); dailyUsed += stake
            ToastCenter.shared.show("下单成功，\(periodTitle(period))后结算", .success)
            return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    func periodTitle(_ p: String) -> String {
        guard let s = cfg?.periods?.first(where: { $0.period == p })?.seconds else { return p }
        return s >= 3600 ? "\(s / 3600)小时" : s >= 60 ? "\(s / 60)分钟" : "\(s)秒"
    }
    func remaining(_ o: EventOrder) -> Int { max(0, Int(Double(o.expiryTime ?? 0) / 1000 - now.timeIntervalSince1970)) }
    func progress(_ o: EventOrder) -> Double {
        let total = Double((o.expiryTime ?? 0) - (o.openTime ?? o.createdAt ?? 0)) / 1000
        return total > 0 ? min(1, 1 - Double(remaining(o)) / total) : 1
    }
}

struct EventContractView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @StateObject private var m = EventModel()
    @State private var tab = 0
    @State private var pending: Bool?   // 待确认方向 true 涨 / false 跌
    @State private var showRules = false

    var body: some View {
        Group {
            switch m.state {
            case .idle, .loading: ScrollView { SkeletonList(rows: 6) }
            case let .failed(msg): ErrorStateView(message: "事件合约加载失败：\(msg)") { Task { await m.load() } }
            case .loaded:
                if m.disabled { ComingSoonView(title: "事件合约", note: "猜涨跌，到期按指数价结算，敬请期待") } else { content }
            }
        }
        .background(p.bg)
        .task { await m.start(); if Router.shared.shotSheet == "eventConfirm" { Router.shared.shotSheet = nil; pending = true } }
        .onDisappear { m.stop() }
        .onChange(of: session.isLoggedIn) { _ in Task { await m.refreshOrders() } }
        .lbSheet(Binding(get: { pending != nil }, set: { if !$0 { pending = nil } })) {
            if let up = pending { EventConfirmSheet(m: m, up: up) { pending = nil } }
        }
        .lbSheet($showRules) { EventRulesSheet(m: m) { showRules = false } }
    }

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                productBar
                priceCard.padding(.top, 12)
                periodBar.padding(.top, 16)
                stakeBox.padding(.top, 16)
                buttons.padding(.top, 18)
                Rectangle().fill(p.fill).frame(height: 8).padding(.top, 20)
                ordersSection
            }
            .padding(.bottom, Metrics.tabBarH + 60)
        }
        .refreshable { await m.load(); await m.refreshOrders() }
    }

    private var productBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(m.cfg?.products ?? []) { pr in
                    let on = pr.symbol == m.symbol
                    Button { m.symbol = pr.symbol; m.priceTrail = [] } label: {
                        HStack(spacing: 4) {
                            CoinIcon(base: pr.baseAsset ?? String(pr.symbol.dropLast(4)), size: 16)
                            Text("\(pr.baseAsset ?? pr.symbol)").font(.lb(FontSize.caption, on ? .semibold : .regular))
                            if pr.paused == true { Text("暂停").font(.lb(10)).foregroundColor(p.textTertiary) }
                        }
                        .foregroundColor(on ? p.textPrimary : p.textSecondary)
                        .padding(.horizontal, 10).frame(height: 28)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(on ? p.fillStrong : p.fill))
                    }.buttonStyle(PressableStyle())
                }
                Spacer(minLength: 0)
                Button { showRules = true } label: {
                    HStack(spacing: 3) { Image(systemName: "questionmark.circle"); Text("规则") }
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }.padding(.horizontal, Spacing.pageH)
        }
    }

    private var priceCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("\(m.symbol) 指数价格").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                Text("赔率 \(Fmt.amount(m.ratio * 100, 0))%").font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
            }
            Text(m.product?.priceAvailable == false ? "--" : Fmt.priceAuto(m.index)).font(.lbNum(28, .bold)).foregroundColor(p.textPrimary)
            Sparkline(values: m.priceTrail.count > 1 ? m.priceTrail : [0, 0], color: (m.priceTrail.last ?? 0) >= (m.priceTrail.first ?? 0) ? p.up : p.down)
                .frame(height: 64)
            if m.product?.priceAvailable == false { InlineBanner(text: "指数价格暂不可用，暂停下单") }
        }
        .padding(14)
        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
        .padding(.horizontal, Spacing.pageH)
    }

    private var periodBar: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("结算周期").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
            HStack(spacing: 8) {
                ForEach(m.cfg?.periods ?? [], id: \.period) { pd in
                    let on = pd.period == m.period
                    Text(m.periodTitle(pd.period)).font(.lb(FontSize.caption, on ? .semibold : .regular))
                        .foregroundColor(on ? p.textPrimary : p.textSecondary)
                        .frame(maxWidth: .infinity).frame(height: 34)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).stroke(on ? p.textPrimary : p.border, lineWidth: on ? 1.2 : 1))
                        .contentShape(Rectangle())
                        .onTapGesture { Haptics.tap(); m.period = pd.period }
                }
            }
        }.padding(.horizontal, Spacing.pageH)
    }

    private var stakeBox: some View {
        VStack(alignment: .leading, spacing: 8) {
            NumberBox(label: "投注金额", unit: m.asset, text: $m.stakeText, onEdit: {})
            HStack(spacing: 8) {
                ForEach(quick, id: \.self) { v in
                    Text(v).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                        .frame(maxWidth: .infinity).frame(height: 26).background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                        .onTapGesture { m.stakeText = v }
                }
            }
            KVRow(k: "范围", v: "\(m.cfg?.minStake ?? "--") – \(m.cfg?.maxStake ?? "--") \(m.asset)")
            KVRow(k: "今日已用 / 上限", v: "\(Fmt.amount(m.dailyUsed, 2)) / \(m.cfg?.dailyLimit ?? "--")")
            KVRow(k: "猜中预计收益", v: "+\(Fmt.amount(m.stake * m.ratio, 2)) \(m.asset)", vColor: p.up)
        }.padding(.horizontal, Spacing.pageH)
    }
    private var quick: [String] {
        let mn = m.minStake > 0 ? m.minStake : 10
        return [mn, mn * 5, mn * 10, m.maxStake > 0 ? m.maxStake : mn * 50].map { Dec.plain($0, 2) }
    }

    private var buttons: some View {
        HStack(spacing: 12) {
            dirButton(up: true)
            dirButton(up: false)
        }.padding(.horizontal, Spacing.pageH)
    }
    private func dirButton(up: Bool) -> some View {
        LBButton(title: session.isLoggedIn ? (up ? "看涨" : "看跌") : "登录 / 注册", kind: session.isLoggedIn ? (up ? .buy : .sell) : .primary,
                 height: 48, loading: m.placing, subtitle: session.isLoggedIn ? "\(m.periodTitle(m.period))后 \(up ? "高于" : "低于")开仓价" : nil) {
            session.requireLogin {
                if let e = m.validation { ToastCenter.shared.show(e, .error); return }
                pending = up
            }
        }
    }

    private var ordersSection: some View {
        VStack(spacing: 0) {
            HStack(spacing: 18) {
                ForEach(["进行中(\(m.active.count))", "已结算"].indices, id: \.self) { i in
                    Text(["进行中(\(m.active.count))", "已结算"][i]).font(.lb(FontSize.label, tab == i ? .semibold : .regular))
                        .foregroundColor(tab == i ? p.textPrimary : p.textSecondary).onTapGesture { tab = i }
                }
                Spacer()
            }.padding(.horizontal, Spacing.pageH).frame(height: 44)
            let list = tab == 0 ? m.active : m.history
            if !session.isLoggedIn {
                EmptyBlock(text: "登录后查看订单", actions: [("登录 / 注册", { session.present(.login) })])
            } else if list.isEmpty {
                EmptyBlock(text: tab == 0 ? "暂无进行中的订单" : "暂无结算记录", actions: [])
            } else {
                LazyVStack(spacing: 0) {
                    ForEach(list) { o in
                        EventOrderRow(m: m, o: o)
                        Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                    }
                    if tab == 1, m.historyCursor != nil {
                        Button("加载更多") { Task { await m.moreHistory() } }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary).padding(.vertical, 14)
                    }
                }
            }
        }
    }
}

struct EventOrderRow: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: EventModel
    let o: EventOrder
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(o.isUp ? "看涨" : "看跌").font(.lb(FontSize.small, .semibold)).foregroundColor(.white)
                    .padding(.horizontal, 5).frame(height: 18).background(RoundedRectangle(cornerRadius: 3).fill(o.isUp ? p.up : p.down))
                Text(o.symbol).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(m.periodTitle(o.period)).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                statusBadge
            }
            HStack {
                KVCell(k: "投注(\(m.asset))", v: o.stake)
                KVCell(k: "开仓价", v: o.openPrice.map { Fmt.priceAuto(Dec.parse($0)) } ?? "--", align: .center)
                if o.isActive {
                    KVCell(k: "预计收益", v: "+\(o.expectedPayout ?? Fmt.amount(Dec.parse(o.stake) * m.ratio, 2))", align: .trailing, vColor: p.up)
                } else {
                    KVCell(k: "盈亏", v: pnlText, align: .trailing, vColor: Dec.parse(o.pnl) < 0 ? p.down : p.up)
                }
            }
            if o.isActive {
                VStack(spacing: 4) {
                    ProgressView(value: m.progress(o)).tint(o.isUp ? p.up : p.down)
                    HStack {
                        Text(o.status == "settling" ? "结算中…" : "剩余 \(m.remaining(o) / 60):\(String(format: "%02d", m.remaining(o) % 60))")
                        Spacer()
                        Text("当前 \(Fmt.priceAuto(m.cfg?.products?.first { $0.symbol == o.symbol }.map { Dec.parse($0.indexPrice) } ?? 0))")
                    }.font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                }
            } else if let sp = o.settlePrice {
                Text("结算价 \(Fmt.priceAuto(Dec.parse(sp)))\(o.settledAt.map { " · " + Fmt.dateTime(Date(timeIntervalSince1970: Double($0) / 1000)) } ?? "")")
                    .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            } else if let r = o.rejectReason {
                Text(r).font(.lb(FontSize.small)).foregroundColor(p.down)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private var pnlText: String {
        let v = Dec.parse(o.pnl)
        return (v > 0 ? "+" : "") + Fmt.amount(v, 2)
    }
    private var statusBadge: some View {
        let (t, c): (String, Color) = {
            switch o.status {
            case "pending": return ("待开仓", p.textSecondary)
            case "open": return ("进行中", p.brand)
            case "settling": return ("结算中", p.brand)
            case "rejected": return ("已拒绝", p.down)
            default:
                switch o.result {
                case "won": return ("猜中", p.up)
                case "lost": return ("未猜中", p.down)
                case "tie": return ("平局退还", p.textSecondary)
                default: return ("已退款", p.textSecondary)
                }
            }
        }()
        return Text(t).font(.lb(FontSize.small, .medium)).foregroundColor(c)
    }
}

struct EventConfirmSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: EventModel
    let up: Bool
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "确认\(up ? "看涨" : "看跌")", subtitle: m.sheetSubtitle, showClose: true, onClose: onClose) {
            VStack(spacing: 10) {
                KVRow(k: "当前指数价", v: Fmt.priceAuto(m.index))
                KVRow(k: "方向", v: up ? "看涨（到期价 > 开仓价）" : "看跌（到期价 < 开仓价）", vColor: up ? p.up : p.down)
                KVRow(k: "投注金额", v: "\(Fmt.amount(m.stake, 2)) \(m.asset)")
                KVRow(k: "猜中获得", v: "\(Fmt.amount(m.stake * (1 + m.ratio), 2)) \(m.asset)", vColor: p.up)
                KVRow(k: "未猜中", v: "损失全部投注")
                KVRow(k: "平局", v: "退还本金")
                Text("开仓价以服务端成交时刻指数价为准，到期按指数价自动结算。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                LBButton(title: "确认\(up ? "看涨" : "看跌")", kind: up ? .buy : .sell, loading: m.placing) {
                    Task { if await m.place(up: up) { onClose() } }
                }.padding(.top, 6)
            }.padding(.horizontal, Spacing.pageH)
        }
    }
}

struct EventRulesSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: EventModel
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "事件合约规则", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(rules, id: \.self) { r in
                    HStack(alignment: .top, spacing: 6) {
                        Text("•").foregroundColor(p.textSecondary)
                        Text(r).foregroundColor(p.textSecondary)
                    }.font(.lb(FontSize.caption))
                }
                LBButton(title: "知道了", action: onClose).padding(.top, 8)
            }.padding(.horizontal, Spacing.pageH)
        }
    }
    private var rules: [String] {
        ["选择产品与结算周期，预测到期时指数价格相对开仓价的涨跌。",
         "猜中获得投注金额 × \(Fmt.amount(m.ratio * 100, 0))% 的收益并返还本金；未猜中损失投注金额。",
         "到期价格与开仓价相同视为平局，退还本金。",
         "单笔投注 \(m.cfg?.minStake ?? "--") – \(m.cfg?.maxStake ?? "--") \(m.asset)，每日累计上限 \(m.cfg?.dailyLimit ?? "--") \(m.asset)。",
         "指数价格不可用或产品暂停时无法下单；已开仓订单按规则退款。"]
    }
}
