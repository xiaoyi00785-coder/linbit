import Foundation

/// 合约高级委托接口：改单、持仓模式、历史仓位、止盈止损委托、计划委托（均来自 mobile/openapi.yaml）
extension FuturesAPI {
    typealias PositionSide = APIFuturesPlaceOrderRequest.APIPositionSide
    typealias AlgoList = APIFuturesListTpslOrders200Response
    typealias AlgoOne = APIFuturesCancelTpslOrders200Response

    /// PUT /futures/orders/{id}：限价单改价 / 改量（至少填一项；qty 为新总数量）
    static func amend(_ id: Int64, price: String?, qty: String?) async throws -> APIFuturesOrder? {
        let r: APIFuturesPlaceOrder200Response = try await api.put("/futures/orders/\(id)", body: APIFuturesAmendOrderRequest(price: price, qty: qty))
        return r.order
    }

    static func positionMode() async throws -> APIFuturesPositionMode.APIPositionMode {
        let r: APIFuturesPositionMode = try await api.get("/futures/position-mode", auth: true)
        return r.positionMode
    }
    static func setPositionMode(_ m: APIFuturesPositionMode.APIPositionMode) async throws -> APIFuturesPositionMode.APIPositionMode {
        let r: APIFuturesPositionMode = try await api.post("/futures/position-mode", body: APIFuturesPositionMode(positionMode: m))
        return r.positionMode
    }

    /// GET /futures/positions/history：cursor 分页；startTime / endTime 为平仓时间（毫秒）
    static func positionHistory(symbol: String?, startTime: Int64?, endTime: Int64?, cursor: Int64?, limit: Int = 50) async throws -> APIFuturesPositionHistory200Response {
        try await api.get("/futures/positions/history", query: [
            "symbol": symbol, "startTime": startTime.map { "\($0)" }, "endTime": endTime.map { "\($0)" },
            "cursor": cursor.map { "\($0)" }, "limit": "\(limit)"], auth: true)
    }

    // 止盈止损委托（可部分仓位）
    static func createTpsl(_ r: APIFuturesTpslOrderRequest) async throws -> [APIFuturesAlgoOrder] {
        let resp: APIFuturesCreateTpslOrders200Response = try await api.post("/futures/tpsl-orders", body: r)
        return resp.orders ?? resp.order.map { [$0] } ?? []
    }
    static func tpslOrders(symbol: String? = nil, history: Bool = false, cursor: Int64? = nil, limit: Int = 50) async throws -> AlgoList {
        try await api.get("/futures/tpsl-orders", query: ["symbol": symbol, "status": history ? "history" : "active",
                                                          "cursor": cursor.map { "\($0)" }, "limit": "\(limit)"], auth: true)
    }
    static func cancelTpsl(_ id: Int64) async throws {
        let _: AlgoOne = try await api.delete("/futures/tpsl-orders/\(id)")
    }

    // 计划委托（触发后以限价 / 市价下单）
    static func createPlan(_ r: APIFuturesPlaceOrderRequest) async throws -> [APIFuturesAlgoOrder] {
        let resp: APIFuturesCreateTpslOrders200Response = try await api.post("/futures/plan-orders", body: r)
        return resp.orders ?? resp.order.map { [$0] } ?? []
    }
    static func planOrders(symbol: String? = nil, history: Bool = false, cursor: Int64? = nil, limit: Int = 50) async throws -> AlgoList {
        try await api.get("/futures/plan-orders", query: ["symbol": symbol, "status": history ? "history" : "active",
                                                          "cursor": cursor.map { "\($0)" }, "limit": "\(limit)"], auth: true)
    }
    static func cancelPlan(_ id: Int64) async throws {
        let _: AlgoOne = try await api.delete("/futures/plan-orders/\(id)")
    }

    /// 分仓模式下平仓需带 positionSide（spec：可放在请求体）
    struct PositionSideBody: Encodable { let positionSide: String }
    static func close(_ symbol: String, positionSide: String?) async throws {
        if let ps = positionSide, ps != "BOTH" {
            let _: APIFuturesPlaceOrder200Response = try await api.post("/futures/positions/\(symbol)/close", body: PositionSideBody(positionSide: ps))
        } else {
            try await close(symbol)
        }
    }
    static func setTpsl(_ symbol: String, tp: String?, sl: String?, positionSide: String?) async throws {
        let ps = positionSide.flatMap(APIFuturesSetTpslRequest.APIPositionSide.init(rawValue:))
        let _: EmptyResponse = try await api.post("/futures/positions/\(symbol)/tpsl", body: APIFuturesSetTpslRequest(tpPrice: tp, slPrice: sl, positionSide: ps))
    }
    struct MarginBody: Encodable { let amount: String; let positionSide: String? }
    static func adjustMargin(_ symbol: String, amount: String, positionSide: String?) async throws {
        let _: EmptyResponse = try await api.post("/futures/positions/\(symbol)/margin", body: MarginBody(amount: amount, positionSide: positionSide))
    }
}

extension APIFuturesAlgoOrder: Identifiable {
    public var id: Int64 { algoId ?? 0 }
}
