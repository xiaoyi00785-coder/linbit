import SwiftUI
import UIKit

// MARK: - 二级页导航栏（← 标题 右侧操作，对照图 #23/#37/#46）
struct NavBar<Trailing: View>: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let title: String
    var onBack: (() -> Void)? = nil
    @ViewBuilder var trailing: Trailing
    var body: some View {
        ZStack {
            Text(title).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            HStack {
                Button { if let onBack { onBack() } else { dismiss() } } label: {
                    Image(systemName: "arrow.left").font(.system(size: 18, weight: .medium)).foregroundColor(p.textPrimary)
                        .frame(width: 44, height: 44, alignment: .leading)
                }
                .buttonStyle(PressableStyle())
                Spacer()
                trailing
            }
        }
        .padding(.horizontal, Spacing.pageH).frame(height: 44).background(p.bg)
    }
}
extension NavBar where Trailing == EmptyView {
    init(title: String, onBack: (() -> Void)? = nil) { self.init(title: title, onBack: onBack) { EmptyView() } }
}

// MARK: - 输入框（灰底圆角 8，高 48；聚焦黑描边；错误红描边 + 下方红字）
struct LBTextField<Trailing: View>: View {
    @Environment(\.palette) private var p
    var label: String? = nil
    let placeholder: String
    @Binding var text: String
    var secure = false
    var keyboard: UIKeyboardType = .default
    var error: String? = nil
    var height: CGFloat = 48
    @ViewBuilder var trailing: Trailing
    @FocusState private var focused: Bool
    @State private var reveal = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 1) {
                    if let label, !text.isEmpty || focused {
                        Text(label).font(.lb(FontSize.small - 1)).foregroundColor(p.textSecondary)
                    }
                    Group {
                        if secure && !reveal { SecureField("", text: $text) } else { TextField("", text: $text) }
                    }
                    .placeholder(when: text.isEmpty) { Text(label != nil && focused ? "" : placeholder).foregroundColor(p.textTertiary) }
                    .font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                    .keyboardType(keyboard).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .focused($focused)
                }
                if !text.isEmpty && focused {
                    Button { text = "" } label: { Image(systemName: "xmark.circle.fill").foregroundColor(p.textTertiary) }
                }
                if secure {
                    Button { reveal.toggle() } label: {
                        Image(systemName: reveal ? "eye" : "eye.slash").foregroundColor(p.textSecondary)
                    }
                }
                trailing
            }
            .padding(.horizontal, 12).frame(minHeight: height)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
            .overlay(RoundedRectangle(cornerRadius: Radius.md)
                .stroke(error != nil ? p.down : focused ? p.textPrimary : .clear, lineWidth: 1))
            .contentShape(Rectangle()).onTapGesture { focused = true }
            if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
        }
    }
}
extension LBTextField where Trailing == EmptyView {
    init(label: String? = nil, placeholder: String, text: Binding<String>, secure: Bool = false,
         keyboard: UIKeyboardType = .default, error: String? = nil) {
        self.init(label: label, placeholder: placeholder, text: text, secure: secure, keyboard: keyboard, error: error) { EmptyView() }
    }
}

extension View {
    func placeholder<C: View>(when show: Bool, @ViewBuilder _ c: () -> C) -> some View {
        ZStack(alignment: .leading) { c().opacity(show ? 1 : 0).allowsHitTesting(false); self }
    }
}

// MARK: - 验证码输入（6 位 + 「获取验证码 / 59s 后重发」）
struct CodeField: View {
    @Environment(\.palette) private var p
    @Binding var code: String
    var placeholder = "请输入 6 位验证码"
    var autoStart = false
    let send: () async -> Bool
    @State private var left = 0
    @State private var sending = false

    var body: some View {
        LBTextField(placeholder: placeholder, text: Binding(get: { code }, set: { code = String($0.filter(\.isNumber).prefix(6)) }),
                    keyboard: .numberPad) {
            Button {
                Task { await fire() }
            } label: {
                Group {
                    if sending { ProgressView().scaleEffect(0.8) }
                    else { Text(left > 0 ? "\(left)s 后重发" : "获取验证码") }
                }
                .font(.lb(FontSize.label, .medium))
                .foregroundColor(left > 0 ? p.textTertiary : p.brand)
            }
            .disabled(left > 0 || sending)
        }
        .task { if autoStart { start() } }
    }

    private func fire() async {
        sending = true
        let ok = await send()
        sending = false
        if ok { start() }
    }
    private func start() {
        left = 60
        Task { @MainActor in
            while left > 0 { try? await Task.sleep(nanoseconds: 1_000_000_000); left -= 1 }
        }
    }
}

// MARK: - 勾选框
struct Checkbox: View {
    @Environment(\.palette) private var p
    @Binding var on: Bool
    var label: String? = nil
    var body: some View {
        Button { on.toggle(); Haptics.tap() } label: {
            HStack(spacing: 6) {
                RoundedRectangle(cornerRadius: 3)
                    .stroke(on ? p.textPrimary : p.textTertiary, lineWidth: 1.2)
                    .background(RoundedRectangle(cornerRadius: 3).fill(on ? p.textPrimary : .clear))
                    .overlay(on ? Image(systemName: "checkmark").font(.system(size: 9, weight: .bold)).foregroundColor(p.bg) : nil)
                    .frame(width: 15, height: 15)
                if let label { Text(label).font(.lb(FontSize.caption)).foregroundColor(p.textPrimary) }
            }
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 步骤条（网页端 Steps：账户密码 → 安全验证）
struct StepsBar: View {
    @Environment(\.palette) private var p
    let items: [String]
    let current: Int
    var body: some View {
        HStack(spacing: 6) {
            ForEach(items.indices, id: \.self) { i in
                Circle().fill(i <= current ? p.brand : p.fillStrong).frame(width: 18, height: 18)
                    .overlay(Text("\(i + 1)").font(.lb(11, .semibold)).foregroundColor(i <= current ? .black : p.textSecondary))
                Text(items[i]).font(.lb(FontSize.small, i == current ? .medium : .regular))
                    .foregroundColor(i <= current ? p.textPrimary : p.textTertiary)
                if i < items.count - 1 { Rectangle().fill(p.border).frame(width: 24, height: 1) }
            }
        }
    }
}

// MARK: - 买入 / 卖出 分段（对照图 #28：选中绿/红底白字，未选灰底）
struct SideSegment: View {
    @Environment(\.palette) private var p
    @Binding var isBuy: Bool
    var buyTitle = "买入", sellTitle = "卖出"
    var body: some View {
        HStack(spacing: 0) {
            seg(buyTitle, isBuy, p.up) { isBuy = true }
            seg(sellTitle, !isBuy, p.down) { isBuy = false }
        }
        .frame(height: 32)
        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
    }
    private func seg(_ t: String, _ on: Bool, _ c: Color, _ a: @escaping () -> Void) -> some View {
        Button { withAnimation(.easeOut(duration: 0.15)) { a() }; Haptics.tap() } label: {
            Text(t).font(.lb(FontSize.label, .medium)).foregroundColor(on ? .white : p.textPrimary)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(RoundedRectangle(cornerRadius: Radius.sm).fill(on ? c : .clear))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 百分比滑杆（0/25/50/75/100 五个菱形节点，对照图 #28）
struct PercentSlider: View {
    @Environment(\.palette) private var p
    @Binding var value: Double   // 0...1
    var tint: Color
    var body: some View {
        GeometryReader { g in
            let w = g.size.width
            ZStack(alignment: .leading) {
                Rectangle().fill(p.fillStrong).frame(height: 2)
                Rectangle().fill(tint).frame(width: w * value, height: 2)
                ForEach(0..<5) { i in
                    let x = w * Double(i) / 4
                    Rectangle().fill(Double(i) / 4 <= value ? tint : p.bg)
                        .overlay(Rectangle().stroke(Double(i) / 4 <= value ? tint : p.textTertiary, lineWidth: 1.2))
                        .frame(width: 7, height: 7).rotationEffect(.degrees(45))
                        .position(x: min(max(x, 4), w - 4), y: 10)
                }
                Rectangle().fill(p.textPrimary).frame(width: 11, height: 11).rotationEffect(.degrees(45))
                    .position(x: min(max(w * value, 6), w - 6), y: 10)
            }
            .frame(height: 20)
            .contentShape(Rectangle())
            .gesture(DragGesture(minimumDistance: 0).onChanged { v in
                var nv = min(max(v.location.x / w, 0), 1)
                for s in [0, 0.25, 0.5, 0.75, 1] where abs(nv - s) < 0.03 { nv = s }   // 吸附节点
                if nv != value { value = nv; if [0, 0.25, 0.5, 0.75, 1].contains(nv) { Haptics.tap() } }
            })
        }
        .frame(height: 20)
    }
}

/// 圆角灰底搜索框（ref2 #17 选择币种）
struct SearchField: View {
    @Environment(\.palette) private var p
    @Binding var text: String
    var placeholder = "搜索"
    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass").font(.system(size: 14)).foregroundColor(p.textSecondary)
            TextField("", text: $text).placeholder(when: text.isEmpty) { Text(placeholder).foregroundColor(p.textTertiary) }
                .font(.lb(FontSize.label)).foregroundColor(p.textPrimary).autocorrectionDisabled().textInputAutocapitalization(.never)
            if !text.isEmpty {
                Button { text = "" } label: { Image(systemName: "xmark.circle.fill").foregroundColor(p.textTertiary) }
            }
        }.padding(.horizontal, 12).frame(height: 36).background(Capsule().fill(p.fill))
    }
}

// MARK: - 键盘「完成」（numberPad / decimalPad 无回车键）
extension View {
    /// 在键盘工具条右侧放「完成」，点击后收起键盘。
    func lbKeyboardDone(focus: FocusState<Bool>.Binding) -> some View {
        toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button("完成") { focus.wrappedValue = false }
            }
        }
    }
}
