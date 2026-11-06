import SwiftUI

/// 聊天室：房间切换（综合 / 币种）、历史分页、WS 实时（chat:{room}）、发言规则、举报
struct ChatRoomView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    /// 行情 Tab 内嵌时为 true：不再套 FeatureShell，避免双顶栏
    var embedInTab: Bool = false
    @State private var rooms: [ChatRoom] = []
    @State private var rules: ChatRules?
    @State private var room = "general"
    @State private var state: Loadable<Bool> = .idle
    @State private var disabled = false
    @State private var messages: [ChatMessage] = []
    @State private var hasMore = false
    @State private var text = ""
    @State private var sending = false
    @State private var cooldownUntil = Date.distantPast
    @State private var report: ChatMessage?
    @State private var sub: WSSubscription?
    /// 本机发言接口返回的消息 id（WS 推送没有 mine / clientId，用 id 判断是否自己发的）
    @State private var myIds: Set<Int64> = []

    var body: some View {
        Group {
            if embedInTab {
                chatBody
            } else {
                FeatureShell(title: "聊天室") { chatBody }
            }
        }
        .task { await loadRooms() }
        .onDisappear { sub?.cancel(); sub = nil }
        .lbSheet(Binding(get: { report != nil }, set: { if !$0 { report = nil } })) {
            if let msg = report {
                LBSheet(title: "举报消息", subtitle: msg.sender, showClose: true, onClose: { report = nil }) {
                    VStack(spacing: 0) {
                        ForEach(["广告/引流", "诈骗", "辱骂", "其他"], id: \.self) { r in
                            SheetOptionRow(title: r, selected: false) {
                                report = nil
                                Task {
                                    do { try await ChatAPI.report(msg.id, reason: r); ToastCenter.shared.show("已举报，感谢反馈", .success) }
                                    catch { ToastCenter.shared.error(error) }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder private var chatBody: some View {
        if disabled {
            ComingSoonView(title: "聊天室", note: "功能开关 chat 未开启，或服务端返回 FEATURE_DISABLED")
        } else {
            VStack(spacing: 0) {
                roomBar
                switch state {
                case .idle, .loading: SkeletonList(rows: 8)
                case let .failed(e): ErrorStateView(message: e) { Task { await loadRooms() } }
                case .loaded: list
                }
                inputBar
            }
        }
    }

    private var roomBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(rooms) { r in
                    let on = r.id == room
                    Text("\(title(r)) \(r.online.map { "·\($0)" } ?? "")").font(.lb(FontSize.caption, on ? .semibold : .regular))
                        .foregroundColor(on ? p.textPrimary : p.textSecondary).padding(.horizontal, 10).frame(height: 28)
                        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(on ? p.fillStrong : p.fill))
                        .onTapGesture { guard !on else { return }; room = r.id; Task { await loadMessages() } }
                }
            }.padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
        }
    }
    private func title(_ r: ChatRoom) -> String {
        if let n = r.name, !n.isEmpty { return n }
        if r.id == "general" || r.type == "general" { return "综合" }
        return (r.symbol ?? r.id) + (r.type == "futures" ? " 永续" : "")
    }

    private var list: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if hasMore { Button("加载更早消息") { Task { await loadOlder() } }.font(.lb(FontSize.small)).foregroundColor(p.textSecondary).frame(maxWidth: .infinity) }
                    if messages.isEmpty { EmptyStateView(text: "还没有人发言，来聊两句吧").frame(height: 300) }
                    ForEach(messages) { msg in bubble(msg).id(msg.id) }
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 8)
            }
            .onChange(of: messages.last?.id) { id in if let id { withAnimation { proxy.scrollTo(id, anchor: .bottom) } } }
        }
    }

    private func bubble(_ msg: ChatMessage) -> some View {
        let mine = isMine(msg)
        return HStack(alignment: .top) {
            if mine { Spacer(minLength: 50) }
            VStack(alignment: mine ? .trailing : .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(mine ? "我" : (msg.sender ?? "用户")).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    if let d = msg.createdAt { Text(Fmt.dateTime(d)).font(.lb(10)).foregroundColor(p.textTertiary) }
                }
                Text(msg.text).font(.lb(FontSize.label)).foregroundColor(mine ? p.textOnPrimary : p.textPrimary)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .background(RoundedRectangle(cornerRadius: Radius.lg).fill(mine ? p.primary : p.fill))
                    .contextMenu {
                        Button("复制") { UIPasteboard.general.string = msg.text }
                        if !mine { Button("举报", role: .destructive) { report = msg } }
                    }
            }
            if !mine { Spacer(minLength: 50) }
        }
    }

    private var inputBar: some View {
        VStack(spacing: 4) {
            Divider().overlay(p.divider)
            if !session.isLoggedIn {
                LBButton(title: "登录后发言", height: 40) { session.present(.login) }.padding(.horizontal, Spacing.pageH).padding(.vertical, 6)
            } else {
                HStack(spacing: 8) {
                    TextField("说点什么…（禁止发布链接与联系方式）", text: $text)
                        .font(.lb(FontSize.label)).padding(.horizontal, 12).frame(height: 38)
                        .background(RoundedRectangle(cornerRadius: 19).fill(p.fill))
                    Button { Task { await send() } } label: {
                        if sending { ProgressView().frame(width: 38, height: 38) }
                        else { Image(systemName: "paperplane.fill").foregroundColor(canSend ? p.textOnPrimary : p.textTertiary)
                            .frame(width: 38, height: 38).background(Circle().fill(canSend ? p.primary : p.fill)) }
                    }.disabled(!canSend || sending)
                }.padding(.horizontal, Spacing.pageH).padding(.vertical, 6)
                if let mx = rules?.maxLength, text.count > mx { Text("最多 \(mx) 字").font(.lb(FontSize.small)).foregroundColor(p.down) }
            }
        }.background(p.bg)
    }
    private var canSend: Bool {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return !t.isEmpty && t.count <= (rules?.maxLength ?? 500) && Date() >= cooldownUntil
    }

    private func loadRooms() async {
        state = .loading
        do {
            let r = try await ChatAPI.rooms()
            rooms = r.rooms ?? [ChatRoom(id: "general", type: "general", symbol: nil, name: "综合", online: nil)]
            rules = r.rules
            if !rooms.contains(where: { $0.id == room }), let f = rooms.first { room = f.id }
            await loadMessages()
        } catch {
            if (error as? APIFailure)?.code == "FEATURE_DISABLED" { disabled = true } else { state = .failed(error.lbMessage) }
        }
    }
    private func loadMessages() async {
        sub?.cancel()
        do {
            let r = try await ChatAPI.messages(room, before: nil)
            messages = (r.items ?? []).sorted { $0.id < $1.id }; hasMore = r.hasMore ?? false
            state = .loaded(true)
        } catch { state = .failed(error.lbMessage); return }
        let current = room
        sub = MarketSocket.shared.subscribe("chat:\(current)") { f in
            guard let msg = try? CodableHelper.jsonDecoder.decode(ChatMessage.self, from: f.data) else { return }
            Task { @MainActor in
                guard room == current, !messages.contains(where: { $0.id == msg.id }) else { return }
                messages.append(msg)
            }
        }
    }
    private func loadOlder() async {
        guard let first = messages.first, let r = try? await ChatAPI.messages(room, before: first.id) else { return }
        let have = Set(messages.map(\.id))
        messages = (r.items ?? []).filter { !have.contains($0.id) }.sorted { $0.id < $1.id } + messages; hasMore = r.hasMore ?? false
    }
    /// REST 历史用 mine 字段；WS 推送用发言接口返回的 id 比对
    private func isMine(_ msg: ChatMessage) -> Bool { msg.mine == true || myIds.contains(msg.id) }
    private func send() async {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        sending = true; defer { sending = false }
        do {
            let msg = try await ChatAPI.send(room, text: t)
            text = ""
            myIds.insert(msg.id)
            if !messages.contains(where: { $0.id == msg.id }) { messages.append(msg) }
            cooldownUntil = Date().addingTimeInterval(Double(rules?.minIntervalMs ?? 0) / 1000)
        } catch { ToastCenter.shared.error(error) }
    }
}
