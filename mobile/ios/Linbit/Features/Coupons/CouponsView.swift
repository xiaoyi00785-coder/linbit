import SwiftUI

// MARK: - 卡券中心：可使用 / 使用中 / 已失效；体验金券激活需确认
enum CouponAPI {
    static func list(_ status: String) async throws -> APICouponList { try await APIClient.shared.get("/coupons", query: ["status": status], auth: true) }
    static func summary() async throws -> APICouponSummary { try await APIClient.shared.get("/coupons/summary", auth: true) }
    static func activate(_ id: Int64) async throws -> APICouponActivateResult {
        try await APIClient.shared.post("/coupons/\(id)/activate", body: Optional<String>.none)
    }
}

/// 截图演示：SecShot.raw = "coupon:<state>"，state ∈ issued / active / expired / empty / error / confirm / fail
enum CouponShot {
    static var state: String? { SecShot.raw?.hasPrefix("coupon:") == true ? String(SecShot.raw!.dropFirst(7)) : nil }
    static func items(_ status: String) -> [APICoupon] {
        let now = Int64(Date().timeIntervalSince1970 * 1000), day: Int64 = 86_400_000
        switch status {
        case "active":
            return [.init(id: 21, kind: .trial, amount: "50", asset: "USDT", title: "合约体验金", status: .active, activateBy: now + 20 * day, validDays: 14,
                          activatedAt: now - 2 * day, createdAt: now - 5 * day, bonus: .init(remaining: "37.42", status: "active", expiresAt: now + 12 * day))]
        case "expired":
            return [.init(id: 9, kind: .deduction, amount: "10", asset: "USDT", title: "手续费抵扣券", status: .expired, activateBy: now - 3 * day, validDays: 14, createdAt: now - 40 * day),
                    .init(id: 8, kind: .trial, amount: "20", asset: "USDT", title: "合约体验金", status: .revoked, activateBy: now - 9 * day, validDays: 7, createdAt: now - 50 * day)]
        default:
            return [.init(id: 31, kind: .trial, amount: "100", asset: "USDT", title: "新人合约体验金", status: .issued, activateBy: now + 29 * day, validDays: 14, createdAt: now - day),
                    .init(id: 30, kind: .deduction, amount: "20", asset: "USDT", title: "手续费抵扣券", status: .issued, activateBy: now + 6 * day, validDays: 30, createdAt: now - 3 * day)]
        }
    }
}

struct CouponsView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    private static let tabs = [("issued", "可使用"), ("active", "使用中"), ("expired", "已失效")]
    @State private var tab = ["active": 1, "expired": 2][CouponShot.state ?? ""] ?? 0
    @State private var data: Loadable<[APICoupon]> = .loading
    @State private var summary: APICouponSummary?
    @State private var confirm: ConfirmRequest?
    @State private var activating: Int64?
    @State private var banner: String? = CouponShot.state == "fail" ? ErrorText.map["INSUFFICIENT_OWN_FUNDS"] : nil

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "卡券中心", onBack: onBack)
            HStack(spacing: 12) {
                stat("可使用", summary?.available)
                stat("使用中", summary?.active)
            }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
            PrimaryTabs(items: Self.tabs.map(\.1), selected: $tab, size: FontSize.label, underline: true)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Spacing.pageH)
            Divider().overlay(p.border)
            if let banner {
                InlineBanner(text: banner).padding(.horizontal, Spacing.pageH).padding(.top, 12)
            }
            switch data {
            case .idle, .loading: Spacer(); ProgressView(); Spacer()
            case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
            case let .loaded(items) where items.isEmpty: Spacer(); EmptyStateView(text: tab == 0 ? "暂无可用卡券，完成新手任务即可领取" : "暂无卡券"); Spacer()
            case let .loaded(items):
                ScrollView {
                    LazyVStack(spacing: 12) { ForEach(items, id: \.id) { card($0) } }.padding(Spacing.pageH)
                }.refreshable { await load() }
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task(id: tab) { await load() }
        .lbConfirm($confirm)
        .onAppear { if CouponShot.state == "confirm", let c = CouponShot.items("issued").first { ask(c) } }
    }

    private func stat(_ t: String, _ n: Int?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Text(n.map(String.init) ?? "--").font(.lbNum(20, .semibold)).foregroundColor(p.textPrimary)
        }
        .frame(maxWidth: .infinity, alignment: .leading).padding(12)
        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
    }

    private func ms(_ v: Int64?) -> String { v.map { Fmt.dateTime(Date(timeIntervalSince1970: Double($0) / 1000)) } ?? "--" }

    private func card(_ c: APICoupon) -> some View {
        let dim = c.status == .expired || c.status == .revoked
        let trial = c.kind == .trial
        return HStack(spacing: 0) {
            VStack(spacing: 2) {
                Text(Fmt.amount(Dec.parse(c.amount ?? "0"), 2)).font(.lbNum(24, .bold))
                Text(c.asset ?? "USDT").font(.lb(FontSize.small))
            }
            .foregroundColor(dim ? p.textTertiary : p.brand)
            .frame(width: 96).frame(maxHeight: .infinity)
            .background((dim ? p.fillStrong : p.brand.opacity(0.1)))
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    Text(c.title ?? (trial ? "合约体验金" : "手续费抵扣券")).font(.lb(FontSize.body, .semibold))
                        .foregroundColor(dim ? p.textTertiary : p.textPrimary).lineLimit(1)
                    Text(trial ? "体验金" : "抵扣券").font(.lb(10)).foregroundColor(p.textSecondary)
                        .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                }
                switch c.status {
                case .active:
                    if let b = c.bonus {
                        Text("剩余 \(Fmt.amount(Dec.parse(b.remaining ?? "0"), 2)) \(c.asset ?? "USDT")").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        Text("有效期至 \(ms(b.expiresAt))").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                    } else {
                        Text("激活于 \(ms(c.activatedAt))").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                    }
                case .expired, .revoked:
                    Text(c.status == .revoked ? "已作废" : "已过期").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                default:
                    Text("请于 \(ms(c.activateBy)) 前激活").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                    Text("激活后 \(c.validDays ?? 0) 天内有效").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }
            }
            .padding(12).frame(maxWidth: .infinity, alignment: .leading)
            if c.status == .issued {
                LBButton(title: "激活", height: 30, loading: activating == c.id) { ask(c) }
                    .frame(width: 64).padding(.trailing, 12)
            }
        }
        .frame(height: 96)
        .background(p.bg)
        .clipShape(RoundedRectangle(cornerRadius: Radius.lg))
        .overlay(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border, lineWidth: 1))
    }

    private func ask(_ c: APICoupon) {
        let amt = "\(Fmt.amount(Dec.parse(c.amount ?? "0"), 2)) \(c.asset ?? "USDT")"
        let msg = c.kind == .trial
            ? "激活后将向合约账户发放 \(amt) 体验金，\(c.validDays ?? 0) 天内有效。体验金仅可用于开仓，盈利可提取，亏损优先扣除体验金。激活要求合约账户自有资金不低于 \(amt)。"
            : "激活后 \(c.validDays ?? 0) 天内，交易手续费将优先由本券抵扣，最高 \(amt)。"
        confirm = .init(title: "激活\(c.title ?? "卡券")", message: msg, confirmTitle: "确认激活") { Task { await activate(c) } }
    }

    private func activate(_ c: APICoupon) async {
        guard let id = c.id else { return }
        banner = nil
        if CouponShot.state != nil { return }
        activating = id; defer { activating = nil }
        do {
            _ = try await CouponAPI.activate(id)
            Haptics.success(); ToastCenter.shared.show("激活成功", .success)
            await load()
        } catch {
            banner = securityMessage(error); Haptics.error()
        }
    }

    private func load() async {
        let status = Self.tabs[tab].0
        if let st = CouponShot.state {
            summary = .init(available: 2, active: 1)
            if st == "error" { data = .failed("网络连接失败，请检查网络"); return }
            data = .loaded(st == "empty" ? [] : CouponShot.items(status)); return
        }
        if data.value == nil { data = .loading }
        async let s = try? CouponAPI.summary()
        do {
            let items = try await CouponAPI.list(status).items ?? []
            // 切换 Tab 会取消上一次请求：已取消的结果直接丢弃，避免旧 Tab 的数据覆盖新 Tab
            guard !Task.isCancelled else { return }
            data = .loaded(items)
        } catch {
            // 被取消的请求不是错误，不显示错误页（新的加载会接管状态）
            if Task.isCancelled || isRequestCancelled(error) { return }
            data = .failed(securityMessage(error))
        }
        let sum = await s
        if !Task.isCancelled { summary = sum ?? summary }
    }
}
