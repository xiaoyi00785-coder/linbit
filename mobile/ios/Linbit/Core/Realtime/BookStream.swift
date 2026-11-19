import Foundation

/// 盘口数据流：现货 / 合约交易页和币种详情共用。
///
/// 服务端深度帧是一阵一阵到的（一次到十几帧，然后停几百毫秒甚至两三秒）。来一帧画一帧，
/// 看起来就是「卡一下、跳一下」。这里把帧先存下来，按固定节拍（0.5 秒）合并后发布一次，
/// 界面刷新节奏稳定，和参考 App 的观感一致（实测它约 0.6–1 秒刷新一次）。
///
/// - WS 帧只存原始数据，到节拍时才解码；带 snapshot 的帧整本替换，否则按增量合并（数量 0 = 删除）。
/// - WS 断流超过 1 秒时用 REST 兜底，每秒最多一次。
/// - 合约的外部深度目前有 5 档快照和 20 档快照两种来源。刚看到过深盘口时忽略浅快照，
///   避免盘口在 5 档和 20 档之间来回跳。
@MainActor
final class BookStream: ObservableObject {
    @Published private(set) var book = OrderBook()
    /// 至少拿到过一次盘口
    @Published private(set) var ready = false
    /// 首次加载失败的文案（有盘口后不再报错）
    @Published private(set) var error: String?

    static let tickNanos: UInt64 = 500_000_000
    /// 每侧最多保留的档数
    static let keep = 50
    private static let deepLevels = 10

    /// 每次发布后回调（模型需要同步自己的字段时用）
    var onPublish: ((OrderBook) -> Void)?

    private var symbol = ""
    private var futures = false
    private var sub: WSSubscription?
    private var loop: Task<Void, Never>?
    private var frames: [Data] = []
    private var bidMap: [Decimal: Decimal] = [:]
    private var askMap: [Decimal: Decimal] = [:]
    private var seq: Int64 = 0
    private var hasBook = false
    private var dirty = false
    private var lastDeepAt = Date.distantPast
    private var lastAcceptedAt = Date.distantPast
    private var lastRestAt = Date.distantPast
    private var restInFlight = false
    private var generation = 0

    func start(symbol: String, futures: Bool) {
        stop()
        if symbol != self.symbol || futures != self.futures { clear() }
        self.symbol = symbol
        self.futures = futures
        generation += 1
        let gen = generation
        let channel = (futures ? "futures-depth:" : "depth:") + symbol
        // 先拿一次 REST 盘口：WS 的 futures-depth 通道没数据时（后端未推深度），
        // 界面至少立刻有真实盘口，不会整块空着。
        Task { [weak self] in await self?.rest(gen) }
        sub = MarketSocket.shared.subscribe(channel) { [weak self] frame in
            // 回调在主线程：只存数据，不解码
            guard let self, gen == self.generation else { return }
            self.frames.append(frame.data)
            if self.frames.count > 64 { self.frames.removeFirst(self.frames.count - 64) }
        }
        loop = Task { [weak self] in
            await self?.rest(gen)
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: Self.tickNanos)
                guard let self, !Task.isCancelled, gen == self.generation else { return }
                self.tick(gen)
            }
        }
    }

    func stop() {
        generation += 1
        loop?.cancel(); loop = nil
        sub?.cancel(); sub = nil
        frames = []
    }

    /// 换币种时清空，避免先闪一下上一个币的盘口
    func clear() {
        bidMap = [:]; askMap = [:]; seq = 0; hasBook = false; dirty = false
        lastDeepAt = .distantPast; lastAcceptedAt = .distantPast
        if !book.bids.isEmpty || !book.asks.isEmpty { book = OrderBook() }
        if ready { ready = false }
        error = nil
    }

    /// 下拉刷新：立刻用 REST 取一次
    func refresh() async { lastRestAt = .distantPast; await rest(generation) }

    private func tick(_ gen: Int) {
        if !frames.isEmpty {
            let batch = frames
            frames = []
            // 从后往前解码：遇到整本快照就不用再看更早的帧（快照流每个节拍只需解最后一帧）
            var todo: [Frame] = []
            for raw in batch.reversed() {
                guard let f = try? JSONDecoder().decode(Frame.self, from: raw) else { continue }
                todo.append(f)
                if f.snapshot == true { break }
            }
            for f in todo.reversed() { apply(f) }
        }
        let now = Date()
        if now.timeIntervalSince(lastAcceptedAt) > 1, now.timeIntervalSince(lastRestAt) >= 1, !restInFlight {
            Task { await rest(gen) }
        }
        publish()
    }

    private func apply(_ f: Frame) {
        let snapshot = f.snapshot == true || !hasBook
        if snapshot {
            let depth = max(f.bids?.count ?? 0, f.asks?.count ?? 0)
            let now = Date()
            if depth >= Self.deepLevels { lastDeepAt = now }
            else if now.timeIntervalSince(lastDeepAt) < 5 { return }   // 浅快照：刚有深盘口时不采用
            bidMap = [:]; askMap = [:]
        } else if let s = f.lastUpdateId, s > 0, s <= seq {
            return
        }
        func put(_ rows: [[String]]?, into map: inout [Decimal: Decimal]) {
            for r in rows ?? [] where r.count >= 2 {
                guard let price = Decimal(string: r[0], locale: Dec.posix), price > 0 else { continue }
                let qty = Decimal(string: r[1], locale: Dec.posix) ?? 0
                if qty <= 0 { map.removeValue(forKey: price) } else { map[price] = qty }
            }
        }
        put(f.bids, into: &bidMap)
        put(f.asks, into: &askMap)
        if let s = f.lastUpdateId, s > 0 { seq = s }
        hasBook = true
        dirty = true
        lastAcceptedAt = Date()
    }

    private func publish() {
        guard dirty else { return }
        dirty = false
        // 增量流里可能残留穿价的旧档位：以最新一侧为准裁掉
        var bids = bidMap.map { BookLevel(price: $0.key, qty: $0.value) }.sorted { $0.price > $1.price }
        var asks = askMap.map { BookLevel(price: $0.key, qty: $0.value) }.sorted { $0.price < $1.price }
        if bids.count > Self.keep { bids.removeLast(bids.count - Self.keep) }
        if asks.count > Self.keep { asks.removeLast(asks.count - Self.keep) }
        let next = OrderBook(bids: bids, asks: asks)
        if next != book { book = next; onPublish?(next) }
        if !ready { ready = true }
        if error != nil { error = nil }
    }

    private func rest(_ gen: Int) async {
        guard !restInFlight, gen == generation else { return }
        restInFlight = true
        lastRestAt = Date()
        defer { restInFlight = false }
        do {
            let b = try await (futures ? FuturesAPI.depth(symbol, limit: 50) : SpotAPI.depth(symbol, limit: 50))
            guard gen == generation else { return }
            let depth = max(b.bids.count, b.asks.count)
            let now = Date()
            if depth >= Self.deepLevels { lastDeepAt = now }
            else if now.timeIntervalSince(lastDeepAt) < 5 { return }
            guard depth > 0 else { if !ready { ready = true }; return }
            bidMap = Dictionary(b.bids.map { ($0.price, $0.qty) }, uniquingKeysWith: { a, _ in a })
            askMap = Dictionary(b.asks.map { ($0.price, $0.qty) }, uniquingKeysWith: { a, _ in a })
            hasBook = true; dirty = true; lastAcceptedAt = now
            publish()
        } catch {
            guard gen == generation else { return }
            if !hasBook { self.error = error.lbMessage }
        }
    }

    private struct Frame: Decodable {
        var bids: [[String]]?
        var asks: [[String]]?
        var snapshot: Bool?
        var lastUpdateId: Int64?
    }
}
