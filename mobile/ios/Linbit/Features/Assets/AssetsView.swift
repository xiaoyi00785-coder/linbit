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
    private let tabs = ["总览", "资金", "合约", "现货", "理财", "策略", "TradFi"]

    var body: some View {
        NavigationStack(path: $router.assetsPath) {
            VStack(spacing: 0) {
                ScrollView(.horizontal, showsIndicators: false) {
                    PrimaryTabs(items: tabs, selected: Binding(get: { tab }, set: { i in
                        if i >= 4 { ToastCenter.shared.show("\(tabs[i])即将开放") } else { tab = i }
                    })).padding(.horizontal, Spacing.pageH)
                }.frame(height: 44)
                Divider().overlay(p.divider)
                if !session.isLoggedIn { loggedOut } else {
                    ScrollView {
                        switch m.balances {
                        case .failed(let e): ErrorStateView(message: e) { Task { await m.load() } }.padding(.top, 120)
                        case .idle, .loading: SkeletonList().padding(Spacing.pageH)
                        default: content
                        }
                    }.refreshable { await m.load(silent: true) }
                }
            }
            .background(p.bg.ignoresSafeArea())
            .navigationBarHidden(true)
            .navigationDestination(for: AssetsRoute.self) { r in
                switch r {
                case .deposit(let a): if let a { DepositView(asset: a) } else { CoinPickerView(mode: .deposit) }
                case .transfer: TransferView()
                case .records, .history: RecordsView()
                }
            }
            .navigationDestination(for: DepositTarget.self) { DepositView(asset: $0.asset) }
        }
        .task(id: session.isLoggedIn) { await m.load() }
        .lbSheet($showWithdraw) { WithdrawSheet { showWithdraw = false } }
        .lbSheet($showFilter) {
            LBSheet(title: "资产筛选", onClose: { showFilter = false }) {
                HStack { Text("隐藏小额资产（< 1 USDT）").font(.lb(FontSize.body)).foregroundColor(p.textPrimary); Spacer(); Toggle("", isOn: $m.hideSmall).labelsHidden().tint(p.brand) }
                    .padding(.horizontal, Spacing.pageH).padding(.bottom, 28)
            }
        }
    }

    private var loggedOut: some View {
        VStack(spacing: 16) {
            EmptyIllustration()
            Text("登录后查看资产").font(.lb(FontSize.body)).foregroundColor(p.textSecondary)
            LBButton(title: "登录 / 注册") { session.present(.login) }.frame(width: 200)
        }.frame(maxHeight: .infinity)
    }

    @ViewBuilder private var content: some View {
        VStack(alignment: .leading, spacing: 0) {
            if tab == 0 && m.total == 0 { overviewEmpty } else {
                header
                actions.padding(.top, 20)
                if tab == 3 { swapEntry.padding(.top, 16) }
                if tab == 2 { futuresCard } else { assetList }
            }
        }.padding(.bottom, 100)
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
                    Text("查看更多").font(.lb(FontSize.small)).foregroundColor(p.brand)
                }
                Spacer()
                Image(systemName: "checkmark.shield.fill").font(.system(size: 40)).foregroundColor(p.textTertiary)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.top, 12)
    }

    private var header: some View {
        let total = tab == 2 ? m.futuresTotal : tab == 0 ? m.total : m.spotTotal
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("总资产估值").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                Button { m.hideAmount.toggle() } label: { Image(systemName: m.hideAmount ? "eye.slash" : "eye").font(.system(size: 12)).foregroundColor(p.textSecondary) }
                Spacer()
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
                    Text(m.hideAmount ? "****" : tab == 2 ? "\(Fmt.amount(Dec.parse(m.futures?.crossUnrealizedPnl), 2)) USD" : "\(Fmt.amount(m.spotAvailUSDT, 2)) USDT")
                        .font(.lbNum(FontSize.small, .medium)).foregroundColor(p.textPrimary)
                }.padding(.top, 8)
            }
        }.padding(.horizontal, Spacing.pageH).padding(.top, 20)
    }

    private var actions: some View {
        let items: [(String, String, () -> Void)] = {
            let dep = ("充值", "arrow.down.left.circle.fill", { router.open(.deposit(nil)) })
            let buy = ("快捷买币", "creditcard.fill", { ToastCenter.shared.show("快捷买币即将开放") })
            let wd = ("提现", "arrow.up.right.circle.fill", { showWithdraw = true })
            let tr = ("划转", "arrow.left.arrow.right.circle.fill", { router.open(.transfer) })
            let trade = ("去交易", "chart.line.uptrend.xyaxis.circle.fill", { router.tab = tab == 2 ? .futures : .spot })
            switch tab {
            case 2: return [trade, tr, ("盈亏", "chart.pie.fill", { ToastCenter.shared.show("盈亏分析即将开放") })]
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
        Button { ToastCenter.shared.show("现货兑换即将开放") } label: {
            HStack(spacing: 8) {
                Image(systemName: "arrow.triangle.2.circlepath.circle").foregroundColor(p.textPrimary)
                Text("现货兑换").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                Text("0 手续费").font(.lb(10, .medium)).foregroundColor(p.textOnPrimary).padding(.horizontal, 5).padding(.vertical, 2).background(RoundedRectangle(cornerRadius: 3).fill(p.brand))
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 11)).foregroundColor(p.textTertiary)
            }.padding(.horizontal, 14).frame(height: 44).background(RoundedRectangle(cornerRadius: Radius.xl).stroke(p.border))
        }.buttonStyle(PressableStyle()).padding(.horizontal, Spacing.pageH)
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
            if m.rows.isEmpty { EmptyStateView(text: m.query.isEmpty ? "暂无资产" : "暂无相关币种").padding(.vertical, 40) }
            ForEach(m.rows, id: \.asset) { b in
                Button { router.assetsPath.append(DepositTarget(asset: b.asset)) } label: {
                    HStack(spacing: 12) {
                        CoinIcon(base: b.asset)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(b.asset).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                            Text(Dec.parse(b.locked) > 0 ? "冻结 \(b.locked)" : b.asset).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
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
}

struct DepositTarget: Hashable { let asset: String }

/// 提现方式（ref2 #16）：链上提币 / 内部转账（0 手续费）
struct WithdrawSheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    var body: some View {
        LBSheet(title: "提现", onClose: onClose) {
            VStack(spacing: 12) {
                card("arrow.up.right.circle", "链上提币", "往钱包或交易所转出数字货币", badge: nil)
                card("arrow.left.arrow.right.circle", "内部转账", "立即向其他 Linbit 用户转出数字货币", badge: "0 手续费")
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 28)
        }
    }
    private func card(_ icon: String, _ t: String, _ s: String, badge: String?) -> some View {
        Button { onClose(); ToastCenter.shared.show("\(t)将在提现阶段开放") } label: {
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
