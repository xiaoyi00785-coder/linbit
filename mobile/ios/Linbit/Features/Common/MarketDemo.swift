import Foundation

/// DEBUG / ShotGate 行情 example fixture（对齐 OpenAPI depth / ticker / klines shape）。
///
/// - **不是**生产实盘、**不是**真实资金或成交写入。
/// - 仅当 `ShotGate.enabled`（Release 编译期关闭）且 CI/`lb.shot` 命中交易相关屏时启用。
/// - 用于 CI 地区拒绝导致盘口/K 线空白时，仍能验收正常/空/错误 UI。
enum MarketDemo {
    enum Mode: String {
        case ok      // 合成正常盘口 + ticker + K 线
        case empty   // 合法空数据（无档/无烛）
        case error   // 可重试错误态文案
    }

    /// 命中现货/合约/行情截图键时启用（含 chart / conditional 等子屏）。
    static var on: Bool {
        guard ShotGate.enabled, let shot = ShotGate.launchShot, !shot.isEmpty else { return false }
        return covers(shot)
    }

    static func covers(_ shot: String) -> Bool {
        if shot.hasPrefix("futures") { return true }
        if shot.hasPrefix("spot") { return true }
        if shot == "markets" || shot.hasPrefix("markets-") { return true }
        // DemoShot 显式通道：market:ok|empty|error
        if shot.hasPrefix("market:") || shot.hasPrefix("market-") { return true }
        return false
    }

    static var mode: Mode {
        guard let shot = ShotGate.launchShot else { return .ok }
        if shot.hasSuffix("-empty") || shot.hasSuffix(":empty") || shot.contains("market-empty") { return .empty }
        if shot.hasSuffix("-error") || shot.hasSuffix(":error") || shot.contains("market-error") { return .error }
        // futures-pnl-error 等是账户态，不是行情错误
        if shot.contains("pnl-error") || shot.contains("borrow-error") { return .ok }
        return .ok
    }

    // MARK: - API-shaped fixtures（字段名/类型与 Generated 一致）

    static func contract(symbol: String) -> APIFuturesContract {
        APIFuturesContract(
            symbol: symbol,
            baseAsset: String(symbol.dropLast(4)),
            quoteAsset: "USDT",
            maxLeverage: 100,
            maintenanceMarginRate: "0.004",
            tickSize: "0.10",
            minQty: "0.001",
            maxQty: "100",
            makerFeeRate: "0.0002",
            takerFeeRate: "0.0005",
            status: "trading",
            contractType: "perpetual",
            marginAsset: "USDT",
            fundingIntervalHours: 8
        )
    }

    static func futuresTicker(symbol: String) -> APIFuturesTicker {
        let mid = midPrice(symbol)
        let next = Int64((Date().timeIntervalSince1970 + 3600) * 1000)
        return APIFuturesTicker(
            symbol: symbol,
            lastPrice: mid,
            priceSource: "last",
            markPrice: mid,
            indexPrice: bump(mid, -0.0002),
            fundingRate: "0.000100",
            lastFundingRate: "0.000080",
            nextFundingTime: next,
            openPrice24h: bump(mid, -0.012),
            highPrice24h: bump(mid, 0.018),
            lowPrice24h: bump(mid, -0.021),
            volume24h: "12840.552",
            turnover24h: "987654321.12",
            priceChangePercent24h: "0.0066",
            openInterest: "45210.0",
            priceAvailable: true,
            high: bump(mid, 0.018),
            low: bump(mid, -0.021),
            quoteVolume: "987654321.12",
            volume: "12840.552",
            priceChangePercent: "0.66"
        )
    }

    static func depth(symbol: String, levels: Int = 12) -> APIDepth {
        switch mode {
        case .empty:
            return APIDepth(symbol: symbol, lastUpdateId: 1, bids: [], asks: [])
        case .error:
            // 调用方应走 failed；此处仍给合法空结构以防误用
            return APIDepth(symbol: symbol, lastUpdateId: 0, bids: [], asks: [])
        case .ok:
            let mid = Dec.parse(midPrice(symbol))
            var bids: [[String]] = []
            var asks: [[String]] = []
            for i in 0..<levels {
                let off = Decimal(i + 1) * Decimal(string: "0.5")!
                let bq = Decimal(string: String(format: "%.3f", 0.42 - Double(i) * 0.02)) ?? 0.1
                let aq = Decimal(string: String(format: "%.3f", 0.38 - Double(i) * 0.015)) ?? 0.1
                bids.append([Dec.plain(mid - off, 1), Dec.plain(bq, 3)])
                asks.append([Dec.plain(mid + off, 1), Dec.plain(aq, 3)])
            }
            return APIDepth(symbol: symbol, lastUpdateId: 42_000, bids: bids, asks: asks)
        }
    }

    static func orderBook(symbol: String) -> OrderBook { OrderBook(depth(symbol: symbol)) }

    static func spotKlines(symbol: String, interval: String, count: Int = 48) -> [APIKline] {
        switch mode {
        case .empty, .error: return []
        case .ok: return synthSpotCandles(symbol: symbol, interval: interval, count: count)
        }
    }

    static func futuresKlines(symbol: String, interval: String, count: Int = 48) -> [APIFuturesKlinesKlinesInner] {
        switch mode {
        case .empty, .error: return []
        case .ok: return synthFuturesCandles(symbol: symbol, interval: interval, count: count)
        }
    }

    static func futuresTrades(symbol: String, count: Int = 12) -> [APIFuturesPublicTrade] {
        switch mode {
        case .empty, .error: return []
        case .ok:
            let mid = Dec.parse(midPrice(symbol))
            let now = Date()
            return (0..<count).map { i in
                let side: APIFuturesPublicTrade.APITakerSide = i % 3 == 0 ? .sell : .buy
                let px = mid + Decimal(i % 5 - 2) * Decimal(string: "0.5")!
                let qty = Decimal(string: String(format: "%.3f", 0.01 + Double(i % 7) * 0.003)) ?? 0.01
                return APIFuturesPublicTrade(
                    tradeId: Int64(900_000 + i),
                    symbol: symbol,
                    price: Dec.plain(px, 1),
                    qty: Dec.plain(qty, 3),
                    takerSide: side,
                    time: now.addingTimeInterval(TimeInterval(-i * 7))
                )
            }
        }
    }

    static var errorMessage: String { "行情示例加载失败（DEBUG fixture，非实盘）" }

    // MARK: - helpers

    private static func midPrice(_ symbol: String) -> String {
        switch symbol.uppercased() {
        case "ETHUSDT": return "3520.50"
        case "SOLUSDT": return "178.40"
        default: return "67250.50" // BTCUSDT example
        }
    }

    private static func bump(_ mid: String, _ pct: Decimal) -> String {
        let m = Dec.parse(mid)
        return Dec.plain(m * (1 + pct), 2)
    }

    private static func intervalSeconds(_ iv: String) -> Int {
        switch iv {
        case "1h": return 3600
        case "4h": return 14_400
        case "1d": return 86_400
        case "1w": return 604_800
        default: return 900 // 15m
        }
    }

    private static func synthSpotCandles(symbol: String, interval: String, count: Int) -> [APIKline] {
        let mid = Dec.parse(midPrice(symbol))
        let step = intervalSeconds(interval)
        let now = Int(Date().timeIntervalSince1970)
        var out: [APIKline] = []
        var px = mid * Decimal(string: "0.97")!
        for i in 0..<count {
            let t = now - (count - i) * step
            let drift = Decimal(i % 5 - 2) * Decimal(string: "12.5")!
            let o = px
            let c = px + drift
            let h = max(o, c) + Decimal(string: "8")!
            let l = min(o, c) - Decimal(string: "8")!
            let vol = Decimal(string: String(format: "%.3f", 12 + Double(i % 9) * 1.7)) ?? 10
            out.append(APIKline(time: t, _open: Dec.plain(o, 1), high: Dec.plain(h, 1), low: Dec.plain(l, 1), close: Dec.plain(c, 1), volume: Dec.plain(vol, 3)))
            px = c
        }
        return out
    }

    private static func synthFuturesCandles(symbol: String, interval: String, count: Int) -> [APIFuturesKlinesKlinesInner] {
        synthSpotCandles(symbol: symbol, interval: interval, count: count).map { c in
            APIFuturesKlinesKlinesInner(
                openTime: Int64(c.time) * 1000,
                _open: c._open, high: c.high, low: c.low, close: c.close,
                volume: c.volume, turnover: Dec.plain(Dec.parse(c.close) * Dec.parse(c.volume), 2)
            )
        }
    }
}
