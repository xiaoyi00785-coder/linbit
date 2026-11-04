import SwiftUI

// MARK: - 合伙人计划：申请 / 审核中 / 已拒绝 / 合伙人看板（返佣汇总、推广链接、佣金明细、团队）
enum PartnerAPI {
    struct Apply: Encodable { let contact: String; let channel: String?; let scale: String? }
    static func me() async throws -> J { try await APIClient.shared.get("/partner/me", auth: true) }
    static func rules() async throws -> J { try await APIClient.shared.get("/partner/rules", auth: true) }
    static func summary() async throws -> J { try await APIClient.shared.get("/partner/summary", auth: true) }
    static func links() async throws -> J { try await APIClient.shared.get("/partner/links", auth: true) }
    static func commissions(_ kind: String?, page: Int = 1) async throws -> J {
        try await APIClient.shared.get("/partner/commissions", query: ["kind": kind, "page": String(page)], auth: true)
    }
    static func team(_ type: String) async throws -> J { try await APIClient.shared.get("/partner/team", query: ["type": type], auth: true) }
    static func apply(_ a: Apply) async throws -> J { try await APIClient.shared.post("/partner/apply", body: a) }
}

/// 截图演示：partner:<none|apply|apply-error|pending|rejected|active|commissions|team|empty|error>
enum PartnerDemo {
    static var state: String? { DemoShot.state("partner") }
    static let rules: J = ["tiers": [["key": "t1", "name": "LV1 合伙人", "minVolume30d": "0", "futuresRate": "0.3", "spotRate": "0.3"],
                                    ["key": "t2", "name": "LV2 合伙人", "minVolume30d": "5000000", "futuresRate": "0.4", "spotRate": "0.35"],
                                    ["key": "t3", "name": "LV3 合伙人", "minVolume30d": "20000000", "futuresRate": "0.5", "spotRate": "0.4"]],
                          "overrideRate": "0.1", "settlement": "daily", "payoutAsset": "USDT"]
    static func me(_ st: String) -> J {
        switch st {
        case "pending": return ["uid": "10086", "status": "pending", "partner": ["status": "pending", "appliedAt": J(DemoShot.nowMs - 3_600_000)]]
        case "rejected": return ["uid": "10086", "status": "rejected", "partner": ["status": "rejected", "reviewNote": "推广渠道信息不足，请补充社群规模与链接", "appliedAt": J(DemoShot.nowMs - 86_400_000)]]
        case "none", "apply", "apply-error": return ["uid": "10086", "status": "none"]
        default: return ["uid": "10086", "status": "active", "tier": "t2", "tierName": "LV2 合伙人", "futuresRate": "0.4", "spotRate": "0.35"]
        }
    }
    static let summary: J = ["tier": "t2", "tierName": "LV2 合伙人", "futuresRate": "0.4", "spotRate": "0.35", "volume30d": "8253100.52",
                            "nextTier": ["tier": "t3", "name": "LV3 合伙人", "minVolume30d": "20000000", "gap": "11746899.48"],
                            "directs": 128, "directPartners": 6, "unsettledEstimate": "86.32", "yesterday": "152.40", "month": "3281.66",
                            "total": "28640.18", "overrideTotal": "1820.50", "pendingPayout": "86.32", "payoutAsset": "USDT"]
    static let links: J = ["code": "LB8K2Q", "url": "https://linbit.com/r/LB8K2Q", "registerUrl": "https://linbit.com/register?ref=LB8K2Q"]
    static func commissions(_ empty: Bool) -> J {
        if empty { return ["items": []] }
        let kinds = ["futures", "spot", "override", "referral", "futures", "futures"]
        return ["items": J(kinds.enumerated().map { i, k -> J in
            ["settlementId": J(100 + i), "sourceUid": "1\(2000 + i * 37)", "kind": .str(k), "rate": .str(k == "override" ? "0.1" : "0.4"),
             "base": "\(120 + i * 31).5", "amount": "\(48 + i * 9).12", "day": "2026-11-0\(3 - i % 3)", "asset": "USDT"] })]
    }
    static func team(_ empty: Bool) -> J {
        if empty { return ["items": []] }
        return ["items": J((0..<6).map { i -> J in
            ["uid": "1\(3000 + i * 71)", "joinedAt": J(DemoShot.nowMs - Int64(i + 1) * 86_400_000 * 3), "kycLevel": J(i % 3), "isPartner": .bool(i == 1),
             "futuresVolume30d": "\(320_000 - i * 41_000)", "fee30d": "\(160 - i * 20).4", "commission": "\(64 - i * 8).16"] })]
    }
}

struct PartnerView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    @State private var me: Loadable<J> = .loading
    @State private var rules: J = .null
    @State private var applying = PartnerDemo.state?.hasPrefix("apply") == true

    var body: some View {
        VStack(spacing: 0) {
            NavBar(title: "合伙人计划", onBack: onBack)
            switch me {
            case .idle, .loading: Spacer(); ProgressView(); Spacer()
            case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
            case let .loaded(m):
                let st = m["partner"]["status"].s ?? m["status"].s ?? "none"
                if st == "active" { PartnerDashboard(me: m) }
                else { landing(m, status: st) }
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .lbSheet($applying) { PartnerApplySheet(onClose: { applying = false }) { applying = false; Task { await load() } } }
    }

    private func landing(_ m: J, status: String) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("成为 Linbit 合伙人").font(.lb(22, .bold)).foregroundColor(p.textPrimary)
                    Text("邀请好友交易，最高享 \(maxRate) 手续费返佣，每日结算").font(.lb(FontSize.body)).foregroundColor(p.textSecondary)
                }
                switch status {
                case "pending":
                    statusBox("clock.fill", p.brand, "申请审核中", "提交于 \(m["partner"]["appliedAt"].time)，预计 1-3 个工作日内完成审核")
                case "rejected":
                    statusBox("xmark.octagon.fill", p.down, "申请未通过", m["partner"]["reviewNote"].s ?? "请完善资料后重新申请")
                case "disabled":
                    statusBox("nosign", p.down, "合伙人资格已停用", "如有疑问请联系客服")
                default: EmptyView()
                }
                Text("返佣等级").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                VStack(spacing: 0) {
                    HStack {
                        Text("等级").frame(maxWidth: .infinity, alignment: .leading)
                        Text("30 日交易量").frame(maxWidth: .infinity, alignment: .trailing)
                        Text("合约 / 现货").frame(maxWidth: .infinity, alignment: .trailing)
                    }.font(.lb(FontSize.small)).foregroundColor(p.textTertiary).padding(.bottom, 8)
                    ForEach(Array(rules["tiers"].a.enumerated()), id: \.offset) { _, t in
                        HStack {
                            Text(t["name"].s ?? "--").font(.lb(FontSize.small, .medium)).foregroundColor(p.textPrimary).frame(maxWidth: .infinity, alignment: .leading)
                            Text("≥ \(jAmt(t["minVolume30d"], 0))").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary).frame(maxWidth: .infinity, alignment: .trailing)
                            Text("\(jRatio(t["futuresRate"], 0)) / \(jRatio(t["spotRate"], 0))").font(.lbNum(FontSize.small, .medium)).foregroundColor(p.up).frame(maxWidth: .infinity, alignment: .trailing)
                        }.frame(height: 36)
                    }
                }.lbCard()
                InfoNote(text: "另享直属下级合伙人返佣的 \(jRatio(rules["overrideRate"], 0)) 额外奖励；佣金每日 UTC 结算，以 \(rules["payoutAsset"].s ?? "USDT") 发放至资金账户。")
                if status == "none" || status == "rejected" {
                    LBButton(title: status == "rejected" ? "重新申请" : "立即申请") { Session.shared.requireLogin { applying = true } }
                } else if status == "pending" {
                    LBButton(title: "审核中", enabled: false) {}
                }
            }.padding(Spacing.pageH)
        }
    }

    private var maxRate: String {
        let m = rules["tiers"].a.map { $0["futuresRate"].d }.max() ?? 0
        return m > 0 ? Fmt.amount(m * 100, 0) + "%" : "50%"
    }

    private func statusBox(_ icon: String, _ c: Color, _ t: String, _ sub: String) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: icon).font(.system(size: 20)).foregroundColor(c)
            VStack(alignment: .leading, spacing: 4) {
                Text(t).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Text(sub).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }.padding(14).background(RoundedRectangle(cornerRadius: Radius.lg).fill(c.opacity(0.08)))
    }

    private func load() async {
        if let st = PartnerDemo.state {
            rules = PartnerDemo.rules
            me = st == "error" ? .failed("网络连接失败，请检查网络") : .loaded(PartnerDemo.me(st)); return
        }
        guard Session.shared.isLoggedIn else { rules = (try? await PartnerAPI.rules()) ?? .null; me = .loaded(["status": "none"]); return }
        if me.value == nil { me = .loading }
        async let r = try? PartnerAPI.rules()
        do { me = .loaded(try await PartnerAPI.me()) } catch { me = .failed(securityMessage(error)) }
        rules = await r ?? rules
    }
}

struct PartnerApplySheet: View {
    @Environment(\.palette) private var p
    let onClose: () -> Void
    let onDone: () -> Void
    private static let channels = ["社群", "自媒体", "KOL", "机构", "其他"]
    private static let scales = ["< 1,000", "1,000 - 10,000", "10,000 - 100,000", "> 100,000"]
    @State private var contact = PartnerDemo.state?.hasPrefix("apply") == true ? "@linbit_kol" : ""
    @State private var channel = 0
    @State private var scale = 1
    @State private var loading = false
    @State private var error: String? = PartnerDemo.state == "apply-error" ? ErrorText.map["PARTNER_EXISTS"] : nil

    var body: some View {
        LBSheet(title: "申请成为合伙人", showClose: true, onClose: onClose) {
            VStack(alignment: .leading, spacing: 14) {
                if let error { InlineBanner(text: error) }
                LBTextField(label: "联系方式", placeholder: "Telegram / 邮箱 / 微信", text: $contact,
                            error: contact.isEmpty || contact.count >= 3 ? nil : "请填写有效的联系方式")
                Text("推广渠道").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                ChipBar(items: Self.channels, selected: $channel)
                Text("用户规模").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                PillSegment(items: Self.scales, selected: $scale)
                LBButton(title: "提交申请", enabled: contact.trimmingCharacters(in: .whitespaces).count >= 3, loading: loading) { Task { await submit() } }
            }.padding(.horizontal, Spacing.pageH).padding(.bottom, 16)
        }
    }

    private func submit() async {
        error = nil
        if PartnerDemo.state != nil { return }
        loading = true; defer { loading = false }
        do {
            _ = try await PartnerAPI.apply(.init(contact: contact.trimmingCharacters(in: .whitespaces), channel: Self.channels[channel], scale: Self.scales[scale]))
            Haptics.success(); ToastCenter.shared.show("申请已提交", .success); onDone()
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

struct PartnerDashboard: View {
    @Environment(\.palette) private var p
    let me: J
    private static let kinds: [(String?, String)] = [(nil, "全部"), ("futures", "合约"), ("spot", "现货"), ("override", "团队奖励"), ("referral", "邀请")]
    @State private var tab = PartnerDemo.state == "team" ? 1 : 0
    @State private var kind = 0
    @State private var summary: J = .null
    @State private var links: J = .null
    @State private var list: Loadable<[J]> = .loading

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                linkCard
                PrimaryTabs(items: ["佣金明细", "我的团队"], selected: $tab, size: FontSize.label, underline: true)
                if tab == 0 { ChipBar(items: Self.kinds.map(\.1), selected: $kind) }
                switch list {
                case .idle, .loading: ProgressView().frame(maxWidth: .infinity).padding(.vertical, 40)
                case let .failed(m): ErrorStateView(message: m) { Task { await loadList() } }.frame(height: 240)
                case let .loaded(items) where items.isEmpty:
                    EmptyStateView(text: tab == 0 ? "暂无佣金记录，分享邀请链接开始赚取返佣" : "暂无团队成员").frame(height: 240)
                case let .loaded(items):
                    LazyVStack(spacing: 0) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                            if tab == 0 { commissionRow(it) } else { teamRow(it) }
                            Divider().overlay(p.border)
                        }
                    }
                }
            }.padding(Spacing.pageH)
        }
        .refreshable { await loadAll() }
        .task { await loadAll() }
        .onChange(of: tab) { _ in Task { await loadList() } }
        .onChange(of: kind) { _ in Task { await loadList() } }
    }

    private var asset: String { summary["payoutAsset"].s ?? "USDT" }

    private var header: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(summary["tierName"].s ?? me["tierName"].s ?? "合伙人").font(.lb(FontSize.small, .semibold)).foregroundColor(p.textOnPrimary)
                    .padding(.horizontal, 8).frame(height: 22).background(Capsule().fill(p.brand))
                Spacer()
                Text("合约 \(jRatio(summary["futuresRate"], 0)) · 现货 \(jRatio(summary["spotRate"], 0))").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
            }
            Text("累计返佣（\(asset)）").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Text(jAmt(summary["total"])).font(.lbNum(28, .bold)).foregroundColor(p.textPrimary)
            HStack(spacing: 10) {
                StatTile(title: "昨日", value: jAmt(summary["yesterday"]), color: p.up)
                StatTile(title: "本月", value: jAmt(summary["month"]))
                StatTile(title: "待结算", value: jAmt(summary["unsettledEstimate"]))
            }
            HStack(spacing: 10) {
                StatTile(title: "直属邀请", value: summary["directs"].s ?? "--")
                StatTile(title: "下级合伙人", value: summary["directPartners"].s ?? "--")
                StatTile(title: "团队奖励", value: jAmt(summary["overrideTotal"]))
            }
            if !summary["nextTier"].isNull {
                let next = summary["nextTier"]
                let need = next["minVolume30d"].d, gap = next["gap"].d
                let prog = need > 0 ? max(0, min(1, NSDecimalNumber(decimal: (need - gap) / need).doubleValue)) : 1
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text("距 \(next["name"].s ?? "下一等级") 还差 \(jAmt(next["gap"], 0)) USDT 交易量").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                        Spacer()
                    }
                    ProgressView(value: prog).tint(p.brand)
                }
            }
        }.lbCard()
    }

    private var linkCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("我的推广链接").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
            row("邀请码", links["code"].s)
            row("邀请链接", links["registerUrl"].s ?? links["url"].s)
        }.lbCard()
    }

    private func row(_ k: String, _ v: String?) -> some View {
        HStack {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v ?? "--").font(.lbNum(FontSize.small, .medium)).foregroundColor(p.textPrimary).lineLimit(1).truncationMode(.middle)
            Button {
                guard let v else { return }
                UIPasteboard.general.string = v; Haptics.success(); ToastCenter.shared.show("已复制", .success)
            } label: { Image(systemName: "doc.on.doc").font(.system(size: 13)).foregroundColor(p.brand) }
        }
    }

    private func kindName(_ k: String?) -> String {
        ["futures": "合约返佣", "spot": "现货返佣", "override": "团队奖励", "referral": "邀请奖励"][k ?? ""] ?? "返佣"
    }

    private func commissionRow(_ it: J) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(kindName(it["kind"].s)).font(.lb(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                Text("UID \(it["sourceUid"].s ?? "--") · 比例 \(jRatio(it["rate"], 0))").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text("+\(jAmt(it["amount"], 4)) \(it["asset"].s ?? asset)").font(.lbNum(FontSize.body, .semibold)).foregroundColor(p.up)
                Text(it["day"].time).font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
        }.frame(height: 60)
    }

    private func teamRow(_ it: J) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text("UID \(it["uid"].s ?? "--")").font(.lbNum(FontSize.body, .medium)).foregroundColor(p.textPrimary)
                    if it["isPartner"].b {
                        Text("合伙人").font(.lb(10)).foregroundColor(p.brand).padding(.horizontal, 4).frame(height: 16)
                            .background(RoundedRectangle(cornerRadius: 3).fill(p.brand.opacity(0.12)))
                    }
                    Text("KYC Lv.\(it["kycLevel"].s ?? "0")").font(.lb(10)).foregroundColor(p.textSecondary).padding(.horizontal, 4).frame(height: 16)
                        .background(RoundedRectangle(cornerRadius: 3).fill(p.fill))
                }
                Text("加入 \(it["joinedAt"].time)").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text("+\(jAmt(it["commission"])) USDT").font(.lbNum(FontSize.body, .semibold)).foregroundColor(p.up)
                Text("30 日合约量 \(jAmt(it["futuresVolume30d"], 0))").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
        }.frame(height: 60)
    }

    private func loadAll() async {
        if PartnerDemo.state != nil { summary = PartnerDemo.summary; links = PartnerDemo.links; await loadList(); return }
        async let s = try? PartnerAPI.summary()
        async let l = try? PartnerAPI.links()
        await loadList()
        summary = await s ?? summary; links = await l ?? links
    }

    private func loadList() async {
        if let st = PartnerDemo.state {
            list = .loaded((tab == 0 ? PartnerDemo.commissions(st == "empty") : PartnerDemo.team(st == "empty")).items); return
        }
        if list.value == nil { list = .loading }
        do {
            let r = tab == 0 ? try await PartnerAPI.commissions(Self.kinds[kind].0) : try await PartnerAPI.team("all")
            list = .loaded(r.items)
        } catch { list = .failed(securityMessage(error)) }
    }
}
