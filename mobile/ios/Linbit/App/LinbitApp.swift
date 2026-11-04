import SwiftUI

@main
struct LinbitApp: App {
    @StateObject private var theme = ThemeStore()
    @StateObject private var splash = SplashState()
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            ZStack {
                // 首页在动画下层同时挂载：首页数据与行情在动画期间并行加载
                ThemedRoot { RootTabView() }
                    .environmentObject(theme)
                    .environmentObject(Session.shared)
                    .environmentObject(AccountsStore.shared)
                    .opacity(splash.done ? 1 : 0)
                if !splash.done {
                    SplashView(frozen: splash.frozen) {
                        withAnimation(.easeInOut(duration: 0.3)) { splash.done = true }
                    }
                    .transition(.opacity)
                    .zIndex(1)
                }
            }
            .preferredColorScheme(theme.mode.colorScheme)
            .task { if !splash.done { await MarketStore.shared.load() } }
        }
        .onChange(of: phase) { p in
            switch p {
            case .active: MarketSocket.shared.connect()
            case .background: MarketSocket.shared.disconnect()
            default: break
            }
        }
    }
}

struct RootTabView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var router = Router.shared
    @ObservedObject private var session = Session.shared
    @ObservedObject private var overlays = OverlayTracker.shared
    @EnvironmentObject private var theme: ThemeStore

    var body: some View {
        ZStack(alignment: .bottom) {
            Group {
                switch router.tab {
                case .home: HomeView(onOpenMarkets: { router.tab = .markets })
                case .markets: MarketsView()
                case .futures: FuturesRootView()
                case .spot: NavigationStack(path: $router.spotPath) {
                    SpotTradeView().navigationBarHidden(true)
                        .navigationDestination(for: SpotRoute.self) { _ in SpotHistoryView() }
                }
                case .assets: AssetsView()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            if overlays.count == 0, router.spotPath.isEmpty && router.assetsPath.isEmpty || ![.spot, .assets].contains(router.tab) {
                FloatingTabBar(selected: $router.tab)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .overlay(alignment: .top) { ToastOverlay() }
        .onAppear { applyScreenshotRoute() }
        .task { await AppFeatures.shared.refresh(); await Watchlist.shared.sync() }
        .fullScreenCover(item: $router.feature) { page in
            ThemedRoot { FeatureHost(page: page) }
                .environmentObject(session).environmentObject(AccountsStore.shared).environmentObject(theme).overlay(alignment: .top) { ToastOverlay() }
        }
        .fullScreenCover(isPresented: $session.showAuth) {
            ThemedRoot { AuthFlow(start: session.authStart) }.environmentObject(session).environmentObject(theme).overlay(alignment: .top) { ToastOverlay() }
        }
        .fullScreenCover(isPresented: $router.showUserCenter) {
            ThemedRoot { NavigationStack { UserCenterView().navigationBarHidden(true) } }
                .environmentObject(session).environmentObject(AccountsStore.shared).environmentObject(theme).overlay(alignment: .top) { ToastOverlay() }
        }
    }

    /// CI 截图用：仅当模拟器写入 lb.shot 时切换页面，正常启动不改变界面。
    private func applyScreenshotRoute() {
        guard let shot = UserDefaults.standard.string(forKey: "lb.shot"), !shot.isEmpty else { return }
        switch shot {
        case "markets": router.tab = .markets
        case "spot": router.tab = .spot
        case "spot-plan": router.tab = .spot; router.shotSheet = "spotPlan"
        case "spot-market-amount": router.tab = .spot; router.shotSheet = "spotMarketAmount"
        case "spot-fee": router.tab = .spot; router.shotSheet = "spotFee"
        case "spot-history-conditional": router.tab = .spot; router.shotSheet = "spotHistoryCond"; router.spotPath.append(SpotRoute.history)
        case "spot-conditional": router.tab = .spot; router.shotSheet = "spotConditional"
        case "assets": router.tab = .assets
        case "futures": router.tab = .futures; router.futuresTop = 0
        case "futures-leverage": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "leverage"
        case "futures-mode": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "mode"
        case "futures-tpsl": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "tpsl"
        case "futures-kind": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "kind"
        case "futures-tools": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "tools"
        case "futures-chart": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "chart"
        case "futures-plan": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "plan"
        case "futures-orders-tpsl": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "ordersTpsl"
        case "futures-history": router.tab = .futures; router.futuresTop = 0; router.feature = .futuresHistory
        case "futures-algo-amend-plan": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "algoAmendPlan"
        case "futures-algo-amend-tpsl": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "algoAmendTpsl"
        case "futures-orders-plan": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "ordersPlan"
        case "futures-amend": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "amend"
        case "futures-mode-hedge": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "modeHedge"
        case "futures-tpsl-partial": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "tpslPartial"
        case "futures-position-history": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "posHistory"; router.feature = .futuresHistory
        case "futures-algo-history": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "algoHistory"; router.feature = .futuresHistory
        case "futures-tiers": router.tab = .futures; router.futuresTop = 0; router.feature = .futuresRiskTiers
        case "futures-funding": router.tab = .futures; router.futuresTop = 0; router.feature = .futuresFunding
        case "event": router.tab = .futures; router.futuresTop = 1
        case "event-confirm": router.tab = .futures; router.futuresTop = 1; router.shotSheet = "eventConfirm"
        case "earn": router.feature = .earn
        case "earn-subscribe": router.shotSheet = "earnSubscribe"; router.feature = .earn
        case "strategy": router.feature = .strategy
        case "grid-create": router.feature = .gridCreate
        case "grid-confirm": router.shotSheet = "gridConfirm"; router.feature = .gridCreate
        case "swap": router.feature = .swap
        case "swap-picker": router.shotSheet = "swapPicker"; router.feature = .swap
        case "chat": router.feature = .chat
        case "subaccounts": router.feature = .subAccounts
        case "sub-create": router.shotSheet = "subCreate"; router.feature = .subAccounts
        case "account-switch": router.shotSheet = "accountSwitch"; router.feature = .subAccounts
        case "welfare": router.feature = .welfare
        case "futures-pnl": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "pnl"
        case "futures-pnl-error": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "pnlError"
        case "futures-pnl-empty": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "pnlEmpty"
        case "password-register": router.tab = .home; router.shotSheet = "pwdRegister"; session.present(.register)
        case "password-reset": router.tab = .home; router.shotSheet = "pwdReset"; session.present(.login)
        case "password-change": router.shotSheet = "changePwd"; router.feature = .changePassword
        case "login":
            router.tab = .home
            session.present(.login)
        default:
            router.tab = .home
        }
    }
}
