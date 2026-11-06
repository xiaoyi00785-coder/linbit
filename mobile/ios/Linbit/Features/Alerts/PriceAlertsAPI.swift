import Foundation
import Combine

/// 价格预警客户端（P0，契约对齐 origin/main OpenAPI `#108`/`#109`：`/price-alerts`）。
///
/// - market=`spot` · priceSource=`INDEX` · triggerMode=`once` · channels=`[in_app]` · revision 乐观锁
/// - 功能开关 key=`priceAlerts`（`AppFeatures`）；**不能**把 flag=false 当唯一安全闸——写路径仍可能在生产开启
/// - 本文件不含完整 UI；DEBUG `PriceAlertDemo` 仅本地夹具，**永不**打真实创建/改/删
/// - 会话：`PriceAlertsSession` 用 reloadSeq 丢弃错会话/过期列表响应，避免旧页落地

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

    /// 仅表示客户端已知开关；生产可能已开。真正禁用以接口 403 FEATURE_DISABLED 为准。
    @MainActor
    static var isFlagOn: Bool { AppFeatures.shared.isOn(flagKey) }

    static func isDisabledFailure(_ error: Error) -> Bool {
        (error as? APIFailure)?.code == "FEATURE_DISABLED"
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

    func reload(symbol: String? = nil, status: PriceAlertStatusFilter? = nil, page pageNum: Int = 1) async {
        // DEBUG 夹具：只填列表视觉，不打网
        if let demo = PriceAlertDemo.page {
            self.page = .loaded(demo)
            featureDisabled = PriceAlertDemo.featureDisabled
            return
        }
        reloadSeq &+= 1
        let seq = reloadSeq
        if case .idle = page { page = .loading }
        do {
            let r = try await PriceAlertsAPI.list(symbol: symbol, status: status, page: pageNum)
            guard seq == reloadSeq else { return }
            featureDisabled = false
            page = .loaded(r)
        } catch {
            guard seq == reloadSeq else { return }
            if PriceAlertsFeature.isDisabledFailure(error) {
                featureDisabled = true
                page = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放")
                return
            }
            page = .failed(error.lbMessage)
        }
    }

    /// 写操作入口：DEBUG 夹具直接拒绝网络；生产路径调用方负责登录态。测试只走 mock。
    func createForTestOrUI(_ body: CreatePriceAlertRequest) async throws -> PriceAlert {
        if PriceAlertDemo.blocksWrites {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图/测试夹具不创建生产预警"), retryAfter: nil)
        }
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
        if PriceAlertDemo.blocksWrites {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图/测试夹具不修改生产预警"), retryAfter: nil)
        }
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
        if PriceAlertDemo.blocksWrites {
            throw APIFailure.server(status: 403, error: APIError(code: "SHOT_BLOCKED", message: "截图/测试夹具不删除生产预警"), retryAfter: nil)
        }
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
    /// `lb.shot` 前缀 `price-alerts` / `price-alerts-empty` / `price-alerts-disabled`
    static var on: Bool {
        guard ShotGate.enabled, let s = ShotGate.launchShot else { return false }
        return s == "price-alerts" || s.hasPrefix("price-alerts-") || s.hasPrefix("price-alerts:")
    }

    static var blocksWrites: Bool { on || ShotGate.enabled && ShotGate.launchShot != nil }

    static var featureDisabled: Bool {
        guard let s = ShotGate.launchShot else { return false }
        return s.contains("disabled") || s.hasSuffix(":disabled")
    }

    static var page: PriceAlertsPage? {
        guard on else { return nil }
        if featureDisabled { return nil }
        if let s = ShotGate.launchShot, s.contains("empty") {
            return PriceAlertsPage(items: [], page: 1, pageSize: 20, total: 0)
        }
        return PriceAlertsPage(items: [example(active: true), example(active: false)], page: 1, pageSize: 20, total: 2)
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
            limit: .init(activePerUser: 50),
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
