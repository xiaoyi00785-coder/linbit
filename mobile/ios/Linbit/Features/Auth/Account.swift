import SwiftUI
import UIKit
import CoreImage.CIFilterBuiltins

/// 本地多账号（ref2 #2 / #7）：展示信息存 UserDefaults，令牌按 uid 存 Keychain。母子账户待后端接口。
@MainActor
final class AccountsStore: ObservableObject {
    static let shared = AccountsStore()
    struct Saved: Codable, Identifiable, Equatable { var id: String { uid }; var uid: String; var nickname: String; var masked: String; var login: String }
    @Published private(set) var list: [Saved] = []
    private let key = "lb.accounts"

    init() { list = (UserDefaults.standard.data(forKey: key)).flatMap { try? JSONDecoder().decode([Saved].self, from: $0) } ?? [] }
    private func persist() { UserDefaults.standard.set(try? JSONEncoder().encode(list), forKey: key) }

    func remember(_ id: AccountId) {
        Task {
            guard let a = try? await AuthAPI.account() else { return }
            let s = Saved(uid: a.uid, nickname: a.nickname ?? "ec_\(a.uid)", masked: id.masked, login: id.kind == .email ? id.email : id.e164)
            list.removeAll { $0.uid == a.uid }; list.insert(s, at: 0); persist()
            Keychain.set(TokenStore.shared.accessToken, for: "lb.acct.\(a.uid).a")
            Keychain.set(TokenStore.shared.refreshToken, for: "lb.acct.\(a.uid).r")
        }
    }
    func switchTo(_ s: Saved) async {
        guard let r = Keychain.get("lb.acct.\(s.uid).r") else { Session.shared.present(.login); return }
        TokenStore.shared.accessToken = Keychain.get("lb.acct.\(s.uid).a"); TokenStore.shared.refreshToken = r
        await Session.shared.reloadAccount()
        ToastCenter.shared.show("已切换至 \(s.nickname)", .success)
    }
    func remove(_ uids: Set<String>) {
        for u in uids { Keychain.set(nil, for: "lb.acct.\(u).a"); Keychain.set(nil, for: "lb.acct.\(u).r") }
        list.removeAll { uids.contains($0.uid) }; persist()
    }
}

// MARK: 用户中心（ref2 #1）
struct UserCenterView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var theme: ThemeStore
    @State private var tab = 0
    @State private var confirm: ConfirmRequest?
    @State private var showSwitch = false
    @State private var editingNick = false
    @State private var nickDraft = ""
    @State private var nickBusy = false
    @State private var showTheme = false
    @State private var showQuote = false
    @State private var quotes: [String] = []
    @State private var quote = ""

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "") {
                Button { showSwitch = true } label: { Image(systemName: "arrow.left.arrow.right").foregroundColor(p.textPrimary) }
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    accountCard.padding(.horizontal, Spacing.pageH).padding(.top, 4)
                    securityCard.padding(.horizontal, Spacing.pageH).padding(.top, 16)
                    sectionTitle("推荐功能").padding(.top, 26)
                    grid(recommended, columns: 4).padding(.top, 18)
                    sectionTitle("常用功能").padding(.top, 30)
                    grid(common, columns: 4).padding(.top, 18)
                    aboutRow.padding(.top, 28)
                }
                .padding(.bottom, 40)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .navigationBarHidden(true)
        .background(
            NavigationLink(isActive: $showSwitch) { AccountSwitchView() } label: { EmptyView() }
        )
        .lbConfirm($confirm)
        .lbSheet($showTheme) {
            LBSheet(title: "主题模式", showClose: true, onClose: { showTheme = false }) {
                VStack(spacing: 0) {
                    ForEach(ThemeMode.allCases) { mode in
                        SheetOptionRow(title: mode.title, selected: theme.mode == mode) { theme.mode = mode; showTheme = false }
                    }
                }.padding(.bottom, 12)
            }
        }
        .lbSheet($showQuote) {
            LBSheet(title: "计价货币", showClose: true, onClose: { showQuote = false }) {
                VStack(spacing: 0) {
                    if quotes.isEmpty { Text("没有可选货币").font(.lb(FontSize.small)).foregroundColor(p.textSecondary).padding() }
                    ForEach(quotes, id: \.self) { c in
                        SheetOptionRow(title: c, selected: quote == c) { Task { await saveQuote(c) } }
                    }
                }.padding(.bottom, 12)
            }
        }
        .task { await loadQuotes() }
        .lbSheet($editingNick) {
            LBSheet(title: "修改昵称", showClose: true, onClose: { if !nickBusy { editingNick = false } }) {
                VStack(alignment: .leading, spacing: 12) {
                    LBTextField(placeholder: "1-32 个字符", text: $nickDraft)
                    Text("昵称将展示在跟单、聊天室等公开场景。").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    LBButton(title: nickBusy ? "保存中" : "保存", enabled: !nickDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !nickBusy) {
                        Task { await saveNick() }
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }
        }
    }

    // MARK: - 用户中心（按参考 App 结构：账号卡 + 安全提示卡 + 推荐功能 / 常用功能 图标宫格 + 关于）

    private struct UCItem: Identifiable {
        let id = UUID()
        let title: String
        let icon: LBIcon?
        let symbol: String
        let action: () -> Void
    }

    private var recommended: [UCItem] {
        [
            .init(title: "聊天室", icon: .gChat, symbol: "person.2.fill") { open(.chat) },
            .init(title: "福利中心", icon: .gGift, symbol: "gift.fill") { open(.welfare) },
            .init(title: "理财", icon: .gEarn, symbol: "chart.line.uptrend.xyaxis") { open(.earn) },
            .init(title: "TradFi", icon: .gTradfi, symbol: "building.columns.fill") { open(.tradfi) },
            .init(title: "跟单", icon: .gCopy, symbol: "person.2.badge.gearshape.fill") { open(.copyTrading) },
            .init(title: "策略交易", icon: .gBot, symbol: "cpu.fill") { open(.strategy) },
        ]
    }

    private var common: [UCItem] {
        [
            .init(title: "邀请好友", icon: .gGift, symbol: "person.badge.plus") { open(.partner) },
            .init(title: "卡券中心", icon: .gGift, symbol: "ticket.fill") { open(.coupons) },
            .init(title: "账户安全", icon: .gTradfi, symbol: "lock.shield.fill") { Router.shared.showUserCenter = false; DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { SecShot.raw = "sec:"; Router.shared.feature = .security } },
            .init(title: "身份认证", icon: nil, symbol: "person.text.rectangle.fill") { open(.kyc) },
            .init(title: "子账户", icon: nil, symbol: "person.3.fill") { open(.subAccounts) },
            .init(title: "切换账号", icon: nil, symbol: "arrow.left.arrow.right") { showSwitch = true },
            .init(title: "主题模式", icon: nil, symbol: "moon.fill") { showTheme = true },
            .init(title: "计价货币", icon: nil, symbol: "yensign.circle.fill") { showQuote = true },
            .init(title: "通知设置", icon: .bell, symbol: "bell.fill") { open(.notifications) },
            .init(title: "帮助中心", icon: nil, symbol: "questionmark.circle.fill") { open(.support) },
            .init(title: "客服与支持", icon: .headset, symbol: "headphones") { open(.support) },
        ]
    }

    private var accountCard: some View {
        HStack(spacing: 12) {
            ZStack(alignment: .bottomTrailing) {
                RoundedRectangle(cornerRadius: Radius.xl).fill(p.fill).frame(width: 54, height: 54)
                    .overlay(Circle().fill(p.brand).frame(width: 22).offset(y: -6))
                Circle().fill(p.bg).frame(width: 18)
                    .overlay(Image(systemName: "camera").font(.system(size: 8)).foregroundColor(p.textPrimary))
                    .offset(x: 3, y: 3)
            }
            VStack(alignment: .leading, spacing: 3) {
                Text(session.account?.nickname ?? masked).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                HStack(spacing: 6) {
                    Text("UID: \(session.account?.uid ?? "—")").font(.lbNum(FontSize.caption)).foregroundColor(p.textSecondary)
                    Image(systemName: "doc.on.doc").font(.system(size: 11)).foregroundColor(p.textSecondary)
                        .onTapGesture { UIPasteboard.general.string = session.account?.uid; ToastCenter.shared.show("UID 已复制", .success) }
                }
                let kyc = session.account?.kycLevel ?? 0
                Text(kyc > 0 ? "KYC 已认证" : "KYC 未认证")
                    .font(.lb(FontSize.small)).foregroundColor(kyc > 0 ? p.up : p.down)
                    .padding(.horizontal, 6).frame(height: 18)
                    .background(RoundedRectangle(cornerRadius: 3).fill(kyc > 0 ? p.upTint : p.downTint))
                    .contentShape(Rectangle())
                    .onTapGesture { open(.kyc) }
            }
            Spacer()
            IconView(icon: .chevronRight, size: 14, tint: p.textSecondary)
        }
        .contentShape(Rectangle())
        .onTapGesture { nickDraft = session.account?.nickname ?? ""; editingNick = true }
    }

    private var securityCard: some View {
        HStack(spacing: 10) {
            Image(systemName: "lock.shield.fill").font(.system(size: 26, weight: .light)).foregroundColor(p.textTertiary)
            VStack(alignment: .leading, spacing: 4) {
                Text("您的账户安全设置较弱，推荐您设置：").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                Text("通行密钥").font(.lb(FontSize.small)).foregroundColor(p.brand)
            }
            Spacer()
        }
        .padding(.horizontal, 14).padding(.vertical, 12)
        .overlay(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border, lineWidth: 1))
        .contentShape(Rectangle())
        .onTapGesture { SecShot.raw = "sec:passkeys"; Router.shared.showUserCenter = false; DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { Router.shared.feature = .security } }
    }

    private func sectionTitle(_ t: String) -> some View {
        Text(t).font(.lb(20, .semibold)).foregroundColor(p.textPrimary)
            .padding(.horizontal, Spacing.pageH)
    }

    private func grid(_ items: [UCItem], columns: Int) -> some View {
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 0), count: columns), spacing: 22) {
            ForEach(items) { item in
                Button(action: item.action) {
                    VStack(spacing: 7) {
                        Group {
                            if let icon = item.icon {
                                IconView(icon: icon, size: 26)
                            } else {
                                Image(systemName: item.symbol).font(.system(size: 23)).foregroundColor(p.textPrimary)
                            }
                        }
                        .frame(height: 30)
                        Text(item.title).font(.lb(FontSize.caption)).foregroundColor(p.textPrimary).lineLimit(1)
                    }
                    .frame(maxWidth: .infinity)
                }.buttonStyle(.plain)
            }
        }
        .padding(.horizontal, Spacing.pageH)
    }

    private var aboutRow: some View {
        HStack {
            Image(systemName: "info.circle").font(.system(size: 15)).foregroundColor(p.textPrimary)
            Text("关于 Linbit").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
            Spacer()
            Text(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0").font(.lbNum(FontSize.caption)).foregroundColor(p.textTertiary)
            IconView(icon: .chevronRight, size: 12, tint: p.textTertiary)
        }
        .padding(.horizontal, Spacing.pageH)
        .frame(height: 44)
    }

    private var profileRows: some View {
        VStack(spacing: 0) {
            row("UID") {
                HStack(spacing: 8) {
                    Text(session.account?.uid ?? "—").font(.lbNum(FontSize.label)).foregroundColor(p.textSecondary)
                    Image(systemName: "doc.on.doc").font(.system(size: 13)).foregroundColor(p.textSecondary)
                }
            } action: { UIPasteboard.general.string = session.account?.uid; ToastCenter.shared.show("UID 已复制", .success) }
            row("昵称", value: session.account?.nickname ?? "未设置") {
                nickDraft = session.account?.nickname ?? ""; editingNick = true
            }
            row("身份认证", value: (session.account?.kycLevel ?? 0) > 0 ? "已认证" : "去认证") { open(.kyc) }
            NavigationLink { SecurityView() } label: { rowLabel("账户安全", value: nil) }.buttonStyle(RowPressStyle())
            row("子账户", value: nil) { open(.subAccounts) }
            row("切换账号", value: nil) { showSwitch = true }
            row("福利中心", value: nil) { open(.welfare) }
            row("卡券中心", value: nil) { open(.coupons) }
            row("合伙人计划", value: nil) { open(.partner) }
            row("客服工单", value: nil) { open(.support) }
            Button {
                confirm = .init(title: "退出当前账户", message: "退出后需重新登录才能交易与查看资产", confirmTitle: "退出", destructive: true) {
                    Task { await session.logout() }
                }
            } label: { rowLabel("退出当前账户", value: nil, red: true) }.buttonStyle(RowPressStyle())
        }.padding(.top, 8)
    }

    private var preferencePane: some View {
        VStack(alignment: .leading, spacing: 0) {
            row("主题模式", value: theme.mode.title) { showTheme = true }
            row("计价货币", value: quote.isEmpty ? (session.account?.quoteCurrency ?? "系统默认") : quote) { showQuote = true }
        }.padding(.top, 8)
    }

    private func loadQuotes() async {
        guard session.isLoggedIn, let p = try? await AuthAPI.preferences() else { return }
        quotes = p.supportedQuoteCurrencies
        quote = p.quoteCurrency
    }

    private func saveQuote(_ c: String) async {
        do {
            let p = try await AuthAPI.updateQuote(c)
            quote = p.quoteCurrency
            await session.reloadAccount()
            showQuote = false
            ToastCenter.shared.show("计价货币已改为 \(p.quoteCurrency)", .success)
        } catch { ToastCenter.shared.show(error.lbMessage, .error) }
    }

    private func open(_ page: FeaturePage) {
        Router.shared.showUserCenter = false
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { Router.shared.openFeature(page) }
    }

    private func saveNick() async {
        let n = nickDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard (1...32).contains(n.count) else { ToastCenter.shared.show("昵称需为 1-32 个字符", .error); return }
        nickBusy = true
        defer { nickBusy = false }
        do {
            _ = try await SecurityAPI.updateNickname(n)
            await session.reloadAccount()
            editingNick = false
            ToastCenter.shared.show("昵称已更新", .success)
        } catch { ToastCenter.shared.show(error.lbMessage, .error) }
    }

    private var masked: String {
        guard let a = session.account else { return "" }
        return AccountId(kind: a.email != nil ? .email : .phone, email: a.email ?? "", countryCode: a.countryCode ?? "86", phone: a.phone ?? "").masked
    }
    private func row<V: View>(_ t: String, @ViewBuilder trailing: () -> V, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack { Text(t).font(.lb(FontSize.label)).foregroundColor(p.textPrimary); Spacer(); trailing() }
                .padding(.horizontal, Spacing.pageH).frame(height: 50)
        }.buttonStyle(RowPressStyle())
    }
    private func row(_ t: String, value: String?, action: @escaping () -> Void) -> some View {
        Button(action: action) { rowLabel(t, value: value) }.buttonStyle(RowPressStyle())
    }
    private func rowLabel(_ t: String, value: String?, red: Bool = false) -> some View {
        HStack(spacing: 6) {
            Text(t).font(.lb(FontSize.label)).foregroundColor(red ? p.down : p.textPrimary)
            Spacer()
            if let value { Text(value).font(.lb(FontSize.label)).foregroundColor(p.textSecondary) }
            Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textSecondary)
        }.padding(.horizontal, Spacing.pageH).frame(height: 50)
    }
}

// MARK: 账号切换（ref2 #2 普通 / #7 编辑）
struct AccountSwitchView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @ObservedObject private var store = AccountsStore.shared
    @State private var editing = false
    @State private var picked: Set<String> = []
    @State private var confirm: ConfirmRequest?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            NavBar(title: "账号切换") {
                Button { editing.toggle(); picked = [] } label: {
                    if editing { Text("完成").font(.lb(FontSize.label)).foregroundColor(p.textPrimary) }
                    else { Image(systemName: "square.and.pencil").foregroundColor(p.textPrimary) }
                }
            }
            Text("主账户").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary).padding(.horizontal, Spacing.pageH).padding(.top, 12)
            ScrollView {
                VStack(spacing: 10) {
                    ForEach(store.list) { a in card(a) }
                }.padding(Spacing.pageH)
            }
            Spacer()
            Group {
                if editing {
                    VStack(spacing: 10) {
                        LBButton(title: "删除帐户", enabled: !picked.isEmpty) { ask(picked) }
                        LBButton(title: "删除全部帐户", kind: .secondary) { ask(Set(store.list.map(\.uid))) }
                    }
                } else {
                    Button { session.present(.login) } label: {
                        HStack(spacing: 6) { Image(systemName: "plus"); Text("添加账号") }
                            .font(.lb(FontSize.body, .medium)).foregroundColor(p.textOnPrimary)
                            .frame(maxWidth: .infinity).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.primary))
                    }.buttonStyle(PressableStyle())
                }
            }.padding(Spacing.pageH)
        }
        .background(p.bg.ignoresSafeArea())
        .navigationBarHidden(true)
        .lbConfirm($confirm)
    }

    private func ask(_ ids: Set<String>) {
        confirm = .init(title: "删除账户", message: "仅从本机移除登录记录，不会注销账户。删除当前账户将退出登录。", confirmTitle: "删除", destructive: true) {
            let current = session.account?.uid
            store.remove(ids); picked = []
            if let c = current, ids.contains(c) { Task { await session.logout() } }
        }
    }

    private func card(_ a: AccountsStore.Saved) -> some View {
        let current = a.uid == session.account?.uid
        return Button {
            if editing { if picked.contains(a.uid) { picked.remove(a.uid) } else { picked.insert(a.uid) } }
            else if !current { Task { await store.switchTo(a) } }
        } label: {
            HStack(spacing: 12) {
                Circle().fill(p.fill).frame(width: 36).overlay(Circle().fill(p.brand).frame(width: 14).offset(y: -4))
                VStack(alignment: .leading, spacing: 3) {
                    Text(a.nickname).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                    Text(a.masked).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                Spacer()
                if editing {
                    RoundedRectangle(cornerRadius: 3).stroke(p.textTertiary).frame(width: 16, height: 16)
                        .background(picked.contains(a.uid) ? RoundedRectangle(cornerRadius: 3).fill(p.textPrimary) : nil)
                }
            }
            .padding(16)
            .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.bg))
            .overlay(RoundedRectangle(cornerRadius: Radius.lg).stroke(current ? p.brand : p.border, lineWidth: current ? 1.5 : 1))
            .overlay(alignment: .topTrailing) {
                if current {
                    Text("当前登录").font(.lb(10, .medium)).foregroundColor(.black).padding(.horizontal, 6).padding(.vertical, 3)
                        .background(UnevenTag().fill(p.brand))
                }
            }
        }.buttonStyle(PressableStyle(scale: 1))
    }
    struct UnevenTag: Shape {
        func path(in r: CGRect) -> Path { Path(roundedRect: r, cornerRadius: 6) }
    }
}

// MARK: 账户安全（入口 → 安全中心）
struct SecurityView: View {
    var body: some View { SecurityCenterView() }
}

// MARK: 设置身份验证器（ref2 #15）
struct TotpSetupView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    var onDone: (() -> Void)? = nil
    @State private var binding: Loadable<APITotpBinding> = .loading
    @State private var code = ""
    /// 发到已绑定邮箱/手机的安全验证码（purpose=security），开启 TOTP 必填
    @State private var securityCode = ""
    @State private var sending = false
    @State private var cooldown = 0
    @State private var loading = false
    @State private var error: String?

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "设置身份验证器", onBack: onDone)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    WarnBanner(text: "绑定身份验证器后6小时内将无法提现").padding(.bottom, 20)
                    step(1, "下载身份验证器", "从应用商店下载您可用的身份验证器。")
                    step(2, "扫描下面的二维码或手动输入密语以激活验证令牌。", nil).padding(.top, 16)
                    Group {
                        switch binding {
                        case .loading, .idle: ProgressView().frame(height: 210)
                        case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
                        case let .loaded(b):
                            VStack(spacing: 16) {
                                QRImage(text: b.otpauthUri).frame(width: 150, height: 150)
                                Button { UIPasteboard.general.string = b.secret; ToastCenter.shared.show("已复制", .success) } label: {
                                    HStack(spacing: 8) {
                                        Text(b.secret).font(.lbNum(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                                        Image(systemName: "doc.on.doc").font(.system(size: 13)).foregroundColor(p.textPrimary)
                                    }
                                    .padding(.horizontal, 12).frame(height: 38)
                                    .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
                                }.buttonStyle(PressableStyle())
                            }.frame(maxWidth: .infinity).padding(.vertical, 16)
                        }
                    }
                    step(3, "输入身份验证器验证码", nil)
                    LBTextField(placeholder: "身份验证器验证码", text: Binding(get: { code }, set: { code = String($0.filter(\.isNumber).prefix(6)) }),
                                keyboard: .numberPad).padding(.top, 10)
                    step(4, "输入安全验证码", "验证码将发送至您已绑定的邮箱或手机。").padding(.top, 16)
                    LBTextField(placeholder: "6 位安全验证码", text: Binding(get: { securityCode }, set: { securityCode = String($0.filter(\.isNumber).prefix(6)) }),
                                keyboard: .numberPad, error: error) {
                        Button { Task { await sendCode() } } label: {
                            Text(cooldown > 0 ? "\(cooldown)s" : (sending ? "发送中" : "获取验证码"))
                                .font(.lb(FontSize.small, .medium)).foregroundColor(cooldown > 0 ? p.textTertiary : p.brand)
                        }.disabled(cooldown > 0 || sending)
                    }.padding(.top, 10)
                }.padding(.horizontal, Spacing.pageH)
            }
            LBButton(title: "确认", enabled: code.count == 6 && securityCode.count == 6 && binding.value != nil, loading: loading) { Task { await verify() } }
                .padding(Spacing.pageH)
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await load() }
        .onChange(of: code) { _ in error = nil }
        .onChange(of: securityCode) { _ in error = nil }
    }
    private func step(_ n: Int, _ t: String, _ s: String?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Circle().fill(p.textPrimary).frame(width: 16).overlay(Text("\(n)").font(.lb(10, .bold)).foregroundColor(p.bg))
                Text(t).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
            }
            if let s { Text(s).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).padding(.leading, 22) }
        }
    }
    private func load() async {
        binding = .loading
        do { binding = .loaded(try await AuthAPI.bindTotp()) } catch {
            binding = .failed((error as? APIFailure)?.code == "TOTP_ALREADY_ENABLED" ? "已开启身份验证器；如需更换，请先关闭后再重新绑定" : error.lbMessage)
        }
    }
    private func sendCode() async {
        sending = true; defer { sending = false }
        do {
            try await AuthAPI.sendSecurityCode()
            ToastCenter.shared.show("验证码已发送", .success)
            cooldown = 60
            Task { while cooldown > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); cooldown -= 1 } }
        } catch { self.error = error.lbMessage }
    }
    private func verify() async {
        loading = true; defer { loading = false }
        do {
            try await AuthAPI.verifyTotp(code, securityCode: securityCode)
            await Session.shared.reloadAccount()
            ToastCenter.shared.show("身份验证器已绑定", .success); if let onDone { onDone() } else { dismiss() }
        } catch {
            switch (error as? APIFailure)?.code {
            case "TOTP_INVALID": self.error = "身份验证器验证码错误，请重新输入"
            case "CODE_INVALID", "CODE_TOO_MANY_ATTEMPTS": self.error = "安全验证码错误或已过期，请重新获取"
            case "VALIDATION_ERROR": self.error = "请先获取并输入安全验证码"
            default: self.error = error.lbMessage
            }
            Haptics.error()
        }
    }
}

/// 橙色提示条（ref2 #15/#18/#20/#22）
struct WarnBanner: View {
    let text: String
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "exclamationmark.circle").font(.system(size: 13))
            Text(text).font(.lb(FontSize.small))
            Spacer(minLength: 0)
        }
        .foregroundColor(.warnText).padding(12)
        .background(RoundedRectangle(cornerRadius: Radius.md).fill(Color.warnBg))
        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(Color.warnText.opacity(0.35)))
    }
}
extension Color {
    static let warnText = Color.hex(0xE8743B)
    static let warnBg = Color.hex(0xE8743B, 0.10)
}

/// CoreImage 生成二维码（充值地址 / otpauth）
struct QRImage: View {
    let text: String
    var body: some View {
        if let img = Self.make(text) { Image(uiImage: img).interpolation(.none).resizable().scaledToFit() }
        else { Color.clear }
    }
    static func make(_ s: String) -> UIImage? {
        let f = CIFilter.qrCodeGenerator()
        f.message = Data(s.utf8); f.correctionLevel = "H"
        guard let out = f.outputImage?.transformed(by: CGAffineTransform(scaleX: 10, y: 10)),
              let cg = CIContext().createCGImage(out, from: out.extent) else { return nil }
        return UIImage(cgImage: cg)
    }
}

// MARK: 修改登录密码（POST /account/password/change）
struct ChangePasswordView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var session: Session
    var onBack: (() -> Void)? = nil
    @State private var old = ""
    @State private var pwd = Router.shared.shotSheet == "changePwd" ? "linbit" : ""
    @State private var pwd2 = ""
    @State private var code = ""
    @State private var loading = false
    @State private var sending = false
    @State private var cooldown = 0
    @State private var error: String?

    private var useTotp: Bool { session.account?.twoFactorEnabled == true }
    private var mismatch: String? { !pwd2.isEmpty && pwd2 != pwd ? "两次输入的密码不一致" : nil }
    private var canSubmit: Bool { !old.isEmpty && PasswordRule.ok(pwd) && pwd == pwd2 && code.count == 6 }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "修改登录密码", onBack: onBack)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    InlineBanner(text: "修改成功后将退出其他设备的登录，且 24 小时内禁止提现。")
                    LBTextField(label: "原密码", placeholder: "输入当前登录密码", text: $old, secure: true)
                    VStack(alignment: .leading, spacing: 8) {
                        LBTextField(label: "新密码", placeholder: "8-64 位，含字母和数字", text: $pwd, secure: true, error: PasswordRule.hint(pwd))
                        HStack(spacing: 14) {
                            ForEach(PasswordRule.rules(pwd), id: \.0) { r in
                                HStack(spacing: 4) {
                                    Image(systemName: r.1 ? "checkmark.circle.fill" : "circle").font(.system(size: 11))
                                    Text(r.0).font(.lb(FontSize.small))
                                }.foregroundColor(r.1 ? p.up : p.textTertiary)
                            }
                        }
                    }
                    LBTextField(label: "确认新密码", placeholder: "再次输入新密码", text: $pwd2, secure: true, error: mismatch)
                    if useTotp {
                        LBTextField(label: "身份验证器验证码", placeholder: "6 位验证码", text: $code, keyboard: .numberPad)
                    } else {
                        LBTextField(label: "验证码", placeholder: "6 位验证码", text: $code, keyboard: .numberPad) {
                            Button { Task { await send() } } label: {
                                Text(cooldown > 0 ? "\(cooldown)s" : (sending ? "发送中" : "获取验证码"))
                                    .font(.lb(FontSize.small, .medium)).foregroundColor(cooldown > 0 ? p.textTertiary : p.brand)
                            }.disabled(cooldown > 0 || sending)
                        }
                    }
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    LBButton(title: "确认修改", enabled: canSubmit, loading: loading) { Task { await submit() } }.padding(.top, 8)
                }.padding(Spacing.pageH)
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .onChange(of: pwd) { _ in error = nil }
        .onChange(of: code) { v in code = String(v.filter(\.isNumber).prefix(6)); error = nil }
    }

    private func send() async {
        sending = true; defer { sending = false }
        do {
            try await AuthAPI.sendSecurityCode()
            ToastCenter.shared.show("验证码已发送", .success)
            cooldown = 60
            Task { while cooldown > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); cooldown -= 1 } }
        } catch { self.error = error.lbMessage }
    }

    private func submit() async {
        if let h = PasswordRule.hint(pwd) { error = h; return }
        guard pwd == pwd2 else { error = "两次输入的密码不一致"; return }
        loading = true; defer { loading = false }
        do {
            try await AuthAPI.changePassword(old: old, new: pwd, totp: useTotp ? code : nil, code: useTotp ? nil : code)
            ToastCenter.shared.show("登录密码已修改", .success)
            if let onBack { onBack() } else { dismiss() }
        } catch let e as APIFailure {
            switch e.code {
            case "INVALID_TOTP": error = "身份验证器验证码错误"
            case "TWOFA_REQUIRED": error = "请输入身份验证器验证码"
            case "OLD_PASSWORD_INVALID", "PASSWORD_UNCHANGED", "CODE_INVALID": error = ErrorText.map[e.code ?? ""]
            default: error = e.lbMessage
            }
        } catch { self.error = error.lbMessage }
    }
}
