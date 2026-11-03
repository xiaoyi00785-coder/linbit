import Foundation

/// 合约行情 / 历史 / 风险数据接口（均来自 docs/api/openapi.yaml，未自行新增路径）
extension FuturesAPI {
    static func klines(_ symbol: String, interval: String, limit: Int = 120) async throws -> [APIFuturesKlinesKlinesInner] {
        let r: APIFuturesKlines = try await api.get("/futures/market/klines", query: ["symbol": symbol, "interval": interval, "limit": "\(limit)"])
        return r.klines ?? []
    }
    static func marketTrades(_ symbol: String, limit: Int = 50) async throws -> [APIFuturesPublicTrade] {
        let r: APIFuturesMarketTrades200Response = try await api.get("/futures/market/trades", query: ["symbol": symbol, "limit": "\(limit)"])
        return r.trades ?? []
    }
    /// 资金费率历史：翻页时 endTime 传上一页的 nextEndTime
    static func fundingHistory(_ symbol: String, endTime: Int64? = nil, limit: Int = 50) async throws -> APIFuturesFundingHistory200Response {
        try await api.get("/futures/market/funding-history", query: ["symbol": symbol, "limit": "\(limit)", "endTime": endTime.map { "\($0)" }])
    }
    static func riskTiers(_ symbol: String? = nil) async throws -> [APIFuturesRiskTiers200ResponseContractsInner] {
        let r: APIFuturesRiskTiers200Response = try await api.get("/futures/market/risk-tiers", query: ["symbol": symbol])
        return r.contracts ?? []
    }
    /// 全部委托（不带 status，含已成交 / 已撤销）
    static func orders(symbol: String? = nil, limit: Int = 100) async throws -> [APIFuturesOrder] {
        let r: APIFuturesOrders200Response = try await api.get("/futures/orders", query: ["symbol": symbol, "limit": "\(limit)"], auth: true)
        return r.orders ?? []
    }
    static func myTrades(symbol: String? = nil, limit: Int = 100) async throws -> [APIFuturesMyTrades200ResponseTradesInner] {
        let r: APIFuturesMyTrades200Response = try await api.get("/futures/trades", query: ["symbol": symbol, "limit": "\(limit)"], auth: true)
        return r.trades ?? []
    }
    static func bills(kind: String? = nil, limit: Int = 100) async throws -> [APIFuturesBills200ResponseBillsInner] {
        let r: APIFuturesBills200Response = try await api.get("/futures/bills", query: ["kind": kind, "limit": "\(limit)"], auth: true)
        return r.bills ?? []
    }
}
