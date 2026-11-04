import Foundation

extension APIOrder {
    /// 按金额下的市价买单：quantity 为 0，展示 "按金额 X USDT"；实际成交见 filledQuantity
    var isByAmount: Bool { Dec.parse(quantity) == 0 && !(quoteOrderQty ?? "").isEmpty }
    var qtyText: String {
        guard isByAmount, let q = quoteOrderQty else { return quantity }
        let quote = symbol.hasSuffix("USDT") ? "USDT" : ""
        return "按金额 \(q) \(quote)".trimmingCharacters(in: .whitespaces)
    }

    /// 截图演示（CI 未登录）：按金额市价买 + 普通限价/市价单
    static var amountDemo: [APIOrder] {
        let now = Date()
        return [
            APIOrder(orderId: "d1", symbol: "BTCUSDT", side: .buy, type: .market, quantity: "0.000000", quoteOrderQty: "100.00",
                     filledQuantity: "0.001302", status: .filled, createdAt: now.addingTimeInterval(-120)),
            APIOrder(orderId: "d2", symbol: "ETHUSDT", side: .buy, type: .market, quantity: "0.0000", quoteOrderQty: "50.00",
                     filledQuantity: "0.0182", status: .filled, createdAt: now.addingTimeInterval(-3600)),
            APIOrder(orderId: "d3", symbol: "BTCUSDT", side: .sell, type: .limit, price: "79200.00", quantity: "0.010000",
                     filledQuantity: "0.004000", status: .partiallyFilled, createdAt: now.addingTimeInterval(-7200)),
            APIOrder(orderId: "d4", symbol: "BTCUSDT", side: .sell, type: .market, quantity: "0.002000",
                     filledQuantity: "0.002000", status: .filled, createdAt: now.addingTimeInterval(-86400)),
        ]
    }
}

extension APIFuturesPosition {
    /// 强平价展示：<= 0、空单高于 10 倍标记价、多单低于 1/10 标记价时视为暂无强平价
    var liqUnavailable: Bool {
        let liq = Dec.parse(liquidationPrice), mark = Dec.parse(markPrice)
        if liquidationPrice == nil || liq <= 0 { return true }
        guard mark > 0 else { return false }
        if side == .short && liq > mark * 10 { return true }
        if side == .long && liq < mark / 10 { return true }
        return false
    }
    var liqText: String { liqUnavailable ? "--" : Fmt.priceAuto(Dec.parse(liquidationPrice)) }
    var liqTip: String? {
        guard liqUnavailable else { return nil }
        return marginMode == .isolated ? "保证金充足，暂无强平价" : "全仓保证金充足，暂无强平价"
    }
}
