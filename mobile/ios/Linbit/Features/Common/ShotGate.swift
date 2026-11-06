import Foundation

/// CI / Debug 截图演示总闸。
/// - Debug：可读 `UserDefaults` 的 `lb.shot`，可写入演示 raw（现有截图 CI 不变）。
/// - Release：编译期关闭；启动参数 / defaults 无法进入演示态，也无法绕过正常鉴权注入假数据。
///
/// `SupportShot`（客服 PR）等独立 raw 应同样读写 `ShotRaw.value`，勿再各自持久化。
enum ShotGate {
    static var enabled: Bool {
        #if DEBUG
        true
        #else
        false
        #endif
    }

    /// CI 写入的界面路由键；Release 恒为 nil。
    static var launchShot: String? {
        guard enabled else { return nil }
        guard let s = UserDefaults.standard.string(forKey: "lb.shot"), !s.isEmpty else { return nil }
        return s
    }
}

/// 演示路由写入的原始状态串（`SecShot` / 各 Demo / 未来 `SupportShot` 共用）。
/// Release 下 getter 恒 nil、setter 为空操作，避免残留写入。
enum ShotRaw {
    #if DEBUG
    private static var storage: String?
    static var value: String? {
        get { storage }
        set { storage = newValue }
    }
    #else
    static var value: String? {
        get { nil }
        set { _ = newValue }
    }
    #endif
}
