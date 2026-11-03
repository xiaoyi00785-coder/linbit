import Foundation

// MARK: - 通用：宽松 JSON 值（运营位 content 等任意对象）
enum JSONValue: Decodable, Hashable {
    case string(String), number(Double), bool(Bool), object([String: JSONValue]), array([JSONValue]), null
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([String: JSONValue].self) { self = .object(v) }
        else if let v = try? c.decode([JSONValue].self) { self = .array(v) }
        else { self = .null }
    }
    var string: String? {
        switch self {
        case let .string(s): return s
        case let .number(n): return String(n)
        case let .bool(b): return b ? "true" : "false"
        default: return nil
        }
    }
}

/// 数值字段可能是字符串也可能是数字（如 fiat-rates 的 rates）
struct LooseDecimal: Decodable, Hashable {
    let value: Decimal
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if let s = try? c.decode(String.self) { value = Dec.parse(s) }
        else if let d = try? c.decode(Double.self) { value = Decimal(string: String(d)) ?? 0 }
        else { value = 0 }
    }
}

// MARK: - 首页：运营位 / 新手任务 / 功能开关 / 法币汇率 / 自选（feat/home-transfer）

/// GET /home/placements → { slots: { carousel: [..], popup: [..], ... }, serverTime }
struct Placement: Decodable, Identifiable, Hashable {
    let id: String
    let slot: String
    let title: String?
    let imageUrl: String?
    let imageDarkUrl: String?
    let linkUrl: String?
    let frequency: String?
    let countdownEndAt: Date?
    let earnProductId: String?
    let earnApr: String?
    let content: [String: JSONValue]
    let sort: Int

    var subtitle: String? { content["subtitle"]?.string }
    var badge: String? { content["badge"]?.string }

    enum K: String, CodingKey { case id, slot, title, imageUrl, imageDarkUrl, linkUrl, frequency, countdownEndAt, earnProductId, earnApr, content, sort }
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: K.self)
        if let i = try? c.decode(Int.self, forKey: .id) { id = String(i) } else { id = (try? c.decode(String.self, forKey: .id)) ?? UUID().uuidString }
        slot = (try? c.decode(String.self, forKey: .slot)) ?? ""
        title = try? c.decode(String.self, forKey: .title)
        imageUrl = try? c.decode(String.self, forKey: .imageUrl)
        imageDarkUrl = try? c.decode(String.self, forKey: .imageDarkUrl)
        linkUrl = try? c.decode(String.self, forKey: .linkUrl)
        frequency = try? c.decode(String.self, forKey: .frequency)
        countdownEndAt = try? c.decode(Date.self, forKey: .countdownEndAt)
        earnProductId = try? c.decode(String.self, forKey: .earnProductId)
        earnApr = try? c.decode(String.self, forKey: .earnApr)
        content = (try? c.decode([String: JSONValue].self, forKey: .content)) ?? [:]
        sort = (try? c.decode(Int.self, forKey: .sort)) ?? 0
    }
}

enum PlacementAPI {
    struct Resp: Decodable { let slots: [String: [Placement]]? }
    static let slots = "carousel,popup,grid,promo_bar,search_hint"
    static func fetch() async throws -> [Placement] {
        let v = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0"
        let r: Resp = try await APIClient.shared.get("/home/placements", query: [
            "slots": slots, "platform": "app", "locale": "zh-CN", "appVersion": v], auth: TokenStore.shared.isLoggedIn)
        var all: [Placement] = []
        for (k, list) in r.slots ?? [:] {
            all += list.map { p in p.slot.isEmpty ? p.with(slot: k) : p }
        }
        return all.sorted { $0.sort < $1.sort }
    }
}

extension Placement {
    func with(slot s: String) -> Placement { Placement(copying: self, slot: s) }
    init(copying p: Placement, slot: String) {
        id = p.id; self.slot = slot; title = p.title; imageUrl = p.imageUrl; imageDarkUrl = p.imageDarkUrl; linkUrl = p.linkUrl
        frequency = p.frequency; countdownEndAt = p.countdownEndAt; earnProductId = p.earnProductId; earnApr = p.earnApr
        content = p.content; sort = p.sort
    }
}

struct Newcomer: Decodable, Hashable {
    struct Task: Decodable, Hashable, Identifiable {
        var id: String { key ?? title ?? "" }
        let key: String?, title: String?, type: String?, kind: String?, amount: String?
        let target: LooseDecimal?, progress: LooseDecimal?, status: String?
    }
    let available: Bool?
    let loggedIn: Bool?
    let enrolled: Bool?
    let total: String?
    let asset: String?
    let days: Int?
    let completed: Int?
    let claimable: String?
    let deadline: Date?
    let link: String?
    let cta: String?
    let degraded: Bool?
    let tasks: [Task]?
}

/// 功能开关：未知 key 视为已开启（旧版服务端无此接口时不阻断已上线功能）。
@MainActor
final class AppFeatures: ObservableObject {
    static let shared = AppFeatures()
    struct Resp: Decodable { let features: [String: Bool]? }
    @Published private(set) var flags: [String: Bool] = [:]
    @Published private(set) var loaded = false
    func isOn(_ key: String) -> Bool { flags[key] ?? true }
    func refresh() async {
        if let r: Resp = try? await APIClient.shared.get("/app/features") { flags = r.features ?? [:] }
        loaded = true
    }
}

/// 法币汇率：GET /market/fiat-rates?base=USDT；失败用 7.10 兜底。
enum FiatRates {
    struct Resp: Decodable { let base: String?; let rates: [String: LooseDecimal]?; let stale: Bool? }
    static func refresh() async {
        guard let r: Resp = try? await APIClient.shared.get("/market/fiat-rates", query: ["base": "USDT"]),
              let v = r.rates?["CNY"]?.value, v > 0 else { return }
        await MainActor.run { AppConfig.usdtCny = v }
    }
}

enum HomeAPI {
    static func newcomer() async throws -> Newcomer {
        try await APIClient.shared.get("/home/newcomer", auth: TokenStore.shared.isLoggedIn)
    }
}

enum FavoritesAPI {
    struct Resp: Decodable { let market: String?; let symbols: [String]? }
    struct Put: Encodable { let market: String; let symbols: [String] }
    static func list(_ market: String = "spot") async throws -> [String] {
        let r: Resp = try await APIClient.shared.get("/account/favorites", query: ["market": market], auth: true)
        return r.symbols ?? []
    }
    static func replace(_ symbols: [String], market: String = "spot") async throws -> [String] {
        let r: Resp = try await APIClient.shared.put("/account/favorites", body: Put(market: market, symbols: symbols))
        return r.symbols ?? []
    }
}

// MARK: - U 本位合约

enum FuturesAPI {
    static var api: APIClient { .shared }
    static func contracts() async throws -> [APIFuturesContract] {
        let r: APIFuturesContractList = try await api.get("/futures/market/contracts")
        return r.contracts ?? []
    }
    static func ticker(_ symbol: String) async throws -> APIFuturesTicker? {
        let r: APIFuturesTicker200Response = try await api.get("/futures/market/ticker", query: ["symbol": symbol])
        switch r {
        case let .typeAPIFuturesTicker(t): return t
        case let .typeAPIFuturesTicker200ResponseOneOf(l): return l.tickers?.first { $0.symbol == symbol }
        }
    }
    static func depth(_ symbol: String, limit: Int = 20) async throws -> OrderBook {
        let d: APIFuturesDepth = try await api.get("/futures/market/depth", query: ["symbol": symbol, "limit": "\(limit)"])
        return OrderBook(APIDepth(symbol: symbol, lastUpdateId: 0, bids: d.bids ?? [], asks: d.asks ?? []))
    }
    static func account() async throws -> APIFuturesAccount { try await api.get("/futures/account", auth: true) }
    static func leverage(_ symbol: String) async throws -> APIFuturesGetLeverage200Response {
        try await api.get("/futures/leverage", query: ["symbol": symbol], auth: true)
    }
    static func setLeverage(_ r: APIFuturesSetLeverageRequest) async throws -> APIFuturesGetLeverage200Response {
        try await api.post("/futures/leverage", body: r)
    }
    static func place(_ r: APIFuturesPlaceOrderRequest) async throws -> APIFuturesPlaceOrder200Response {
        try await api.post("/futures/orders", body: r)
    }
    static func openOrders(symbol: String? = nil) async throws -> [APIFuturesOrder] {
        let r: APIFuturesOrders200Response = try await api.get("/futures/orders", query: ["symbol": symbol, "status": "open", "limit": "100"], auth: true)
        return r.orders ?? []
    }
    static func cancel(_ id: Int64) async throws {
        let _: APIFuturesPlaceOrder200Response = try await api.delete("/futures/orders/\(id)")
    }
    static func positions() async throws -> [APIFuturesPosition] {
        let r: APIFuturesPositions200Response = try await api.get("/futures/positions", auth: true)
        return r.positions ?? []
    }
    static func close(_ symbol: String) async throws {
        let _: APIFuturesPlaceOrder200Response = try await api.post("/futures/positions/\(symbol)/close", body: Optional<Int>.none)
    }
    static func setTpsl(_ symbol: String, tp: String?, sl: String?) async throws {
        let _: EmptyResponse = try await api.post("/futures/positions/\(symbol)/tpsl", body: APIFuturesSetTpslRequest(tpPrice: tp, slPrice: sl))
    }
    static func adjustMargin(_ symbol: String, amount: String) async throws {
        let _: EmptyResponse = try await api.post("/futures/positions/\(symbol)/margin", body: APIFuturesAdjustMarginRequest(amount: amount))
    }
    static func bonus() async throws -> APIFuturesBonus200Response { try await api.get("/futures/bonus", auth: true) }
}

// MARK: - 理财 / 网格策略

enum EarnAPI {
    static var api: APIClient { .shared }
    struct Subscribe: Encodable { let amount: String }
    static func products() async throws -> [APIEarnProduct] {
        let r: APIEarnProducts200Response = try await api.get("/earn/products")
        return r.items ?? []
    }
    static func summary() async throws -> APIEarnSummary200Response { try await api.get("/earn/summary", auth: true) }
    static func positions() async throws -> APIEarnPositions200Response { try await api.get("/earn/positions", auth: true) }
    static func subscribe(_ id: String, amount: String) async throws -> APIEarnSubscribe200Response {
        try await api.post("/earn/products/\(id)/subscribe", body: Subscribe(amount: amount))
    }
    static func redeem(_ id: String, amount: String?, all: Bool) async throws -> APIEarnRedeem200Response {
        try await api.post("/earn/products/\(id)/redeem", body: APIEarnRedeemRequest(amount: amount, all: all))
    }
}

enum GridAPI {
    static var api: APIClient { .shared }
    static func list() async throws -> [APIGridBot] {
        let r: APIStrategyListGrids200Response = try await api.get("/strategy/grid", auth: true)
        return r.items ?? []
    }
    static func preview(_ r: APIGridCreateRequest) async throws -> APIStrategyPreview200Response {
        try await api.get("/strategy/grid/preview", query: [
            "symbol": r.symbol, "mode": r.mode.rawValue, "lowerPrice": r.lowerPrice, "upperPrice": r.upperPrice,
            "gridCount": "\(r.gridCount)", "investment": r.investment, "stopLossPrice": r.stopLossPrice, "takeProfitPrice": r.takeProfitPrice], auth: true)
    }
    static func create(_ r: APIGridCreateRequest) async throws -> APIGridBot { try await api.post("/strategy/grid", body: r) }
    static func stop(_ id: String, sellBase: Bool) async throws -> APIGridBot {
        try await api.post("/strategy/grid/\(id)/stop", body: APIStrategyStopGridRequest(sellBase: sellBase))
    }
}

// MARK: - 事件合约（feat/event-contracts）

struct EventPeriod: Decodable, Hashable { let period: String; let seconds: Int }
struct EventProductItem: Decodable, Hashable, Identifiable {
    var id: String { symbol }
    let symbol: String
    let baseAsset: String?
    let quoteAsset: String?
    let paused: Bool?
    let indexPrice: String?
    let priceAvailable: Bool?
}
struct EventProducts: Decodable, Hashable {
    let enabled: Bool?
    let stakeAsset: String?
    let payoutRatio: String?
    let minStake: String?
    let maxStake: String?
    let dailyLimit: String?
    let tieRule: String?
    let periods: [EventPeriod]?
    let products: [EventProductItem]?
}
struct EventOrder: Decodable, Hashable, Identifiable {
    let id: String
    let clientId: String?
    let symbol: String
    let period: String
    let direction: String
    let stake: String
    let payoutRatio: String?
    let openPrice: String?
    let openTime: Int64?
    let expiryTime: Int64?
    let status: String
    let result: String?
    let settlePrice: String?
    let payout: String?
    let pnl: String?
    let settledAt: Int64?
    let expectedPayout: String?
    let rejectReason: String?
    let createdAt: Int64?
    var isUp: Bool { direction == "up" }
    var isActive: Bool { ["pending", "open", "settling"].contains(status) }
}
struct EventOrderPage: Decodable { let orders: [EventOrder]?; let nextBefore: String?; let dailyUsed: String? }

enum EventAPI {
    struct Place: Encodable { let symbol: String; let period: String; let direction: String; let stake: String; let clientId: String }
    static func products() async throws -> EventProducts { try await APIClient.shared.get("/event/products", auth: TokenStore.shared.isLoggedIn) }
    static func place(_ p: Place) async throws -> EventOrder { try await APIClient.shared.post("/event/orders", body: p) }
    static func orders(status: String?, before: String?, limit: Int = 20) async throws -> EventOrderPage {
        try await APIClient.shared.get("/event/orders", query: ["status": status, "before": before, "limit": "\(limit)"], auth: true)
    }
    static func order(_ id: String) async throws -> EventOrder { try await APIClient.shared.get("/event/orders/\(id)", auth: true) }
}

// MARK: - 子账户（feat/sub-accounts）

struct SubAccount: Decodable, Hashable, Identifiable {
    var id: String { uid }
    let uid: String
    let username: String?
    let loginEmail: String?
    let nickname: String?
    let note: String?
    let status: String?
    let loginEnabled: Bool?
    let totpBound: Bool?
    var frozen: Bool { status == "frozen" }
    var displayName: String { (nickname?.isEmpty == false ? nickname : username) ?? uid }
}
struct SubBalance: Decodable, Hashable { let asset: String; let free: String?; let locked: String?; let total: String? }
struct SubAssets: Decodable, Hashable { let uid: String?; let balances: [SubBalance]? }
struct SubAssetsAll: Decodable { let items: [SubAssets]?; let totals: [SubBalance]? }

enum SubAPI {
    struct Page: Decodable { let items: [SubAccount]?; let total: Int? }
    struct Create: Encodable { let username: String; let nickname: String?; let note: String? }
    struct Transfer: Encodable { let clientId: String; let subAccountId: String; let direction: String; let asset: String; let amount: String }
    struct TransferResp: Decodable { let transferId: String?; let status: String?; let errorCode: String? }
    struct Freeze: Encodable { let reason: String? }
    static func list() async throws -> [SubAccount] {
        let r: Page = try await APIClient.shared.get("/sub-accounts", query: ["page": "1", "pageSize": "100"], auth: true)
        return r.items ?? []
    }
    static func assets() async throws -> SubAssetsAll { try await APIClient.shared.get("/sub-accounts/assets", auth: true) }
    static func create(_ c: Create) async throws -> SubAccount { try await APIClient.shared.post("/sub-accounts", body: c) }
    static func transfer(_ t: Transfer) async throws -> TransferResp { try await APIClient.shared.post("/sub-accounts/transfer", body: t) }
    static func freeze(_ id: String, _ on: Bool) async throws -> SubAccount {
        on ? try await APIClient.shared.post("/sub-accounts/\(id)/freeze", body: Freeze(reason: nil))
           : try await APIClient.shared.post("/sub-accounts/\(id)/unfreeze", body: Freeze(reason: nil))
    }
}

// MARK: - 聊天室（feat/chat）

/// type: general / spot / futures；name 为服务端给出的展示名
struct ChatRoom: Decodable, Hashable, Identifiable { let id: String; let type: String?; let symbol: String?; let name: String?; let online: Int? }
struct ChatRules: Decodable, Hashable { let maxLength: Int?; let minIntervalMs: Int?; let minKycLevel: Int?; let blockLinks: Bool?; let blockContacts: Bool? }
struct ChatMessage: Decodable, Hashable, Identifiable {
    let id: Int64
    let room: String?
    let sender: String?
    let text: String
    /// 仅历史与发言接口返回；WS 推送不带（自己的消息靠发言接口返回的 id 识别）
    let mine: Bool?
    let createdAt: Date?
}

enum ChatAPI {
    struct Rooms: Decodable { let rooms: [ChatRoom]?; let rules: ChatRules? }
    struct Messages: Decodable { let items: [ChatMessage]?; let hasMore: Bool? }
    struct Send: Encodable { let clientId: String; let text: String }
    struct Report: Encodable { let reason: String? }
    struct OK: Decodable { let ok: Bool? }
    static func rooms() async throws -> Rooms { try await APIClient.shared.get("/chat/rooms", auth: TokenStore.shared.isLoggedIn) }
    static func messages(_ room: String, before: Int64?) async throws -> Messages {
        try await APIClient.shared.get("/chat/rooms/\(room)/messages", query: ["before": before.map { "\($0)" }, "limit": "50"], auth: TokenStore.shared.isLoggedIn)
    }
    static func send(_ room: String, text: String) async throws -> ChatMessage {
        try await APIClient.shared.post("/chat/rooms/\(room)/messages", body: Send(clientId: "ios-\(UUID().uuidString.prefix(12))", text: text))
    }
    static func report(_ id: Int64, reason: String) async throws {
        let _: OK = try await APIClient.shared.post("/chat/messages/\(id)/report", body: Report(reason: reason))
    }
}

extension GridAPI {
    static func square() async throws -> [APIGridSquareItem] {
        let r: APIStrategySquare200Response = try await APIClient.shared.get("/strategy/grid/square", query: ["page": "1", "pageSize": "30"])
        return r.items ?? []
    }
}

extension AssetsAPI {
    /// 现货可用余额（无记录视为 0）
    static func spotBalance(_ asset: String) async throws -> Decimal {
        let all = try await balances()
        return Dec.parse(all.first { $0.asset == asset }?.free)
    }
}

/// 闪兑（PR #10，docs/api/convert.md）
enum ConvertAPI {
    struct Asset: Decodable, Hashable { let asset: String; let precision: Int?; let enabled: Bool? }
    struct Pair: Decodable, Hashable {
        let from: String, to: String
        let fromPrecision: Int?, toPrecision: Int?, spreadBps: Int?, route: String?, paused: Bool?, pauseReason: String?
    }
    struct Pairs: Decodable {
        let assets: [Asset]?, pairs: [Pair]?
        let minUsdt: String?, maxUsdt: String?, spreadBps: Int?, fee: String?, quoteTtlSeconds: Int?
    }
    struct QuoteReq: Encodable { let from: String; let to: String; let fromAmount: String }
    struct Quote: Decodable, Hashable {
        let quoteId: String, from: String?, to: String?, fromAmount: String?, toAmount: String?
        let rate: String?, inverseRate: String?, spreadBps: Int?, fee: String?, feeAsset: String?
        let expiresAt: Double?, ttlSeconds: Int?
    }
    struct Order: Decodable, Hashable, Identifiable {
        let id: String, from: String?, to: String?, fromAmount: String?, toAmount: String?, rate: String?, status: String?, createdAt: Double?
        enum K: String, CodingKey { case id, from, to, fromAmount, toAmount, rate, status, createdAt }
        init(from d: Decoder) throws {
            let c = try d.container(keyedBy: K.self)
            if let i = try? c.decode(Int.self, forKey: .id) { id = String(i) } else { id = (try? c.decode(String.self, forKey: .id)) ?? UUID().uuidString }
            from = try? c.decode(String.self, forKey: .from); to = try? c.decode(String.self, forKey: .to)
            fromAmount = try? c.decode(String.self, forKey: .fromAmount); toAmount = try? c.decode(String.self, forKey: .toAmount)
            rate = try? c.decode(String.self, forKey: .rate); status = try? c.decode(String.self, forKey: .status)
            createdAt = try? c.decode(Double.self, forKey: .createdAt)
        }
    }
    struct Accept: Decodable { let order: Order?; let replay: Bool? }
    struct AcceptReq: Encodable { let quoteId: String }
    struct Orders: Decodable { let items: [Order]?; let total: Int? }

    static func pairs() async throws -> Pairs { try await APIClient.shared.get("/convert/pairs") }
    static func quote(from: String, to: String, amount: String) async throws -> Quote {
        try await APIClient.shared.post("/convert/quote", body: QuoteReq(from: from, to: to, fromAmount: amount))
    }
    static func accept(_ id: String) async throws -> Accept { try await APIClient.shared.post("/convert/accept", body: AcceptReq(quoteId: id)) }
    static func orders(page: Int = 1) async throws -> [Order] {
        let r: Orders = try await APIClient.shared.get("/convert/orders", query: ["page": String(page), "pageSize": "20"], auth: true)
        return r.items ?? []
    }
}
