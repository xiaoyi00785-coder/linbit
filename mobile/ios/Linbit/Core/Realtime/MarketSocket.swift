import Foundation

/// WS 推送帧（docs/api/websocket.md）。data 保留原始 JSON，由订阅方按频道解码。
struct WSFrame {
    let channel: String
    let ts: Int64?
    let data: Data
}

struct WSTicker: Decodable {
    let symbol: String
    let lastPrice: String
    let priceChangePercent: String
    let high: String?
    let low: String?
    let volume: String?
    let quoteVolume: String?
}

/// 成交或 ticker 帧。成交用 price，ticker 用 lastPrice；涨跌幅可能缺省或是数字。
struct TapePrice: Decodable {
    var price: String
    var lastPrice: String?
    var priceChangePercent: String?
    static func decode(_ data: Data) -> TapePrice? { try? CodableHelper.jsonDecoder.decode(TapePrice.self, from: data) }
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        price = (try? c.decode(String.self, forKey: .price)) ?? "0"
        lastPrice = try? c.decode(String.self, forKey: .lastPrice)
        if let s = try? c.decode(String.self, forKey: .priceChangePercent) { priceChangePercent = s }
        else if let n = try? c.decode(Double.self, forKey: .priceChangePercent) { priceChangePercent = String(n) }
    }
    private enum CodingKeys: String, CodingKey { case price, lastPrice, priceChangePercent }
}

/// 行情 / 用户数据 WebSocket 客户端。
/// - 单连接多订阅；断线后指数退避重连并自动重订阅。
/// - 心跳：每 20s 发 {"op":"ping"}；服务端 {"event":"ping"} 回 {"op":"pong"}。
/// - 看门狗：60s 内未收到任何帧即判定断开，主动重连。
/// - 已登录时连上先发 auth，再订阅 orders/balances 等私有频道。
@MainActor
final class MarketSocket: ObservableObject {
    static let shared = MarketSocket()

    enum State: Equatable { case idle, connecting, open, reconnecting(attempt: Int) }
    @Published private(set) var state: State = .idle

    static let heartbeatInterval: TimeInterval = 20
    static let idleTimeout: TimeInterval = 60

    private let url: URL
    private let session: URLSession
    private var task: URLSessionWebSocketTask?
    private var heartbeat: Task<Void, Never>?
    private var watchdog: Task<Void, Never>?
    private var lastFrameAt = Date()
    private var attempt = 0
    private var wanted = false
    /// channel -> 订阅者回调
    private var handlers: [String: [UUID: (WSFrame) -> Void]] = [:]
    private var reqSeq = 0

    init(url: URL = AppConfig.wsURL, session: URLSession = .shared) {
        self.url = url
        self.session = session
        NotificationCenter.default.addObserver(forName: .lbTokenRefreshed, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.sendAuthIfNeeded() }
        }
    }

    // MARK: 公共 API

    func connect() {
        wanted = true
        guard task == nil else { return }
        open()
    }

    /// 进入后台时调用；回到前台再 connect()。
    func disconnect() {
        wanted = false
        teardown()
        state = .idle
    }

    /// 订阅频道，返回取消令牌。channel 例：ticker:BTCUSDT、depth:BTCUSDT、kline:BTCUSDT:15m、orders
    @discardableResult
    func subscribe(_ channel: String, handler: @escaping (WSFrame) -> Void) -> WSSubscription {
        let id = UUID()
        let isNew = handlers[channel] == nil
        handlers[channel, default: [:]][id] = handler
        if isNew, state == .open { send(op: "subscribe", args: [channel]) }
        connect()
        return WSSubscription { [weak self] in
            Task { @MainActor in self?.unsubscribe(channel, id) }
        }
    }

    // MARK: 内部

    private func unsubscribe(_ channel: String, _ id: UUID) {
        handlers[channel]?[id] = nil
        if handlers[channel]?.isEmpty == true {
            handlers[channel] = nil
            if state == .open { send(op: "unsubscribe", args: [channel]) }
        }
    }

    private func open() {
        state = attempt == 0 ? .connecting : .reconnecting(attempt: attempt)
        var wsReq = URLRequest(url: url)
        wsReq.setValue(AppConfig.userAgent, forHTTPHeaderField: "User-Agent")
        let t = session.webSocketTask(with: wsReq)
        task = t
        t.resume()
        lastFrameAt = Date()
        receive(on: t)
        // 发出首帧前即视为 open；首个回执/帧到达会刷新看门狗
        state = .open
        sendAuthIfNeeded()
        let chans = Array(handlers.keys)
        if !chans.isEmpty { send(op: "subscribe", args: chans) }
        startTimers()
    }

    private func startTimers() {
        heartbeat?.cancel(); watchdog?.cancel()
        heartbeat = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(Self.heartbeatInterval * 1_000_000_000))
                await self?.send(op: "ping", args: nil)
            }
        }
        watchdog = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 5_000_000_000)
                guard let self else { return }
                if await Date().timeIntervalSince(self.lastFrameAt) > Self.idleTimeout {
                    await self.scheduleReconnect()
                    return
                }
            }
        }
    }

    /// 收帧和 JSON 解析放在后台线程，主线程只做分发。行情帧一秒几十条，全在主线程解析会挤掉滑动和动画的帧。
    private func receive(on t: URLSessionWebSocketTask) {
        Task.detached(priority: .userInitiated) { [weak self] in
            while true {
                do {
                    let msg = try await t.receive()
                    let parsed: Parsed
                    switch msg {
                    case let .string(s): parsed = Self.parse(Data(s.utf8))
                    case let .data(d): parsed = Self.parse(d)
                    @unknown default: parsed = .ignored
                    }
                    guard let self else { return }
                    guard await self.deliver(parsed, from: t) else { return }
                } catch {
                    await self?.receiveFailed(t)
                    return
                }
            }
        }
    }

    private enum Parsed { case ping, ignored, frame(WSFrame) }

    private nonisolated static func parse(_ raw: Data) -> Parsed {
        guard let obj = try? JSONSerialization.jsonObject(with: raw) as? [String: Any] else { return .ignored }
        if let event = obj["event"] as? String {
            return event == "ping" ? .ping : .ignored // subscribed / authed / pong / error 回执
        }
        guard let channel = obj["channel"] as? String, let payload = obj["data"],
              let data = try? JSONSerialization.data(withJSONObject: payload) else { return .ignored }
        return .frame(WSFrame(channel: channel, ts: (obj["ts"] as? NSNumber)?.int64Value, data: data))
    }

    /// 返回 false 表示这条连接已被替换，接收循环应退出
    private func deliver(_ p: Parsed, from t: URLSessionWebSocketTask) -> Bool {
        guard task === t else { return false }
        lastFrameAt = Date()
        attempt = 0 // 收到真实帧才重置退避
        switch p {
        case .ping: send(op: "pong", args: nil)
        case .ignored: break
        case let .frame(frame): handlers[frame.channel]?.values.forEach { $0(frame) }
        }
        return true
    }

    private func receiveFailed(_ t: URLSessionWebSocketTask) {
        guard task === t else { return }
        scheduleReconnect()
    }

    private func sendAuthIfNeeded() {
        guard state == .open, let token = TokenStore.shared.accessToken else { return }
        send(op: "auth", args: [token])
    }

    private func send(op: String, args: [String]?) {
        guard let task else { return }
        reqSeq += 1
        var obj: [String: Any] = ["op": op, "id": "ios-\(reqSeq)"]
        if let args { obj["args"] = args }
        guard let data = try? JSONSerialization.data(withJSONObject: obj),
              let s = String(data: data, encoding: .utf8) else { return }
        Task { [weak self] in
            do { try await task.send(.string(s)) } catch { self?.scheduleReconnect() }
        }
    }

    private func teardown() {
        heartbeat?.cancel(); watchdog?.cancel()
        heartbeat = nil; watchdog = nil
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    private func scheduleReconnect() {
        guard wanted else { return }
        teardown()
        attempt += 1
        state = .reconnecting(attempt: attempt)
        let delay = min(pow(2.0, Double(attempt - 1)), 30) // 1,2,4…30s
        Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            await MainActor.run {
                guard let self, self.wanted, self.task == nil else { return }
                self.open()
            }
        }
    }
}

final class WSSubscription {
    private let cancelAction: () -> Void
    init(_ cancel: @escaping () -> Void) { cancelAction = cancel }
    func cancel() { cancelAction() }
    deinit { cancelAction() }
}

/// mark:<symbol> 频道帧（合约标记价格，约 1s 一帧）
struct WSMark: Decodable {
    let symbol: String
    let markPrice: String
    let indexPrice: String?
    let fundingRate: String?
    let nextFundingTime: Int64?
}
