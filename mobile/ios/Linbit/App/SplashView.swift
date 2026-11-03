import SwiftUI

/// Linbit Logo：品牌绿圆角方块 + 「L」，纯 SwiftUI 绘制（与 LaunchLogo.svg 同一几何，96 单位网格）。
struct LinbitLogoL: Shape {
    func path(in r: CGRect) -> Path {
        let u = min(r.width, r.height) / 96
        var p = Path()
        p.move(to: CGPoint(x: r.minX + 30 * u, y: r.minY + 22 * u))
        p.addLine(to: CGPoint(x: r.minX + 42 * u, y: r.minY + 22 * u))
        p.addLine(to: CGPoint(x: r.minX + 42 * u, y: r.minY + 62 * u))
        p.addLine(to: CGPoint(x: r.minX + 66 * u, y: r.minY + 62 * u))
        p.addLine(to: CGPoint(x: r.minX + 66 * u, y: r.minY + 74 * u))
        p.addLine(to: CGPoint(x: r.minX + 30 * u, y: r.minY + 74 * u))
        p.closeSubpath()
        return p
    }
}

/// 启动动画：与系统 LaunchScreen 无缝衔接（同背景色、同位置 96pt Logo），
/// 约 1.2s：0–0.35 绿色底块出现并缩放回弹 → 0.25–0.6 「L」描出 → 0.4–0.85 光晕扩散 → 0.85–1 整体淡出。
/// 由单一进度 t∈[0,1] 驱动，CI 可冻结在任意时刻截图。
struct SplashView: View {
    /// nil = 正常播放；非 nil = 冻结在该进度（CI 截图）
    var frozen: Double? = nil
    var onFinish: () -> Void = {}

    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var start = Date()

    static let duration: Double = 1.2
    private let brand = Color(red: 0xAB / 255, green: 0xE1 / 255, blue: 0x27 / 255)

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
        let tile = seg(t, 0, 0.35), letter = seg(t, 0.25, 0.6), glow = seg(t, 0.4, 0.85), out = seg(t, 0.85, 1)
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
                LinbitLogoL()
                    .trim(from: 0, to: easeOut(letter))
                    .stroke(Color(red: 0x0B / 255, green: 0x0B / 255, blue: 0x0C / 255), style: StrokeStyle(lineWidth: 2, lineJoin: .miter))
                    .frame(width: 96, height: 96)
                LinbitLogoL()
                    .fill(Color(red: 0x0B / 255, green: 0x0B / 255, blue: 0x0C / 255))
                    .frame(width: 96, height: 96)
                    .opacity(seg(t, 0.45, 0.6))
                    .scaleEffect(0.9 + 0.1 * easeOut(letter))
            }
            .scaleEffect(1 + 0.08 * out)
        }
        .opacity(1 - out)
    }
}

/// 冷启动播放一次；进入后台再回前台不重复（状态挂在 App 生命周期内的 StateObject 上）。
final class SplashState: ObservableObject {
    @Published var done = false
    /// CI：lb.shot = splash-start / splash-mid / splash-end 时冻结
    let frozen: Double? = {
        switch UserDefaults.standard.string(forKey: "lb.shot") {
        case "splash-start": return 0.12
        case "splash-mid": return 0.5
        case "splash-end": return 0.92
        default: return nil
        }
    }()
}
