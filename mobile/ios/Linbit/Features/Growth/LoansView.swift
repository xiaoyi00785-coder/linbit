import SwiftUI

// MARK: - 质押借币：借币（期限/质押币种/报价）/ 我的借款（LTV 预警、还款、追加质押、续期）
enum LoanAPI {
    struct Borrow: Encodable { let collateralAsset: String; let amount: String; let collateralAmount: String; let term: String }
    struct Repay: Encodable { let amount: String?; let all: Bool? }
    struct Amount: Encodable { let amount: String }
    static func market() async throws -> J { try await APIClient.shared.get("/loans/market") }
    static func quote(_ c: String, amount: String, term: String) async throws -> J {
        try await APIClient.shared.get("/loans/quote", query: ["collateralAsset": c, "amount": amount, "term": term], auth: true)
    }
    static func borrow(_ b: Borrow) async throws -> J { try await APIClient.shared.post("/loans", body: b) }
    static func list(_ status: String) async throws -> J { try await APIClient.shared.get("/loans", query: ["status": status], auth: true) }
    static func summary() async throws -> J { try await APIClient.shared.get("/loans/summary", auth: true) }
    static func repay(_ id: Int64, _ r: Repay) async throws -> J { try await APIClient.shared.post("/loans/\(id)/repay", body: r) }
    static func addCollateral(_ id: Int64, _ amount: String) async throws -> J { try await APIClient.shared.post("/loans/\(id)/collateral", body: Amount(amount: amount)) }
    static func renew(_ id: Int64) async throws -> J { try await APIClient.shared.post("/loans/\(id)/renew", body: Optional<String>.none) }
}

/// 截图演示：loan:<market|quote|borrow-error|loans|loans-empty|closed|repay|collateral|error>
enum LoanDemo {
    static var state: String? { DemoShot.state("loan") }
    static let market: J = ["asset": "USDT", "initialLtv": "0.65", "warnLtv": "0.75", "liquidationLtv": "0.85", "userLimit": "200000", "minAmount": "100",
                            "earlyRepayPenaltyRate": "0", "overdueMultiplier": "3", "overdueLiquidationDays": 3,
                            "terms": [["term": "7d", "hourlyRate": "0.0000042", "status": "active"], ["term": "30d", "hourlyRate": "0.0000048", "status": "active"],
                                      ["term": "90d", "hourlyRate": "0.0000055", "status": "active"], ["term": "flex", "hourlyRate": "0.0000061", "status": "offline"]],
                            "collaterals": [["asset": "BTC", "cap": "50", "price": "67250.5"], ["asset": "ETH", "cap": "800", "price": "3521.32"], ["asset": "SOL", "cap": "20000", "price": "168.4"]]]
    static let quote: J = ["amount": "5000", "collateral": "0.11376", "price": "67250.5", "ltv": "0.65", "hourlyRate": "0.0000048",
                           "hourlyInterest": "0.024", "termInterest": "17.28", "liquidationPrice": "51695.8"]
    static func loans(_ st: String) -> J {
        if st == "loans-empty" { return ["items": []] }
        let now = DemoShot.nowMs, day: Int64 = 86_400_000
        if st == "closed" {
            return ["items": [["id": 12, "asset": "USDT", "collateralAsset": "ETH", "term": "7d", "principal": "0", "initialPrincipal": "2000", "collateral": "0",
                               "initialCollateral": "0.9", "interestPaid": "1.41", "status": "repaid", "startedAt": J(now - day * 40), "closedAt": J(now - day * 34)]]]
        }
        return ["items": [
            ["id": 31, "asset": "USDT", "collateralAsset": "BTC", "term": "30d", "hourlyRate": "0.0000048", "principal": "5000", "collateral": "0.11376",
             "interest": "4.03", "debt": "5004.03", "status": "active", "startedAt": J(now - day * 7), "dueAt": J(now + day * 23), "price": "67250.5",
             "ltv": "0.6541", "liquidationPrice": "51748.2", "warning": false],
            ["id": 29, "asset": "USDT", "collateralAsset": "ETH", "term": "7d", "hourlyRate": "0.0000042", "principal": "8000", "collateral": "3.05",
             "interest": "12.88", "debt": "8012.88", "status": "active", "startedAt": J(now - day * 6), "dueAt": J(now + day), "price": "3521.32",
             "ltv": "0.7461", "liquidationPrice": "3089.2", "warning": true]]]
    }
    static let summary: J = ["active": 2, "totalDebt": "13016.91", "totalCollateralValue": "18390.2", "asset": "USDT"]
}

private func termName(_ t: String?) -> String {
    guard let t else { return "--" }
    if t == "flex" { return "灵活" }
    return t.hasSuffix("d") ? "\(t.dropLast()) 天" : t
}

struct LoansView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    @State private var top = ["loans", "loans-empty", "closed", "repay", "collateral"].contains(LoanDemo.state ?? "") ? 1 : 0

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "质押借币", onBack: onBack)
            PrimaryTabs(items: ["借币", "我的借款"], selected: $top, size: FontSize.label, underline: true)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Spacing.pageH)
            Divider().overlay(p.border)
            if top == 0 { LoanBorrowView { top = 1 } } else { LoanListView() }
        }
        .background(p.bg.ignoresSafeArea())
    }
}

struct LoanBorrowView: View {
    @Environment(\.palette) private var p
    let onDone: () -> Void
    @State private var market: Loadable<J> = .loading
    @State private var coll = 0
    @State private var term = 1
    @State private var amount = ["quote", "borrow-error"].contains(LoanDemo.state ?? "") ? "5000" : ""
    @State private var quote: J = .null
    @State private var quoting = false
    @State private var loading = false
    @State private var error: String? = LoanDemo.state == "borrow-error" ? ErrorText.map["LTV_TOO_HIGH"] : nil
    @State private var confirm: ConfirmRequest?

    var body: some View {
        switch market {
        case .idle, .loading: VStack { Spacer(); ProgressView(); Spacer() }.task { await load() }
        case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
        case let .loaded(m): form(m)
        }
    }

    private func form(_ m: J) -> some View {
        let colls = m["collaterals"].a, terms = m["terms"].a
        let asset = m["asset"].s ?? "USDT"
        let c = colls.indices.contains(coll) ? colls[coll] : .null
        let t = terms.indices.contains(term) ? terms[term] : .null
        let termOff = t["status"].s != nil && t["status"].s != "active"
        let minAmt = m["minAmount"].d
        let amtErr: String? = amount.isEmpty ? nil : (Dec.parse(amount) < minAmt ? "最低借款 \(jAmt(m["minAmount"], 0)) \(asset)"
            : (Dec.parse(amount) > m["userLimit"].d && m["userLimit"].d > 0 ? "超过个人借款上限 \(jAmt(m["userLimit"], 0)) \(asset)" : nil))
        // 没有报价（或报价里没有所需质押数量）时不能提交，不能用 0 兜底
        let ok = !amount.isEmpty && amtErr == nil && !termOff && !quote.isNull && quote["collateral"].d > 0 && !quoting
        return ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                if let error { InlineBanner(text: error) }
                LBTextField(label: "借款金额 (\(asset))", placeholder: "最低 \(jAmt(m["minAmount"], 0)) \(asset)", text: $amount, keyboard: .decimalPad, error: amtErr)
                Text("质押币种").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                PillSegment(items: colls.map { $0["asset"].s ?? "--" }, selected: $coll)
                Text("借款期限").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                PillSegment(items: terms.map { termName($0["term"].s) }, selected: $term)
                if termOff { Text("该期限暂停借款，请选择其他期限").font(.lb(FontSize.small)).foregroundColor(p.down) }
                VStack(spacing: 10) {
                    KV(k: "参考价格", v: "1 \(c["asset"].s ?? "") ≈ \(jAmt(quote["price"].isNull ? c["price"] : quote["price"])) \(asset)")
                    KV(k: "所需质押", v: quote.isNull ? "--" : "\(jAmt(quote["collateral"], 6)) \(c["asset"].s ?? "")")
                    KV(k: "初始质押率", v: jRatio(quote["ltv"].isNull ? m["initialLtv"] : quote["ltv"]))
                    KV(k: "小时利率", v: jRatio(t["hourlyRate"], 5))
                    KV(k: "预计到期利息", v: quote.isNull ? "--" : "\(jAmt(quote["termInterest"])) \(asset)")
                    KV(k: "强平价格", v: quote.isNull ? "--" : "\(jAmt(quote["liquidationPrice"])) \(asset)", color: p.down)
                    if quoting { ProgressView().frame(maxWidth: .infinity) }
                }.lbCard()
                InfoNote(text: "质押率达 \(jRatio(m["warnLtv"], 0)) 时提醒追加质押，达 \(jRatio(m["liquidationLtv"], 0)) 将触发强制平仓。逾期利息按 \(m["overdueMultiplier"].s ?? "3") 倍计收，逾期 \(m["overdueLiquidationDays"].s ?? "3") 天将强平。")
                LBButton(title: "借币", enabled: ok, loading: loading) {
                    Session.shared.requireLogin {
                        confirm = .init(title: "确认借币", message: "借入 \(amount) \(asset)，质押 \(jAmt(quote["collateral"], 6)) \(c["asset"].s ?? "")，期限 \(termName(t["term"].s))。到期需归还本金及利息。", confirmTitle: "确认借币") {
                            Task { await borrow(c["asset"].s ?? "", t["term"].s ?? "") }
                        }
                    }
                }
            }.padding(Spacing.pageH)
        }
        .task(id: "\(coll)-\(term)-\(amount)") { await requote(c["asset"].s, t["term"].s, ok: amtErr == nil) }
        .lbConfirm($confirm)
    }

    private func load() async {
        if let st = LoanDemo.state {
            market = st == "error" ? .failed("网络连接失败，请检查网络") : .loaded(LoanDemo.market)
            if ["quote", "borrow-error"].contains(st) { quote = LoanDemo.quote }
            return
        }
        do { market = .loaded(try await LoanAPI.market()) } catch { market = .failed(securityMessage(error)) }
    }

    private func requote(_ c: String?, _ t: String?, ok: Bool) async {
        if LoanDemo.state != nil { return }
        quote = .null
        guard let c, let t, ok, Dec.parse(amount) > 0, Session.shared.isLoggedIn else { return }
        try? await Task.sleep(nanoseconds: 400_000_000)
        if Task.isCancelled { return }
        quoting = true; defer { quoting = false }
        do { quote = try await LoanAPI.quote(c, amount: amount, term: t); error = nil } catch { if !Task.isCancelled { self.error = securityMessage(error) } }
    }

    private func borrow(_ c: String, _ t: String) async {
        error = nil
        if LoanDemo.state != nil { return }
        guard let collateral = quote["collateral"].s, Dec.parse(collateral) > 0 else { return }
        loading = true; defer { loading = false }
        do {
            _ = try await LoanAPI.borrow(.init(collateralAsset: c, amount: amount, collateralAmount: collateral, term: t))
            Haptics.success(); ToastCenter.shared.show("借币成功", .success); amount = ""; onDone()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

struct LoanListView: View {
    @Environment(\.palette) private var p
    @State private var tab = LoanDemo.state == "closed" ? 1 : 0
    @State private var data: Loadable<[J]> = .loading
    @State private var summary: J = .null
    @State private var action: (J, Int)? = nil
    @State private var showAction = false
    @State private var confirm: ConfirmRequest?
    @State private var banner: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 10) {
                    StatTile(title: "借款中", value: summary["active"].s ?? "--")
                    StatTile(title: "总负债 (USDT)", value: jAmt(summary["totalDebt"]))
                    StatTile(title: "质押价值 (USDT)", value: jAmt(summary["totalCollateralValue"]))
                }
                PillSegment(items: ["进行中", "已结束"], selected: $tab)
                if let banner { InlineBanner(text: banner) }
                switch data {
                case .idle, .loading: ProgressView().frame(maxWidth: .infinity).padding(.vertical, 60)
                case let .failed(m): ErrorStateView(message: m) { Task { await load() } }.frame(height: 300)
                case let .loaded(items) where items.isEmpty: EmptyStateView(text: tab == 0 ? "暂无借款" : "暂无已结束的借款").frame(height: 300)
                case let .loaded(items):
                    ForEach(Array(items.enumerated()), id: \.offset) { _, l in card(l) }
                }
            }.padding(Spacing.pageH)
        }
        .refreshable { await load() }
        .task(id: tab) { await load() }
        .lbConfirm($confirm)
        .lbSheet($showAction) {
            if let a = action { LoanActionSheet(loan: a.0, kind: a.1, onClose: { showAction = false }) { showAction = false; Task { await load() } } }
        }
        .onAppear {
            if let st = LoanDemo.state, st == "repay" || st == "collateral", let l = LoanDemo.loans("loans").items.last {
                action = (l, st == "repay" ? 0 : 1); showAction = true
            }
        }
    }

    private func card(_ l: J) -> some View {
        let active = l["status"].s == "active"
        let warn = l["warning"].b
        let ca = l["collateralAsset"].s ?? ""
        return VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("借 \(l["asset"].s ?? "USDT") · 押 \(ca)").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text(termName(l["term"].s)).font(.lb(10)).foregroundColor(p.textSecondary)
                    .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                Spacer()
                if active {
                    Text("质押率 \(jRatio(l["ltv"]))").font(.lbNum(FontSize.small, .semibold)).foregroundColor(warn ? p.down : p.up)
                } else {
                    Text(l["status"].s == "liquidated" ? "已强平" : "已还清").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }
            }
            if warn && active {
                Text("质押率已接近强平线，请及时追加质押或还款").font(.lb(FontSize.small)).foregroundColor(p.down)
                    .padding(8).frame(maxWidth: .infinity, alignment: .leading).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.down.opacity(0.08)))
            }
            if active {
                KV(k: "待还总额", v: "\(jAmt(l["debt"])) \(l["asset"].s ?? "USDT")")
                KV(k: "本金 / 利息", v: "\(jAmt(l["principal"])) / \(jAmt(l["interest"]))")
                KV(k: "质押数量", v: "\(jAmt(l["collateral"], 6)) \(ca)")
                KV(k: "强平价格", v: jAmt(l["liquidationPrice"]), color: p.down)
                KV(k: "到期时间", v: l["dueAt"].time, color: l["overdue"].b ? p.down : nil)
                HStack(spacing: 8) {
                    LBButton(title: "还款", height: 34) { action = (l, 0); showAction = true }
                    LBButton(title: "追加质押", kind: .secondary, height: 34) { action = (l, 1); showAction = true }
                    // 活期借款无需续借（后端返回 TERM_NOT_RENEWABLE），不显示续期按钮
                    if !isFlexible(l) {
                        LBButton(title: "续期", kind: .outline, height: 34) {
                            let asset = l["asset"].s ?? "USDT"
                            confirm = .init(title: "续期",
                                            message: "续期将先从资金账户扣除当前未结利息 \(jAmt(l["interest"])) \(asset)，再将到期日顺延一个相同期限（已逾期则从现在起算），本金不变。请确保资金账户余额充足。",
                                            confirmTitle: "确认续期") { Task { await renew(l) } }
                        }
                    }
                }
            } else {
                KV(k: "借款本金", v: "\(jAmt(l["initialPrincipal"])) \(l["asset"].s ?? "USDT")")
                KV(k: "已付利息", v: jAmt(l["interestPaid"]))
                KV(k: "结束时间", v: l["closedAt"].time)
            }
        }.lbCard()
    }

    /// 活期借款：期限为 flex，或没有到期时间
    private func isFlexible(_ l: J) -> Bool { l["term"].s == "flex" || l["dueAt"].isNull }

    private func renew(_ l: J) async {
        banner = nil
        if LoanDemo.state != nil { return }
        guard let id = l["id"].i else { return }
        do { _ = try await LoanAPI.renew(id); Haptics.success(); ToastCenter.shared.show("续期成功", .success); await load() }
        catch { banner = securityMessage(error); Haptics.error() }
    }

    private func load() async {
        if let st = LoanDemo.state {
            summary = LoanDemo.summary
            data = st == "error" ? .failed("网络连接失败，请检查网络") : .loaded(LoanDemo.loans(tab == 1 ? "closed" : st).items); return
        }
        guard Session.shared.isLoggedIn else { data = .loaded([]); return }
        if data.value == nil { data = .loading }
        async let s = try? LoanAPI.summary()
        do { data = .loaded(try await LoanAPI.list(tab == 0 ? "active" : "closed").items) } catch { data = .failed(securityMessage(error)) }
        summary = await s ?? summary
    }
}

/// kind: 0 还款 1 追加质押
struct LoanActionSheet: View {
    @Environment(\.palette) private var p
    let loan: J
    let kind: Int
    let onClose: () -> Void
    let onDone: () -> Void
    @State private var amount = LoanDemo.state == "collateral" ? "0.5" : ""
    @State private var all = LoanDemo.state == "repay"
    @State private var loading = false
    @State private var error: String?

    private var asset: String { kind == 0 ? (loan["asset"].s ?? "USDT") : (loan["collateralAsset"].s ?? "") }
    private var amtErr: String? {
        guard !amount.isEmpty else { return nil }
        if Dec.parse(amount) <= 0 { return "请输入有效数量" }
        if kind == 0 && Dec.parse(amount) > loan["debt"].d { return "超过待还总额" }
        return nil
    }
    private var valid: Bool { (kind == 0 && all) || (!amount.isEmpty && amtErr == nil) }

    var body: some View {
        LBSheet(title: kind == 0 ? "还款" : "追加质押", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                if let error { InlineBanner(text: error) }
                KV(k: "待还总额", v: "\(jAmt(loan["debt"])) \(loan["asset"].s ?? "USDT")")
                KV(k: "当前质押率", v: jRatio(loan["ltv"]), color: loan["warning"].b ? p.down : nil)
                if kind == 0 { PillSegment(items: ["全部还清", "部分还款"], selected: Binding(get: { all ? 0 : 1 }, set: { all = $0 == 0 })) }
                if kind == 1 || !all {
                    LBTextField(label: "数量 (\(asset))", placeholder: kind == 0 ? "还款金额" : "追加质押数量", text: $amount, keyboard: .decimalPad, error: amtErr)
                }
                InfoNote(text: kind == 0 ? "还款优先偿还利息，再偿还本金；全部还清后质押物将退回资金账户。" : "追加质押可降低质押率，避免触发强制平仓。")
                LBButton(title: kind == 0 ? "确认还款" : "确认追加", enabled: valid, loading: loading) { Task { await submit() } }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 16)
        }
    }

    private func submit() async {
        error = nil
        if LoanDemo.state != nil { return }
        guard let id = loan["id"].i else { return }
        loading = true; defer { loading = false }
        do {
            if kind == 0 { _ = try await LoanAPI.repay(id, all ? .init(amount: nil, all: true) : .init(amount: amount, all: nil)) }
            else { _ = try await LoanAPI.addCollateral(id, amount) }
            Haptics.success(); ToastCenter.shared.show(kind == 0 ? "还款成功" : "追加成功", .success); onDone()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}
