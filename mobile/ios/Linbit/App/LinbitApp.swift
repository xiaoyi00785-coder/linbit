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
        .onAppear {
            #if DEBUG
            applyScreenshotRoute()
            #endif
        }
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

    /// CI 截图用：仅 Debug + 模拟器写入 lb.shot 时切换页面。Release 整段剥离，无法经 defaults/启动参数进入演示。
    #if DEBUG
    private func applyScreenshotRoute() {
        guard ShotGate.enabled, let shot = ShotGate.launchShot else { return }
        switch shot {
        case "markets": router.tab = .markets
        case "spot": router.tab = .spot
        case "spot-plan": router.tab = .spot; router.shotSheet = "spotPlan"
        case "spot-market-amount": router.tab = .spot; router.shotSheet = "spotMarketAmount"
        case "spot-fee": router.tab = .spot; router.shotSheet = "spotFee"
        case "spot-history-conditional": router.tab = .spot; router.shotSheet = "spotHistoryCond"; router.spotPath.append(SpotRoute.history)
        case "spot-conditional": router.tab = .spot; router.shotSheet = "spotConditional"
        case "assets": router.tab = .assets
        case "assets-overview": router.tab = .assets; router.assetsPath.append(AssetsRoute.overview)
        case "assets-networks": router.tab = .assets; router.assetsPath.append(AssetsRoute.networks("USDT"))
        case "assets-account-transfer": router.tab = .assets; router.assetsPath.append(AssetsRoute.accountTransfer)
        case "assets-internal-transfer": router.tab = .assets; router.assetsPath.append(AssetsRoute.internalTransfer)
        case "assets-withdraw-records": router.tab = .assets; router.assetsPath.append(AssetsRoute.withdrawRecords)
        case "assets-deposit": router.tab = .assets; router.assetsPath.append(AssetsRoute.deposit("USDT"))
        case "assets-withdraw": router.tab = .assets; router.assetsPath.append(AssetsRoute.withdraw("USDT"))
        case "assets-withdraw-ok": router.tab = .assets; router.assetsPath.append(AssetsRoute.withdraw("USDT"))
        case "assets-withdraw-paused": router.tab = .assets; router.assetsPath.append(AssetsRoute.withdraw("USDT"))
        case "assets-address-book": router.tab = .assets; router.assetsPath.append(AssetsRoute.addressBook)
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
        case "futures-mark-history": router.tab = .futures; router.futuresTop = 0; router.feature = .futuresMarkHistory
        case "futures-insurance": router.tab = .futures; router.futuresTop = 0; router.feature = .futuresInsurance
        case "futures-guide": router.tab = .futures; router.futuresTop = 0; router.feature = .futuresGuide
        case "futures-calc": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "calc"
        case "spot-chart": router.tab = .spot; router.shotSheet = "spotChart"
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
        case "futures-liq": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "liq"
        case "spot-history-amount": router.tab = .spot; router.shotSheet = "spotHistoryAmount"; router.spotPath.append(SpotRoute.history)
        case "futures-pnl": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "pnl"
        case "futures-pnl-error": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "pnlError"
        case "futures-pnl-empty": router.tab = .futures; router.futuresTop = 0; router.shotSheet = "pnlEmpty"
        case "password-register": router.tab = .home; router.shotSheet = "pwdRegister"; session.present(.register)
        case "password-reset": router.tab = .home; router.shotSheet = "pwdReset"; session.present(.login)
        case let s where s.hasPrefix("support-"): SupportShot.raw = String(s.dropFirst("support-".count)); router.feature = .support
        case "security": router.shotSheet = "sec:"; SecShot.raw = "sec:"; router.feature = .security
        case "kyc-none": SecShot.raw = "kyc:none"; router.feature = .kyc
        case "kyc-pending": SecShot.raw = "kyc:pending"; router.feature = .kyc
        case "kyc-rejected": SecShot.raw = "kyc:rejected"; router.feature = .kyc
        case "kyc-approved": SecShot.raw = "kyc:approved"; router.feature = .kyc
        case "kyc-form": SecShot.raw = "kyc:form"; router.feature = .kyc
        case "kyc-form2": SecShot.raw = "kyc:form2"; router.feature = .kyc
        case "kyc-form2-full": SecShot.raw = "kyc:form2full"; router.feature = .kyc
        case "kyc-error": SecShot.raw = "kyc:error"; router.feature = .kyc
        case "price-alerts", "price-alerts-empty", "price-alerts-error", "price-alerts-disabled", "price-alerts-create", "price-alerts-edit", "price-alerts-delete":
            router.feature = .priceAlerts
        case "notif-list": SecShot.raw = "notif:list"; router.feature = .notifications
        case "notif-empty": SecShot.raw = "notif:empty"; router.feature = .notifications
        case "notif-error": SecShot.raw = "notif:error"; router.feature = .notifications
        case "notif-settings": SecShot.raw = "notif:settings"; router.feature = .notifications
        case "notif-settings-error": SecShot.raw = "notif:settings-error"; router.feature = .notifications
        case "home-bell": SecShot.raw = "notif:list"; router.tab = .home
        case "coupon-issued": SecShot.raw = "coupon:issued"; router.feature = .coupons
        case "coupon-active": SecShot.raw = "coupon:active"; router.feature = .coupons
        case "coupon-expired": SecShot.raw = "coupon:expired"; router.feature = .coupons
        case "coupon-empty": SecShot.raw = "coupon:empty"; router.feature = .coupons
        case "coupon-error": SecShot.raw = "coupon:error"; router.feature = .coupons
        case "coupon-confirm": SecShot.raw = "coupon:confirm"; router.feature = .coupons
        case "coupon-fail": SecShot.raw = "coupon:fail"; router.feature = .coupons
        case "partner-none": SecShot.raw = "partner:none"; router.feature = .partner
        case "partner-apply": SecShot.raw = "partner:apply"; router.feature = .partner
        case "partner-apply-error": SecShot.raw = "partner:apply-error"; router.feature = .partner
        case "partner-pending": SecShot.raw = "partner:pending"; router.feature = .partner
        case "partner-rejected": SecShot.raw = "partner:rejected"; router.feature = .partner
        case "partner-active": SecShot.raw = "partner:active"; router.feature = .partner
        case "partner-team": SecShot.raw = "partner:team"; router.feature = .partner
        case "partner-empty": SecShot.raw = "partner:empty"; router.feature = .partner
        case "partner-error": SecShot.raw = "partner:error"; router.feature = .partner
        case "copy-list": SecShot.raw = "copy:list"; router.feature = .copyTrading
        case "copy-empty": SecShot.raw = "copy:empty"; router.feature = .copyTrading
        case "copy-error": SecShot.raw = "copy:error"; router.feature = .copyTrading
        case "copy-detail": SecShot.raw = "copy:detail"; router.feature = .copyTrading
        case "copy-follow": SecShot.raw = "copy:follow"; router.feature = .copyTrading
        case "copy-follow-error": SecShot.raw = "copy:follow-error"; router.feature = .copyTrading
        case "copy-follows": SecShot.raw = "copy:follows"; router.feature = .copyTrading
        case "copy-follows-empty": SecShot.raw = "copy:follows-empty"; router.feature = .copyTrading
        case "copy-become": SecShot.raw = "copy:become"; router.feature = .copyTrading
        case "copy-become-error": SecShot.raw = "copy:become-error"; router.feature = .copyTrading
        case "loan-market": SecShot.raw = "loan:market"; router.feature = .loans
        case "loan-quote": SecShot.raw = "loan:quote"; router.feature = .loans
        case "loan-borrow-error": SecShot.raw = "loan:borrow-error"; router.feature = .loans
        case "loan-loans": SecShot.raw = "loan:loans"; router.feature = .loans
        case "loan-loans-empty": SecShot.raw = "loan:loans-empty"; router.feature = .loans
        case "loan-closed": SecShot.raw = "loan:closed"; router.feature = .loans
        case "loan-repay": SecShot.raw = "loan:repay"; router.feature = .loans
        case "loan-collateral": SecShot.raw = "loan:collateral"; router.feature = .loans
        case "loan-error": SecShot.raw = "loan:error"; router.feature = .loans
        case "tradfi-list": SecShot.raw = "tradfi:list"; router.feature = .tradfi
        case "tradfi-metal": SecShot.raw = "tradfi:metal"; router.feature = .tradfi
        case "tradfi-empty": SecShot.raw = "tradfi:empty"; router.feature = .tradfi
        case "tradfi-restricted": SecShot.raw = "tradfi:restricted"; router.feature = .tradfi
        case "tradfi-disabled": SecShot.raw = "tradfi:disabled"; router.feature = .tradfi
        case "tradfi-unavailable": SecShot.raw = "tradfi:unavailable"; router.feature = .tradfi
        case "tradfi-error": SecShot.raw = "tradfi:error"; router.feature = .tradfi
        case "security-error": router.shotSheet = "sec::error"; SecShot.raw = "sec::error"; router.feature = .security
        case "security-profile": router.shotSheet = "sec:profile"; SecShot.raw = "sec:profile"; router.feature = .security
        case "security-profile-error": router.shotSheet = "sec:profile:error"; SecShot.raw = "sec:profile:error"; router.feature = .security
        case "security-email": router.shotSheet = "sec:email"; SecShot.raw = "sec:email"; router.feature = .security
        case "security-email-error": router.shotSheet = "sec:email:error"; SecShot.raw = "sec:email:error"; router.feature = .security
        case "security-phone": router.shotSheet = "sec:phone"; SecShot.raw = "sec:phone"; router.feature = .security
        case "security-fund": router.shotSheet = "sec:fundPassword"; SecShot.raw = "sec:fundPassword"; router.feature = .security
        case "security-fund-error": router.shotSheet = "sec:fundPassword:error"; SecShot.raw = "sec:fundPassword:error"; router.feature = .security
        case "security-fund-reset": router.shotSheet = "sec:fundPassword:reset"; SecShot.raw = "sec:fundPassword:reset"; router.feature = .security
        case "security-antiphishing": router.shotSheet = "sec:antiPhishing"; SecShot.raw = "sec:antiPhishing"; router.feature = .security
        case "security-antiphishing-error": router.shotSheet = "sec:antiPhishing:error"; SecShot.raw = "sec:antiPhishing:error"; router.feature = .security
        case "security-devices": router.shotSheet = "sec:devices"; SecShot.raw = "sec:devices"; router.feature = .security
        case "security-sessions": router.shotSheet = "sec:devices:sessions"; SecShot.raw = "sec:devices:sessions"; router.feature = .security
        case "security-devices-error": router.shotSheet = "sec:devices:error"; SecShot.raw = "sec:devices:error"; router.feature = .security
        case "security-devices-empty": router.shotSheet = "sec:devices:empty"; SecShot.raw = "sec:devices:empty"; router.feature = .security
        case "security-passkeys": router.shotSheet = "sec:passkeys"; SecShot.raw = "sec:passkeys"; router.feature = .security
        case "security-passkeys-empty": router.shotSheet = "sec:passkeys:empty"; SecShot.raw = "sec:passkeys:empty"; router.feature = .security
        case "security-passkeys-add": router.shotSheet = "sec:passkeys:add"; SecShot.raw = "sec:passkeys:add"; router.feature = .security
        case "security-2fa-disable": router.shotSheet = "sec:disable2fa"; SecShot.raw = "sec:disable2fa"; router.feature = .security
        case "security-2fa-disable-error": router.shotSheet = "sec:disable2fa:error"; SecShot.raw = "sec:disable2fa:error"; router.feature = .security
        case "security-avatar": router.shotSheet = "sec:profile:avatar"; SecShot.raw = "sec:profile:avatar"; router.feature = .security
        case "security-avatar-crop": router.shotSheet = "sec:profile:crop"; SecShot.raw = "sec:profile:crop"; router.feature = .security
        case "security-avatar-uploading": router.shotSheet = "sec:profile:uploading"; SecShot.raw = "sec:profile:uploading"; router.feature = .security
        case "security-avatar-error": router.shotSheet = "sec:profile:avatar-error"; SecShot.raw = "sec:profile:avatar-error"; router.feature = .security
        case "home-avatar": SecShot.raw = "sec:home"
        case "password-change": router.shotSheet = "changePwd"; router.feature = .changePassword
        case "login":
            router.tab = .home
            session.present(.login)
        default:
            router.tab = .home
        }
    }
    #endif
}
