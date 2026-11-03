import Foundation

enum AppConfig {
    static var apiBase: URL {
        URL(string: Bundle.main.object(forInfoDictionaryKey: "LBAPIBase") as? String ?? "https://api.cowa.click")!
    }
    static var wsURL: URL {
        URL(string: Bundle.main.object(forInfoDictionaryKey: "LBWSURL") as? String ?? "wss://api.cowa.click/ws")!
    }
    /// USDT→CNY 折算汇率。TODO：后端暂无汇率接口，先用固定值，接口就绪后替换。
    static var usdtCny: Decimal = Decimal(string: "7.10")!
    /// 后端按 UA 识别客户端（所有 REST 与 WS 请求都带）
    static let userAgent = "Linbit-iOS/1.0"
}
