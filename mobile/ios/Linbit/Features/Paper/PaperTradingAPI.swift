import Foundation
import Combine

/// Paper Trading 客户端（model / API / gate / session；UI 见 PaperTradingView）。
/// 契约对齐 origin/main OpenAPI：`/paper/*`（account · reset · balances · orders · trades）。
///
/// 安全边界：
/// - **永不**调用 `/spot/orders` 等真钱路径；下单/重置只走 `/paper/...`
/// - 金额字段保持十进制**字符串原样**，不经 `Double`/`Float`
/// - 功能开关 `paperTrading`：**缺失或 false = 关闭**（不用 `AppFeatures.isOn` 的缺省 true）
/// - DEBUG + `lb.shot`：写路径本地 mock / `SHOT_BLOCKED`，不打远端；Release `ShotGate` 关闭后走正式 API
/// - 会话 `reloadSeq` / `writeGen` 丢弃切账号后的旧响应与写成功

// MARK: - Models（十进制金额均为 String）

struct PaperAccount: Codable, Hashable {
    var accountId: String
    var mode: String              // paper
    var market: String            // spot
    var generation: Int64
    var quoteAsset: String        // USDT
    var initialBalance: String    // decimal string
    var resetLimitPerUtcDay: Int
    var resetsUsedToday: Int
    var nextResetAt: Date?
    var orderTypes: [String]      // market
    var priceSource: String       // INDEX
    var feeModel: String          // none
    var slippageModel: String     // none
    var createdAt: Date?
    var updatedAt: Date?
}

struct PaperBalance: Codable, Hashable, Identifiable {
    var id: String { asset }
    var asset: String
    var free: String
    var locked: String
    var total: String
    var valuationUsd: String?
    var priceUsd: String?
}

struct PaperResetRequest: Encodable {
    /// OpenAPI enum: RESET_PAPER_ACCOUNT
    var confirmation: String = "RESET_PAPER_ACCOUNT"
}

struct PaperReset: Codable, Hashable {
    var accountId: String
    var mode: String
    var generation: Int64
    var balances: [PaperBalance]
    var resetAt: Date?
}

struct PaperBalancesPage: Codable, Hashable {
    var items: [PaperBalance]
    var page: Int?
    var pageSize: Int?
    var total: Int?
    var account: String?       // paper
    var mode: String?          // paper
    var generation: Int64?
}

/// 下单请求：market-only；sell 必填 quantity；buy 的 quantity / quoteOrderQty 二选一。空串=未提供。
struct PaperPlaceOrderRequest: Encodable {
    var symbol: String
    var side: String              // buy | sell
    var type: String = "market"
    var quantity: String = ""
    var quoteOrderQty: String = ""
    var clientOrderId: String
}

struct PaperOrder: Codable, Hashable, Identifiable {
    var id: String { orderId }
    var orderId: String
    var clientOrderId: String
    var mode: String
    var accountId: String
    var generation: Int64
    var symbol: String
    var side: String
    var type: String
    var price: String?            // market → null
    var quantity: String
    var quoteOrderQty: String?
    var filledQuantity: String
    var status: String            // filled
    var priceSource: String       // INDEX
    var referencePrice: String
    var marketObservedAt: Date?
    var fee: String
    var feeAsset: String
    var createdAt: Date?
    var executedAt: Date?
    var trades: [PaperTrade]?
}

struct PaperOrdersPage: Codable, Hashable {
    var items: [PaperOrder]
    var page: Int?
    var pageSize: Int?
    var total: Int?
    var generation: Int64?
}

struct PaperTrade: Codable, Hashable, Identifiable {
    var id: String { tradeId }
    var tradeId: String
    var orderId: String
    var mode: String
    var symbol: String
    var side: String
    var price: String
    var quantity: String
    var fee: String
    var feeAsset: String
    var role: String              // taker
    var priceSource: String
    var marketObservedAt: Date?
    var createdAt: Date?
}

struct PaperTradesPage: Codable, Hashable {
    var items: [PaperTrade]
    var page: Int?
    var pageSize: Int?
    var total: Int?
    var generation: Int64?
}

// MARK: - Feature gate（缺省关闭）

enum PaperTradingFeature {
    /// `/app/features` key；与后端约定 `paperTrading`
    static let flagKey = "paperTrading"

    /// **缺失或 false = 关**。禁止用 `AppFeatures.isOn`（其对未知 key 默认 true）。
    @MainActor
    static var isFlagOn: Bool {
        AppFeatures.shared.flags[flagKey] == true
    }

    static func isDisabledFailure(_ error: Error) -> Bool {
        (error as? APIFailure)?.code == "FEATURE_DISABLED"
    }

    static func isIndexUnavailable(_ error: Error) -> Bool {
        (error as? APIFailure)?.code == "INDEX_UNAVAILABLE" || (error as? APIFailure)?.status == 503
    }

    static func isResetLimitExceeded(_ error: Error) -> Bool {
        (error as? APIFailure)?.code == "PAPER_RESET_LIMIT_EXCEEDED" || (error as? APIFailure)?.status == 429
    }

    /// buy：quoteOrderQty 或 quantity 二选一正数串；sell：quantity 必填正数串。不转浮点。
    static func validatePlace(_ body: PaperPlaceOrderRequest) -> String? {
        let side = body.side.lowercased()
        let qty = body.quantity.trimmingCharacters(in: .whitespaces)
        let quote = body.quoteOrderQty.trimmingCharacters(in: .whitespaces)
        func positiveDecimal(_ s: String) -> Bool {
            guard !s.isEmpty, s.first != "-", s.first != "+", !s.lowercased().contains("e") else { return false }
            // 仅允许 [0-9]. 结构；值用字符串比较前先粗检
            let okChars = s.allSatisfy { $0.isNumber || $0 == "." }
            guard okChars, s.filter({ $0 == "." }).count <= 1 else { return false }
            return Dec.parse(s) > 0
        }
        if side == "sell" {
            if !positiveDecimal(qty) { return "卖单须提供正数 quantity（十进制字符串）" }
            if !quote.isEmpty { return "卖单不要填 quoteOrderQty" }
            return nil
        }
        if side == "buy" {
            let hasQ = positiveDecimal(qty)
            let hasQuote = positiveDecimal(quote)
            if hasQ == hasQuote { return "买单须在 quantity 与 quoteOrderQty 中二选一（正数十进制字符串）" }
            return nil
        }
        return "side 仅为 buy|sell"
    }
}

// MARK: - REST（仅 /paper/*）

enum PaperTradingAPI {
    static var api: APIClient { .shared }

    /// GET /paper/account
    static func account() async throws -> PaperAccount {
        try await api.get("/paper/account", auth: true)
    }

    /// POST /paper/account/reset + Idempotency-Key
    static func reset(idempotencyKey: String, body: PaperResetRequest = .init()) async throws -> PaperReset {
        try await api.post(
            "/paper/account/reset",
            body: body,
            auth: true,
            headers: ["Idempotency-Key": idempotencyKey]
        )
    }

    /// GET /paper/balances
    static func balances(page: Int = 1, pageSize: Int = 20) async throws -> PaperBalancesPage {
        try await api.get(
            "/paper/balances",
            query: ["page": "\(page)", "pageSize": "\(pageSize)"],
            auth: true
        )
    }

    /// GET /paper/orders
    static func orders(symbol: String? = nil, status: String? = nil, generation: Int64? = nil, page: Int = 1, pageSize: Int = 20) async throws -> PaperOrdersPage {
        try await api.get(
            "/paper/orders",
            query: [
                "symbol": symbol,
                "status": status,
                "generation": generation.map { "\($0)" },
                "page": "\(page)",
                "pageSize": "\(pageSize)",
            ],
            auth: true
        )
    }

    /// POST /paper/orders — **不是** /spot/orders
    static func place(_ body: PaperPlaceOrderRequest) async throws -> PaperOrder {
        try await api.post("/paper/orders", body: body, auth: true)
    }

    /// GET /paper/orders/{orderId}
    static func order(orderId: String) async throws -> PaperOrder {
        try await api.get("/paper/orders/\(orderId.urlPath)", auth: true)
    }

    /// GET /paper/trades
    static func trades(symbol: String? = nil, generation: Int64? = nil, page: Int = 1, pageSize: Int = 20) async throws -> PaperTradesPage {
        try await api.get(
            "/paper/trades",
            query: [
                "symbol": symbol,
                "generation": generation.map { "\($0)" },
                "page": "\(page)",
                "pageSize": "\(pageSize)",
            ],
            auth: true
        )
    }

    static func retryAfterSeconds(from error: Error) -> Int? {
        if let f = error as? APIFailure, let ra = f.retryAfter, ra > 0 { return ra }
        return nil
    }
}

// MARK: - Session

@MainActor
final class PaperTradingSession: ObservableObject {
    @Published private(set) var account: Loadable<PaperAccount> = .idle
    @Published private(set) var balances: Loadable<PaperBalancesPage> = .idle
    @Published private(set) var orders: Loadable<PaperOrdersPage> = .idle
    @Published private(set) var trades: Loadable<PaperTradesPage> = .idle
    /// 分页/刷新失败时保留已加载列表，仅挂错误文案供同页重试
    @Published var ordersError: String? = nil
    @Published var tradesError: String? = nil
    @Published private(set) var featureDisabled = false
    @Published private(set) var rateLimitUntil: Date?

    private var reloadSeq: UInt64 = 0
    private var writeGen: UInt64 = 0
    private var balPage = 1
    private var ordersPageNum = 1
    private var tradesPageNum = 1
    private var ordersSymbol: String? = nil
    private var ordersGeneration: Int64? = nil
    private var tradesSymbol: String? = nil
    private var tradesGeneration: Int64? = nil
    @Published private(set) var loadingMoreBalances = false
    @Published private(set) var loadingMoreOrders = false
    @Published private(set) var loadingMoreTrades = false

    var canLoadMoreBalances: Bool {
        guard case let .loaded(p) = balances, let total = p.total else { return false }
        return p.items.count < total
    }

    var isRateLimited: Bool {
        if let t = rateLimitUntil { return t > Date() }
        return false
    }

    var rateLimitRemainingSec: Int {
        guard let t = rateLimitUntil else { return 0 }
        return max(0, Int(t.timeIntervalSinceNow.rounded(.up)))
    }

    func invalidate() {
        reloadSeq &+= 1
        writeGen &+= 1
        account = .idle
        balances = .idle
        orders = .idle
        trades = .idle
        ordersError = nil
        tradesError = nil
        featureDisabled = false
        rateLimitUntil = nil
        ordersPageNum = 1
        tradesPageNum = 1
    }

    func reloadAccount() async {
        if PaperTradingDemo.on {
            featureDisabled = PaperTradingDemo.featureDisabled
            if PaperTradingDemo.featureDisabled {
                account = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                return
            }
            if PaperTradingDemo.forceError {
                account = .failed("网络连接失败，请检查网络")
                return
            }
            if let a = PaperTradingDemo.account { account = .loaded(a) }
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        account = .loading
        do {
            let r = try await PaperTradingAPI.account()
            guard seq == reloadSeq else { return }
            featureDisabled = false
            account = .loaded(r)
        } catch {
            guard seq == reloadSeq else { return }
            if PaperTradingFeature.isDisabledFailure(error) {
                featureDisabled = true
                account = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                return
            }
            account = .failed(error.lbMessage)
        }
    }

    func reloadBalances(page: Int = 1) async {
        balPage = page
        if PaperTradingDemo.on {
            if let b = PaperTradingDemo.balancesPage { balances = .loaded(b) }
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        balances = .loading
        do {
            let r = try await PaperTradingAPI.balances(page: page)
            guard seq == reloadSeq else { return }
            balances = .loaded(r)
        } catch {
            guard seq == reloadSeq else { return }
            if PaperTradingFeature.isDisabledFailure(error) {
                featureDisabled = true
                balances = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                return
            }
            balances = .failed(error.lbMessage)
        }
    }

    func loadMoreBalances() async {
        guard canLoadMoreBalances, !loadingMoreBalances, !PaperTradingDemo.on else { return }
        loadingMoreBalances = true
        defer { loadingMoreBalances = false }
        let next = balPage + 1
        reloadSeq &+= 1
        let seq = reloadSeq
        do {
            let r = try await PaperTradingAPI.balances(page: next)
            guard seq == reloadSeq else { return }
            if case let .loaded(cur) = balances {
                var merged = cur
                merged.items.append(contentsOf: r.items)
                merged.page = r.page ?? next
                merged.total = r.total ?? cur.total
                balances = .loaded(merged)
                balPage = next
            } else {
                balances = .loaded(r)
                balPage = next
            }
        } catch {
            guard seq == reloadSeq else { return }
            if let f = error as? APIFailure, f.status == 429 { applyRateLimit(from: error) }
            ToastCenter.shared.show(error.lbMessage, .error)
        }
    }

    var canLoadMoreOrders: Bool {
        guard case let .loaded(p) = orders, let total = p.total else { return false }
        return p.items.count < total
    }

    var canLoadMoreTrades: Bool {
        guard case let .loaded(p) = trades, let total = p.total else { return false }
        return p.items.count < total
    }

    func reloadOrders(symbol: String? = nil, generation: Int64? = nil, page: Int = 1) async {
        ordersSymbol = symbol
        ordersGeneration = generation
        ordersPageNum = page
        ordersError = nil
        if PaperTradingDemo.on {
            if PaperTradingDemo.forceError && PaperTradingDemo.historyKind == .orders {
                if case .loaded = orders { ordersError = "网络连接失败，请检查网络" }
                else { orders = .failed("网络连接失败，请检查网络") }
                return
            }
            if let o = PaperTradingDemo.ordersPage(page: page) { orders = .loaded(o) }
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        let keep = orders.value != nil
        if !keep { orders = .loading }
        do {
            let r = try await PaperTradingAPI.orders(symbol: symbol, generation: generation, page: page)
            guard seq == reloadSeq else { return }
            orders = .loaded(r)
            ordersError = nil
        } catch {
            guard seq == reloadSeq else { return }
            if let f = error as? APIFailure, f.status == 429 { applyRateLimit(from: error) }
            if keep {
                ordersError = error.lbMessage // 保留原列表，同页可重试
            } else {
                orders = .failed(error.lbMessage)
            }
        }
    }

    func loadMoreOrders() async {
        guard canLoadMoreOrders, !loadingMoreOrders else { return }
        loadingMoreOrders = true
        defer { loadingMoreOrders = false }
        let next = ordersPageNum + 1
        if PaperTradingDemo.on {
            guard let more = PaperTradingDemo.ordersPage(page: next), case let .loaded(cur) = orders else { return }
            var merged = cur
            merged.items.append(contentsOf: more.items)
            merged.page = more.page ?? next
            merged.total = more.total ?? cur.total
            orders = .loaded(merged)
            ordersPageNum = next
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        do {
            let r = try await PaperTradingAPI.orders(symbol: ordersSymbol, generation: ordersGeneration, page: next)
            guard seq == reloadSeq else { return }
            if case let .loaded(cur) = orders {
                var merged = cur
                merged.items.append(contentsOf: r.items)
                merged.page = r.page ?? next
                merged.total = r.total ?? cur.total
                orders = .loaded(merged)
                ordersPageNum = next
            }
        } catch {
            guard seq == reloadSeq else { return }
            if let f = error as? APIFailure, f.status == 429 { applyRateLimit(from: error) }
            ordersError = error.lbMessage
            ToastCenter.shared.show(error.lbMessage, .error)
        }
    }

    func reloadTrades(symbol: String? = nil, generation: Int64? = nil, page: Int = 1) async {
        tradesSymbol = symbol
        tradesGeneration = generation
        tradesPageNum = page
        tradesError = nil
        if PaperTradingDemo.on {
            if PaperTradingDemo.forceError && PaperTradingDemo.historyKind == .trades {
                if case .loaded = trades { tradesError = "网络连接失败，请检查网络" }
                else { trades = .failed("网络连接失败，请检查网络") }
                return
            }
            if let o = PaperTradingDemo.tradesPage(page: page) { trades = .loaded(o) }
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        let keep = trades.value != nil
        if !keep { trades = .loading }
        do {
            let r = try await PaperTradingAPI.trades(symbol: symbol, generation: generation, page: page)
            guard seq == reloadSeq else { return }
            trades = .loaded(r)
            tradesError = nil
        } catch {
            guard seq == reloadSeq else { return }
            if let f = error as? APIFailure, f.status == 429 { applyRateLimit(from: error) }
            if keep {
                tradesError = error.lbMessage
            } else {
                trades = .failed(error.lbMessage)
            }
        }
    }

    func loadMoreTrades() async {
        guard canLoadMoreTrades, !loadingMoreTrades else { return }
        loadingMoreTrades = true
        defer { loadingMoreTrades = false }
        let next = tradesPageNum + 1
        if PaperTradingDemo.on {
            guard let more = PaperTradingDemo.tradesPage(page: next), case let .loaded(cur) = trades else { return }
            var merged = cur
            merged.items.append(contentsOf: more.items)
            merged.page = more.page ?? next
            merged.total = more.total ?? cur.total
            trades = .loaded(merged)
            tradesPageNum = next
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        do {
            let r = try await PaperTradingAPI.trades(symbol: tradesSymbol, generation: tradesGeneration, page: next)
            guard seq == reloadSeq else { return }
            if case let .loaded(cur) = trades {
                var merged = cur
                merged.items.append(contentsOf: r.items)
                merged.page = r.page ?? next
                merged.total = r.total ?? cur.total
                trades = .loaded(merged)
                tradesPageNum = next
            }
        } catch {
            guard seq == reloadSeq else { return }
            if let f = error as? APIFailure, f.status == 429 { applyRateLimit(from: error) }
            tradesError = error.lbMessage
            ToastCenter.shared.show(error.lbMessage, .error)
        }
    }

    /// 写：DEBUG+shot → mock；否则正式 `/paper/orders`。校验十进制串；切会话丢弃成功。
    func placeForTestOrUI(_ body: PaperPlaceOrderRequest) async throws -> PaperOrder {
        if let err = PaperTradingFeature.validatePlace(body) {
            throw APIFailure.server(status: 400, error: APIError(code: "VALIDATION_ERROR", message: err), retryAfter: nil)
        }
        let gen = writeGen
        if let mocked = try PaperTradingDemo.interceptPlace(body) {
            guard gen == writeGen else { throw Self.staleWriteError }
            return mocked
        }
        if isRateLimited {
            throw APIFailure.server(status: 429, error: APIError(code: "RATE_LIMITED", message: "操作过于频繁，请稍后再试"), retryAfter: rateLimitRemainingSec)
        }
        do {
            let r = try await PaperTradingAPI.place(body)
            guard gen == writeGen else { throw Self.staleWriteError }
            return r
        } catch {
            applyRateLimit(from: error)
            throw error
        }
    }

    func resetForTestOrUI(idempotencyKey: String) async throws -> PaperReset {
        let gen = writeGen
        if let mocked = try PaperTradingDemo.interceptReset(idempotencyKey: idempotencyKey) {
            guard gen == writeGen else { throw Self.staleWriteError }
            return mocked
        }
        if isRateLimited {
            throw APIFailure.server(status: 429, error: APIError(code: "RATE_LIMITED", message: "操作过于频繁，请稍后再试"), retryAfter: rateLimitRemainingSec)
        }
        do {
            let r = try await PaperTradingAPI.reset(idempotencyKey: idempotencyKey)
            guard gen == writeGen else { throw Self.staleWriteError }
            return r
        } catch {
            applyRateLimit(from: error)
            throw error
        }
    }

    private func applyRateLimit(from error: Error) {
        if let sec = PaperTradingAPI.retryAfterSeconds(from: error), sec > 0 {
            rateLimitUntil = Date().addingTimeInterval(TimeInterval(sec))
        }
    }

    private static var staleWriteError: APIFailure {
        .server(status: 409, error: APIError(code: "SESSION_STALE", message: "登录状态已变更，未应用本次模拟操作"), retryAfter: nil)
    }
}

// MARK: - DEBUG fixture

enum PaperTradingDemo {
    /// `lb.shot` 前缀 `paper` / `paper-` / `paper:`
    static var on: Bool {
        guard ShotGate.enabled, let s = ShotGate.launchShot else { return false }
        return s == "paper" || s.hasPrefix("paper-") || s.hasPrefix("paper:")
    }

    /// Release 恒 false。DEBUG 且任意 lb.shot → 拦截远端写（含非 paper 截图，防误写）。
    static var blocksRemoteWrites: Bool {
        guard ShotGate.enabled else { return false }
        return ShotGate.launchShot != nil
    }

    static var featureDisabled: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("disabled") || s.hasSuffix(":disabled")
    }

    static var forceError: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("error") || s.hasSuffix(":error")
    }

    static var openReset: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s == "paper-reset" || s.hasSuffix(":reset") || s.contains("-reset")
    }

    /// paper-place | paper-place-ok | paper-place-fail
    static var openPlace: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.hasPrefix("paper-place") || s.contains("-place")
    }

    static var placeForceFail: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("place-fail") || s.hasSuffix(":place-fail") || s.contains("place-error")
    }

    static var placeForceOk: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("place-ok") || s.hasSuffix(":place-ok") || s.contains("place-success")
    }

    static var mockWriteLog: [(op: String, id: String)] = []
    static func resetMockLog() { mockWriteLog.removeAll() }

    static var account: PaperAccount? {
        guard on, !featureDisabled, !forceError else { return nil }
        return exampleAccount()
    }

    static var balancesPage: PaperBalancesPage? {
        guard on, !featureDisabled, !forceError else { return nil }
        if let s = ShotGate.launchShot, s.contains("empty") {
            return PaperBalancesPage(items: [], page: 1, pageSize: 20, total: 0, account: "paper", mode: "paper", generation: 1)
        }
        return PaperBalancesPage(
            items: [exampleBalance()],
            page: 1, pageSize: 20, total: 1,
            account: "paper", mode: "paper", generation: 1
        )
    }

    enum HistoryKind { case none, orders, trades }
    static var historyKind: HistoryKind {
        guard let s = ShotGate.launchShot else { return .none }
        if s.contains("trades") { return .trades }
        if s.contains("orders") { return .orders }
        return .none
    }

    static var openHistory: Bool { historyKind != .none }

    static func ordersPage(page: Int = 1) -> PaperOrdersPage? {
        guard on, !featureDisabled else { return nil }
        if forceError && historyKind == .orders { return nil }
        if let s = ShotGate.launchShot, s.contains("empty") {
            return PaperOrdersPage(items: [], page: 1, pageSize: 3, total: 0, generation: 1)
        }
        let all: [PaperOrder] = [
            exampleOrder(side: "buy", id: "po_demo_1"),
            exampleOrder(side: "sell", id: "po_demo_2"),
            exampleOrder(side: "buy", id: "po_demo_3"),
            exampleOrder(side: "sell", id: "po_demo_4"),
            exampleOrder(side: "buy", id: "po_demo_5"),
        ]
        let pageSize = 3
        let start = (page - 1) * pageSize
        guard start < all.count else {
            return PaperOrdersPage(items: [], page: page, pageSize: pageSize, total: all.count, generation: 1)
        }
        let slice = Array(all[start..<min(start + pageSize, all.count)])
        return PaperOrdersPage(items: slice, page: page, pageSize: pageSize, total: all.count, generation: 1)
    }

    static func tradesPage(page: Int = 1) -> PaperTradesPage? {
        guard on, !featureDisabled else { return nil }
        if forceError && historyKind == .trades { return nil }
        if let s = ShotGate.launchShot, s.contains("empty") {
            return PaperTradesPage(items: [], page: 1, pageSize: 3, total: 0, generation: 1)
        }
        let all: [PaperTrade] = [
            exampleTrade(id: "pt_demo_1", side: "buy"),
            exampleTrade(id: "pt_demo_2", side: "sell"),
            exampleTrade(id: "pt_demo_3", side: "buy"),
            exampleTrade(id: "pt_demo_4", side: "sell"),
            exampleTrade(id: "pt_demo_5", side: "buy"),
        ]
        let pageSize = 3
        let start = (page - 1) * pageSize
        guard start < all.count else {
            return PaperTradesPage(items: [], page: page, pageSize: pageSize, total: all.count, generation: 1)
        }
        let slice = Array(all[start..<min(start + pageSize, all.count)])
        return PaperTradesPage(items: slice, page: page, pageSize: pageSize, total: all.count, generation: 1)
    }

    static func interceptPlace(_ body: PaperPlaceOrderRequest) throws -> PaperOrder? {
        guard blocksRemoteWrites else { return nil }
        guard on else {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图夹具不提交模拟订单"), retryAfter: nil)
        }
        if placeForceFail {
            mockWriteLog.append((op: "place-fail", id: body.clientOrderId))
            throw APIFailure.server(status: 503, error: APIError(code: "INDEX_UNAVAILABLE", message: "新鲜外部指数不可用"), retryAfter: nil)
        }
        var o = exampleOrder(side: body.side)
        o.clientOrderId = body.clientOrderId
        o.symbol = body.symbol
        o.quantity = body.quantity.isEmpty ? (body.side == "buy" ? "0.001" : body.quantity) : body.quantity
        o.quoteOrderQty = body.quoteOrderQty.isEmpty ? nil : body.quoteOrderQty
        o.orderId = "po_mock_\(mockWriteLog.count + 1)"
        mockWriteLog.append((op: "place", id: o.orderId))
        return o
    }

    static func interceptReset(idempotencyKey: String) throws -> PaperReset? {
        guard blocksRemoteWrites else { return nil }
        guard on else {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图夹具不重置模拟账户"), retryAfter: nil)
        }
        mockWriteLog.append((op: "reset", id: idempotencyKey))
        return PaperReset(
            accountId: "pa_demo_1",
            mode: "paper",
            generation: 2,
            balances: [exampleBalance()],
            resetAt: nil
        )
    }

    static func exampleAccount() -> PaperAccount {
        PaperAccount(
            accountId: "pa_demo_1",
            mode: "paper",
            market: "spot",
            generation: 1,
            quoteAsset: "USDT",
            initialBalance: "10000.00000000",
            resetLimitPerUtcDay: 1,
            resetsUsedToday: 0,
            nextResetAt: nil,
            orderTypes: ["market"],
            priceSource: "INDEX",
            feeModel: "none",
            slippageModel: "none",
            createdAt: nil,
            updatedAt: nil
        )
    }

    static func exampleBalance() -> PaperBalance {
        PaperBalance(
            asset: "USDT",
            free: "10000.00000000",
            locked: "0",
            total: "10000.00000000",
            valuationUsd: "10000.00",
            priceUsd: "1"
        )
    }

    static func exampleOrder(side: String, id: String = "po_demo_1") -> PaperOrder {
        PaperOrder(
            orderId: id,
            clientOrderId: "demo.client.1",
            mode: "paper",
            accountId: "pa_demo_1",
            generation: 1,
            symbol: "BTCUSDT",
            side: side,
            type: "market",
            price: nil,
            quantity: "0.010000",
            quoteOrderQty: side == "buy" ? "810.00" : nil,
            filledQuantity: "0.010000",
            status: "filled",
            priceSource: "INDEX",
            referencePrice: "81000.04",
            marketObservedAt: nil,
            fee: "0",
            feeAsset: "USDT",
            createdAt: nil,
            executedAt: nil,
            trades: nil
        )
    }

    static func exampleTrade(id: String, side: String) -> PaperTrade {
        PaperTrade(
            tradeId: id,
            orderId: "po_demo_1",
            mode: "paper",
            symbol: "BTCUSDT",
            side: side,
            price: "81000.04",
            quantity: "0.010000",
            fee: "0",
            feeAsset: "USDT",
            role: "taker",
            priceSource: "INDEX",
            marketObservedAt: nil,
            createdAt: nil
        )
    }
}
