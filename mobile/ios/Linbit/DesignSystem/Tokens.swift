import SwiftUI

/// 设计令牌：与 tokens/tokens.json 一一对应（从对照图 @3x 截图取样）。
struct Palette {
    let bg, bgElevated, fill, fillStrong, border, divider: Color
    let textPrimary, textSecondary, textTertiary, textOnPrimary: Color
    let primary, brand, up, down, upTint, downTint, warning, onChange: Color
    let tabBarBg, tabBarSelected, tabBarBorder, iconTileBg, badgeDot, mask: Color

    static let light = Palette(
        bg: .hex(0xFFFFFF), bgElevated: .hex(0xFFFFFF), fill: .hex(0xF3F3F3), fillStrong: .hex(0xE9E9E9),
        border: .hex(0xEDEDED), divider: .hex(0xF2F2F2),
        textPrimary: .hex(0x080808), textSecondary: .hex(0x7F7F7F), textTertiary: .hex(0xA3A3A3), textOnPrimary: .hex(0xFFFFFF),
        primary: .hex(0x111111), brand: .hex(0x9BCB2A), up: .hex(0x5FB971), down: .hex(0xDD5754),
        upTint: .hex(0xEFFAF1), downTint: .hex(0xFDEEEE), warning: .hex(0xD97706), onChange: .hex(0xFFFFFF),
        tabBarBg: .hex(0xFCFCFE, 0.90), tabBarSelected: .hex(0xE9E9EB), tabBarBorder: .hex(0xE4E4E6),
        iconTileBg: .hex(0xF3F3F3), badgeDot: .hex(0x9BCB2A), mask: .hex(0x000000, 0.4))

    static let dark = Palette(
        bg: .hex(0x0B0B0C), bgElevated: .hex(0x161618), fill: .hex(0x1E1E20), fillStrong: .hex(0x2A2A2D),
        border: .hex(0x2A2A2D), divider: .hex(0x1C1C1E),
        textPrimary: .hex(0xF5F5F5), textSecondary: .hex(0x8C8C8F), textTertiary: .hex(0x5E5E62), textOnPrimary: .hex(0x0B0B0C),
        primary: .hex(0xF5F5F5), brand: .hex(0xABE127), up: .hex(0x5FB971), down: .hex(0xDD5754),
        upTint: .hex(0x14261A), downTint: .hex(0x2C1616), warning: .hex(0xF0A03A), onChange: .hex(0xFFFFFF),
        tabBarBg: .hex(0x1C1C1E, 0.90), tabBarSelected: .hex(0x2E2E31), tabBarBorder: .hex(0x2A2A2D),
        iconTileBg: .hex(0x1E1E20), badgeDot: .hex(0xABE127), mask: .hex(0x000000, 0.63))
}

enum FontSize {
    static let assetHero: CGFloat = 40, tabPrimary: CGFloat = 18, title: CGFloat = 17, symbol: CGFloat = 17
    static let symbolWithIcon: CGFloat = 16, body: CGFloat = 15, price: CGFloat = 16, change: CGFloat = 15
    static let label: CGFloat = 14, tabSecondary: CGFloat = 15, chip: CGFloat = 14, caption: CGFloat = 13
    static let small: CGFloat = 12, tabBar: CGFloat = 11
}

enum Spacing {
    static let xxs: CGFloat = 2, xs: CGFloat = 4, sm: CGFloat = 8, md: CGFloat = 12, lg: CGFloat = 16
    static let xl: CGFloat = 20, xxl: CGFloat = 24, pageH: CGFloat = 16, rowV: CGFloat = 12
}

enum Radius {
    static let xs: CGFloat = 4, sm: CGFloat = 6, md: CGFloat = 8, lg: CGFloat = 10, xl: CGFloat = 12, sheet: CGFloat = 16
}

enum Metrics {
    static let searchBarH: CGFloat = 31, heroButtonW: CGFloat = 80, heroButtonH: CGFloat = 32
    static let taskCardH: CGFloat = 44, bannerH: CGFloat = 60, gridIcon: CGFloat = 40, gridIconGlyph: CGFloat = 22
    static let marketRowH: CGFloat = 56, coinIcon: CGFloat = 32, changeBadgeW: CGFloat = 76, changeBadgeH: CGFloat = 31
    static let chipH: CGFloat = 24, tabBarH: CGFloat = 61, tabBarInset: CGFloat = 21, navIcon: CGFloat = 22
}

extension Color {
    static func hex(_ v: UInt32, _ a: Double = 1) -> Color {
        Color(.sRGB, red: Double((v >> 16) & 0xFF) / 255, green: Double((v >> 8) & 0xFF) / 255,
              blue: Double(v & 0xFF) / 255, opacity: a)
    }
}
