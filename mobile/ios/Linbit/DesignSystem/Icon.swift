import SwiftUI

/// 重绘 SVG 图标（Assets 中 ic-*，带日/夜两套外观）。
enum LBIcon: String {
    case avatar, search, scan, headset, bell, eye, chevronRight = "chevron-right", caretDown = "caret-down"
    case sort, more, arrowRight = "arrow-right", logo
    case gChat = "g-chat", gGift = "g-gift", gEarn = "g-earn", gTradfi = "g-tradfi"
    case gCopy = "g-copy", gBot = "g-bot", gMore = "g-more"
    case tabHome = "tab-home", tabMarket = "tab-market", tabFutures = "tab-futures", tabSpot = "tab-spot", tabAssets = "tab-assets"
}

struct IconView: View {
    let icon: LBIcon
    var size: CGFloat = Metrics.navIcon
    var tint: Color? = nil
    var body: some View {
        let img = Image("ic-\(icon.rawValue)").resizable()
        Group {
            if let tint { img.renderingMode(.template).foregroundColor(tint) } else { img.renderingMode(.original) }
        }
        .aspectRatio(contentMode: .fit)
        .frame(width: size, height: size)
    }
}

/// 品牌字标（链环标志 + 「Linbit」），Assets 中 ic-wordmark：日间深色字 / 夜间品牌绿标志 + 白字，透明背景。
struct LinbitWordmark: View {
    var height: CGFloat = 28
    var body: some View {
        Image("ic-wordmark").resizable().renderingMode(.original)
            .aspectRatio(1427.0 / 540.0, contentMode: .fit)
            .frame(height: height)
            .accessibilityLabel("Linbit")
    }
}
