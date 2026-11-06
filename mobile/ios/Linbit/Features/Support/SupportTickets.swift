import SwiftUI
import PhotosUI
import UIKit

// MARK: - 接口：客服工单（列表/创建/详情/回复/关闭）与附件（上传/查看）
enum SupportAPI {
    static let maxImageBytes = 5 * 1024 * 1024
    static let maxImagesPerMessage = 3
    private static var api: APIClient { .shared }

    static func list(status: APISupportTicketStatus?, page: Int = 1) async throws -> APISupportTicketList {
        try await api.get("/support/tickets", query: ["status": status?.rawValue, "page": "\(page)", "pageSize": "20"], auth: true)
    }
    static func create(_ r: APISupportCreateTicketRequest) async throws -> APISupportTicket { try await api.post("/support/tickets", body: r) }
    static func detail(_ id: String) async throws -> APISupportTicket { try await api.get("/support/tickets/\(id)", auth: true) }
    static func reply(_ id: String, _ r: APISupportReplyTicketRequest) async throws -> APISupportTicket { try await api.post("/support/tickets/\(id)/messages", body: r) }
    static func close(_ id: String) async throws -> APISupportTicket {
        struct Empty: Encodable {}
        return try await api.post("/support/tickets/\(id)/close", body: Empty())
    }
    static func upload(_ img: PickedImage) async throws -> APISupportAttachment {
        try await api.uploadMultipart("/support/attachments", field: "file", filename: "image.\(img.ext)", mime: img.mime, data: img.data)
    }
    static func image(_ a: APISupportAttachment) async throws -> Data {
        try await api.getData(a.url ?? "/support/attachments/\(a.id)")
    }
}

/// 选中的图片：仅 JPG / PNG / WEBP，按文件头识别格式（不转码），单张 ≤ 5MiB
struct PickedImage: Identifiable, Equatable {
    let id = UUID()
    let data: Data
    let mime: String
    let ext: String
    static func validate(_ d: Data) -> Result<PickedImage, SupportPickError> {
        let b = [UInt8](d.prefix(12))
        let kind: (String, String)?
        if b.count >= 3, b[0] == 0xFF, b[1] == 0xD8, b[2] == 0xFF { kind = ("image/jpeg", "jpg") }
        else if b.count >= 8, b[0..<8] == [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A] { kind = ("image/png", "png") }
        else if b.count >= 12, b[0..<4] == [0x52, 0x49, 0x46, 0x46], b[8..<12] == [0x57, 0x45, 0x42, 0x50] { kind = ("image/webp", "webp") }
        else { kind = nil }
        guard let kind else { return .failure(.format) }
        guard d.count <= SupportAPI.maxImageBytes else { return .failure(.size) }
        return .success(PickedImage(data: d, mime: kind.0, ext: kind.1))
    }

    /// 相册里选出的图片：先校验格式 / 大小，再去掉 EXIF（含 GPS 定位）等元数据，处理后再校验一次。
    /// 去不干净时返回 .process，不会退回去上传原图。
    static func fromLibrary(_ d: Data) -> Result<PickedImage, SupportPickError> {
        switch validate(d) {
        case let .failure(e): return .failure(e)
        case let .success(img):
            guard let clean = ImageMetadataStripper.strip(img.data, mime: img.mime) else { return .failure(.process) }
            return validate(clean)
        }
    }
}

enum SupportPickError: Error {
    case format, size, count, process
    var text: String {
        switch self {
        case .process: return "无法处理所选图片，请换一张再试"
        case .format: return "仅支持 JPG / PNG / WEBP 格式图片"
        case .size: return "单张图片不能超过 5MB"
        case .count: return "每条消息最多上传 3 张图片"
        }
    }
}

extension APISupportTicketCategory {
    var title: String {
        switch self {
        case .account: return "账户"
        case .depositWithdraw: return "充值提现"
        case .trade: return "交易"
        case .kyc: return "身份认证"
        case .security: return "账户安全"
        case .other: return "其他"
        }
    }
    var icon: String {
        switch self {
        case .account: return "person.crop.circle"
        case .depositWithdraw: return "arrow.down.left.arrow.up.right"
        case .trade: return "chart.line.uptrend.xyaxis"
        case .kyc: return "person.text.rectangle"
        case .security: return "lock.shield"
        case .other: return "ellipsis.bubble"
        }
    }
}

extension APISupportTicketStatus {
    var title: String {
        switch self {
        case ._open: return "待处理"
        case .replied: return "客服已回复"
        case .closed: return "已关闭"
        }
    }
}

struct SupportStatusBadge: View {
    @Environment(\.palette) private var p
    let status: APISupportTicketStatus
    var body: some View {
        let (fg, bg): (Color, Color) = {
            switch status {
            case ._open: return (p.textPrimary, p.fillStrong)
            case .replied: return (p.up, p.upTint)
            case .closed: return (p.textTertiary, p.fill)
            }
        }()
        Text(status.title).font(.lb(FontSize.small, .medium)).foregroundColor(fg)
            .padding(.horizontal, 8).frame(height: 22)
            .background(RoundedRectangle(cornerRadius: 4).fill(bg))
    }
}

// MARK: - CI 截图演示数据（lb.shot = support-*）
enum SupportShot {
    static var raw: String?
    static var on: Bool { raw != nil }
    static let now = Date(timeIntervalSince1970: 1_793_000_000)
    static func ticket(_ id: String, _ c: APISupportTicketCategory, _ s: APISupportTicketStatus, _ subject: String, unread: Bool = false, ago: Double, messages: [APISupportTicketMessage]? = nil) -> APISupportTicket {
        APISupportTicket(id: id, category: c, subject: subject, status: s, lastReplyBy: s == .replied ? .support : .user,
                         lastReplyAt: now.addingTimeInterval(-ago), unread: unread, messageCount: messages?.count ?? 2,
                         closedAt: s == .closed ? now.addingTimeInterval(-ago) : nil, closedBy: s == .closed ? "user" : nil,
                         createdAt: now.addingTimeInterval(-ago - 86400), updatedAt: now.addingTimeInterval(-ago), messages: messages)
    }
    static var list: [APISupportTicket] { [
        ticket("T1001", .depositWithdraw, .replied, "USDT 充值 2 小时未到账", unread: true, ago: 600),
        ticket("T1002", .kyc, ._open, "身份认证提交后一直审核中", ago: 3600 * 3),
        ticket("T1003", .trade, ._open, "合约止盈单没有触发", ago: 3600 * 20),
        ticket("T1004", .security, .closed, "更换绑定手机号", ago: 86400 * 3),
        ticket("T1005", .account, .replied, "如何修改昵称", ago: 86400 * 5),
        ticket("T1006", .other, .closed, "APP 夜间模式建议", ago: 86400 * 9),
    ] }
    static func detail(_ s: APISupportTicketStatus) -> APISupportTicket {
        let att = APISupportAttachment(id: "demo.png", mime: .png, size: 182_331, url: nil)
        var msgs = [
            APISupportTicketMessage(id: "m1", author: .user, body: "TRC20 充值 500 USDT，交易哈希 9f2c…a71b，已经 2 小时还没到账。官网 https://example.com 上查到已确认。", attachments: [att], createdAt: now.addingTimeInterval(-7200)),
        ]
        if s != ._open {
            msgs.append(APISupportTicketMessage(id: "m2", author: .support, authorName: "客服", body: "您好，该笔充值已达到 20 次网络确认，系统正在入账，预计 10 分钟内到账，请留意资产变动。", attachments: [], createdAt: now.addingTimeInterval(-600)))
        }
        return ticket("T1001", .depositWithdraw, s, "USDT 充值 2 小时未到账", ago: 600, messages: msgs)
    }
    /// 演示附件：纯色 PNG（不读网络）
    static func demoImage() -> UIImage {
        UIGraphicsImageRenderer(size: CGSize(width: 120, height: 120)).image { ctx in
            UIColor(red: 0.61, green: 0.80, blue: 0.16, alpha: 1).setFill(); ctx.fill(CGRect(x: 0, y: 0, width: 120, height: 120))
            UIColor.white.setFill(); ctx.fill(CGRect(x: 30, y: 40, width: 60, height: 40))
        }
    }
}

// MARK: - 工单入口：列表 → 新建 / 详情
struct SupportTicketsView: View {
    enum Route: Equatable { case list, create, detail(String) }
    var onBack: () -> Void = { Task { @MainActor in Router.shared.feature = nil } }
    @State private var route: Route = {
        switch SupportShot.raw {
        case "create", "create-ratelimit", "create-error": return .create
        case let r? where r.hasPrefix("detail"): return .detail("T1001")
        default: return .list
        }
    }()
    /// 详情返回 / 新建成功后递增，驱使列表重新拉取（未读、状态与后端同步）
    @State private var listEpoch = 0

    var body: some View {
        switch route {
        case .list:
            SupportTicketListView(
                onBack: onBack,
                open: { route = .detail($0) },
                create: { Session.shared.requireLogin { route = .create } },
                epoch: listEpoch
            )
        case .create:
            SupportCreateView(onBack: { route = .list }, created: { id in
                listEpoch += 1
                route = .detail(id)
            })
        case let .detail(id):
            SupportTicketDetailView(id: id, onBack: {
                listEpoch += 1
                route = .list
            })
        }
    }
}

struct SupportTicketListView: View {
    @Environment(\.palette) private var p
    @ObservedObject private var session = Session.shared
    let onBack: () -> Void
    let open: (String) -> Void
    let create: () -> Void
    var epoch: Int = 0
    @State private var tab = 0
    @State private var data: Loadable<[APISupportTicket]> = .idle
    @State private var page = 1
    @State private var total = 0
    @State private var loadingMore = false
    @State private var listReq = 0
    private static let filters: [APISupportTicketStatus?] = [nil, ._open, .replied, .closed]
    private static let pageSize = 20

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "我的工单", onBack: onBack) {
                Button(action: create) { Image(systemName: "square.and.pencil").foregroundColor(p.textPrimary) }
            }
            PrimaryTabs(items: ["全部", "待处理", "已回复", "已关闭"], selected: $tab, size: FontSize.label, underline: true)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Spacing.pageH)
            ScrollView {
                LoadableView(state: data, emptyText: "暂无工单", isEmpty: { $0.isEmpty }, retry: { Task { await load(reset: true) } }) { list in
                    LazyVStack(spacing: 0) {
                        ForEach(list, id: \.id) { t in
                            Button { open(t.id) } label: { row(t) }.buttonStyle(RowPressStyle())
                        }
                        if list.count < total {
                            ProgressView().frame(height: 44).onAppear { Task { await more() } }
                        }
                    }
                }
                if case .loaded(let l) = data, l.isEmpty, session.isLoggedIn || SupportShot.on {
                    LBButton(title: "提交工单", kind: .primary, height: 40, action: create).frame(width: 160)
                }
            }
            .refreshable { await load(reset: true) }
        }
        .background(p.bg.ignoresSafeArea())
        .task(id: "\(tab)-\(epoch)") { await load(reset: true) }
        .onChange(of: session.isLoggedIn) { ok in if ok { Task { await load(reset: true) } } }
    }

    private func row(_ t: APISupportTicket) -> some View {
        HStack(alignment: .top, spacing: 12) {
            ZStack { Circle().fill(p.fill); Image(systemName: t.category.icon).font(.system(size: 15)).foregroundColor(p.textPrimary) }
                .frame(width: 36, height: 36)
                .overlay(alignment: .topTrailing) { if t.unread { Circle().fill(p.badgeDot).frame(width: 8, height: 8) } }
            VStack(alignment: .leading, spacing: 5) {
                HStack {
                    Text(verbatim: t.subject).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary).lineLimit(1)
                    Spacer(minLength: 8)
                    SupportStatusBadge(status: t.status)
                }
                Text("\(t.category.title) · \(Fmt.dateTime(t.lastReplyAt)) · \(t.messageCount) 条消息")
                    .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            }
        }
        .padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
    }

    private func load(reset: Bool) async {
        let st = Self.filters[tab]
        if let s = SupportShot.raw {
            switch s {
            case "list-empty": data = .loaded([]); total = 0
            case "list-error": data = .failed("网络异常，请稍后重试")
            case "list-loading": data = .loading
            default:
                let items = SupportShot.list.filter { st == nil || $0.status == st }
                total = items.count; data = .loaded(items)
            }
            return
        }
        guard session.isLoggedIn else {
            data = .failed(ErrorText.loginRequired); total = 0; return
        }
        if reset {
            page = 1
            if data.value == nil { data = .loading }
        }
        listReq += 1
        let req = listReq
        do {
            let r = try await SupportAPI.list(status: st, page: page)
            guard req == listReq else { return }
            total = r.total
            if reset {
                data = .loaded(r.items)
            } else {
                let cur = data.value ?? []
                let seen = Set(cur.map(\.id))
                data = .loaded(cur + r.items.filter { !seen.contains($0.id) })
            }
        } catch {
            guard req == listReq else { return }
            if data.value == nil { data = .failed(error.lbMessage) }
            else { ToastCenter.shared.show(error.lbMessage, .error) }
        }
    }

    private func more() async {
        guard SupportShot.raw == nil, session.isLoggedIn else { return }
        guard !loadingMore, let cur = data.value, cur.count < total else { return }
        loadingMore = true; defer { loadingMore = false }
        page += 1
        await load(reset: false)
        // 若本次失败且未追加，回退页码以免跳页
        if let now = data.value, now.count == cur.count { page = max(1, page - 1) }
    }
}

// MARK: - 图片选择条（≤3 张，JPG/PNG/WEBP，≤5MB）
struct SupportImagePicker: View {
    @Environment(\.palette) private var p
    @Binding var images: [PickedImage]
    @Binding var error: String?
    var disabled = false
    @State private var items: [PhotosPickerItem] = []
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                ForEach(images) { img in
                    ZStack(alignment: .topTrailing) {
                        Group {
                            if let u = UIImage(data: img.data) { Image(uiImage: u).resizable().scaledToFill() } else { p.fill }
                        }
                        .frame(width: 64, height: 64).clipShape(RoundedRectangle(cornerRadius: Radius.md))
                        Button { images.removeAll { $0.id == img.id } } label: {
                            Image(systemName: "xmark.circle.fill").font(.system(size: 16)).foregroundColor(.white).shadow(radius: 1)
                        }.offset(x: 5, y: -5)
                    }
                }
                if images.count < SupportAPI.maxImagesPerMessage {
                    PhotosPicker(selection: $items, maxSelectionCount: SupportAPI.maxImagesPerMessage - images.count, matching: .images) {
                        VStack(spacing: 2) {
                            Image(systemName: "photo.badge.plus").font(.system(size: 18))
                            Text("\(images.count)/\(SupportAPI.maxImagesPerMessage)").font(.lbNum(FontSize.small))
                        }
                        .foregroundColor(p.textSecondary).frame(width: 64, height: 64)
                        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                    }.disabled(disabled)
                }
            }
            Text("支持 JPG / PNG / WEBP，单张 ≤ 5MB，最多 3 张").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }
        .onChange(of: items) { sel in
            guard !sel.isEmpty else { return }
            Task {
                var err: String?
                for it in sel {
                    guard images.count < SupportAPI.maxImagesPerMessage else { err = SupportPickError.count.text; break }
                    guard let d = try? await it.loadTransferable(type: Data.self) else { err = "无法读取所选图片"; continue }
                    switch PickedImage.fromLibrary(d) {
                    case let .success(img): images.append(img)
                    case let .failure(e): err = e.text
                    }
                }
                error = err
                items = []
            }
        }
    }
}

/// 429 冷却：按 Retry-After 秒数倒计时
@MainActor
final class RetryCountdown: ObservableObject {
    @Published private(set) var left = 0
    private var task: Task<Void, Never>?
    var active: Bool { left > 0 }
    func start(_ s: Int) {
        task?.cancel(); left = max(1, s)
        task = Task { [weak self] in
            while let self, self.left > 0 {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                if Task.isCancelled { return }
                self.left -= 1
            }
        }
    }
    var hint: String { "操作过于频繁，请 \(left) 秒后再试" }
}

// MARK: - 新建工单
struct SupportCreateView: View {
    @Environment(\.palette) private var p
    let onBack: () -> Void
    let created: (String) -> Void
    @State private var category: APISupportTicketCategory? = SupportShot.on ? .depositWithdraw : nil
    @State private var subject = SupportShot.on ? "USDT 充值 2 小时未到账" : ""
    @State private var content = SupportShot.on ? "TRC20 充值 500 USDT，已确认但未到账。" : ""
    @State private var images: [PickedImage] = []
    @State private var pickError: String?
    @State private var error: String? = SupportShot.raw == "create-error" ? "提交失败，请稍后重试" : nil
    @State private var sending = false
    @StateObject private var cooldown = RetryCountdown()

    private var valid: Bool {
        category != nil && !subject.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && subject.count <= 120
            && !content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && content.count <= 5000
    }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "提交工单", onBack: onBack)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text("问题类型").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: 3), spacing: 8) {
                        ForEach(APISupportTicketCategory.allCases, id: \.self) { c in
                            let on = category == c
                            Button { category = c } label: {
                                HStack(spacing: 4) {
                                    Image(systemName: c.icon).font(.system(size: 12))
                                    Text(c.title).font(.lb(FontSize.caption, on ? .medium : .regular))
                                }
                                .foregroundColor(on ? p.textOnPrimary : p.textPrimary)
                                .frame(maxWidth: .infinity).frame(height: 36)
                                .background(RoundedRectangle(cornerRadius: Radius.md).fill(on ? p.primary : p.fill))
                            }.buttonStyle(PressableStyle())
                        }
                    }
                    LBTextField(label: "标题", placeholder: "简要描述问题（120 字以内）", text: $subject, error: subject.count > 120 ? "标题不能超过 120 字" : nil) { EmptyView() }
                    VStack(alignment: .leading, spacing: 6) {
                        Text("问题描述").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                        TextEditor(text: $content).font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                            .scrollContentBackground(.hidden).padding(8).frame(height: 140)
                            .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        Text("\(content.count)/5000").font(.lbNum(FontSize.small)).foregroundColor(content.count > 5000 ? p.down : p.textTertiary)
                            .frame(maxWidth: .infinity, alignment: .trailing)
                    }
                    SupportImagePicker(images: $images, error: $pickError, disabled: sending)
                    if let e = pickError { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    if cooldown.active { Text(cooldown.hint).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    else if let e = error { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
                    LBButton(title: cooldown.active ? "\(cooldown.left)s 后可提交" : "提交", kind: .primary,
                             enabled: valid && !cooldown.active, loading: sending) { Task { await submit() } }
                }
                .padding(Spacing.pageH)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .onAppear { if SupportShot.raw == "create-ratelimit" { cooldown.start(58) } }
    }

    private func submit() async {
        guard let category else { return }
        sending = true; error = nil
        defer { sending = false }
        do {
            var ids: [String] = []
            for img in images { ids.append(try await SupportAPI.upload(img).id) }
            let t = try await SupportAPI.create(APISupportCreateTicketRequest(category: category, subject: subject, content: content, attachments: ids.isEmpty ? nil : ids))
            ToastCenter.shared.show("工单已提交", .success)
            created(t.id)
        } catch {
            if let f = error as? APIFailure, f.status == 429 { cooldown.start(f.retryAfter ?? 60) } else { self.error = error.lbMessage }
        }
    }
}

// MARK: - 工单详情：消息（纯文本，不识别链接）+ 图片附件 + 回复 / 关闭
struct SupportTicketDetailView: View {
    @Environment(\.palette) private var p
    let id: String
    let onBack: () -> Void
    @State private var data: Loadable<APISupportTicket> = .idle
    @State private var text = ""
    @State private var images: [PickedImage] = []
    @State private var pickError: String?
    @State private var error: String?
    @State private var sending = false
    @State private var closedByConflict = false
    @State private var confirm: ConfirmRequest?
    @State private var viewing: APISupportAttachment?
    @StateObject private var cooldown = RetryCountdown()

    private var closed: Bool { closedByConflict || data.value?.status == .closed }

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "工单详情", onBack: onBack) {
                if let t = data.value, t.status != .closed, !closedByConflict {
                    Button("关闭工单") {
                        confirm = .init(title: "关闭工单", message: "关闭后将无法继续回复，如仍有问题可重新提交工单", confirmTitle: "关闭", destructive: true) {
                            Task { await closeTicket() }
                        }
                    }.font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                }
            }
            ScrollView {
                LoadableView(state: data, retry: { Task { await load() } }) { t in
                    VStack(alignment: .leading, spacing: 14) {
                        header(t)
                        ForEach(t.messages ?? [], id: \.id) { m in bubble(m) }
                    }.padding(Spacing.pageH)
                }
            }
            if data.value != nil { composer }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .lbConfirm($confirm)
        .fullScreenCover(item: $viewing) { a in SupportImageViewer(attachment: a) { viewing = nil } }
    }

    private func header(_ t: APISupportTicket) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top) {
                Text(verbatim: t.subject).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
                Spacer(minLength: 8)
                SupportStatusBadge(status: closedByConflict ? .closed : t.status)
            }
            Text("\(t.category.title) · 工单号 \(t.id) · \(Fmt.dateTime(t.createdAt))").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }
    }

    private func bubble(_ m: APISupportTicketMessage) -> some View {
        let mine = m.author == .user
        return VStack(alignment: mine ? .trailing : .leading, spacing: 4) {
            Text(mine ? "我" : (m.authorName ?? "客服")).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            VStack(alignment: .leading, spacing: 8) {
                // 纯文本：verbatim 不解析 Markdown，也不做链接识别
                Text(verbatim: m.body).font(.lb(FontSize.label)).foregroundColor(mine ? p.textOnPrimary : p.textPrimary)
                    .fixedSize(horizontal: false, vertical: true)
                if !m.attachments.isEmpty {
                    HStack(spacing: 6) {
                        ForEach(m.attachments, id: \.id) { a in
                            Button { viewing = a } label: { SupportAttachmentThumb(attachment: a, size: 72) }.buttonStyle(PressableStyle())
                        }
                    }
                }
            }
            .padding(10)
            .background(RoundedRectangle(cornerRadius: Radius.lg).fill(mine ? p.primary : p.fill))
            Text(Fmt.dateTime(m.createdAt)).font(.lbNum(11)).foregroundColor(p.textTertiary)
        }
        .frame(maxWidth: .infinity, alignment: mine ? .trailing : .leading)
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 8) {
            if closed {
                HStack(spacing: 6) {
                    Image(systemName: "lock.fill").font(.system(size: 12))
                    Text("工单已关闭，无法继续回复。如仍有问题，请重新提交工单。").font(.lb(FontSize.caption))
                }.foregroundColor(p.textSecondary).frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12).background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
            } else {
                SupportImagePicker(images: $images, error: $pickError, disabled: sending || cooldown.active)
                if let e = pickError { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
                if cooldown.active { Text(cooldown.hint).font(.lb(FontSize.small)).foregroundColor(p.down) }
                else if let e = error { Text(e).font(.lb(FontSize.small)).foregroundColor(p.down) }
                HStack(spacing: 8) {
                    TextField("输入回复内容", text: $text, axis: .vertical).lineLimit(1...4)
                        .font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                        .padding(.horizontal, 12).padding(.vertical, 10)
                        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                        .disabled(cooldown.active)
                    LBButton(title: cooldown.active ? "\(cooldown.left)s" : "发送", kind: .primary, height: 40,
                             enabled: !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && text.count <= 5000 && !cooldown.active,
                             loading: sending) { Task { await send() } }
                        .frame(width: 72)
                }
            }
        }
        .padding(.horizontal, Spacing.pageH).padding(.vertical, 10)
        .background(p.bgElevated.overlay(Rectangle().fill(p.divider).frame(height: 0.5), alignment: .top))
    }

    private func load() async {
        if let s = SupportShot.raw {
            switch s {
            case "detail-replied", "detail-ratelimit": data = .loaded(SupportShot.detail(.replied))
            case "detail-closed": data = .loaded(SupportShot.detail(.closed))
            case "detail-conflict": data = .loaded(SupportShot.detail(.replied)); closedByConflict = true
            case "detail-error": data = .failed("网络异常，请稍后重试")
            default: data = .loaded(SupportShot.detail(._open))
            }
            if s == "detail-ratelimit" { text = "好的，我再等等"; cooldown.start(42) }
            return
        }
        if data.value == nil { data = .loading }
        do { data = .loaded(try await SupportAPI.detail(id)) } catch { if data.value == nil { data = .failed(error.lbMessage) } }
    }

    private func send() async {
        sending = true; error = nil
        defer { sending = false }
        do {
            var ids: [String] = []
            for img in images { ids.append(try await SupportAPI.upload(img).id) }
            let t = try await SupportAPI.reply(id, APISupportReplyTicketRequest(content: text, attachments: ids.isEmpty ? nil : ids))
            data = .loaded(t); text = ""; images = []
        } catch let f as APIFailure where f.status == 409 {
            closedByConflict = true
            ToastCenter.shared.show("工单已关闭，无法继续回复", .error)
        } catch let f as APIFailure where f.status == 429 {
            cooldown.start(f.retryAfter ?? 60)
        } catch { self.error = error.lbMessage }
    }

    private func closeTicket() async {
        do { data = .loaded(try await SupportAPI.close(id)); ToastCenter.shared.show("工单已关闭", .success) }
        catch { ToastCenter.shared.show(error.lbMessage, .error) }
    }
}

extension APISupportAttachment: Identifiable {}

/// 附件缩略图：带鉴权拉取 GET /support/attachments/{id}
struct SupportAttachmentThumb: View {
    @Environment(\.palette) private var p
    let attachment: APISupportAttachment
    var size: CGFloat = 72
    @State private var img: UIImage? = SupportShot.on ? SupportShot.demoImage() : nil
    @State private var failed = false
    var body: some View {
        ZStack {
            p.fillStrong
            if let img { Image(uiImage: img).resizable().scaledToFill() }
            else if failed { Image(systemName: "photo").foregroundColor(p.textTertiary) }
            else { ProgressView() }
        }
        .frame(width: size, height: size).clipShape(RoundedRectangle(cornerRadius: Radius.md))
        .task {
            guard img == nil else { return }
            if let d = try? await SupportAPI.image(attachment), let u = UIImage(data: d) { img = u } else { failed = true }
        }
    }
}

struct SupportImageViewer: View {
    let attachment: APISupportAttachment
    let onClose: () -> Void
    @State private var img: UIImage? = SupportShot.on ? SupportShot.demoImage() : nil
    @State private var failed = false
    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black.ignoresSafeArea()
            Group {
                if let img { Image(uiImage: img).resizable().scaledToFit() }
                else if failed { Text("图片加载失败").foregroundColor(.white) }
                else { ProgressView().tint(.white) }
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
            Button(action: onClose) { Image(systemName: "xmark").font(.system(size: 18, weight: .semibold)).foregroundColor(.white).padding(16) }
        }
        .task {
            guard img == nil else { return }
            if let d = try? await SupportAPI.image(attachment), let u = UIImage(data: d) { img = u } else { failed = true }
        }
    }
}
