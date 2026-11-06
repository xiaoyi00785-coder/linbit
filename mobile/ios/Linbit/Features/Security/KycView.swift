import SwiftUI
import PhotosUI
import UIKit

// MARK: - 身份认证（KYC）：状态 → 基础认证（L1）/ 高级认证（L2，需地址证明）
enum KycAPI {
    static func status() async throws -> APIKycStatus { try await APIClient.shared.get("/kyc/status", auth: true) }

    /// POST /kyc/documents（multipart：file + side），JPEG/PNG ≤ 10MB
    static func upload(_ jpeg: Data, side: APIKycDocument.APISide) async throws -> APIKycDocument {
        try await APIClient.shared.uploadMultipart("/kyc/documents", field: "file", filename: "\(side.rawValue).jpg", mime: "image/jpeg",
                                                   data: jpeg, fields: ["side": side.rawValue])
    }

    /// 后端 dateOfBirth 为 "yyyy-MM-dd" 字符串，故不直接用生成的 APIKycSubmission（Date 会编码成完整时间戳）
    struct Submit: Encodable {
        let firstName, lastName, country, idType, idNumber, dateOfBirth: String
        let level: Int
        let documents: APIKycSubmissionDocuments
    }
    static func submit(_ s: Submit) async throws -> APIKycStatus { try await APIClient.shared.post("/kyc", body: s) }

    static func compress(_ img: UIImage, maxBytes: Int = 10 * 1024 * 1024) -> Data? {
        var side = max(img.size.width, img.size.height)
        side = min(side, 2400)
        while side >= 600 {
            let k = side / max(img.size.width, img.size.height)
            let size = CGSize(width: img.size.width * k, height: img.size.height * k)
            let fmt = UIGraphicsImageRendererFormat.default(); fmt.scale = 1; fmt.opaque = true
            let out = UIGraphicsImageRenderer(size: size, format: fmt).image { _ in img.draw(in: CGRect(origin: .zero, size: size)) }
            for q in stride(from: 0.85, through: 0.4, by: -0.15) {
                if let d = out.jpegData(compressionQuality: q), d.count <= maxBytes { return d }
            }
            side *= 0.7
        }
        return nil
    }
}

/// 截图演示：SecShot.raw = "kyc:<state>"，state ∈ none / pending / rejected / approved / form / form2 / error / loading
enum KycShot {
    static var state: String? { SecShot.raw?.hasPrefix("kyc:") == true ? String(SecShot.raw!.dropFirst(4)) : nil }
    static var status: APIKycStatus? {
        switch state {
        case "pending": return .init(level: 1, status: .pending, submittedAt: SecShot.now.addingTimeInterval(-3600))
        case "rejected": return .init(level: 1, status: .rejected, submittedAt: SecShot.now.addingTimeInterval(-86400), reason: "证件照片模糊，请重新拍摄并确保四角完整")
        case "approved", "form2", "form2full": return .init(level: 1, status: .approved, submittedAt: SecShot.now.addingTimeInterval(-86400 * 3))
        case nil, "error", "loading": return nil
        default: return .init(level: 0, status: ._none)
        }
    }
}

struct KycView: View {
    @Environment(\.palette) private var p
    var onBack: (() -> Void)? = nil
    @State private var data: Loadable<APIKycStatus> = .loading
    @State private var form: Int? = ["form": 1, "form2": 2, "form2full": 2][KycShot.state ?? ""]

    var body: some View {
        if let lv = form, let st = data.value {
            KycFormView(level: lv, onBack: { form = nil }) { new in data = .loaded(new); form = nil }
                .transition(.move(edge: .trailing))
                .id(st.level * 10 + lv)
        } else {
            VStack(spacing: 0) {
                NavBar(title: "KYC 验证", onBack: onBack)
                switch data {
                case .idle, .loading: Spacer(); ProgressView(); Spacer()
                case let .failed(m): ErrorStateView(message: m) { Task { await load() } }
                case let .loaded(s): ScrollView { content(s).padding(Spacing.pageH) }
                }
            }
            .background(p.bg.ignoresSafeArea())
            .task { await load() }
        }
    }

    @ViewBuilder private func content(_ s: APIKycStatus) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            statusCard(s)
            levelCard(1, title: "基础认证", perks: ["法币 / 提现额度提升至 50,000 USDT / 日", "解锁合约、理财等全部交易功能"],
                      need: "证件正面 + 手持自拍", s: s)
            levelCard(2, title: "高级认证", perks: ["提现额度提升至 2,000,000 USDT / 日", "大额 OTC 与机构服务"],
                      need: "基础认证通过后，补充地址证明（近 3 个月账单）", s: s)
            InfoNote(text: "认证资料仅用于合规审核，经加密存储。审核通常在 1 个工作日内完成，结果将通过站内信与邮件通知。")
        }
    }

    private func statusCard(_ s: APIKycStatus) -> some View {
        let (title, color, icon): (String, Color, String) = {
            switch s.status {
            case .approved: return ("已通过 Lv.\(s.level) 认证", p.up, "checkmark.seal.fill")
            case .pending: return ("审核中", p.brand, "clock.fill")
            case .rejected: return ("认证未通过", p.down, "xmark.octagon.fill")
            case ._none: return ("未认证", p.textSecondary, "person.text.rectangle")
            }
        }()
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: icon).foregroundColor(color)
                Text(title).font(.lb(18, .semibold)).foregroundColor(p.textPrimary)
            }
            if let t = s.submittedAt, s.status != ._none {
                Text("提交时间 \(Fmt.dateTime(t))").font(.lbNum(FontSize.small)).foregroundColor(p.textTertiary)
            }
            if s.status == .rejected, let r = s.reason, !r.isEmpty {
                Text("原因：\(r)").font(.lb(FontSize.small)).foregroundColor(p.down)
            }
            if s.status == .pending {
                Text("资料已提交，请耐心等待审核").font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading).padding(16)
        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.fill))
    }

    private func levelCard(_ lv: Int, title: String, perks: [String], need: String, s: APIKycStatus) -> some View {
        let passed = s.status == .approved && s.level >= lv
        let pendingHere = s.status == .pending && s.level == lv
        let locked = lv == 2 && !(s.status == .approved && s.level >= 1) && !(s.level >= 2)
        let btn: String? = passed || pendingHere ? nil : (locked ? nil : (s.status == .rejected && s.level == lv ? "重新认证" : "去认证"))
        return VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("Lv.\(lv) \(title)").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary)
                Spacer()
                if passed { tag("已认证", p.up) } else if pendingHere { tag("审核中", p.brand) } else if locked { tag("待解锁", p.textTertiary) }
            }
            ForEach(perks, id: \.self) { t in
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: "checkmark").font(.system(size: 10, weight: .bold)).foregroundColor(p.brand).padding(.top, 3)
                    Text(t).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
                }
            }
            Text("所需资料：\(need)").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
            if let btn { LBButton(title: btn, height: 40) { form = lv } }
        }
        .padding(16)
        // 对照 ref2/12：未通过的基础认证卡浅绿底；锁定/已通过仍描边白底
        .background(RoundedRectangle(cornerRadius: Radius.lg).fill(lv == 1 && !passed && !locked ? p.brand.opacity(0.10) : p.bg))
        .overlay(RoundedRectangle(cornerRadius: Radius.lg).stroke(lv == 1 && !passed && !locked ? p.brand.opacity(0.35) : p.border, lineWidth: 1))
    }

    private func tag(_ t: String, _ c: Color) -> some View {
        Text(t).font(.lb(FontSize.small)).foregroundColor(c).padding(.horizontal, 6).frame(height: 20)
            .background(RoundedRectangle(cornerRadius: 4).fill(c.opacity(0.12)))
    }

    private func load() async {
        if KycShot.state != nil {
            if KycShot.state == "error" { data = .failed("网络连接失败，请检查网络"); return }
            if KycShot.state == "loading" { data = .loading; return }
            if let s = KycShot.status { data = .loaded(s) }
            return
        }
        if data.value == nil { data = .loading }
        do { data = .loaded(try await KycAPI.status()) } catch { data = .failed(securityMessage(error)) }
    }
}

// MARK: - 认证表单
struct KycFormView: View {
    @Environment(\.palette) private var p
    let level: Int
    let onBack: () -> Void
    let onDone: (APIKycStatus) -> Void
    private static let countries: [(String, String)] = [("SG", "新加坡"), ("HK", "中国香港"), ("TW", "中国台湾"), ("MY", "马来西亚"), ("JP", "日本"),
                                                        ("KR", "韩国"), ("TH", "泰国"), ("VN", "越南"), ("PH", "菲律宾"), ("ID", "印度尼西亚"),
                                                        ("AE", "阿联酋"), ("GB", "英国"), ("DE", "德国"), ("AU", "澳大利亚"), ("CA", "加拿大")]
    private static let idTypes: [APIKycSubmission.APIIdType] = [.passport, .idCard, .driverLicense]
    @State private var first = KycShot.state?.hasPrefix("form") == true ? "San" : ""
    @State private var last = KycShot.state?.hasPrefix("form") == true ? "Zhang" : ""
    @State private var country = "SG"
    @State private var idType = KycShot.state == "form2full" ? 1 : 0
    @State private var idNumber = KycShot.state?.hasPrefix("form") == true ? "E12345678" : ""
    @State private var dob = Calendar.current.date(byAdding: .year, value: -30, to: Date()) ?? Date()
    @State private var docs: [APIKycDocument.APISide: String] = KycShot.state == "form2full"
        ? [.front: "demo-front", .back: "demo-back", .selfie: "demo-selfie", .proofOfAddress: "demo-poa"] : [:]
    @State private var previews: [APIKycDocument.APISide: UIImage] = KycShot.state == "form" ? [.front: AvatarDemo.image(300)]
        : KycShot.state == "form2full" ? [.front: AvatarDemo.image(300), .back: AvatarDemo.image(300), .selfie: AvatarDemo.image(300), .proofOfAddress: AvatarDemo.image(300)] : [:]
    @State private var uploading: APIKycDocument.APISide? = KycShot.state == "form" ? .selfie : nil
    @State private var loading = false
    @State private var error: String? = KycShot.state == "form2" ? ErrorText.map["DOCUMENT_REQUIRED"] : nil
    @State private var showCountry = false

    private var type: APIKycSubmission.APIIdType { Self.idTypes[idType] }
    private var sides: [(APIKycDocument.APISide, String, Bool)] {
        var base: [(APIKycDocument.APISide, String, Bool)] = type == .passport
            ? [(.front, "护照信息页", true), (.selfie, "手持证件自拍", true)]
            : [(.front, "证件正面", true), (.back, "证件背面", true), (.selfie, "手持证件自拍", true)]
        // 高级认证：后端要求同时重新提交 Lv.1 全部资料与证件，另加地址证明
        if level == 2 { base.append((.proofOfAddress, "地址证明（近 3 个月水电 / 银行账单）", true)) }
        return base
    }
    private var age: Int { Calendar.current.dateComponents([.year], from: dob, to: Date()).year ?? 0 }
    private var idHint: String? { idNumber.isEmpty || idNumber.count >= 5 ? nil : "证件号码格式不正确" }
    private var valid: Bool {
        let docsOK = sides.allSatisfy { !$0.2 || docs[$0.0] != nil }
        return !first.trimmingCharacters(in: .whitespaces).isEmpty && !last.trimmingCharacters(in: .whitespaces).isEmpty
            && idNumber.count >= 5 && age >= 18 && docsOK
    }

    var body: some View {
        SecForm(title: level == 2 ? "高级认证" : "基础认证", onBack: onBack, error: error, button: "提交审核",
                enabled: valid && uploading == nil, loading: loading, action: { Task { await submit() } }) {
            if level == 2 {
                InfoNote(text: "高级认证需重新填写基础资料并上传证件，另需提供近 3 个月的地址证明。")
            }
            Group {
                HStack(spacing: 12) {
                    LBTextField(label: "名", placeholder: "与证件一致", text: $first)
                    LBTextField(label: "姓", placeholder: "与证件一致", text: $last)
                }
                Button { showCountry = true } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("国家 / 地区").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                            Text(Self.countries.first { $0.0 == country }.map { "\($0.1) (\($0.0))" } ?? country)
                                .font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                        }
                        Spacer()
                        Image(systemName: "chevron.down").font(.system(size: 12)).foregroundColor(p.textTertiary)
                    }
                    .padding(.horizontal, 14).frame(height: 56)
                    .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                }.buttonStyle(.plain)
                VStack(alignment: .leading, spacing: 8) {
                    Text("证件类型").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                    PillSegment(items: ["护照", "身份证", "驾照"], selected: $idType)
                }
                LBTextField(label: "证件号码", placeholder: "请输入证件号码", text: $idNumber, error: idHint)
                HStack {
                    Text("出生日期").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                    Spacer()
                    DatePicker("", selection: $dob, in: ...Date(), displayedComponents: .date).labelsHidden()
                        .environment(\.locale, Locale(identifier: "zh_CN"))
                }
                .padding(.horizontal, 14).frame(height: 52)
                .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
                if age < 18 { Text("未满 18 周岁无法完成认证").font(.lb(FontSize.small)).foregroundColor(p.down) }
            }
            Text("上传证件").font(.lb(FontSize.body, .semibold)).foregroundColor(p.textPrimary).padding(.top, 4)
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible())], spacing: 12) {
                ForEach(sides, id: \.0) { side, title, required in
                    KycDocSlot(title: title, required: required, image: previews[side], uploaded: docs[side] != nil, uploading: uploading == side) { img in
                        Task { await upload(img, side) }
                    }
                }
            }
            InfoNote(text: "请上传清晰、完整、无遮挡的原件照片，仅支持 JPG / PNG，单张不超过 10MB。")
        }
        .lbSheet($showCountry) {
            LBSheet(title: "选择国家 / 地区", onClose: { showCountry = false }) {
                VStack(spacing: 0) {
                    ForEach(Self.countries, id: \.0) { c in
                        Button { country = c.0; showCountry = false } label: {
                            HStack {
                                Text("\(c.1) (\(c.0))").font(.lb(FontSize.body)).foregroundColor(p.textPrimary)
                                Spacer()
                                if c.0 == country { Image(systemName: "checkmark").foregroundColor(p.brand) }
                            }.frame(height: 44).contentShape(Rectangle())
                        }.buttonStyle(.plain)
                    }
                }
            }
        }
        .onChange(of: idType) { _ in if type == .passport { docs[.back] = nil; previews[.back] = nil } }
    }

    private func upload(_ img: UIImage, _ side: APIKycDocument.APISide) async {
        // DEBUG 截图夹具：只填视觉，永不实际上传证件
        if KycShot.state != nil {
            previews[side] = img; docs[side] = "shot-\(side.rawValue)"; return
        }
        guard let jpeg = KycAPI.compress(img) else { error = ErrorText.map["FILE_TOO_LARGE"]; return }
        previews[side] = img; uploading = side; error = nil
        defer { uploading = nil }
        do { docs[side] = try await KycAPI.upload(jpeg, side: side).fileId }
        catch { previews[side] = nil; self.error = securityMessage(error); Haptics.error() }
    }

    private func submit() async {
        // DEBUG 截图夹具：永不真实提交 KYC
        if KycShot.state != nil {
            ToastCenter.shared.show("截图夹具不提交认证", .info); return
        }
        loading = true; defer { loading = false }
        let f = DateFormatter(); f.calendar = Calendar(identifier: .gregorian); f.locale = Locale(identifier: "en_US_POSIX"); f.dateFormat = "yyyy-MM-dd"
        let body = KycAPI.Submit(firstName: first.trimmingCharacters(in: .whitespaces), lastName: last.trimmingCharacters(in: .whitespaces),
                                 country: country, idType: type.rawValue, idNumber: idNumber.trimmingCharacters(in: .whitespaces),
                                 dateOfBirth: f.string(from: dob), level: level,
                                 documents: .init(front: docs[.front] ?? "", selfie: docs[.selfie] ?? "", back: docs[.back], proofOfAddress: docs[.proofOfAddress]))
        do {
            let st = try await KycAPI.submit(body)
            Haptics.success(); ToastCenter.shared.show("已提交，等待审核", .success)
            await Session.shared.reloadAccount()
            onDone(st)
        } catch { self.error = securityMessage(error); Haptics.error() }
    }
}

struct KycDocSlot: View {
    @Environment(\.palette) private var p
    let title: String
    let required: Bool
    let image: UIImage?
    let uploaded: Bool
    let uploading: Bool
    let onPick: (UIImage) -> Void
    @State private var item: PhotosPickerItem?
    var body: some View {
        PhotosPicker(selection: $item, matching: .images) {
            VStack(spacing: 6) {
                ZStack {
                    RoundedRectangle(cornerRadius: Radius.md).fill(p.fill)
                    if let image {
                        Image(uiImage: image).resizable().scaledToFill().clipShape(RoundedRectangle(cornerRadius: Radius.md))
                    } else {
                        VStack(spacing: 4) {
                            Image(systemName: "camera.fill").font(.system(size: 20)).foregroundColor(p.textTertiary)
                            Text("点击上传").font(.lb(FontSize.small)).foregroundColor(p.textTertiary)
                        }
                    }
                    if uploading { RoundedRectangle(cornerRadius: Radius.md).fill(Color.black.opacity(0.45)); ProgressView().tint(.white) }
                    if uploaded && !uploading {
                        Image(systemName: "checkmark.circle.fill").foregroundColor(p.up).background(Circle().fill(.white))
                            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topTrailing).padding(6)
                    }
                }
                .frame(height: 96).clipped()
                Text(title + (required ? "" : "（选填）")).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).lineLimit(2).multilineTextAlignment(.center)
            }
        }
        .disabled(uploading)
        .onChange(of: item) { it in
            guard let it else { return }
            Task {
                if let d = try? await it.loadTransferable(type: Data.self), let img = UIImage(data: d) { onPick(img) }
                item = nil
            }
        }
    }
}
