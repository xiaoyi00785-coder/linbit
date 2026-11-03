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
                    row("身份认证", value: (session.account?.kycLevel ?? 0) > 0 ? "已认证" : "去认证") {}
                    NavigationLink { SecurityView() } label: { rowLabel("账户安全", value: nil) }.buttonStyle(RowPressStyle())
                    row("子账户", value: nil) {
                        Router.shared.showUserCenter = false
                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.45) { Router.shared.openFeature(.subAccounts) }
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

// MARK: 账户安全（入口：身份验证器 / 修改密码）
struct SecurityView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "账户安全")
            NavigationLink { TotpSetupView() } label: {
                HStack {
                    Text("身份验证器").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                    Spacer()
                    Text(session.account?.twoFactorEnabled == true ? "已绑定" : "未绑定").font(.lb(FontSize.label))
                        .foregroundColor(session.account?.twoFactorEnabled == true ? p.up : p.textSecondary)
                    Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textSecondary)
                }.padding(.horizontal, Spacing.pageH).frame(height: 50)
            }.buttonStyle(RowPressStyle()).disabled(session.account?.twoFactorEnabled == true)
            Spacer()
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
    }
}

// MARK: 设置身份验证器（ref2 #15）
struct TotpSetupView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var binding: Loadable<APITotpBinding> = .loading
    @State private var code = ""
    @State private var loading = false
    @State private var error: String?

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "设置身份验证器")
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
            ToastCenter.shared.show("身份验证器已绑定", .success); dismiss()
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
