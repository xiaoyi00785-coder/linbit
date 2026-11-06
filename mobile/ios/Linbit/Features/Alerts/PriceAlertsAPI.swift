import Foundation
import Combine

/// 价格预警客户端（P0，契约对齐 origin/main OpenAPI `#108`/`#109`：`/price-alerts`）。
///
/// - market=`spot` · priceSource=`INDEX` · triggerMode=`once` · channels=`[in_app]` · revision 乐观锁
/// - 功能开关 key=`priceAlerts`（`AppFeatures`）；**不能**把 flag=false 当唯一安全闸——写路径仍可能在生产开启
/// - DEBUG `PriceAlertDemo` / 任意 `lb.shot`：写路径走本地 mock 或 SHOT_BLOCKED，**不**打远端
/// - Release：`ShotGate.enabled=false` → 写走正式 `PriceAlertsAPI`，夹具不误封
/// - 会话：`PriceAlertsSession` 用 reloadSeq 丢弃错会话/过期列表响应，避免旧页落地
/// - 契约上限：`limit.activePerUser` OpenAPI enum=[50]；客户端不另发明额度

// MARK: - OpenAPI-shaped models（手写，与 Generated 风格一致；未跑 openapi-generator）

struct PriceAlert: Codable, Hashable, Identifiable {
    var id: String { alertId }
    var alertId: String
    var clientAlertId: String
    var market: String           // spot
    var symbol: String
    var condition: String        // gte | lte
    var threshold: String
    var priceSource: String      // INDEX
    var triggerMode: String      // once
    var channels: [String]       // in_app
    var enabled: Bool
    var status: String           // active | triggered | disabled
    var revision: Int64
    var limit: Limit?
    var lastObservedPrice: String?
    var lastEvaluatedAt: Date?
    var triggeredPrice: String?
    var triggeredAt: Date?
    var notificationId: String?
    var deliveryStatus: String?  // none | pending | delivered_in_app | failed | unsupported
    var cooldownUntil: Date?
    var note: String?
    var createdAt: Date?
    var updatedAt: Date?

    struct Limit: Codable, Hashable {
        var activePerUser: Int?
    }
}

struct PriceAlertsPage: Codable, Hashable {
    var items: [PriceAlert]
    var page: Int?
    var pageSize: Int?
    var total: Int?
}

struct CreatePriceAlertRequest: Encodable {
    var clientAlertId: String
    var market: String = "spot"
    var symbol: String
    var condition: String
    var threshold: String
    var priceSource: String = "INDEX"
    var triggerMode: String = "once"
    var channels: [String] = ["in_app"]
    var enabled: Bool = true
    var note: String = ""
}

struct UpdatePriceAlertRequest: Encodable {
    var revision: Int64
    var threshold: String? = nil
    var condition: String? = nil
    var enabled: Bool? = nil
    var channels: [String]? = nil
    var note: String? = nil
}

/// 429 体：`retryAfterSec` 与 `Retry-After` 头一致（契约 PriceAlertRateLimitError）
struct PriceAlertRateLimitError: Decodable {
    var code: String
    var message: String
    var requestId: String?
    var retryAfterSec: Int
}

enum PriceAlertStatusFilter: String {
    case active, triggered, disabled
}

// MARK: - Feature gate

enum PriceAlertsFeature {
    /// `/app/features` key，与 services/go/market-data/features.go + paper README 一致
    static let flagKey = "priceAlerts"

    /// OpenAPI `PriceAlert.limit.activePerUser` enum=[50]；展示用，不另发明额度
    static let activePerUserCap = 50

    /// 仅表示客户端已知开关；生产可能已开。真正禁用以接口 403 FEATURE_DISABLED 为准。
    @MainActor
    static var isFlagOn: Bool { AppFeatures.shared.isOn(flagKey) }

    static func isDisabledFailure(_ error: Error) -> Bool {
        (error as? APIFailure)?.code == "FEATURE_DISABLED"
    }

    /// 409 幂等/revision 冲突（OpenAPI PaperConflict）
    static func isConflictFailure(_ error: Error) -> Bool {
        (error as? APIFailure)?.status == 409
    }

    static func isNotFoundFailure(_ error: Error) -> Bool {
        let c = (error as? APIFailure)?.code
        return c == "ALERT_NOT_FOUND" || (error as? APIFailure)?.status == 404
    }
}

// MARK: - REST（真实 URLSession 路径；测试勿调用写接口）

enum PriceAlertsAPI {
    static var api: APIClient { .shared }

    /// GET /price-alerts
    static func list(symbol: String? = nil, status: PriceAlertStatusFilter? = nil, page: Int = 1, pageSize: Int = 20) async throws -> PriceAlertsPage {
        try await api.get(
            "/price-alerts",
            query: [
                "symbol": symbol,
                "status": status?.rawValue,
                "page": "\(page)",
                "pageSize": "\(pageSize)",
            ],
            auth: true
        )
    }

    /// POST /price-alerts → 201
    static func create(_ body: CreatePriceAlertRequest) async throws -> PriceAlert {
        try await api.post("/price-alerts", body: body, auth: true)
    }

    /// PATCH /price-alerts/{alertId}
    static func update(alertId: String, _ body: UpdatePriceAlertRequest) async throws -> PriceAlert {
        try await api.patch("/price-alerts/\(alertId.urlPath)", body: body)
    }

    /// DELETE /price-alerts/{alertId}?revision= — 204；重复删除幂等
    static func delete(alertId: String, revision: Int64?) async throws {
        var q: [String: String?] = [:]
        if let revision { q["revision"] = "\(revision)" }
        let _: EmptyResponse = try await api.delete("/price-alerts/\(alertId.urlPath)", query: q)
    }

    /// 从 APIFailure 取建议等待秒：优先响应头 Retry-After，其次体 retryAfterSec（若调用方另解）
    static func retryAfterSeconds(from error: Error, body: PriceAlertRateLimitError? = nil) -> Int? {
        if let f = error as? APIFailure, let ra = f.retryAfter, ra > 0 { return ra }
        if let body, body.retryAfterSec > 0 { return body.retryAfterSec }
        return nil
    }
}

// MARK: - Session / stale discard（错会话旧响应不落地）

@MainActor
final class PriceAlertsSession: ObservableObject {
    @Published private(set) var page: Loadable<PriceAlertsPage> = .idle
    @Published private(set) var featureDisabled = false
    @Published private(set) var rateLimitUntil: Date?
    /// 递增以丢弃过期 list 响应（切账号 / 快速重载）
    private var reloadSeq: UInt64 = 0

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
        page = .idle
        featureDisabled = false
    }

    private var listSymbol: String?
    private var listStatus: PriceAlertStatusFilter?
    private(set) var currentPage = 1
    @Published private(set) var loadingMore = false

    var canLoadMore: Bool {
        guard case let .loaded(p) = page, let total = p.total else { return false }
        return p.items.count < total
    }

    func reload(symbol: String? = nil, status: PriceAlertStatusFilter? = nil, page pageNum: Int = 1) async {
        listSymbol = symbol
        listStatus = status
        currentPage = pageNum
        // DEBUG 夹具：只填列表视觉，不打网、不写生产
        if PriceAlertDemo.on {
            featureDisabled = PriceAlertDemo.featureDisabled
            if PriceAlertDemo.featureDisabled {
                page = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                return
            }
            if PriceAlertDemo.forceError {
                page = .failed("网络连接失败，请检查网络")
                return
            }
            if let demo = PriceAlertDemo.page {
                page = .loaded(demo)
            }
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        if pageNum <= 1 { page = .loading }
        do {
            let r = try await PriceAlertsAPI.list(symbol: symbol, status: status, page: pageNum)
            guard seq == reloadSeq else { return }
            featureDisabled = false
            page = .loaded(r)
            currentPage = pageNum
        } catch {
            guard seq == reloadSeq else { return }
            if PriceAlertsFeature.isDisabledFailure(error) {
                featureDisabled = true
                page = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                return
            }
            if let f = error as? APIFailure, f.status == 429 {
                applyRateLimit(from: error)
                page = .failed(error.lbMessage)
                return
            }
            page = .failed(error.lbMessage)
        }
    }

    func loadMore() async {
        guard canLoadMore, !loadingMore, !PriceAlertDemo.on else { return }
        loadingMore = true
        defer { loadingMore = false }
        let next = currentPage + 1
        reloadSeq &+= 1
        let seq = reloadSeq
        do {
            let r = try await PriceAlertsAPI.list(symbol: listSymbol, status: listStatus, page: next)
            guard seq == reloadSeq else { return }
            if case let .loaded(cur) = page {
                var merged = cur
                merged.items.append(contentsOf: r.items)
                merged.page = r.page ?? next
                merged.total = r.total ?? cur.total
                page = .loaded(merged)
                currentPage = next
            } else {
                page = .loaded(r)
                currentPage = next
            }
        } catch {
            guard seq == reloadSeq else { return }
            if let f = error as? APIFailure, f.status == 429 { applyRateLimit(from: error) }
            ToastCenter.shared.show(error.lbMessage, .error)
        }
    }

    /// 写入口：Release / 无 shot → 正式 API；DEBUG+lb.shot → 本地 mock 或 SHOT_BLOCKED（永不远端写）。
    func createForTestOrUI(_ body: CreatePriceAlertRequest) async throws -> PriceAlert {
        if let mocked = try PriceAlertDemo.interceptCreate(body) { return mocked }
        if isRateLimited {
            throw APIFailure.server(status: 429, error: APIError(code: "RATE_LIMITED", message: "价格预警写操作过于频繁，请稍后再试"), retryAfter: rateLimitRemainingSec)
        }
        do {
            return try await PriceAlertsAPI.create(body)
        } catch {
            applyRateLimit(from: error)
            throw error
        }
    }

    func updateForTestOrUI(alertId: String, _ body: UpdatePriceAlertRequest) async throws -> PriceAlert {
        if let mocked = try PriceAlertDemo.interceptUpdate(alertId: alertId, body) { return mocked }
        if isRateLimited {
            throw APIFailure.server(status: 429, error: APIError(code: "RATE_LIMITED", message: "价格预警写操作过于频繁，请稍后再试"), retryAfter: rateLimitRemainingSec)
        }
        do {
            return try await PriceAlertsAPI.update(alertId: alertId, body)
        } catch {
            applyRateLimit(from: error)
            throw error
        }
    }

    func deleteForTestOrUI(alertId: String, revision: Int64?) async throws {
        if try PriceAlertDemo.interceptDelete(alertId: alertId, revision: revision) { return }
        if isRateLimited {
            throw APIFailure.server(status: 429, error: APIError(code: "RATE_LIMITED", message: "价格预警写操作过于频繁，请稍后再试"), retryAfter: rateLimitRemainingSec)
        }
        do {
            try await PriceAlertsAPI.delete(alertId: alertId, revision: revision)
        } catch {
            applyRateLimit(from: error)
            throw error
        }
    }

    private func applyRateLimit(from error: Error) {
        if let sec = PriceAlertsAPI.retryAfterSeconds(from: error), sec > 0 {
            rateLimitUntil = Date().addingTimeInterval(TimeInterval(sec))
        }
    }
}

// MARK: - DEBUG example fixture（Release / ShotGate 关闭时恒空操作）

enum PriceAlertDemo {
    /// `lb.shot`：price-alerts | -empty | -error | -disabled | -create | -edit | -delete
    static var on: Bool {
        guard ShotGate.enabled, let s = ShotGate.launchShot else { return false }
        return s == "price-alerts" || s.hasPrefix("price-alerts-") || s.hasPrefix("price-alerts:")
    }

    /// Release 恒 false（ShotGate.enabled=false）。DEBUG 且任意 lb.shot 时拦截远端写，避免截图误写生产。
    static var blocksRemoteWrites: Bool {
        guard ShotGate.enabled else { return false }
        return ShotGate.launchShot != nil
    }

    /// 兼容旧名：等同 blocksRemoteWrites
    static var blocksWrites: Bool { blocksRemoteWrites }

    static var featureDisabled: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("disabled") || s.hasSuffix(":disabled")
    }

    static var forceError: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("error") || s.hasSuffix(":error")
    }

    static var openCreate: Bool { shotHas("create") }
    static var openEdit: Bool { shotHas("edit") }
    static var openDelete: Bool { shotHas("delete") }

    private static func shotHas(_ token: String) -> Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s == "price-alerts-\(token)" || s.hasSuffix(":\(token)") || s.contains("-\(token)")
    }

    static var page: PriceAlertsPage? {
        guard on else { return nil }
        if featureDisabled || forceError { return nil }
        if let s = ShotGate.launchShot, s.contains("empty") {
            return PriceAlertsPage(items: [], page: 1, pageSize: 20, total: 0)
        }
        return PriceAlertsPage(items: [example(active: true), example(active: false)], page: 1, pageSize: 20, total: 2)
    }

    /// 测试可观察的本地写记录（仅 DEBUG 有意义；Release 编译后 ShotGate 关闭，拦截不触发）
    static var mockWriteLog: [(op: String, id: String)] = []

    static func resetMockLog() { mockWriteLog.removeAll() }

    /// - Returns: mock 结果；若应拦截但非 price-alerts 夹具则抛 SHOT_BLOCKED；若不拦截返回 nil 走正式 API
    static func interceptCreate(_ body: CreatePriceAlertRequest) throws -> PriceAlert? {
        guard blocksRemoteWrites else { return nil }
        guard on else {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图夹具不创建生产预警"), retryAfter: nil)
        }
        var a = example(active: body.enabled)
        a.alertId = "al_mock_\(mockWriteLog.count + 1)"
        a.clientAlertId = body.clientAlertId
        a.symbol = body.symbol
        a.condition = body.condition
        a.threshold = body.threshold
        a.note = body.note
        a.revision = 1
        mockWriteLog.append((op: "create", id: a.alertId))
        return a
    }

    static func interceptUpdate(alertId: String, _ body: UpdatePriceAlertRequest) throws -> PriceAlert? {
        guard blocksRemoteWrites else { return nil }
        guard on else {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图夹具不修改生产预警"), retryAfter: nil)
        }
        var a = example(active: body.enabled ?? true)
        a.alertId = alertId
        if let th = body.threshold { a.threshold = th }
        if let c = body.condition { a.condition = c }
        if let n = body.note { a.note = n }
        if let e = body.enabled { a.enabled = e; a.status = e ? "active" : "disabled" }
        a.revision = body.revision + 1
        mockWriteLog.append((op: "update", id: alertId))
        return a
    }

    /// - Returns: true 已本地处理；false 应走正式 API
    static func interceptDelete(alertId: String, revision: Int64?) throws -> Bool {
        guard blocksRemoteWrites else { return false }
        guard on else {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图夹具不删除生产预警"), retryAfter: nil)
        }
        mockWriteLog.append((op: "delete", id: alertId))
        return true
    }

    static func example(active: Bool) -> PriceAlert {
        PriceAlert(
            alertId: active ? "al_demo_1" : "al_demo_2",
            clientAlertId: active ? "demo.client.1" : "demo.client.2",
            market: "spot",
            symbol: "BTCUSDT",
            condition: active ? "gte" : "lte",
            threshold: active ? "80000.00" : "75000.00",
            priceSource: "INDEX",
            triggerMode: "once",
            channels: ["in_app"],
            enabled: active,
            status: active ? "active" : "triggered",
            revision: 1,
            limit: .init(activePerUser: PriceAlertsFeature.activePerUserCap),
            lastObservedPrice: "79950.12",
            lastEvaluatedAt: nil,
            triggeredPrice: active ? nil : "74990.00",
            triggeredAt: nil,
            notificationId: nil,
            deliveryStatus: active ? "none" : "delivered_in_app",
            cooldownUntil: nil,
            note: "DEBUG example — not live",
            createdAt: nil,
            updatedAt: nil
        )
    }
}
