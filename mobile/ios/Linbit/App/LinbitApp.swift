import SwiftUI

@main
struct LinbitApp: App {
    @StateObject private var theme = ThemeStore()
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            ThemedRoot { RootTabView() }
                .environmentObject(theme)
                .environmentObject(Session.shared)
                .environmentObject(AccountsStore.shared)
                .preferredColorScheme(theme.mode.colorScheme)
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
    @EnvironmentObject private var theme: ThemeStore

    var body: some View {
        ZStack(alignment: .bottom) {
            Group {
                switch router.tab {
                case .home: HomeView(onOpenMarkets: { router.tab = .markets })
                case .markets: MarketsView()
                case .futures: PlaceholderView(title: "合约", note: "合约交易页（下一阶段）")
                case .spot: NavigationStack(path: $router.spotPath) {
                    SpotTradeView().navigationBarHidden(true)
                        .navigationDestination(for: SpotRoute.self) { _ in SpotHistoryView() }
                }
                case .assets: AssetsView()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            if router.spotPath.isEmpty && router.assetsPath.isEmpty || ![.spot, .assets].contains(router.tab) {
                FloatingTabBar(selected: $router.tab)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .overlay(alignment: .top) { ToastOverlay() }
        .onAppear { applyScreenshotRoute() }
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
        case "assets": router.tab = .assets
        case "login":
            router.tab = .home
            session.present(.login)
        default:
            router.tab = .home
        }
    }
}
