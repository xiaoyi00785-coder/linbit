import SwiftUI

/// 理财：总览（持仓/收益）+ 产品列表（活期/定期）+ 申购/赎回弹窗
@MainActor
final class EarnModel: ObservableObject {
    @Published var products: Loadable<[APIEarnProduct]> = .idle
    @Published var summary: APIEarnSummary200Response?
    @Published var positions: APIEarnPositions200Response?
    func load() async {
        if products.value == nil { products = .loading }
        do { products = .loaded(try await EarnAPI.products()) } catch { if products.value == nil { products = .failed(error.lbMessage) } }
        await loadMine()
    }
    func loadMine() async {
        guard TokenStore.shared.isLoggedIn else { summary = nil; positions = nil; return }
        summary = try? await EarnAPI.summary()
        positions = try? await EarnAPI.positions()
    }
    func held(_ id: String?) -> APIEarnPositions200ResponseItemsInner? { positions?.items?.first { $0.productId == id } }
}

struct EarnView: View {
    var embedded = false
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @StateObject private var m = EarnModel()
    @State private var tab = 0          // 0 全部 1 简单赚币 2 定期 3 链上赚币 4 VIP 5 质押借币 6 我的
    @State private var subscribe: APIEarnProduct?
    @State private var redeem: APIEarnPositions200ResponseItemsInner?
    @State private var hide = false
    @State private var search = ""
    @State private var mineOpen = false

    var body: some View {
        Group {
            if embedded { earnBody } else { FeatureShell(title: "理财") { earnBody } }
        }
        .task {
            await m.load()
            if Router.shared.shotSheet == "earnSubscribe", let f = m.products.value?.first { Router.shared.shotSheet = nil; subscribe = f }
        }
        .onChange(of: session.isLoggedIn) { _ in Task { await m.loadMine() } }
        .lbSheet(Binding(get: { subscribe != nil }, set: { if !$0 { subscribe = nil } })) {
            if let pr = subscribe { EarnSubscribeSheet(product: pr) { done in subscribe = nil; if done { Task { await m.loadMine() } } } }
        }
        .lbSheet(Binding(get: { redeem != nil }, set: { if !$0 { redeem = nil } })) {
            if let h = redeem { EarnRedeemSheet(pos: h) { done in redeem = nil; if done { Task { await m.loadMine() } } } }
        }
    }

    private var earnBody: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                overview
                entries.padding(.top, 26)
                if !recommendations.isEmpty {
                    sectionTitle("专属推荐").padding(.top, 30)
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 10) {
                            ForEach(recommendations, id: \.id) { pr in
                                recommendCard(pr)
                            }
                        }.padding(.horizontal, Spacing.pageH)
                    }
                    .padding(.top, 14)
                }
                sectionTitle("全部产品").padding(.top, 30)
                searchField.padding(.top, 14)
                if tab == 6 { mineBody.padding(.top, 8) } else { list }
            }.padding(.bottom, 40)
        }
        .refreshable { await m.load() }
        .lbSheet(Binding(get: { mineOpen }, set: { if !$0 { mineOpen = false } })) {
            LBSheet(title: "我的持仓", showClose: true, onClose: { mineOpen = false }) {
                mineBody
            }
        }
    }

    /// 参考 App 的四个入口：简单赚币 / 链上赚币 / VIP 尊享财富 / 质押借币（点选即筛选下面的列表）
    private var entries: some View {
        HStack(spacing: 0) {
            ForEach(Array(["简单赚币", "链上赚币", "VIP 尊享财富", "质押借币"].enumerated()), id: \.offset) { i, t in
                let on = tab == i + 1
                Button { tab = on ? 0 : i + 1 } label: {
                    VStack(spacing: 8) {
                        ZStack {
                            RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill).frame(width: 46, height: 46)
                            Image(systemName: ["leaf.fill", "link", "crown.fill", "building.columns.fill"][i])
                                .font(.system(size: 20)).foregroundColor(on ? p.brand : p.textPrimary)
                        }
                        Text(t).font(.lb(FontSize.small)).foregroundColor(on ? p.textPrimary : p.textSecondary).lineLimit(1)
                    }.frame(maxWidth: .infinity)
                }.buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 6)
    }

    private var recommendations: [APIEarnProduct] {
        let all = m.products.value ?? []
        var seen = Set<String>()
        return all.filter { pr in
            let a = pr.asset ?? ""
            guard !a.isEmpty, !seen.contains(a) else { return false }
            seen.insert(a); return true
        }.prefix(3).map { $0 }
    }

    private func recommendCard(_ pr: APIEarnProduct) -> some View {
        Button { session.requireLogin { subscribe = pr } } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 5) {
                    CoinIcon(base: pr.asset ?? "USDT", size: 18)
                    Text(pr.asset ?? "").font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                    Text(pr.kind == .onchain ? "质押" : "抢购")
                        .font(.lb(10)).foregroundColor(p.down)
                        .padding(.horizontal, 4).frame(height: 15)
                        .background(RoundedRectangle(cornerRadius: 3).fill(p.downTint))
                }
                HStack(alignment: .firstTextBaseline, spacing: 3) {
                    Text(EarnProductRow.aprText(pr)).font(.lbNum(17, .semibold)).foregroundColor(p.textPrimary)
                    Text("APR").font(.lb(10)).foregroundColor(p.textSecondary)
                }
                Text(pr.kind == .onchain ? "链上赚币" : (pr.kind == .fixed ? "定期理财" : "理财宝"))
                    .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            }
            .padding(12).frame(width: 106, height: 96, alignment: .topLeading)
            .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
        }.buttonStyle(.plain)
    }

    private func sectionTitle(_ t: String) -> some View {
        Text(t).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            .padding(.horizontal, Spacing.pageH)
    }

    private var searchField: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass").font(.system(size: 14)).foregroundColor(p.textTertiary)
            TextField("搜索币种", text: $search).font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
        }
        .padding(.horizontal, 12).frame(height: 34)
        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
        .padding(.horizontal, Spacing.pageH)
    }

    private var overview: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text("理财资产(USDT)").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Image(systemName: hide ? "eye.slash" : "eye").font(.system(size: 12)).foregroundColor(p.textSecondary).onTapGesture { hide.toggle() }
                Spacer()
                Button { mineOpen = true } label: {
                    HStack(spacing: 2) {
                        Text("我的持仓").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                        Image(systemName: "chevron.right").font(.system(size: 10)).foregroundColor(p.textTertiary)
                    }
                }.buttonStyle(.plain)
            }
            if session.isLoggedIn {
                HStack(alignment: .firstTextBaseline, spacing: 4) {
                    Text(hide ? "******" : Fmt.amount(Dec.parse(m.summary?.totalValueUsdt), 2))
                        .font(.lbNum(30, .bold)).foregroundColor(p.textPrimary)
                    Text("USDT").font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                }
                Text(hide ? "******" : "昨日收益 +" + Fmt.amount(Dec.parse(m.summary?.recentInterestUsdt), 4))
                    .font(.lbNum(FontSize.caption)).foregroundColor(p.up)
            } else {
                Text("--").font(.lbNum(30, .bold)).foregroundColor(p.textPrimary)
                LBButton(title: "登录查看理财资产", kind: .secondary, height: 36) { session.present(.login) }
            }
        }
        .padding(.horizontal, Spacing.pageH).padding(.top, 10)
    }

    private func matches(_ pr: APIEarnProduct, _ tab: Int) -> Bool {
        let title = pr.title ?? ""
        switch tab {
        case 0: return true
        case 1: return pr.kind == .flexible || pr.kind == .simple
        case 2: return pr.kind == .fixed
        case 3: return pr.kind == .onchain || title.contains("链上")
        case 4: return title.localizedCaseInsensitiveContains("VIP")
        case 5: return title.contains("质押") || title.contains("借币")
        default: return false
        }
    }

    @ViewBuilder private var list: some View {
        switch m.products {
        case .idle, .loading: SkeletonList(rows: 6)
        case let .failed(e): ErrorStateView(message: e) { Task { await m.load() } }.frame(height: 300)
        case let .loaded(all):
            if tab == 6 {
                mineBody
            } else if filteredRows(all).isEmpty {
                EmptyStateView(text: "暂无理财产品").frame(height: 260)
            } else {
                LazyVStack(spacing: 0) {
                    ForEach(filteredRows(all), id: \.id) { pr in
                        EarnProductRow(product: pr) { session.requireLogin { subscribe = pr } }
                        Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                    }
                }
            }
        }
    }

    private func filteredRows(_ all: [APIEarnProduct]) -> [APIEarnProduct] {
        var rows = all.filter { matches($0, tab) }
        let q = search.trimmingCharacters(in: .whitespaces).uppercased()
        if !q.isEmpty {
            rows = rows.filter { ($0.asset ?? "").uppercased().contains(q) || ($0.title ?? "").uppercased().contains(q) }
        }
        return rows
    }

    @ViewBuilder private var mineBody: some View {
        if !session.isLoggedIn {
            EmptyBlock(text: "登录后查看我的理财", actions: [("登录 / 注册", { session.present(.login) })])
        } else if (m.positions?.items ?? []).isEmpty && (m.positions?.unlocking ?? []).isEmpty {
            EmptyBlock(text: "暂无持仓，去申购吧", actions: [("去申购", { tab = 0 })])
        } else {
            LazyVStack(spacing: 0) {
                ForEach(Array((m.positions?.items ?? []).enumerated()), id: \.offset) { _, h in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            CoinIcon(base: h.asset ?? "USDT", size: 22)
                            Text(h.title ?? h.asset ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(h.kind == "fixed" ? "定期" : "活期").font(.lb(10)).foregroundColor(p.textSecondary)
                                .padding(.horizontal, 4).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                            Spacer()
                            if h.kind != "fixed" {
                                Button("赎回") { redeem = h }.font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                                    .padding(.horizontal, 12).frame(height: 28).background(Capsule().fill(p.fill))
                            }
                        }
                        HStack {
                            KVCell(k: "持有(\(h.asset ?? ""))", v: Fmt.amount(Dec.parse(h.amount), 4))
                            KVCell(k: "预计日收益", v: Fmt.amount(Dec.parse(h.estDailyInterest), 6), align: .center)
                            KVCell(k: "累计收益", v: Fmt.amount(Dec.parse(h.totalInterest), 6), align: .trailing, vColor: p.up)
                        }
                    }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                    Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
                }
                ForEach(Array((m.positions?.unlocking ?? []).enumerated()), id: \.offset) { _, u in
                    HStack {
                        Text("赎回中 \(u.amount ?? "") \(u.asset ?? "")").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary)
                        Spacer()
                        Text(u.releaseAt.map { "预计 " + Fmt.dateTime(Date(timeIntervalSince1970: Double($0) / 1000)) + " 到账" } ?? "处理中")
                            .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }.padding(.horizontal, Spacing.pageH).frame(height: 44)
                }
            }
        }
    }
}

struct EarnProductRow: View {
    @Environment(\.palette) private var p
    let product: APIEarnProduct
    let onTap: () -> Void
    var body: some View {
        let soldOut = product.status == "sold_out" || product.status == "soldout" || (product.remaining.map { Dec.parse($0) <= 0 } ?? false)
        HStack(spacing: 10) {
            CoinIcon(base: product.asset ?? "USDT", size: 30)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 4) {
                    Text(product.title ?? product.asset ?? "").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                    if product.newcomer == true { tag("新人专享", p.brand) }
                    if product.hot == true { tag("热门", p.down) }
                }
                Text(product.kind == .fixed ? "定期 \(product.termDays ?? 0) 天" : "活期 · 随存随取")
                    .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text(aprText).font(.lbNum(FontSize.title, .semibold)).foregroundColor(p.up)
                Text("参考年化").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Button(soldOut ? "已售罄" : "申购", action: onTap).disabled(soldOut)
                .font(.lb(FontSize.caption, .medium)).foregroundColor(soldOut ? p.textTertiary : p.textOnPrimary)
                .padding(.horizontal, 12).frame(height: 30).background(Capsule().fill(soldOut ? p.fill : p.primary))
        }
        .padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }
    static func aprText(_ p: APIEarnProduct) -> String {
        let a = Dec.parse(p.apr) * 100
        if let mx = p.aprMax, Dec.parse(mx) > Dec.parse(p.apr) { return "\(Fmt.amount(a, 2))%~\(Fmt.amount(Dec.parse(mx) * 100, 2))%" }
        return "\(Fmt.amount(a + Dec.parse(p.bonusApr) * 100, 2))%"
    }
    private var aprText: String {
        let a = Dec.parse(product.apr) * 100
        if let mx = product.aprMax, Dec.parse(mx) > Dec.parse(product.apr) { return "\(Fmt.amount(a, 2))%~\(Fmt.amount(Dec.parse(mx) * 100, 2))%" }
        return "\(Fmt.amount(a + Dec.parse(product.bonusApr) * 100, 2))%"
    }
    private func tag(_ t: String, _ c: Color) -> some View {
        Text(t).font(.lb(10)).foregroundColor(c).padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).stroke(c, lineWidth: 0.6))
    }
}

struct EarnSubscribeSheet: View {
    @Environment(\.palette) private var p
    let product: APIEarnProduct
    let onClose: (Bool) -> Void
    @State private var amount = ""
    @State private var avail: Decimal = 0
    @State private var agree = false
    @State private var busy = false
    private var asset: String { product.asset ?? "USDT" }
    private var v: Decimal { Dec.parse(amount) }
    private var maxAmt: Decimal {
        var x = avail
        if let pm = product.perUserMax, Dec.parse(pm) > 0 { x = min(x, Dec.parse(pm)) }
        if let r = product.remaining, Dec.parse(r) > 0 { x = min(x, Dec.parse(r)) }
        return x
    }
    private var error: String? {
        if amount.isEmpty { return nil }
        if v < Dec.parse(product.minAmount) { return "最小申购 \(product.minAmount ?? "") \(asset)" }
        if v > avail { return "可用余额不足" }
        if v > maxAmt { return "超过可申购上限" }
        return nil
    }
    var body: some View {
        LBSheet(title: "申购 \(product.title ?? asset)", subtitle: product.kind == .fixed ? "定期 \(product.termDays ?? 0) 天" : "活期", showClose: true, onClose: { onClose(false) }) {
            VStack(alignment: .leading, spacing: 10) {
                NumberBox(label: "申购数量", unit: asset, text: $amount, onEdit: {})
                if let e = error { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
                HStack {
                    Text("可用 \(Fmt.amount(avail, 4)) \(asset)").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Button("最大") { amount = Dec.plain(maxAmt, 4) }.font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                    Button("划转") { onClose(false); Router.shared.feature = nil; Router.shared.open(.transfer) }.font(.lb(FontSize.small)).foregroundColor(p.brand)
                }
                KVRow(k: "参考年化", v: "\(Fmt.amount(Dec.parse(product.apr) * 100, 2))%", vColor: p.up)
                KVRow(k: "预计日收益", v: "\(Fmt.amount(v * Dec.parse(product.apr) / 365, 6)) \(asset)")
                if product.kind == .fixed { KVRow(k: "到期收益", v: "\(Fmt.amount(v * Dec.parse(product.apr) * Decimal(product.termDays ?? 0) / 365, 6)) \(asset)") }
                KVRow(k: "赎回规则", v: product.redeemRule ?? (product.kind == .fixed ? "到期自动返还" : "随时赎回，\(product.unlockDays ?? 0) 天到账"))
                Checkbox(on: $agree, label: "我已阅读并同意《理财服务协议》")
                LBButton(title: "确认申购", enabled: agree && v > 0 && error == nil, loading: busy) {
                    busy = true
                    Task {
                        do {
                            let r = try await EarnAPI.subscribe(product.id ?? "", amount: Dec.plain(v, 8))
                            ToastCenter.shared.show("申购成功\(r.expectedInterest.map { "，预计收益 \($0) \(asset)" } ?? "")", .success)
                            onClose(true)
                        } catch { ToastCenter.shared.error(error) }
                        busy = false
                    }
                }
            }.padding(.horizontal, Spacing.pageH)
        }
        .task { if let b = try? await AssetsAPI.spotBalance(asset) { avail = b } }
    }
}

struct EarnRedeemSheet: View {
    @Environment(\.palette) private var p
    let pos: APIEarnPositions200ResponseItemsInner
    let onClose: (Bool) -> Void
    @State private var amount = ""
    @State private var busy = false
    private var held: Decimal { Dec.parse(pos.amount) }
    var body: some View {
        LBSheet(title: "赎回 \(pos.title ?? pos.asset ?? "")", showClose: true, onClose: { onClose(false) }) {
            VStack(alignment: .leading, spacing: 10) {
                NumberBox(label: "赎回数量", unit: pos.asset ?? "", text: $amount, onEdit: {})
                HStack {
                    Text("可赎回 \(Fmt.amount(held, 4))").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Button("全部") { amount = Dec.plain(held, 8) }.font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                }
                if Dec.parse(amount) > held { Text("超过可赎回数量").font(.lb(FontSize.small)).foregroundColor(p.down) }
                Text("赎回后停止计息，资金将按产品规则返还至现货账户。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                LBButton(title: "确认赎回", enabled: Dec.parse(amount) > 0 && Dec.parse(amount) <= held, loading: busy) {
                    busy = true
                    Task {
                        do {
                            let all = Dec.parse(amount) == held
                            let r = try await EarnAPI.redeem(pos.productId ?? "", amount: all ? nil : amount, all: all)
                            ToastCenter.shared.show(r.releaseAt != nil ? "已提交赎回，预计到账时间见列表" : "赎回成功", .success)
                            onClose(true)
                        } catch { ToastCenter.shared.error(error) }
                        busy = false
                    }
                }
            }.padding(.horizontal, Spacing.pageH)
        }
    }
}
