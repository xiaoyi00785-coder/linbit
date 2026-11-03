import SwiftUI

enum ThemeMode: String, CaseIterable, Identifiable {
    case system, light, dark
    var id: String { rawValue }
    var title: String { switch self { case .system: "跟随系统"; case .light: "日间模式"; case .dark: "夜间模式" } }
    var colorScheme: ColorScheme? { switch self { case .system: nil; case .light: .light; case .dark: .dark } }
}

/// 主题偏好（持久化），对应对照图 #14「主题模式」。
final class ThemeStore: ObservableObject {
    @Published var mode: ThemeMode {
        didSet { UserDefaults.standard.set(mode.rawValue, forKey: "lb.theme") }
    }
    init() {
        mode = ThemeMode(rawValue: UserDefaults.standard.string(forKey: "lb.theme") ?? "") ?? .light
    }
}

private struct PaletteKey: EnvironmentKey { static let defaultValue = Palette.light }
extension EnvironmentValues {
    var palette: Palette {
        get { self[PaletteKey.self] }
        set { self[PaletteKey.self] = newValue }
    }
}

/// 根据当前 colorScheme 注入 Palette。
struct ThemedRoot<Content: View>: View {
    @Environment(\.colorScheme) private var scheme
    @ViewBuilder var content: Content
    var body: some View {
        content.environment(\.palette, scheme == .dark ? .dark : .light)
    }
}

extension Font {
    static func lb(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font { .system(size: size, weight: weight) }
    /// 数字：等宽数字，保证行情跳动时不抖动。
    static func lbNum(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight).monospacedDigit()
    }
}
