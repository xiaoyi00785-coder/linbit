import SwiftUI

/// 划转。资金、现货、合约分别对应 funding、spot、futures。理财和子账户留在选择面板里，点选只说明原因，不跳转。
struct TransferView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @ObservedObject private var m = AssetsModel.shared
    @State private var from: AssetAccount = .funding
    @State private var to: AssetAccount = .spot
    @State private var asset = "USDT"
    @State private var amount = ""
    @State private var submitting = false
    @State private var pickFrom: Bool?
    @State private var pickAsset = false
    @FocusState private var focus: Bool

    private var value: Decimal { Dec.parse(amount) }
    private var avail: Decimal? {
        switch from {
        case .funding: return free(m.funding)
        case .spot: return free(m.spot)
        case .futures: return asset == "USDT" && m.futures != nil ? Dec.parse(m.futures?.availableBalance) : nil
        case .earn, .sub: return nil
        }
    }
    private var error: String? {
        if let reason = from.transferBlock ?? to.transferBlock { return reason }
        if from == to { return "转出和转入账户不能相同" }
        if (from == .futures || to == .futures) && asset != "USDT" { return "合约账户仅支持 USDT" }
        if value > 0, let avail, value > avail { return "可用余额不足" }
        if value > 0, avail == nil, from.transferID != nil { return "没有读到\(from.title)的 \(asset) 可用余额" }
        return nil
    }
    private var coins: [String] {
        if from == .futures || to == .futures { return ["USDT"] }
        let rows = from == .funding ? m.funding : from == .spot ? m.spot : []
        var seen = Set<String>()
        return rows.map(\.asset).filter { seen.insert($0).inserted }
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "划转") { NavigationLink(value: AssetsRoute.records) { Image(systemName: "doc.text.magnifyingglass").foregroundColor(p.textPrimary) } }
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    ZStack {
                        VStack(spacing: 8) {
                            accountCard("从", from.title, from: true)
                            accountCard("到", to.title, from: false)
                        }
                        Button { withAnimation(.spring(response: 0.3)) { swap() }; Haptics.tap() } label: {
                            Image(systemName: "arrow.up.arrow.down").font(.system(size: 12, weight: .semibold)).foregroundColor(p.textPrimary)
                                .frame(width: 30, height: 30).background(Circle().fill(p.bg)).overlay(Circle().stroke(p.border))
                        }.buttonStyle(PressableStyle())
                    }
                    Text("币种").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary).padding(.top, 10)
                    HStack(spacing: 8) {
                        CoinIcon(base: asset, size: 22)
                        Text(asset).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                        Spacer()
                        Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textTertiary)
                    }.padding(.horizontal, 12).frame(height: 48).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        .onTapGesture { pickAsset = true }
                    Text("划转数量").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary).padding(.top, 10)
                    HStack {
                        TextField("", text: Binding(get: { amount }, set: { v in
                            let c = v.filter { $0.isNumber || $0 == "." }
                            if c.filter({ $0 == "." }).count <= 1, (c.split(separator: ".").dropFirst().first?.count ?? 0) <= 8 { amount = c }
                        })).placeholder(when: amount.isEmpty) { Text("0").foregroundColor(p.textTertiary) }
                            .font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary).keyboardType(.decimalPad).focused($focus)
                        Text(asset).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                        Button("最大") { if let avail { amount = SpotModel.fmt(avail, 8) } }.font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
                    }.padding(.horizontal, 12).frame(height: 48)
                        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(error != nil ? p.down : focus ? p.textPrimary : .clear))
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(from.transferBlock != nil || to.transferBlock != nil ? p.textSecondary : p.down) }
                    HStack { Text("可用").foregroundColor(p.textSecondary); Spacer(); Text("\(avail.map { Fmt.amount($0, 8) } ?? "--") \(asset)").foregroundColor(p.textPrimary) }.font(.lbNum(FontSize.small))
                }.padding(.horizontal, Spacing.pageH).padding(.top, 8)
            }
            LBButton(title: "确认划转", enabled: value > 0 && error == nil && from.transferID != nil && to.transferID != nil, loading: submitting) { Task { await submit() } }
                .padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await m.load(silent: true) }
        .lbSheet(Binding(get: { pickFrom != nil }, set: { if !$0 { pickFrom = nil } })) {
            LBSheet(title: pickFrom == true ? "从" : "到", onClose: { pickFrom = nil }) {
                VStack(spacing: 0) {
                    ForEach(AssetAccount.allCases) { a in
                        SheetOptionRow(title: a.title, subtitle: a.transferBlock, selected: (pickFrom == true ? from : to) == a) {
                            if pickFrom == true { if a == to { to = from }; from = a } else { if a == from { from = to }; to = a }
                            amount = ""
                            if from == .futures || to == .futures { asset = "USDT" }
                            pickFrom = nil
                        }
                    }
                }.padding(.bottom, 20)
            }
        }
        .lbSheet($pickAsset) {
            LBSheet(title: "币种", onClose: { pickAsset = false }) {
                VStack(spacing: 0) {
                    if coins.isEmpty { Text("来源账户没有可划转币种").font(.lb(FontSize.small)).foregroundColor(p.textSecondary).padding() }
                    ForEach(coins, id: \.self) { c in
                        SheetOptionRow(title: c, selected: c == asset) { asset = c; amount = ""; pickAsset = false }
                    }
                }.padding(.bottom, 20)
            }
        }
    }

    private func free(_ rows: [APIBalance]) -> Decimal? {
        guard let row = rows.first(where: { $0.asset == asset }), !row.free.isEmpty else { return nil }
        return Dec.parse(row.free)
    }
    private func swap() { (from, to) = (to, from); amount = ""; if from == .futures || to == .futures { asset = "USDT" } }

    private func accountCard(_ k: String, _ v: String, from: Bool) -> some View {
        Button { pickFrom = from } label: {
            HStack(spacing: 12) {
                Text(k).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(v).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    Text("USDT").font(.lb(11)).foregroundColor(p.textTertiary)
                }
                Spacer(); Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textSecondary)
            }.padding(.horizontal, 12).frame(height: 60).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
        }.buttonStyle(PressableStyle(scale: 1))
    }

    private func submit() async {
        focus = false; submitting = true; defer { submitting = false }
        guard let f = from.transferID.flatMap(APITransferRequest.APIFrom.init(rawValue:)),
              let t = to.transferID.flatMap(APITransferRequest.APITo.init(rawValue:)) else { return }
        do {
            let r = try await AssetsAPI.accountTransfer(APITransferRequest(clientId: "ios-\(UUID().uuidString.lowercased())", asset: asset, amount: SpotModel.fmt(value, 8), from: f, to: t))
            Haptics.success()
            ToastCenter.shared.show(r.status == .failed ? "划转失败" : "划转成功", r.status == .failed ? .error : .success)
            amount = ""; await m.load(silent: true); dismiss()
        } catch { ToastCenter.shared.error(error) }
    }
}

/// 资金记录：充值 / 划转
struct RecordsView: View {
    @Environment(\.palette) private var p
    @State private var tab = 0
    @State private var deposits: Loadable<[APIDeposit]> = .loading
    @State private var transfers: Loadable<[APIFuturesTransfers200ResponseTransfersInner]> = .loading
    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "资金记录") { EmptyView() }
            PrimaryTabs(items: ["充值", "提现", "划转"], selected: Binding(get: { tab }, set: { i in
                if i == 1 { ToastCenter.shared.show("提现记录将在提现阶段开放") } else { tab = i }
            }), size: FontSize.tabSecondary, underline: true).padding(.horizontal, Spacing.pageH).frame(maxWidth: .infinity, alignment: .leading)
            Divider().overlay(p.divider)
            Group {
                if tab == 0 {
                    LoadableView(state: deposits, emptyText: "暂无充值记录", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { l in
                        List(l, id: \.id) { d in
                            row("\(d.asset) · \(NetworkSheet.display(d.network))", "+\(d.amount)", status: d.status.rawValue, date: d.createdAt, sub: d.confirmations.map { "确认数 \($0)" })
                        }.listStyle(.plain)
                    }
                } else {
                    LoadableView(state: transfers, emptyText: "暂无划转记录", isEmpty: { $0.isEmpty }, retry: { Task { await load() } }) { l in
                        List(l.indices, id: \.self) { i in
                            let t = l[i]
                            row(t.direction == "in" ? "资金账户 → 合约账户" : "合约账户 → 资金账户", "\(t.amount ?? "0") USDT", status: t.status ?? "", date: t.createdAt, sub: t.ref)
                        }.listStyle(.plain)
                    }
                }
            }.frame(maxHeight: .infinity)
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await load() }
    }
    private func load() async {
        async let d = AssetsAPI.deposits(page: 1)
        async let t = AssetsAPI.transfers()
        do { deposits = .loaded(try await d.items) } catch { deposits = .failed(error.lbMessage) }
        do { transfers = .loaded(try await t) } catch { transfers = .failed(error.lbMessage) }
    }
    private func row(_ t: String, _ amt: String, status: String, date: Date?, sub: String?) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack { Text(t).font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary); Spacer(); Text(amt).font(.lbNum(FontSize.label, .medium)).foregroundColor(p.textPrimary) }
            HStack {
                Text(date.map(Fmt.dateTime) ?? "--").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                Spacer()
                Text(["pending": "处理中", "completed": "已完成", "success": "已完成", "failed": "失败"][status] ?? status)
                    .font(.lb(FontSize.small)).foregroundColor(status == "failed" ? p.down : status == "pending" ? p.brand : p.textSecondary)
            }
            if let sub { Text(sub).font(.lbNum(11)).foregroundColor(p.textTertiary).lineLimit(1).truncationMode(.middle) }
        }.listRowBackground(p.bg).listRowSeparatorTint(p.divider)
    }
}
