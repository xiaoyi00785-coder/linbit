import SwiftUI

/// 现货条件单（止损 stop / 止盈 take_profit / 计划委托 trigger）与手续费率接口（mobile/openapi.yaml）
extension SpotAPI {
    typealias CondList = APIListConditionalOrders200Response
    static func createConditional(_ r: APIConditionalOrderRequest) async throws -> APIConditionalOrder {
        try await api.post("/spot/orders/conditional", body: r)
    }
    /// status：open（默认，pending/triggering）、all 或具体状态
    static func conditional(symbol: String? = nil, status: String = "open", page: Int = 1, pageSize: Int = 20) async throws -> CondList {
        try await api.get("/spot/orders/conditional", query: ["symbol": symbol, "status": status, "page": "\(page)", "pageSize": "\(pageSize)"], auth: true)
    }
    static func cancelConditional(_ id: String) async throws -> APIConditionalOrder {
        try await api.delete("/spot/orders/conditional/\(id)")
    }
    /// 我的费率（含个人覆盖）
    static func feeRates(symbol: String?) async throws -> APIFeeRates {
        try await api.get("/account/fee-rates", query: ["symbol": symbol], auth: true)
    }
    /// 平台默认费率（公开）
    static func feeSchedule() async throws -> APIGetFeeSchedule200Response {
        try await api.get("/spot/fee-schedule")
    }
}

extension APIConditionalOrder: Identifiable {}

enum SpotCond {
    static func kindText(_ k: APIConditionalOrder.APIKind?) -> String {
        switch k { case .stop: return "止损"; case .takeProfit: return "止盈"; default: return "计划委托" }
    }
    static func statusText(_ s: APIConditionalOrder.APIStatus?) -> String {
        switch s {
        case .pending: return "等待触发"
        case .triggering: return "触发中"
        case .triggered: return "已触发"
        case .canceled: return "已撤销"
        case .failed: return "触发失败"
        case nil: return "--"
        }
    }
    /// 费率显示：0.001 → 0.1%
    static func pct(_ d: Decimal?) -> String {
        guard let d else { return "--" }
        return Dec.plain(d * 100, 4).replacingOccurrences(of: #"\.?0+$"#, with: "", options: .regularExpression) + "%"
    }
}

/// 条件单行
struct ConditionalOrderRow: View {
    @Environment(\.palette) private var p
    let o: APIConditionalOrder
    var onCancel: (() -> Void)? = nil
    var body: some View {
        let buy = o.side == "buy"
        let open = o.status == .pending
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(buy ? "买入" : "卖出").font(.lb(FontSize.small, .medium)).foregroundColor(buy ? p.up : p.down)
                    .padding(.horizontal, 4).padding(.vertical, 1).background(RoundedRectangle(cornerRadius: 3).fill(buy ? p.upTint : p.downTint))
                Text((o.symbol ?? "").replacingOccurrences(of: "USDT", with: "/USDT")).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                Text(SpotCond.kindText(o.kind)).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Spacer()
                if let onCancel, open {
                    Button(action: onCancel) {
                        Text("撤单").font(.lb(FontSize.small)).foregroundColor(p.textPrimary).padding(.horizontal, 12).frame(height: 24)
                            .background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.fill))
                    }.buttonStyle(PressableStyle())
                } else {
                    Text(SpotCond.statusText(o.status)).font(.lb(FontSize.small)).foregroundColor(o.status == .failed ? p.down : p.textSecondary)
                }
            }
            if let d = o.createdAt { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
            HStack {
                kv("触发价", "\(o.triggerDirection == .down ? "≤" : "≥") \(Fmt.priceAuto(Dec.parse(o.triggerPrice)))")
                kv("委托价", o.type == .market ? "市价" : Fmt.priceAuto(Dec.parse(o.price)))
                kv(o.quantity != nil ? "数量" : "金额", o.quantity ?? o.quoteOrderQty.map { "\($0) USDT" } ?? "--", end: true)
            }
            if let fa = o.frozenAmount, Dec.parse(fa) > 0, open {
                Text("冻结 \(fa) \(o.frozenAsset ?? "")").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            }
            if let r = o.failReason, !r.isEmpty {
                Text("失败原因：\(ErrorText.map[r] ?? r)").font(.lb(FontSize.small)).foregroundColor(p.down)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    private func kv(_ k: String, _ v: String, end: Bool = false) -> some View {
        VStack(alignment: end ? .trailing : .leading, spacing: 3) {
            Text(k).font(.lb(11)).foregroundColor(p.textSecondary)
            Text(v).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
        }.frame(maxWidth: .infinity, alignment: end ? .trailing : .leading)
    }
}

/// 历史页「条件单」：状态筛选 + 分页 + 撤单（加载 / 空 / 错误）
struct SpotConditionalList: View {
    @Environment(\.palette) private var p
    let symbol: String?
    @State private var status = "all"
    @State private var items: [APIConditionalOrder] = []
    @State private var state: Loadable<Void> = .loading
    @State private var page = 1
    @State private var hasMore = false
    @State private var loadingMore = false
    @State private var confirm: ConfirmRequest?
    private let statuses: [(String, String)] = [("all", "全部"), ("open", "未触发"), ("triggered", "已触发"), ("canceled", "已撤销"), ("failed", "失败")]

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(statuses, id: \.0) { s in
                        Button { status = s.0 } label: {
                            Text(s.1).font(.lb(FontSize.small, status == s.0 ? .medium : .regular))
                                .foregroundColor(status == s.0 ? p.textPrimary : p.textSecondary)
                                .padding(.horizontal, 10).frame(height: 26)
                                .background(RoundedRectangle(cornerRadius: Radius.xs).fill(status == s.0 ? p.fillStrong : p.fill))
                        }
                    }
                }.padding(.horizontal, Spacing.pageH)
            }.padding(.bottom, 8)
            switch state {
            case .idle, .loading: SkeletonList().padding(.horizontal, Spacing.pageH); Spacer()
            case .failed(let e): ErrorStateView(message: e) { Task { await reload() } }.frame(maxHeight: .infinity)
            case .loaded:
                if items.isEmpty {
                    VStack(spacing: 10) { EmptyIllustration(); Text("暂无条件单").font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                        .frame(maxHeight: .infinity).offset(y: -60)
                } else {
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(items) { o in
                                ConditionalOrderRow(o: o) {
                                    confirm = .init(title: "撤销条件单", message: "撤销后将释放冻结资金，确定撤销吗？", confirmTitle: "撤销", destructive: true) {
                                        Task {
                                            do { _ = try await SpotAPI.cancelConditional(o.id ?? ""); ToastCenter.shared.show("已撤销", .success); await reload() }
                                            catch { ToastCenter.shared.error(error) }
                                        }
                                    }
                                }
                                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                            }
                            Group {
                                if hasMore { ProgressView().onAppear { Task { await more() } } }
                                else { Text("没有更多了").font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                            }.frame(height: 44)
                        }
                    }.refreshable { await reload() }
                }
            }
        }
        .task(id: "\(symbol ?? "")-\(status)") { await reload() }
        .lbConfirm($confirm)
    }

    private func reload() async {
        state = .loading; page = 1
        guard TokenStore.shared.isLoggedIn else { items = []; hasMore = false; state = .loaded(()); return }
        do {
            let r = try await SpotAPI.conditional(symbol: symbol, status: status, page: 1)
            items = r.items ?? []; hasMore = (r.page ?? 1) * (r.pageSize ?? 20) < (r.total ?? 0)
            state = .loaded(())
        } catch { state = .failed(error.lbMessage) }
    }
    private func more() async {
        guard !loadingMore, hasMore else { return }
        loadingMore = true; defer { loadingMore = false }
        do {
            let r = try await SpotAPI.conditional(symbol: symbol, status: status, page: page + 1)
            page += 1; items += r.items ?? []; hasMore = (r.page ?? page) * (r.pageSize ?? 20) < (r.total ?? 0)
        } catch { ToastCenter.shared.error(error); hasMore = false }
    }
}

/// 手续费率弹窗：我的费率（登录）/ 平台默认费率
struct SpotFeeSheet: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: SpotModel
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "手续费率", subtitle: "\(m.base)/\(m.quote)", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                switch m.feeState {
                case .idle, .loading: SkeletonList(rows: 2)
                case .failed(let e): ErrorStateView(message: e) { Task { await m.loadFees() } }
                case .loaded:
                    row("现货 Maker（挂单）", SpotCond.pct(m.makerRate))
                    row("现货 Taker（吃单）", SpotCond.pct(m.takerRate))
                    if let f = m.futuresFee {
                        row("合约 Maker", SpotCond.pct(f.maker.map(Dec.parse)))
                        row("合约 Taker", SpotCond.pct(f.taker.map(Dec.parse)))
                    }
                    Text(m.feeOverridden ? "已适用个人专属费率" : (TokenStore.shared.isLoggedIn ? "当前为平台标准费率" : "登录后显示您的专属费率"))
                        .font(.lb(FontSize.small)).foregroundColor(m.feeOverridden ? p.up : p.textTertiary)
                    if let n = m.feeNote, !n.isEmpty { Text(n).font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 20)
        }
        .task { if m.feeState.value == nil { await m.loadFees() } }
    }
    private func row(_ k: String, _ v: String) -> some View {
        HStack {
            Text(k).font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v).font(.lbNum(FontSize.label, .medium)).foregroundColor(p.textPrimary)
        }
    }
}
