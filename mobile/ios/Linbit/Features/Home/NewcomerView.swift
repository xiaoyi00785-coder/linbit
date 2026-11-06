import SwiftUI

/// 福利中心 / 新人任务（GET /home/newcomer；报名/领取走 OpenAPI /rewards/newcomer/*）
struct NewcomerView: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var state: Loadable<Newcomer> = .idle
    @State private var actionBusy = false

    var body: some View {
        Group {
            switch state {
            case .idle, .loading: ScrollView { SkeletonList(rows: 5) }
            case let .failed(e): ErrorStateView(message: e) { Task { await load() } }
            case let .loaded(n):
                if n.available != true { ComingSoonView(title: "新人福利", note: "暂无进行中的活动，敬请期待") }
                else { content(n) }
            }
        }
        .task { await load() }
        .onChange(of: session.isLoggedIn) { _ in Task { await load() } }
    }

    private func load() async {
        if state.value == nil { state = .loading }
        do { state = .loaded(try await HomeAPI.newcomer()) } catch { if state.value == nil { state = .failed(error.lbMessage) } }
    }

    private func content(_ n: Newcomer) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 6) {
                    Text("新人专享").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
                    HStack(alignment: .firstTextBaseline, spacing: 4) {
                        Text(n.total ?? "--").font(.lbNum(30, .bold)).foregroundColor(p.brand)
                        Text(n.asset ?? "USDT").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    }
                    Text(subtitle(n)).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                    if n.degraded == true { InlineBanner(text: "进度暂时无法读取，稍后刷新") }
                    LBButton(title: ctaTitle(n.cta), enabled: !actionBusy && n.cta != "done" && n.cta != "none", loading: actionBusy) {
                        Task { await runCta(n) }
                    }.padding(.top, 6)
                }
                .padding(16).background(RoundedRectangle(cornerRadius: Radius.xl).fill(p.fill))
                Text("任务").font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary).padding(.top, 6)
                if (n.tasks ?? []).isEmpty { EmptyStateView(text: "暂无任务") }
                ForEach(n.tasks ?? []) { t in row(t) }
            }.padding(.horizontal, Spacing.pageH).padding(.vertical, 12)
        }.refreshable { await load() }
    }

    private func subtitle(_ n: Newcomer) -> String {
        var parts: [String] = []
        if let c = n.completed, let all = n.tasks?.count { parts.append("已完成 \(c)/\(all)") }
        if let d = n.deadline { parts.append("截止 \(Fmt.dateTime(d))") } else if let days = n.days { parts.append("注册后 \(days) 天内有效") }
        if let cl = n.claimable, Dec.parse(cl) > 0 { parts.append("待领取 \(cl)") }
        return parts.isEmpty ? "完成任务领取奖励" : parts.joined(separator: " · ")
    }
    private func ctaTitle(_ c: String?) -> String {
        ["login": "登录领取", "kyc": "去认证", "enroll": "立即参与", "claim": "领取奖励", "continue": "继续任务", "done": "已完成", "none": "暂无可领取", "open": "查看详情"][c ?? ""] ?? "立即参与"
    }

    private func runCta(_ n: Newcomer) async {
        switch n.cta {
        case "login": session.present(.login)
        case "kyc": Router.shared.feature = .kyc
        case "continue", "open":
            Router.shared.feature = nil; Router.shared.tab = .spot
        case "enroll":
            guard session.isLoggedIn else { session.present(.login); return }
            await withBusy {
                guard session.isLoggedIn else { session.present(.login); return }
                try await HomeAPI.enrollNewcomer()
                ToastCenter.shared.show("报名成功", .success)
                await load()
            }
        case "claim":
            guard session.isLoggedIn else { session.present(.login); return }
            // 仅 claimable/completed 可领；claimed/done 不自动领取，避免误触发真实奖励发放
            let key = (n.tasks ?? []).first(where: { ($0.status == "claimable" || $0.status == "completed") && !($0.key ?? "").isEmpty })?.key
            guard let key else {
                ToastCenter.shared.show("暂无可领取任务", .error); return
            }
            await withBusy {
                guard session.isLoggedIn else { session.present(.login); return }
                let r = try await HomeAPI.claimNewcomerTask(key)
                let amt = (r.amount ?? "").trimmingCharacters(in: .whitespaces)
                let title = (r.title ?? "").trimmingCharacters(in: .whitespaces)
                if !amt.isEmpty {
                    ToastCenter.shared.show("已领取 \(amt)\(title.isEmpty ? "" : " · \(title)")", .success)
                } else if !title.isEmpty {
                    ToastCenter.shared.show("已领取 · \(title)", .success)
                } else {
                    ToastCenter.shared.show("领取成功", .success)
                }
                await load()
            }
        default:
            ToastCenter.shared.show("当前状态无可执行操作")
        }
    }

    private func withBusy(_ body: () async throws -> Void) async {
        guard !actionBusy else { return }
        actionBusy = true
        defer { actionBusy = false }
        do { try await body() }
        catch { ToastCenter.shared.error(error); Haptics.error() }
    }

    private func row(_ t: Newcomer.Task) -> some View {
        let target = t.target?.value ?? 0, prog = t.progress?.value ?? 0
        let done = t.status == "done" || t.status == "completed" || t.status == "claimed"
        return HStack(spacing: 10) {
            Image(systemName: done ? "checkmark.circle.fill" : "gift").font(.system(size: 20)).foregroundColor(done ? p.up : p.brand)
            VStack(alignment: .leading, spacing: 4) {
                Text(t.title ?? "").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                if target > 0 {
                    ProgressView(value: min(1, Dec.double(prog / target))).tint(p.brand)
                    Text("\(Fmt.amount(prog, 0)) / \(Fmt.amount(target, 0))").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }
            Spacer()
            Text("+\(t.amount ?? "0")").font(.lbNum(FontSize.label, .semibold)).foregroundColor(p.brand)
        }
        .padding(12).background(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border))
    }
}
