import SwiftUI
import UIKit

// 新对照图 ref2 #3–#11：登录 / 注册 / 密码 / 验证码 页通用元素

/// 顶栏：左 ⌄ 关闭（首屏）或 ← 返回（二级），右 客服耳机
struct AuthTopBar: View {
    @Environment(\.palette) private var p
    var isRoot: Bool
    let onLeft: () -> Void
    var body: some View {
        HStack {
            Button(action: onLeft) {
                Image(systemName: isRoot ? "chevron.down" : "arrow.left")
                    .font(.system(size: 18, weight: .medium)).foregroundColor(p.textPrimary).frame(width: 44, height: 44, alignment: .leading)
            }.buttonStyle(PressableStyle())
            Spacer()
            Button {
                if AppFeatures.shared.isOn("supportTickets") {
                    Router.shared.openFeature(.support)
                } else {
                    ToastCenter.shared.show("在线客服即将开放")
                }
            } label: {
                Image(systemName: "headphones").font(.system(size: 18)).foregroundColor(p.textPrimary).frame(width: 44, height: 44, alignment: .trailing)
            }.buttonStyle(PressableStyle())
        }
        .padding(.horizontal, Spacing.pageH).frame(height: 44)
    }
}

/// 大标题 + 灰色副标题（28pt bold）
struct AuthHeader: View {
    @Environment(\.palette) private var p
    let title: String
    var subtitle: Text? = nil
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.lb(28, .bold)).foregroundColor(p.textPrimary)
            if let subtitle { subtitle.font(.lb(FontSize.caption)).foregroundColor(p.textSecondary) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.top, 16).padding(.bottom, 28)
    }
}

struct FieldLabel: View {
    @Environment(\.palette) private var p
    let text: String
    var body: some View {
        Text(text).font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary).frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// 「邮箱/手机号码」智能输入：纯数字时出现 🇨🇳 +86 ▾ 前缀；输入字母时下拉邮箱后缀 + 上一次登录
struct AccountField: View {
    @Environment(\.palette) private var p
    @Binding var id: AccountId
    @Binding var raw: String
    var error: String?
    @FocusState private var focused: Bool
    @State private var showCountry = false
    @AppStorage("lb.lastLogin") private var lastLogin = ""

    static let domains = ["@hotmail.com", "@msn.com", "@yahoo.com", "@gmail.com", "@outlook.com"]
    var isPhone: Bool { !raw.isEmpty && raw.allSatisfy(\.isNumber) }

    var suggestions: [(String, Bool)] {
        guard focused, !raw.isEmpty, !isPhone else { return [] }
        let local = raw.split(separator: "@", omittingEmptySubsequences: false).first.map(String.init) ?? raw
        let typedDomain = raw.contains("@") ? "@" + (raw.split(separator: "@").dropFirst().first ?? "") : ""
        var out: [(String, Bool)] = []
        if !lastLogin.isEmpty, lastLogin.lowercased().hasPrefix(raw.lowercased().prefix(1)) { out.append((lastLogin, true)) }
        out += Self.domains.filter { typedDomain.isEmpty || $0.hasPrefix(typedDomain) }.map { (local + $0, false) }
        return out.filter { $0.0 != raw }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                if isPhone {
                    Button { showCountry = true } label: {
                        HStack(spacing: 4) {
                            Text(CountryCode.flag(id.countryCode)).font(.system(size: 16))
                            Text("+\(id.countryCode)").font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                            Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)).foregroundColor(p.textTertiary)
                        }
                    }
                    Rectangle().fill(p.border).frame(width: 1, height: 18)
                }
                TextField("", text: $raw)
                    .placeholder(when: raw.isEmpty) { Text("邮箱/手机号码 (不含国际区号)").foregroundColor(p.textTertiary) }
                    .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                    .keyboardType(.emailAddress).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .textContentType(.username).focused($focused).submitLabel(.done)
                if !raw.isEmpty && focused {
                    Button { raw = "" } label: { Image(systemName: "xmark.circle.fill").foregroundColor(p.textTertiary) }
                }
            }
            .padding(.horizontal, 12).frame(minHeight: 48)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(error != nil ? p.bg : p.fill))
            .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(error != nil ? p.down : focused ? p.textPrimary : .clear, lineWidth: 1))
            if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
            if !suggestions.isEmpty {
                VStack(spacing: 0) {
                    ForEach(suggestions, id: \.0) { s in
                        Button { raw = s.0; focused = false } label: {
                            HStack {
                                Text(s.0).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                                Spacer()
                                if s.1 { Text("上一次登录").font(.lb(FontSize.small)).foregroundColor(p.textTertiary) }
                            }
                            .padding(.horizontal, 12).frame(height: 34)
                        }.buttonStyle(RowPressStyle())
                    }
                }
                .padding(.vertical, 4)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.bgElevated).shadow(color: .black.opacity(0.08), radius: 10, y: 4))
            }
        }
        .onChange(of: raw) { v in
            let t = v.trimmingCharacters(in: .whitespaces)
            if !t.isEmpty && t.allSatisfy(\.isNumber) { id.kind = .phone; id.phone = t } else { id.kind = .email; id.email = t }
        }
        .lbSheet($showCountry) {
            CountryCodeSheet(selected: id.countryCode) { id.countryCode = $0; showCountry = false } onClose: { showCountry = false }
        }
    }
}

enum CountryCode {
    static let list: [(String, String, String)] = [
        ("86", "中国大陆", "🇨🇳"), ("852", "中国香港", "🇭🇰"), ("853", "中国澳门", "🇲🇴"), ("886", "中国台湾", "🇹🇼"),
        ("1", "美国 / 加拿大", "🇺🇸"), ("44", "英国", "🇬🇧"), ("81", "日本", "🇯🇵"), ("82", "韩国", "🇰🇷"),
        ("65", "新加坡", "🇸🇬"), ("60", "马来西亚", "🇲🇾"), ("66", "泰国", "🇹🇭"), ("84", "越南", "🇻🇳"),
        ("63", "菲律宾", "🇵🇭"), ("62", "印度尼西亚", "🇮🇩"), ("91", "印度", "🇮🇳"), ("61", "澳大利亚", "🇦🇺"),
        ("49", "德国", "🇩🇪"), ("33", "法国", "🇫🇷"), ("7", "俄罗斯", "🇷🇺"), ("971", "阿联酋", "🇦🇪"),
    ]
    static func flag(_ cc: String) -> String { list.first { $0.0 == cc }?.2 ?? "🌐" }
}

struct CountryCodeSheet: View {
    @Environment(\.palette) private var p
    let selected: String
    let onPick: (String) -> Void
    let onClose: () -> Void
    @State private var q = ""
    var body: some View {
        LBSheet(title: "选择国家/地区", showClose: true, onClose: onClose) {
            LBTextField(placeholder: "搜索", text: $q).padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(CountryCode.list.filter { q.isEmpty || $0.1.contains(q) || $0.0.hasPrefix(q) }, id: \.0) { c in
                        SheetOptionRow(title: "\(c.2)  \(c.1)", subtitle: nil, selected: c.0 == selected) { onPick(c.0) }
                            .overlay(alignment: .trailing) {
                                Text("+\(c.0)").font(.lbNum(FontSize.label)).foregroundColor(p.textSecondary).padding(.trailing, c.0 == selected ? 48 : 16)
                            }
                    }
                }
            }
            .frame(height: 420)
        }
    }
}

/// 6 格验证码（#11）：当前格 lime 描边 + 光标；左下「粘贴」，右下「重新发送(58s)」
struct OTPBoxes: View {
    @Environment(\.palette) private var p
    @Binding var code: String
    var error = false
    var resendable = true
    let onResend: () async -> Bool
    @FocusState private var focused: Bool
    @State private var left = 60
    @State private var blink = false

    var body: some View {
        VStack(spacing: 10) {
            ZStack {
                TextField("", text: Binding(get: { code }, set: { code = String($0.filter(\.isNumber).prefix(6)) }))
                    .keyboardType(.numberPad).textContentType(.oneTimeCode).focused($focused).opacity(0.01)
                HStack(spacing: 10) {
                    ForEach(0..<6, id: \.self) { i in
                        let ch = i < code.count ? String(Array(code)[i]) : ""
                        let active = focused && i == min(code.count, 5)
                        RoundedRectangle(cornerRadius: Radius.md)
                            .fill(active ? p.bg : p.fill)
                            .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(error ? p.down : active ? p.brand : .clear, lineWidth: 1))
                            .overlay(
                                ZStack {
                                    Text(ch).font(.lbNum(22, .semibold)).foregroundColor(p.textPrimary)
                                    if active && ch.isEmpty { Rectangle().fill(p.brand).frame(width: 1.5, height: 20).opacity(blink ? 0 : 1) }
                                })
                            .frame(height: 48)
                    }
                }
                .contentShape(Rectangle()).onTapGesture { focused = true }
            }
            HStack {
                Button {
                    if let s = UIPasteboard.general.string?.filter(\.isNumber), s.count >= 6 { code = String(s.prefix(6)) }
                    else { ToastCenter.shared.show("剪贴板中没有验证码") }
                } label: { Text("粘贴").font(.lb(FontSize.caption, .medium)).foregroundColor(p.brand) }
                Spacer()
                if resendable {
                    Button { Task { if await onResend() { left = 60; tick() } } } label: {
                        Text(left > 0 ? "重新发送(\(left)s)" : "重新发送").font(.lb(FontSize.caption))
                            .foregroundColor(left > 0 ? p.textTertiary : p.brand)
                    }.disabled(left > 0)
                }
            }
        }
        .onAppear {
            focused = true; tick()
            withAnimation(.easeInOut(duration: 0.5).repeatForever()) { blink = true }
        }
    }
    private func tick() {
        Task { @MainActor in while left > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); left -= 1 } }
    }
}

/// 虚线下划线文字链接（「忘记密码?」「收不到验证码?」）
struct DashedLink: View {
    @Environment(\.palette) private var p
    let title: String
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                Line().stroke(style: StrokeStyle(lineWidth: 1, dash: [3, 3])).foregroundColor(p.textTertiary).frame(height: 1)
            }.fixedSize()
        }.buttonStyle(PressableStyle())
    }
    struct Line: Shape { func path(in r: CGRect) -> Path { Path { $0.move(to: .zero); $0.addLine(to: CGPoint(x: r.width, y: 0)) } } }
}

/// 「—— 或者 ——」+ 第三方登录灰按钮
struct OrDivider: View {
    @Environment(\.palette) private var p
    var body: some View {
        HStack(spacing: 12) {
            Rectangle().fill(p.divider).frame(height: 1)
            Text("或者").font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize()
            Rectangle().fill(p.divider).frame(height: 1)
        }
    }
}

struct SocialButton: View {
    @Environment(\.palette) private var p
    let icon: String
    let title: String
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            ZStack {
                HStack { Image(systemName: icon).font(.system(size: 16)).foregroundColor(p.textPrimary); Spacer() }
                Text(title).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
            }
            .padding(.horizontal, 20).frame(height: 44)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
        }.buttonStyle(PressableStyle())
    }
}
