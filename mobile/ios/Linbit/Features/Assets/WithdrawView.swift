import SwiftUI
import UIKit

// MARK: - 接口：链上提现
extension AssetsAPI {
    /// POST /wallet/withdraw：已开启 TOTP 传 totpCode，否则传发到邮箱/手机的 code（purpose=withdraw）
    static func withdraw(_ r: APIWithdrawRequest) async throws -> APIWithdrawal { try await api.post("/wallet/withdraw", body: r) }
    /// 未开启 TOTP 时，提现验证码发往账户已绑定的邮箱/手机
    static func sendWithdrawCode() async throws {
        let _: APISendCodeResponse = try await api.post("/account/code/send", body: APISendCodeAuthedRequest(purpose: .withdraw))
    }
}

/// 提现金额规则（纯函数，金额一律 Decimal）。最小 / 最大 / Gas 全部取后端 /wallet/withdraw/networks，不写死。
enum WithdrawRules {
    static func positive(_ s: String?) -> Decimal? { let d = Dec.parse(s); return d > 0 ? d : nil }

    /// Gas 以提现币种计价时从提现数量中扣除（后端 GasDeductMode=amount）；以其他币种计价时不影响到账数量。
    static func gasInAsset(_ n: APIWithdrawNetwork, asset: String) -> Bool { (n.gasFeeAsset ?? asset) == asset }

    /// 返回 nil 表示通过；数量为空时不提示错误。
    static func amountError(_ amount: String, asset: String, available: Decimal?, net: APIWithdrawNetwork?) -> String? {
        guard !amount.isEmpty else { return nil }
        let a = Dec.parse(amount)
        if a <= 0 { return "请输入提现数量" }
        if let av = available, a > av { return "可用余额不足" }
        guard let n = net else { return nil }
        if let mn = positive(n.minWithdraw), a < mn { return "最小提现数量 \(n.minWithdraw ?? "") \(asset)" }
        if let mx = positive(n.maxWithdraw), a > mx { return "单笔最大提现 \(n.maxWithdraw ?? "") \(asset)" }
        if gasInAsset(n, asset: asset), let gas = positive(n.gasFeeEstimate), a <= gas {
            return "提现数量需大于链上网络费 \(n.gasFeeEstimate ?? "") \(asset)"
        }
        return nil
    }

    /// 预计到账数量（数量 − 同币种 Gas）；无法计算时为 nil。
    static func receive(_ amount: String, asset: String, net: APIWithdrawNetwork?) -> Decimal? {
        let a = Dec.parse(amount)
        guard a > 0, let n = net else { return nil }
        let gas = gasInAsset(n, asset: asset) ? (positive(n.gasFeeEstimate) ?? 0) : 0
        return max(a - gas, 0)
    }

    static func eta(_ minutes: Int?) -> String {
        guard let m = minutes, m > 0 else { return "--" }
        return m < 60 ? "约 \(m) 分钟" : "约 \(m / 60) 小时\(m % 60 > 0 ? " \(m % 60) 分钟" : "")"
    }
}

// MARK: - 链上提现（对照 Android WithdrawScreen：选币 → 选网络 → 地址 / Memo → 数量 → 安全验证）
struct WithdrawView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var asset: String
    @State private var mode = 0
    @State private var recipient = ""
    @State private var nets: Loadable<[APIWithdrawNetwork]> = .idle
    @State private var recommended: Set<String> = []
    @State private var net: APIWithdrawNetwork?
    @State private var address = ""
    @State private var memo = ""
    @State private var amount = ""
    @State private var available: Decimal?
    @State private var withdrawAccount = "funding"
    @State private var security: APISecurityOverview?
    @State private var showAsset = false
    @State private var showNet = false
    @State private var showBook = false
    @State private var showVerify = false
    @State private var submitting = false

    init(asset: String?) { _asset = State(initialValue: asset ?? "USDT") }

    private var addr: String { address.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var memoRequired: Bool { net?.memoRequired == true }
    private var lockedUntil: Date? { security?.withdrawLockedUntil.flatMap { $0 > Date() ? $0 : nil } }
    /// 后台要求必须设置资金密码，而用户尚未设置：直接提交必然被拒（FUND_PASSWORD_NOT_SET）
    private var fundPasswordMissing: Bool {
        security?.policy?.fundPasswordMandatory == true && security?.fundPasswordSet != true
    }
    private var amountError: String? {
        if mode == 1 {
            guard !amount.isEmpty else { return nil }
            let a = Dec.parse(amount)
            if a <= 0 { return "请输入转账数量" }
            if let av = available, a > av { return "可用余额不足" }
            return nil
        }
        return WithdrawRules.amountError(amount, asset: asset, available: available, net: net)
    }
    private var receive: Decimal? { WithdrawRules.receive(amount, asset: asset, net: net) }
    /// 后端返回的原始字符串（只在 > 0 时展示，避免出现「最小 0」）
    private var minText: String? { WithdrawRules.positive(net?.minWithdraw) == nil ? nil : net?.minWithdraw }
    private var maxText: String? { WithdrawRules.positive(net?.maxWithdraw) == nil ? nil : net?.maxWithdraw }

    /// 提交前的完整校验；nil 表示可以提交
    private var validation: String? {
        if lockedUntil != nil { return mode == 1 ? "提现保护期中，暂不可转账" : "提现保护期中，暂不可提现" }
        if fundPasswordMissing { return "请先设置资金密码" }
        if mode == 1 {
            if security?.twoFactor?.enabled != true { return "内部转账需先绑定谷歌验证器" }
            if recipient.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return "请输入接收方 UID 或邮箱" }
            if amount.isEmpty { return "请输入转账数量" }
            return amountError
        }
        if net == nil { return "请选择提现网络" }
        if addr.count < 10 { return "请输入有效的提现地址" }
        if memoRequired && memo.trimmingCharacters(in: .whitespaces).isEmpty { return "该网络必须填写 Memo / Tag" }
        if amount.isEmpty { return "请输入提现数量" }
        return amountError
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "提现") {
                HStack(spacing: 14) {
                    Button { Router.shared.assetsPath.append(AssetsRoute.addressBook) } label: {
                        Text("地址簿").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                    NavigationLink(value: AssetsRoute.withdrawRecords) {
                        Image(systemName: "doc.text.magnifyingglass").font(.system(size: 16)).foregroundColor(p.textPrimary)
                    }.accessibilityLabel("提现记录")
                }
            }
            AssetsLoginGate {
                VStack(spacing: 0) {
                    ScrollView { form.padding(.horizontal, Spacing.pageH).padding(.vertical, 12) }
                        .refreshable { await load() }
                    bottomBar
                }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task(id: "\(session.isLoggedIn)-\(asset)") { await load() }
        .lbSheet($showAsset) {
            AssetPickSheet(current: asset) { a in
                if a != asset { asset = a; nets = .idle; net = nil; recommended = []; available = nil; memo = ""; amount = "" }
                showAsset = false
            } onClose: { showAsset = false }
        }
        .lbSheet($showNet) {
            WithdrawNetworkSheet(asset: asset, nets: nets.value ?? [], recommended: recommended, selected: net?.network) { n in
                net = n; showNet = false
            } onClose: { showNet = false }
        }
        .lbSheet($showBook) {
            WithdrawAddressPickSheet(asset: asset) { a in
                showBook = false
                address = a.address
                memo = a.memo ?? ""
                if let n = nets.value?.first(where: { $0.network == a.network }) { net = n }
                else { ToastCenter.shared.show("该地址所在网络（\(a.network)）当前暂停提现", .error) }
            } onClose: { showBook = false }
        }
        .lbSheet($showVerify) {
            WithdrawVerifySheet(
                summary: summary,
                useTotp: mode == 1 || security?.twoFactor?.enabled == true,
                showFund: security?.fundPasswordSet == true,
                fundRequired: security?.fundPasswordSet == true && security?.policy?.fundPasswordForWithdraw != false,
                loading: submitting,
                onSubmit: { totp, code, fund in submit(totp: totp, code: code, fund: fund) },
                onClose: { if !submitting { showVerify = false } }
            )
        }
    }

    // MARK: 表单
    @ViewBuilder private var form: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let until = lockedUntil {
                let reason = security?.withdrawLockReason.flatMap { $0.isEmpty ? nil : $0 } ?? "安全设置变更"
                WarnBanner(text: "\(reason)，\(Fmt.dateTime(until)) 前暂停提现与内部转账")
            } else {
                WarnBanner(text: "修改密码、绑定或解绑身份验证器等安全设置变更后，会进入提现保护期")
            }
            if fundPasswordMissing {
                HStack {
                    Text("提现前须先设置资金密码").font(.lb(FontSize.small)).foregroundColor(p.down)
                    Spacer()
                    Button("去设置") { Router.shared.openFeature(.security) }
                        .font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                }
            }

            PickerField(label: "币种", value: asset) { showAsset = true }

            PillSegment(items: ["链上提现", "内部转账"], selected: $mode, height: 36)

            if mode == 0 {
                VStack(alignment: .leading, spacing: 6) {
                    PickerField(label: "提现网络", value: net?.network ?? "", placeholder: networkPlaceholder) {
                        if case .failed = nets { Task { await loadNetworks() } } else { showNet = true }
                    }
                    if case .failed(let e) = nets {
                        Text("\(e)，点击上方重试").font(.lb(FontSize.small)).foregroundColor(p.down)
                    } else if let n = net {
                        Text("预计到账：\(WithdrawRules.eta(n.estimatedArrivalMinutes))\(n.confirmations.map { " · \($0) 次网络确认" } ?? "")")
                            .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                }

                LBTextField(label: "提现地址", placeholder: "输入或粘贴 \(asset) 地址", text: $address) {
                    HStack(spacing: 12) {
                        Button {
                            if let s = UIPasteboard.general.string { address = s.trimmingCharacters(in: .whitespacesAndNewlines) }
                        } label: { Text("粘贴").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary) }
                        Button { showBook = true } label: {
                            Image(systemName: "book.closed").font(.system(size: 15)).foregroundColor(p.textPrimary)
                        }.accessibilityLabel("从地址簿选择")
                    }
                }

                if memoRequired || !memo.isEmpty {
                    LBTextField(label: "Memo / Tag", placeholder: memoRequired ? "该网络必须填写 Memo / Tag" : "Memo / Tag（选填）", text: $memo)
                }
            } else {
                WarnBanner(text: "转给 Linbit 用户，免手续费、实时到账、不上链。需已绑定谷歌验证器。")
                LBTextField(label: "收款人", placeholder: "请输入接收方 UID 或邮箱", text: $recipient)
            }

            VStack(alignment: .leading, spacing: 6) {
                NumberBox(label: mode == 0 ? "提现数量" : "转账数量", unit: asset, text: $amount, onEdit: {})
                    .overlay(alignment: .trailing) {
                        Button("全部") { if let av = available, av > 0 { amount = "\(av)" } }
                            .font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary).padding(.trailing, 60)
                    }
                HStack {
                    Text("\(AssetAccount.title(withdrawAccount))可用 \(available.map { Fmt.amount($0, 8) } ?? "--") \(asset)")
                        .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                }
                if let e = amountError { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
            }

            if mode == 0 {
                AssetsCard {
                    AssetKVRow(k: "单笔最小提现", v: "\(minText ?? "--") \(asset)")
                    AssetKVRow(k: "单笔最大提现", v: maxText.map { "\($0) \(asset)" } ?? "不限")
                    AssetKVRow(k: "链上网络费（Gas）", v: "\(net?.gasFeeEstimate ?? "--") \(net?.gasFeeAsset ?? asset)")
                    AssetKVRow(k: "到账数量", v: "\(receive.map { Fmt.amount($0, 8) } ?? "--") \(asset)")
                }
            } else {
                AssetsCard {
                    AssetKVRow(k: "手续费", v: "免费")
                    AssetKVRow(k: "预计到账", v: "实时到账")
                }
            }

            Text("请确认收款地址与所选网络一致，链上转账一旦发出无法撤回。Linbit 不会以任何形式索要密码或要求私下转账，谨防诈骗。")
                .font(.lb(11)).foregroundColor(p.textTertiary)
        }
    }

    private var networkPlaceholder: String {
        switch nets {
        case .idle, .loading: return "加载中…"
        case .failed: return "加载失败，点击重试"
        case .loaded(let ns): return ns.isEmpty ? "\(asset) 暂无可提现网络" : "请选择网络"
        }
    }

    private var bottomBar: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text("预计到账").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                Text(mode == 0 ? "\(receive.map { Fmt.amount($0, 8) } ?? "0") \(asset)" : "\(Dec.parse(amount) > 0 ? amount : "0") \(asset)")
                    .font(.lbNum(FontSize.body, .bold)).foregroundColor(p.textPrimary)
                    .lineLimit(1).minimumScaleFactor(0.7)
            }
            Spacer()
            LBButton(title: mode == 0 ? "提现" : "转账", enabled: validation == nil, loading: submitting) { openVerify() }.frame(width: 140)
        }
        .padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
        .background(p.bg)
        .overlay(alignment: .top) { Divider().overlay(p.divider) }
    }

    private var summary: [(String, String)] {
        if mode == 1 {
            return [("币种", asset), ("收款人", recipient.trimmingCharacters(in: .whitespacesAndNewlines)),
                    ("转账数量", "\(amount) \(asset)"), ("手续费", "免费"), ("到账", "实时到账")]
        }
        return [("币种 / 网络", "\(asset) · \(net?.network ?? "--")"),
         ("提现地址", addr + (memo.isEmpty ? "" : "\nMemo: \(memo)")),
         ("提现数量", "\(amount) \(asset)"),
         ("网络费（Gas）", "\(net?.gasFeeEstimate ?? "--") \(net?.gasFeeAsset ?? asset)"),
         ("到账数量", "\(receive.map { Fmt.amount($0, 8) } ?? "--") \(asset)")]
    }

    // MARK: 动作
    private func openVerify() {
        session.requireLogin {
            if let v = validation { ToastCenter.shared.show(v, .error); return }
            // 验证方式取决于账户安全设置（TOTP 或邮箱/短信验证码、是否需要资金密码），必须先拿到
            if security != nil { showVerify = true; return }
            Task {
                security = try? await SecurityAPI.overview()
                if security == nil { ToastCenter.shared.show("暂时无法获取安全设置，请稍后重试", .error) } else { showVerify = true }
            }
        }
    }

    private func submitInternal(totp: String?, fund: String?) {
        guard let totp, !totp.isEmpty, !submitting else {
            if (totp ?? "").isEmpty { ToastCenter.shared.show("请输入谷歌验证码", .error) }
            return
        }
        let who = recipient.trimmingCharacters(in: .whitespacesAndNewlines)
        let email = who.contains("@")
        submitting = true
        Task {
            defer { submitting = false }
            do {
                let t = try await AssetsAPI.internalTransfer(APIInternalTransferRequest(
                    clientId: "ios-\(UUID().uuidString.lowercased())", asset: asset, amount: amount,
                    toUid: email ? nil : who, toEmail: email ? who : nil, totpCode: totp, fundPassword: fund))
                showVerify = false
                Haptics.success()
                ToastCenter.shared.show(t.status == .failed ? "转账失败" : "转账成功", t.status == .failed ? .error : .success)
                amount = ""; recipient = ""
                await AssetsModel.shared.load(silent: true)
            } catch {
                Haptics.error()
                ToastCenter.shared.show(securityMessage(error), .error)
            }
        }
    }

    private func submit(totp: String?, code: String?, fund: String?) {
        if mode == 1 { submitInternal(totp: totp, fund: fund); return }
        guard let n = net, !submitting else { return }
        submitting = true
        Task {
            defer { submitting = false }
            do {
                let w = try await AssetsAPI.withdraw(APIWithdrawRequest(
                    asset: asset, network: n.network, address: addr, amount: amount,
                    memo: memo.isEmpty ? nil : memo.trimmingCharacters(in: .whitespaces),
                    totpCode: totp, code: code, fundPassword: fund))
                showVerify = false
                Haptics.success()
                ToastCenter.shared.show(w.status == .pending ? "提现申请已提交，等待审核" : "提现已提交，正在处理", .success)
                amount = ""; address = ""; memo = ""
                Router.shared.assetsPath.append(AssetsRoute.withdrawRecords)
                await AssetsModel.shared.load(silent: true)
            } catch {
                // 保留验证弹窗，便于修正验证码 / 资金密码后重试
                Haptics.error()
                ToastCenter.shared.show(securityMessage(error), .error)
            }
        }
    }

    // MARK: 加载
    private func load() async {
        guard session.isLoggedIn else { return }
        let a = asset
        async let sec = try? SecurityAPI.overview()
        async let ov = try? AssetsAPI.overview()
        await loadNetworks()
        let o = await ov
        let s = await sec
        guard a == asset else { return }
        security = s ?? security
        if let o {
            withdrawAccount = o.withdrawAccount?.rawValue ?? "funding"
            let acc = o.accounts?.first { $0.account?.rawValue == withdrawAccount }
            available = Dec.parse(acc?.balances?.first { $0.asset == a }?.free)
        }
    }

    private func loadNetworks() async {
        let a = asset
        if nets.value == nil { nets = .loading }
        do {
            async let rec = try? AssetsAPI.walletNetworks(asset: a)
            let list = try await AssetsAPI.networks(asset: a).filter(\.withdrawEnabled)
            let recSet = Set((await rec ?? []).filter { $0.recommended == true }.compactMap(\.network))
            guard a == asset else { return }
            nets = .loaded(list)
            recommended = recSet
            // 保留仍可用的已选网络，否则默认推荐网络 → 第一个
            if let cur = net, let still = list.first(where: { $0.network == cur.network }) { net = still }
            else { net = list.first { recSet.contains($0.network) } ?? list.first }
        } catch {
            guard a == asset else { return }
            nets = .failed(error.lbMessage)
        }
    }
}

// MARK: - 提现网络选择
struct WithdrawNetworkSheet: View {
    @Environment(\.palette) private var p
    let asset: String
    let nets: [APIWithdrawNetwork]
    let recommended: Set<String>
    let selected: String?
    let onPick: (APIWithdrawNetwork) -> Void
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "选择提现网络", subtitle: "请确保与收款地址所在网络一致，选错网络可能导致资产无法找回", onClose: onClose) {
            ScrollView {
                VStack(spacing: 10) {
                    if nets.isEmpty { EmptyStateView(text: "\(asset) 暂无可提现网络") }
                    ForEach(nets, id: \.network) { n in
                        Button { onPick(n) } label: {
                            AssetsCard {
                                HStack(spacing: 6) {
                                    Text(n.network).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                                    if recommended.contains(n.network) { RecommendedChip() }
                                    if n.memoRequired { StatusTag(text: "需 Memo", color: p.brand) }
                                    Spacer()
                                    if n.network == selected { Image(systemName: "checkmark.circle.fill").foregroundColor(p.textPrimary) }
                                }
                                AssetKVRow(k: "网络费（Gas）", v: "\(n.gasFeeEstimate ?? "--") \(n.gasFeeAsset ?? asset)")
                                AssetKVRow(k: "最小提现", v: "\(n.minWithdraw ?? "--") \(asset)")
                                AssetKVRow(k: "预计到账", v: WithdrawRules.eta(n.estimatedArrivalMinutes))
                            }
                        }.buttonStyle(PressableStyle())
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }.frame(maxHeight: 520)
        }
    }
}

// MARK: - 从地址簿选择（仅显示当前币种）
struct WithdrawAddressPickSheet: View {
    @Environment(\.palette) private var p
    let asset: String
    let onPick: (APIWithdrawAddress) -> Void
    let onClose: () -> Void
    @State private var state: Loadable<[APIWithdrawAddress]> = .idle
    var body: some View {
        LBSheet(title: "从地址簿选择", onClose: onClose) {
            ScrollView {
                VStack(spacing: 10) {
                    switch state {
                    case .idle, .loading: SkeletonList(rows: 3)
                    case .failed(let e): ErrorStateView(message: e) { Task { await load() } }
                    case .loaded(let all):
                        let list = all.filter { $0.asset == asset }
                        if list.isEmpty { EmptyStateView(text: "暂无 \(asset) 提现地址") }
                        ForEach(list, id: \.id) { a in
                            Button { onPick(a) } label: {
                                AssetsCard {
                                    HStack(spacing: 6) {
                                        Text(a.label?.isEmpty == false ? a.label! : a.asset).font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                                        StatusTag(text: a.network, color: p.textSecondary)
                                        if a.whitelisted { StatusTag(text: "白名单", color: p.up) }
                                        Spacer()
                                    }
                                    Text(a.address).font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary).lineLimit(2).multilineTextAlignment(.leading)
                                }
                            }.buttonStyle(PressableStyle())
                        }
                    }
                    Button {
                        onClose(); Router.shared.assetsPath.append(AssetsRoute.addressBook)
                    } label: { Text("管理地址簿").font(.lb(FontSize.small)).foregroundColor(p.textSecondary).underline() }
                        .padding(.top, 4)
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }.frame(maxHeight: 480)
        }
        .task { await load() }
    }
    private func load() async {
        if state.value == nil { state = .loading }
        do { state = .loaded(try await AssetsAPI.addresses()) } catch { state = .failed(error.lbMessage) }
    }
}

// MARK: - 提现安全验证：已开启 TOTP 用谷歌验证码，否则用邮箱/短信验证码；按策略要求资金密码
struct WithdrawVerifySheet: View {
    @Environment(\.palette) private var p
    let summary: [(String, String)]
    let useTotp: Bool
    let showFund: Bool
    let fundRequired: Bool
    let loading: Bool
    let onSubmit: (_ totp: String?, _ code: String?, _ fund: String?) -> Void
    let onClose: () -> Void
    @State private var totp = ""
    @State private var code = ""
    @State private var fund = ""
    @State private var sending = false
    @State private var cooldown = 0

    private var ready: Bool {
        (useTotp ? totp.count == 6 : code.count == 6) && (fundRequired ? fund.count == 6 : (fund.isEmpty || fund.count == 6))
    }
    private func digits(_ s: Binding<String>) -> Binding<String> {
        Binding(get: { s.wrappedValue }, set: { s.wrappedValue = String($0.filter(\.isNumber).prefix(6)) })
    }

    var body: some View {
        LBSheet(title: "确认提现", subtitle: "为保障资金安全，请核对信息并完成安全验证", showClose: true, onClose: onClose) {
            ScrollView {
                VStack(spacing: 12) {
                    AssetsCard { ForEach(summary.indices, id: \.self) { i in AssetKVRow(k: summary[i].0, v: summary[i].1) } }
                    if useTotp {
                        LBTextField(label: "谷歌验证码", placeholder: "请输入 6 位谷歌验证码", text: digits($totp), keyboard: .numberPad)
                    } else {
                        LBTextField(label: "验证码", placeholder: "邮箱 / 短信验证码", text: digits($code), keyboard: .numberPad) {
                            Button { Task { await send() } } label: {
                                Text(cooldown > 0 ? "\(cooldown)s" : (sending ? "发送中" : "获取验证码"))
                                    .font(.lb(FontSize.small, .medium)).foregroundColor(cooldown > 0 ? p.textTertiary : p.brand)
                            }.disabled(cooldown > 0 || sending)
                        }
                    }
                    if showFund {
                        LBTextField(label: fundRequired ? "资金密码" : "资金密码（选填）", placeholder: "6 位数字资金密码",
                                    text: digits($fund), secure: true, keyboard: .numberPad)
                    }
                    LBButton(title: "确认提现", enabled: ready, loading: loading) {
                        onSubmit(useTotp ? totp : nil, useTotp ? nil : code, fund.isEmpty ? nil : fund)
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }.frame(maxHeight: 560)
        }
    }

    private func send() async {
        sending = true; defer { sending = false }
        do {
            try await AssetsAPI.sendWithdrawCode()
            ToastCenter.shared.show("验证码已发送", .success)
            cooldown = 60
            Task { while cooldown > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); cooldown -= 1 } }
        } catch { ToastCenter.shared.error(error) }
    }
}
