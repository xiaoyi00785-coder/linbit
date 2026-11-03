import SwiftUI

/// 子账户：列表（资产汇总）/ 创建 / 母子划转 / 冻结；顶部“切换账号”
struct SubAccountsView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var list: Loadable<[SubAccount]> = .idle
    @State private var assets: SubAssetsAll?
    @State private var create = false
    @State private var transferTo: SubAccount?
    @State private var switcher = false
    @State private var confirm: ConfirmRequest?

    var body: some View {
        FeatureShell(title: "子账户", trailing: {
            Button { switcher = true } label: { Image(systemName: "person.2.circle").foregroundColor(p.textPrimary) }
        }) {
            if !session.isLoggedIn {
                EmptyBlock(text: "登录后管理子账户", actions: [("登录 / 注册", { session.present(.login) })])
            } else {
                VStack(spacing: 0) {
                    ScrollView {
                        VStack(spacing: 0) { totals; content }.padding(.bottom, 80)
                    }.refreshable { await load() }
                    LBButton(title: "创建子账户") { create = true }.padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
                }
            }
        }
        .task {
            await load()
            switch Router.shared.shotSheet {
            case "subCreate": Router.shared.shotSheet = nil; create = true
            case "accountSwitch": Router.shared.shotSheet = nil; switcher = true
            default: break
            }
        }
        .onChange(of: session.isLoggedIn) { _ in Task { await load() } }
        .lbSheet($create) { SubCreateSheet { done in create = false; if done { Task { await load() } } } }
        .lbSheet(Binding(get: { transferTo != nil }, set: { if !$0 { transferTo = nil } })) {
            if let s = transferTo { SubTransferSheet(sub: s, balances: assets?.items?.first { $0.uid == s.uid }?.balances ?? []) { done in transferTo = nil; if done { Task { await load() } } } }
        }
        .lbSheet($switcher) { AccountSwitchSheet(subs: list.value ?? []) { switcher = false } }
        .lbConfirm($confirm)
    }

    private func load() async {
        guard TokenStore.shared.isLoggedIn else { return }
        if list.value == nil { list = .loading }
        do { list = .loaded(try await SubAPI.list()) } catch { if list.value == nil { list = .failed(error.lbMessage) } }
        assets = try? await SubAPI.assets()
    }

    private var totals: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("子账户总资产").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            let t = assets?.totals ?? []
            if t.isEmpty { Text("0.00 USDT").font(.lbNum(22, .bold)).foregroundColor(p.textPrimary) }
            ForEach(t.prefix(3), id: \.asset) { b in
                Text("\(Fmt.amount(Dec.parse(b.total), 4)) \(b.asset)").font(.lbNum(b.asset == t.first?.asset ? 22 : FontSize.label, b.asset == t.first?.asset ? .bold : .regular)).foregroundColor(p.textPrimary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
        .padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
    }

    @ViewBuilder private var content: some View {
        switch list {
        case .idle, .loading: SkeletonList(rows: 4)
        case let .failed(e): ErrorStateView(message: e) { Task { await load() } }.frame(height: 300)
        case let .loaded(subs):
            if subs.isEmpty { EmptyBlock(text: "暂无子账户，可用于隔离策略与资金", actions: [("创建子账户", { create = true })]) }
            ForEach(subs) { s in
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 6) {
                        Text(s.displayName).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                        if s.frozen { Text("已冻结").font(.lb(10)).foregroundColor(p.down).padding(.horizontal, 4).background(RoundedRectangle(cornerRadius: 3).fill(p.downTint)) }
                        Spacer()
                        Menu {
                            Button("划转") { transferTo = s }.disabled(s.frozen)
                            Button(s.frozen ? "解冻" : "冻结", role: s.frozen ? nil : .destructive) { askFreeze(s) }
                        } label: { Image(systemName: "ellipsis").foregroundColor(p.textSecondary).frame(width: 32, height: 28) }
                    }
                    Text("UID \(s.uid)\(s.loginEmail.map { " · \($0)" } ?? "")").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    let bals = assets?.items?.first { $0.uid == s.uid }?.balances ?? []
                    Text(bals.isEmpty ? "暂无资产" : bals.prefix(3).map { "\($0.asset) \(Fmt.amount(Dec.parse($0.total), 4))" }.joined(separator: "  ·  "))
                        .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                    if let n = s.note, !n.isEmpty { Text(n).font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                Divider().overlay(p.divider).padding(.horizontal, Spacing.pageH)
            }
        }
    }

    private func askFreeze(_ s: SubAccount) {
        confirm = ConfirmRequest(title: s.frozen ? "解冻子账户" : "冻结子账户",
                                 message: s.frozen ? "解冻后子账户可正常登录与交易。" : "冻结后子账户将无法登录、交易和划转。",
                                 confirmTitle: s.frozen ? "解冻" : "冻结", destructive: !s.frozen) {
            Task {
                do { _ = try await SubAPI.freeze(s.uid, !s.frozen); ToastCenter.shared.show(s.frozen ? "已解冻" : "已冻结", .success); await load() }
                catch { ToastCenter.shared.error(error) }
            }
        }
    }
}

struct SubCreateSheet: View {
    @Environment(\.palette) private var p
    let onClose: (Bool) -> Void
    @State private var username = ""
    @State private var nickname = ""
    @State private var note = ""
    @State private var busy = false
    private var err: String? {
        if username.isEmpty { return nil }
        return username.range(of: "^[A-Za-z0-9_]{4,20}$", options: .regularExpression) == nil ? "4-20 位字母、数字或下划线" : nil
    }
    var body: some View {
        LBSheet(title: "创建子账户", showClose: true, onClose: { onClose(false) }) {
            VStack(spacing: 12) {
                LBTextField(label: "用户名", placeholder: "4-20 位字母、数字或下划线", text: $username, error: err)
                LBTextField(label: "昵称（选填）", placeholder: "便于识别", text: $nickname)
                LBTextField(label: "备注（选填）", placeholder: "如：网格策略专用", text: $note)
                LBButton(title: "创建", enabled: !username.isEmpty && err == nil, loading: busy) {
                    busy = true
                    Task {
                        do { _ = try await SubAPI.create(.init(username: username, nickname: nickname.isEmpty ? nil : nickname, note: note.isEmpty ? nil : note))
                             ToastCenter.shared.show("子账户已创建", .success); onClose(true) }
                        catch { ToastCenter.shared.error(error) }
                        busy = false
                    }
                }
            }.padding(.horizontal, Spacing.pageH)
        }
    }
}

struct SubTransferSheet: View {
    @Environment(\.palette) private var p
    let sub: SubAccount
    let balances: [SubBalance]
    let onClose: (Bool) -> Void
    @State private var toSub = true
    @State private var asset = "USDT"
    @State private var amount = ""
    @State private var mainAvail: Decimal = 0
    @State private var busy = false
    private var avail: Decimal { toSub ? mainAvail : Dec.parse(balances.first { $0.asset == asset }?.free) }
    var body: some View {
        LBSheet(title: "母子划转", subtitle: sub.displayName, showClose: true, onClose: { onClose(false) }) {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("从  \(toSub ? "母账户" : sub.displayName)")
                        Text("到  \(toSub ? sub.displayName : "母账户")")
                    }.font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                    Spacer()
                    Button { toSub.toggle(); amount = "" } label: { Image(systemName: "arrow.up.arrow.down").foregroundColor(p.textPrimary) }
                }.padding(12).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                NumberBox(label: "数量", unit: asset, text: $amount, onEdit: {})
                HStack {
                    Text("可用 \(Fmt.amount(avail, 6)) \(asset)").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Spacer()
                    Button("全部") { amount = Dec.plain(avail, 8) }.font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                }
                if Dec.parse(amount) > avail { Text("可用余额不足").font(.lb(FontSize.small)).foregroundColor(p.down) }
                LBButton(title: "确认划转", enabled: Dec.parse(amount) > 0 && Dec.parse(amount) <= avail, loading: busy) {
                    busy = true
                    Task {
                        do {
                            let r = try await SubAPI.transfer(.init(clientId: "ios-\(UUID().uuidString.prefix(12))", subAccountId: sub.uid,
                                                                     direction: toSub ? "to_sub" : "to_parent", asset: asset, amount: amount))
                            if let e = r.errorCode { ToastCenter.shared.show(e, .error) } else { ToastCenter.shared.show("划转成功", .success); onClose(true) }
                        } catch { ToastCenter.shared.error(error) }
                        busy = false
                    }
                }
            }.padding(.horizontal, Spacing.pageH)
        }
        .task { mainAvail = (try? await AssetsAPI.spotBalance(asset)) ?? 0 }
    }
}

/// 切换账号：当前账号 + 可登录的子账户；切换 = 退出后以目标账号登录
struct AccountSwitchSheet: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    let subs: [SubAccount]
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "切换账号", showClose: true, onClose: onClose) {
            VStack(spacing: 0) {
                SheetOptionRow(icon: "person.crop.circle", title: session.account?.nickname ?? session.account?.email ?? "当前账号",
                               subtitle: "母账户 · UID \(session.account?.uid ?? "--")", selected: true) {}
                ForEach(subs.filter { $0.loginEnabled == true && !$0.frozen }) { s in
                    SheetOptionRow(icon: "person.crop.circle.badge", title: s.displayName, subtitle: s.loginEmail ?? "UID \(s.uid)", selected: false) { switchTo() }
                }
                SheetOptionRow(icon: "plus.circle", title: "登录其他账号", selected: false) { switchTo() }
            }
        }
    }
    private func switchTo() {
        onClose()
        Task {
            await session.logout()
            Router.shared.feature = nil
            session.present(.login)
            ToastCenter.shared.show("已退出当前账号，请登录目标账号")
        }
    }
}
