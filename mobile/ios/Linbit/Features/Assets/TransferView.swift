import SwiftUI

/// 划转（对照图 #46）：从/到账户卡片 + 中间对调按钮、币种、划转数量 + 最大、可用、确认划转
/// 后端仅支持 现货(资金)↔合约（/futures/transfer in/out），币种固定 USDT
struct TransferView: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @ObservedObject private var m = AssetsModel.shared
    @State private var toFutures = true
    @State private var amount = ""
    @State private var submitting = false
    @State private var pickFrom: Bool?
    @FocusState private var focus: Bool

    private var avail: Decimal { toFutures ? m.spotAvailUSDT : m.futuresAvail }
    private var value: Decimal { Dec.parse(amount) }
    private var error: String? { value > avail ? "可用余额不足" : nil }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "划转") { NavigationLink(value: AssetsRoute.records) { Image(systemName: "doc.text.magnifyingglass").foregroundColor(p.textPrimary) } }
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    ZStack {
                        VStack(spacing: 8) {
                            accountCard("从", toFutures ? "资金账户" : "合约账户", from: true)
                            accountCard("到", toFutures ? "合约账户" : "资金账户", from: false)
                        }
                        Button { withAnimation(.spring(response: 0.3)) { toFutures.toggle() }; Haptics.tap() } label: {
                            Image(systemName: "arrow.up.arrow.down").font(.system(size: 12, weight: .semibold)).foregroundColor(p.textPrimary)
                                .frame(width: 30, height: 30).background(Circle().fill(p.bg)).overlay(Circle().stroke(p.border))
                        }.buttonStyle(PressableStyle())
                    }
                    Text("币种").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary).padding(.top, 10)
                    HStack(spacing: 8) {
                        CoinIcon(base: "USDT", size: 22)
                        Text("USDT").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                        Text("Tether USDT").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        Spacer()
                        Image(systemName: "arrowtriangle.down.fill").font(.system(size: 7)).foregroundColor(p.textTertiary)
                    }.padding(.horizontal, 12).frame(height: 48).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        .onTapGesture { ToastCenter.shared.show("当前仅支持 USDT 划转") }
                    Text("划转数量").font(.lb(FontSize.caption)).foregroundColor(p.textPrimary).padding(.top, 10)
                    HStack {
                        TextField("", text: Binding(get: { amount }, set: { v in
                            let c = v.filter { $0.isNumber || $0 == "." }
                            if c.filter({ $0 == "." }).count <= 1, (c.split(separator: ".").dropFirst().first?.count ?? 0) <= 2 { amount = c }
                        })).placeholder(when: amount.isEmpty) { Text("0").foregroundColor(p.textTertiary) }
                            .font(.lbNum(FontSize.body)).foregroundColor(p.textPrimary).keyboardType(.decimalPad).focused($focus)
                        Text("USDT").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                        Button("最大") { amount = SpotModel.fmt(avail, 2) }.font(.lb(FontSize.label, .medium)).foregroundColor(p.brand)
                    }.padding(.horizontal, 12).frame(height: 48)
                        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(error != nil ? p.down : focus ? p.textPrimary : .clear))
                    if let error { Text(error).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    HStack { Text("可用").foregroundColor(p.textSecondary); Spacer(); Text("\(Fmt.amount(avail, 2)) USDT").foregroundColor(p.textPrimary) }.font(.lbNum(FontSize.small))
                }.padding(.horizontal, Spacing.pageH).padding(.top, 8).padding(.bottom, 16)
            }
            .scrollDismissesKeyboard(.interactively)
            .lbKeyboardDone(focus: $focus)
            LBButton(title: "确认划转", enabled: value > 0 && error == nil, loading: submitting) { Task { await submit() } }
                .padding(.horizontal, Spacing.pageH).padding(.bottom, 8)
        }
        .background(p.bg.ignoresSafeArea()).navigationBarHidden(true)
        .task { await m.load(silent: true) }
        .lbSheet(Binding(get: { pickFrom != nil }, set: { if !$0 { pickFrom = nil } })) {
            LBSheet(title: pickFrom == true ? "从" : "到", onClose: { pickFrom = nil }) {
                VStack(spacing: 0) {
                    ForEach(["资金账户", "合约账户"], id: \.self) { a in
                        let cur = (pickFrom == true) == toFutures ? "资金账户" : "合约账户"
                        SheetOptionRow(title: a, subtitle: "USDT", selected: a == cur) {
                            if a != cur { toFutures.toggle() }; pickFrom = nil
                        }
                    }
                    ForEach(["现货账户", "理财账户", "子账户"], id: \.self) { a in
                        SheetOptionRow(title: a, subtitle: "即将开放", selected: false) { ToastCenter.shared.show("\(a)划转即将开放") }.opacity(0.5)
                    }
                }.padding(.bottom, 20)
            }
        }
    }

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
        do {
            let r = try await AssetsAPI.transfer(toFutures ? ._in : .out, amount: SpotModel.fmt(value, 2))
            Haptics.success()
            ToastCenter.shared.show(r.status == "pending" ? "划转已提交，处理中" : "划转成功", .success)
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
                if i == 1 { Router.shared.open(.withdrawRecords) } else { tab = i }
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
