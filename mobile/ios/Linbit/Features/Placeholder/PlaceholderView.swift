import SwiftUI

struct PlaceholderView: View {
    @Environment(\.palette) private var p
    let title: String
    var note = "功能开发中"
    var body: some View {
        VStack(spacing: 10) {
            Spacer()
            IconView(icon: .logo, size: 44)
            Text(title).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            Text(note).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
            Spacer()
        }
        .frame(maxWidth: .infinity).background(p.bg)
    }
}

/// 资产页占位 + 主题切换入口（日间 / 夜间 / 跟随系统）
struct AssetsPlaceholder: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var theme: ThemeStore
    var body: some View {
        VStack(spacing: 20) {
            PlaceholderView(title: "资产", note: "登录后查看资产（待接入 /auth 与 /assets）")
            Picker("主题", selection: $theme.mode) {
                ForEach(ThemeMode.allCases, id: \.self) { Text($0.title).tag($0) }
            }
            .pickerStyle(.segmented).padding(.horizontal, 32).padding(.bottom, Metrics.tabBarH + 60)
        }
        .background(p.bg)
    }
}
