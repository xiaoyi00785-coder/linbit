import SwiftUI
import UIKit

// MARK: - 点击态：按下变暗 + 轻微缩放（全局统一）
struct PressableStyle: ButtonStyle {
    var scale: CGFloat = 0.98
    func makeBody(configuration: ButtonStyleConfiguration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.6 : 1)
            .scaleEffect(configuration.isPressed ? scale : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

/// 行点击态：按下时整行灰底（列表行、Sheet 选项）
struct RowPressStyle: ButtonStyle {
    @Environment(\.palette) private var p
    func makeBody(configuration: ButtonStyleConfiguration) -> some View {
        configuration.label
            .background(configuration.isPressed ? p.fill : Color.clear)
            .contentShape(Rectangle())
    }
}

// MARK: - 主按钮：黑底（夜间白底）/ 买绿 / 卖红 / 次级灰；禁用灰；加载转圈
struct LBButton: View {
    enum Kind { case primary, secondary, buy, sell, outline }
    @Environment(\.palette) private var p
    let title: String
    var kind: Kind = .primary
    var height: CGFloat = 44
    var enabled = true
    var loading = false
    var subtitle: String? = nil
    let action: () -> Void

    var body: some View {
        Button {
            guard enabled, !loading else { return }
            Haptics.tap()
            action()
        } label: {
            ZStack {
                if loading {
                    ProgressView().tint(fg)
                } else {
                    VStack(spacing: 1) {
                        Text(title).font(.lb(FontSize.body, .medium))
                        if let subtitle { Text(subtitle).font(.lb(FontSize.small - 1)).opacity(0.8) }
                    }
                }
            }
            .foregroundColor(fg)
            .frame(maxWidth: .infinity).frame(height: height)
            .background(RoundedRectangle(cornerRadius: Radius.md).fill(bg))
            .overlay(kind == .outline ? RoundedRectangle(cornerRadius: Radius.md).stroke(p.border) : nil)
        }
        .buttonStyle(PressableStyle())
        .disabled(!enabled)
    }

    private var bg: Color {
        guard enabled else { return p.fill }
        switch kind {
        case .primary: return p.primary
        case .secondary: return p.fill
        case .buy: return p.up
        case .sell: return p.down
        case .outline: return .clear
        }
    }
    private var fg: Color {
        guard enabled else { return p.textTertiary }
        switch kind {
        case .primary: return p.textOnPrimary
        case .secondary, .outline: return p.textPrimary
        case .buy, .sell: return .white
        }
    }
}

enum Haptics {
    static func tap() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
    static func success() { UINotificationFeedbackGenerator().notificationOccurred(.success) }
    static func error() { UINotificationFeedbackGenerator().notificationOccurred(.error) }
}

// MARK: - Toast（居中黑色半透明胶囊，2 秒消失；支持 成功/失败/普通）
@MainActor
final class ToastCenter: ObservableObject {
    static let shared = ToastCenter()
    enum Kind: Equatable { case info, success, error }
    struct Item: Equatable, Identifiable { let id = UUID(); let text: String; let kind: Kind }
    @Published var current: Item?
    private var hideTask: Task<Void, Never>?

    func show(_ text: String, _ kind: Kind = .info) {
        if kind == .error { Haptics.error() } else if kind == .success { Haptics.success() }
        current = Item(text: text, kind: kind)
        hideTask?.cancel()
        hideTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if !Task.isCancelled { self?.current = nil }
        }
    }
    func error(_ e: Error) { show(e.lbMessage, .error) }
}

struct ToastOverlay: View {
    @ObservedObject var center = ToastCenter.shared
    var body: some View {
        ZStack {
            if let t = center.current {
                HStack(spacing: 6) {
                    switch t.kind {
                    case .success: Image(systemName: "checkmark.circle.fill")
                    case .error: Image(systemName: "exclamationmark.circle.fill")
                    case .info: EmptyView()
                    }
                    Text(t.text).font(.lb(FontSize.label)).multilineTextAlignment(.center)
                }
                .foregroundColor(.white)
                .padding(.horizontal, 16).padding(.vertical, 11)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(Color.black.opacity(0.82)))
                .padding(.horizontal, 48)
                .transition(.opacity.combined(with: .scale(scale: 0.95)))
                .id(t.id)
            }
        }
        .animation(.easeOut(duration: 0.18), value: center.current)
        .allowsHitTesting(false)
    }
}

extension Error {
    /// 统一错误文案：服务端 message 优先，否则按类型兜底
    var lbMessage: String {
        if let f = self as? APIFailure {
            switch f {
            case let .server(status, e, _):
                if status == 401 || e?.code == "AUTH_UNAUTHORIZED" {
                    return TokenStore.shared.sessionExpired ? "登录已过期，请重新登录" : ErrorText.loginRequired
                }
                if let e { return ErrorText.map[e.code] ?? e.message }
                return status >= 500 ? "服务繁忙，请稍后再试" : "请求失败（\(status)）"
            case .transport: return "网络连接失败，请检查网络"
            case .decoding: return "数据异常，请稍后再试"
            }
        }
        return localizedDescription
    }
}

enum ErrorText {
    static let loginRequired = "登录后查看"
    static let map: [String: String] = [
        "INTERNAL_TRANSFER_DISABLED": "内部转账暂未开放",
        "RECIPIENT_NOT_FOUND": "收款人不存在，请核对 UID 或邮箱",
        "SELF_TRANSFER": "不能向自己转账",
        "RECIPIENT_SUB_ACCOUNT": "不支持向子账户内部转账",
        "RECIPIENT_UNAVAILABLE": "收款人账户状态异常，暂无法收款",
        "INTERNAL_TRANSFER_LIMIT_EXCEEDED": "已超过 24 小时内部转账额度",
        "TWOFA_REQUIRED": "请先绑定并完成二次验证",
        "INVALID_TOTP": "验证码错误",
        "WITHDRAW_LOCKED": "安全设置变更后 24 小时内暂不可提现/转账",
        "FUND_PASSWORD_REQUIRED": "请输入资金密码",
        "FUND_PASSWORD_INVALID": "资金密码错误",
        "FUND_PASSWORD_LOCKED": "资金密码错误次数过多，已临时锁定",
        "FUND_PASSWORD_NOT_SET": "请先设置资金密码",
        "MIN_WITHDRAW": "低于最小提现数量",
        "WITHDRAW_LIMIT_EXCEEDED": "超过 24 小时提现额度",
        "ADDRESS_EXISTS": "该地址已在地址簿中",
        "WITHDRAW_NOT_CANCELABLE": "该提现已进入处理，无法撤销",
        "AUTH_UNAUTHORIZED": "登录已过期，请重新登录",
        "AUTH_INVALID_CREDENTIALS": "账户或密码错误",
        "AUTH_EMAIL_EXISTS": "该邮箱已被注册",
        "AUTH_PHONE_EXISTS": "该手机号已被注册",
        "CODE_INVALID": "验证码错误或已过期",
        "CODE_TOO_MANY_ATTEMPTS": "尝试次数过多，请稍后再试",
        "TOTP_INVALID": "动态码错误",
        "RESET_TOKEN_INVALID": "重置令牌已过期，请重新获取验证码",
        "RATE_LIMITED": "操作过于频繁，请稍后再试",
        "INSUFFICIENT_BALANCE": "可用余额不足",
        "NO_LIQUIDITY": "当前盘口无对手单，暂无法市价成交",
        "IDEMPOTENCY_CONFLICT": "重复提交，请勿重复下单",
        "CONDITIONAL_LIMIT_EXCEEDED": "条件单数量已达上限，请先撤销部分条件单",
        "TRIGGER_WOULD_IMMEDIATELY_FIRE": "触发价将立即触发，请调整触发价",
        "INSUFFICIENT_MARGIN": "保证金不足",
        "INVALID_AMOUNT": "数量不正确",
        "AMOUNT_TOO_SMALL": "低于最小数量",
        "INVALID_EMAIL": "邮箱地址无效或无法接收邮件，请检查后重试",
        "LOGOUT_INCOMPLETE": "退出未完成，请重试",
        "EMAIL_UNAVAILABLE": "邮件服务暂不可用，请稍后再试",
        "SMS_UNAVAILABLE": "短信服务暂不可用，请稍后再试",
        "STAKE_OUT_OF_RANGE": "投注金额超出范围",
        "DAILY_LIMIT_EXCEEDED": "已超过今日投注上限",
        "EXPOSURE_LIMIT": "当前方向敞口已满，请稍后再试",
        "SYMBOL_PAUSED": "该产品暂停交易",
        "INDEX_UNAVAILABLE": "指数价格暂不可用，请稍后再试",
        "FEATURE_DISABLED": "该功能暂未开放",
        "PAIR_NOT_SUPPORTED": "暂不支持该币对兑换",
        "AMOUNT_TOO_LARGE": "超过单笔兑换上限",
        "PRICE_UNAVAILABLE": "暂无可用报价，请稍后再试",
        "PAIR_PAUSED_DEPEG": "USDC 价格偏离，兑换暂停",
        "QUOTE_NOT_FOUND": "报价不存在，请重新询价",
        "QUOTE_EXPIRED": "报价已过期，请重新询价",
        "SAME_ACCOUNT": "转出与转入账户相同",
        "ACCOUNT_NOT_SUPPORTED": "该账户不支持划转",
        "SUB_ACCOUNT_FROZEN": "子账户已冻结",
        "SUB_ACCOUNT_LIMIT": "子账户数量已达上限",
        "USERNAME_TAKEN": "用户名已被占用",
        "CHAT_RATE_LIMITED": "发言过于频繁，请稍后再试",
        "CHAT_MUTED": "你已被禁言",
        "CHAT_KYC_REQUIRED": "完成身份认证后才能发言",
        "CHAT_BANNED": "你已被禁止发言",
        "CHAT_TOO_LONG": "消息过长",
        "CHAT_LINK_BLOCKED": "不允许发送链接",
        "CHAT_CONTACT_BLOCKED": "不允许发送联系方式",
        "CHAT_WORD_BLOCKED": "消息包含敏感词",
        "CHAT_UNAVAILABLE": "聊天室暂不可用",
        "KYC_REQUIRED": "请先完成身份认证",
        "INSUFFICIENT_OWN_FUNDS": "合约账户自有资金不足，请先划转资金后再激活",
        "COUPON_NOT_FOUND": "卡券不存在",
        "COUPON_NOT_ACTIVATABLE": "该卡券已激活或已过期，无法激活",
        "SECURITY_EMAIL_REQUIRED": "安全通知邮件不可关闭",
        "UNDERAGE": "未满 18 周岁，无法完成身份认证",
        "KYC_LEVEL_REQUIRED": "请先完成基础认证",
        "KYC_PENDING": "认证资料审核中，请勿重复提交",
        "ID_NUMBER_TAKEN": "该证件已被其他账户使用",
        "DOCUMENT_NOT_FOUND": "证件照片已失效，请重新上传",
        "DOCUMENT_SIDE_MISMATCH": "证件照片类型不匹配，请重新上传",
        "DOCUMENT_REQUIRED": "请上传所需的全部证件照片",
        // 合约下单 / 改单 / 条件委托 / 持仓模式
        "VALIDATION_ERROR": "参数有误，请检查后重试",
        "UNKNOWN_SYMBOL": "合约不存在",
        "INVALID_PRICE": "价格不符合精度或超出限价范围",
        "INVALID_QTY": "数量不符合要求（改单数量需大于已成交数量）",
        "INVALID_TPSL": "止盈止损价格不合理，请检查方向与触发价",
        "POSITION_LIMIT": "超过当前杠杆档位的最大持仓",
        "REDUCE_ONLY_REJECTED": "只减仓委托会增加仓位，已拒绝",
        "CONTRACT_HALTED": "该合约暂停交易",
        "POST_ONLY_REJECTED": "只做 Maker 委托会立即成交，已撤销",
        "POST_ONLY_DISABLED": "该合约暂不支持只做 Maker",
        "POSITION_SIDE_REQUIRED": "分仓模式下需指定多/空方向",
        "POSITION_SIDE_MISMATCH": "仓位方向与当前持仓模式不符",
        "ALGO_LIMIT": "条件委托数量已达上限",
        "TRIGGER_DEVIATION": "触发价偏离当前价格过大",
        "NO_CHANGE": "价格与数量均未变化",
        "AMEND_LIMIT": "该委托改单次数已达上限",
        "ORDER_NOT_OPEN": "委托已成交或已撤销，无法修改",
        "NOT_AMENDABLE": "该委托不支持修改",
        "NOT_FOUND": "委托不存在或已失效",
        "ALGO_NOT_ACTIVE": "条件委托已触发或已撤销",
        "NO_POSITION": "当前无持仓，无法设置止盈止损",
        "HEDGE_MODE_DISABLED": "分仓模式暂未开放",
        "HAS_POSITION": "存在持仓，请先全部平仓后再切换",
        "HAS_OPEN_ORDERS": "存在挂单或条件委托，请先全部撤销后再切换",
        "COPY_TRADER_ONE_WAY": "带单交易员仅支持合仓模式",
        "CONTACT_UNCHANGED": "新旧联系方式相同",
        "EMAIL_EXISTS": "该邮箱已被其他账户使用",
        "PHONE_EXISTS": "该手机号已被其他账户使用",
        "FUND_PASSWORD_EXISTS": "资金密码已设置，请使用修改或重置",
        "PASSKEY_EXISTS": "该通行密钥已绑定",
        "PASSKEY_INVALID": "通行密钥校验失败",
        "PASSKEY_LIMIT": "最多可绑定 10 个通行密钥",
        "PASSKEY_SESSION_INVALID": "通行密钥会话已过期，请重试",
        "PASSKEY_UNAVAILABLE": "通行密钥功能暂未开放",
        "TOTP_NOT_BOUND": "请先绑定身份验证器",
        "TOTP_NOT_ENABLED": "身份验证器未开启",
        "FILE_TOO_LARGE": "头像过大，请选择 2MB 以内的图片",
        "UNSUPPORTED_MEDIA_TYPE": "仅支持 JPG、PNG、WebP 格式的图片",
        "STORAGE_UNAVAILABLE": "头像服务暂不可用，请稍后再试",
        "OLD_PASSWORD_INVALID": "原密码错误",
        "PASSWORD_UNCHANGED": "新密码不能与原密码相同",
    ]
}

// MARK: - 底部弹窗（对照图 #12/#19/#45：遮罩 + 顶部圆角 16 + 灰色把手 + 标题）
struct LBSheet<Content: View>: View {
    @Environment(\.palette) private var p
    var title: String? = nil
    var subtitle: String? = nil
    var showClose = false
    let onClose: () -> Void
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Capsule().fill(p.fillStrong).frame(width: 36, height: 4).frame(maxWidth: .infinity).padding(.top, 8)
            if title != nil || showClose {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 6) {
                        if let title { Text(title).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary) }
                        if let subtitle { Text(subtitle).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary) }
                    }
                    Spacer()
                    if showClose {
                        Button(action: onClose) { Image(systemName: "xmark").font(.system(size: 15, weight: .medium)).foregroundColor(p.textSecondary) }
                    }
                }
                .padding(.horizontal, Spacing.pageH).padding(.top, 16).padding(.bottom, 12)
            }
            content
        }
        .padding(.bottom, 24)
        .background(RoundedRectangle(cornerRadius: Radius.sheet).fill(p.bgElevated).ignoresSafeArea(edges: .bottom))
    }
}

/// 自定义底部弹窗容器：遮罩点击关闭、下滑关闭、弹簧动画（iOS16 的 .sheet 无法做出对照图的遮罩与高度）
/// 当前已弹出的自绘弹窗数量；>0 时根视图隐藏底部 Tab 栏，避免遮挡弹窗主按钮。
@MainActor
final class OverlayTracker: ObservableObject {
    static let shared = OverlayTracker()
    @Published private(set) var count = 0
    func push() { count += 1 }
    func pop() { count = max(0, count - 1) }
}

private struct TrackOverlay: ViewModifier {
    func body(content: Content) -> some View {
        content
            .onAppear { OverlayTracker.shared.push() }
            .onDisappear { OverlayTracker.shared.pop() }
    }
}

struct SheetHost<Sheet: View>: ViewModifier {
    @Environment(\.palette) private var p
    @Binding var isPresented: Bool
    @ViewBuilder var sheet: () -> Sheet
    @GestureState private var drag: CGFloat = 0

    func body(content: Content) -> some View {
        ZStack(alignment: .bottom) {
            content
            if isPresented {
                p.mask.ignoresSafeArea()
                    .transition(.opacity)
                    .onTapGesture { isPresented = false }
                sheet()
                    .modifier(TrackOverlay())
                    .offset(y: max(drag, 0))
                    .gesture(DragGesture().updating($drag) { v, s, _ in s = v.translation.height }
                        .onEnded { v in if v.translation.height > 80 { isPresented = false } })
                    .transition(.move(edge: .bottom))
            }
        }
        .animation(.spring(response: 0.32, dampingFraction: 0.9), value: isPresented)
    }
}

extension View {
    func lbSheet<S: View>(_ isPresented: Binding<Bool>, @ViewBuilder _ sheet: @escaping () -> S) -> some View {
        modifier(SheetHost(isPresented: isPresented, sheet: sheet))
    }
    func lbConfirm(_ c: Binding<ConfirmRequest?>) -> some View { modifier(ConfirmHost(request: c)) }
}

/// 单选列表 Sheet 选项行（图标 + 文字 + 选中打勾，对照图 #12/#19）
struct SheetOptionRow: View {
    @Environment(\.palette) private var p
    var icon: String? = nil
    let title: String
    var subtitle: String? = nil
    let selected: Bool
    let action: () -> Void
    var body: some View {
        Button(action: { Haptics.tap(); action() }) {
            HStack(spacing: 12) {
                if let icon { Image(systemName: icon).font(.system(size: 16)).foregroundColor(p.textPrimary).frame(width: 22) }
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.lb(FontSize.body, selected ? .medium : .regular)).foregroundColor(p.textPrimary)
                    if let subtitle { Text(subtitle).font(.lb(FontSize.small)).foregroundColor(p.textSecondary) }
                }
                Spacer()
                if selected { Image(systemName: "checkmark.circle.fill").font(.system(size: 18)).foregroundColor(p.textPrimary) }
            }
            .padding(.horizontal, Spacing.pageH).frame(height: 52)
        }
        .buttonStyle(RowPressStyle())
    }
}

// MARK: - 确认框（居中卡片：标题 / 正文 / 取消 + 确认，危险操作红色）
struct ConfirmRequest: Identifiable {
    let id = UUID()
    var title: String
    var message: String
    var confirmTitle = "确认"
    var cancelTitle = "取消"
    var destructive = false
    var action: () -> Void
}

struct ConfirmHost: ViewModifier {
    @Environment(\.palette) private var p
    @Binding var request: ConfirmRequest?
    func body(content: Content) -> some View {
        ZStack {
            content
            if let r = request {
                p.mask.ignoresSafeArea().transition(.opacity)
                VStack(spacing: 0) {
                    Text(r.title).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary).padding(.top, 22)
                    Text(r.message).font(.lb(FontSize.label)).foregroundColor(p.textSecondary)
                        .multilineTextAlignment(.center).padding(.horizontal, 20).padding(.top, 10).padding(.bottom, 22)
                    HStack(spacing: 10) {
                        LBButton(title: r.cancelTitle, kind: .secondary, height: 40) { request = nil }
                        LBButton(title: r.confirmTitle, kind: r.destructive ? .sell : .primary, height: 40) {
                            request = nil; r.action()
                        }
                    }
                    .padding(.horizontal, 16).padding(.bottom, 16)
                }
                .frame(width: 300)
                .background(RoundedRectangle(cornerRadius: Radius.xl).fill(p.bgElevated))
                .modifier(TrackOverlay())
                .transition(.scale(scale: 0.92).combined(with: .opacity))
            }
        }
        .animation(.easeOut(duration: 0.18), value: request?.id)
    }
}

// MARK: - 页面加载态：加载中（骨架）/ 空 / 错误 + 重试
enum Loadable<T> {
    case idle, loading, loaded(T), failed(String)
    var value: T? { if case let .loaded(v) = self { return v }; return nil }
}

struct LoadableView<T, Content: View>: View {
    @Environment(\.palette) private var p
    let state: Loadable<T>
    var emptyText = "暂无数据"
    var isEmpty: (T) -> Bool = { _ in false }
    var skeletonRows = 6
    let retry: () -> Void
    @ViewBuilder var content: (T) -> Content

    var body: some View {
        switch state {
        case .idle, .loading: SkeletonList(rows: skeletonRows)
        case let .failed(msg): ErrorStateView(message: msg, retry: retry)
        case let .loaded(v): if isEmpty(v) { EmptyStateView(text: emptyText) } else { content(v) }
        }
    }
}

struct SkeletonList: View {
    @Environment(\.palette) private var p
    var rows = 6
    @State private var on = false
    var body: some View {
        VStack(spacing: 16) {
            ForEach(0..<rows, id: \.self) { _ in
                HStack(spacing: 10) {
                    Circle().fill(p.fill).frame(width: 32, height: 32)
                    VStack(alignment: .leading, spacing: 6) {
                        RoundedRectangle(cornerRadius: 3).fill(p.fill).frame(width: 90, height: 12)
                        RoundedRectangle(cornerRadius: 3).fill(p.fill).frame(width: 56, height: 10)
                    }
                    Spacer()
                    RoundedRectangle(cornerRadius: 3).fill(p.fill).frame(width: 70, height: 12)
                }
            }
        }
        .padding(Spacing.pageH)
        .opacity(on ? 0.45 : 1)
        .onAppear { withAnimation(.easeInOut(duration: 0.8).repeatForever()) { on = true } }
    }
}

struct ErrorStateView: View {
    @Environment(\.palette) private var p
    let message: String
    let retry: () -> Void
    @ObservedObject private var session = Session.shared
    private var needLogin: Bool { message == ErrorText.loginRequired || (message.hasPrefix("登录已过期") && !session.isLoggedIn) }
    var body: some View {
        if needLogin { loginState } else { errorState }
    }
    private var loginState: some View {
        VStack(spacing: 12) {
            Image(systemName: "person.crop.circle.badge.exclamationmark").font(.system(size: 34)).foregroundColor(p.textTertiary)
            Text(message == ErrorText.loginRequired ? "登录后查看" : message).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
            LBButton(title: "登录", kind: .primary, height: 36) { session.requireLogin { retry() } }.frame(width: 140)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 48)
        .onChange(of: session.isLoggedIn) { v in if v { retry() } }
    }
    private var errorState: some View {
        VStack(spacing: 12) {
            Image(systemName: "wifi.exclamationmark").font(.system(size: 34)).foregroundColor(p.textTertiary)
            Text(message).font(.lb(FontSize.caption)).foregroundColor(p.textSecondary).multilineTextAlignment(.center)
            Button(action: retry) {
                Text("重试").font(.lb(FontSize.label, .medium)).foregroundColor(p.textPrimary)
                    .padding(.horizontal, 22).frame(height: 32)
                    .overlay(RoundedRectangle(cornerRadius: Radius.md).stroke(p.border))
            }
            .buttonStyle(PressableStyle())
        }
        .frame(maxWidth: .infinity).padding(.vertical, 48)
    }
}

/// 顶部横幅错误（不打断页面：如 WS 断线、行情延迟）
struct InlineBanner: View {
    @Environment(\.palette) private var p
    let text: String
    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 12))
            Text(text).font(.lb(FontSize.small))
            Spacer()
        }
        .foregroundColor(p.down).padding(.horizontal, Spacing.pageH).frame(height: 30).background(p.downTint)
    }
}
