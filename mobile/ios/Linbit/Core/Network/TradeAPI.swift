import Foundation

/// 订单簿档位
struct BookLevel: Identifiable, Equatable {
    var id: Decimal { price }
    let price: Decimal
    let qty: Decimal
}

struct OrderBook: Equatable {
    var bids: [BookLevel] = []
    var asks: [BookLevel] = []   // 价格升序
    var bestBid: Decimal? { bids.first?.price }
    var bestAsk: Decimal? { asks.first?.price }
    /// 买卖力量对比（前 N 档数量），对照图「B 61% / 39% S」
    var buyRatio: Double {
        let b = bids.reduce(Decimal(0)) { $0 + $1.qty }, a = asks.reduce(Decimal(0)) { $0 + $1.qty }
        let t = b + a
        return t == 0 ? 0.5 : NSDecimalNumber(decimal: b / t).doubleValue
    }
    init(bids: [BookLevel] = [], asks: [BookLevel] = []) { self.bids = bids; self.asks = asks }
    init(_ d: APIDepth) {
        func lv(_ r: [[String]]) -> [BookLevel] {
            r.compactMap { row -> BookLevel? in
                guard row.count >= 2, let p = Decimal(string: row[0]), let q = Decimal(string: row[1]) else { return nil }
                return BookLevel(price: p, qty: q)
            }
        }
        bids = lv(d.bids).sorted { $0.price > $1.price }
        asks = lv(d.asks).sorted { $0.price < $1.price }
    }
}

enum SpotAPI {
    static var api: APIClient { .shared }
    static func depth(_ symbol: String, limit: Int = 20) async throws -> OrderBook {
        OrderBook(try await api.get("/market/depth", query: ["symbol": symbol, "limit": "\(limit)"]))
    }
    static func place(_ r: APIPlaceOrderRequest) async throws -> APIOrder { try await api.post("/spot/orders", body: r) }
    static func cancel(_ id: String) async throws -> APIOrder { try await api.delete("/spot/orders/\(id)") }
    static func open(symbol: String? = nil) async throws -> [APIOrder] {
        let r: APIPaginatedOrders = try await api.get("/spot/orders/open", query: ["symbol": symbol, "pageSize": "100"], auth: true)
        return r.items
    }
    static func history(symbol: String?, status: APIOrderStatus?, page: Int) async throws -> APIPaginatedOrders {
        try await api.get("/spot/orders/history", query: ["symbol": symbol, "status": status?.rawValue, "page": "\(page)", "pageSize": "20"], auth: true)
    }
    static func trades(symbol: String?, page: Int) async throws -> APIPaginatedTrades {
        try await api.get("/spot/trades", query: ["symbol": symbol, "page": "\(page)", "pageSize": "20"], auth: true)
    }
}

enum AssetsAPI {
    static var api: APIClient { .shared }
    static func balances() async throws -> [APIBalance] {
        let r: APIPaginatedBalances = try await api.get("/assets/balances", query: ["pageSize": "200"], auth: true)
        return r.items
    }
    static func depositAddress(asset: String, network: String) async throws -> APIDepositAddress {
        try await api.get("/wallet/deposit/address", query: ["asset": asset, "network": network], auth: true)
    }
    static func deposits(page: Int) async throws -> APIPaginatedDeposits {
        try await api.get("/wallet/deposit/records", query: ["page": "\(page)", "pageSize": "20"], auth: true)
    }
    /// 充值和提现共用网络列表（后端暂无单独的充值网络接口）
    static func networks(asset: String) async throws -> [APIWithdrawNetwork] {
        let r: APIWithdrawNetworkOptions = try await api.get("/wallet/withdraw/networks", query: ["asset": asset], auth: true)
        return r.networks
    }
    static func futuresAccount() async throws -> APIFuturesAccount { try await api.get("/futures/account", auth: true) }
    /// in = 现货→合约，out = 合约→现货
    static func transfer(_ dir: APIFuturesTransferRequest.APIDirection, amount: String) async throws -> APIFuturesTransfer200Response {
        try await api.post("/futures/transfer", body: APIFuturesTransferRequest(direction: dir, amount: amount))
    }
    static func transfers() async throws -> [APIFuturesTransfers200ResponseTransfersInner] {
        let r: APIFuturesTransfers200Response = try await api.get("/futures/transfers", auth: true)
        return r.transfers ?? []
    }
}

