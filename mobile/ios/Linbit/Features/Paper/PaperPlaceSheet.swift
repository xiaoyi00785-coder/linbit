import SwiftUI

/// Paper 市价买卖表单 + 最终确认（仅 `/paper/orders`；十进制字符串原样）。
/// 同确认内 clientOrderId 稳定；busy 互斥；flag 关/切会话拒写；成功重读余额。
struct PaperPlaceSheet: View {
    @Environment(\.palette) private var p
    let session: PaperTradingSession
    var initialSide: String = "buy" // buy | sell
    let onDone: (Bool) -> Void

    @ObservedObject private var flags = AppFeatures.shared
    @State private var side: String = "buy"
    @State private var symbol: String = "BTCUSDT"
    @State private var amount: String = "" // buy→quoteOrderQty；sell→quantity
    @State private var busy = false
    @State private var formError: String? = nil
    @State private var showConfirm = false
    @State private var clientOrderId: String = ""
    @State private var lastSuccess: PaperOrder? = nil

    private var quoteAsset: String {
        if case let .loaded(a) = session.account { return a.quoteAsset }
        return "USDT"
    }

    private var generationLabel: String {
        if case let .loaded(a) = session.account { return "\(a.generation)" }
        return "—"
    }

    private var amountValid: Bool {
        let t = amount.trimmingCharacters(in: .whitespaces)
        guard !t.isEmpty, t.first != "-", t.first != "+", !t.lowercased().contains("e") else { return false }
        guard t.allSatisfy({ $0.isNumber || $0 == "." }), t.filter({ $0 == "." }).count <= 1 else { return false }
        return Dec.parse(t) > 0
    }

    private var symbolValid: Bool {
        let s = symbol.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
        return (6...20).contains(s.count) && s.allSatisfy { $0.isLetter || $0.isNumber }
    }

    var body: some View {
        LBSheet(title: "模拟市价单", showClose: true, onClose: { onDone(false) }) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    InfoNote(text: "独立虚拟现货账户 · 仅 INDEX 市价全成或全拒（由服务端指数决定）。买单填成交金额（quoteOrderQty），卖单填数量（quantity）。不走真钱 /spot/orders。")

                    if session.isRateLimited {
                        InlineBanner(text: "请 \(session.rateLimitRemainingSec) 秒后再试（Retry-After）")
                    }

                    if case let .loaded(a) = session.account {
                        Text("account \(a.accountId) · generation \(a.generation) · \(a.mode)/\(a.market)")
                            .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    fieldLabel("方向")
                    HStack(spacing: 8) {
                        sideChip("买入", "buy", p.up)
                        sideChip("卖出", "sell", p.down)
                    }

                    fieldLabel("交易对")
                    HStack {
                        TextField("BTCUSDT", text: Binding(get: { symbol }, set: { v in
                            guard !busy else { return }
                            symbol = v.uppercased().filter { $0.isLetter || $0.isNumber }
                            formError = nil
                        }))
                        .font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                        Text("spot · INDEX").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    }
                    .padding(.horizontal, 10).frame(minHeight: 44)
                    .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))

                    NumberBox(
                        label: side == "buy" ? "成交金额（\(quoteAsset)）" : "数量",
                        unit: side == "buy" ? quoteAsset : symbol.replacingOccurrences(of: "USDT", with: ""),
                        text: $amount,
                        onEdit: { formError = nil }
                    )
                    .disabled(busy)

                    Text(side == "buy"
                         ? "将作为 quoteOrderQty 原样提交（十进制字符串）。"
                         : "将作为 quantity 原样提交（十进制字符串）。")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)

                    if let formError {
                        Text(formError).font(.lb(FontSize.small)).foregroundColor(p.down)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    if let o = lastSuccess {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("最近成功（mock/正式返回）").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                            Text("\(o.orderId) · \(o.side) · qty \(o.quantity) · ref \(o.referencePrice) · INDEX")
                                .font(.lbNum(FontSize.small)).foregroundColor(p.textPrimary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .padding(10)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
                    }

                    HStack(spacing: 10) {
                        LBButton(title: "取消", kind: .secondary, enabled: !busy) { onDone(false) }
                        LBButton(
                            title: "预览确认",
                            kind: side == "buy" ? .buy : .sell,
                            enabled: canSubmitEnabled,
                            loading: false
                        ) {
                            mintClientOrderIdIfNeeded()
                            showConfirm = true
                        }
                    }
                }
                .padding(.horizontal, Spacing.pageH)
                .padding(.bottom, 28)
            }
        }
        .onAppear {
            side = initialSide
            if PaperTradingDemo.on {
                symbol = "BTCUSDT"
                amount = side == "buy" ? "100.00" : "0.010000"
            }
            if PaperTradingDemo.placeForceOk || PaperTradingDemo.placeForceFail || PaperTradingDemo.openPlace {
                mintClientOrderIdIfNeeded()
                showConfirm = true
            }
        }
        .lbSheet($showConfirm) { confirmSheet }
    }

    private var canSubmitEnabled: Bool {
        let flagOk = PaperTradingFeature.isFlagOn || PaperTradingDemo.on
        return !busy && amountValid && symbolValid && !session.isRateLimited && flagOk
    }

    private func sideChip(_ title: String, _ value: String, _ tint: Color) -> some View {
        Button {
            guard !busy else { return }
            side = value
            amount = ""
            formError = nil
            clientOrderId = "" // 改方向视为新订单意图
            lastSuccess = nil
        } label: {
            Text(title)
                .font(.lb(FontSize.small, side == value ? .medium : .regular))
                .foregroundColor(side == value ? .white : p.textPrimary)
                .frame(maxWidth: .infinity).frame(height: 36)
                .background(RoundedRectangle(cornerRadius: Radius.sm).fill(side == value ? tint : p.fill))
        }
        .buttonStyle(PressableStyle())
        .disabled(busy)
    }

    private func fieldLabel(_ t: String) -> some View {
        Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
    }

    private func mintClientOrderIdIfNeeded() {
        if clientOrderId.isEmpty {
            clientOrderId = "ios.paper.\(UUID().uuidString.replacingOccurrences(of: "-", with: "").prefix(20))"
        }
    }

    private func buildBody() -> PaperPlaceOrderRequest {
        let sym = symbol.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
        let amt = amount.trimmingCharacters(in: .whitespaces)
        if side == "buy" {
            return PaperPlaceOrderRequest(symbol: sym, side: "buy", type: "market", quantity: "", quoteOrderQty: amt, clientOrderId: clientOrderId)
        }
        return PaperPlaceOrderRequest(symbol: sym, side: "sell", type: "market", quantity: amt, quoteOrderQty: "", clientOrderId: clientOrderId)
    }

    @ViewBuilder private var confirmSheet: some View {
        LBSheet(title: "确认模拟市价单", showClose: true, onClose: { showConfirm = false }) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text("\(symbol) · spot · INDEX · market · \(side == "buy" ? "买" : "卖")")
                        .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                    Text(side == "buy" ? "quoteOrderQty \(amount) \(quoteAsset)" : "quantity \(amount)")
                        .font(.lbNum(FontSize.title)).foregroundColor(p.textPrimary)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("generation \(generationLabel) · clientOrderId\n\(clientOrderId)")
                        .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("同确认内重试沿用同一 clientOrderId；取消不提交。DEBUG 截图仅 mock，不写生产。")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        .fixedSize(horizontal: false, vertical: true)
                    if let formError {
                        Text(formError).font(.lb(FontSize.small)).foregroundColor(p.down)
                    }
                    HStack(spacing: 10) {
                        LBButton(title: "返回修改", kind: .secondary, enabled: !busy) { showConfirm = false }
                        LBButton(
                            title: "确认提交",
                            kind: side == "buy" ? .buy : .sell,
                            enabled: !busy && !session.isRateLimited && canSubmitEnabled,
                            loading: busy
                        ) {
                            Task { await submit() }
                        }
                    }
                }
                .padding(.horizontal, Spacing.pageH)
                .padding(.bottom, 28)
            }
        }
    }

    @MainActor
    private func submit() async {
        guard !busy else { return }
        // flag 刷新：提交前再读一次缺省关闸
        if !PaperTradingFeature.isFlagOn && !PaperTradingDemo.on {
            formError = ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放"
            showConfirm = false
            return
        }
        guard !session.isRateLimited else {
            formError = "请 \(session.rateLimitRemainingSec) 秒后再试"
            return
        }
        mintClientOrderIdIfNeeded()
        let body = buildBody()
        if let v = PaperTradingFeature.validatePlace(body) {
            formError = v
            showConfirm = false
            return
        }
        busy = true
        formError = nil
        defer { busy = false }
        do {
            let o = try await session.placeForTestOrUI(body)
            lastSuccess = o
            ToastCenter.shared.show("模拟成交 \(o.orderId)", .success)
            showConfirm = false
            await session.reloadBalances()
            await session.reloadAccount()
            onDone(true)
        } catch {
            if (error as? APIFailure)?.code == "SESSION_STALE" {
                formError = "登录状态已变更，未提交模拟单"
                ToastCenter.shared.show(formError ?? "", .error)
                showConfirm = false
                onDone(false)
                return
            }
            if PaperTradingFeature.isIndexUnavailable(error) {
                formError = ErrorText.map["INDEX_UNAVAILABLE"] ?? "指数价格暂不可用，请稍后再试"
            } else if PaperTradingFeature.isDisabledFailure(error) {
                formError = ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放"
            } else if let f = error as? APIFailure, f.status == 429 {
                formError = "请 \(session.rateLimitRemainingSec) 秒后再试"
            } else if let f = error as? APIFailure, f.status == 409 {
                formError = ErrorText.map["IDEMPOTENCY_CONFLICT"] ?? "重复提交冲突，请调整后重试"
            } else if (error as? APIFailure)?.code == "INSUFFICIENT_BALANCE" {
                formError = ErrorText.map["INSUFFICIENT_BALANCE"] ?? "可用余额不足"
            } else {
                formError = error.lbMessage
            }
            ToastCenter.shared.show(formError ?? error.lbMessage, .error)
            // 失败保留表单与同一 clientOrderId，便于同确认重试
        }
    }
}
