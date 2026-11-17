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
                    Text(items[i])
                        .font(.lb(size, i == selected ? .semibold : .regular))
                        .foregroundColor(i == selected ? p.textPrimary : p.textSecondary)
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
struct CoinIcon: View {
    @Environment(\.palette) private var p
    let base: String
    var size: CGFloat = Metrics.coinIcon
    @State private var image: UIImage?
    var body: some View {
        ZStack {
            Circle().fill(Self.tint(base).opacity(0.18))
            Text(String(base.prefix(1))).font(.lb(size * 0.42, .semibold)).foregroundColor(Self.tint(base))
            if let image { Image(uiImage: image).resizable().scaledToFill() }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .task(id: base) { image = await CoinIconLoader.shared.image(for: base) }
    }

    private static func tint(_ base: String) -> Color {
        let hues: [Color] = [.hex(0xF7931A), .hex(0x627EEA), .hex(0x26A17B), .hex(0xE84142), .hex(0x9BCB2A), .hex(0x2775CA)]
        let n = base.unicodeScalars.reduce(0) { $0 + Int($1.value) }
        return hues[n % hues.count]
    }
}

private final class CoinIconLoader {
    static let shared = CoinIconLoader()
    private let cache = NSCache<NSString, UIImage>()
    private let missing = NSCache<NSString, NSNumber>()

    func image(for base: String) async -> UIImage? {
        let key = base.uppercased() as NSString
        if let hit = cache.object(forKey: key) { return hit }
        if missing.object(forKey: key) != nil { return nil }
        let sym = base.lowercased().filter { $0.isLetter || $0.isNumber }
        guard !sym.isEmpty else { return nil }
        let urls = [
            "https://assets.coincap.io/assets/icons/\(sym)@2x.png",
            "https://cdn.jsdelivr.net/gh/spothq/cryptocurrency-icons@0.18.1/128/color/\(sym).png",
            "https://bin.bnbstatic.com/static/assets/logos/\(sym.uppercased()).png",
            "https://static.okx.com/cdn/oksupport/asset/currency/icon/\(sym).png",
        ]
        for raw in urls {
            guard let url = URL(string: raw) else { continue }
            var req = URLRequest(url: url, timeoutInterval: 8)
            req.setValue("image/png,image/*,*/*", forHTTPHeaderField: "Accept")
            req.setValue("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15", forHTTPHeaderField: "User-Agent")
            guard let (data, resp) = try? await URLSession.shared.data(for: req),
                  let http = resp as? HTTPURLResponse, http.statusCode == 200,
                  let img = UIImage(data: data), img.size.width > 8 else { continue }
            cache.setObject(img, forKey: key)
            return img
        }
        missing.setObject(1, forKey: key)
        return nil
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

// MARK: - 悬浮胶囊 TabBar（对照图底部：高 61，左右各 21，毛玻璃，选中项灰色胶囊）
enum MainTab: Int, CaseIterable {
    case home, markets, futures, spot, assets
    var title: String { ["首页", "行情", "合约", "现货", "资产"][rawValue] }
    var icon: LBIcon { [.tabHome, .tabMarket, .tabFutures, .tabSpot, .tabAssets][rawValue] }
}

struct FloatingTabBar: View {
    @Environment(\.palette) private var p
    @Binding var selected: MainTab
    var body: some View {
        HStack(spacing: 0) {
            ForEach(MainTab.allCases, id: \.self) { tab in
                VStack(spacing: 3) {
                    IconView(icon: tab.icon, size: 20)
                    Text(tab.title).font(.lb(FontSize.tabBar, .medium)).foregroundColor(p.textPrimary)
                }
                .frame(maxWidth: .infinity).frame(height: Metrics.tabBarH - 10)
                .background(Capsule().fill(selected == tab ? p.tabBarSelected : .clear))
                .contentShape(Rectangle())
                .onTapGesture { selected = tab }
            }
        }
        .padding(5)
        .frame(height: Metrics.tabBarH)
        .background(Capsule().fill(.ultraThinMaterial))
        .background(Capsule().fill(p.tabBarBg))
        .overlay(Capsule().stroke(p.tabBarBorder, lineWidth: 0.5))
        .shadow(color: .black.opacity(0.08), radius: 12, y: 4)
        .padding(.horizontal, Metrics.tabBarInset)
    }
}
