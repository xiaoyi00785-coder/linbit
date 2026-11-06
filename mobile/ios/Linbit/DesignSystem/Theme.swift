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
    /// 随系统动态字体缩放（UIFontMetrics）；大字号下配合 ScrollView / minHeight 避免裁切。
    static func lb(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        Font(UIFontMetrics.default.scaledFont(for: .systemFont(ofSize: size, weight: weight.uiFontWeight)))
    }
    /// 数字：等宽数字，保证行情跳动时不抖动；同样参与动态字体缩放。
    static func lbNum(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        let base = UIFont.monospacedDigitSystemFont(ofSize: size, weight: weight.uiFontWeight)
        return Font(UIFontMetrics.default.scaledFont(for: base))
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
