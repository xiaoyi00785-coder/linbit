import SwiftUI

// MARK: - 接口：资产总览 / 币种网络 / 账户划转 / 站内转账 / 提现记录 / 地址簿与白名单
extension AssetsAPI {
    private struct Empty: Encodable {}
    static func overview() async throws -> APIAssetsOverview { try await api.get("/assets/overview", auth: true) }
    static func walletNetworks(asset: String?) async throws -> [APIWalletNetwork] {
        let r: APIListWalletNetworks200Response = try await api.get("/wallet/networks", query: ["asset": asset], auth: true)
        return r.networks
    }
    static func accountTransfer(_ r: APITransferRequest) async throws -> APITransfer { try await api.post("/assets/transfer", body: r) }
    static func accountTransfers(page: Int) async throws -> APIListAssetTransfers200Response {
        try await api.get("/assets/transfers", query: ["page": "\(page)", "pageSize": "20"], auth: true)
    }
    static func internalTransfer(_ r: APIInternalTransferRequest) async throws -> APIInternalTransfer { try await api.post("/wallet/internal-transfer", body: r) }
    static func internalTransfers(page: Int) async throws -> APIListInternalTransfers200Response {
        try await api.get("/wallet/internal-transfers", query: ["page": "\(page)", "pageSize": "20"], auth: true)
    }
    static func withdrawals(page: Int) async throws -> APIPaginatedWithdrawals {
        try await api.get("/wallet/withdraw/records", query: ["page": "\(page)", "pageSize": "20"], auth: true)
    }
    static func cancelWithdrawal(_ id: String) async throws -> APIWithdrawal { try await api.post("/wallet/withdraw/\(id)/cancel", body: Empty()) }
    static func addresses() async throws -> [APIWithdrawAddress] {
        let r: APIWithdrawAddressList = try await api.get("/account/withdraw-addresses", auth: true)
        return r.items
    }
    static func createAddress(_ r: APICreateWithdrawAddressRequest) async throws -> APIWithdrawAddress { try await api.post("/account/withdraw-addresses", body: r) }
    static func updateAddress(_ id: String, _ r: APIUpdateWithdrawAddressRequest) async throws -> APIWithdrawAddress { try await api.patch("/account/withdraw-addresses/\(id)", body: r) }
    static func whitelist() async throws -> APIWhitelistStatus { try await api.get("/account/withdraw-whitelist", auth: true) }
    static func setWhitelist(_ r: APISetWhitelistRequest) async throws -> APIWhitelistStatus { try await api.put("/account/withdraw-whitelist", body: r) }
}

enum AssetAccount: String, CaseIterable, Identifiable {
    case funding, spot, futures
    var id: String { rawValue }
    var title: String { switch self { case .funding: "资金账户"; case .spot: "现货账户"; case .futures: "合约账户" } }
    var icon: String { switch self { case .funding: "wallet.pass.fill"; case .spot: "chart.bar.fill"; case .futures: "chart.line.uptrend.xyaxis" } }
    static func title(_ raw: String?) -> String { raw.flatMap(AssetAccount.init(rawValue:))?.title ?? (raw ?? "--") }
}

private func newClientId() -> String { "ios-\(UUID().uuidString.lowercased())" }

// MARK: - 通用小组件
struct AssetsLoginGate<Content: View>: View {
    @EnvironmentObject private var session: Session
    @ViewBuilder var content: Content
    var body: some View {
        if session.isLoggedIn { content } else {
            VStack(spacing: 16) {
                EmptyStateView(text: "登录后查看")
                LBButton(title: "登录 / 注册", height: 40) { session.present(.login) }.frame(width: 160)
            }.padding(.top, 60).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        }
    }
}

struct AssetsCard<Content: View>: View {
    @Environment(\.palette) private var p
    @ViewBuilder var content: Content
    var body: some View {
        VStack(alignment: .leading, spacing: 10) { content }
            .padding(14).frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: Radius.xl).fill(p.bgElevated))
            .overlay(RoundedRectangle(cornerRadius: Radius.xl).stroke(p.border))
    }
}

struct AssetKVRow: View {
    @Environment(\.palette) private var p
    let k: String
    let v: String
    var color: Color? = nil
    var body: some View {
        HStack(alignment: .top) {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Spacer(minLength: 12)
            Text(v).font(.lbNum(FontSize.small, .medium)).foregroundColor(color ?? p.textPrimary).multilineTextAlignment(.trailing)
        }
    }
}

struct StatusTag: View {
    let text: String
    let color: Color
    var body: some View {
        Text(text).font(.lb(11, .medium)).foregroundColor(color)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(RoundedRectangle(cornerRadius: Radius.xs).fill(color.opacity(0.12)))
    }
}

/// 「推荐」角标：后端 WalletNetwork.recommended == true 时显示；品牌色小标签，日/夜主题均取 palette.brand
struct RecommendedChip: View {
    @Environment(\.palette) private var p
    var body: some View {
        Text("推荐").font(.lb(10, .semibold)).foregroundColor(p.brand)
            .padding(.horizontal, 5).padding(.vertical, 1)
            .background(RoundedRectangle(cornerRadius: Radius.xs).fill(p.brand.opacity(0.15)))
            .overlay(RoundedRectangle(cornerRadius: Radius.xs).stroke(p.brand.opacity(0.45), lineWidth: 0.5))
    }
}

/// 选择框（左标签 + 值 + ▾）
struct PickerField: View {
    @Environment(\.palette) private var p
    let label: String
    let value: String
    var placeholder = "请选择"
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(label).font(.lb(11)).foregroundColor(p.textSecondary)
                    Text(value.isEmpty ? placeholder : value).font(.lb(FontSize.body, .medium))
                        .foregroundColor(value.isEmpty ? p.textTertiary : p.textPrimary)
                }
                Spacer()
                Image(systemName: "chevron.down").font(.system(size: 11)).foregroundColor(p.textSecondary)
            }
            .padding(.horizontal, 12).frame(height: 52)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
        }.buttonStyle(PressableStyle(scale: 1))
    }
}

/// 简单币种选择
struct AssetPickSheet: View {
    let current: String
    var assets = ["USDT", "BTC", "ETH", "SOL", "BNB", "XRP", "DOGE", "TRX"]
    let onPick: (String) -> Void
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "选择币种", onClose: onClose) {
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(assets, id: \.self) { a in SheetOptionRow(title: a, selected: a == current) { onPick(a) } }
                }
            }.frame(maxHeight: 420).padding(.bottom, 20)
        }
    }
}

/// 二次验证输入弹窗（谷歌验证码 + 可选资金密码）
struct VerifySheet: View {
    @Environment(\.palette) private var p
    let title: String
    var needFundPassword = false
    let onSubmit: (_ totp: String, _ fundPassword: String?) -> Void
    let onClose: () -> Void
    @State private var totp = ""
    @State private var fund = ""
    var body: some View {
        LBSheet(title: title, subtitle: "为保障资金安全，请完成安全验证", onClose: onClose) {
            VStack(spacing: 12) {
                LBTextField(label: "谷歌验证码", placeholder: "请输入 6 位谷歌验证码", text: $totp, keyboard: .numberPad)
                if needFundPassword {
                    LBTextField(label: "资金密码（如已设置）", placeholder: "资金密码（未设置可留空）", text: $fund, secure: true)
                }
                LBButton(title: "确认", enabled: totp.count == 6) { onSubmit(totp, fund.isEmpty ? nil : fund) }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
        }
    }
}

// MARK: - 资产总览
struct AssetOverviewView: View {
    @Environment(\.palette) private var p
    @State private var state: Loadable<APIAssetsOverview> = .idle
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "资产总览")
            AssetsLoginGate {
                ScrollView {
                    VStack(spacing: 14) {
                        switch state {
                        case .idle, .loading: SkeletonList(rows: 5)
                        case .failed(let e): ErrorStateView(message: e) { Task { await load() } }.padding(.top, 80)
                        case .loaded(let o): content(o)
                        }
                    }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                }
                .refreshable { await load() }
                .task { if state.value == nil { await load() } }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
    }

    @ViewBuilder private func content(_ o: APIAssetsOverview) -> some View {
        let total = Dec.parse(o.totalValueUsdt)
        VStack(alignment: .leading, spacing: 6) {
            Text("总资产估值").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(Fmt.amount(total, 2)).font(.lbNum(32, .bold)).foregroundColor(p.textPrimary)
                Text("USDT").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
            }
            Text("≈ " + Fmt.fiat(total, rate: AppConfig.usdtCny)).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            HStack(spacing: 6) {
                StatusTag(text: "充值入账：\(AssetAccount.title(o.depositAccount?.rawValue))", color: p.textSecondary)
                StatusTag(text: "提现扣款：\(AssetAccount.title(o.withdrawAccount?.rawValue))", color: p.textSecondary)
            }.padding(.top, 4)
        }.frame(maxWidth: .infinity, alignment: .leading)

        entries

        let accounts = o.accounts ?? []
        if accounts.isEmpty { EmptyStateView(text: "暂无资产") }
        ForEach(accounts.indices, id: \.self) { i in accountCard(accounts[i], total: total) }
    }

    private var entries: some View {
        let items: [(String, String, AssetsRoute)] = [
            ("账户划转", "arrow.left.arrow.right", .accountTransfer), ("内部转账", "person.2.fill", .internalTransfer),
            ("提现记录", "list.bullet.rectangle", .withdrawRecords), ("地址簿", "book.closed.fill", .addressBook),
            ("币种网络", "network", .networks(nil)),
        ]
        return HStack {
            ForEach(items.indices, id: \.self) { i in
                NavigationLink(value: items[i].2) {
                    VStack(spacing: 6) {
                        Image(systemName: items[i].1).font(.system(size: 16)).foregroundColor(p.textPrimary)
                            .frame(width: 40, height: 40).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                        Text(items[i].0).font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                    }.frame(maxWidth: .infinity)
                }.buttonStyle(PressableStyle())
            }
        }
    }

    private func accountCard(_ a: APIAssetsOverviewAccount, total: Decimal) -> some View {
        let acc = AssetAccount(rawValue: a.account?.rawValue ?? "") ?? .funding
        let v = Dec.parse(a.totalValueUsdt)
        let share = total > 0 ? v / total : 0
        return AssetsCard {
            HStack {
                Image(systemName: acc.icon).font(.system(size: 13)).foregroundColor(p.textPrimary)
                Text(acc.title).font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                Spacer()
                if a.available == false { StatusTag(text: "暂不可用", color: p.down) }
                else { Text("\(Fmt.amount(v, 2)) USDT").font(.lbNum(FontSize.label, .semibold)).foregroundColor(p.textPrimary) }
            }
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(p.fill)
                    Capsule().fill(p.brand).frame(width: g.size.width * CGFloat(NSDecimalNumber(decimal: min(share, 1)).doubleValue))
                }
            }.frame(height: 4)
            if a.available == false {
                Text("合约服务暂不可用，余额稍后刷新").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            } else {
                let bs = (a.balances ?? []).filter { Dec.parse($0.total) > 0 }
                if bs.isEmpty { Text("暂无余额").font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                ForEach(bs.prefix(6).indices, id: \.self) { j in
                    let b = bs[j]
                    HStack(spacing: 8) {
                        CoinIcon(base: b.asset ?? "", size: 20)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(b.asset ?? "--").font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                            if let px = AssetsModel.unitPrice(asset: b.asset, priceUsd: b.priceUsd) {
                                Text("$\(Fmt.amount(px, px < 1 ? 6 : 2))").font(.lbNum(11)).foregroundColor(p.textTertiary)
                            }
                        }
                        Spacer()
                        VStack(alignment: .trailing, spacing: 1) {
                            Text(Fmt.amount(Dec.parse(b.total), 8)).font(.lbNum(FontSize.caption)).foregroundColor(p.textPrimary)
                            Text(b.valuationUsd.map { "≈ \(Fmt.amount(Dec.parse($0), 2)) USDT" } ?? "暂无估值")
                                .font(.lbNum(11)).foregroundColor(p.textTertiary)
                        }
                    }
                }
            }
        }
    }

    private func load() async {
        if state.value == nil { state = .loading }
        do { state = .loaded(try await AssetsAPI.overview()) } catch { state = .failed(error.lbMessage) }
    }
}

// MARK: - 币种网络（充提开关、最小充提、确认数、Memo、手续费、到账时间）
struct NetworkCard: View {
    @Environment(\.palette) private var p
    let n: APIWalletNetwork
    var selected = false
    var body: some View {
        AssetsCard {
            HStack(spacing: 6) {
                Text(n.network ?? "--").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                if n.recommended == true { RecommendedChip() }
                if n.memoRequired == true { StatusTag(text: "需 Memo", color: p.brand) }
                Spacer()
                StatusTag(text: n.depositEnabled == false ? "暂停充值" : "可充值", color: n.depositEnabled == false ? p.down : p.up)
                StatusTag(text: n.withdrawEnabled == false ? "暂停提现" : "可提现", color: n.withdrawEnabled == false ? p.down : p.up)
                if selected { Image(systemName: "checkmark.circle.fill").foregroundColor(p.textPrimary) }
            }
            AssetKVRow(k: "提现手续费", v: "\(n.withdrawFee ?? "--") \(n.withdrawFeeAsset ?? n.asset ?? "")")
            AssetKVRow(k: "最小提现", v: "\(n.minWithdraw ?? "--") \(n.asset ?? "")")
            if let mx = n.maxWithdraw { AssetKVRow(k: "单笔最大提现", v: "\(mx) \(n.asset ?? "")") }
            AssetKVRow(k: "最小充值", v: "\(n.minDeposit ?? "--") \(n.asset ?? "")")
            AssetKVRow(k: "到账确认数", v: n.confirmations.map { "\($0) 次网络确认" } ?? "--")
            if let m = n.estimatedArrivalMinutes { AssetKVRow(k: "预计到账", v: "约 \(m) 分钟") }
        }
    }
}

struct WalletNetworksView: View {
    @Environment(\.palette) private var p
    @State private var asset: String
    @State private var state: Loadable<[APIWalletNetwork]> = .idle
    @State private var showAsset = false
    init(asset: String?) { _asset = State(initialValue: asset ?? "USDT") }
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "币种网络")
            AssetsLoginGate {
                VStack(spacing: 12) {
                    PickerField(label: "币种", value: asset) { showAsset = true }
                    ScrollView {
                        VStack(spacing: 10) {
                            switch state {
                            case .idle, .loading: SkeletonList(rows: 3)
                            case .failed(let e): ErrorStateView(message: e) { Task { await load() } }.padding(.top, 60)
                            case .loaded(let ns):
                                if ns.isEmpty { EmptyStateView(text: "\(asset) 暂无可用网络") }
                                ForEach(ns.indices, id: \.self) { i in NetworkCard(n: ns[i]) }
                                InlineBanner(text: "请务必确认充提网络一致，选错网络可能导致资产无法找回")
                            }
                        }.padding(.bottom, 30)
                    }.refreshable { await load() }
                }.padding(.horizontal, Spacing.pageH).padding(.top, 8)
                .task(id: asset) { await load() }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .lbSheet($showAsset) { AssetPickSheet(current: asset) { asset = $0; showAsset = false } onClose: { showAsset = false } }
    }
    private func load() async {
        state = .loading
        do { state = .loaded(try await AssetsAPI.walletNetworks(asset: asset)) } catch { state = .failed(error.lbMessage) }
    }
}

/// 网络选择弹窗（提现地址、提现等使用；不可提现的网络置灰不可选）
struct NetworkPickerSheet: View {
    @Environment(\.palette) private var p
    let asset: String
    var forWithdraw = true
    let selected: String?
    let onPick: (APIWalletNetwork) -> Void
    let onClose: () -> Void
    @State private var state: Loadable<[APIWalletNetwork]> = .idle
    var body: some View {
        LBSheet(title: "选择网络", subtitle: "请确保与对方平台/钱包使用相同网络", onClose: onClose) {
            ScrollView {
                VStack(spacing: 10) {
                    switch state {
                    case .idle, .loading: SkeletonList(rows: 3)
                    case .failed(let e): ErrorStateView(message: e) { Task { await load() } }
                    case .loaded(let ns):
                        if ns.isEmpty { EmptyStateView(text: "\(asset) 暂无可用网络") }
                        ForEach(ns.indices, id: \.self) { i in
                            let n = ns[i]
                            let enabled = forWithdraw ? n.withdrawEnabled != false : n.depositEnabled != false
                            Button { if enabled { onPick(n) } else { ToastCenter.shared.show("该网络暂停\(forWithdraw ? "提现" : "充值")") } } label: {
                                NetworkCard(n: n, selected: n.network == selected).opacity(enabled ? 1 : 0.45)
                            }.buttonStyle(PressableStyle())
                        }
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }.frame(maxHeight: 520)
        }
        .task { await load() }
    }
    private func load() async {
        state = .loading
        do { state = .loaded(try await AssetsAPI.walletNetworks(asset: asset)) } catch { state = .failed(error.lbMessage) }
    }
}

// MARK: - 账户划转（资金 / 现货 / 合约）
struct AccountTransferView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var from: AssetAccount = .funding
    @State private var to: AssetAccount = .spot
    @State private var asset = "USDT"
    @State private var amount = ""
    @State private var overview: APIAssetsOverview?
    @State private var picking: Int? = nil   // 0 from, 1 to
    @State private var showAsset = false
    @State private var submitting = false
    @State private var records: Loadable<[APITransfer]> = .idle
    @State private var page = 1
    @State private var hasMore = false

    private var available: Decimal {
        let acc = overview?.accounts?.first { $0.account?.rawValue == from.rawValue }
        return Dec.parse(acc?.balances?.first { $0.asset == asset }?.free)
    }
    private var validation: String? {
        if from == to { return "转出与转入账户不能相同" }
        if (from == .futures || to == .futures) && asset != "USDT" { return "合约账户仅支持 USDT 划转" }
        let a = Dec.parse(amount)
        if a <= 0 { return "请输入划转数量" }
        if overview != nil && a > available { return "可用余额不足" }
        return nil
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "账户划转")
            ScrollView {
                VStack(spacing: 14) {
                    accounts
                    PickerField(label: "币种", value: asset) { showAsset = true }
                    VStack(alignment: .leading, spacing: 6) {
                        NumberBox(label: "划转数量", unit: asset, text: $amount, onEdit: {})
                            .overlay(alignment: .trailing) {
                                Button("全部") { amount = "\(available)" }.font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary).padding(.trailing, 60)
                            }
                        Text("可用 \(Fmt.amount(available, 8)) \(asset)").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                    if from == .futures || to == .futures { InlineBanner(text: "合约账户仅支持 USDT；理财不属于划转账户，请使用申购 / 赎回") }
                    LBButton(title: "确认划转", enabled: session.isLoggedIn ? validation == nil : true, loading: submitting) { submit() }
                    if let v = validation, !amount.isEmpty { Text(v).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    history
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
            }.refreshable { await loadAll() }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task(id: session.isLoggedIn) { await loadAll() }
        .lbSheet(Binding(get: { picking != nil }, set: { if !$0 { picking = nil } })) {
            LBSheet(title: picking == 0 ? "转出账户" : "转入账户", onClose: { picking = nil }) {
                VStack(spacing: 0) {
                    ForEach(AssetAccount.allCases) { a in
                        SheetOptionRow(title: a.title, selected: (picking == 0 ? from : to) == a) {
                            if picking == 0 { if a == to { to = from }; from = a } else { if a == from { from = to }; to = a }
                            if a == .futures { asset = "USDT" }
                            picking = nil
                        }
                    }
                }.padding(.bottom, 20)
            }
        }
        .lbSheet($showAsset) { AssetPickSheet(current: asset) { asset = $0; showAsset = false } onClose: { showAsset = false } }
    }

    private var accounts: some View {
        HStack(spacing: 12) {
            VStack(spacing: 14) {
                row("从", from) { picking = 0 }
                Divider().overlay(p.divider)
                row("到", to) { picking = 1 }
            }
            Button { Haptics.tap(); (from, to) = (to, from) } label: {
                Image(systemName: "arrow.up.arrow.down").font(.system(size: 14, weight: .semibold)).foregroundColor(p.textPrimary)
                    .frame(width: 36, height: 36).background(Circle().fill(p.bgElevated)).overlay(Circle().stroke(p.border))
            }.accessibilityLabel("交换账户")
        }
        .padding(14).background(RoundedRectangle(cornerRadius: Radius.xl).fill(p.fill))
    }
    private func row(_ k: String, _ a: AssetAccount, _ tap: @escaping () -> Void) -> some View {
        Button(action: tap) {
            HStack(spacing: 10) {
                Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).frame(width: 18, alignment: .leading)
                Image(systemName: a.icon).font(.system(size: 13)).foregroundColor(p.textPrimary)
                Text(a.title).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 11)).foregroundColor(p.textTertiary)
            }
        }.buttonStyle(.plain)
    }

    @ViewBuilder private var history: some View {
        HStack { Text("划转记录").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary); Spacer() }.padding(.top, 10)
        if !session.isLoggedIn { EmptyStateView(text: "登录后查看划转记录") } else {
            switch records {
            case .idle, .loading: SkeletonList(rows: 3)
            case .failed(let e): ErrorStateView(message: e) { Task { await loadRecords() } }
            case .loaded(let rs):
                if rs.isEmpty { EmptyStateView(text: "暂无划转记录") }
                ForEach(rs.indices, id: \.self) { i in transferRow(rs[i]) }
                if hasMore { Button("加载更多") { Task { await more() } }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
            }
        }
    }
    private func transferRow(_ t: APITransfer) -> some View {
        let txt = t.status == .done ? "已完成" : (t.status == .failed ? "失败" : "处理中")
        let c: Color = t.status == .done ? p.up : (t.status == .failed ? p.down : p.textSecondary)
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("\(AssetAccount.title(t.from)) → \(AssetAccount.title(t.to))").font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                Spacer(); StatusTag(text: txt, color: c)
            }
            HStack {
                Text("\(t.amount ?? "--") \(t.asset ?? "")").font(.lbNum(FontSize.caption)).foregroundColor(p.textPrimary)
                Spacer()
                if let d = t.createdAt { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
            }
            Divider().overlay(p.divider)
        }
    }

    private func submit() {
        session.requireLogin {
            if let v = validation { ToastCenter.shared.show(v, .error); return }
            guard let f = APITransferRequest.APIFrom(rawValue: from.rawValue), let t = APITransferRequest.APITo(rawValue: to.rawValue) else { return }
            submitting = true
            Task {
                defer { submitting = false }
                do {
                    let r = try await AssetsAPI.accountTransfer(APITransferRequest(clientId: newClientId(), asset: asset, amount: amount, from: f, to: t))
                    ToastCenter.shared.show(r.status == .failed ? "划转失败" : "划转成功", r.status == .failed ? .error : .success)
                    amount = ""; await loadAll()
                } catch { ToastCenter.shared.error(error) }
            }
        }
    }
    private func loadAll() async {
        guard session.isLoggedIn else { return }
        overview = try? await AssetsAPI.overview()
        await loadRecords()
    }
    private func loadRecords() async {
        if records.value == nil { records = .loading }
        do {
            let r = try await AssetsAPI.accountTransfers(page: 1)
            page = 1; records = .loaded(r.items ?? []); hasMore = (r.items?.count ?? 0) < (r.total ?? 0)
        } catch { records = .failed(error.lbMessage) }
    }
    private func more() async {
        guard let cur = records.value, let r = try? await AssetsAPI.accountTransfers(page: page + 1) else { return }
        page += 1; let all = cur + (r.items ?? []); records = .loaded(all); hasMore = all.count < (r.total ?? 0)
    }
}

// MARK: - 站内转账（UID / 邮箱，免手续费，需 2FA）
struct InternalTransferView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var byEmail = 0
    @State private var recipient = ""
    @State private var asset = "USDT"
    @State private var amount = ""
    @State private var available: Decimal?
    @State private var showAsset = false
    @State private var showVerify = false
    @State private var submitting = false
    @State private var records: Loadable<[APIInternalTransfer]> = .idle

    private var validation: String? {
        let r = recipient.trimmingCharacters(in: .whitespaces)
        if r.isEmpty { return byEmail == 1 ? "请输入收款人邮箱" : "请输入收款人 UID" }
        if byEmail == 1 && !(r.contains("@") && r.contains(".")) { return "邮箱格式不正确" }
        if byEmail == 0 && !r.allSatisfy(\.isNumber) { return "UID 仅包含数字" }
        let a = Dec.parse(amount)
        if a <= 0 { return "请输入转账数量" }
        if let av = available, a > av { return "可用余额不足" }
        return nil
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "内部转账")
            ScrollView {
                VStack(spacing: 14) {
                    HStack(spacing: 6) {
                        StatusTag(text: "0 手续费", color: p.brand); StatusTag(text: "实时到账", color: p.up)
                        Spacer()
                    }
                    PillSegment(items: ["UID", "邮箱"], selected: $byEmail, height: 34)
                    LBTextField(label: byEmail == 1 ? "收款人邮箱" : "收款人 UID", placeholder: byEmail == 1 ? "请输入对方注册邮箱" : "请输入对方 UID",
                                text: $recipient, keyboard: byEmail == 1 ? .emailAddress : .numberPad)
                    PickerField(label: "币种", value: asset) { showAsset = true }
                    VStack(alignment: .leading, spacing: 6) {
                        NumberBox(label: "转账数量", unit: asset, text: $amount, onEdit: {})
                        Text("资金账户可用 \(available.map { Fmt.amount($0, 8) } ?? "--") \(asset)").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                    InlineBanner(text: "内部转账从资金账户扣款，实时到账且不可撤回；请仔细核对收款人信息")
                    LBButton(title: "转账", enabled: session.isLoggedIn ? validation == nil : true, loading: submitting) {
                        session.requireLogin {
                            if let v = validation { ToastCenter.shared.show(v, .error) } else { showVerify = true }
                        }
                    }
                    if let v = validation, !amount.isEmpty || !recipient.isEmpty { Text(v).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    history
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
            }
            .scrollDismissesKeyboard(.interactively)
            .refreshable { await load() }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task(id: "\(session.isLoggedIn)-\(asset)") { await load() }
        .lbSheet($showAsset) { AssetPickSheet(current: asset) { asset = $0; showAsset = false } onClose: { showAsset = false } }
        .lbSheet($showVerify) {
            VerifySheet(title: "确认转账 \(amount) \(asset)", needFundPassword: true) { totp, fund in showVerify = false; submit(totp, fund) } onClose: { showVerify = false }
        }
    }

    @ViewBuilder private var history: some View {
        HStack { Text("转账记录").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary); Spacer() }.padding(.top, 10)
        if !session.isLoggedIn { EmptyStateView(text: "登录后查看转账记录") } else {
            switch records {
            case .idle, .loading: SkeletonList(rows: 3)
            case .failed(let e): ErrorStateView(message: e) { Task { await load() } }
            case .loaded(let rs):
                if rs.isEmpty { EmptyStateView(text: "暂无转账记录") }
                ForEach(rs.indices, id: \.self) { i in row(rs[i]) }
            }
        }
    }
    private func row(_ t: APIInternalTransfer) -> some View {
        let out = t.direction == .out
        let txt = t.status == .completed ? "已完成" : (t.status == .failed ? "失败" : "处理中")
        let c: Color = t.status == .completed ? p.up : (t.status == .failed ? p.down : p.textSecondary)
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(out ? "转出" : "转入").font(.lb(FontSize.caption, .medium)).foregroundColor(out ? p.down : p.up)
                Text(out ? (t.toMaskedEmail ?? t.toUid.map { "UID \($0)" } ?? "--") : (t.fromUid.map { "来自 UID \($0)" } ?? "--"))
                    .font(.lb(FontSize.caption)).foregroundColor(p.textPrimary)
                Spacer(); StatusTag(text: txt, color: c)
            }
            HStack {
                Text("\(out ? "-" : "+")\(t.amount ?? "--") \(t.asset ?? "")").font(.lbNum(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                Spacer()
                if let d = t.createdAt { Text(Fmt.dateTime(d)).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary) }
            }
            if let r = t.failReason, !r.isEmpty { Text(ErrorText.map[r] ?? r).font(.lb(FontSize.small)).foregroundColor(p.down) }
            Divider().overlay(p.divider)
        }
    }

    private func submit(_ totp: String, _ fund: String?) {
        let r = recipient.trimmingCharacters(in: .whitespaces)
        submitting = true
        Task {
            defer { submitting = false }
            do {
                let req = APIInternalTransferRequest(clientId: newClientId(), asset: asset, amount: amount,
                                                     toUid: byEmail == 0 ? r : nil, toEmail: byEmail == 1 ? r : nil, totpCode: totp, fundPassword: fund)
                let t = try await AssetsAPI.internalTransfer(req)
                ToastCenter.shared.show(t.status == .failed ? (ErrorText.map[t.failReason ?? ""] ?? "转账失败") : "转账成功", t.status == .failed ? .error : .success)
                amount = ""; await load()
            } catch { ToastCenter.shared.error(error) }
        }
    }
    private func load() async {
        guard session.isLoggedIn else { return }
        if let o = try? await AssetsAPI.overview() {
            available = Dec.parse(o.accounts?.first { $0.account == .funding }?.balances?.first { $0.asset == asset }?.free)
        }
        if records.value == nil { records = .loading }
        do { records = .loaded(try await AssetsAPI.internalTransfers(page: 1).items ?? []) } catch { records = .failed(error.lbMessage) }
    }
}

// MARK: - 提现记录（pending 可撤销）
struct WithdrawRecordsView: View {
    @Environment(\.palette) private var p
    @State private var state: Loadable<[APIWithdrawal]> = .idle
    @State private var page = 1
    @State private var hasMore = false
    @State private var confirm: ConfirmRequest?
    @State private var cancelling: String?
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "提现记录")
            AssetsLoginGate {
                ScrollView {
                    LazyVStack(spacing: 10) {
                        switch state {
                        case .idle, .loading: SkeletonList(rows: 5)
                        case .failed(let e): ErrorStateView(message: e) { Task { await load() } }.padding(.top, 80)
                        case .loaded(let ws):
                            if ws.isEmpty { EmptyStateView(text: "暂无提现记录").padding(.top, 60) }
                            ForEach(ws, id: \.id) { w in row(w) }
                            if hasMore { Color.clear.frame(height: 1).task { await more() } }
                        }
                    }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                }
                .refreshable { await load() }
                .task { if state.value == nil { await load() } }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .lbConfirm($confirm)
    }
    static func statusText(_ s: APIWithdrawal.APIStatus) -> String {
        switch s { case .pending: "待审核"; case .processing: "处理中"; case .completed: "已完成"; case .failed: "失败"; case .canceled: "已撤销" }
    }
    private func row(_ w: APIWithdrawal) -> some View {
        let c: Color = w.status == .completed ? p.up : (w.status == .failed ? p.down : (w.status == .canceled ? p.textTertiary : p.brand))
        return AssetsCard {
            HStack(spacing: 8) {
                CoinIcon(base: w.asset, size: 20)
                Text("\(w.amount) \(w.asset)").font(.lbNum(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                Spacer(); StatusTag(text: Self.statusText(w.status), color: c)
            }
            AssetKVRow(k: "网络", v: w.network)
            AssetKVRow(k: "地址", v: w.address)
            AssetKVRow(k: "Gas 费", v: "\(w.gasFee ?? w.fee) \(w.gasFeeAsset ?? w.asset)")
            AssetKVRow(k: "预计到账", v: "\(Fmt.amount(Dec.parse(w.amount) - Dec.parse(w.gasFee ?? w.fee), 8)) \(w.asset)")
            AssetKVRow(k: "时间", v: Fmt.dateTime(w.createdAt))
            if w.status == .pending {
                HStack {
                    Spacer()
                    Button {
                        confirm = .init(title: "撤销提现", message: "撤销后冻结的 \(w.amount) \(w.asset) 将退回可用余额，确定撤销吗？", confirmTitle: "撤销", destructive: true) {
                            Task { await cancel(w) }
                        }
                    } label: {
                        Text(cancelling == w.id ? "撤销中…" : "撤销").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                            .padding(.horizontal, 14).frame(height: 28).background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                    }.buttonStyle(PressableStyle()).disabled(cancelling != nil)
                }
            }
        }
    }
    private func cancel(_ w: APIWithdrawal) async {
        cancelling = w.id; defer { cancelling = nil }
        do { _ = try await AssetsAPI.cancelWithdrawal(w.id); ToastCenter.shared.show("已撤销，资金已退回", .success); await load() }
        catch { ToastCenter.shared.error(error) }
    }
    private func load() async {
        if state.value == nil { state = .loading }
        do { let r = try await AssetsAPI.withdrawals(page: 1); page = 1; state = .loaded(r.items); hasMore = r.items.count < r.total }
        catch { state = .failed(error.lbMessage) }
    }
    private func more() async {
        guard let cur = state.value, let r = try? await AssetsAPI.withdrawals(page: page + 1) else { hasMore = false; return }
        page += 1; let all = cur + r.items; state = .loaded(all); hasMore = all.count < r.total && !r.items.isEmpty
    }
}

// MARK: - 地址簿 + 提现白名单
struct AddressBookView: View {
    @Environment(\.palette) private var p
    @State private var state: Loadable<[APIWithdrawAddress]> = .idle
    @State private var whitelist: Bool?
    @State private var showAdd = false
    @State private var verify: VerifyAction?
    @State private var filter = 0   // 0 全部 1 白名单
    enum VerifyAction: Identifiable { case whitelist(Bool), address(APIWithdrawAddress, Bool)
        var id: String { switch self { case .whitelist(let b): "w\(b)"; case .address(let a, let b): "\(a.id)\(b)" } }
    }
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "地址簿") {
                Button { showAdd = true } label: { Image(systemName: "plus").font(.system(size: 16, weight: .medium)).foregroundColor(p.textPrimary) }
            }
            AssetsLoginGate {
                ScrollView {
                    VStack(spacing: 12) {
                        whitelistCard
                        PillSegment(items: ["全部地址", "白名单地址"], selected: $filter, height: 32)
                        switch state {
                        case .idle, .loading: SkeletonList(rows: 4)
                        case .failed(let e): ErrorStateView(message: e) { Task { await load() } }.padding(.top, 40)
                        case .loaded(let all):
                            let list = filter == 1 ? all.filter(\.whitelisted) : all
                            if list.isEmpty {
                                EmptyStateView(text: filter == 1 ? "暂无白名单地址" : "暂无提现地址")
                                LBButton(title: "添加地址", kind: .outline, height: 36) { showAdd = true }.frame(width: 140)
                            }
                            ForEach(list, id: \.id) { a in row(a) }
                        }
                    }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                }
                .refreshable { await load() }
                .task { if state.value == nil { await load() } }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .lbSheet($showAdd) { AddAddressSheet { showAdd = false; Task { await load() } } onClose: { showAdd = false } }
        .lbSheet(Binding(get: { verify != nil }, set: { if !$0 { verify = nil } })) {
            if let v = verify {
                VerifySheet(title: title(v)) { totp, _ in verify = nil; Task { await apply(v, totp) } } onClose: { verify = nil }
            }
        }
    }
    private func title(_ v: VerifyAction) -> String {
        switch v { case .whitelist(let b): b ? "开启提现白名单" : "关闭提现白名单"; case .address(_, let b): b ? "加入白名单" : "移出白名单" }
    }
    private var whitelistCard: some View {
        AssetsCard {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("提现白名单").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                    Text("开启后仅能向白名单地址提现").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                Spacer()
                Toggle("", isOn: Binding(get: { whitelist ?? false }, set: { verify = .whitelist($0) }))
                    .labelsHidden().tint(p.up).disabled(whitelist == nil)
            }
        }
    }
    private func row(_ a: APIWithdrawAddress) -> some View {
        AssetsCard {
            HStack(spacing: 8) {
                CoinIcon(base: a.asset, size: 20)
                Text(a.label?.isEmpty == false ? a.label! : a.asset).font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                StatusTag(text: a.network, color: p.textSecondary)
                if a.whitelisted { StatusTag(text: "白名单", color: p.up) }
                Spacer()
            }
            Text(a.address).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary).textSelection(.enabled)
            if let m = a.memo, !m.isEmpty { AssetKVRow(k: "Memo", v: m) }
            HStack {
                Text("添加于 \(Fmt.dateTime(a.createdAt))").font(.lb(11)).foregroundColor(p.textTertiary)
                Spacer()
                Button { UIPasteboard.general.string = a.address; ToastCenter.shared.show("已复制地址", .success) } label: {
                    Text("复制").font(.lb(FontSize.small)).foregroundColor(p.textPrimary).padding(.horizontal, 10).frame(height: 26)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                }.buttonStyle(PressableStyle())
                Button { verify = .address(a, !a.whitelisted) } label: {
                    Text(a.whitelisted ? "移出白名单" : "加入白名单").font(.lb(FontSize.small)).foregroundColor(p.textPrimary).padding(.horizontal, 10).frame(height: 26)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                }.buttonStyle(PressableStyle())
            }
        }
    }
    private func apply(_ v: VerifyAction, _ totp: String) async {
        do {
            switch v {
            case .whitelist(let b):
                whitelist = try await AssetsAPI.setWhitelist(APISetWhitelistRequest(enabled: b, totpCode: totp)).enabled
                ToastCenter.shared.show(b ? "已开启提现白名单" : "已关闭提现白名单", .success)
            case .address(let a, let b):
                _ = try await AssetsAPI.updateAddress(a.id, APIUpdateWithdrawAddressRequest(whitelisted: b, totpCode: totp))
                ToastCenter.shared.show(b ? "已加入白名单" : "已移出白名单", .success)
                await load()
            }
        } catch { ToastCenter.shared.error(error) }
    }
    private func load() async {
        if state.value == nil { state = .loading }
        async let w = try? AssetsAPI.whitelist()
        do { state = .loaded(try await AssetsAPI.addresses()) } catch { state = .failed(error.lbMessage) }
        let ws = await w
        whitelist = ws?.enabled ?? whitelist
    }
}

struct AddAddressSheet: View {
    @Environment(\.palette) private var p
    let onDone: () -> Void
    let onClose: () -> Void
    @State private var asset = "USDT"
    @State private var network: APIWalletNetwork?
    @State private var address = ""
    @State private var memo = ""
    @State private var label = ""
    @State private var whitelisted = true
    @State private var totp = ""
    @State private var showAsset = false
    @State private var showNet = false
    @State private var saving = false
    private var validation: String? {
        if network == nil { return "请选择网络" }
        if address.trimmingCharacters(in: .whitespaces).count < 10 { return "请输入有效的提现地址" }
        if network?.memoRequired == true && memo.isEmpty { return "该网络需填写 Memo" }
        if totp.count != 6 { return "请输入 6 位谷歌验证码" }
        return nil
    }
    var body: some View {
        LBSheet(title: "添加提现地址", showClose: true, onClose: onClose) {
            ScrollView {
                VStack(spacing: 12) {
                    PickerField(label: "币种", value: asset) { showAsset = true }
                    PickerField(label: "网络", value: network?.network ?? "", placeholder: "请选择网络") { showNet = true }
                    LBTextField(label: "地址", placeholder: "粘贴或输入提现地址", text: $address)
                    if network?.memoRequired == true { LBTextField(label: "Memo / Tag", placeholder: "该网络必须填写 Memo", text: $memo) }
                    LBTextField(label: "备注（选填）", placeholder: "如：我的冷钱包", text: $label)
                    HStack { Checkbox(on: $whitelisted, label: "加入提现白名单"); Spacer() }
                    LBTextField(label: "谷歌验证码", placeholder: "请输入 6 位谷歌验证码", text: $totp, keyboard: .numberPad)
                    LBButton(title: "保存", enabled: validation == nil, loading: saving) { Task { await save() } }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }.frame(maxHeight: 560)
        }
        .lbSheet($showAsset) { AssetPickSheet(current: asset) { asset = $0; network = nil; showAsset = false } onClose: { showAsset = false } }
        .lbSheet($showNet) { NetworkPickerSheet(asset: asset, selected: network?.network) { network = $0; showNet = false } onClose: { showNet = false } }
        .task(id: asset) { await preselectRecommended() }
    }
    /// 未选择网络时，默认选中后端推荐且可提现的网络
    private func preselectRecommended() async {
        guard network == nil else { return }
        let a = asset
        guard let ns = try? await AssetsAPI.walletNetworks(asset: a), a == asset, network == nil else { return }
        network = ns.first { $0.recommended == true && $0.withdrawEnabled != false }
    }
    private func save() async {
        guard let n = network?.network else { return }
        saving = true; defer { saving = false }
        do {
            _ = try await AssetsAPI.createAddress(APICreateWithdrawAddressRequest(asset: asset, network: n, address: address.trimmingCharacters(in: .whitespaces),
                                                                                memo: memo.isEmpty ? nil : memo, label: label.isEmpty ? nil : label,
                                                                                whitelisted: whitelisted, totpCode: totp))
            ToastCenter.shared.show("地址已保存", .success); onDone()
        } catch { ToastCenter.shared.error(error) }
    }
}
