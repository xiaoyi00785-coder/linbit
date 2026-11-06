import SwiftUI

/// Linbit 标志：两个互扣的圆角链环（与 icons/logo.svg、LaunchLogo.svg 同一几何，1024 单位网格 = App 图标画布）。
/// `.upper` 为左上链环（开口朝下），`.lower` 为右下链环（开口朝上），两者叠放即完整标志。
struct LinbitLink: Shape {
    enum Part { case upper, lower }
    var part: Part

    func path(in r: CGRect) -> Path {
        let u = min(r.width, r.height) / 1024
        let x0 = r.midX - 512 * u, y0 = r.midY - 512 * u
        func pt(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: x0 + x * u, y: y0 + y * u) }
        var p = Path()
        switch part {
        case .upper:
            p.move(to: pt(384.45, 244))
            p.addLine(to: pt(514.39, 244))
            p.addCurve(to: pt(609.93, 339.54), control1: pt(567.16, 244), control2: pt(609.93, 286.78))
            p.addLine(to: pt(609.93, 559.29))
            p.addCurve(to: pt(514.39, 654.84), control1: pt(609.93, 612.06), control2: pt(567.16, 654.84))
            p.addLine(to: pt(506.75, 654.84))
            p.addLine(to: pt(506.75, 584.14))
            p.addLine(to: pt(514.39, 584.14))
            p.addCurve(to: pt(539.23, 559.29), control1: pt(528.11, 584.14), control2: pt(539.23, 573.01))
            p.addLine(to: pt(539.23, 339.54))
            p.addCurve(to: pt(514.39, 314.7), control1: pt(539.23, 325.82), control2: pt(528.11, 314.7))
            p.addLine(to: pt(384.45, 314.7))
            p.addCurve(to: pt(359.61, 339.54), control1: pt(370.73, 314.7), control2: pt(359.61, 325.82))
            p.addLine(to: pt(359.61, 559.29))
            p.addCurve(to: pt(384.45, 584.14), control1: pt(359.61, 573.01), control2: pt(370.73, 584.14))
            p.addLine(to: pt(392.09, 584.14))
            p.addLine(to: pt(392.09, 654.84))
            p.addLine(to: pt(384.45, 654.84))
            p.addCurve(to: pt(288.91, 559.29), control1: pt(331.68, 654.84), control2: pt(288.91, 612.06))
            p.addLine(to: pt(288.91, 339.54))
            p.addCurve(to: pt(384.45, 244), control1: pt(288.91, 286.78), control2: pt(331.68, 244))
            p.closeSubpath()
        case .lower:
            p.move(to: pt(509.61, 458.97))
            p.addLine(to: pt(517.25, 458.97))
            p.addLine(to: pt(517.25, 529.68))
            p.addLine(to: pt(509.61, 529.68))
            p.addCurve(to: pt(484.77, 554.52), control1: pt(495.89, 529.68), control2: pt(484.77, 540.8))
            p.addLine(to: pt(484.77, 684.46))
            p.addCurve(to: pt(509.61, 709.3), control1: pt(484.77, 698.18), control2: pt(495.89, 709.3))
            p.addLine(to: pt(639.55, 709.3))
            p.addCurve(to: pt(664.39, 684.46), control1: pt(653.27, 709.3), control2: pt(664.39, 698.18))
            p.addLine(to: pt(664.39, 554.52))
            p.addCurve(to: pt(639.55, 529.68), control1: pt(664.39, 540.8), control2: pt(653.27, 529.68))
            p.addLine(to: pt(631.91, 529.68))
            p.addLine(to: pt(631.91, 458.97))
            p.addLine(to: pt(639.55, 458.97))
            p.addCurve(to: pt(735.09, 554.52), control1: pt(692.32, 458.97), control2: pt(735.09, 501.75))
            p.addLine(to: pt(735.09, 684.46))
            p.addCurve(to: pt(639.55, 780), control1: pt(735.09, 737.22), control2: pt(692.32, 780))
            p.addLine(to: pt(509.61, 780))
            p.addCurve(to: pt(414.07, 684.46), control1: pt(456.84, 780), control2: pt(414.07, 737.22))
            p.addLine(to: pt(414.07, 554.52))
            p.addCurve(to: pt(509.61, 458.97), control1: pt(414.07, 501.75), control2: pt(456.84, 458.97))
            p.closeSubpath()
        }
        return p
    }
}

/// 完整标志（两链环合并），可用于任意尺寸。
struct LinbitMark: Shape {
    func path(in r: CGRect) -> Path {
        var p = LinbitLink(part: .upper).path(in: r)
        p.addPath(LinbitLink(part: .lower).path(in: r))
        return p
    }
}

/// 启动动画：与系统 LaunchScreen 无缝衔接（同背景色、同位置 96pt 品牌绿圆角方块 + 链环标志），
/// 约 1.4s：0–0.3 绿色底块出现并缩放回弹 → 0.18–0.6 两个链环分别从左上 / 右下沿对角线滑入并扣合
/// → 0.55–0.72 扣合瞬间轻微「咔嗒」缩放 → 0.45–0.85 光晕扩散 → 0.85–1 整体淡出。
/// 由单一进度 t∈[0,1] 驱动，CI 可冻结在任意时刻截图。
struct SplashView: View {
    /// nil = 正常播放；非 nil = 冻结在该进度（CI 截图）
    var frozen: Double? = nil
    var onFinish: () -> Void = {}

    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var start = Date()

    static let duration: Double = 1.4
    private let brand = Color(red: 0xAB / 255, green: 0xE1 / 255, blue: 0x27 / 255)
    private let ink = Color(red: 0x07 / 255, green: 0x08 / 255, blue: 0x08 / 255)

    var body: some View {
        Group {
            if let f = frozen {
                frame(f)
            } else if reduceMotion {
                // 减少动态效果：静态 Logo，由外层直接淡入首页
                frame(0.85)
            } else {
                TimelineView(.animation) { ctx in
                    frame(min(1, ctx.date.timeIntervalSince(start) / Self.duration))
                }
            }
        }
        .ignoresSafeArea()
        .onAppear {
            start = Date()
            guard frozen == nil else { return }
            let d = reduceMotion ? 0.05 : Self.duration
            DispatchQueue.main.asyncAfter(deadline: .now() + d) { onFinish() }
        }
        .accessibilityHidden(true)
    }

    private func seg(_ t: Double, _ a: Double, _ b: Double) -> Double { max(0, min(1, (t - a) / (b - a))) }
    private func easeOut(_ x: Double) -> Double { 1 - pow(1 - x, 3) }
    private func backOut(_ x: Double) -> Double { let c = 1.70158; return 1 + (c + 1) * pow(x - 1, 3) + c * pow(x - 1, 2) }

    @ViewBuilder private func frame(_ t: Double) -> some View {
        let tile = seg(t, 0, 0.3), links = seg(t, 0.18, 0.6), click = seg(t, 0.55, 0.72)
        let glow = seg(t, 0.45, 0.85), out = seg(t, 0.85, 1)
        // 链环滑入距离（pt）：从对角线两端相向移动，最后完全扣合
        let gap = 22 * (1 - easeOut(links))
        let pulse = 1 + 0.06 * sin(Double.pi * click)
        ZStack {
            (scheme == .dark ? Color(red: 0x0B / 255, green: 0x0B / 255, blue: 0x0C / 255) : Color.white)
            ZStack {
                Circle()
                    .fill(RadialGradient(colors: [brand.opacity(scheme == .dark ? 0.55 : 0.4), brand.opacity(0)],
                                         center: .center, startRadius: 0, endRadius: 110))
                    .frame(width: 220, height: 220)
                    .scaleEffect(0.6 + 0.6 * easeOut(glow))
                    .opacity(glow * (1 - out * 0.6))
                RoundedRectangle(cornerRadius: 24, style: .continuous)
                    .fill(brand)
                    .frame(width: 96, height: 96)
                    .shadow(color: brand.opacity(0.6 * glow), radius: 18 * glow)
                    .scaleEffect(0.6 + 0.4 * backOut(tile))
                    .opacity(easeOut(tile))
                ZStack {
                    LinbitLink(part: .upper).fill(ink).offset(x: -gap, y: -gap)
                    LinbitLink(part: .lower).fill(ink).offset(x: gap, y: gap)
                }
                .frame(width: 96, height: 96)
                .opacity(easeOut(seg(t, 0.18, 0.4)))
                .mask(RoundedRectangle(cornerRadius: 24, style: .continuous).frame(width: 96, height: 96))
            }
            .scaleEffect(pulse * (1 + 0.08 * out))
        }
        .opacity(1 - out)
    }
}

/// 冷启动播放一次；进入后台再回前台不重复（状态挂在 App 生命周期内的 StateObject 上）。
final class SplashState: ObservableObject {
    @Published var done = false
    /// CI：lb.shot = splash-start / splash-mid / splash-end 时冻结（仅 Debug / ShotGate）
    /// start = 链环刚开始滑入（仍分开），mid = 即将扣合，end = 扣合完成、开始淡出
    let frozen: Double? = {
        guard ShotGate.enabled else { return nil }
        switch ShotGate.launchShot {
        case "splash-start": return 0.28
        case "splash-mid": return 0.5
        case "splash-end": return 0.88
        default: return nil
        }
    }()
}
