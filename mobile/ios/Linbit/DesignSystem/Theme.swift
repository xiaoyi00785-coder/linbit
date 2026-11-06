import SwiftUI
import UIKit

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
    /// 随系统动态字体缩放；上限 1.35× 设计字号，避免合约/盘口等密排在超大字体下溢出（配合 ScrollView / minHeight）。
    private static let dynamicCap: CGFloat = 1.35
    static func lb(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        let scaled = min(UIFontMetrics.default.scaledValue(for: size), size * dynamicCap)
        return Font(UIFont.systemFont(ofSize: scaled, weight: weight.uiFontWeight))
    }
    /// 数字：等宽数字；同样缩放并封顶。
    static func lbNum(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        let scaled = min(UIFontMetrics.default.scaledValue(for: size), size * dynamicCap)
        let base = UIFont.monospacedDigitSystemFont(ofSize: scaled, weight: weight.uiFontWeight)
        return Font(base)
    }
}

private extension Font.Weight {
    var uiFontWeight: UIFont.Weight {
        switch self {
        case .ultraLight: return .ultraLight
        case .thin: return .thin
        case .light: return .light
        case .regular: return .regular
        case .medium: return .medium
        case .semibold: return .semibold
        case .bold: return .bold
        case .heavy: return .heavy
        case .black: return .black
        default: return .regular
        }
    }
}
