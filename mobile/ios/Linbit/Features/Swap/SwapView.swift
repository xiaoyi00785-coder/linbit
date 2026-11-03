import SwiftUI

/// 闪兑：/convert/pairs → /convert/quote（10s 有效，过期自动重询）→ /convert/accept（幂等）；记录 /convert/orders。
/// 未登录时用现货指数价给出参考值，登录后以服务端报价为准。
struct SwapView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @StateObject private var store = MarketStore.shared
    @State private var cfg: ConvertAPI.Pairs?
    @State private var cfgError: String?
    @State private var loading = true
    @State private var from = "USDT"
    @State private var to = "BTC"
    @State private var amount = ""
    @State private var balances: [String: Decimal] = [:]
    @State private var quote: ConvertAPI.Quote?
    @State private var quoteErr: String?
    @State private var quoting = false
    @State private var picking: Side?
    @State private var confirm = false
    @State private var busy = false
    @State private var records = false
    @State private var now = Date()
    @State private var quoteTask: Task<Void, Never>?
    private let timer = Timer.publish(every: 1, on: .main, in: .common).autoconnect()
    enum Side: String, Identifiable { case from, to; var id: String { rawValue } }

    private var v: Decimal { Dec.parse(amount) }
    private var pair: ConvertAPI.Pair? { cfg?.pairs?.first { $0.from == from && $0.to == to } }
    private func prec(_ a: String) -> Int { cfg?.assets?.first { $0.asset == a }?.precision ?? (a == "USDT" || a == "USDC" ? 2 : 6) }
    private var avail: Decimal { balances[from] ?? 0 }
    private func usdtPrice(_ a: String) -> Decimal { a == "USDT" || a == "USDC" ? 1 : (store.spot.first { $0.symbol == a + "USDT" }?.last ?? 0) }
    private var indicative: Decimal {
        let pf = usdtPrice(from), pt = usdtPrice(to)
        guard pf > 0, pt > 0 else { return 0 }
        return pf / pt * Decimal(10000 - (pair?.spreadBps ?? cfg?.spreadBps ?? 20)) / 10000
    }
    private var receive: Decimal { quote.map { Dec.parse($0.toAmount) } ?? v * indicative }
    private var rateText: String {
        if let r = quote?.rate { return "1 \(from) ≈ \(Fmt.priceAuto(Dec.parse(r))) \(to)" }
        return indicative > 0 ? "1 \(from) ≈ \(Fmt.priceAuto(indicative)) \(to)" : "--"
    }
    private var secondsLeft: Int {
        guard let e = quote?.expiresAt else { return 0 }
        return max(0, Int((e / 1000 - now.timeIntervalSince1970).rounded(.up)))
    }
    private var usdtValue: Decimal { v * usdtPrice(from) }
    private var error: String? {
        if amount.isEmpty { return nil }
        if pair?.paused == true { return pair?.pauseReason == "depeg" ? "USDC 价格偏离，兑换暂停" : "暂无可用报价，兑换暂停" }
        if session.isLoggedIn, v > avail { return "可用余额不足" }
        if let mn = cfg?.minUsdt, usdtValue > 0, usdtValue < Dec.parse(mn) { return "单笔最低 \(mn) USDT" }
        if let mx = cfg?.maxUsdt, usdtValue > Dec.parse(mx) { return "单笔最高 \(mx) USDT" }
        return quoteErr
    }

    var body: some View {
        FeatureShell(title: "闪兑", trailing: {
            Button { session.requireLogin { records = true } } label: { Image(systemName: "doc.text").foregroundColor(p.textPrimary) }
        }) {
            ScrollView {
                VStack(spacing: 0) {
                    if loading && cfg == nil { SkeletonList(rows: 3) }
                    else if cfg == nil, let e = cfgError { ErrorStateView(message: e) { Task { await loadPairs() } }.frame(height: 300) }
                    else { form }
                }.padding(.horizontal, Spacing.pageH).padding(.top, 12)
            }
        }
        .task {
            await loadPairs()
            if store.spot.isEmpty { await store.load() }
            await loadBalances()
            if Router.shared.shotSheet == "swapPicker" { Router.shared.shotSheet = nil; picking = .to }
        }
        .onReceive(timer) { t in
            now = t
            if quote != nil, secondsLeft == 0, !quoting { requote(delay: 0) }
        }
        .onChange(of: amount) { _ in requote(delay: 0.5) }
        .lbSheet(Binding(get: { picking != nil }, set: { if !$0 { picking = nil } })) {
            ConvertAssetSheet(assets: pickable, current: picking == .from ? from : to) { a in pick(a) } onClose: { picking = nil }
        }
        .lbSheet($confirm) { confirmSheet }
        .lbSheet($records) { ConvertRecordsSheet { records = false } }
    }

    private var pickable: [String] {
        let all = (cfg?.assets ?? []).filter { $0.enabled != false }.map(\.asset)
        guard let side = picking, let pairs = cfg?.pairs, !pairs.isEmpty else { return all }
        let other = side == .from ? to : from
        let ok = Set(pairs.compactMap { side == .from ? ($0.to == other ? $0.from : nil) : ($0.from == other ? $0.to : nil) })
        return all.filter { ok.contains($0) || $0 == other }
    }

    private func pick(_ a: String) {
        guard let side = picking else { return }
        picking = nil
        if side == .from { if a == to { to = from }; from = a } else { if a == from { from = to }; to = a }
        amount = ""; quote = nil; quoteErr = nil
    }

    private var form: some View {
        VStack(spacing: 8) {
            box(title: "从", asset: from, side: .from, value: AnyView(
                TextField("0.00", text: $amount).keyboardType(.decimalPad).font(.lbNum(22, .semibold)).foregroundColor(p.textPrimary)),
                footer: session.isLoggedIn ? "可用 \(Fmt.amount(avail, prec(from))) \(from)" : "登录后查看余额",
                action: session.isLoggedIn ? ("最大", { amount = Dec.plain(avail, prec(from)) }) : nil)
            Button { Haptics.tap(); let t0 = from; from = to; to = t0; amount = ""; quote = nil; quoteErr = nil } label: {
                Image(systemName: "arrow.up.arrow.down").font(.system(size: 14, weight: .semibold)).foregroundColor(p.textPrimary)
                    .frame(width: 36, height: 36).background(Circle().fill(p.bg)).overlay(Circle().stroke(p.border))
            }.padding(.vertical, -18).zIndex(1)
            box(title: "到", asset: to, side: .to, value: AnyView(
                HStack(spacing: 6) {
                    Text(receive > 0 ? Dec.plain(receive, prec(to)) : "0.00").font(.lbNum(22, .semibold)).foregroundColor(receive > 0 ? p.textPrimary : p.textTertiary)
                    if quoting { ProgressView().scaleEffect(0.7) }
                }.frame(maxWidth: .infinity, alignment: .leading)), footer: nil, action: nil)
            if let e = error { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down).frame(maxWidth: .infinity, alignment: .leading) }
            VStack(spacing: 8) {
                KVRow(k: quote == nil ? "参考汇率" : "报价汇率", v: rateText)
                KVRow(k: "报价有效期", v: quote == nil ? "--" : "\(secondsLeft)s")
                KVRow(k: "手续费", v: (cfg?.fee).map { Dec.parse($0) == 0 ? "0（已含点差）" : $0 } ?? "0")
                KVRow(k: "单笔限额", v: "\(cfg?.minUsdt ?? "10") – \(cfg?.maxUsdt ?? "50000") USDT")
            }.padding(.top, 8)
            LBButton(title: session.isLoggedIn ? "预览兑换" : "登录 / 注册", enabled: !session.isLoggedIn || (v > 0 && error == nil && quote != nil && !quoting)) {
                session.requireLogin { confirm = true }
            }.padding(.top, 12)
            Text("报价 \(cfg?.quoteTtlSeconds ?? 10) 秒有效，过期自动刷新；实际到账以确认时报价为准").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }
    }

    private func box(title: String, asset: String, side: Side, value: AnyView, footer: String?, action: (String, () -> Void)?) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            HStack {
                value
                Button { picking = side } label: {
                    HStack(spacing: 4) {
                        CoinIcon(base: asset, size: 22)
                        Text(asset).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                        Image(systemName: "chevron.down").font(.system(size: 11)).foregroundColor(p.textSecondary)
                    }
                }
            }
            if footer != nil || action != nil {
                HStack {
                    if let f = footer { Text(f).font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                    Spacer()
                    if let a = action { Button(a.0, action: a.1).font(.lb(FontSize.small, .medium)).foregroundColor(p.brand) }
                }
            }
        }
        .padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
    }

    private var confirmSheet: some View {
        LBSheet(title: "确认兑换", subtitle: secondsLeft > 0 ? "报价 \(secondsLeft)s 后过期" : "正在刷新报价…", showClose: true, onClose: { confirm = false }) {
            VStack(spacing: 10) {
                KVRow(k: "支付", v: "\(quote?.fromAmount ?? amount) \(from)")
                KVRow(k: "获得", v: "\(Dec.plain(receive, prec(to))) \(to)", vColor: p.up)
                KVRow(k: "汇率", v: rateText)
                KVRow(k: "手续费", v: "\(quote?.fee ?? "0") \(quote?.feeAsset ?? to)")
                LBButton(title: "确认兑换", enabled: quote != nil && secondsLeft > 0 && !quoting, loading: busy) { Task { await execute() } }.padding(.top, 6)
            }.padding(.horizontal, Spacing.pageH)
        }
    }

    private func requote(delay: Double) {
        quoteTask?.cancel()
        quoteErr = nil
        guard session.isLoggedIn, v > 0, from != to, pair?.paused != true else { quote = nil; quoting = false; return }
        let f = from, t = to, a = amount
        quoteTask = Task {
            if delay > 0 { try? await Task.sleep(nanoseconds: UInt64(delay * 1e9)) }
            guard !Task.isCancelled else { return }
            quoting = true; defer { quoting = false }
            do {
                let q = try await ConvertAPI.quote(from: f, to: t, amount: a)
                guard !Task.isCancelled, f == from, t == to, a == amount else { return }
                quote = q
            } catch {
                guard !Task.isCancelled else { return }
                quote = nil; quoteErr = error.lbMessage
            }
        }
    }

    private func execute() async {
        guard let q = quote else { return }
        busy = true; defer { busy = false }
        do {
            let r = try await ConvertAPI.accept(q.quoteId)
            Haptics.success()
            ToastCenter.shared.show("兑换成功：获得 \(r.order?.toAmount ?? q.toAmount ?? "") \(to)", .success)
            confirm = false; amount = ""; quote = nil
            await loadBalances()
        } catch {
            if (error as? APIFailure)?.code == "QUOTE_EXPIRED" || (error as? APIFailure)?.code == "QUOTE_NOT_FOUND" {
                ToastCenter.shared.show("报价已过期，已重新询价"); requote(delay: 0)
            } else { ToastCenter.shared.error(error) }
        }
    }

    private func loadPairs() async {
        loading = true; defer { loading = false }
        do {
            let c = try await ConvertAPI.pairs()
            cfg = c; cfgError = nil
            if let first = c.pairs?.first, !(c.pairs ?? []).contains(where: { $0.from == from && $0.to == to }) { from = first.from; to = first.to }
        } catch { cfgError = error.lbMessage }
    }

    private func loadBalances() async {
        guard TokenStore.shared.isLoggedIn, let all = try? await AssetsAPI.balances() else { return }
        balances = Dictionary(all.map { ($0.asset, Dec.parse($0.free)) }, uniquingKeysWith: { a, _ in a })
    }
}

struct ConvertAssetSheet: View {
    @Environment(\.palette) private var p
    let assets: [String]
    let current: String
    let onPick: (String) -> Void
    let onClose: () -> Void
    @State private var q = ""
    var body: some View {
        let rows = assets.filter { q.isEmpty || $0.localizedCaseInsensitiveContains(q) }
        LBSheet(title: "选择币种", showClose: true, onClose: onClose) {
            VStack(spacing: 8) {
                SearchField(text: $q, placeholder: "搜索币种").padding(.horizontal, Spacing.pageH)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if rows.isEmpty { EmptyStateView(text: assets.isEmpty ? "暂无可兑换币种" : "无匹配币种") }
                        ForEach(rows, id: \.self) { a in
                            Button { onPick(a) } label: {
                                HStack(spacing: 10) {
                                    CoinIcon(base: a, size: 26)
                                    Text(a).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                                    Spacer()
                                    if a == current { Image(systemName: "checkmark").foregroundColor(p.brand) }
                                }.padding(.horizontal, Spacing.pageH).frame(height: 50)
                            }.buttonStyle(RowPressStyle())
                        }
                    }
                }.frame(height: 380)
            }
        }
    }
}

struct ConvertRecordsSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    @State private var items: [ConvertAPI.Order] = []
    @State private var loading = true
    @State private var err: String?
    var body: some View {
        LBSheet(title: "兑换记录", showClose: true, onClose: onClose) {
            ScrollView {
                LazyVStack(spacing: 0) {
                    if loading && items.isEmpty { SkeletonList(rows: 4) }
                    else if let e = err, items.isEmpty { ErrorStateView(message: e) { Task { await load() } }.frame(height: 240) }
                    else if items.isEmpty { EmptyStateView(text: "暂无兑换记录").frame(height: 240) }
                    ForEach(items) { o in
                        VStack(alignment: .leading, spacing: 6) {
                            HStack {
                                Text("\(o.from ?? "") → \(o.to ?? "")").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                                Spacer()
                                Text(o.status == "filled" ? "已完成" : (o.status ?? "")).font(.lb(FontSize.small)).foregroundColor(p.up)
                            }
                            KVRow(k: "支付", v: "\(o.fromAmount ?? "--") \(o.from ?? "")")
                            KVRow(k: "获得", v: "\(o.toAmount ?? "--") \(o.to ?? "")")
                            if let t = o.createdAt { KVRow(k: "时间", v: Fmt.dateTime(Date(timeIntervalSince1970: t / 1000))) }
                        }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
                        Divider().background(p.border)
                    }
                }
            }.frame(height: 440)
        }
        .task { await load() }
    }
    private func load() async {
        loading = true; defer { loading = false }
        do { items = try await ConvertAPI.orders(); err = nil } catch { err = error.lbMessage }
    }
}

struct SwapAssetSheet: View {
    @Environment(\.palette) private var p
    let current: String
    let onPick: (String) -> Void
    let onClose: () -> Void
    @StateObject private var store = MarketStore.shared
    @State private var q = ""
    var body: some View {
        let rows = store.spot.filter { $0.quote == "USDT" && (q.isEmpty || $0.base.localizedCaseInsensitiveContains(q)) }
        LBSheet(title: "选择币种", showClose: true, onClose: onClose) {
            VStack(spacing: 8) {
                SearchField(text: $q, placeholder: "搜索币种").padding(.horizontal, Spacing.pageH)
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if rows.isEmpty { EmptyStateView(text: "无匹配币种") }
                        ForEach(rows) { r in
                            Button { onPick(r.base) } label: {
                                HStack(spacing: 10) {
                                    CoinIcon(base: r.base, size: 26)
                                    Text(r.base).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                                    Spacer()
                                    Text(Fmt.priceAuto(r.last)).font(.lbNum(FontSize.label)).foregroundColor(p.textSecondary)
                                    if r.base == current { Image(systemName: "checkmark").foregroundColor(p.brand) }
                                }.padding(.horizontal, Spacing.pageH).frame(height: 50)
                            }.buttonStyle(RowPressStyle())
                        }
                    }
                }.frame(height: 380)
            }
        }
    }
}
