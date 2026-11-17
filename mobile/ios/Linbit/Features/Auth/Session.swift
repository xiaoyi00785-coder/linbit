import SwiftUI

/// 登录态 + 当前账户。未登录点受保护操作时 `requireLogin` 弹出登录页，登录成功后继续原操作。
@MainActor
final class Session: ObservableObject {
    static let shared = Session()
    @Published private(set) var account: APIAccount?
    @Published var isLoggedIn = TokenStore.shared.isLoggedIn
    @Published var showAuth = false
    @Published var authStart: AuthFlow.Start = .login
    private var pending: (() -> Void)?

    init() {
        NotificationCenter.default.addObserver(forName: .lbSessionExpired, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in
                guard let self, self.isLoggedIn, !TokenStore.shared.loggingOut else { return }
                self.isLoggedIn = false; self.account = nil
                await NotifCenter.shared.refresh()
                ToastCenter.shared.show("登录已过期，请重新登录", .error)
                self.showAuth = true
            }
        }
    }

    func requireLogin(_ then: @escaping () -> Void) {
        if isLoggedIn { then() } else { pending = then; authStart = .login; showAuth = true }
    }
    func present(_ start: AuthFlow.Start) { authStart = start; showAuth = true }

    func didLogin() async {
        TokenStore.shared.loggingOut = false
        isLoggedIn = true
        showAuth = false
        await reloadAccount()
        Task { await Watchlist.shared.sync() }
        Task { await NotifCenter.shared.refresh() }
        ToastCenter.shared.show("登录成功", .success)
        let p = pending; pending = nil
        p?()
    }
    func reloadAccount() async { account = try? await AuthAPI.account() }

    func logout() async {
        isLoggedIn = false; account = nil
        await NotifCenter.shared.refresh()
        await AuthAPI.logout()
        ToastCenter.shared.show("已退出登录")
    }
    func cancelAuth() { pending = nil; showAuth = false }
}

/// 全局路由：切 Tab、指定现货交易对、打开二级页
@MainActor
final class Router: ObservableObject {
    static let shared = Router()
    @Published var tab: MainTab = .home
    /// 现货页顶部：0 现货 1 策略 2 理财 3 闪兑
    @Published var spotTop = 0
    @Published var spotSymbol = "BTCUSDT"
    @Published var assetsPath = NavigationPath()
    @Published var spotPath = NavigationPath()
    @Published var showUserCenter = false
    /// 行情点进的币种详情
    @Published var marketDetail: String?
    /// 详情页是合约还是现货。必须先于 marketDetail 赋值。
    @Published var marketDetailFutures = false
    /// 从首页搜索进行情时直接打开搜索页
    @Published var marketSearch = false
    func showMarket(_ symbol: String, futures: Bool) {
        marketDetailFutures = futures
        marketDetail = symbol
    }
    /// 二级全屏功能页（理财 / 策略 / 闪兑 / 聊天室 / 子账户 …）
    @Published var feature: FeaturePage?
    /// 合约 Tab 顶部：0 合约 1 事件合约 2 TradFi 3 模拟交易
    @Published var futuresTop = 0
    @Published var futuresSymbol = "BTCUSDT"
    /// CI 截图：自动弹出的弹窗名（leverage / mode / tpsl / kind / tools / subscribe / gridSheet …）
    @Published var shotSheet: String?

    /// 主 Tab 页此刻是否在屏幕上：是当前 Tab，且没有全屏页盖在上面。
    func isLive(_ t: MainTab) -> Bool { tab == t && feature == nil && marketDetail == nil && !showUserCenter }

    func trade(_ symbol: String) { spotSymbol = symbol; spotTop = 0; tab = .spot }
    func tradeFutures(_ symbol: String) { futuresSymbol = symbol; futuresTop = 0; tab = .futures }
    /// 切到现货 Tab 的某一子页。0 现货，1 策略，2 理财，3 闪兑。
    func openSpotTab(_ index: Int) { spotTop = index; tab = .spot }
    func openFeature(_ f: FeaturePage) { feature = f }
    func open(_ r: AssetsRoute) {
        Session.shared.requireLogin { [self] in tab = .assets; assetsPath.append(r) }
    }
}

enum AssetsRoute: Hashable { case deposit(String?), withdraw(String?), transfer, records, history, overview, accountTransfer, internalTransfer, withdrawRecords, addressBook, networks(String?), proof }
