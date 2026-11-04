import SwiftUI
import UIKit

/// 选择币种（对照图 #38 / ref2 #17）：搜索、热门币种、A-Z 分组 + 右侧索引
struct CoinPickerView: View {
    enum Mode { case deposit }
    @Environment(\.palette) private var p
    let mode: Mode
    @State private var q = ""
    @ObservedObject private var store = MarketStore.shared
    @ObservedObject private var assets = AssetsModel.shared
    private let hot = ["BTC", "ETH", "USDT"]

    private var all: [String] {
        let s = Set(store.spot.map { String($0.symbol.dropLast(4)) } + ["USDT"] + assets.list.map(\.asset))
        return s.sorted()
    }
    private var names: [String: String] { ["BTC": "Bitcoin", "ETH": "Ether", "USDT": "Tether USDT", "BNB": "BNB", "SOL": "SOL", "TRX": "TRON", "DOGE": "DOGE"] }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "选择币种") { EmptyView() }
            SearchField(text: $q, placeholder: "搜索币种").padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
            let filtered = all.filter { q.isEmpty || $0.localizedCaseInsensitiveContains(q) || (names[$0] ?? "").localizedCaseInsensitiveContains(q) }
            if filtered.isEmpty { EmptyStateView(text: "暂无相关币种").frame(maxHeight: .infinity) } else {
                ScrollViewReader { proxy in
                    ZStack(alignment: .trailing) {
                        ScrollView {
                            LazyVStack(alignment: .leading, spacing: 0, pinnedViews: []) {
                                if q.isEmpty { section("热门币种"); ForEach(hot, id: \.self) { row($0) } }
                                let groups = Dictionary(grouping: filtered) { String($0.prefix(1)) }
                                ForEach(groups.keys.sorted(), id: \.self) { k in
                                    section(k).id(k)
                                    ForEach(groups[k]!, id: \.self) { row($0) }
                                }
                            }
                        }
                        VStack(spacing: 2) {
                            ForEach(Array(Set(filtered.map { String($0.prefix(1)) })).sorted(), id: \.self) { k in
                                Button { withAnimation { proxy.scrollTo(k, anchor: .top) }; Haptics.tap() } label: { Text(k).font(.lb(10)).foregroundColor(p.textSecondary).frame(width: 18) }
                            }
                        }.padding(.trailing, 4)
                    }
                }
            }
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { if store.spot.isEmpty { await store.load() } }
    }
    private func section(_ t: String) -> some View {
        Text(t).font(.lb(FontSize.caption, t.count > 1 ? .semibold : .regular)).foregroundColor(t.count > 1 ? p.textPrimary : p.textTertiary)
            .padding(.horizontal, Spacing.pageH).padding(.top, 14).padding(.bottom, 4)
    }
    private func row(_ a: String) -> some View {
        NavigationLink(value: DepositTarget(asset: a)) {
            HStack(spacing: 12) {
                CoinIcon(base: a, size: 28)
                VStack(alignment: .leading, spacing: 2) {
                    Text(a).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    Text(names[a] ?? a).font(.lb(11)).foregroundColor(p.textTertiary)
                }
                Spacer()
                if let b = assets.list.first(where: { $0.asset == a }) {
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(b.total).font(.lbNum(FontSize.label)).foregroundColor(p.textPrimary)
                        Text("≈ " + Fmt.fiat(assets.usd(b), rate: AppConfig.usdtCny)).font(.lbNum(11)).foregroundColor(p.textTertiary)
                    }
                }
            }.padding(.leading, Spacing.pageH).padding(.trailing, 28).frame(height: 54).contentShape(Rectangle())
        }.buttonStyle(RowPressStyle())
    }
}

/// 充值（对照图 #37）：币种、二维码、网络（ref2 #18 风格 Sheet）、地址 + 复制、最小充值 / 到账确认、保存并分享地址
struct DepositView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State var asset: String
    @State private var nets: Loadable<[APIWithdrawNetwork]> = .loading
    @State private var net: APIWithdrawNetwork?
    @State private var addr: Loadable<APIDepositAddress> = .idle
    @State private var showNets = false
    @State private var showAccount = false
    @State private var account = "资金账户"

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "充值") {
                NavigationLink(value: AssetsRoute.records) { Image(systemName: "doc.text.magnifyingglass").foregroundColor(p.textPrimary) }
            }
            ScrollView {
                VStack(spacing: 16) {
                    Button { dismiss() } label: {
                        HStack(spacing: 8) {
                            CoinIcon(base: asset, size: 22)
                            Text(asset).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Spacer(); Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
                        }.padding(.horizontal, 12).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                    }.buttonStyle(PressableStyle(scale: 1))
                    qr
                    infoCard
                    VStack(spacing: 12) {
                        kv("充值后自动划转至") {
                            Button { showAccount = true } label: { HStack(spacing: 3) { Text(account); Image(systemName: "arrowtriangle.down.fill").font(.system(size: 6)) } }
                                .font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary)
                        }
                        kv("最小充值") { Text("\(addr.value?.minDeposit ?? net?.minWithdraw ?? "--") \(asset)").font(.lbNum(FontSize.caption, .medium)).foregroundColor(p.textPrimary) }
                        kv("充值确认") { Text(net?.confirmations.map { "\($0)次网络确认" } ?? "--").font(.lb(FontSize.caption, .medium)).foregroundColor(p.textPrimary) }
                        Text("* 请勿向上述地址充值 \(asset) 以外的资产，否则资产将不可找回").font(.lb(11)).foregroundColor(p.textTertiary).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }.padding(.horizontal, Spacing.pageH).padding(.top, 8)
            }
            LBButton(title: "保存并分享地址", enabled: addr.value != nil) { share() }.padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await loadNets() }
        .lbSheet($showNets) { NetworkSheet(asset: asset, nets: nets.value ?? [], selected: net?.network) { n in showNets = false; net = n; Task { await loadAddr() } } onClose: { showNets = false } }
        .lbSheet($showAccount) {
            LBSheet(title: "充值后自动划转至", onClose: { showAccount = false }) {
                VStack(spacing: 0) {
                    ForEach(["资金账户", "现货账户"], id: \.self) { a in SheetOptionRow(title: a, selected: a == account) { account = a; showAccount = false } }
                }.padding(.bottom, 20)
            }
        }
    }

    @ViewBuilder private var qr: some View {
        ZStack {
            switch addr {
            case .loaded(let a):
                QRImage(text: a.address).frame(width: 168, height: 168)
                CoinIcon(base: asset, size: 32).padding(3).background(Circle().fill(.white))
            case .failed(let e): ErrorStateView(message: e) { Task { await loadAddr() } }
            case .idle: Text("请选择网络").font(.lb(FontSize.caption)).foregroundColor(p.textTertiary)
            case .loading: ProgressView()
            }
        }.frame(width: 190, height: 190).background(RoundedRectangle(cornerRadius: Radius.md).fill(.white))
    }

    private var infoCard: some View {
        VStack(alignment: .leading, spacing: 14) {
            Button { if nets.value != nil { showNets = true } else { Task { await loadNets() } } } label: {
                HStack {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("网络").font(.lb(11)).foregroundColor(p.textSecondary)
                        Text(net.map { NetworkSheet.display($0.network) } ?? (nets.value == nil ? "加载中…" : "请选择网络"))
                            .font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    }
                    Spacer(); Image(systemName: "arrow.left.arrow.right").font(.system(size: 13)).foregroundColor(p.textSecondary)
                }.contentShape(Rectangle())
            }.buttonStyle(PressableStyle(scale: 1))
            Divider().overlay(p.divider)
            VStack(alignment: .leading, spacing: 4) {
                Text("充值地址").font(.lb(11)).foregroundColor(p.textSecondary)
                HStack(alignment: .center) {
                    Text(addr.value?.address ?? "--").font(.lbNum(FontSize.label, .medium)).foregroundColor(p.textPrimary).lineLimit(3)
                    Spacer(minLength: 12)
                    Button { copy(addr.value?.address) } label: {
                        Text("复制").font(.lb(FontSize.small, .medium)).foregroundColor(p.textOnPrimary).padding(.horizontal, 14).frame(height: 28)
                            .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.primary))
                    }.buttonStyle(PressableStyle()).disabled(addr.value == nil)
                }
                if let memo = addr.value?.memo, !memo.isEmpty {
                    HStack { Text("Memo  \(memo)").font(.lbNum(FontSize.caption)).foregroundColor(p.textPrimary); Spacer(); Button("复制") { copy(memo) }.font(.lb(FontSize.small)) }
                    WarnBanner(text: "充值 \(asset) 时必须同时填写 Memo，否则资产将无法到账")
                }
            }
        }.padding(14).background(RoundedRectangle(cornerRadius: Radius.xl).fill(p.fill))
    }

    private func kv<V: View>(_ k: String, @ViewBuilder _ v: () -> V) -> some View {
        HStack { Text(k).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary); Spacer(); v() }
    }
    private func copy(_ s: String?) { guard let s else { return }; UIPasteboard.general.string = s; Haptics.success(); ToastCenter.shared.show("已复制", .success) }

    private func loadNets() async {
        nets = .loading
        do {
            // 充值网络按币种取 GET /wallet/networks?asset=（含 minDeposit / 充值开关）
            let l = try await AssetsAPI.walletNetworks(asset: asset).compactMap { n -> APIWithdrawNetwork? in
                guard let name = n.network, n.depositEnabled != false, n.asset == nil || n.asset == asset else { return nil }
                return APIWithdrawNetwork(network: name, minWithdraw: n.minDeposit, confirmations: n.confirmations, estimatedArrivalMinutes: n.estimatedArrivalMinutes)
            }
            nets = .loaded(l)
            if l.count == 1 { net = l[0]; await loadAddr() } else if !l.isEmpty { showNets = true }
            else { addr = .failed("该币种暂不支持充值") }
        } catch { nets = .failed(error.lbMessage); addr = .failed(error.lbMessage) }
    }
    private func loadAddr() async {
        guard let n = net else { return }
        addr = .loading
        do { addr = .loaded(try await AssetsAPI.depositAddress(asset: asset, network: n.network)) } catch { addr = .failed(error.lbMessage) }
    }
    private func share() {
        guard let a = addr.value else { return }
        let r = ImageRenderer(content: DepositShareCard(asset: asset, network: NetworkSheet.display(a.network), address: a.address).frame(width: 360))
        r.scale = 3
        guard let img = r.uiImage else { return }
        let vc = UIActivityViewController(activityItems: [img, a.address], applicationActivities: nil)
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow }.first?.rootViewController?.present(vc, animated: true)
    }
}

struct DepositShareCard: View {
    let asset: String, network: String, address: String
    var body: some View {
        VStack(spacing: 14) {
            Text("Linbit · \(asset) 充值地址").font(.lb(17, .bold))
            QRImage(text: address).frame(width: 200, height: 200)
            Text(network).font(.lb(13)).foregroundColor(.gray)
            Text(address).font(.lbNum(13)).multilineTextAlignment(.center)
        }.padding(24).background(Color.white).foregroundColor(.black)
    }
}

/// 选择网络（ref2 #18）：提示 + 网络卡片（最低 / 推荐 角标、手续费、预计到账时间）
struct NetworkSheet: View {
    @Environment(\.palette) private var p
    let asset: String
    let nets: [APIWithdrawNetwork]
    let selected: String?
    let onPick: (APIWithdrawNetwork) -> Void
    let onClose: () -> Void

    static func display(_ n: String) -> String {
        ["TRON": "Tron (TRC20)", "TRC20": "Tron (TRC20)", "ETH": "Ethereum (ERC20)", "ERC20": "Ethereum (ERC20)", "BSC": "BNB Smart Chain (BEP20)",
         "BEP20": "BNB Smart Chain (BEP20)", "ARBITRUM": "Arbitrum One", "SOL": "Solana", "BTC": "Bitcoin"][n.uppercased()] ?? n
    }
    private var cheapest: String? { nets.min { Dec.parse($0.gasFeeEstimate) < Dec.parse($1.gasFeeEstimate) }?.network }
    private var fastest: String? { nets.min { ($0.estimatedArrivalMinutes ?? 999) < ($1.estimatedArrivalMinutes ?? 999) }?.network }

    var body: some View {
        LBSheet(title: "选择网络", showClose: true, onClose: onClose) {
            VStack(spacing: 10) {
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: "info.circle").font(.system(size: 12))
                    Text("请确保您选择的网络与提币平台使用的网络一致，否则可能导致资产丢失。").font(.lb(FontSize.small))
                }.foregroundColor(p.textSecondary).frame(maxWidth: .infinity, alignment: .leading)
                if nets.isEmpty { EmptyStateView(text: "暂无可用网络").frame(height: 200) }
                ForEach(nets, id: \.network) { n in
                    Button { onPick(n) } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            HStack(spacing: 6) {
                                Text(Self.display(n.network)).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                                if n.network == cheapest && nets.count > 1 { badge("最低") }
                                if n.network == fastest && nets.count > 1 { badge("推荐") }
                                Spacer()
                            }
                            Text("手续费 \(n.gasFeeEstimate ?? "0") \(n.gasFeeAsset ?? asset)").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                            Text("预计到账时间 ≈ \(n.estimatedArrivalMinutes ?? 1) 分钟").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        .padding(14).frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: Radius.xl).stroke(n.network == selected ? p.textPrimary : p.border, lineWidth: n.network == selected ? 1.5 : 1))
                        .contentShape(Rectangle())
                    }.buttonStyle(PressableStyle())
                }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 24)
        }
    }
    private func badge(_ t: String) -> some View {
        Text(t).font(.lb(10, .medium)).foregroundColor(p.brand).padding(.horizontal, 4).padding(.vertical, 1).background(RoundedRectangle(cornerRadius: 3).fill(p.brand.opacity(0.12)))
    }
}
