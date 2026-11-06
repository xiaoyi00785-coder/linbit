import SwiftUI

// MARK: - TradFi：代币化股票 / 贵金属 / 商品，点击进入现货或永续交易
enum TradFiAPI {
    static func assets(_ kind: String?) async throws -> J { try await APIClient.shared.get("/tradfi/assets", query: ["kind": kind]) }
}

/// 截图演示：tradfi:<list|metal|empty|restricted|disabled|unavailable|error>
enum TradFiDemo {
    static var state: String? { DemoShot.state("tradfi") }
    static func a(_ asset: String, _ kind: String, _ name: String, _ u: String, _ market: String, _ last: String, _ ref: String, _ dev: String, _ widened: Bool = false, _ status: String = "active") -> J {
        ["asset": J.str(asset), "kind": J.str(kind), "name": J.str(name), "underlying": J.str(u), "market": J.str(market), "symbol": J.str(asset + "USDT"),
         "status": J.str(status), "lastPrice": J.str(last), "referencePrice": J.str(ref), "referenceSource": "binance-tradfi",
         "currentDeviationBps": J.str(dev), "widened": J.bool(widened)]
    }
    static let all: [J] = [
        a("AAPL", "stock", "苹果", "Apple Inc. (NASDAQ: AAPL)", "spot", "228.41", "228.12", "13"),
        a("TSLA", "stock", "特斯拉", "Tesla, Inc. (NASDAQ: TSLA)", "spot", "251.07", "250.66", "16"),
        a("NVDA", "stock", "英伟达", "NVIDIA Corp. (NASDAQ: NVDA)", "spot", "138.92", "135.85", "226", true),
        a("SPY", "stock", "标普 500 ETF", "SPDR S&P 500 ETF (NYSE: SPY)", "spot", "571.3", "571.02", "5"),
        a("COIN", "stock", "Coinbase", "Coinbase Global (NASDAQ: COIN)", "spot", "205.8", "205.5", "15", false, "suspended"),
        a("PAXG", "metal", "Pax Gold", "1 金衡盎司伦敦金", "spot", "2741.6", "2738.9", "10"),
        a("XAUT", "metal", "Tether Gold", "1 金衡盎司伦敦金", "spot", "2739.2", "2738.9", "1"),
        a("XAG", "metal", "白银永续", "白银现货 (XAG/USD)", "futures", "32.68", "32.66", "6"),
        a("WTI", "commodity", "原油永续", "WTI 原油期货", "futures", "71.42", "71.38", "6"),
    ]
}

struct TradFiView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    @State private var kind = TradFiDemo.state == "metal" ? 2 : 0
    @State private var data: Loadable<[J]> = .loading
    private let kinds: [String?] = [nil, "stock", "metal", "commodity"]

    var body: some View {
        VStack(spacing: 0) {
            if let onBack { NavBar(title: "TradFi", onBack: onBack) }
            PillSegment(items: ["全部", "股票", "贵金属", "商品"], selected: $kind).padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
            switch data {
            case .idle, .loading: VStack { Spacer(); ProgressView(); Spacer() }
            case let .failed(m): blocked(m)
            case let .loaded(items) where items.isEmpty: EmptyStateView(text: "暂无可交易资产").frame(maxHeight: .infinity)
            case let .loaded(items):
                ScrollView {
                    LazyVStack(spacing: 0) {
                        HStack {
                            Text("资产").frame(maxWidth: .infinity, alignment: .leading)
                            Text("最新价 / 参考价").frame(width: 130, alignment: .trailing)
                            Text("偏离").frame(width: 64, alignment: .trailing)
                        }.font(.lb(FontSize.small)).foregroundColor(p.textTertiary).padding(.horizontal, Spacing.pageH).padding(.bottom, 6)
                        ForEach(Array(items.enumerated()), id: \.offset) { _, a in row(a) }
                        InfoNote(text: "代币化资产由发行方 1:1 托管底层资产；参考价来自永续合约市场，偏离过大时做市价差会自动放宽。部分国家或地区不提供此服务。")
                            .padding(Spacing.pageH)
                    }
                }.refreshable { await load() }
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task(id: kind) { await load() }
    }

    private func blocked(_ m: String) -> some View {
        let restricted = m == ErrorText.map["REGION_RESTRICTED"] || m == ErrorText.map["FEATURE_DISABLED"]
        return VStack(spacing: 12) {
            Spacer()
            Image(systemName: restricted ? "globe.badge.chevron.backward" : "exclamationmark.triangle").font(.system(size: 40)).foregroundColor(p.textTertiary)
            Text(m).font(.lb(FontSize.body)).foregroundColor(p.textSecondary).multilineTextAlignment(.center)
            if !restricted { LBButton(title: "重试", kind: .outline, height: 36) { Task { await load() } }.frame(width: 120) }
            Spacer()
        }.padding(Spacing.pageH).frame(maxWidth: .infinity)
    }

    private func row(_ a: J) -> some View {
        let active = a["status"].s != "suspended"
        let dev = a["currentDeviationBps"].d
        return Button {
            guard active, let sym = a["symbol"].s else { return }
            Task { @MainActor in
                Router.shared.feature = nil
                if a["market"].s == "futures" { Router.shared.tradeFutures(sym) } else { Router.shared.trade(sym) }
            }
        } label: {
            HStack(spacing: 10) {
                Text(String((a["asset"].s ?? "?").prefix(1))).font(.lb(FontSize.body, .bold)).foregroundColor(p.textPrimary)
                    .frame(width: 32, height: 32).background(Circle().fill(p.fill))
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 4) {
                        Text(a["asset"].s ?? "--").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                        Text(a["market"].s == "futures" ? "永续" : "现货").font(.lb(10)).foregroundColor(p.textSecondary)
                            .padding(.horizontal, 4).frame(height: 16).background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                        if !active { Text("暂停").font(.lb(10)).foregroundColor(p.down) }
                    }
                    Text(a["underlying"].s ?? a["name"].s ?? "").font(.lb(FontSize.small)).foregroundColor(p.textTertiary).lineLimit(1)
                }.frame(maxWidth: .infinity, alignment: .leading)
                VStack(alignment: .trailing, spacing: 3) {
                    Text(jAmt(a["lastPrice"])).font(.lbNum(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                    Text(jAmt(a["referencePrice"])).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                }.frame(width: 130, alignment: .trailing)
                Text(a["currentDeviationBps"].isNull ? "--" : "\(Fmt.amount(dev / 100, 2))%")
                    .font(.lbNum(FontSize.small, .semibold)).foregroundColor(a["widened"].b ? p.down : p.textSecondary)
                    .frame(width: 64, alignment: .trailing)
            }
            .padding(.horizontal, Spacing.pageH).padding(.vertical, 10).opacity(active ? 1 : 0.5).contentShape(Rectangle())
        }.buttonStyle(.plain)
    }

    private func load() async {
        if let st = TradFiDemo.state {
            switch st {
            case "error": data = .failed("网络连接失败，请检查网络")
            case "restricted": data = .failed(ErrorText.map["REGION_RESTRICTED"] ?? "")
            case "disabled": data = .failed(ErrorText.map["FEATURE_DISABLED"] ?? "")
            case "unavailable": data = .failed(ErrorText.map["SERVICE_UNAVAILABLE"] ?? "")
            case "empty": data = .loaded([])
            default: data = .loaded(TradFiDemo.all.filter { kinds[kind] == nil || $0["kind"].s == kinds[kind] })
            }
            return
        }
        if data.value == nil { data = .loading }
        do { data = .loaded(try await TradFiAPI.assets(kinds[kind]).items) } catch { data = .failed(securityMessage(error)) }
    }
}
