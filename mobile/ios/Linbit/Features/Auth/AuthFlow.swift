import SwiftUI

/// 登录 / 注册 / 找回密码 全流程（新对照图 ref2 #3–#11；业务流程沿用网页 Auth.tsx 的接口顺序）
///  登录：账户(#3/#9) → 密码(#10) → [新设备] 验证码(#11，totp/sms/email) → 完成
///  注册：账户+邀请码+协议(#4/#5) → 发送验证码 → 验证码(#11) → 设置密码 → /auth/register → 完成
///  找回：账户 → 验证码 → 新密码×2 → 返回登录
struct AuthFlow: View {
    enum Start { case login, register }
    enum Step: Hashable { case password, verifyLogin(Int), verifyRegister, setPassword, forgot, forgotCode, forgotNew(String) }

    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State var start: Start
    @State private var path: [Step] = []
    @StateObject private var m = AuthModel()

    var body: some View {
        NavigationStack(path: $path) {
            Group {
                if start == .login { LoginAccountView(m: m, path: $path, switchTo: { start = .register }) }
                else { RegisterView(m: m, path: $path, switchTo: { start = .login }) }
            }
            .navigationBarHidden(true)
            .navigationDestination(for: Step.self) { step in
                Group {
                    switch step {
                    case .password: PasswordView(m: m, path: $path)
                    case let .verifyLogin(i): VerifyView(m: m, mode: .login(i), path: $path)
                    case .verifyRegister: VerifyView(m: m, mode: .register, path: $path)
                    case .setPassword: SetPasswordView(m: m, path: $path, mode: .register)
                    case .forgot: ForgotAccountView(m: m, path: $path)
                    case .forgotCode: VerifyView(m: m, mode: .reset, path: $path)
                    case let .forgotNew(token): SetPasswordView(m: m, path: $path, mode: .reset(token))
                    }
                }
                .navigationBarHidden(true)
            }
        }
        .background(p.bg)
        .onAppear {
            // 截图路由：直接展示注册 / 重置的设置密码页
            switch Router.shared.shotSheet {
            case "pwdRegister": m.id.email = "demo@linbit.com"; path = [.setPassword]; Router.shared.shotSheet = "pwdShotBad"
            case "pwdReset": m.id.email = "demo@linbit.com"; path = [.forgotNew("shot")]; Router.shared.shotSheet = "pwdShotGood"
            default: break
            }
        }
    }
}

@MainActor
final class AuthModel: ObservableObject {
    @Published var id = AccountId(kind: .email)
    @Published var raw = ""
    @Published var password = ""
    @Published var invite = ""
    @Published var code = ""
    @Published var challenge: APILoginChallenge?
    @Published var loading = false

    func finishLogin() async {
        UserDefaults.standard.set(id.kind == .email ? id.email : id.phone, forKey: "lb.lastLogin")
        AccountsStore.shared.remember(id)
        await Session.shared.didLogin()
    }
}

// MARK: 登录 · 账户（#3 / #9）
struct LoginAccountView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @ObservedObject var m: AuthModel
    @Binding var path: [AuthFlow.Step]
    let switchTo: () -> Void
    @State private var error: String?
    @State private var passkeyBusy = false
    @StateObject private var passkeys = RegistrarBox()

    var body: some View {
        VStack(spacing: 0) {
            AuthTopBar(isRoot: true) { session.cancelAuth() }
            ScrollView {
                VStack(spacing: 0) {
                    LinbitWordmark(height: 30).frame(maxWidth: .infinity, alignment: .leading).padding(.bottom, 12)
                    AuthHeader(title: "登录", subtitle: Text("立即登录，每日登录领红包 🧧"))
                    FieldLabel(text: "邮箱/手机号码").padding(.bottom, 8)
                    AccountField(id: $m.id, raw: $m.raw, error: error)
                    LBButton(title: "登录", enabled: !m.raw.isEmpty) {
                        guard m.id.isValid else { error = m.id.kind == .phone ? "手机号格式不正确" : "邮箱格式不正确"; return }
                        error = nil; path.append(.password)
                    }
                    .padding(.top, 28)
                    OrDivider().padding(.vertical, 24)
                    VStack(spacing: 10) {
                        SocialButton(icon: "person.badge.key", title: "使用通行密钥继续") { Task { await passkeyLogin() } }
                        SocialButton(icon: "apple.logo", title: "通过 Apple 继续") { ToastCenter.shared.show("Apple 登录需 OAuth 契约，OpenAPI 暂无端点") }
                        SocialButton(icon: "g.circle", title: "通过 Google 继续") { ToastCenter.shared.show("Google 登录需 OAuth 契约，OpenAPI 暂无端点") }
                    }
                    Button("创建账户", action: switchTo).font(.lb(FontSize.label, .medium)).foregroundColor(p.brand).padding(.top, 20)
                }
                .padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(p.bg.ignoresSafeArea())
        .onChange(of: m.raw) { _ in error = nil }
        .disabled(passkeyBusy || m.loading)
    }

    private func passkeyLogin() async {
        passkeyBusy = true
        defer { passkeyBusy = false }
        do {
            let c = try await AuthAPI.beginPasskeyLogin()
            guard let sid = c.sessionId, let opts = c.publicKey else { throw PasskeyError.badOptions }
            let cred = try await passkeys.r.assert(options: opts)
            try await AuthAPI.finishPasskeyLogin(session: sid, credential: cred)
            await m.finishLogin()
        } catch PasskeyError.cancelled {
        } catch {
            ToastCenter.shared.error(error)
            Haptics.error()
        }
    }
}

// MARK: 登录 · 密码（#10）
struct PasswordView: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: AuthModel
    @Binding var path: [AuthFlow.Step]
    @State private var error: String?
    /// 429 RATE_LIMITED：按 Retry-After 倒计时，期间禁止再次提交
    @State private var cooldown = 0
    @State private var cooldownTask: Task<Void, Never>?

    var body: some View {
        VStack(spacing: 0) {
            AuthTopBar(isRoot: false) { path.removeLast() }
            VStack(spacing: 0) {
                AuthHeader(title: "请输入密码", subtitle: Text(m.id.masked))
                FieldLabel(text: "密码").padding(.bottom, 8)
                LBTextField(placeholder: "输入您的密码", text: $m.password, secure: true, error: error)
                if cooldown > 0 {
                    InlineBanner(text: "验证码发送过于频繁，请 \(cooldown) 秒后重试").padding(.top, 12)
                }
                LBButton(title: cooldown > 0 ? "\(cooldown)s 后重试" : "下一步", enabled: !m.password.isEmpty && cooldown == 0, loading: m.loading) { Task { await submit() } }.padding(.top, 28)
                HStack { DashedLink(title: "忘记密码?") { path.append(.forgot) }; Spacer() }.padding(.top, 20)
                Spacer()
            }
            .padding(.horizontal, Spacing.pageH)
        }
        .background(p.bg.ignoresSafeArea())
        .onChange(of: m.password) { _ in error = nil }
        .onDisappear { cooldownTask?.cancel() }
    }

    private func startCooldown(_ seconds: Int) {
        cooldownTask?.cancel()
        cooldown = max(1, seconds)
        cooldownTask = Task { @MainActor in
            while cooldown > 0 {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                if Task.isCancelled { return }
                cooldown -= 1
            }
        }
    }

    private func submit() async {
        m.loading = true; defer { m.loading = false }
        do {
            switch try await AuthAPI.login(m.id, password: m.password) {
            case .done: await m.finishLogin()
            case let .challenge(c):
                m.challenge = c; m.code = ""
                path.append(.verifyLogin(0))
            }
        } catch let e as APIFailure where e.code == "AUTH_INVALID_CREDENTIALS" {
            error = "请输入正确的账户和密码"; Haptics.error()
        } catch let e as APIFailure where e.code == "RATE_LIMITED" || e.status == 429 {
            // 不进入验证码页，按 Retry-After 倒计时
            startCooldown(e.retryAfter ?? 60); Haptics.error()
        } catch let e as APIFailure where ["EMAIL_UNAVAILABLE", "SMS_UNAVAILABLE", "INVALID_EMAIL"].contains(e.code ?? "") {
            // 验证码未能发出：留在本页显示错误，不跳验证码页
            error = ErrorText.map[e.code ?? ""] ?? e.lbMessage; Haptics.error()
        } catch { ToastCenter.shared.error(error) }
    }
}

// MARK: 验证码（#11）：登录二次验证 / 注册 / 找回密码
struct VerifyView: View {
    enum Mode: Equatable { case login(Int), register, reset }
    @Environment(\.palette) private var p
    @ObservedObject var m: AuthModel
    let mode: Mode
    @Binding var path: [AuthFlow.Step]
    @State private var error = false
    @State private var showHelp = false
    @State private var methodIndex = 0

    private var method: APILoginChallenge.APIMethods {
        if case .login = mode, let ms = m.challenge?.methods, !ms.isEmpty { return ms[min(methodIndex, ms.count - 1)] }
        return m.id.kind == .phone ? .sms : .email
    }
    private var title: String { method == .totp ? "谷歌验证" : method == .sms ? "验证手机" : "验证邮箱" }
    private var subtitle: Text {
        if method == .totp { return Text("请输入谷歌验证器（Authenticator）中显示的 6 位动态码") }
        return Text("请输入\(method == .sms ? "手机" : "邮箱") ") + Text(m.id.masked).foregroundColor(p.textPrimary) + Text(" 收到的6位验证码")
    }

    var body: some View {
        VStack(spacing: 0) {
            AuthTopBar(isRoot: false) { path.removeLast() }
            ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                AuthHeader(title: title, subtitle: subtitle)
                FieldLabel(text: "验证码").padding(.bottom, 8)
                OTPBoxes(code: $m.code, error: error, resendable: method != .totp) { await resend() }
                    .id(methodIndex)
                LBButton(title: "确认", enabled: m.code.count == 6, loading: m.loading) { Task { await submit() } }.padding(.top, 28)
                HStack {
                    DashedLink(title: method == .totp ? "无法使用验证器?" : "收不到验证码?") { showHelp = true }
                    Spacer()
                    if case .login = mode, let ms = m.challenge?.methods, ms.count > 1 {
                        Button("切换验证方式") { methodIndex = (methodIndex + 1) % ms.count; m.code = "" }
                            .font(.lb(FontSize.label)).foregroundColor(p.brand)
                    }
                }.padding(.top, 20)
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(p.bg.ignoresSafeArea())
        .onChange(of: m.code) { c in error = false; if c.count == 6 && !m.loading { Task { await submit() } } }
        .lbSheet($showHelp) {
            LBSheet(title: method == .totp ? "无法使用验证器?" : "收不到验证码?", showClose: true, onClose: { showHelp = false }) {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(helpLines, id: \.self) { Text("• " + $0).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary) }
                }.padding(.horizontal, Spacing.pageH)
                LBButton(title: "我知道了") { showHelp = false }.padding(Spacing.pageH)
            }
        }
    }
    private var helpLines: [String] {
        method == .totp
            ? ["请确认手机系统时间为自动设置", "如更换手机或丢失验证器，请联系在线客服重置", "重置 2FA 后 24 小时内禁止提现"]
            : ["请检查垃圾邮件 / 短信拦截", "确认\(m.id.kind == .phone ? "手机号" : "邮箱")填写正确：\(m.id.masked)", "验证码 10 分钟内有效，60 秒后可重新发送", "仍未收到请联系在线客服"]
    }

    private func resend() async -> Bool {
        do {
            switch mode {
            case .login: if let c = m.challenge { try await AuthAPI.resendLoginCode(c) }
            case .register: try await AuthAPI.sendCode(m.id, purpose: .register)
            case .reset: try await AuthAPI.sendCode(m.id, purpose: .resetPassword)
            }
            ToastCenter.shared.show("验证码已发送", .success); return true
        } catch { ToastCenter.shared.error(error); return false }
    }

    private func submit() async {
        guard m.code.count == 6 else { return }
        m.loading = true; defer { m.loading = false }
        do {
            switch mode {
            case .login:
                guard let c = m.challenge else { return }
                try await AuthAPI.verifyLogin(c, method: method, code: m.code)
                await m.finishLogin()
            case .register:
                path.append(.setPassword)   // 注册接口需要 code + password，一并在设置密码页提交
            case .reset:
                let r = try await AuthAPI.verifyResetCode(m.id, code: m.code)
                path.append(.forgotNew(r.resetToken))
            }
        } catch {
            self.error = true; m.code = ""
            ToastCenter.shared.show(["RATE_LIMITED", "CODE_TOO_MANY_ATTEMPTS"].contains((error as? APIFailure)?.code ?? "") ? "尝试次数过多，请稍后再试" : "验证码错误或已过期", .error)
        }
    }
}

// MARK: 注册（#4 / #5 / #6 / #8）
struct RegisterView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @ObservedObject var m: AuthModel
    @Binding var path: [AuthFlow.Step]
    let switchTo: () -> Void
    @State private var error: String?
    @State private var showInvite = false
    @State private var agree = true
    @State private var showRetain = false

    var body: some View {
        VStack(spacing: 0) {
            AuthTopBar(isRoot: true) { showRetain = true }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AuthHeader(title: "创建帐户", subtitle: Text("完成注册可获得最高 ") + Text("15,200 USDT").foregroundColor(p.textPrimary))
                    FieldLabel(text: "邮箱/手机号码").padding(.bottom, 8)
                    AccountField(id: $m.id, raw: $m.raw, error: error)
                    Button { withAnimation { showInvite.toggle() } } label: {
                        HStack(spacing: 4) {
                            Text("邀请码").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                            Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)).foregroundColor(p.textPrimary)
                                .rotationEffect(.degrees(showInvite ? 180 : 0))
                        }
                    }.padding(.top, 24)
                    if showInvite { LBTextField(placeholder: "邀请码（选填）", text: $m.invite).padding(.top, 8) }
                    HStack(alignment: .top, spacing: 6) {
                        Checkbox(on: $agree)
                        (Text("我已阅读并同意 Linbit 的") + Text("服务条款").foregroundColor(p.brand) + Text("与") + Text("隐私政策").foregroundColor(p.brand))
                            .font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                    }.padding(.top, 18)
                    LBButton(title: "注册", enabled: !m.raw.isEmpty, loading: m.loading) { Task { await next() } }.padding(.top, 16)
                    OrDivider().padding(.vertical, 24)
                    VStack(spacing: 10) {
                        SocialButton(icon: "apple.logo", title: "通过 Apple 继续") { ToastCenter.shared.show("Apple 注册需 OAuth 契约，OpenAPI 暂无端点") }
                        SocialButton(icon: "g.circle", title: "通过 Google 继续") { ToastCenter.shared.show("Google 注册需 OAuth 契约，OpenAPI 暂无端点") }
                    }
                    Button("已有账户，立即登录", action: switchTo).font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
                        .frame(maxWidth: .infinity).padding(.top, 20)
                }
                .padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(p.bg.ignoresSafeArea())
        .onChange(of: m.raw) { _ in error = nil }
        // #8 挽留弹窗
        .lbSheet($showRetain) {
            LBSheet(onClose: { showRetain = false }) {
                VStack(spacing: 8) {
                    Image(systemName: "gift.fill").font(.system(size: 56)).foregroundColor(p.brand).padding(.top, 16).padding(.bottom, 8)
                    (Text("完成注册最高领 ") + Text("15,200 USDT").foregroundColor(p.brand)).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    Text("1分钟即可完成注册").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    HStack(spacing: 10) {
                        LBButton(title: "退出", kind: .secondary) { showRetain = false; session.cancelAuth() }
                        LBButton(title: "继续") { showRetain = false }
                    }.padding(.top, 20)
                }.padding(.horizontal, Spacing.pageH)
            }
        }
    }

    private func next() async {
        guard m.id.isValid else { error = m.id.kind == .phone ? "手机号格式不正确" : "邮箱格式不正确"; Haptics.error(); return }
        guard agree else { ToastCenter.shared.show("请先同意服务条款", .error); return }
        m.loading = true; defer { m.loading = false }
        do {
            try await AuthAPI.sendCode(m.id, purpose: .register)
            m.code = ""; path.append(.verifyRegister)
        } catch let e as APIFailure where e.code == "AUTH_EMAIL_EXISTS" || e.code == "AUTH_PHONE_EXISTS" {
            error = "该账户已注册，请直接登录"
        } catch { ToastCenter.shared.error(error) }
    }
}

// MARK: 设置密码（注册第 3 步 / 重置密码第 3 步）
struct SetPasswordView: View {
    enum Mode: Equatable { case register, reset(String) }
    @Environment(\.palette) private var p
    @ObservedObject var m: AuthModel
    @Binding var path: [AuthFlow.Step]
    let mode: Mode
    @State private var pwd = ShotGate.enabled && Router.shared.shotSheet == "pwdShotBad" ? "linbitpass" : (ShotGate.enabled && Router.shared.shotSheet == "pwdShotGood" ? "linbit2026" : "")
    @State private var pwd2 = ""
    @State private var error: String?

    var rules: [(String, Bool)] {
        PasswordRule.rules(pwd)
    }
    var body: some View {
        VStack(spacing: 0) {
            AuthTopBar(isRoot: false) { path.removeLast() }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AuthHeader(title: mode == .register ? "设置密码" : "重置密码",
                               subtitle: Text(mode == .register ? m.id.masked : "为保障资金安全，重置密码后 24 小时内禁止提现。"))
                    FieldLabel(text: "密码").padding(.bottom, 8)
                    LBTextField(placeholder: "8-64 位，含字母和数字", text: $pwd, secure: true, error: PasswordRule.hint(pwd))
                    HStack(spacing: 14) {
                        ForEach(rules, id: \.0) { r in
                            HStack(spacing: 4) {
                                Image(systemName: r.1 ? "checkmark.circle.fill" : "circle").font(.system(size: 11))
                                Text(r.0).font(.lb(FontSize.small))
                            }.foregroundColor(r.1 ? p.up : p.textTertiary)
                        }
                    }.padding(.top, 8)
                    if case .reset = mode {
                        FieldLabel(text: "确认密码").padding(.top, 20).padding(.bottom, 8)
                        LBTextField(placeholder: "再次输入密码", text: $pwd2, secure: true, error: error)
                    }
                    LBButton(title: "确认", enabled: PasswordRule.ok(pwd), loading: m.loading) { Task { await submit() } }.padding(.top, 28)
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(p.bg.ignoresSafeArea())
    }

    private func submit() async {
        if let h = PasswordRule.hint(pwd) { error = h; return }
        m.loading = true; defer { m.loading = false }
        do {
            switch mode {
            case .register:
                try await AuthAPI.register(m.id, password: pwd, code: m.code, invite: m.invite)
                await m.finishLogin()
                ToastCenter.shared.show("注册成功", .success)
            case let .reset(token):
                guard pwd == pwd2 else { error = "两次输入的密码不一致"; return }
                try await AuthAPI.resetPassword(token: token, newPassword: pwd)
                ToastCenter.shared.show("密码已重置，请使用新密码登录", .success)
                m.password = ""; path = [.password]
            }
        } catch let e as APIFailure where e.code == "RESET_TOKEN_INVALID" {
            ToastCenter.shared.show("重置令牌已过期，请重新获取验证码", .error); path.removeLast(2)
        } catch let e as APIFailure where e.code == "CODE_INVALID" {
            ToastCenter.shared.show("验证码错误或已过期", .error); path.removeLast()
        } catch { ToastCenter.shared.error(error) }
    }
}

// MARK: 忘记密码 · 账户
struct ForgotAccountView: View {
    @Environment(\.palette) private var p
    @ObservedObject var m: AuthModel
    @Binding var path: [AuthFlow.Step]
    @State private var error: String?
    var body: some View {
        VStack(spacing: 0) {
            AuthTopBar(isRoot: false) { path.removeLast() }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AuthHeader(title: "重置密码", subtitle: Text("为保障资金安全，重置密码后 24 小时内禁止提现。"))
                    FieldLabel(text: "邮箱/手机号码").padding(.bottom, 8)
                    AccountField(id: $m.id, raw: $m.raw, error: error)
                    LBButton(title: "下一步", enabled: !m.raw.isEmpty, loading: m.loading) {
                        Task {
                            guard m.id.isValid else { error = "请输入正确的邮箱/手机号"; return }
                            m.loading = true; defer { m.loading = false }
                            do { try await AuthAPI.sendCode(m.id, purpose: .resetPassword); m.code = ""; path.append(.forgotCode) }
                            catch { ToastCenter.shared.error(error) }
                        }
                    }.padding(.top, 28)
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(p.bg.ignoresSafeArea())
    }
}
