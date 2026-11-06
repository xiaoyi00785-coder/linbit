import SwiftUI

/// 创建 / 编辑价格预警表单（契约：spot · INDEX · once · in_app；threshold 十进制原样正数字符串）。
/// 取消不写；失败保留字段；busy 防重复提交；DEBUG+shot 走本地 mock，Release 走正式 API。
struct PriceAlertEditorSheet: View {
    enum Mode {
        case create
        case edit(PriceAlert)
    }

    @Environment(\.palette) private var p
    let mode: Mode
    let session: PriceAlertsSession
    /// 当前列表已加载条数（active 粗估）；上限取自契约 enum 或条目 limit
    var loadedActiveCount: Int = 0
    var activeCap: Int = PriceAlertsFeature.activePerUserCap
    let onDone: (Bool) -> Void

    @State private var symbol: String = "BTCUSDT"
    @State private var condition: String = "gte" // gte | lte
    @State private var threshold: String = ""
    @State private var note: String = ""
    @State private var enabled: Bool = true
    @State private var revision: Int64 = 1
    @State private var alertId: String = ""
    @State private var busy = false
    @State private var formError: String? = nil
    @State private var showConfirm = false

    private var isEdit: Bool {
        if case .edit = mode { return true }
        return false
    }

    private var title: String { isEdit ? "编辑预警" : "创建预警" }

    /// 阈值：过滤后原样保留用户输入；必须为正数（>0）
    private var thresholdValid: Bool {
        let t = threshold.trimmingCharacters(in: .whitespaces)
        guard !t.isEmpty, Dec.parse(t) > 0 else { return false }
        // 拒绝科学计数/符号；NumberBox 已滤非数字与多点
        return t.first != "-" && t.first != "+" && !t.lowercased().contains("e")
    }

    private var symbolValid: Bool {
        let s = symbol.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
        return s.count >= 6 && s.count <= 20 && s.allSatisfy { $0.isLetter || $0.isNumber }
    }

    private var canSubmit: Bool {
        !busy && thresholdValid && symbolValid && !session.isRateLimited
    }

    var body: some View {
        LBSheet(title: title, showClose: true, onClose: { onDone(false) }) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    InfoNote(text: "现货 · 价格源 INDEX · 一次性触发 · 仅站内信。阈值按输入原样提交（正数十进制）。每账户有效上限 \(activeCap)（契约 activePerUser）。")

                    if session.isRateLimited {
                        InlineBanner(text: "写操作限流，请 \(session.rateLimitRemainingSec) 秒后再试（Retry-After）")
                    }

                    if !isEdit, loadedActiveCount >= activeCap {
                        InlineBanner(text: "有效预警已达契约上限 \(activeCap) 条，请先删除或停用后再创建")
                    }

                    fieldLabel("交易对")
                    symbolField

                    fieldLabel("方向")
                    conditionPicker

                    NumberBox(label: "触发价（INDEX）", unit: "USDT", text: $threshold, onEdit: { formError = nil })

                    if isEdit {
                        Toggle(isOn: $enabled) {
                            Text("启用").font(.lb(FontSize.label)).foregroundColor(p.textPrimary)
                        }
                        .tint(p.brand)
                        Text("revision \(revision)（乐观锁；冲突时将刷新列表）")
                            .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    }

                    LBTextField(label: "备注（可选）", placeholder: "最多 120 字", text: $note, keyboard: .default)
                        .onChange(of: note) { v in
                            if v.count > 120 { note = String(v.prefix(120)) }
                        }

                    if let formError {
                        Text(formError).font(.lb(FontSize.small)).foregroundColor(p.down)
                    }

                    HStack(spacing: 10) {
                        LBButton(title: "取消", kind: .secondary, enabled: !busy) {
                            onDone(false) // 取消无写
                        }
                        LBButton(
                            title: isEdit ? "保存" : "创建",
                            enabled: canSubmit && (isEdit || loadedActiveCount < activeCap),
                            loading: busy
                        ) {
                            showConfirm = true
                        }
                    }
                }
                .padding(.horizontal, Spacing.pageH)
                .padding(.bottom, 28)
            }
        }
        .onAppear(perform: seed)
        .lbSheet($showConfirm) {
            confirmSheet
        }
    }

    private var symbolField: some View {
        HStack {
            TextField("如 BTCUSDT", text: Binding(get: { symbol }, set: { v in
                symbol = v.uppercased().filter { $0.isLetter || $0.isNumber }
                formError = nil
            }))
            .font(.lbNum(FontSize.body)).foregroundColor(isEdit ? p.textTertiary : p.textPrimary)
            .disabled(isEdit) // 编辑不可改币对（契约 Update 无 symbol）
            Text("spot").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
        }
        .padding(.horizontal, 10).frame(minHeight: 44)
        .background(RoundedRectangle(cornerRadius: Radius.sm).fill(p.fill))
    }

    private var conditionPicker: some View {
        HStack(spacing: 8) {
            condChip("价格 ≥ 触发价", "gte")
            condChip("价格 ≤ 触发价", "lte")
        }
    }

    private func condChip(_ title: String, _ value: String) -> some View {
        Button {
            condition = value
            formError = nil
        } label: {
            Text(title)
                .font(.lb(FontSize.small, condition == value ? .medium : .regular))
                .foregroundColor(condition == value ? p.textOnPrimary : p.textPrimary)
                .frame(maxWidth: .infinity).frame(height: 36)
                .background(RoundedRectangle(cornerRadius: Radius.sm).fill(condition == value ? p.primary : p.fill))
        }
        .buttonStyle(PressableStyle())
    }

    private func fieldLabel(_ t: String) -> some View {
        Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
    }

    @ViewBuilder private var confirmSheet: some View {
        LBSheet(title: "确认\(isEdit ? "保存" : "创建")", showClose: true, onClose: { showConfirm = false }) {
            VStack(alignment: .leading, spacing: 12) {
                Text("\(symbol) · spot · INDEX · once · 站内信")
                    .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                Text("\(condition == "gte" ? "≥" : "≤") \(threshold)")
                    .font(.lbNum(FontSize.title)).foregroundColor(p.textPrimary)
                if isEdit {
                    Text("revision \(revision)").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }
                Text("提交后不可撤销本次网络请求；DEBUG 截图夹具仅本地 mock，不写生产。")
                    .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                HStack(spacing: 10) {
                    LBButton(title: "返回修改", kind: .secondary, enabled: !busy) { showConfirm = false }
                    LBButton(title: "确认提交", enabled: !busy && !session.isRateLimited, loading: busy) { Task { await submit() } }
                }
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.bottom, 24)
        }
    }

    private func seed() {
        switch mode {
        case .create:
            if PriceAlertDemo.on {
                symbol = "BTCUSDT"
                threshold = "80000.00"
                condition = "gte"
            }
        case let .edit(a):
            alertId = a.alertId
            symbol = a.symbol
            condition = a.condition
            threshold = a.threshold // 原样
            note = a.note ?? ""
            enabled = a.enabled
            revision = a.revision
        }
    }

    @MainActor
    private func submit() async {
        guard !busy else { return }
        guard !session.isRateLimited else {
            formError = "操作过于频繁，请 \(session.rateLimitRemainingSec) 秒后再试"
            showConfirm = false
            return
        }
        guard thresholdValid, symbolValid else {
            formError = "请填写有效交易对与正数触发价"
            showConfirm = false
            return
        }
        busy = true
        formError = nil
        defer { busy = false }
        do {
            if isEdit {
                let body = UpdatePriceAlertRequest(
                    revision: revision,
                    threshold: threshold,
                    condition: condition,
                    enabled: enabled,
                    channels: ["in_app"],
                    note: note
                )
                _ = try await session.updateForTestOrUI(alertId: alertId, body)
                ToastCenter.shared.show("预警已更新", .success)
            } else {
                let body = CreatePriceAlertRequest(
                    clientAlertId: "ios.\(UUID().uuidString.replacingOccurrences(of: "-", with: "").prefix(20))",
                    market: "spot",
                    symbol: symbol.trimmingCharacters(in: .whitespacesAndNewlines).uppercased(),
                    condition: condition,
                    threshold: threshold, // 十进制原样
                    priceSource: "INDEX",
                    triggerMode: "once",
                    channels: ["in_app"],
                    enabled: true,
                    note: note
                )
                _ = try await session.createForTestOrUI(body)
                ToastCenter.shared.show("预警已创建", .success)
            }
            showConfirm = false
            onDone(true)
        } catch {
            showConfirm = false // 关确认层，保留编辑表单
            if (error as? APIFailure)?.code == "SESSION_STALE" {
                // 须先于 isConflictFailure：SESSION_STALE 也用了 status 409
                formError = "登录状态已变更，未应用本次修改"
                ToastCenter.shared.show(formError ?? "", .error)
                onDone(false)
            } else if PriceAlertsFeature.isConflictFailure(error) {
                formError = ErrorText.map["REVISION_CONFLICT"] ?? "数据已变更，请刷新后重试"
                ToastCenter.shared.show(formError ?? "", .error)
                await session.reload() // revision 冲突刷新列表
                // 失败保留编辑：若同 alert 仍在列表，抬升本地 revision，避免用旧锁连撞
                if isEdit, case let .loaded(pg) = session.page,
                   let fresh = pg.items.first(where: { $0.alertId == alertId }) {
                    revision = fresh.revision
                    enabled = fresh.enabled
                    if threshold.isEmpty { threshold = fresh.threshold }
                }
            } else if let f = error as? APIFailure, f.status == 429 {
                formError = "操作过于频繁，请 \(session.rateLimitRemainingSec) 秒后再试"
                ToastCenter.shared.show(formError ?? "", .error)
            } else if PriceAlertsFeature.isDisabledFailure(error) {
                formError = ErrorText.map["FEATURE_DISABLED"] ?? "该功能暂未开放"
                ToastCenter.shared.show(formError ?? "", .error)
            } else {
                formError = error.lbMessage
                ToastCenter.shared.error(error)
            }
            // 失败保留 symbol/condition/threshold/note/revision
        }
    }
}

/// 删除确认（revision 乐观锁；取消无写；DEBUG mock 拦截远端）
struct PriceAlertDeleteSheet: View {
    @Environment(\.palette) private var p
    let alert: PriceAlert
    let session: PriceAlertsSession
    let onDone: (Bool) -> Void
    @State private var busy = false
    @State private var err: String? = nil

    var body: some View {
        LBSheet(title: "删除预警", showClose: true, onClose: { onDone(false) }) {
            VStack(alignment: .leading, spacing: 12) {
                Text("确认删除 \(alert.symbol) \(alert.condition == "gte" ? "≥" : "≤") \(alert.threshold)？")
                    .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                Text("revision \(alert.revision) · 软删除 · 重复删除幂等 204")
                    .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                if session.isRateLimited {
                    InlineBanner(text: "写操作限流，请 \(session.rateLimitRemainingSec) 秒后再试")
                }
                if let err { Text(err).font(.lb(FontSize.small)).foregroundColor(p.down) }
                HStack(spacing: 10) {
                    LBButton(title: "取消", kind: .secondary, enabled: !busy) { onDone(false) }
                    LBButton(title: "确认删除", kind: .sell, enabled: !busy && !session.isRateLimited, loading: busy) {
                        Task { await del() }
                    }
                }
            }
            .padding(.horizontal, Spacing.pageH)
            .padding(.bottom, 24)
        }
    }

    @MainActor
    private func del() async {
        guard !busy else { return }
        guard !session.isRateLimited else {
            err = "操作过于频繁，请 \(session.rateLimitRemainingSec) 秒后再试"
            return
        }
        busy = true
        defer { busy = false }
        do {
            try await session.deleteForTestOrUI(alertId: alert.alertId, revision: alert.revision)
            ToastCenter.shared.show("已删除", .success)
            onDone(true)
        } catch {
            if (error as? APIFailure)?.code == "SESSION_STALE" {
                err = "登录状态已变更，未应用本次删除"
                ToastCenter.shared.show(err ?? "", .error)
                onDone(false)
                return
            } else if PriceAlertsFeature.isConflictFailure(error) {
                err = ErrorText.map["REVISION_CONFLICT"] ?? "数据已变更，请刷新后重试"
                await session.reload()
            } else if let f = error as? APIFailure, f.status == 429 {
                err = "操作过于频繁，请 \(session.rateLimitRemainingSec) 秒后再试"
            } else {
                err = error.lbMessage
            }
            ToastCenter.shared.show(err ?? error.lbMessage, .error)
            // 失败保留本确认页，用户可取消或再试
        }
    }
}
