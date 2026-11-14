import SwiftUI

struct SpotKline: Decodable {
    var open, high, low, close: String?
}

/// 现货页内展开的 K 线，数据来自 /market/klines。
struct SpotChartPanel: View {
    @Environment(\.palette) private var p
    let symbol: String
    let pricePrec: Int
    @State private var interval = "15m"
    @State private var candles: [APIFuturesKlinesKlinesInner] = []
    @State private var loaded = false
    @State private var error: String?
    private let intervals = ["15m", "1h", "4h", "1d"]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 14) {
                ForEach(intervals, id: \.self) { iv in
                    Text(iv).font(.lb(FontSize.small, interval == iv ? .semibold : .regular))
                        .foregroundColor(interval == iv ? p.textPrimary : p.textSecondary)
                        .onTapGesture { interval = iv; Task { await load() } }
                }
                Spacer()
            }
            if let error {
                ErrorStateView(message: error) { Task { await load() } }.frame(height: 180)
            } else if !loaded {
                ProgressView().frame(maxWidth: .infinity).frame(height: 180)
            } else if candles.isEmpty {
                EmptyStateView(text: "暂无 K 线").frame(height: 180)
            } else {
                CandleChart(candles: candles, pricePrec: pricePrec).frame(height: 180)
            }
        }
        .task(id: "\(symbol)-\(interval)") { await load() }
    }

    private func load() async {
        error = nil
        do {
            let rows: [SpotKline] = try await APIClient.shared.get("/market/klines", query: ["symbol": symbol, "interval": interval, "limit": "120"])
            candles = rows.map { APIFuturesKlinesKlinesInner(_open: $0.open, high: $0.high, low: $0.low, close: $0.close) }
            loaded = true
            error = nil
        } catch {
            self.error = error.lbMessage
        }
    }
}

struct SpotKlineSheet: View {
    @Environment(\.palette) private var p
    let symbol: String
    let onClose: () -> Void
    @State private var interval = "15m"
    @State private var candles: [APIFuturesKlinesKlinesInner] = []
    @State private var error: String?
    private let intervals = ["15m", "1h", "4h", "1d"]

    var body: some View {
        LBSheet(title: "\(symbol) K 线", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    ForEach(intervals, id: \.self) { iv in
                        Text(iv).font(.lb(FontSize.small, interval == iv ? .semibold : .regular))
                            .foregroundColor(interval == iv ? p.textPrimary : p.textSecondary)
                            .onTapGesture { interval = iv; Task { await load() } }
                    }
                }
                if let error {
                    ErrorStateView(message: error) { Task { await load() } }.frame(height: 180)
                } else if candles.isEmpty {
                    ProgressView().frame(height: 180)
                } else {
                    CandleChart(candles: candles, pricePrec: 2).frame(height: 220)
                }
            }.padding(.horizontal, Spacing.pageH)
        }
        .task { await load() }
    }

    private func load() async {
        do {
            let rows: [SpotKline] = try await APIClient.shared.get("/market/klines", query: ["symbol": symbol, "interval": interval, "limit": "120"])
            candles = rows.map { APIFuturesKlinesKlinesInner(_open: $0.open, high: $0.high, low: $0.low, close: $0.close) }
            error = nil
        } catch {
            self.error = error.lbMessage
        }
    }
}

struct PaperAccountDTO: Decodable {
    var balance, available, unrealizedPnl: String?
}

struct PaperFuturesScreen: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var account: PaperAccountDTO?
    @State private var qty = "0.001"
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Text("模拟交易").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                Text("使用模拟资金 USDX，不影响真实余额。").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                if !session.isLoggedIn {
                    EmptyStateView(text: "登录后开始模拟交易")
                } else {
                    Text("余额 \(account?.balance ?? "--") USDX").font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary)
                    Text("可用 \(account?.available ?? "--")").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Text("未实现盈亏 \(account?.unrealizedPnl ?? "--")").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    LBTextField(placeholder: "数量", text: $qty)
                    LBButton(title: "领取 1000 USDX") { Task { await claim() } }
                    LBButton(title: "市价开多 BTCUSDX") { Task { await order("buy") } }
                    LBButton(title: "市价开空 BTCUSDX") { Task { await order("sell") } }
                }
            }.padding(Spacing.pageH)
        }
    }

    private func load() async {
        do { account = try await APIClient.shared.get("/paper/futures/account", auth: true); error = nil }
        catch { self.error = error.lbMessage }
    }
    private func claim() async {
        struct Body: Encodable { let amount: String }
        do { let _: PaperAccountDTO = try await APIClient.shared.post("/paper/futures/account/claim", body: Body(amount: "1000")); await load() }
        catch { self.error = error.lbMessage }
    }
    private func order(_ side: String) async {
        struct Body: Encodable { let symbol, side, type, action, quantity: String }
        do {
            let _: JSONValue = try await APIClient.shared.post("/paper/futures/orders", body: Body(symbol: "BTCUSDX", side: side, type: "market", action: "open", quantity: qty))
            await load()
        } catch { self.error = error.lbMessage }
    }
}

struct PriceAlertSheet: View {
    @Environment(\.palette) private var p
    let symbol: String
    let onClose: () -> Void
    @State private var price = ""
    @State private var error: String?

    var body: some View {
        LBSheet(title: "价格预警", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                Text("\(symbol) 价格上穿后发送站内通知").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                LBTextField(placeholder: "触发价格", text: $price)
                if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                LBButton(title: "创建预警") { Task { await create() } }
            }.padding(.horizontal, Spacing.pageH)
        }
    }

    private func create() async {
        struct Body: Encodable {
            let clientAlertId, market, symbol, condition, threshold, priceSource, triggerMode: String
            let channels: [String]
        }
        do {
            let _: JSONValue = try await APIClient.shared.post("/price-alerts", body: Body(
                clientAlertId: UUID().uuidString, market: "futures", symbol: symbol, condition: "above",
                threshold: price, priceSource: "last", triggerMode: "once", channels: ["inapp"]))
            onClose()
        } catch { self.error = error.lbMessage }
    }
}
