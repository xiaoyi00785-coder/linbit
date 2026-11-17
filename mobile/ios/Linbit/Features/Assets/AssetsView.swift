import SwiftUI

/// 资产页（对照图 #36 总览空状态 / #41 合约 / #42 资金 / #43 现货；提现 Sheet 按 ref2 #16）
struct AssetsView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @ObservedObject private var router = Router.shared
    @ObservedObject private var m = AssetsModel.shared
    @State private var tab = 0
    @State private var showWithdraw = false
    @State private var showSearch = false
    @State private var showFilter = false
    @State private var earnSummary: APIEarnSummary200Response?
    @State private var earnPositions: [APIEarnPositions200ResponseItemsInner] = []
    @State private var earnProducts: [APIEarnProduct] = []
    @State private var earnOrders: [APIEarnOrders200ResponseItemsInner] = []
    @State private var earnGroup = 0
    @State private var showEarnOrders = false
    @State private var grids: [APIGridBot] = []
    @State private var tradfi: [String: TradFiRef] = [:]
    @State private var sideError: String?
    private let tabs = ["总览", "资金", "合约", "现货", "理财", "策略", "TradFi"]

    var body: some View {
        NavigationStack(path: $router.assetsPath) {
            VStack(spacing: 0) {
                ScrollView(.horizontal, showsIndicators: false) {
                    PrimaryTabs(items: tabs, selected: $tab).padding(.horizontal, Spacing.pageH)
                }.frame(height: 44)
                Divider().overlay(p.divider)
                if !session.isLoggedIn { loggedOut } else {
                    ScrollView {
                        switch m.balances {
                        case .failed(let e): ErrorStateView(message: e) { Task { await m.load() } }.padding(.top, 120)
                        case .idle, .loading: SkeletonList().padding(Spacing.pageH)
                        default: content
                        }
                    }.refreshable { await m.load(silent: true); await loadSide() }
                }
            }
            .background(p.bg.ignoresSafeArea())
            .navigationBarHidden(true)
            .navigationDestination(for: AssetsRoute.self) { r in
                switch r {
                case .deposit(let a): if let a { DepositView(asset: a) } else { CoinPickerView(mode: .deposit) }
                case .withdraw(let a): WithdrawView(asset: a)
                case .transfer: TransferView()
                case .records, .history: RecordsView()
                case .overview: AssetOverviewView()
                case .accountTransfer: AccountTransferView()
                case .internalTransfer: InternalTransferView()
                case .withdrawRecords: WithdrawRecordsView()
                case .addressBook: AddressBookView()
                case .networks(let a): WalletNetworksView(asset: a)
                case .proof: ProofOfReservesView()
                }
            }
            .navigationDestination(for: DepositTarget.self) { DepositView(asset: $0.asset) }
        }
        .task(id: session.isLoggedIn) { await m.load() }
        .task(id: "\(tab)-\(session.isLoggedIn)") { if session.isLoggedIn { await loadSide() } }
        .sheet(isPresented: $showEarnOrders) { earnOrderSheet }
        .lbSheet($showWithdraw) { WithdrawSheet { showWithdraw = false } }
        .lbSheet($showFilter) {
            LBSheet(title: "资产筛选", onClose: { showFilter = false }) {
                HStack { Text("隐藏小额资产（< 1 USDT）").font(.lb(FontSize.body)).foregroundColor(p.textPrimary); Spacer(); Toggle("", isOn: $m.hideSmall).labelsHidden().tint(p.brand) }
                    .padding(.horizontal, Spacing.pageH).padding(.bottom, 28)
            }
        }
    }

    /// 未登录点资产会直接进入登录页；关掉登录后回到首页，避免停在空白资产页。
    private var loggedOut: some View {
        Color.clear
            .onAppear { session.present(.login) }
            .onChange(of: session.showAuth) { shown in
                if !shown && !session.isLoggedIn { router.tab = .home }
            }
    }

    @ViewBuilder private var content: some View {
        if tab == 4 { earnAccount }
        else if tab == 5 { strategyAccount }
        else if tab == 6 { tradfiAccount }
        else {
            VStack(alignment: .leading, spacing: 0) {
                if tab == 0 && m.total == 0 { overviewEmpty } else {
                    header
                    actions.padding(.top, 20)
                    if tab == 3 { swapEntry.padding(.top, 16) }
                    if tab == 2 { futuresCard } else { assetList }
                }
            }.padding(.bottom, 100)
        }
    }

    // #36：总资产为 0 时的引导
    private var overviewEmpty: some View {
        VStack(alignment: .leading, spacing: 16) {
            ZStack {
                RoundedRectangle(cornerRadius: 16).fill(LinearGradient(colors: [p.fill, p.fillStrong], startPoint: .top, endPoint: .bottom)).frame(width: 150, height: 120).rotationEffect(.degrees(-6))
                Circle().fill(p.brand).frame(width: 46).offset(x: 30, y: -50)
                Text("Linbit").font(.lb(18, .heavy)).foregroundColor(p.textTertiary).rotationEffect(.degrees(-6))
            }.frame(maxWidth: .infinity).frame(height: 200)
            Text("完成充值，开启您的 Linbit 之旅!").font(.lb(24, .bold)).foregroundColor(p.textPrimary)
            LBButton(title: "充值") { router.open(.deposit(nil)) }
            HStack(spacing: 12) {
                Image(systemName: "gift.fill").font(.system(size: 30)).foregroundColor(p.brand)
                VStack(alignment: .leading, spacing: 4) {
                    Text("您的账户尚无可用资产").font(.lb(11)).foregroundColor(p.textSecondary)
                    Text("充值任意金额，参与合约交易").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    Button { router.tab = .futures } label: { HStack(spacing: 4) { Text("立即查看"); Image(systemName: "arrow.right") }.font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary) }
                }
            }.padding(.top, 8)
            Divider().overlay(p.divider)
            Text("责任与守护").font(.lb(FontSize.title, .bold)).foregroundColor(p.textPrimary)
            HStack {
                VStack(alignment: .leading, spacing: 6) {
                    Text("资产安全，我们的第一责任").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                    Button { router.assetsPath.append(AssetsRoute.proof) } label: {
                        Text("查看更多").font(.lb(FontSize.small)).foregroundColor(p.brand)
                    }.buttonStyle(.plain)
                }
                Spacer()
                Image(systemName: "checkmark.shield.fill").font(.system(size: 40)).foregroundColor(p.textTertiary)
            }
            HStack {
                VStack(alignment: .leading, spacing: 6) {
                    Text("帮助中心和客户服务，7×24 小时解答你的问题").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                    Button { Router.shared.openFeature(.support) } label: {
                        Text("立即前往").font(.lb(FontSize.small)).foregroundColor(p.brand)
                    }.buttonStyle(.plain)
                }
                Spacer()
                Image(systemName: "headphones").font(.system(size: 32)).foregroundColor(p.textTertiary)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.top, 12)
    }

    private var header: some View {
        let total = tab == 2 ? m.futuresTotal : tab == 1 ? m.fundingTotal : tab == 3 ? m.spotTotal : m.total
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("总资产估值").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Button { m.hideAmount.toggle() } label: { Image(systemName: m.hideAmount ? "eye.slash" : "eye").font(.system(size: 12)).foregroundColor(p.textSecondary) }
                Spacer()
                NavigationLink(value: AssetsRoute.overview) { Image(systemName: "square.grid.2x2").font(.system(size: 16)).foregroundColor(p.textPrimary) }
                    .padding(.trailing, 12).accessibilityLabel("资产总览")
                NavigationLink(value: AssetsRoute.records) { Image(systemName: "doc.text.magnifyingglass").font(.system(size: 16)).foregroundColor(p.textPrimary) }
            }
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(m.hideAmount ? "****" : Fmt.amount(total, 2)).font(.lbNum(32, .bold)).foregroundColor(p.textPrimary)
                Text("USDT ▾").font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
            }
            Text(m.hideAmount ? "≈ ****" : "≈ " + Fmt.fiat(total, rate: AppConfig.usdtCny)).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            if tab == 1 || tab == 2 {
                HStack(spacing: 8) {
                    Text(tab == 2 ? "今日盈亏" : "可用余额").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    Text(m.hideAmount ? "****" : tab == 2 ? "\(Fmt.amount(Dec.parse(m.futures?.crossUnrealizedPnl), 2)) USD" : "\(Fmt.amount(tab == 1 ? m.fundingAvailUSDT : m.spotAvailUSDT, 2)) USDT")
                        .font(.lbNum(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                }.padding(.top, 8)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.top, 20)
    }

    private var actions: some View {
        let items: [(String, String, () -> Void)] = {
            let dep = ("充值", "arrow.down.left.circle.fill", { router.open(.deposit(nil)) })
            let buy = ("快捷买币", "creditcard.fill", { Router.shared.openSpotTab(3) })
            let wd = ("提现", "arrow.up.right.circle.fill", { showWithdraw = true })
            let tr = ("划转", "arrow.left.arrow.right.circle.fill", { router.open(.transfer) })
            let trade = ("去交易", "chart.line.uptrend.xyaxis.circle.fill", { router.tab = tab == 2 ? .futures : .spot })
            switch tab {
            case 2: return [trade, tr, ("盈亏", "chart.pie.fill", { Router.shared.openFeature(.futuresHistory) })]
            case 3: return [trade, dep, buy, tr]
            default: return [dep, buy, wd, tr]
            }
        }()
        return HStack {
            ForEach(items.indices, id: \.self) { i in
                Button(action: items[i].2) {
                    VStack(spacing: 8) {
                        Image(systemName: items[i].1).font(.system(size: 18)).foregroundColor(p.textPrimary)
                            .frame(width: 40, height: 40).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                        Text(items[i].0).font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                    }.frame(maxWidth: .infinity)
                }.buttonStyle(PressableStyle())
            }
        }.padding(.horizontal, 8)
    }

    private var swapEntry: some View {
        Button { Router.shared.openSpotTab(3) } label: {
            HStack(spacing: 8) {
                Image(systemName: "arrow.triangle.2.circlepath.circle").foregroundColor(p.textPrimary)
                Text("现货兑换").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                Text("0 手续费").font(.lb(10, .medium)).foregroundColor(p.textOnPrimary).padding(.horizontal, 5).padding(.vertical, 2).background(RoundedRectangle(cornerRadius: 3).fill(p.brand))
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 11)).foregroundColor(p.textTertiary)
            }.padding(.horizontal, 14).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.xl).stroke(p.border))
        }.buttonStyle(PressableStyle()).padding(.horizontal, Spacing.pageH)
    }

    private struct Holding: Identifiable {
        var account: String
        var balance: APIBalance
        var id: String { account + "-" + balance.asset + "-" + (balance.free ?? "") + "-" + (balance.total ?? "") }
    }
    private var holdings: [Holding] {
        switch tab {
        case 1: return m.filtered(m.funding).map { Holding(account: "资金", balance: $0) }
        case 3: return m.filtered(m.spot).map { Holding(account: "现货", balance: $0) }
        default:
            return m.filtered(m.funding).map { Holding(account: "资金", balance: $0) }
                + m.filtered(m.spot).map { Holding(account: "现货", balance: $0) }
        }
    }

    private var assetList: some View {
        VStack(spacing: 0) {
            Rectangle().fill(p.fill).frame(height: 8).padding(.top, 20)
            HStack(spacing: 18) {
                if showSearch {
                    SearchField(text: $m.query, placeholder: "搜索币种")
                    Button("取消") { m.query = ""; showSearch = false }.font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                } else {
                    Text("资产").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                    Spacer()
                    Button { withAnimation { showSearch = true } } label: { Image(systemName: "magnifyingglass") }
                    Button { showFilter = true } label: { Image(systemName: "slider.horizontal.3") }
                }
            }.font(.system(size: 16)).foregroundColor(p.textSecondary).padding(.horizontal, Spacing.pageH).frame(height: 56)
            if let note = tab == 1 ? m.fundingError : tab == 3 ? m.spotError : nil {
                Text(note).font(.lb(FontSize.small)).foregroundColor(p.down).padding(.horizontal, Spacing.pageH)
            }
            if holdings.isEmpty { EmptyStateView(text: m.query.isEmpty ? "暂无资产" : "暂无相关币种").padding(.vertical, 40) }
            ForEach(holdings) { row in
                let b = row.balance
                Button { router.assetsPath.append(DepositTarget(asset: b.asset)) } label: {
                    HStack(spacing: 12) {
                        CoinIcon(base: b.asset)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(b.asset).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text((tab == 0 ? row.account + " · " : "") + (Dec.parse(b.locked) > 0 ? "冻结 \(b.locked)" : (m.unitPrice(b).map { "单价 $\(Fmt.amount($0, $0 < 1 ? 6 : 2))" } ?? b.asset))).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        Spacer()
                        VStack(alignment: .trailing, spacing: 3) {
                            Text(m.hideAmount ? "****" : b.total).font(.lbNum(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(m.hideAmount ? "≈ ****" : "≈ " + Fmt.fiat(m.usd(b), rate: AppConfig.usdtCny)).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                    }.padding(.horizontal, Spacing.pageH).frame(height: 60).contentShape(Rectangle())
                }.buttonStyle(RowPressStyle())
            }
        }
    }

    // #41 合约：持有仓位（USDT 保证金账户明细）
    private var futuresCard: some View {
        let f = m.futures
        let cells: [(String, String?)] = [("账户权益(USDT)", f?.marginBalance), ("未实现盈亏", f?.crossUnrealizedPnl), ("可用余额", f?.availableBalance),
                                          ("钱包余额", f?.walletBalance), ("委托保证金", f?.orderMargin), ("全仓保证金", f?.crossPositionMargin),
                                          ("维持保证金", f?.crossMaintenanceMargin), ("逐仓保证金", f?.isolatedMargin), ("逐仓未实现盈亏", f?.isolatedUnrealizedPnl)]
        return VStack(alignment: .leading, spacing: 14) {
            Rectangle().fill(p.fill).frame(height: 8).padding(.horizontal, -Spacing.pageH).padding(.top, 20)
            Text("持有仓位").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            HStack(spacing: 6) { CoinIcon(base: "USDT", size: 20); Text("USDT").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary) }
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), alignment: .leading), count: 3), spacing: 14) {
                ForEach(cells.indices, id: \.self) { i in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(cells[i].0).font(.lb(11)).foregroundColor(p.textSecondary).lineLimit(1)
                        Text(m.hideAmount ? "****" : Fmt.amount(Dec.parse(cells[i].1), 2)).font(.lbNum(FontSize.label)).foregroundColor(p.textPrimary)
                        Text("≈ " + Fmt.fiat(Dec.parse(cells[i].1), rate: AppConfig.usdtCny)).font(.lbNum(10)).foregroundColor(p.textTertiary)
                    }
                }
            }
        }.padding(.horizontal, Spacing.pageH)
    }

    private var earnAccount: some View {
        VStack(alignment: .leading, spacing: 16) {
            accountHead(Dec.parse(earnSummary?.totalValueUsdt), pairs: [("累计收益", earnSummary?.totalInterestUsdt), ("近期收益", earnSummary?.recentInterestUsdt)])
            accountActions([
                ("理财总览", "chart.pie.fill", { Router.shared.openSpotTab(2) }),
                ("划转", "arrow.left.arrow.right.circle.fill", { router.open(.transfer) }),
                ("理财订单", "list.bullet.rectangle", { Task { await openEarnOrders() } }),
                ("借贷订单", "banknote", { Router.shared.openFeature(.loans) }),
            ])
            if let product = earnProducts.first(where: { Dec.parse($0.apr ?? $0.aprMax) > 0 }) {
                Button { Router.shared.openSpotTab(2) } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(product.title ?? product.asset ?? "理财").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text("APR \(Fmt.percent(Dec.parse(product.aprMax ?? product.apr) * 100).replacingOccurrences(of: "+", with: ""))")
                                .font(.lbNum(FontSize.small)).foregroundColor(p.brand)
                        }
                        Spacer()
                        Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(p.textTertiary)
                    }.padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                }.buttonStyle(.plain).padding(.horizontal, Spacing.pageH)
            }
            PrimaryTabs(items: ["按币种", "按产品"], selected: $earnGroup, size: FontSize.label, underline: true)
                .padding(.horizontal, Spacing.pageH)
            if let sideError { Text(sideError).font(.lb(FontSize.small)).foregroundColor(p.down).padding(.horizontal, Spacing.pageH) }
            if earnGroup == 0 {
                let rows = earnSummary?.assets ?? []
                if rows.isEmpty { EmptyStateView(text: "暂无理财资产") }
                ForEach(rows.indices, id: \.self) { i in
                    let row = rows[i]
                    holdingRow(row.asset ?? "--", row.amount ?? "0", Dec.parse(row.valueUsdt))
                }
            } else {
                if earnPositions.isEmpty { EmptyStateView(text: "暂无理财产品") }
                ForEach(earnPositions.indices, id: \.self) { i in
                    let row = earnPositions[i]
                    HStack {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(row.title ?? row.asset ?? "--").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(row.asset ?? "").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        Spacer()
                        VStack(alignment: .trailing, spacing: 3) {
                            Text(m.hideAmount ? "****" : (row.amount ?? "0")).font(.lbNum(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(m.hideAmount ? "****" : "利息 \(row.totalInterest ?? "0")").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                    }.padding(.horizontal, Spacing.pageH).frame(height: 56)
                }
            }
        }.padding(.bottom, 100)
    }

    private var strategyAccount: some View {
        let running = grids.filter { $0.status == .running }
        let value = running.reduce(Decimal(0)) { $0 + Dec.parse($1.value) }
        let profit = grids.reduce(Decimal(0)) { $0 + Dec.parse($1.totalProfit) }
        return VStack(alignment: .leading, spacing: 16) {
            accountHead(value, pairs: [("运行中", "\(running.count)"), ("累计收益", Fmt.amount(profit, 2))])
            accountActions([
                ("策略交易", "square.grid.2x2.fill", { Router.shared.openSpotTab(1) }),
                ("充值", "arrow.down.left.circle.fill", { router.open(.deposit(nil)) }),
                ("划转", "arrow.left.arrow.right.circle.fill", { router.open(.transfer) }),
            ])
            PrimaryTabs(items: ["现货网格"], selected: .constant(0), size: FontSize.label, underline: true)
                .padding(.horizontal, Spacing.pageH)
            if let sideError { Text(sideError).font(.lb(FontSize.small)).foregroundColor(p.down).padding(.horizontal, Spacing.pageH) }
            if grids.isEmpty { EmptyStateView(text: "暂无策略") }
            ForEach(grids, id: \.id) { bot in
                Button { Router.shared.openSpotTab(1) } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(bot.symbol ?? "--").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(gridStatus(bot.status)).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        Spacer()
                        VStack(alignment: .trailing, spacing: 3) {
                            Text(m.hideAmount ? "****" : Fmt.amount(Dec.parse(bot.value), 2)).font(.lbNum(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(m.hideAmount ? "****" : Fmt.amount(Dec.parse(bot.totalProfit), 2)).font(.lbNum(FontSize.small)).foregroundColor(Dec.parse(bot.totalProfit) < 0 ? p.down : p.up)
                        }
                    }.padding(.horizontal, Spacing.pageH).frame(height: 60)
                }.buttonStyle(.plain)
            }
        }.padding(.bottom, 100)
    }

    private var tradfiAccount: some View {
        let held = m.rows.filter { tradfi[$0.asset] != nil && Dec.parse($0.total) > 0 }
        let equity = held.reduce(Decimal(0)) { $0 + m.usd($1) }
        return VStack(alignment: .leading, spacing: 16) {
            accountHead(equity, pairs: [("未实现盈亏", nil), ("保证金比例", nil)])
            accountActions([
                ("去交易", "chart.line.uptrend.xyaxis.circle.fill", { Router.shared.openFeature(.tradfi) }),
                ("划转", "arrow.left.arrow.right.circle.fill", { router.open(.transfer) }),
            ])
            Text("持有仓位").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary).padding(.horizontal, Spacing.pageH)
            if let sideError { Text(sideError).font(.lb(FontSize.small)).foregroundColor(p.down).padding(.horizontal, Spacing.pageH) }
            if held.isEmpty { EmptyStateView(text: "暂无持仓") }
            ForEach(held, id: \.asset) { b in
                Button {
                    guard let ref = tradfi[b.asset] else { return }
                    if ref.market == "futures" { Router.shared.tradeFutures(ref.symbol) } else { Router.shared.trade(ref.symbol) }
                } label: {
                    holdingRow(b.asset, b.total, m.usd(b))
                }.buttonStyle(.plain)
            }
        }.padding(.bottom, 100)
    }

    private func accountHead(_ total: Decimal, pairs: [(String, String?)]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 4) {
                Text("总资产估值").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Button { m.hideAmount.toggle() } label: { Image(systemName: m.hideAmount ? "eye.slash" : "eye").font(.system(size: 12)).foregroundColor(p.textSecondary) }
            }
            Text(m.hideAmount ? "****" : Fmt.amount(total, 2) + " USDT").font(.lbNum(28, .bold)).foregroundColor(p.textPrimary)
            Text(m.hideAmount ? "≈ ****" : "≈ " + Fmt.fiat(total, rate: AppConfig.usdtCny)).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            HStack(spacing: 24) {
                ForEach(pairs.indices, id: \.self) { i in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(pairs[i].0).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        Text(m.hideAmount ? "****" : (pairs[i].1 ?? "--")).font(.lbNum(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    }
                }
            }
        }.padding(.horizontal, Spacing.pageH).padding(.top, 16)
    }

    private func accountActions(_ items: [(String, String, () -> Void)]) -> some View {
        HStack {
            ForEach(items.indices, id: \.self) { i in
                Button(action: items[i].2) {
                    VStack(spacing: 8) {
                        Image(systemName: items[i].1).font(.system(size: 18)).foregroundColor(p.textPrimary)
                            .frame(width: 40, height: 40).background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
                        Text(items[i].0).font(.lb(FontSize.small)).foregroundColor(p.textPrimary)
                    }.frame(maxWidth: .infinity)
                }.buttonStyle(PressableStyle())
            }
        }.padding(.horizontal, 8)
    }

    private func holdingRow(_ name: String, _ amount: String, _ usd: Decimal) -> some View {
        HStack(spacing: 12) {
            CoinIcon(base: name, size: 28)
            Text(name).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
            Spacer()
            VStack(alignment: .trailing, spacing: 3) {
                Text(m.hideAmount ? "****" : amount).font(.lbNum(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text(m.hideAmount ? "≈ ****" : "≈ " + Fmt.fiat(usd, rate: AppConfig.usdtCny)).font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            }
        }.padding(.horizontal, Spacing.pageH).frame(height: 56)
    }

    private func gridStatus(_ s: APIGridBot.APIStatus?) -> String {
        switch s {
        case .running: return "运行中"
        case .stopping: return "停止中"
        case .stopped: return "已停止"
        case nil: return "--"
        }
    }

    private var earnOrderSheet: some View {
        NavigationStack {
            ScrollView {
                if earnOrders.isEmpty { EmptyStateView(text: "暂无定期订单") }
                ForEach(earnOrders.indices, id: \.self) { i in
                    let o = earnOrders[i]
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(o.title ?? o.asset ?? "订单").font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Spacer()
                            Text(o.status ?? "--").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                        Text("\(o.amount ?? "0") \(o.asset ?? "")").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                    }.padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
                }
            }
            .navigationTitle("理财订单")
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private func openEarnOrders() async {
        do {
            earnOrders = try await EarnAPI.orders()
            showEarnOrders = true
            sideError = nil
        } catch { sideError = error.lbMessage }
    }

    private func loadSide() async {
        sideError = nil
        switch tab {
        case 4:
            async let summary = try? EarnAPI.summary()
            async let positions = try? EarnAPI.positions()
            async let products = try? EarnAPI.products()
            let (s, p, prod) = await (summary, positions, products)
            earnSummary = s
            earnPositions = p?.items ?? []
            earnProducts = prod ?? []
            if s == nil && p == nil { sideError = "理财资产加载失败" }
        case 5:
            do { grids = try await GridAPI.list() } catch { sideError = error.lbMessage }
        case 6:
            do {
                var map: [String: TradFiRef] = [:]
                for item in try await TradFiAPI.assets(nil).items {
                    guard let asset = item["asset"].s, let symbol = item["symbol"].s else { continue }
                    map[asset] = TradFiRef(symbol: symbol, market: item["market"].s ?? "spot")
                }
                tradfi = map
            } catch { sideError = error.lbMessage }
        default: break
        }
    }
}

private struct TradFiRef { let symbol: String; let market: String }

struct DepositTarget: Hashable { let asset: String }

/// 储备金证明。数字来自 /market/proof-of-reserves，未发布时不填比例。
struct ProofOfReservesView: View {
    @Environment(\.palette) private var p
    @State private var data: Loadable<APIProofOfReserves> = .loading

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "储备金证明")
            switch data {
            case .idle, .loading: ProgressView().frame(maxHeight: .infinity)
            case let .failed(message): ErrorStateView(message: message) { Task { await load() } }
            case let .loaded(proof) where !proof.available || proof.items.isEmpty:
                EmptyStateView(text: "尚未发布储备快照").frame(maxHeight: .infinity)
            case let .loaded(proof):
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("储备金比率 = 平台储备 / 用户资产").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        if let at = proof.snapshotAt {
                            Text("快照时间 \(Fmt.dateTime(at))").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                        }
                        ForEach(proof.items, id: \.asset) { item in
                            HStack {
                                Text(item.asset).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                                Spacer()
                                Text(ratioText(item.ratio)).font(.lbNum(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                            }.frame(height: 44)
                            Text("储备 \(item.reserves) · 用户资产 \(item.liabilities)")
                                .font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                        }
                    }.padding(Spacing.pageH)
                }
            }
        }.background(p.bg.ignoresSafeArea()).task { await load() }
    }

    private func ratioText(_ raw: String) -> String {
        let r = Dec.parse(raw)
        guard r > 0 else { return "--" }
        return Fmt.amount(r * 100, 2) + "%"
    }

    private func load() async {
        if data.value == nil { data = .loading }
        do { data = .loaded(try await APIClient.shared.get("/market/proof-of-reserves")) }
        catch { data = .failed(error.lbMessage) }
    }
}

/// 提现方式（ref2 #16）：链上提币 / 内部转账（0 手续费）
struct WithdrawSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "提现", onClose: onClose) {
            VStack(spacing: 12) {
                card("arrow.up.right.circle", "链上提币", "往钱包或交易所转出数字货币", badge: nil)
                card("arrow.left.arrow.right.circle", "内部转账", "立即向其他 Linbit 用户转出数字货币", badge: "0 手续费")
                HStack(spacing: 20) {
                    link("提现记录", .withdrawRecords); link("地址簿", .addressBook); link("币种网络", .networks(nil))
                }.padding(.top, 4)
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 28)
        }
    }
    private func link(_ t: String, _ r: AssetsRoute) -> some View {
        Button { onClose(); Router.shared.open(r) } label: {
            Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).underline()
        }
    }
    private func card(_ icon: String, _ t: String, _ s: String, badge: String?) -> some View {
        Button {
            onClose()
            Router.shared.open(t == "内部转账" ? .internalTransfer : .withdraw(nil))
        } label: {
            HStack(spacing: 14) {
                Image(systemName: icon).font(.system(size: 20)).foregroundColor(p.textPrimary)
                VStack(alignment: .leading, spacing: 3) {
                    Text(t).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                    Text(s).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
                Spacer()
            }
            .padding(16).frame(maxWidth: .infinity)
            .background(RoundedRectangle(cornerRadius: Radius.xl).stroke(p.border))
            .overlay(alignment: .topTrailing) {
                if let badge {
                    Text(badge).font(.lb(10, .medium)).foregroundColor(p.brand).padding(.horizontal, 5).padding(.vertical, 2)
                        .overlay(RoundedRectangle(cornerRadius: 3).stroke(p.brand)).padding(10)
                }
            }
        }.buttonStyle(PressableStyle())
    }
}
