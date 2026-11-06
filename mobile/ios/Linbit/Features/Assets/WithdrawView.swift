import SwiftUI

/// 链上提币（OpenAPI POST /wallet/withdraw）：币种 / 网络 / 地址 / 数量 / Memo / 二次验证与资金密码。
struct WithdrawTarget: Hashable { let asset: String }

struct WithdrawView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var session: Session
    @ObservedObject private var assets = AssetsModel.shared

    @State var asset: String
    @State private var networks: [APIWithdrawNetwork] = []
    @State private var network: APIWithdrawNetwork?
    @State private var address = ""
    @State private var memo = ""
    @State private var amount = ""
    @State private var totp = ""
    @State private var smsCode = ""
    @State private var fundPassword = ""
    @State private var saved: [APIWithdrawAddress] = []
    @State private var showAsset = false
    @State private var showNet = false
    @State private var showBook = false
    @State private var submitting = false
    @State private var sendingCode = false
    @State private var codeLeft = 0
    @State private var loadError: String?
    @State private var fieldError: String?
    @FocusState private var focus: Bool

    private var avail: Decimal {
        Dec.parse(assets.list.first { $0.asset == asset }?.free)
    }
    private var amountDec: Decimal { Dec.parse(amount) }
    private var minW: Decimal { Dec.parse(network?.minWithdraw) }
    private var maxW: Decimal? {
        guard let s = network?.maxWithdraw, !s.isEmpty else { return nil }
        return Dec.parse(s)
    }
    private var gas: Decimal { Dec.parse(network?.gasFeeEstimate) }
    /// 截图夹具可强制展示 TOTP 栏（useTotp 本身是计算属性不可写）
    @State private var shotForceTotp = false
    private var useTotp: Bool { shotForceTotp || session.account?.twoFactorEnabled == true }

    private var validation: String? {
        if network == nil { return "请选择网络" }
        if network?.withdrawEnabled == false { return "该网络暂停提现" }
        let addr = address.trimmingCharacters(in: .whitespacesAndNewlines)
        if addr.count < 10 { return "请输入有效提现地址" }
        if network?.memoRequired == true && memo.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return "该网络需填写 Memo / Tag"
        }
        if amountDec <= 0 { return "请输入提现数量" }
        if minW > 0 && amountDec < minW { return "低于最小提现 \(network?.minWithdraw ?? "")" }
        if let mx = maxW, amountDec > mx { return "超过单笔最大提现" }
        if gas > 0 && amountDec <= gas { return "数量须大于预估 Gas" }
        if amountDec > avail { return "可用余额不足" }
        if useTotp {
            if totp.count != 6 { return "请输入 6 位谷歌验证码" }
        } else if smsCode.count != 6 {
            return "请输入 6 位验证码"
        }
        return nil
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "链上提币") {
                NavigationLink(value: AssetsRoute.withdrawRecords) {
                    Image(systemName: "doc.text.magnifyingglass").foregroundColor(p.textPrimary)
                }
            }
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        if let loadError {
                            InlineBanner(text: loadError).padding(.bottom, 4)
                        }
                        Button { showAsset = true } label: {
                            HStack(spacing: 8) {
                                CoinIcon(base: asset, size: 22)
                                Text(asset).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                                Spacer()
                                Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textTertiary)
                            }
                            .padding(.horizontal, 12).frame(minHeight: 48)
                            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        }.buttonStyle(PressableStyle(scale: 1))

                        PickerField(label: "网络", value: network?.network ?? "", placeholder: "请选择网络") { showNet = true }
                        if let n = network {
                            VStack(alignment: .leading, spacing: 4) {
                                AssetKVRow(k: "最小提现", v: "\(n.minWithdraw ?? "--") \(asset)")
                                if let mx = n.maxWithdraw { AssetKVRow(k: "单笔最大", v: "\(mx) \(asset)") }
                                AssetKVRow(k: "预估 Gas", v: "\(n.gasFeeEstimate ?? "--") \(n.gasFeeAsset ?? asset)")
                                if let m = n.estimatedArrivalMinutes {
                                    AssetKVRow(k: "预计到账", v: "约 \(m) 分钟")
                                }
                            }
                            .padding(12).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill.opacity(0.6)))
                        }

                        // 小屏：地址与地址簿纵向排布，避免挤扁输入框
                        VStack(alignment: .leading, spacing: 8) {
                            LBTextField(label: "提现地址", placeholder: "粘贴或输入地址", text: $address)
                            HStack {
                                Spacer(minLength: 0)
                                Button { showBook = true } label: {
                                    Text("从地址簿选择").font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                                }
                            }
                        }
                        if network?.memoRequired == true {
                            LBTextField(label: "Memo / Tag", placeholder: "该网络必须填写", text: $memo)
                        }

                        VStack(alignment: .leading, spacing: 6) {
                            NumberBox(label: "提现数量", unit: asset, text: $amount, onEdit: { fieldError = nil })
                            HStack {
                                Text("可用 \(Fmt.amount(avail, 8)) \(asset)").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                                    .lineLimit(1).minimumScaleFactor(0.8)
                                Spacer()
                                Button("最大") {
                                    let leave = gas
                                    let v = max(0, avail - leave)
                                    amount = SpotModel.fmt(v, 8)
                                }.font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                            }
                        }

                        InlineBanner(text: "提现从资金/现货可用余额扣款；链上 Gas 从提现数量中扣除，请仔细核对地址与网络。")

                        VStack(alignment: .leading, spacing: 12) {
                            if useTotp {
                                LBTextField(label: "谷歌验证码", placeholder: "请输入 6 位谷歌验证码", text: $totp, keyboard: .numberPad)
                                    .id("sec.totp")
                            } else {
                                LBTextField(label: "安全验证码", placeholder: "6 位验证码", text: $smsCode, keyboard: .numberPad) {
                                    Button {
                                        Task { await sendCode() }
                                    } label: {
                                        Text(codeLeft > 0 ? "\(codeLeft)s" : (sendingCode ? "发送中" : "获取验证码"))
                                            .font(.lb(FontSize.small, .medium))
                                            .foregroundColor(codeLeft > 0 || sendingCode ? p.textTertiary : p.brand)
                                    }
                                    .disabled(codeLeft > 0 || sendingCode)
                                }
                                .id("sec.code")
                            }
                            LBTextField(label: "资金密码（如已设置）", placeholder: "未设置可留空", text: $fundPassword, secure: true)
                                .id("sec.fund")
                        }
                        .padding(.top, 4)

                        if let fieldError { Text(fieldError).font(.lb(FontSize.small)).foregroundColor(p.down) }
                        else if let v = validation, !amount.isEmpty || !address.isEmpty {
                            Text(v).font(.lb(FontSize.small)).foregroundColor(p.down)
                        }
                        // 给吸底按钮留出滚动空间，避免 2FA 键盘挡住确认
                        Color.clear.frame(height: 12).id("sec.bottom")
                    }
                    .padding(.horizontal, Spacing.pageH).padding(.vertical, 12).padding(.bottom, 8)
                }
                .scrollDismissesKeyboard(.interactively)
                .lbKeyboardDone(focus: $focus)
                .onChange(of: totp) { c in if useTotp && c.count >= 1 { withAnimation { proxy.scrollTo("sec.totp", anchor: .center) } } }
                .onChange(of: smsCode) { c in if !useTotp && c.count >= 1 { withAnimation { proxy.scrollTo("sec.code", anchor: .center) } } }
                .onChange(of: fundPassword) { c in if !c.isEmpty { withAnimation { proxy.scrollTo("sec.fund", anchor: .center) } } }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 0) {
                Divider().overlay(p.divider)
                LBButton(title: "确认提现", enabled: session.isLoggedIn && validation == nil, loading: submitting) {
                    session.requireLogin { Task { await submit() } }
                }
                .padding(.horizontal, Spacing.pageH)
                .padding(.top, 10)
                .padding(.bottom, 10)
            }
            .background(p.bg.ignoresSafeArea(edges: .bottom))
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await reload() }
        .onChange(of: asset) { _ in Task { await reload(resetAddress: true) } }
        .lbSheet($showAsset) {
            AssetPickSheet(current: asset, assets: preferredAssets()) { asset = $0; showAsset = false } onClose: { showAsset = false }
        }
        .lbSheet($showNet) {
            NetworkPickerSheet(asset: asset, forWithdraw: true, selected: network?.network, onPick: { n in
                showNet = false
                Task { await applyWalletNetwork(n) }
            }, onClose: { showNet = false })
        }
        .lbSheet($showBook) {
            LBSheet(title: "选择地址", onClose: { showBook = false }) {
                ScrollView {
                    VStack(spacing: 0) {
                        let list = saved.filter { $0.asset == asset && (network == nil || $0.network == network?.network) }
                        if list.isEmpty {
                            EmptyStateView(text: "暂无可用地址，请先在地址簿添加").padding(.vertical, 40)
                        } else {
                            ForEach(list, id: \.id) { a in
                                Button {
                                    address = a.address
                                    if let m = a.memo { memo = m }
                                    if let n = networks.first(where: { $0.network == a.network }) { network = n }
                                    showBook = false
                                } label: {
                                    VStack(alignment: .leading, spacing: 4) {
                                        Text(a.label ?? a.network).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                                        Text(a.address).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary).lineLimit(2)
                                    }
                                    .padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                }.buttonStyle(RowPressStyle())
                            }
                        }
                    }.padding(.bottom, 20)
                }.frame(maxHeight: 420)
            }
        }
    }

    private func preferredAssets() -> [String] {
        let fromBal = assets.list.map(\.asset)
        let base = fromBal.isEmpty ? ["USDT", "BTC", "ETH"] : fromBal
        return Array(Set(base)).sorted()
    }

    private func reload(resetAddress: Bool = false) async {
        if ShotGate.launchShot == "assets-withdraw" {
            // 仅填充表单视觉；不调用 createWithdrawal
            let n = APIWithdrawNetwork(network: "TRC20", minWithdraw: "10", maxWithdraw: "20000", gasFeeEstimate: "1", gasFeeAsset: "USDT", confirmations: 20, estimatedArrivalMinutes: 5, withdrawEnabled: true, memoRequired: false)
            networks = [n]; network = n
            address = "TXfixtureDemoAddressDoNotWithdraw0001"
            amount = "10"
            totp = "123456"
            shotForceTotp = true
            loadError = nil
            return
        }

        loadError = nil
        if resetAddress { address = ""; memo = ""; network = nil }
        do {
            async let nets = AssetsAPI.networks(asset: asset)
            async let book = try? AssetsAPI.addresses()
            async let _ = assets.load(silent: true)
            let n = try await nets
            networks = n.filter { $0.withdrawEnabled }
            saved = (await book) ?? []
            if network == nil {
                network = networks.first
            } else if let cur = network, !networks.contains(where: { $0.network == cur.network }) {
                network = networks.first
            }
            await session.reloadAccount()
        } catch {
            loadError = error.lbMessage
            networks = []
        }
    }

    private func applyWalletNetwork(_ n: APIWalletNetwork) async {
        // Prefer matching withdraw-network list entry for fees/memo flags
        if let match = networks.first(where: { $0.network == n.network }) {
            network = match
            return
        }
        // Fallback: synthesize from wallet network
        network = APIWithdrawNetwork(
            network: n.network ?? "",
            minWithdraw: n.minWithdraw,
            maxWithdraw: n.maxWithdraw,
            gasFeeEstimate: n.withdrawFee,
            gasFeeAsset: n.withdrawFeeAsset ?? asset,
            confirmations: n.confirmations,
            estimatedArrivalMinutes: n.estimatedArrivalMinutes,
            withdrawEnabled: n.withdrawEnabled != false,
            memoRequired: n.memoRequired == true
        )
    }

    private func sendCode() async {
        sendingCode = true; defer { sendingCode = false }
        do {
            try await AssetsAPI.sendWithdrawCode()
            ToastCenter.shared.show("验证码已发送", .success)
            codeLeft = 60
            Task { @MainActor in
                while codeLeft > 0 {
                    try? await Task.sleep(nanoseconds: 1_000_000_000)
                    codeLeft -= 1
                }
            }
        } catch { ToastCenter.shared.error(error) }
    }

    private func submit() async {
        focus = false
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        if let v = validation { fieldError = v; return }
        guard let net = network?.network else { return }
        submitting = true; defer { submitting = false }
        do {
            let req = APIWithdrawRequest(
                asset: asset,
                network: net,
                address: address.trimmingCharacters(in: .whitespacesAndNewlines),
                amount: SpotModel.fmt(amountDec, 8),
                memo: memo.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : memo.trimmingCharacters(in: .whitespacesAndNewlines),
                totpCode: useTotp ? totp : nil,
                code: useTotp ? nil : smsCode,
                fundPassword: fundPassword.isEmpty ? nil : fundPassword
            )
            let w = try await AssetsAPI.createWithdrawal(req)
            Haptics.success()
            ToastCenter.shared.show("提现已提交（\(WithdrawRecordsView.statusText(w.status))）", .success)
            amount = ""; totp = ""; smsCode = ""; fundPassword = ""
            await assets.load(silent: true)
            dismiss()
            Router.shared.open(.withdrawRecords)
        } catch {
            fieldError = error.lbMessage
            ToastCenter.shared.error(error)
        }
    }
}
