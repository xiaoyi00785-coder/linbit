import SwiftUI

// MARK: - 站内信 / 通知中心（全部 · 系统 · 交易 · 活动 · 安全），全部已读、邮件通知偏好
enum NotifAPI {
    static func list(kind: String?, page: Int, pageSize: Int = 20) async throws -> APINotificationList {
        try await APIClient.shared.get("/notifications", query: ["kind": kind, "page": String(page), "pageSize": String(pageSize)], auth: true)
    }
    static func unread() async throws -> APIUnreadCount { try await APIClient.shared.get("/notifications/unread-count", auth: true) }
    static func read(ids: [String]? = nil, all: Bool? = nil, kind: String? = nil) async throws -> APIUpdatedCount {
        try await APIClient.shared.post("/notifications/read", body: APIReadNotificationsRequest(ids: ids, all: all, kind: kind))
    }
    static func prefs() async throws -> APINotifyPrefs { try await APIClient.shared.get("/notifications/settings", auth: true) }
    static func setPrefs(_ p: APINotifyPrefsPatch) async throws -> APINotifyPrefs { try await APIClient.shared.put("/notifications/settings", body: p) }
}

/// 未读数（首页铃铛角标）
@MainActor final class NotifCenter: ObservableObject {
    static let shared = NotifCenter()
    @Published var unread = 0
    func refresh() async {
        if NotifShot.state != nil { unread = 3; return }
        guard Session.shared.isLoggedIn else { unread = 0; return }
        if let u = try? await NotifAPI.unread() { unread = u.unread ?? 0 }
    }
}

/// 截图演示：SecShot.raw = "notif:<state>"，state ∈ list / empty / error / settings / settings-error
enum NotifShot {
    static var state: String? { SecShot.raw?.hasPrefix("notif:") == true ? String(SecShot.raw!.dropFirst(6)) : nil }
    static var items: [APINotification] {
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        return [
            .init(id: "105", kind: .security, title: "新设备登录提醒", body: "您的账户于 iPhone 15 Pro（新加坡）登录，如非本人操作请立即修改密码并冻结账户。", time: now - 120_000, read: false, emailStatus: "sent"),
            .init(id: "104", kind: .trade, title: "订单已成交", body: "BTC/USDT 限价买单 0.0500 BTC 已全部成交，成交均价 64,218.50 USDT。", time: now - 3_600_000, read: false, emailStatus: "sent"),
            .init(id: "103", kind: .activity, title: "新人福利：100 USDT 体验金", body: "完成身份认证并首次入金，即可领取 100 USDT 合约体验金。", time: now - 86_400_000, read: false, emailStatus: "none"),
            .init(id: "102", kind: .system, title: "系统维护公告", body: "为提升服务质量，平台将于 11 月 8 日 02:00–04:00（UTC+8）进行系统升级，期间充提暂停。", time: now - 2 * 86_400_000, read: true, emailStatus: "sent"),
            .init(id: "101", kind: .trade, title: "合约强平预警", body: "您的 ETHUSDT 永续多仓保证金率已低于 20%，请及时追加保证金。", time: now - 5 * 86_400_000, read: true, emailStatus: "failed"),
        ]
    }
}

struct NotificationsView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    private static let kinds: [(String?, String)] = [(nil, "全部"), ("system", "系统"), ("trade", "交易"), ("activity", "活动"), ("security", "安全")]
    @State private var tab = 0
    @State private var data: Loadable<[APINotification]> = .loading
    @State private var byKind: [String: Int] = [:]
    @State private var page = 1
    @State private var total = 0
    @State private var loadingMore = false
    @State private var settings = NotifShot.state?.hasPrefix("settings") == true
    @State private var expanded: String?

    private var kind: String? { Self.kinds[tab].0 }
    private var unreadHere: Int { kind.map { byKind[$0] ?? 0 } ?? byKind.values.reduce(0, +) }

    var body: some View {
        if settings {
            NotifSettingsView(onBack: { settings = false })
        } else {
            VStack(spacing: 0) {
                NavBar(title: "消息通知", onBack: onBack) {
                    HStack(spacing: 16) {
                        Button { Task { await readAll() } } label: {
                            Image(systemName: "checkmark.circle").font(.system(size: 18))
                                .foregroundColor(unreadHere > 0 ? p.textPrimary : p.textTertiary)
                        }.disabled(unreadHere == 0).accessibilityLabel("全部已读")
                        Button { settings = true } label: { Image(systemName: "gearshape").font(.system(size: 18)).foregroundColor(p.textPrimary) }
                            .accessibilityLabel("通知设置")
                    }
                }
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 20) {
                        ForEach(Self.kinds.indices, id: \.self) { i in
                            Button { tab = i } label: {
                                HStack(spacing: 4) {
                                    Text(Self.kinds[i].1).font(.lb(FontSize.label, tab == i ? .semibold : .regular))
                                        .foregroundColor(tab == i ? p.textPrimary : p.textSecondary)
                                    let n = Self.kinds[i].0.map { byKind[$0] ?? 0 } ?? byKind.values.reduce(0, +)
                                    if n > 0 { Badge(n: n) }
                                }
                                .frame(height: 40)
                                .overlay(Rectangle().fill(tab == i ? p.textPrimary : .clear).frame(height: 2), alignment: .bottom)
                            }.buttonStyle(.plain)
                        }
                    }.padding(.horizontal, Spacing.pageH)
                }
                Divider().overlay(p.border)
                switch data {
                case .idle, .loading: Spacer(); ProgressView(); Spacer()
                case let .failed(m): ErrorStateView(message: m) { Task { await load(reset: true) } }
                case let .loaded(items) where items.isEmpty: Spacer(); EmptyStateView(text: "暂无消息"); Spacer()
                case let .loaded(items):
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(items, id: \.id) { n in row(n) }
                            if items.count < total {
                                ProgressView().frame(height: 44).onAppear { Task { await more() } }
                            }
                        }
                    }
                    .refreshable { await load(reset: true) }
                }
            }
            .background(p.bg.ignoresSafeArea())
            .task(id: tab) { await load(reset: true) }
        }
    }

    private func row(_ n: APINotification) -> some View {
        let unread = n.read == false
        let open = expanded == n.id
        return Button {
            withAnimation(.easeInOut(duration: 0.15)) { expanded = open ? nil : n.id }
            if unread, let id = n.id { Task { await markRead(id) } }
        } label: {
            HStack(alignment: .top, spacing: 12) {
                ZStack(alignment: .topTrailing) {
                    Circle().fill(p.fill).frame(width: 36, height: 36)
                        .overlay(Image(systemName: icon(n.kind)).font(.system(size: 15)).foregroundColor(p.textPrimary))
                    if unread { Circle().fill(p.down).frame(width: 8, height: 8).offset(x: 1, y: -1) }
                }
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(n.title ?? "").font(.lb(FontSize.body, unread ? .semibold : .regular)).foregroundColor(p.textPrimary).lineLimit(1)
                        Spacer()
                        Text(Fmt.dateTime(Date(timeIntervalSince1970: Double(n.time ?? 0) / 1000)))
                            .font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
                    }
                    Text(n.body ?? "").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        .lineLimit(open ? nil : 2).fixedSize(horizontal: false, vertical: open)
                        .multilineTextAlignment(.leading)
                    if open, let e = emailText(n.emailStatus) {
                        Text(e).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    }
                }
            }
            .padding(.horizontal, Spacing.pageH).padding(.vertical, 14).contentShape(Rectangle())
        }
        .buttonStyle(RowPressStyle())
    }

    private func icon(_ k: APINotification.APIKind?) -> String {
        switch k { case .trade: return "arrow.left.arrow.right"; case .activity: return "gift"; case .security: return "shield"; default: return "megaphone" }
    }
    private func emailText(_ s: String?) -> String? {
        ["sent": "已同步发送至邮箱", "pending": "邮件发送中", "failed": "邮件发送失败", "no_address": "未绑定邮箱，仅站内通知"][s ?? ""]
    }

    private func load(reset: Bool) async {
        if let st = NotifShot.state {
            if st == "error" { data = .failed("网络连接失败，请检查网络"); return }
            let all = st == "empty" ? [] : NotifShot.items
            let list = kind.map { k in all.filter { $0.kind?.rawValue == k } } ?? all
            byKind = Dictionary(grouping: all.filter { $0.read == false }, by: { $0.kind?.rawValue ?? "" }).mapValues(\.count)
            total = list.count; data = .loaded(list); return
        }
        if reset { page = 1; if data.value == nil { data = .loading } }
        do {
            let r = try await NotifAPI.list(kind: kind, page: 1)
            total = r.total ?? 0; byKind = r.unreadByKind ?? [:]
            data = .loaded(r.items ?? [])
            NotifCenter.shared.unread = r.unread ?? NotifCenter.shared.unread
        } catch { if data.value == nil { data = .failed(securityMessage(error)) } else { ToastCenter.shared.show(securityMessage(error), .error) } }
    }

    private func more() async {
        guard !loadingMore, let cur = data.value, NotifShot.state == nil else { return }
        loadingMore = true; defer { loadingMore = false }
        if let r = try? await NotifAPI.list(kind: kind, page: page + 1) {
            page += 1; total = r.total ?? total
            let seen = Set(cur.compactMap(\.id))
            data = .loaded(cur + (r.items ?? []).filter { !seen.contains($0.id ?? "") })
        }
    }

    private func setRead(_ pred: (APINotification) -> Bool) {
        guard let cur = data.value else { return }
        data = .loaded(cur.map { n in
            guard pred(n), n.read == false else { return n }
            if let k = n.kind?.rawValue { byKind[k] = max(0, (byKind[k] ?? 0) - 1) }
            NotifCenter.shared.unread = max(0, NotifCenter.shared.unread - 1)
            return .init(id: n.id, kind: n.kind, title: n.title, body: n.body, time: n.time, read: true, emailStatus: n.emailStatus)
        })
    }

    /// 标记已读失败时回滚：把这条消息改回未读，并还原分类未读数和首页角标
    private func revertRead(_ id: String) {
        guard let cur = data.value, let i = cur.firstIndex(where: { $0.id == id }), cur[i].read == true else { return }
        var items = cur
        let n = cur[i]
        items[i] = .init(id: n.id, kind: n.kind, title: n.title, body: n.body, time: n.time, read: false, emailStatus: n.emailStatus)
        if let k = n.kind?.rawValue { byKind[k, default: 0] += 1 }
        NotifCenter.shared.unread += 1
        data = .loaded(items)
    }

    private func markRead(_ id: String) async {
        setRead { $0.id == id }
        guard NotifShot.state == nil else { return }
        do { _ = try await NotifAPI.read(ids: [id]) }
        catch {
            revertRead(id)
            ToastCenter.shared.show(securityMessage(error), .error)
        }
    }

    private func readAll() async {
        if NotifShot.state == nil {
            do { _ = try await NotifAPI.read(all: true, kind: kind) }
            catch { ToastCenter.shared.show(securityMessage(error), .error); return }
        }
        setRead { _ in true }
        if kind == nil { byKind = [:] } else { byKind[kind!] = 0 }
        ToastCenter.shared.show("已全部标为已读", .success)
        await NotifCenter.shared.refresh()
    }
}

struct Badge: View {
    @Environment(\.palette) private var p
    let n: Int
    var body: some View {
        Text(n > 99 ? "99+" : "\(n)").font(.lbNum(10, .semibold)).foregroundColor(.white)
            .padding(.horizontal, 4).frame(minWidth: 16, minHeight: 16)
            .background(Capsule().fill(p.down))
    }
}

// MARK: - 邮件通知偏好（安全通知不可关闭）
struct NotifSettingsView: View {
    @Environment(\.palette) private var p
    let onBack: () -> Void
    @State private var data: Loadable<APINotifyPrefs> = .loading
    @State private var saving: String?

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "通知设置", onBack: onBack)
            switch data {
            case .idle, .loading: Spacer(); ProgressView(); Spacer()
            case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
            case let .loaded(s):
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        Text("邮件通知").font(.lb(FontSize.small)).foregroundColor(p.textTertiary).padding(.vertical, 12)
                        toggle("系统公告", "平台维护、规则变更等重要公告", key: "emailSystem", on: s.emailSystem ?? true)
                        toggle("交易通知", "委托成交、强平预警、充提到账", key: "emailTrade", on: s.emailTrade ?? true)
                        toggle("活动推送", "福利活动、新币上线等营销信息", key: "emailActivity", on: s.emailActivity ?? false)
                        toggle("安全通知", "登录提醒、密码与安全设置变更", key: "emailSecurity", on: true, locked: true)
                        InfoNote(text: "为保障账户安全，安全类通知将始终通过邮件发送，无法关闭。站内信不受此设置影响。").padding(.top, 16)
                    }.padding(.horizontal, Spacing.pageH)
                }
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
    }

    private func toggle(_ title: String, _ sub: String, key: String, on: Bool, locked: Bool = false) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                Text(sub).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            }
            Spacer()
            if saving == key { ProgressView().padding(.trailing, 8) }
            Toggle("", isOn: Binding(get: { on }, set: { v in Task { await set(key, v) } }))
                .labelsHidden().tint(p.brand).disabled(locked || saving != nil).opacity(locked ? 0.5 : 1)
        }
        .frame(minHeight: 64)
        .overlay(Rectangle().fill(p.border).frame(height: 0.5), alignment: .bottom)
    }

    private func load() async {
        if let st = NotifShot.state {
            data = st == "settings-error" ? .failed("网络连接失败，请检查网络") : .loaded(.init(emailSystem: true, emailTrade: true, emailActivity: false, emailSecurity: true))
            return
        }
        do { data = .loaded(try await NotifAPI.prefs()) } catch { data = .failed(securityMessage(error)) }
    }

    private func set(_ key: String, _ v: Bool) async {
        guard let cur = data.value else { return }
        let patch = APINotifyPrefsPatch(emailSystem: key == "emailSystem" ? v : nil, emailTrade: key == "emailTrade" ? v : nil,
                                        emailActivity: key == "emailActivity" ? v : nil)
        let local = APINotifyPrefs(emailSystem: patch.emailSystem ?? cur.emailSystem, emailTrade: patch.emailTrade ?? cur.emailTrade,
                                   emailActivity: patch.emailActivity ?? cur.emailActivity, emailSecurity: true)
        if NotifShot.state != nil { data = .loaded(local); return }
        saving = key; defer { saving = nil }
        do { data = .loaded(try await NotifAPI.setPrefs(patch)); Haptics.success() }
        catch { ToastCenter.shared.show(securityMessage(error), .error); Haptics.error() }
    }
}
