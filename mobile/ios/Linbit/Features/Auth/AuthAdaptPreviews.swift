import SwiftUI

#if DEBUG
/// 关键适配预览：小屏 / 常规 / 大屏 + 字体放大（Xcode Canvas）。
/// CI 适配套件：mobile/ci/screens-adapt.txt（workflow_dispatch 勾选 adapt_layout）。
private struct AuthAdaptPreviewHost: View {
    let start: AuthFlow.Start
    var body: some View {
        ThemedRoot { AuthFlow(start: start) }
            .environmentObject(Session.shared)
            .environmentObject(ThemeStore())
    }
}

#Preview("登录 · 常规") {
    AuthAdaptPreviewHost(start: .login)
}

#Preview("登录 · 字体 xxxl") {
    AuthAdaptPreviewHost(start: .login)
        .environment(\.sizeCategory, .accessibilityExtraExtraExtraLarge)
}

#Preview("注册 · 常规") {
    AuthAdaptPreviewHost(start: .register)
}

#Preview("划转 · 常规") {
    NavigationStack {
        ThemedRoot { TransferView() }
            .environmentObject(Session.shared)
            .environmentObject(ThemeStore())
    }
}

/// 固定小屏/大屏画布（PreviewProvider，兼容 deployment iOS 16）
struct AuthAdaptSizePreviews: PreviewProvider {
    static var previews: some View {
        Group {
            AuthAdaptPreviewHost(start: .login)
                .previewDevice("iPhone SE (3rd generation)")
                .previewDisplayName("登录 · SE")
            AuthAdaptPreviewHost(start: .login)
                .previewDevice("iPhone 16 Pro Max")
                .previewDisplayName("登录 · 大屏")
            AuthAdaptPreviewHost(start: .login)
                .environment(\.sizeCategory, .accessibilityExtraExtraExtraLarge)
                .previewDevice("iPhone SE (3rd generation)")
                .previewDisplayName("登录 · SE · xxxl")
            NavigationStack {
                ThemedRoot { TransferView() }
                    .environmentObject(Session.shared)
                    .environmentObject(ThemeStore())
            }
            .previewDevice("iPhone SE (3rd generation)")
            .previewDisplayName("划转 · SE")
        }
    }
}
#endif
