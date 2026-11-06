import SwiftUI

/// 模拟交易垂直切片：独立虚拟现货账户 / 余额 / 重置确认（买卖与历史下批）。
/// 契约：paper · spot · INDEX market-only；与合约真钱账户隔离；非完整合约模拟盘。
struct PaperTradingView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    @ObservedObject private var flags = AppFeatures.shared
    @StateObject private var model = PaperTradingSession()
    @State private var showReset = false
    /// Feature 壳返回；合约 Tab 内嵌时为 nil
    var onBack: (() -> Void)? = nil

    private let scopeNote = "当前为独立虚拟现货账户（mode=paper · market=spot · 价格源 INDEX · 仅市价）。与合约/现货真钱账户隔离，不是完整合约模拟盘；重置按 UTC 日限额，不涉及真实资金。"

    var body: some View {
        VStack(spacing: 0) {
            if let onBack {
                NavBar(title: "模拟交易", onBack: onBack) {
                    resetNavButton
                }
            } else {
                // 合约 Tab 内：自带顶栏操作
                HStack {
                    Text("模拟交易").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    Spacer()
                    resetNavButton
                }
                .padding(.horizontal, Spacing.pageH).frame(height: 44)
            }

            if !PaperTradingFeature.isFlagOn && !PaperTradingDemo.on {
                featureClosed
            } else if !session.isLoggedIn && !PaperTradingDemo.on {
                loginGate
            } else {
                content
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task {
            await reloadAll()
            if PaperTradingDemo.openReset { showReset = true }
        }
        .onChange(of: session.isLoggedIn) { _ in
            model.invalidate()
            Task { await reloadAll() }
        }
        .lbSheet($showReset) {
            PaperResetSheet(session: model) { ok in
                showReset = false
                if ok { Task { await reloadAll() } }
            }
        }
    }

    @ViewBuilder private var resetNavButton: some View {
        if (PaperTradingFeature.isFlagOn || PaperTradingDemo.on) && (session.isLoggedIn || PaperTradingDemo.on) {
            Button { showReset = true } label: {
                Text("重置").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    .frame(height: 44, alignment: .trailing)
            }
            .buttonStyle(PressableStyle())
            .disabled(model.isRateLimited)
        }
    }

    private var featureClosed: some View {
        ScrollView {
            VStack(spacing: 12) {
                Image(systemName: "lock.rectangle").font(.system(size: 36)).foregroundColor(p.textTertiary)
                Text("模拟交易暂未开放").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                Text("功能开关 paperTrading 未开启（缺省关闭），或服务端返回 FEATURE_DISABLED。开通后提供独立虚拟现货账户，不占用真钱余额。")
                    .font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    .multilineTextAlignment(.center)
                Text(scopeNote)
                    .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    .multilineTextAlignment(.center)
            }
            .padding(Spacing.pageH)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 48)
        }
    }

    private var loginGate: some View {
        VStack(spacing: 12) {
            Spacer()
            EmptyStateView(text: ErrorText.loginRequired)
            Button {
                session.showAuth = true
            } label: {
                Text("登录 / 注册").font(.lb(FontSize.label, .medium)).foregroundColor(p.textOnPrimary)
                    .frame(maxWidth: .infinity).frame(height: 44)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.primary))
            }
            .padding(.horizontal, Spacing.pageH)
            Spacer()
        }
    }

    @ViewBuilder private var content: some View {
        if model.isRateLimited {
            InlineBanner(text: "操作过于频繁，请 \(model.rateLimitRemainingSec) 秒后再试")
        }
        if model.featureDisabled {
            ScrollView {
                VStack(spacing: 12) {
                    Spacer().frame(height: 40)
                    Text("模拟交易暂不可用").font(.lb(FontSize.title, .medium)).foregroundColor(p.textPrimary)
                    Text(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                        .font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    Text(scopeNote).font(.lb(FontSize.small)).foregroundColor(p.textTertiary).multilineTextAlignment(.center)
                    LBButton(title: "重试", kind: .outline, height: 36) { Task { await reloadAll() } }.frame(width: 120)
                }
                .padding(Spacing.pageH)
            }
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    InfoNote(text: scopeNote)
                        .padding(.horizontal, Spacing.pageH)
                        .padding(.top, 8)

                    accountSection
                    balancesSection

                    Text("买卖下单与成交历史将在下一批接入；本页仅账户 / 余额 / 重置。DEBUG 截图夹具不写生产。")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        .padding(.horizontal, Spacing.pageH)
                        .padding(.bottom, 28)
                }
            }
            .refreshable { await reloadAll() }
        }
    }

    @ViewBuilder private var accountSection: some View {
        sectionTitle("虚拟账户")
        switch model.account {
        case .idle, .loading:
            HStack { Spacer(); ProgressView(); Spacer() }.padding(.vertical, 24)
        case let .failed(m):
            failedBlock(m) { Task { await model.reloadAccount() } }
        case let .loaded(a):
            VStack(alignment: .leading, spacing: 8) {
                kv("账户", a.accountId)
                kv("模式", "\(a.mode) · \(a.market)")
                kv("generation", "\(a.generation)")
                kv("计价", a.quoteAsset)
                kv("初始资金", a.initialBalance) // 十进制原样
                kv("价格源", a.priceSource)
                kv("订单类型", a.orderTypes.joined(separator: ", "))
                kv("今日重置", "\(a.resetsUsedToday) / \(a.resetLimitPerUtcDay)")
                if let n = a.nextResetAt {
                    kv("下次可重置", Fmt.dateTime(n))
                }
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.vertical, 8)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill).padding(.horizontal, Spacing.pageH))
        }
    }

    @ViewBuilder private var balancesSection: some View {
        sectionTitle("虚拟余额")
        switch model.balances {
        case .idle, .loading:
            HStack { Spacer(); ProgressView(); Spacer() }.padding(.vertical, 24)
        case let .failed(m):
            failedBlock(m) {
                guard !model.isRateLimited else {
                    ToastCenter.shared.show("请 \(model.rateLimitRemainingSec) 秒后再试", .error)
                    return
                }
                Task { await model.reloadBalances() }
            }
        case let .loaded(pg) where pg.items.isEmpty:
            EmptyStateView(text: "暂无虚拟余额")
                .padding(.vertical, 16)
        case let .loaded(pg):
            LazyVStack(spacing: 0) {
                ForEach(pg.items) { b in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(b.asset).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                            Text("冻结 \(b.locked)").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                        }
                        Spacer()
                        VStack(alignment: .trailing, spacing: 2) {
                            Text(b.free).font(.lbNum(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                            Text("合计 \(b.total)").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                    }
                    .padding(.horizontal, Spacing.pageH)
                    .padding(.vertical, 12)
                    Divider().overlay(p.border).padding(.horizontal, Spacing.pageH)
                }
                if model.canLoadMoreBalances {
                    HStack {
                        if model.loadingMoreBalances { ProgressView().scaleEffect(0.8) }
                        Text(model.loadingMoreBalances ? "加载中…" : "加载更多")
                            .font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    }
                    .frame(maxWidth: .infinity).frame(height: 44)
                    .onAppear { Task { await model.loadMoreBalances() } }
                }
            }
        }
    }

    private func sectionTitle(_ t: String) -> some View {
        Text(t).font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
            .padding(.horizontal, Spacing.pageH).padding(.top, 12)
    }

    private func kv(_ k: String, _ v: String) -> some View {
        HStack(alignment: .top) {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).frame(width: 88, alignment: .leading)
            Text(v).font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func failedBlock(_ m: String, retry: @escaping () -> Void) -> some View {
        VStack(spacing: 8) {
            Text(m).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary).multilineTextAlignment(.center)
            Button("重试", action: retry)
                .font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 20)
        .padding(.horizontal, Spacing.pageH)
    }

    private func reloadAll() async {
        await model.reloadAccount()
        await model.reloadBalances()
    }
}

/// 重置确认：同一次确认内幂等键稳定复用；取消无写；切会话不显示成功。
struct PaperResetSheet: View {
    @Environment(\.palette) private var p
    let session: PaperTradingSession
    let onDone: (Bool) -> Void
    @State private var busy = false
    @State private var err: String? = nil
    /// 本确认会话内不变；重试沿用，取消/关闭后下次新开再生成
    @State private var idempotencyKey: String = ""

    var body: some View {
        LBSheet(title: "重置模拟账户", showClose: true, onClose: { onDone(false) }) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text("确认将虚拟现货账户重置为初始资金？每 UTC 日限 1 次，不涉及真实资金或合约仓位。")
                        .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("confirmation = RESET_PAPER_ACCOUNT")
                        .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                    Text("Idempotency-Key（本确认内复用）\n\(idempotencyKey)")
                        .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                        .fixedSize(horizontal: false, vertical: true)
                    if session.isRateLimited {
                        InlineBanner(text: "请 \(session.rateLimitRemainingSec) 秒后再试")
                    }
                    if let err {
                        Text(err).font(.lb(FontSize.small)).foregroundColor(p.down)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    HStack(spacing: 10) {
                        LBButton(title: "取消", kind: .secondary, enabled: !busy) { onDone(false) }
                        LBButton(
                            title: "确认重置",
                            kind: .sell,
                            enabled: !busy && !session.isRateLimited && !idempotencyKey.isEmpty,
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
        .onAppear {
            if idempotencyKey.isEmpty {
                idempotencyKey = "ios.paper.reset.\(UUID().uuidString.replacingOccurrences(of: "-", with: "").prefix(24))"
            }
        }
    }

    @MainActor
    private func submit() async {
        guard !busy else { return }
        guard !session.isRateLimited else {
            err = "请 \(session.rateLimitRemainingSec) 秒后再试"
            return
        }
        busy = true
        defer { busy = false }
        do {
            let r = try await session.resetForTestOrUI(idempotencyKey: idempotencyKey)
            ToastCenter.shared.show("已重置 · generation \(r.generation)", .success)
            onDone(true)
        } catch {
            if (error as? APIFailure)?.code == "SESSION_STALE" {
                err = "登录状态已变更，未应用本次重置"
                ToastCenter.shared.show(err ?? "", .error)
                onDone(false)
                return
            }
            if PaperTradingFeature.isResetLimitExceeded(error) {
                err = ErrorText.map["PAPER_RESET_LIMIT_EXCEEDED"] ?? "每 UTC 日仅可重置一次模拟账户"
            } else if PaperTradingFeature.isDisabledFailure(error) {
                err = ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放"
            } else if let f = error as? APIFailure, f.status == 429 {
                err = "请 \(session.rateLimitRemainingSec) 秒后再试"
            } else {
                err = error.lbMessage
            }
            ToastCenter.shared.show(err ?? error.lbMessage, .error)
            // 失败保留本确认页与同一 idempotencyKey，便于重试
        }
    }
}
