import SwiftUI

#if DEBUG
/// 合约 / 充值 / 客服 适配预览（Canvas）。非截图验收替代。
struct TradeAdaptPreviews: PreviewProvider {
    static var previews: some View {
        Group {
            ThemedRoot { FuturesRootView() }
                .environmentObject(Session.shared)
                .environmentObject(AccountsStore.shared)
                .environmentObject(ThemeStore())
                .previewDevice("iPhone SE (3rd generation)")
                .previewDisplayName("合约 · SE")

            ThemedRoot { FuturesRootView() }
                .environmentObject(Session.shared)
                .environmentObject(AccountsStore.shared)
                .environmentObject(ThemeStore())
                .environment(\.sizeCategory, .accessibilityExtraLarge)
                .previewDevice("iPhone SE (3rd generation)")
                .previewDisplayName("合约 · SE · aXL")

            NavigationStack {
                ThemedRoot { DepositView(asset: "USDT") }
                    .environmentObject(Session.shared)
                    .environmentObject(ThemeStore())
            }
            .previewDevice("iPhone SE (3rd generation)")
            .previewDisplayName("充值 · SE")

            ThemedRoot { SupportTicketsView(onBack: {}) }
                .environmentObject(Session.shared)
                .environmentObject(ThemeStore())
                .previewDevice("iPhone SE (3rd generation)")
                .previewDisplayName("客服列表 · SE")
        }
    }
}
#endif
