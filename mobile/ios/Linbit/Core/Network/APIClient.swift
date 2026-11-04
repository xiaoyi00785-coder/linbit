import Foundation

enum APIFailure: Error, LocalizedError {
    /// retryAfter：响应头 Retry-After（秒），429 RATE_LIMITED 时用于倒计时
    case server(status: Int, error: APIError?, retryAfter: Int? = nil)
    case decoding(Error)
    case transport(Error)
    var errorDescription: String? {
        switch self {
        case let .server(s, e, _): return e?.message ?? "HTTP \(s)"
        case let .decoding(e): return "数据解析失败：\(e)"
        case let .transport(e): return e.localizedDescription
        }
    }
    var code: String? { if case let .server(_, e, _) = self { return e?.code }; return nil }
    var status: Int? { if case let .server(s, _, _) = self { return s }; return nil }
    var retryAfter: Int? { if case let .server(_, _, r) = self { return r }; return nil }
}

/// 令牌存储（Keychain）。
final class TokenStore {
    static let shared = TokenStore()
    var accessToken: String? { get { Keychain.get("lb.access") } set { Keychain.set(newValue, for: "lb.access") } }
    var refreshToken: String? { get { Keychain.get("lb.refresh") } set { Keychain.set(newValue, for: "lb.refresh") } }
    var isLoggedIn: Bool { accessToken != nil }
    /// 仅当 refresh token 刷新失败时置为 true（区分“未登录”与“登录过期”）
    var sessionExpired = false
    /// 退出登录进行中 / 刚退出：旧访问令牌返回 401 属预期，不提示「登录已过期」
    var loggingOut = false
    func save(_ t: APIAuthTokens) { accessToken = t.accessToken; refreshToken = t.refreshToken; sessionExpired = false }
    func clear() { accessToken = nil; refreshToken = nil }
}

/// REST 客户端：Bearer JWT、X-Device-Id、401 自动用 refreshToken 刷新一次后重试。
actor APIClient {
    static let shared = APIClient()

    private let session: URLSession
    private let base: URL
    private let tokens = TokenStore.shared
    private var refreshTask: Task<Bool, Never>?

    init(base: URL = AppConfig.apiBase, session: URLSession = .shared) {
        self.base = base
        self.session = session
    }

    func get<T: Decodable>(_ path: String, query: [String: String?] = [:], auth: Bool = false) async throws -> T {
        try await send(path, method: "GET", query: query, body: Optional<Int>.none, auth: auth)
    }

    func delete<T: Decodable>(_ path: String, query: [String: String?] = [:]) async throws -> T {
        try await send(path, method: "DELETE", query: query, body: Optional<Int>.none, auth: true)
    }

    func post<T: Decodable, B: Encodable>(_ path: String, body: B?, auth: Bool = true) async throws -> T {
        try await send(path, method: "POST", query: [:], body: body, auth: auth)
    }

    /// 不在 401 时自动刷新令牌（用于退出登录：访问令牌过期时服务端凭请求体 refreshToken 识别用户）
    func postNoRefresh<T: Decodable, B: Encodable>(_ path: String, body: B?) async throws -> T {
        try await send(path, method: "POST", query: [:], body: body, auth: true, retried: true)
    }

    func put<T: Decodable, B: Encodable>(_ path: String, body: B?) async throws -> T {
        try await send(path, method: "PUT", query: [:], body: body, auth: true)
    }

    func patch<T: Decodable, B: Encodable>(_ path: String, body: B?) async throws -> T {
        try await send(path, method: "PATCH", query: [:], body: body, auth: true)
    }

    func deleteAuth(_ path: String) async throws {
        let _: EmptyResponse = try await send(path, method: "DELETE", query: [:], body: Optional<Int>.none, auth: true)
    }

    private func makeRequest<B: Encodable>(_ path: String, _ method: String, _ query: [String: String?], _ body: B?, _ auth: Bool) throws -> URLRequest {
        var comps = URLComponents(url: base.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        let items = query.compactMap { k, v in v.map { URLQueryItem(name: k, value: $0) } }
        if !items.isEmpty { comps.queryItems = items }
        var req = URLRequest(url: comps.url!, timeoutInterval: 15)
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        req.setValue(AppConfig.userAgent, forHTTPHeaderField: "User-Agent")
        req.setValue(Keychain.deviceId, forHTTPHeaderField: "X-Device-Id")
        req.setValue(Locale.preferredLanguages.first ?? "zh-CN", forHTTPHeaderField: "Accept-Language")
        if auth, let t = tokens.accessToken { req.setValue("Bearer \(t)", forHTTPHeaderField: "Authorization") }
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try CodableHelper.jsonEncoder.encode(body)
        }
        return req
    }

    private func send<T: Decodable, B: Encodable>(_ path: String, method: String, query: [String: String?], body: B?, auth: Bool, retried: Bool = false) async throws -> T {
        let req = try makeRequest(path, method, query, body, auth)
        let data: Data, resp: URLResponse
        do { (data, resp) = try await session.data(for: req) } catch { throw APIFailure.transport(error) }
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if status == 401, auth, !retried, await refresh() {
            return try await send(path, method: method, query: query, body: body, auth: auth, retried: true)
        }
        guard (200..<300).contains(status) else {
            let ra = ((resp as? HTTPURLResponse)?.value(forHTTPHeaderField: "Retry-After")).flatMap { Int($0.trimmingCharacters(in: .whitespaces)) }
            throw APIFailure.server(status: status, error: try? CodableHelper.jsonDecoder.decode(APIError.self, from: data), retryAfter: ra)
        }
        if T.self == EmptyResponse.self { return EmptyResponse() as! T }
        do { return try CodableHelper.jsonDecoder.decode(T.self, from: data) } catch { throw APIFailure.decoding(error) }
    }

    /// multipart/form-data 上传（单文件），带 401 刷新重试
    func uploadMultipart<T: Decodable>(_ path: String, field: String, filename: String, mime: String, data file: Data, retried: Bool = false) async throws -> T {
        var req = try makeRequest(path, "POST", [:], Optional<EmptyResponse>.none, true)
        req.timeoutInterval = 60
        let boundary = "lb-\(UUID().uuidString)"
        req.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        var body = Data()
        body.append("--\(boundary)\r\nContent-Disposition: form-data; name=\"\(field)\"; filename=\"\(filename)\"\r\nContent-Type: \(mime)\r\n\r\n".data(using: .utf8)!)
        body.append(file)
        body.append("\r\n--\(boundary)--\r\n".data(using: .utf8)!)
        let data: Data, resp: URLResponse
        do { (data, resp) = try await session.upload(for: req, from: body) } catch { throw APIFailure.transport(error) }
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if status == 401, !retried, await refresh() {
            return try await uploadMultipart(path, field: field, filename: filename, mime: mime, data: file, retried: true)
        }
        guard (200..<300).contains(status) else {
            throw APIFailure.server(status: status, error: try? CodableHelper.jsonDecoder.decode(APIError.self, from: data))
        }
        do { return try CodableHelper.jsonDecoder.decode(T.self, from: data) } catch { throw APIFailure.decoding(error) }
    }

    /// 并发 401 只触发一次刷新。
    private func refresh() async -> Bool {
        if let t = refreshTask { return await t.value }
        let task = Task<Bool, Never> { [tokens, base, session] in
            guard let rt = tokens.refreshToken else { return false }
            var req = URLRequest(url: base.appendingPathComponent("/auth/refresh"))
            req.httpMethod = "POST"
            req.setValue(AppConfig.userAgent, forHTTPHeaderField: "User-Agent")
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try? JSONEncoder().encode(APIRefreshTokenRequest(refreshToken: rt))
            guard let (data, resp) = try? await session.data(for: req),
                  (resp as? HTTPURLResponse)?.statusCode == 200,
                  let t = try? CodableHelper.jsonDecoder.decode(APIAuthTokens.self, from: data) else {
                tokens.clear()
                if tokens.loggingOut { return false }
                tokens.sessionExpired = true
                NotificationCenter.default.post(name: .lbSessionExpired, object: nil)
                return false
            }
            tokens.save(t)
            return true
        }
        refreshTask = task
        let ok = await task.value
        refreshTask = nil
        return ok
    }
}

struct EmptyResponse: Decodable {}

extension Notification.Name {
    static let lbSessionExpired = Notification.Name("lb.sessionExpired")
    static let lbTokenRefreshed = Notification.Name("lb.tokenRefreshed")
}
