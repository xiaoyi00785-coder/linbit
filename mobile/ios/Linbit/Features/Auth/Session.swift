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
                guard let self, self.isLoggedIn else { return }
                self.isLoggedIn = false; self.account = nil
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
        isLoggedIn = true
        showAuth = false
        await reloadAccount()
        ToastCenter.shared.show("登录成功", .success)
        let p = pending; pending = nil
        p?()
    }
    func reloadAccount() async { account = try? await AuthAPI.account() }

    func logout() async {
        await AuthAPI.logout()
        isLoggedIn = false; account = nil
        ToastCenter.shared.show("已退出登录")
    }
    func cancelAuth() { pending = nil; showAuth = false }
}

/// 全局路由：切 Tab、指定现货交易对、打开二级页
@MainActor
final class Router: ObservableObject {
    static let shared = Router()
    @Published var tab: MainTab = .home
    @Published var spotSymbol = "BTCUSDT"
    @Published var assetsPath = NavigationPath()
    @Published var spotPath = NavigationPath()
    @Published var showUserCenter = false

    func trade(_ symbol: String) { spotSymbol = symbol; tab = .spot }
    func open(_ r: AssetsRoute) {
        Session.shared.requireLogin { [self] in tab = .assets; assetsPath.append(r) }
    }
}

enum AssetsRoute: Hashable { case deposit(String?), transfer, records, history }
