import SwiftUI

/// 行情行（对照图 #9：行高 56，图标 32，基础币 16 medium + 「/ USDT」13 灰，价格 16 medium + ¥ 12 灰，色块 76×31）
struct MarketRowView: View {
    @Environment(\.palette) private var p
    let row: MarketRow
    var showIcon = true
    /// 首页榜单样式：显示完整交易对（BTCUSDT），17pt
    var compactPair = false

    var body: some View {
        HStack(spacing: 10) {
            if showIcon { CoinIcon(base: row.base) }
            VStack(alignment: .leading, spacing: 3) {
                if compactPair {
                    Text(row.symbol).font(.lb(FontSize.symbol, .medium)).foregroundColor(p.textPrimary)
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 2) {
                        Text(row.base).font(.lb(FontSize.symbolWithIcon, .medium)).foregroundColor(p.textPrimary)
                        Text("/ \(row.quote)").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    }
                }
                Text(Fmt.volumeCN(row.quoteVolume)).font(.lbNum(FontSize.caption)).foregroundColor(p.textSecondary)
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 3) {
                Text(priceText).font(.lbNum(compactPair ? FontSize.symbol : FontSize.price, .medium)).foregroundColor(p.textPrimary)
                Text(Fmt.fiat(row.last, rate: AppConfig.usdtCny)).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            }
            ChangeBadge(percent: row.changePct).padding(.leading, 12)
        }
        .padding(.horizontal, Spacing.pageH)
        .frame(height: Metrics.marketRowH)
        .contentShape(Rectangle())
    }

    private var priceText: String {
        if let pr = row.precision { return Fmt.price(row.last, precision: pr) }
        return Fmt.priceAuto(row.last)
    }
}

/// 可排序表头
struct MarketListHeader: View {
    @Environment(\.palette) private var p
    var leftTitle = "币种/成交额"
    var sortable = true
    @Binding var field: SortField
    @Binding var dir: SortDir

    var body: some View {
        HStack {
            button(leftTitle, .volume)
            Spacer()
            button("最新价", .price)
            button("涨跌幅", .change).frame(width: Metrics.changeBadgeW + 12, alignment: .trailing)
        }
        .padding(.horizontal, Spacing.pageH)
        .frame(height: 30)
    }

    private func button(_ t: String, _ f: SortField) -> some View {
        SortLabel(title: t, showArrow: sortable)
            .onTapGesture {
                guard sortable else { return }
                if field != f { field = f; dir = .desc } else { dir = dir == .desc ? .asc : dir == .asc ? .none : .desc }
            }
    }
}
