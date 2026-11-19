import SwiftUI

// MARK: - 涨跌幅色块（对照图 #2/#8：76×31pt，圆角 6，15pt medium 白字）
struct ChangeBadge: View {
    @Environment(\.palette) private var p
    let percent: Decimal
    var body: some View {
        Text(Fmt.percent(percent))
            .font(.lbNum(FontSize.change, .medium))
            .foregroundColor(p.onChange)
            .frame(width: Metrics.changeBadgeW, height: Metrics.changeBadgeH)
            .background(RoundedRectangle(cornerRadius: Radius.sm).fill(percent < 0 ? p.down : p.up))
    }
}

// MARK: - 胶囊筛选（对照图 #8「全部/公链…」：高 24，选中灰底）
struct ChipBar: View {
    @Environment(\.palette) private var p
    let items: [String]
    @Binding var selected: Int
    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Spacing.xs) {
                ForEach(items.indices, id: \.self) { i in
                    Text(items[i])
                        .font(.lb(FontSize.chip, i == selected ? .medium : .regular))
                        .foregroundColor(i == selected ? p.textPrimary : p.textSecondary)
                        .padding(.horizontal, 10)
                        .frame(height: Metrics.chipH)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(i == selected ? p.fill : .clear))
                        .onTapGesture { selected = i }
                }
            }
            .padding(.horizontal, Spacing.pageH)
        }
    }
}

// MARK: - 一级 Tab（18pt，选中黑色 semibold，未选灰色）
struct PrimaryTabs: View {
    @Environment(\.palette) private var p
    let items: [String]
    @Binding var selected: Int
    var size: CGFloat = FontSize.tabPrimary
    var underline = false
    var spacing: CGFloat = 20
    var body: some View {
        HStack(spacing: spacing) {
            ForEach(items.indices, id: \.self) { i in
                VStack(spacing: 6) {
                    // 不换行：标签多、放不下时由外层横向滚动，不能把「简单赚币」挤成两行
                    Text(items[i])
                        .font(.lb(size, i == selected ? .semibold : .regular))
                        .foregroundColor(i == selected ? p.textPrimary : p.textSecondary)
                        .lineLimit(1).fixedSize()
                    if underline {
                        Capsule().fill(i == selected ? p.textPrimary : .clear).frame(width: 16, height: 3)
                    }
                }
                .contentShape(Rectangle())
                .onTapGesture { selected = i }
            }
        }
    }
}

// MARK: - 排序表头（「币种/成交额 ⇕」）
struct SortLabel: View {
    @Environment(\.palette) private var p
    let title: String
    var showArrow = true
    var body: some View {
        HStack(spacing: 2) {
            Text(title).font(.lb(FontSize.caption)).foregroundColor(p.textTertiary)
            if showArrow { IconView(icon: .sort, size: 10, tint: p.textTertiary) }
        }
    }
}

// MARK: - 币种图标（多来源 PNG，都失败时用带首字母的色块，避免空白）
/// 与网页 `components/CoinIcon.tsx` 同一套：本地 `coin-<代码>` 图标，别名表相同，没有文件时用首字母。
enum CoinArt {
    static let alias: [String: String] = [
        "GOLD": "xau", "XAUUSD": "xau", "SILVER": "xag", "XAGUSD": "xag",
        "USOIL": "wti", "CL": "wti", "UKOIL": "brent", "NATGAS": "ngas",
        "GOOG": "googl", "WBTC": "wbtc", "MATIC": "pol", "1000PEPE": "pepe", "1000SHIB": "shib",
    ]
    static func base(of raw: String) -> String {
        var x = raw.uppercased().trimmingCharacters(in: .whitespaces)
        if let cut = x.firstIndex(where: { "/-_ ".contains($0) }) { x = String(x[..<cut]) }
        if x.isEmpty { return "?" }
        for quote in ["USDT", "USDC", "BUSD", "USD"] where x.hasSuffix(quote) && x.count - quote.count >= 2 {
            return String(x.dropLast(quote.count))
        }
        return x
    }
    static func image(for raw: String) -> UIImage? {
        let symbol = base(of: raw)
        let file = alias[symbol] ?? symbol.lowercased()
        return UIImage(named: "coin-\(file)")
    }
}

struct CoinIcon: View {
    @Environment(\.palette) private var p
    let base: String
    var size: CGFloat = Metrics.coinIcon
    var body: some View {
        let symbol = CoinArt.base(of: base)
        let image = CoinArt.image(for: symbol)
        ZStack {
            Circle().fill(Self.tint(symbol).opacity(0.18))
            if image == nil {
                Text(String(symbol.prefix(1))).font(.lb(size * 0.42, .semibold)).foregroundColor(Self.tint(symbol))
            }
            if let image { Image(uiImage: image).resizable().scaledToFit() }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .id(symbol)
    }

    private static func tint(_ base: String) -> Color {
        let hues: [Color] = [.hex(0xF7931A), .hex(0x627EEA), .hex(0x26A17B), .hex(0xE84142), .hex(0x9BCB2A), .hex(0x2775CA)]
        let n = base.unicodeScalars.reduce(0) { $0 + Int($1.value) }
        return hues[n % hues.count]
    }
}

// MARK: - 空状态
struct EmptyStateView: View {
    @Environment(\.palette) private var p
    var text = "暂无数据"
    var body: some View {
        VStack(spacing: Spacing.sm) {
            RoundedRectangle(cornerRadius: Radius.md).fill(p.fillStrong).frame(width: 56, height: 44)
                .overlay(RoundedRectangle(cornerRadius: 3).fill(p.brand).frame(width: 28, height: 20).offset(y: -6))
            Text(text).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 40)
    }
}

// MARK: - 底部 TabBar
// 参考 App 用的是系统底栏样式：白色不透明通栏、上沿 0.5pt 分隔线、图标 24–25pt，
// 选中项图标与文字同时变绿，底部 34pt 安全区一起染色。这里按实机测量复刻（iOS 16 上 375×812：
// 栏体 49pt，从 y=716 起，选中图标 ≈25pt）。
enum MainTab: Int, CaseIterable {
    case home, markets, futures, spot, assets
    var title: String { ["首页", "行情", "合约", "现货", "资产"][rawValue] }
    var icon: LBIcon { [.tabHome, .tabMarket, .tabFutures, .tabSpot, .tabAssets][rawValue] }
}

struct FloatingTabBar: View {
    @Environment(\.palette) private var p
    @Binding var selected: MainTab
    var body: some View {
        VStack(spacing: 0) {
            Rectangle().fill(p.tabBarBorder).frame(height: 0.5)
            HStack(spacing: 0) {
                ForEach(MainTab.allCases, id: \.self) { tab in
                    let on = selected == tab
                    VStack(spacing: 2) {
                        IconView(icon: tab.icon, size: Metrics.tabBarIcon, tint: on ? p.tabBarTint : p.textSecondary)
                        Text(tab.title)
                            .font(.lb(FontSize.tabBar, on ? .semibold : .regular))
                            .foregroundColor(on ? p.tabBarTint : p.textSecondary)
                    }
                    .frame(maxWidth: .infinity).frame(height: Metrics.tabBarH)
                    .contentShape(Rectangle())
                    .onTapGesture { selected = tab }
                }
            }
            .padding(.top, 5)
            .background(p.tabBarBg)
            .background(p.tabBarBg.ignoresSafeArea(edges: .bottom))
        }
    }
}
