import SwiftUI

#if DEBUG
/// DEBUG-only layout width probe for multi-size review in Xcode Previews.
/// 320pt：无可靠 CI runtime 时仅本地 Preview，不伪造已实机/已 CI。
/// 多尺寸 fixture 仅 DEBUG；Release / ShotGate 生产路径不引用本文件逻辑。
enum LayoutSizeProbe {
    struct Size: Identifiable {
        var id: String { label }
        let label: String
        let width: CGFloat
        let height: CGFloat
        let note: String
    }

    static let sizes: [Size] = [
        .init(label: "320 (legacy SE1)", width: 320, height: 568,
              note: "CI macos-15/iOS18 通常无 bootable 320 runtime；Preview only"),
        .init(label: "375 SE", width: 375, height: 667, note: "adapt se375 / 750×1334@2x"),
        .init(label: "393 iPhone16", width: 393, height: 852, note: "PR smoke ~1179×2556@3x"),
        .init(label: "430 Plus", width: 430, height: 932, note: "optional adapt max430"),
    ]
}

struct LayoutSizeProbeView: View {
    @Environment(\.palette) private var p
    let size: LayoutSizeProbe.Size
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(size.label).font(.lb(FontSize.title, .semibold)).foregroundColor(p.textPrimary)
            Text("\(Int(size.width))×\(Int(size.height)) pt").font(.lbNum(FontSize.label)).foregroundColor(p.textSecondary)
            Text(size.note).font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            ChipBar(items: ["现货", "合约"], selected: .constant(0))
            PrimaryTabs(items: ["合约", "事件合约", "TradFi", "模拟交易"], selected: .constant(0), spacing: 16)
            Text("动态字体封顶 1.35×；本页仅 Preview 探针").font(.lb(FontSize.caption)).foregroundColor(p.textSecondary)
        }
        .padding(Spacing.pageH)
        .frame(width: size.width, height: size.height, alignment: .topLeading)
        .background(p.bg)
    }
}

#Preview("320 preview-only") {
    ThemedRoot { LayoutSizeProbeView(size: LayoutSizeProbe.sizes[0]) }
}
#Preview("375 SE") {
    ThemedRoot { LayoutSizeProbeView(size: LayoutSizeProbe.sizes[1]) }
}
#Preview("393 mid") {
    ThemedRoot { LayoutSizeProbeView(size: LayoutSizeProbe.sizes[2]) }
}
#Preview("430 max") {
    ThemedRoot { LayoutSizeProbeView(size: LayoutSizeProbe.sizes[3]) }
}
#endif
