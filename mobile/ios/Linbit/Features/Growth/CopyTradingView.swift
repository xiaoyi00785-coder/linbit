import SwiftUI

// MARK: - 合约跟单：带单员榜单 / 详情（收益曲线、当前持仓）/ 跟单设置 / 我的跟单 / 成为带单员
enum CopyAPI {
    struct Follow: Encodable {
        let traderId: String; let amount: String; let mode: String
        let fixedMargin: String?; let ratio: String?; let maxPerTrade: String?; let tpRatio: String?; let slRatio: String?
    }
    struct Trader: Encodable { let nickname: String; let bio: String; let profitShare: String }
    static func summary() async throws -> J { try await APIClient.shared.get("/futures/copy/summary") }
    static func traders(_ sort: String, days: Int) async throws -> J {
        try await APIClient.shared.get("/futures/copy/traders", query: ["sort": sort, "days": String(days)])
    }
    static func trader(_ id: String) async throws -> J { try await APIClient.shared.get("/futures/copy/traders/\(id)") }
    static func follows() async throws -> J { try await APIClient.shared.get("/futures/copy/follows", auth: true) }
    static func follow(_ f: Follow) async throws -> J { try await APIClient.shared.post("/futures/copy/follows", body: f) }
    static func stop(_ id: Int64) async throws { try await APIClient.shared.deleteAuth("/futures/copy/follows/\(id)") }
    static func becomeTrader(_ t: Trader) async throws -> J { try await APIClient.shared.post("/futures/copy/trader", body: t) }
}

/// 截图演示：copy:<list|empty|error|detail|follow|follow-error|follows|follows-empty|become|become-error>
enum CopyDemo {
    static var state: String? { DemoShot.state("copy") }
    static let names = ["Alpha 量化", "稳健复利", "BTC 趋势猎手", "ETH 波段王", "网格老司机", "Macro Lee"]
    static func trader(_ i: Int) -> J {
        let roi = [1.8264, 0.6421, 0.4830, 0.2215, -0.0532, 0.1307][i % 6]
        let curve: [J] = (0..<30).map { d -> J in
            let v = roi * 10000 * Double(d) / 29 + sin(Double(d + i) / 2.5) * 300
            return ["day": "2026-10-\(String(format: "%02d", d + 1))", "pnl": "\(Int(v / 10))", "cumPnl": "\(Int(v))"]
        }
        return ["traderId": "T\(1001 + i)", "nickname": "\(names[i % 6])", "bio": "专注 BTC/ETH 永续趋势交易，严格止损，单笔风险不超过 2%",
                "profitShare": "0.1", "status": "active", "days": J(120 + i * 33), "pnl": "\(Int(roi * 10000))", "roi": "\(roi)",
                "winRate": "\(0.72 - Double(i) * 0.05)", "closedTrades": J(312 - i * 40), "maxDrawdown": "\(0.08 + Double(i) * 0.02)",
                "followers": J(286 - i * 41), "maxFollowers": 300, "aum": "\(1_250_000 - i * 180_000)", "followerProfit": "\(Int(roi * 52000))",
                "equity": "\(52000 - i * 6000)", "score": "\(92 - i * 6)", "curve": J(curve)]
    }
    static var traders: J { ["items": J((0..<6).map { trader($0) })] }
    static var detail: J {
        guard case var .obj(o) = trader(0) else { return .null }
        o["positions"] = [["symbol": "BTCUSDT", "side": "long", "entryPrice": "67250.5", "leverage": 10, "marginMode": "cross", "openedAt": J(DemoShot.nowMs - 7_200_000)],
                          ["symbol": "ETHUSDT", "side": "short", "entryPrice": "3521.32", "leverage": 5, "marginMode": "isolated", "openedAt": J(DemoShot.nowMs - 86_400_000)]]
        return .obj(o)
    }
    static let summary: J = ["traders": 236, "totalFollowers": 18420, "followerProfit": "2860312.55", "aum": "48210000"]
    static func follows(_ empty: Bool) -> J {
        if empty { return ["items": []] }
        let day: Int64 = 86_400_000, now = DemoShot.nowMs
        return ["items": [
            ["id": 81, "traderId": "T1001", "nickname": "\(names[0])", "status": "active", "mode": "ratio", "ratio": "1", "principal": "1000", "returned": "0", "sharePaid": "12.6", "pnl": "238.42", "createdAt": J(now - day * 9)],
            ["id": 77, "traderId": "T1003", "nickname": "\(names[2])", "status": "active", "mode": "fixed", "fixedMargin": "50", "principal": "500", "returned": "0", "sharePaid": "0", "pnl": "-18.20", "createdAt": J(now - day * 20)],
            ["id": 52, "traderId": "T1005", "nickname": "\(names[4])", "status": "stopped", "mode": "ratio", "ratio": "0.5", "principal": "300", "returned": "342.18", "sharePaid": "4.2", "pnl": "42.18", "createdAt": J(now - day * 60), "stoppedAt": J(now - day * 30)]]]
    }
}

struct CopyTradingView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    private static let sorts = [("composite", "综合"), ("roi", "收益率"), ("followers", "跟随人数"), ("winrate", "胜率"), ("aum", "管理规模")]
    @State private var top = CopyDemo.state?.hasPrefix("follows") == true ? 1 : 0
    @State private var sort = 0
    @State private var days = 1
    @State private var summary: J = .null
    @State private var list: Loadable<[J]> = .loading
    @State private var detail: String? = ["detail", "follow", "follow-error"].contains(CopyDemo.state ?? "") ? "T1001" : nil
    @State private var become = CopyDemo.state?.hasPrefix("become") == true

    var body: some View {
        ZStack {
            VStack(spacing: 0) {
                NavBar(title: "跟单交易", onBack: onBack) {
                    Button { Session.shared.requireLogin { become = true } } label: {
                        Text("成为带单员").font(.lb(FontSize.small, .medium)).foregroundColor(p.brand)
                    }
                }
                PrimaryTabs(items: ["带单员", "我的跟单"], selected: $top, size: FontSize.label, underline: true)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Spacing.pageH)
                Divider().overlay(p.border)
                if top == 0 { tradersTab } else { CopyFollowsView() }
            }
            .background(p.bg.ignoresSafeArea())
            if let id = detail {
                CopyTraderDetail(traderId: id) { detail = nil }.transition(.move(edge: .trailing))
            }
        }
        .task(id: "\(sort)-\(days)") { await load() }
        .lbSheet($become) { CopyBecomeSheet { become = false } }
    }

    private var tradersTab: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 10) {
                    StatTile(title: "带单员", value: summary["traders"].s ?? "--")
                    StatTile(title: "跟单人数", value: summary["totalFollowers"].s ?? "--")
                    StatTile(title: "跟单总收益", value: jAmt(summary["followerProfit"], 0), color: p.up)
                }
                HStack {
                    ChipBar(items: Self.sorts.map(\.1), selected: $sort)
                }
                PillSegment(items: ["7 天", "30 天", "90 天"], selected: $days, height: 30)
                switch list {
                case .idle, .loading: ProgressView().frame(maxWidth: .infinity).padding(.vertical, 60)
                case let .failed(m): ErrorStateView(message: m) { Task { await load() } }.frame(height: 300)
                case let .loaded(items) where items.isEmpty: EmptyStateView(text: "暂无带单员").frame(height: 300)
                case let .loaded(items):
                    LazyVStack(spacing: 12) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, t in
                            CopyTraderCard(t: t) { detail = t["traderId"].s }
                        }
                    }
                }
            }.padding(Spacing.pageH)
        }.refreshable { await load() }
    }

    private func load() async {
        if let st = CopyDemo.state {
            summary = CopyDemo.summary
            list = st == "error" ? .failed("网络连接失败，请检查网络") : .loaded(st == "empty" ? [] : CopyDemo.traders.items); return
        }
        if list.value == nil { list = .loading }
        async let s = try? CopyAPI.summary()
        do { list = .loaded(try await CopyAPI.traders(Self.sorts[sort].0, days: [7, 30, 90][days]).items) } catch { list = .failed(securityMessage(error)) }
        summary = await s ?? summary
    }
}

struct CopyCurve: View {
    @Environment(\.palette) private var p
    let points: [Double]
    var body: some View {
        GeometryReader { g in
            let lo = points.min() ?? 0, hi = points.max() ?? 1, span = max(hi - lo, 1)
            let up = (points.last ?? 0) >= 0
            Path { path in
                for (i, v) in points.enumerated() {
                    let x = g.size.width * CGFloat(i) / CGFloat(max(points.count - 1, 1))
                    let y = g.size.height * CGFloat(1 - (v - lo) / span)
                    if i == 0 { path.move(to: .init(x: x, y: y)) } else { path.addLine(to: .init(x: x, y: y)) }
                }
            }.stroke(up ? p.up : p.down, lineWidth: 1.5)
        }
    }
}

private func traderAvatar(_ name: String, _ p: Palette) -> some View {
    Text(String(name.prefix(1))).font(.lb(16, .bold)).foregroundColor(p.brand)
        .frame(width: 40, height: 40).background(Circle().fill(p.brand.opacity(0.12)))
}

struct CopyTraderCard: View {
    @Environment(\.palette) private var p
    let t: J
    let onTap: () -> Void
    var body: some View {
        let roi = t["roi"].d
        let full = (t["followers"].i ?? 0) >= (t["maxFollowers"].i ?? Int64.max)
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                traderAvatar(t["nickname"].s ?? "?", p)
                VStack(alignment: .leading, spacing: 3) {
                    Text(t["nickname"].s ?? "--").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary).lineLimit(1)
                    Text("跟随 \(t["followers"].s ?? "0")/\(t["maxFollowers"].s ?? "--") · 带单 \(t["days"].s ?? "0") 天").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                }
                Spacer()
                LBButton(title: full ? "已满员" : "跟单", height: 30, enabled: !full, action: onTap).frame(width: 72)
            }
            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("收益率").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Text((roi >= 0 ? "+" : "") + jRatio(t["roi"])).font(.lbNum(22, .bold)).foregroundColor(roi >= 0 ? p.up : p.down)
                }
                Spacer()
                CopyCurve(points: t["curve"].a.map { NSDecimalNumber(decimal: $0["cumPnl"].d).doubleValue }).frame(width: 120, height: 40)
            }
            HStack {
                KV(k: "收益", v: jSigned(t["pnl"]))
                Spacer(minLength: 16)
                KV(k: "胜率", v: jRatio(t["winRate"], 1))
                Spacer(minLength: 16)
                KV(k: "回撤", v: jRatio(t["maxDrawdown"], 1))
            }
        }
        .lbCard()
        .contentShape(Rectangle())
        .onTapGesture(perform: onTap)
    }
}

struct CopyTraderDetail: View {
    @Environment(\.palette) private var p
    let traderId: String
    let onBack: () -> Void
    @State private var data: Loadable<J> = .loading
    @State private var following = CopyDemo.state?.hasPrefix("follow") == true && CopyDemo.state?.hasPrefix("follows") == false

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "带单员详情", onBack: onBack)
            switch data {
            case .idle, .loading: Spacer(); ProgressView(); Spacer()
            case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
            case let .loaded(raw):
                let t = raw["trader"].isNull ? raw : raw["trader"]
                let positions = raw["positions"].a
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(spacing: 12) {
                            traderAvatar(t["nickname"].s ?? "?", p)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(t["nickname"].s ?? "--").font(.lb(18, .bold)).foregroundColor(p.textPrimary)
                                Text("分润比例 \(jRatio(t["profitShare"], 0)) · 带单 \(t["days"].s ?? "0") 天").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                            }
                        }
                        if let bio = t["bio"].s, !bio.isEmpty { Text(bio).font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                        VStack(alignment: .leading, spacing: 10) {
                            Text("收益曲线").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                            CopyCurve(points: t["curve"].a.map { NSDecimalNumber(decimal: $0["cumPnl"].d).doubleValue }).frame(height: 120)
                        }.lbCard()
                        LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10), GridItem(.flexible())], spacing: 10) {
                            StatTile(title: "收益率", value: jRatio(t["roi"]), color: t["roi"].d >= 0 ? p.up : p.down)
                            StatTile(title: "累计收益", value: jSigned(t["pnl"]))
                            StatTile(title: "胜率", value: jRatio(t["winRate"], 1))
                            StatTile(title: "最大回撤", value: jRatio(t["maxDrawdown"], 1))
                            StatTile(title: "跟单收益", value: jAmt(t["followerProfit"], 0))
                            StatTile(title: "管理规模", value: jAmt(t["aum"], 0))
                        }
                        Text("当前持仓（\(positions.count)）").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                        if positions.isEmpty {
                            EmptyStateView(text: "暂无持仓").frame(height: 140)
                        } else {
                            ForEach(Array(positions.enumerated()), id: \.offset) { _, pos in
                                let long = pos["side"].s == "long"
                                HStack {
                                    VStack(alignment: .leading, spacing: 4) {
                                        HStack(spacing: 6) {
                                            Text(pos["symbol"].s ?? "--").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                                            Text("\(long ? "多" : "空") \(pos["leverage"].s ?? "-")x").font(.lb(10)).foregroundColor(long ? p.up : p.down)
                                                .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill((long ? p.up : p.down).opacity(0.12)))
                                            Text(pos["marginMode"].s == "isolated" ? "逐仓" : "全仓").font(.lb(10)).foregroundColor(p.textSecondary)
                                        }
                                        Text("开仓 \(pos["openedAt"].time)").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                                    }
                                    Spacer()
                                    VStack(alignment: .trailing, spacing: 4) {
                                        Text("开仓价").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                                        Text(jAmt(pos["entryPrice"], 2)).font(.lbNum(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                                    }
                                }.lbCard()
                            }
                        }
                    }.padding(Spacing.pageH)
                }
                let closed = t["status"].s != nil && t["status"].s != "active"
                let full = (t["followers"].i ?? 0) >= (t["maxFollowers"].i ?? Int64.max)
                LBButton(title: closed ? "带单已关闭" : (full ? "跟单人数已满" : "立即跟单"), enabled: !closed && !full) {
                    Session.shared.requireLogin { following = true }
                }
                .padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .lbSheet($following) {
            CopyFollowSheet(trader: data.value.map { $0["trader"].isNull ? $0 : $0["trader"] } ?? .null) { following = false }
        }
    }

    private func load() async {
        if CopyDemo.state != nil { data = .loaded(CopyDemo.detail); return }
        do { data = .loaded(try await CopyAPI.trader(traderId)) } catch { data = .failed(securityMessage(error)) }
    }
}

struct CopyFollowSheet: View {
    @Environment(\.palette) private var p
    let trader: J
    let onClose: () -> Void
    @State private var mode = 0
    @State private var amount = CopyDemo.state?.hasPrefix("follow") == true ? "1000" : ""
    @State private var ratio = "1"
    @State private var fixedMargin = "50"
    @State private var maxPerTrade = ""
    @State private var tp = ""
    @State private var sl = ""
    @State private var loading = false
    @State private var error: String? = CopyDemo.state == "follow-error" ? ErrorText.map["INSUFFICIENT_BALANCE"] : nil

    private var amountErr: String? { amount.isEmpty || Dec.parse(amount) >= 10 ? nil : "跟单金额最低 10 USDT" }
    private var ratioErr: String? { mode == 0 && !ratio.isEmpty && (Dec.parse(ratio) <= 0 || Dec.parse(ratio) > 10) ? "跟单比例需在 0.01 - 10 倍之间" : nil }
    private var fixedErr: String? { mode == 1 && !fixedMargin.isEmpty && Dec.parse(fixedMargin) <= 0 ? "请输入有效的单笔保证金" : nil }
    private var pctErr: String? {
        let bad = [tp, sl].contains { !$0.isEmpty && (Dec.parse($0) <= 0 || Dec.parse($0) > 100) }
        return bad ? "止盈 / 止损比例需在 1% - 100% 之间" : nil
    }
    private var valid: Bool {
        Dec.parse(amount) >= 10 && amountErr == nil && ratioErr == nil && fixedErr == nil && pctErr == nil
            && (mode == 0 ? !ratio.isEmpty : !fixedMargin.isEmpty)
    }

    var body: some View {
        LBSheet(title: "跟单 \(trader["nickname"].s ?? "")", subtitle: "分润比例 \(jRatio(trader["profitShare"], 0))，仅对盈利部分收取", showClose: true, onClose: onClose) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if let error { InlineBanner(text: error) }
                    PillSegment(items: ["按比例跟单", "固定保证金"], selected: $mode)
                    LBTextField(label: "跟单金额 (USDT)", placeholder: "最低 10 USDT", text: $amount, keyboard: .decimalPad, error: amountErr)
                    if mode == 0 {
                        LBTextField(label: "跟单比例（倍）", placeholder: "带单员仓位的倍数，如 1", text: $ratio, keyboard: .decimalPad, error: ratioErr)
                    } else {
                        LBTextField(label: "单笔保证金 (USDT)", placeholder: "每笔跟单使用的保证金", text: $fixedMargin, keyboard: .decimalPad, error: fixedErr)
                    }
                    LBTextField(label: "单笔最大保证金 (USDT，选填)", placeholder: "不限", text: $maxPerTrade, keyboard: .decimalPad)
                    HStack(spacing: 10) {
                        LBTextField(label: "止盈比例 %（选填）", placeholder: "止盈 %", text: $tp, keyboard: .decimalPad)
                        LBTextField(label: "止损比例 %（选填）", placeholder: "止损 %", text: $sl, keyboard: .decimalPad)
                    }
                    if let pctErr { Text(pctErr).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    InfoNote(text: "跟单资金将从合约账户划入独立跟单仓位，带单员开平仓时自动跟随。停止跟单将按市价平掉跟单仓位并返还剩余资金。")
                    LBButton(title: "确认跟单", enabled: valid, loading: loading) { Task { await submit() } }
                }.padding(.horizontal, Spacing.pageH).padding(.bottom, 16)
            }.frame(maxHeight: 560)
        }
    }

    private func opt(_ s: String) -> String? { s.isEmpty ? nil : s }
    private func pct(_ s: String) -> String? { s.isEmpty ? nil : "\(Dec.parse(s) / 100)" }

    private func submit() async {
        error = nil
        if CopyDemo.state != nil { return }
        guard let id = trader["traderId"].s else { return }
        loading = true; defer { loading = false }
        do {
            _ = try await CopyAPI.follow(.init(traderId: id, amount: amount, mode: mode == 0 ? "ratio" : "fixed",
                                               fixedMargin: mode == 1 ? fixedMargin : nil, ratio: mode == 0 ? ratio : nil,
                                               maxPerTrade: opt(maxPerTrade), tpRatio: pct(tp), slRatio: pct(sl)))
            Haptics.success(); ToastCenter.shared.show("跟单成功", .success); onClose()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

struct CopyFollowsView: View {
    @Environment(\.palette) private var p
    @State private var data: Loadable<[J]> = .loading
    @State private var confirm: ConfirmRequest?
    @State private var banner: String?

    var body: some View {
        VStack(spacing: 0) {
            if let banner { InlineBanner(text: banner) }
            switch data {
            case .idle, .loading: Spacer(); ProgressView(); Spacer()
            case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
            case let .loaded(items) where items.isEmpty: Spacer(); EmptyStateView(text: "暂无跟单，去带单员榜单挑选吧"); Spacer()
            case let .loaded(items):
                ScrollView {
                    LazyVStack(spacing: 12) { ForEach(Array(items.enumerated()), id: \.offset) { _, f in card(f) } }.padding(Spacing.pageH)
                }.refreshable { await load() }
            }
        }
        .task { await load() }
        .lbConfirm($confirm)
    }

    private func card(_ f: J) -> some View {
        let active = f["status"].s == "active"
        let pnl = f["pnl"]
        return VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text(f["nickname"].s ?? f["traderId"].s ?? "--").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text(active ? "跟单中" : "已停止").font(.lb(10)).foregroundColor(active ? p.up : p.textTertiary)
                    .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill(active ? p.up.opacity(0.12) : p.fill))
                Spacer()
                Text(f["mode"].s == "fixed" ? "固定 \(jAmt(f["fixedMargin"])) U" : "比例 \(f["ratio"].s ?? "1")x").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            }
            KV(k: "跟单本金", v: "\(jAmt(f["principal"])) USDT")
            if !pnl.isNull { KV(k: "跟单收益", v: "\(jSigned(pnl)) USDT", color: pnl.d >= 0 ? p.up : p.down) }
            KV(k: "已付分润", v: "\(jAmt(f["sharePaid"])) USDT")
            KV(k: active ? "开始时间" : "停止时间", v: active ? f["createdAt"].time : f["stoppedAt"].time)
            if active {
                LBButton(title: "停止跟单", kind: .outline, height: 34) {
                    confirm = .init(title: "停止跟单", message: "停止后将按市价平掉该带单员的全部跟单仓位，剩余资金返还至合约账户。", confirmTitle: "确认停止", destructive: true) {
                        Task { await stop(f) }
                    }
                }
            }
        }.lbCard()
    }

    private func stop(_ f: J) async {
        banner = nil
        if CopyDemo.state != nil { return }
        guard let id = f["id"].i else { return }
        do { try await CopyAPI.stop(id); Haptics.success(); ToastCenter.shared.show("已停止跟单", .success); await load() }
        catch { banner = securityMessage(error); Haptics.error() }
    }

    private func load() async {
        if let st = CopyDemo.state {
            data = .loaded(CopyDemo.follows(st == "follows-empty").items); return
        }
        guard Session.shared.isLoggedIn else { data = .loaded([]); return }
        if data.value == nil { data = .loading }
        do { data = .loaded(try await CopyAPI.follows().items) } catch { data = .failed(securityMessage(error)) }
    }
}

struct CopyBecomeSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    @State private var nickname = CopyDemo.state?.hasPrefix("become") == true ? "Alpha 量化" : ""
    @State private var bio = CopyDemo.state?.hasPrefix("become") == true ? "专注 BTC/ETH 永续趋势交易" : ""
    @State private var share = 1
    @State private var loading = false
    @State private var error: String? = CopyDemo.state == "become-error" ? ErrorText.map["TRADER_EQUITY_TOO_LOW"] : nil
    private static let shares = ["5%", "10%", "15%", "20%"]

    private var nickErr: String? { nickname.isEmpty || (2...20).contains(nickname.count) ? nil : "昵称需为 2-20 个字符" }
    private var bioErr: String? { bio.count <= 200 ? nil : "简介不超过 200 字" }

    var body: some View {
        LBSheet(title: "成为带单员", subtitle: "合约账户权益需满足最低要求，跟单者盈利时按分润比例向您支付", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 12) {
                if let error { InlineBanner(text: error) }
                LBTextField(label: "昵称", placeholder: "2-20 个字符", text: $nickname, error: nickErr)
                LBTextField(label: "简介", placeholder: "介绍您的交易风格", text: $bio, error: bioErr)
                Text("分润比例").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                PillSegment(items: Self.shares, selected: $share)
                LBButton(title: "提交", enabled: (2...20).contains(nickname.count) && bioErr == nil, loading: loading) { Task { await submit() } }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 16)
        }
    }

    private func submit() async {
        error = nil
        if CopyDemo.state != nil { return }
        loading = true; defer { loading = false }
        do {
            _ = try await CopyAPI.becomeTrader(.init(nickname: nickname, bio: bio, profitShare: ["0.05", "0.1", "0.15", "0.2"][share]))
            Haptics.success(); ToastCenter.shared.show("已成为带单员", .success); onClose()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}
