import SwiftUI
import PhotosUI
import UIKit

// MARK: - 头像：上传（POST /account/avatar，multipart 字段 file）与展示（GET /account/avatars/{file}，无需鉴权）
enum AvatarAPI {
    /// 服务端上限 2 MiB
    static let maxBytes = 2 * 1024 * 1024

    static func upload(_ jpeg: Data) async throws -> APIAccount {
        try await APIClient.shared.uploadMultipart("/account/avatar", field: "file", filename: "avatar.jpg", mime: "image/jpeg", data: jpeg)
    }

    /// avatarUrl 可能是绝对地址，也可能是 "/account/avatars/xxx" 相对路径（相对 API 根）
    static func resolve(_ s: String?) -> URL? {
        guard let s = s?.trimmingCharacters(in: .whitespaces), !s.isEmpty else { return nil }
        if let u = URL(string: s), u.scheme != nil { return u }
        let path = s.hasPrefix("/") ? String(s.dropFirst()) : s
        return URL(string: path, relativeTo: AppConfig.apiBase.absoluteString.hasSuffix("/") ? AppConfig.apiBase : AppConfig.apiBase.appendingPathComponent(""))?.absoluteURL
    }

    /// 缩放到 ≤ 512px 正方形并逐级降低 JPEG 质量，直到 ≤ 2 MiB
    static func compress(_ img: UIImage, side: CGFloat = 512) -> Data? {
        var target = side
        while target >= 128 {
            let fmt = UIGraphicsImageRendererFormat.default(); fmt.scale = 1; fmt.opaque = true
            let scaled = UIGraphicsImageRenderer(size: CGSize(width: target, height: target), format: fmt).image { _ in
                img.draw(in: CGRect(x: 0, y: 0, width: target, height: target))
            }
            for q in stride(from: 0.9, through: 0.3, by: -0.15) {
                if let d = scaled.jpegData(compressionQuality: q), d.count <= maxBytes { return d }
            }
            target /= 2
        }
        return nil
    }
}

/// 圆形头像：有 avatarUrl 显示远程图，否则显示昵称首字 / 默认图标
struct AvatarView: View {
    @Environment(\.palette) private var p
    var url: String?
    var name: String?
    var size: CGFloat = 28
    var local: UIImage? = nil
    var body: some View {
        Group {
            if let local {
                Image(uiImage: local).resizable().scaledToFill()
            } else if let u = AvatarAPI.resolve(url) {
                AsyncImage(url: u) { phase in
                    switch phase {
                    case let .success(img): img.resizable().scaledToFill()
                    default: fallback
                    }
                }
            } else { fallback }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .overlay(Circle().stroke(p.divider, lineWidth: 0.5))
    }
    @ViewBuilder private var fallback: some View {
        if let c = name?.trimmingCharacters(in: .whitespaces).first {
            ZStack {
                Circle().fill(p.brand.opacity(0.18))
                Text(String(c).uppercased()).font(.lb(size * 0.45, .semibold)).foregroundColor(p.brand)
            }
        } else {
            ZStack { Circle().fill(p.fill); IconView(icon: .avatar, size: size) }
        }
    }
}

/// 截图用演示头像（渐变 + 首字）
enum AvatarDemo {
    static func image(_ side: CGFloat = 600) -> UIImage {
        UIGraphicsImageRenderer(size: CGSize(width: side, height: side)).image { ctx in
            let colors = [UIColor(red: 0.98, green: 0.55, blue: 0.2, alpha: 1).cgColor, UIColor(red: 0.35, green: 0.2, blue: 0.75, alpha: 1).cgColor]
            let g = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: colors as CFArray, locations: [0, 1])!
            ctx.cgContext.drawLinearGradient(g, start: .zero, end: CGPoint(x: side, y: side), options: [])
            let s = NSAttributedString(string: "L", attributes: [.font: UIFont.systemFont(ofSize: side * 0.45, weight: .bold), .foregroundColor: UIColor.white])
            let sz = s.size(); s.draw(at: CGPoint(x: (side - sz.width) / 2, y: (side - sz.height) / 2))
        }
    }
}

// MARK: - 裁剪：正方形取景框，拖动 + 双指缩放
struct AvatarCropView: View {
    @Environment(\.palette) private var p
    let image: UIImage
    let onCancel: () -> Void
    let onDone: (UIImage) -> Void
    @State private var scale: CGFloat = 1
    @State private var lastScale: CGFloat = 1
    @State private var offset: CGSize = .zero
    @State private var lastOffset: CGSize = .zero

    var body: some View {
        GeometryReader { geo in
            let side = min(geo.size.width - 40, 320)
            VStack(spacing: 0) {
                HStack {
                    Button("取消", action: onCancel).foregroundColor(.white)
                    Spacer()
                    Text("裁剪头像").font(.lb(FontSize.body, .semibold)).foregroundColor(.white)
                    Spacer()
                    Button("完成") { onDone(render(side: side)) }.foregroundColor(p.brand)
                }
                .font(.lb(FontSize.body)).padding(.horizontal, 20).frame(height: 52)
                Spacer()
                ZStack {
                    Image(uiImage: image).resizable().scaledToFill()
                        .frame(width: side * scale, height: side * scale)
                        .offset(offset)
                        .frame(width: side, height: side)
                        .clipped()
                    Circle().stroke(Color.white, lineWidth: 1.5).frame(width: side, height: side)
                }
                .frame(width: side, height: side)
                .contentShape(Rectangle())
                .gesture(DragGesture().onChanged { v in
                    offset = clamp(CGSize(width: lastOffset.width + v.translation.width, height: lastOffset.height + v.translation.height), side)
                }.onEnded { _ in lastOffset = offset })
                .simultaneousGesture(MagnificationGesture().onChanged { v in
                    scale = min(max(lastScale * v, 1), 4); offset = clamp(offset, side)
                }.onEnded { _ in lastScale = scale; lastOffset = offset })
                Spacer()
                Text("拖动、双指缩放调整头像范围").font(.lb(FontSize.small)).foregroundColor(.white.opacity(0.7)).padding(.bottom, 32)
            }
            .frame(maxWidth: .infinity)
        }
        .background(Color.black.ignoresSafeArea())
    }

    private func clamp(_ o: CGSize, _ side: CGFloat) -> CGSize {
        let m = (side * scale - side) / 2
        return CGSize(width: min(max(o.width, -m), m), height: min(max(o.height, -m), m))
    }

    /// 把取景框内容按原图分辨率渲染成正方形
    private func render(side: CGFloat) -> UIImage {
        let img = image
        let w = img.size.width, h = img.size.height
        let fill = max(side / w, side / h) * scale          // 屏幕点 / 原图像素
        let out: CGFloat = min(1024, min(w, h) / scale)
        let k = out / side
        let fmt = UIGraphicsImageRendererFormat.default(); fmt.scale = 1
        return UIGraphicsImageRenderer(size: CGSize(width: out, height: out), format: fmt).image { _ in
            let dw = w * fill * k, dh = h * fill * k
            let x = (out - dw) / 2 + offset.width * k, y = (out - dh) / 2 + offset.height * k
            img.draw(in: CGRect(x: x, y: y, width: dw, height: dh))
        }
    }
}

// MARK: - 资料页头像区：选择 → 裁剪 → 压缩 → 上传（加载 / 错误态）
struct AvatarEditor: View {
    @Environment(\.palette) private var p
    @EnvironmentObject private var session: Session
    @State private var item: PhotosPickerItem?
    @State private var cropping: UIImage? = SecShot.state == "crop" ? AvatarDemo.image() : nil
    @State private var preview: UIImage? = ["uploading", "avatar-error", "avatar"].contains(SecShot.state) ? AvatarDemo.image(200) : nil
    @State private var uploading = SecShot.state == "uploading"
    @State private var error: String? = SecShot.state == "avatar-error" ? ErrorText.map["FILE_TOO_LARGE"] : nil

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 14) {
                PhotosPicker(selection: $item, matching: .images) {
                    ZStack(alignment: .bottomTrailing) {
                        AvatarView(url: session.account?.avatarUrl, name: session.account?.nickname ?? (SecShot.on ? "lb_Trader" : nil), size: 64, local: preview)
                            .overlay {
                                if uploading {
                                    ZStack { Circle().fill(Color.black.opacity(0.45)); ProgressView().tint(.white) }
                                }
                            }
                        ZStack { Circle().fill(p.brand); Image(systemName: "camera.fill").font(.system(size: 10)).foregroundColor(.white) }
                            .frame(width: 22, height: 22).overlay(Circle().stroke(p.bg, lineWidth: 2))
                    }
                }
                .disabled(uploading)
                VStack(alignment: .leading, spacing: 4) {
                    Text(session.account?.nickname ?? (SecShot.on ? "lb_Trader" : "—")).font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                    Text("UID \(session.account?.uid ?? (SecShot.on ? "80000001" : "—"))").font(.lbNum(FontSize.small)).foregroundColor(p.textSecondary)
                    Text(uploading ? "头像上传中…" : "点击头像更换，支持 JPG / PNG / WebP，≤ 2MB")
                        .font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                }
            }
            if let error {
                Text(error).font(.lb(FontSize.small)).foregroundColor(p.down)
            }
        }
        .padding(.vertical, 8)
        .onChange(of: item) { it in
            guard let it else { return }
            Task {
                error = nil
                guard let d = try? await it.loadTransferable(type: Data.self), let img = UIImage(data: d) else {
                    error = "无法读取所选图片"; return
                }
                cropping = img
            }
        }
        .fullScreenCover(isPresented: Binding(get: { cropping != nil }, set: { if !$0 { cropping = nil } })) {
            if let img = cropping {
                AvatarCropView(image: img, onCancel: { cropping = nil; item = nil }) { cropped in
                    cropping = nil; item = nil
                    Task { await upload(cropped) }
                }
            }
        }
    }

    private func upload(_ img: UIImage) async {
        guard let jpeg = AvatarAPI.compress(img) else { error = ErrorText.map["FILE_TOO_LARGE"]; return }
        preview = img; uploading = true; error = nil
        defer { uploading = false }
        do {
            _ = try await AvatarAPI.upload(jpeg)
            await session.reloadAccount()
            preview = nil
            Haptics.success(); ToastCenter.shared.show("头像已更新", .success)
        } catch {
            preview = nil
            self.error = securityMessage(error); Haptics.error()
        }
    }
}
