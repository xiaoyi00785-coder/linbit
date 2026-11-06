import SwiftUI
import UIKit

// MARK: - 账户安全中心（安全等级卡 + 分组行；子页面在本页内切换）
enum SecPage: String, Identifiable {
    case profile, email, phone, fundPassword, antiPhishing, devices, passkeys, disable2fa, changePassword, bind2fa
    var id: String { rawValue }
}

/// 截图（CI 未登录）用：Router.shotSheet = "sec:<page>[:state]"，注入演示数据。
/// raw 经 ShotRaw 闸门：Release 恒为 nil，无法由 lb.shot / 启动参数注入。
enum SecShot {
    static var raw: String? {
        get { ShotRaw.value }
        set { ShotRaw.value = newValue }
    }
    static var parts: [String] { raw.map { $0.split(separator: ":").map(String.init) } ?? [] }
    static var on: Bool { ShotGate.enabled && raw != nil }
    static var page: SecPage? { parts.count > 1 ? SecPage(rawValue: parts[1]) : nil }
    static var state: String { parts.count > 2 ? parts[2] : "" }

    static let now = Date()
    static var overview: APISecurityOverview {
        APISecurityOverview(
            securityLevel: APISecurityLevel(score: 3, max: 5, level: .medium, missing: [.phoneverified, .antiphishing],
                items: [.init(key: .emailverified, ok: true), .init(key: .phoneverified, ok: false), .init(key: .twofactor, ok: true),
                        .init(key: .fundpassword, ok: true), .init(key: .antiphishing, ok: false)]),
            twoFactor: APISecurityOverviewTwoFactor(enabled: true), email: "tra***@example.com", emailVerified: true,
            phone: nil, phoneVerified: false, fundPasswordSet: true, antiPhishingSet: false, passkeysCount: 1,
            withdrawLockedUntil: now.addingTimeInterval(18 * 3600), withdrawLockReason: "修改登录密码",
            recentDevices: devices, policy: APISecurityOverviewPolicy(fundPasswordForWithdraw: true, withdrawLockHours: 24))
    }
    static var devices: [APIDevice] { [
        APIDevice(deviceId: "ios-7f3a", name: "iPhone 16 Pro", userAgent: "Linbit iOS 0.1.0", ip: "203.0.113.7", current: true,
                  firstSeen: now.addingTimeInterval(-86400 * 30), lastSeen: now),
        APIDevice(deviceId: "cid:web-91c2", name: "Chrome on macOS", userAgent: "Mozilla/5.0", ip: "198.51.100.23", current: false,
                  firstSeen: now.addingTimeInterval(-86400 * 9), lastSeen: now.addingTimeInterval(-3600 * 5)),
    ] }
    static var sessions: [APISession] { [
        APISession(id: "s1", deviceId: "ios-7f3a", name: "iPhone 16 Pro", ip: "203.0.113.7", current: true,
                   createdAt: now.addingTimeInterval(-86400 * 2), expiresAt: now.addingTimeInterval(86400 * 28)),
        APISession(id: "s2", deviceId: "cid:web-91c2", name: "Chrome on macOS", ip: "198.51.100.23", current: false,
                   createdAt: now.addingTimeInterval(-86400 * 9), expiresAt: now.addingTimeInterval(86400 * 21)),
        APISession(id: "s3", deviceId: "and-11b0", name: "Pixel 9", ip: "192.0.2.44", current: false,
                   createdAt: now.addingTimeInterval(-86400 * 12), expiresAt: now.addingTimeInterval(86400 * 18)),
    ] }
    static var passkeys: [APIPasskey] { [
        APIPasskey(id: "pk1", name: "iPhone 16 Pro", createdAt: now.addingTimeInterval(-86400 * 20), lastUsedAt: now.addingTimeInterval(-3600 * 3)),
        APIPasskey(id: "pk2", name: "MacBook Air", createdAt: now.addingTimeInterval(-86400 * 40), lastUsedAt: nil),
    ] }
}

struct SecurityCenterView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    var onBack: (() -> Void)? = nil
    @State private var data: Loadable<APISecurityOverview> = .idle
    @State private var page: SecPage? = SecShot.page

    var body: some View {
        ZStack {
            VStack(spacing: 0) {
                NavBar(title: "账户安全", onBack: onBack)
                content
            }
            .background(p.bg.ignoresSafeArea())
            if let pg = page {
                sub(pg).background(p.bg.ignoresSafeArea()).transition(.move(edge: .trailing)).zIndex(1)
            }
        }
        .animation(.easeOut(duration: 0.22), value: page)
        .navigationBarHidden(true)
        .task { await load() }
    }

    @ViewBuilder private var content: some View {
        switch data {
        case .idle, .loading: SkeletonList(rows: 7)
        case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
        case let .loaded(o):
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if let until = o.withdrawLockedUntil, until > Date() {
                        WarnBanner(text: "\(o.withdrawLockReason ?? "安全设置变更")，\(Fmt.dateTime(until)) 前暂停提现与内部转账")
                            .padding(.horizontal, Spacing.pageH).padding(.top, 8)
                    }
                    if let lv = o.securityLevel { SecurityLevelCard(level: lv).padding(Spacing.pageH) }
                    group("账户信息")
                    row("个人资料", value: session.account?.nickname ?? "", icon: "person") { page = .profile }
                    row("邮箱", value: o.email ?? "未绑定", ok: o.emailVerified == true, icon: "envelope") { page = .email }
                    row("手机", value: o.phone ?? "未绑定", ok: o.phoneVerified == true, icon: "iphone") { page = .phone }
                    group("登录验证")
                    row("身份验证器", value: o.twoFactor?.enabled == true ? "已开启" : "未开启", ok: o.twoFactor?.enabled == true, icon: "lock.shield") {
                        page = o.twoFactor?.enabled == true ? .disable2fa : .bind2fa
                    }
                    row("通行密钥", value: (o.passkeysCount ?? 0) > 0 ? "\(o.passkeysCount ?? 0) 个" : "未添加", ok: (o.passkeysCount ?? 0) > 0, icon: "person.badge.key") { page = .passkeys }
                    row("登录密码", value: "修改", icon: "key") { page = .changePassword }
                    group("资金安全")
                    row("资金密码", value: fundText(o), ok: o.fundPasswordSet == true, icon: "lock") { page = .fundPassword }
                    row("防钓鱼码", value: o.antiPhishingSet == true ? (o.antiPhishingHint ?? "已设置") : "未设置", ok: o.antiPhishingSet == true, icon: "checkmark.seal") { page = .antiPhishing }
                    group("设备管理")
                    row("登录设备与会话", value: "\(o.recentDevices?.count ?? 0) 台设备", icon: "laptopcomputer.and.iphone") { page = .devices }
                    Spacer(minLength: 40)
                }
            }
            .refreshable { await load(silent: true) }
        }
    }

    private func fundText(_ o: APISecurityOverview) -> String {
        if let l = o.fundPasswordLockedUntil, l > Date() { return "已锁定" }
        return o.fundPasswordSet == true ? "已设置" : "未设置"
    }

    @ViewBuilder private func sub(_ pg: SecPage) -> some View {
        let o = data.value ?? APISecurityOverview()
        let twofa = o.twoFactor?.enabled == true
        let back: () -> Void = { page = nil; Task { await load(silent: true) } }
        switch pg {
        case .profile: ProfileEditView(onBack: back)
        case .email: ChangeContactView(kind: .email, current: o.email, twofa: twofa, onBack: back)
        case .phone: ChangeContactView(kind: .phone, current: o.phone, twofa: twofa, onBack: back)
        case .fundPassword: FundPasswordView(isSet: o.fundPasswordSet == true, lockedUntil: o.fundPasswordLockedUntil, twofa: twofa, onBack: back)
        case .antiPhishing: AntiPhishingView(hint: o.antiPhishingSet == true ? o.antiPhishingHint : nil, twofa: twofa, onBack: back)
        case .devices: DevicesSessionsView(onBack: back)
        case .passkeys: PasskeysView(twofa: twofa, onBack: back)
        case .disable2fa: DisableTotpView(onBack: back)
        case .bind2fa: TotpSetupView(onDone: back)
        case .changePassword: ChangePasswordView(onBack: back)
        }
    }

    private func load(silent: Bool = false) async {
        if SecShot.on {
            if SecShot.page == nil && SecShot.state == "error" { data = .failed("网络连接失败，请检查网络"); return }
            if SecShot.page == nil && SecShot.state == "loading" { data = .loading; return }
            data = .loaded(SecShot.overview); return
        }
        if !silent || data.value == nil { data = .loading }
        do { data = .loaded(try await SecurityAPI.overview()) } catch { if !silent || data.value == nil { data = .failed(error.lbMessage) } }
    }

    private func group(_ t: String) -> some View {
        Text(t).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
            .padding(.horizontal, Spacing.pageH).padding(.top, 18).padding(.bottom, 4)
    }
    private func row(_ t: String, value: String, ok: Bool? = nil, icon: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: icon).font(.system(size: 15)).foregroundColor(p.textPrimary).frame(width: 22)
                Text(t).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                Spacer()
                if let ok, !ok { Circle().fill(Color.warnText).frame(width: 6, height: 6) }
                Text(value).font(.lb(FontSize.caption)).foregroundColor(ok == true ? p.up : p.textSecondary).lineLimit(1)
                Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textTertiary)
            }.padding(.horizontal, Spacing.pageH).frame(height: 52).contentShape(Rectangle())
        }.buttonStyle(RowPressStyle())
    }
}


struct SecurityLevelCard: View {
    @Environment(\.palette) private var p
    let level: APISecurityLevel
    private var color: Color { level.level == .high ? p.up : (level.level == .medium ? Color.warnText : p.down) }
    private var text: String { level.level == .high ? "高" : (level.level == .medium ? "中" : "低") }
    private static let names: [APISecurityLevelItemsInner.APIKey: String] = [
        .emailverified: "邮箱验证", .phoneverified: "手机验证", .twofactor: "二次验证", .fundpassword: "资金密码", .antiphishing: "防钓鱼码",
    ]
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text("安全等级").font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
                Text(text).font(.lb(22, .bold)).foregroundColor(color)
                Spacer()
                Text("\(level.score)/\(level.max)").font(.lbNum(FontSize.label, .medium)).foregroundColor(p.textSecondary)
            }
            HStack(spacing: 4) {
                ForEach(0..<max(level.max, 1), id: \.self) { i in
                    Capsule().fill(i < level.score ? color : p.fillStrong).frame(height: 4)
                }
            }
            let items = level.items ?? []
            LazyVGrid(columns: [GridItem(.flexible(), alignment: .leading), GridItem(.flexible(), alignment: .leading)], spacing: 8) {
                ForEach(items, id: \.key) { it in
                    HStack(spacing: 5) {
                        Image(systemName: it.ok ? "checkmark.circle.fill" : "circle").font(.system(size: 12))
                            .foregroundColor(it.ok ? p.up : p.textTertiary)
                        Text(Self.names[it.key] ?? it.key.rawValue).font(.lb(FontSize.small)).foregroundColor(it.ok ? p.textPrimary : p.textSecondary)
                    }
                }
            }
        }
        .padding(16)
        .background(RoundedRectangle(cornerRadius: Radius.xl).fill(p.fill))
    }
}

// MARK: - 通用：安全验证（已开 2FA 用身份验证器，否则发送 purpose=security 验证码）
struct SecVerifyField: View {
    @Environment(\.palette) private var p
    let twofa: Bool
    @Binding var code: String
    var label: String? = nil
    @State private var sending = false
    @State private var cooldown = 0
    @State private var err: String?
    var body: some View {
        Group {
            if twofa {
                LBTextField(label: label ?? "身份验证器验证码", placeholder: "6 位身份验证器验证码", text: digits, keyboard: .numberPad, error: err)
            } else {
                LBTextField(label: label ?? "安全验证码", placeholder: "6 位验证码", text: digits, keyboard: .numberPad, error: err) {
                    Button { Task { await send() } } label: {
                        Text(cooldown > 0 ? "\(cooldown)s" : (sending ? "发送中" : "获取验证码"))
                            .font(.lb(FontSize.small, .medium)).foregroundColor(cooldown > 0 ? p.textTertiary : p.brand)
                    }.disabled(cooldown > 0 || sending)
                }
            }
        }
    }
    private var digits: Binding<String> { Binding(get: { code }, set: { code = String($0.filter(\.isNumber).prefix(6)); err = nil }) }
    private func send() async {
        sending = true; defer { sending = false }
        do {
            try await SecurityAPI.sendSecurityCode()
            ToastCenter.shared.show("验证码已发送", .success)
            cooldown = 60
            Task { while cooldown > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); cooldown -= 1 } }
        } catch { err = securityMessage(error) }
    }
}

/// 子页通用骨架：导航 + 可滚动表单 + 底部主按钮
struct SecForm<Content: View>: View {
    @Environment(\.palette) private var p
    let title: String
    let onBack: () -> Void
    var error: String? = nil
    var button: String? = nil
    var enabled = true
    var loading = false
    var action: () -> Void = {}
    @ViewBuilder var content: Content
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: title, onBack: onBack)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    content
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                }.padding(Spacing.pageH)
            }
            if let button {
                LBButton(title: button, enabled: enabled, loading: loading, action: action).padding(Spacing.pageH)
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
    }
}

// MARK: - 个人资料（PATCH /account/profile）
struct ProfileEditView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    let onBack: () -> Void
    @State private var nick = SecShot.on ? "lb_Trader" : ""
    @State private var loading = false
    @State private var error: String? = SecShot.state == "error" ? "昵称长度需为 1–32 个字符" : nil
    private var hint: String? { nick.trimmingCharacters(in: .whitespaces).isEmpty ? nil : (nick.count > 32 ? "昵称最多 32 个字符" : nil) }
    var body: some View {
        SecForm(title: "个人资料", onBack: onBack, error: error, button: "保存",
                enabled: !nick.trimmingCharacters(in: .whitespaces).isEmpty && hint == nil && nick != session.account?.nickname,
                loading: loading, action: { Task { await save() } }) {
            AvatarEditor()
            LBTextField(label: "昵称", placeholder: "1-32 个字符", text: $nick, error: hint)
            Text("昵称将展示在跟单、聊天室等公开场景。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }
        .onAppear { if nick.isEmpty { nick = session.account?.nickname ?? "" } }
        .onChange(of: nick) { _ in error = nil }
    }
    private func save() async {
        loading = true; defer { loading = false }
        do {
            _ = try await SecurityAPI.updateNickname(nick.trimmingCharacters(in: .whitespaces))
            await session.reloadAccount()
            ToastCenter.shared.show("资料已更新", .success); onBack()
        } catch { self.error = securityMessage(error) }
    }
}

// MARK: - 绑定 / 修改邮箱、手机（新旧双重验证）
struct ChangeContactView: View {
    enum Kind { case email, phone }
    @Environment(\.palette) private var p
    let kind: Kind
    let current: String?
    let twofa: Bool
    let onBack: () -> Void
    @State private var value = ""
    @State private var cc = "86"
    @State private var newCode = ""
    @State private var oldCode = ""
    @State private var totp = ""
    @State private var sending = false
    @State private var cooldown = 0
    @State private var loading = false
    @State private var error: String?

    init(kind: Kind, current: String?, twofa: Bool, onBack: @escaping () -> Void) {
        self.kind = kind; self.current = current; self.twofa = twofa; self.onBack = onBack
        if SecShot.page == (kind == .email ? .email : .phone) {
            _value = State(initialValue: kind == .email ? "new.trader@example" : "13800138000")
            if SecShot.state == "error" { _error = State(initialValue: ErrorText.map[kind == .email ? "EMAIL_EXISTS" : "PHONE_EXISTS"]) }
        }
    }
    private var name: String { kind == .email ? "邮箱" : "手机号" }
    private var valid: Bool {
        kind == .email ? AccountId(kind: .email, email: value).isValid : AccountId(kind: .phone, countryCode: cc, phone: value).isValid
    }
    private var fieldError: String? { value.isEmpty || valid ? nil : (kind == .email ? "邮箱格式不正确" : "手机号格式不正确") }
    private var canSubmit: Bool { valid && newCode.count == 6 && oldCode.count == 6 && (!twofa || totp.count == 6) }

    var body: some View {
        SecForm(title: current == nil ? "绑定\(name)" : "修改\(name)", onBack: onBack, error: error, button: "确认",
                enabled: canSubmit, loading: loading, action: { Task { await submit() } }) {
            WarnBanner(text: "为保障资金安全，修改后 24 小时内将暂停提现与内部转账。")
            if let current { kv("当前\(name)", current) }
            if kind == .phone {
                HStack(alignment: .top, spacing: 8) {
                    LBTextField(label: "区号", placeholder: "+86", text: Binding(get: { "+" + cc }, set: { cc = String($0.filter(\.isNumber).prefix(4)) }),
                                keyboard: .phonePad).frame(width: 86)
                    LBTextField(label: "新手机号", placeholder: "输入新手机号", text: $value, keyboard: .phonePad, error: fieldError)
                }
            } else {
                LBTextField(label: "新邮箱", placeholder: "输入新邮箱地址", text: $value, keyboard: .emailAddress, error: fieldError)
            }
            LBTextField(label: "新\(name)验证码", placeholder: "6 位验证码", text: digits($newCode), keyboard: .numberPad) {
                Button { Task { await sendNew() } } label: {
                    Text(cooldown > 0 ? "\(cooldown)s" : (sending ? "发送中" : "获取验证码"))
                        .font(.lb(FontSize.small, .medium)).foregroundColor(cooldown > 0 || !valid ? p.textTertiary : p.brand)
                }.disabled(cooldown > 0 || sending || !valid)
            }
            SecVerifyField(twofa: false, code: $oldCode, label: current == nil ? "账户验证码" : "原\(name)验证码")
            if twofa { SecVerifyField(twofa: true, code: $totp) }
        }
        .onChange(of: value) { _ in error = nil }
    }
    private func kv(_ k: String, _ v: String) -> some View {
        HStack { Text(k).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary); Spacer(); Text(v).font(.lb(FontSize.caption)).foregroundColor(p.textPrimary) }
    }
    private func digits(_ b: Binding<String>) -> Binding<String> { Binding(get: { b.wrappedValue }, set: { b.wrappedValue = String($0.filter(\.isNumber).prefix(6)); error = nil }) }
    private var phoneDigits: String { AccountId(kind: .phone, countryCode: cc, phone: value).phoneDigits }
    private func sendNew() async {
        sending = true; defer { sending = false }
        do {
            if kind == .email { try await SecurityAPI.sendContactCode(email: value.trimmingCharacters(in: .whitespaces), phone: nil, cc: nil) }
            else { try await SecurityAPI.sendContactCode(email: nil, phone: phoneDigits, cc: "+" + cc) }
            ToastCenter.shared.show("验证码已发送", .success)
            cooldown = 60
            Task { while cooldown > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); cooldown -= 1 } }
        } catch { self.error = securityMessage(error) }
    }
    private func submit() async {
        loading = true; defer { loading = false }
        do {
            if kind == .email { try await SecurityAPI.changeEmail(value.trimmingCharacters(in: .whitespaces), newCode: newCode, oldCode: oldCode, totp: twofa ? totp : nil) }
            else { try await SecurityAPI.changePhone(phoneDigits, cc: "+" + cc, newCode: newCode, oldCode: oldCode, totp: twofa ? totp : nil) }
            await Session.shared.reloadAccount()
            ToastCenter.shared.show("\(name)已更新", .success); onBack()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

// MARK: - 资金密码（6 位数字：设置 / 修改 / 忘记重置）
struct FundPasswordView: View {
    @Environment(\.palette) private var p
    let isSet: Bool
    let lockedUntil: Date?
    let twofa: Bool
    let onBack: () -> Void
    @State private var reset = false
    @State private var old = ""
    @State private var pwd = ""
    @State private var pwd2 = ""
    @State private var code = ""
    @State private var loading = false
    @State private var error: String?

    init(isSet: Bool, lockedUntil: Date?, twofa: Bool, onBack: @escaping () -> Void) {
        self.isSet = isSet; self.lockedUntil = lockedUntil; self.twofa = twofa; self.onBack = onBack
        if SecShot.page == .fundPassword {
            _old = State(initialValue: "123456"); _pwd = State(initialValue: "112233"); _pwd2 = State(initialValue: "112235")
            if SecShot.state == "error" { _error = State(initialValue: ErrorText.map["FUND_PASSWORD_INVALID"]) }
            if SecShot.state == "reset" { _reset = State(initialValue: true) }
        }
    }
    private var mode: SecurityAPI.FundMode { !isSet ? .set : (reset ? .reset : .change) }
    private var locked: Bool { (lockedUntil ?? .distantPast) > Date() }
    private var mismatch: String? { !pwd2.isEmpty && pwd2 != pwd ? "两次输入的资金密码不一致" : nil }
    private var weak: String? {
        guard pwd.count == 6 else { return nil }
        if Set(pwd).count == 1 { return "资金密码过于简单，请勿使用相同数字" }
        return nil
    }
    private var canSubmit: Bool { pwd.count == 6 && pwd == pwd2 && weak == nil && code.count == 6 && (mode != .change || old.count == 6) }
    private var title: String { mode == .set ? "设置资金密码" : (mode == .reset ? "重置资金密码" : "修改资金密码") }

    var body: some View {
        SecForm(title: title, onBack: { if reset && isSet && !SecShot.on { reset = false } else { onBack() } }, error: error,
                button: "确认", enabled: canSubmit, loading: loading, action: { Task { await submit() } }) {
            if locked && mode == .change {
                WarnBanner(text: "资金密码错误次数过多，已锁定至 \(Fmt.dateTime(lockedUntil!))。可通过「忘记资金密码」重置。")
            } else {
                InfoNote(text: "资金密码用于提现、内部转账等资金操作，须为 6 位数字且不能与登录密码相同。修改或重置后 24 小时内暂停提现。")
            }
            if mode == .change {
                LBTextField(label: "原资金密码", placeholder: "6 位数字", text: six($old), secure: true, keyboard: .numberPad)
            }
            LBTextField(label: "新资金密码", placeholder: "6 位数字", text: six($pwd), secure: true, keyboard: .numberPad, error: weak)
            LBTextField(label: "确认资金密码", placeholder: "再次输入", text: six($pwd2), secure: true, keyboard: .numberPad, error: mismatch)
            SecVerifyField(twofa: twofa, code: $code)
            if mode == .change {
                Button { reset = true; old = ""; error = nil } label: {
                    Text("忘记资金密码？").font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                }
            }
        }
    }
    private func six(_ b: Binding<String>) -> Binding<String> { Binding(get: { b.wrappedValue }, set: { b.wrappedValue = String($0.filter(\.isNumber).prefix(6)); error = nil }) }
    private func submit() async {
        loading = true; defer { loading = false }
        do {
            try await SecurityAPI.fundPassword(mode, pwd: pwd, old: mode == .change ? old : nil, totp: twofa ? code : nil, code: twofa ? nil : code)
            ToastCenter.shared.show(mode == .set ? "资金密码已设置" : (mode == .reset ? "资金密码已重置" : "资金密码已修改"), .success); onBack()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

struct InfoNote: View {
    @Environment(\.palette) private var p
    let text: String
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "info.circle").font(.system(size: 13))
            Text(text).font(.lb(FontSize.small)).fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .foregroundColor(p.textSecondary).padding(12)
        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
    }
}

// MARK: - 防钓鱼码（4-20 位字母或数字）
struct AntiPhishingView: View {
    @Environment(\.palette) private var p
    let hint: String?
    let twofa: Bool
    let onBack: () -> Void
    @State private var value = SecShot.page == .antiPhishing ? (SecShot.state == "error" ? "Lb_26" : "Lb2026") : ""
    @State private var code = ""
    @State private var loading = false
    @State private var error: String?
    static func check(_ s: String) -> String? {
        if s.isEmpty { return nil }
        if s.range(of: "^[A-Za-z0-9]+$", options: .regularExpression) == nil { return "仅支持英文字母和数字" }
        if s.count < 4 || s.count > 20 { return "防钓鱼码须为 4-20 位" }
        return nil
    }
    var body: some View {
        SecForm(title: hint == nil ? "设置防钓鱼码" : "修改防钓鱼码", onBack: onBack, error: error, button: "确认",
                enabled: !value.isEmpty && Self.check(value) == nil && code.count == 6, loading: loading, action: { Task { await submit() } }) {
            VStack(alignment: .leading, spacing: 10) {
                Text("什么是防钓鱼码？").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
                Text("设置后，Linbit 发送给您的所有邮件开头都会显示该码。若邮件中没有或不一致，即为仿冒邮件。").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                VStack(alignment: .leading, spacing: 6) {
                    Text("Linbit · 安全通知").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                    HStack(spacing: 4) {
                        Text("防钓鱼码：").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        Text(value.isEmpty ? (hint ?? "******") : value).font(.lbNum(FontSize.small, .semibold)).foregroundColor(p.brand)
                    }
                }.padding(12).frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
            }
            if let hint { HStack { Text("当前防钓鱼码").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary); Spacer(); Text(hint).font(.lbNum(FontSize.caption)).foregroundColor(p.textPrimary) } }
            LBTextField(label: "防钓鱼码", placeholder: "4-20 位字母或数字", text: $value, error: Self.check(value))
            SecVerifyField(twofa: twofa, code: $code)
        }
        .onChange(of: value) { _ in error = nil }
    }
    private func submit() async {
        loading = true; defer { loading = false }
        do {
            try await SecurityAPI.setAntiPhishing(value, totp: twofa ? code : nil, code: twofa ? nil : code)
            ToastCenter.shared.show("防钓鱼码已设置", .success); onBack()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

// MARK: - 登录设备与会话
struct DevicesSessionsView: View {
    @Environment(\.palette) private var p
    let onBack: () -> Void
    @State private var tab = SecShot.state == "sessions" ? 1 : 0
    @State private var devices: Loadable<[APIDevice]> = .idle
    @State private var sessions: Loadable<[APISession]> = .idle
    @State private var confirm: ConfirmRequest?

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "登录设备与会话", onBack: onBack)
            PrimaryTabs(items: ["登录设备", "会话"], selected: $tab, size: FontSize.label, underline: true)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
            Divider().overlay(p.divider)
            if tab == 0 { deviceList } else { sessionList }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .lbConfirm($confirm)
        .task { await load() }
        .onChange(of: tab) { _ in Task { await load() } }
    }

    @ViewBuilder private var deviceList: some View {
        switch devices {
        case .idle, .loading: SkeletonList(rows: 4)
        case let .failed(m): ErrorStateView(message: m) { Task { await load(force: true) } }
        case let .loaded(list) where list.isEmpty: EmptyStateView(text: "暂无登录设备")
        case let .loaded(list):
            ScrollView {
                VStack(spacing: 10) {
                    ForEach(list, id: \.deviceId) { d in
                        card(icon: icon(d.name ?? d.userAgent), title: d.name ?? "未知设备", current: d.current == true,
                             lines: ["IP \(d.ip ?? "—")", "最近活跃 \(Fmt.dateTime(d.lastSeen))", "首次登录 \(Fmt.dateTime(d.firstSeen))"],
                             action: d.current == true ? nil : act("移除") { askRemoveDevice(d) })
                    }
                    Text("移除设备会注销该设备上的全部登录会话。").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }.padding(Spacing.pageH)
            }
        }
    }

    @ViewBuilder private var sessionList: some View {
        switch sessions {
        case .idle, .loading: SkeletonList(rows: 4)
        case let .failed(m): ErrorStateView(message: m) { Task { await load(force: true) } }
        case let .loaded(list) where list.isEmpty: EmptyStateView(text: "暂无有效会话")
        case let .loaded(list):
            VStack(spacing: 0) {
                ScrollView {
                    VStack(spacing: 10) {
                        ForEach(list, id: \.id) { s in
                            card(icon: icon(s.name ?? s.userAgent), title: s.name ?? "未知设备", current: s.current == true,
                                 lines: ["IP \(s.ip ?? "—")", "登录时间 \(s.createdAt.map(Fmt.dateTime) ?? "—")", "有效期至 \(s.expiresAt.map(Fmt.dateTime) ?? "—")"],
                                 action: s.current == true ? nil : act("注销") { askRevoke(s) })
                        }
                    }.padding(Spacing.pageH)
                }
                if list.contains(where: { $0.current != true }) {
                    LBButton(title: "注销其他全部会话", kind: .secondary) { askRevokeOthers() }.padding(Spacing.pageH)
                }
            }
        }
    }

    private func act(_ t: String, _ f: @escaping () -> Void) -> (String, () -> Void)? { (t, f) }
    private func icon(_ s: String?) -> String {
        let n = (s ?? "").lowercased()
        if n.contains("iphone") || n.contains("ios") || n.contains("android") || n.contains("pixel") { return "iphone" }
        if n.contains("ipad") { return "ipad" }
        return "laptopcomputer"
    }
    private func card(icon: String, title: String, current: Bool, lines: [String], action: (String, () -> Void)?) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: icon).font(.system(size: 18)).foregroundColor(p.textPrimary)
                .frame(width: 36, height: 36).background(Circle().fill(p.fill))
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(title).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary).lineLimit(1)
                    if current {
                        Text("本机").font(.lb(10, .medium)).foregroundColor(.black).padding(.horizontal, 5).padding(.vertical, 2)
                            .background(RoundedRectangle(cornerRadius: 4).fill(p.brand))
                    }
                }
                ForEach(lines, id: \.self) { Text($0).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary) }
            }
            Spacer()
            if let action {
                Button(action: action.1) {
                    Text(action.0).font(.lb(FontSize.small, .medium)).foregroundColor(p.down)
                        .padding(.horizontal, 10).frame(height: 26).overlay(RoundedRectangle(cornerRadius: Radius.sm).stroke(p.down.opacity(0.5)))
                }.buttonStyle(PressableStyle())
            }
        }
        .padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).stroke(current ? p.brand : p.border))
    }

    private func load(force: Bool = false) async {
        if SecShot.on {
            let st = SecShot.state
            devices = st == "error" ? .failed("网络连接失败，请检查网络") : (st == "empty" ? .loaded([]) : .loaded(SecShot.devices))
            sessions = st == "loading" ? .loading : .loaded(SecShot.sessions)
            return
        }
        if tab == 0 {
            if devices.value == nil || force { devices = .loading }
            do { devices = .loaded(try await SecurityAPI.devices()) } catch { devices = .failed(error.lbMessage) }
        } else {
            if sessions.value == nil || force { sessions = .loading }
            do { sessions = .loaded(try await SecurityAPI.sessions()) } catch { sessions = .failed(error.lbMessage) }
        }
    }
    private func askRemoveDevice(_ d: APIDevice) {
        confirm = .init(title: "移除设备", message: "将注销「\(d.name ?? "该设备")」上的所有登录会话。", confirmTitle: "移除", destructive: true) {
            Task {
                do { try await SecurityAPI.removeDevice(d.deviceId); ToastCenter.shared.show("已移除", .success); await load() }
                catch { ToastCenter.shared.show(securityMessage(error), .error) }
            }
        }
    }
    private func askRevoke(_ s: APISession) {
        confirm = .init(title: "注销会话", message: "「\(s.name ?? "该设备")」将需要重新登录。", confirmTitle: "注销", destructive: true) {
            Task {
                do { try await SecurityAPI.revokeSession(s.id ?? ""); ToastCenter.shared.show("已注销", .success); await load() }
                catch { ToastCenter.shared.show(securityMessage(error), .error) }
            }
        }
    }
    private func askRevokeOthers() {
        confirm = .init(title: "注销其他会话", message: "除本机外的所有设备都将退出登录。", confirmTitle: "全部注销", destructive: true) {
            Task {
                do { let n = try await SecurityAPI.revokeOthers(); ToastCenter.shared.show("已注销 \(n) 个会话", .success); await load() }
                catch { ToastCenter.shared.show(securityMessage(error), .error) }
            }
        }
    }
}

// MARK: - 通行密钥（Passkey）
struct PasskeysView: View {
    @Environment(\.palette) private var p
    let twofa: Bool
    let onBack: () -> Void
    @State private var data: Loadable<APIListPasskeys200Response> = .idle
    @State private var confirm: ConfirmRequest?
    @State private var adding = SecShot.state == "add"
    @State private var code = ""
    @State private var name = DeviceInfo.name
    @State private var busy = false
    @State private var error: String?
    @StateObject private var registrar = RegistrarBox()

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "通行密钥", onBack: onBack)
            switch data {
            case .idle, .loading: SkeletonList(rows: 3)
            case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
            case let .loaded(r):
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        InfoNote(text: "通行密钥使用面容 ID / 触控 ID 登录，无需输入密码，可有效防止钓鱼。")
                        if r.enabled == false {
                            WarnBanner(text: "通行密钥功能暂未开放")
                        } else if (r.items ?? []).isEmpty {
                            EmptyStateView(text: "尚未添加通行密钥")
                        } else {
                            ForEach(r.items ?? [], id: \.id) { k in item(k) }
                        }
                        if adding && r.enabled != false { addForm }
                    }.padding(Spacing.pageH)
                }
                if r.enabled != false && !adding {
                    LBButton(title: "添加通行密钥", enabled: (r.items ?? []).count < 10) { adding = true; error = nil }.padding(Spacing.pageH)
                }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .lbConfirm($confirm)
        .task { await load() }
    }

    private var addForm: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("添加通行密钥").font(.lb(FontSize.label, .semibold)).foregroundColor(p.textPrimary)
            LBTextField(label: "名称", placeholder: "如 我的 iPhone", text: Binding(get: { name }, set: { name = String($0.prefix(64)) }))
            SecVerifyField(twofa: twofa, code: $code)
            if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
            HStack(spacing: 10) {
                LBButton(title: "取消", kind: .secondary) { adding = false; code = "" }
                LBButton(title: "继续", enabled: code.count == 6, loading: busy) { Task { await add() } }
            }
        }
        .padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border))
    }

    private func item(_ k: APIPasskey) -> some View {
        HStack(spacing: 12) {
            Image(systemName: "person.badge.key.fill").font(.system(size: 16)).foregroundColor(p.textPrimary)
                .frame(width: 36, height: 36).background(Circle().fill(p.fill))
            VStack(alignment: .leading, spacing: 4) {
                Text(k.name ?? "通行密钥").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                Text("添加于 \(k.createdAt.map(Fmt.dateTime) ?? "—")").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                Text(k.lastUsedAt.map { "最近使用 \(Fmt.dateTime($0))" } ?? "从未使用").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Spacer()
            Button { askDelete(k) } label: { Image(systemName: "trash").font(.system(size: 15)).foregroundColor(p.down) }.buttonStyle(PressableStyle())
        }
        .padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border))
    }

    private func load() async {
        if SecShot.on {
            switch SecShot.state {
            case "empty": data = .loaded(.init(enabled: true, items: []))
            case "disabled": data = .loaded(.init(enabled: false, items: []))
            case "error": data = .failed("网络连接失败，请检查网络")
            default: data = .loaded(.init(enabled: true, items: SecShot.passkeys))
            }
            return
        }
        if data.value == nil { data = .loading }
        do { data = .loaded(try await SecurityAPI.passkeys()) } catch { data = .failed(error.lbMessage) }
    }
    private func add() async {
        busy = true; defer { busy = false }
        do {
            let c = try await SecurityAPI.beginPasskey(totp: twofa ? code : nil, code: twofa ? nil : code)
            guard let sid = c.sessionId, let opts = c.publicKey else { throw PasskeyError.badOptions }
            let cred = try await registrar.r.register(options: opts)
            _ = try await SecurityAPI.finishPasskey(session: sid, name: name.isEmpty ? DeviceInfo.name : name, credential: cred)
            ToastCenter.shared.show("通行密钥已添加", .success)
            adding = false; code = ""; await load()
        } catch PasskeyError.cancelled {
            error = "已取消添加"
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
    private func askDelete(_ k: APIPasskey) {
        confirm = .init(title: "删除通行密钥", message: "删除后将无法使用「\(k.name ?? "该通行密钥")」登录。", confirmTitle: "删除", destructive: true) {
            Task {
                do { try await SecurityAPI.deletePasskey(k.id ?? ""); ToastCenter.shared.show("已删除", .success); await load() }
                catch { ToastCenter.shared.show(securityMessage(error), .error) }
            }
        }
    }
}

@MainActor final class RegistrarBox: ObservableObject { let r = PasskeyRegistrar() }

// MARK: - 关闭身份验证器（POST /2fa/totp/disable）
struct DisableTotpView: View {
    @Environment(\.palette) private var p
    let onBack: () -> Void
    @State private var code = SecShot.page == .disable2fa ? "123456" : ""
    @State private var loading = false
    @State private var error: String? = SecShot.page == .disable2fa && SecShot.state == "error" ? ErrorText.map["TOTP_INVALID"] : nil
    @State private var confirm: ConfirmRequest?
    var body: some View {
        SecForm(title: "关闭身份验证器", onBack: onBack, error: error, button: "关闭身份验证器", enabled: code.count == 6, loading: loading,
                action: { askConfirm() }) {
            WarnBanner(text: "关闭后账户安全等级将降低，且 24 小时内暂停提现与内部转账。")
            HStack(spacing: 12) {
                Image(systemName: "lock.shield.fill").font(.system(size: 22)).foregroundColor(p.up)
                VStack(alignment: .leading, spacing: 3) {
                    Text("身份验证器已开启").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    Text("登录、提现及安全设置变更需要验证").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }.padding(.vertical, 6)
            SecVerifyField(twofa: true, code: $code)
        }
        .lbConfirm($confirm)
        .onChange(of: code) { _ in error = nil }
    }
    private func askConfirm() {
        confirm = .init(title: "确认关闭", message: "关闭身份验证器会降低账户安全性，确定继续？", confirmTitle: "关闭", destructive: true) {
            Task { await submit() }
        }
    }
    private func submit() async {
        loading = true; defer { loading = false }
        do {
            try await SecurityAPI.disableTotp(code)
            await Session.shared.reloadAccount()
            ToastCenter.shared.show("身份验证器已关闭", .success); onBack()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}
