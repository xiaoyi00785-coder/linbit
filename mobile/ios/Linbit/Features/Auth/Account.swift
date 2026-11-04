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
    @State private var tab = 0
    @State private var confirm: ConfirmRequest?
    @State private var showSwitch = false
    @ObservedObject private var flags = AppFeatures.shared

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "用户中心") {
                Button { showSwitch = true } label: { Image(systemName: "arrow.left.arrow.right").foregroundColor(p.textPrimary) }
            }
            ScrollView {
                VStack(spacing: 6) {
                    ZStack(alignment: .bottomTrailing) {
                        RoundedRectangle(cornerRadius: Radius.xl).fill(p.fill).frame(width: 68, height: 68)
                            .overlay(Circle().fill(p.brand).frame(width: 26).offset(y: -8))
                        Circle().fill(p.bg).frame(width: 22).overlay(Image(systemName: "camera").font(.system(size: 10)).foregroundColor(p.textPrimary)).offset(x: 4, y: 4)
                    }.padding(.top, 8)
                    Text(session.account?.nickname ?? "—").font(.lb(18, .semibold)).foregroundColor(p.textPrimary).padding(.top, 6)
                    Text(masked).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                }
                PrimaryTabs(items: ["个人资料", "偏好设置", "通知设置"], selected: $tab, size: FontSize.label, underline: true)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Spacing.pageH).padding(.top, 24)
                VStack(spacing: 0) {
                    row("UID") {
                        HStack(spacing: 8) {
                            Text(session.account?.uid ?? "—").font(.lbNum(FontSize.label)).foregroundColor(p.textSecondary)
                            Image(systemName: "doc.on.doc").font(.system(size: 13)).foregroundColor(p.textSecondary)
                        }
                    } action: { UIPasteboard.general.string = session.account?.uid; ToastCenter.shared.show("已复制", .success) }
                    row("昵称", value: session.account?.nickname ?? "") {}
                    row("身份认证", value: (session.account?.kycLevel ?? 0) > 0 ? "已认证" : "去认证") {
                        Router.shared.showUserCenter = false
                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { Router.shared.openFeature(.kyc) }
                    }
                    NavigationLink { SecurityView() } label: { rowLabel("账户安全", value: nil) }.buttonStyle(RowPressStyle())
                    row("子账户", value: nil) {
                        Router.shared.showUserCenter = false
                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { Router.shared.openFeature(.subAccounts) }
                    }
                    if flags.isOn("supportTickets") {
                        row("帮助中心 · 我的工单", value: nil) {
                            Router.shared.showUserCenter = false
                            DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { Router.shared.openFeature(.support) }
                        }
                    }
                    row("切换账号", value: nil) { showSwitch = true }
                    Button {
                        confirm = .init(title: "退出当前账户", message: "退出后需重新登录才能交易与查看资产", confirmTitle: "退出", destructive: true) {
                            Task { await session.logout() }
                        }
                    } label: { rowLabel("退出当前账户", value: nil, red: true) }.buttonStyle(RowPressStyle())
                }.padding(.top, 8)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .navigationBarHidden(true)
        .background(
            NavigationLink(isActive: $showSwitch) { AccountSwitchView() } label: { EmptyView() }
        )
        .lbConfirm($confirm)
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
                                keyboard: .numberPad, error: error).padding(.top, 10)
                }.padding(.horizontal, Spacing.pageH)
            }
            LBButton(title: "确认", enabled: code.count == 6 && binding.value != nil, loading: loading) { Task { await verify() } }
                .padding(Spacing.pageH)
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await load() }
        .onChange(of: code) { _ in error = nil }
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
        do { binding = .loaded(try await AuthAPI.bindTotp()) } catch { binding = .failed(error.lbMessage) }
    }
    private func verify() async {
        loading = true; defer { loading = false }
        do {
            try await AuthAPI.verifyTotp(code)
            await Session.shared.reloadAccount()
            ToastCenter.shared.show("身份验证器已绑定", .success); if let onDone { onDone() } else { dismiss() }
        } catch { self.error = "验证码错误，请重新输入"; Haptics.error() }
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
