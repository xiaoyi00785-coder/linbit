import Foundation

/// 账户标识：邮箱 或 手机（国家码 + 去掉前导 0 的号码），与网页 Auth.tsx 一致。
struct AccountId: Equatable {
    enum Kind: Int { case email, phone }
    var kind: Kind
    var email = ""
    var countryCode = "86"
    var phone = ""

    var phoneDigits: String { String(phone.filter(\.isNumber).drop(while: { $0 == "0" })) }
    var e164: String { "+\(countryCode)\(phoneDigits)" }
    var isValid: Bool {
        switch kind {
        case .email: return email.range(of: #"^[^\s@]+@[^\s@]+\.[^\s@]+$"#, options: .regularExpression) != nil
        case .phone: return (5...15).contains(phoneDigits.count)
        }
    }
    /// 「验证码已发送至 X」用的脱敏展示
    var masked: String {
        switch kind {
        case .email:
            let parts = email.split(separator: "@")
            guard parts.count == 2 else { return email }
            return "\(parts[0].prefix(2))***@\(parts[1])"
        case .phone:
            let d = phoneDigits
            return "+\(countryCode) \(d.prefix(3))****\(d.suffix(4))"
        }
    }
    var emailOrNil: String? { kind == .email ? email.trimmingCharacters(in: .whitespaces) : nil }
    var phoneOrNil: String? { kind == .phone ? e164 : nil }
    var ccOrNil: String? { kind == .phone ? countryCode : nil }
}

enum PasswordRule {
    /// 8 位以上，含字母和数字
    static func ok(_ s: String) -> Bool {
        s.count >= 8 && s.contains(where: \.isLetter) && s.contains(where: \.isNumber)
    }
}

enum LoginResult { case done, challenge(APILoginChallenge) }

enum AuthAPI {
    static var api: APIClient { .shared }

    static func login(_ id: AccountId, password: String) async throws -> LoginResult {
        let body = APILoginRequest(email: id.emailOrNil, phone: id.phoneOrNil, countryCode: id.ccOrNil, password: password,
                                   deviceId: Keychain.deviceId, deviceName: DeviceInfo.name)
        let r: APILogin200Response = try await api.post("/auth/login", body: body, auth: false)
        switch r {
        case let .typeAPIAuthTokens(t): TokenStore.shared.save(t); return .done
        case let .typeAPILoginChallenge(c): return .challenge(c)
        }
    }

    static func verifyLogin(_ c: APILoginChallenge, method: APILoginChallenge.APIMethods, code: String) async throws {
        let body = APILoginVerifyRequest(challengeToken: c.challengeToken,
                                         totpCode: method == .totp ? code : nil, code: method == .totp ? nil : code)
        let t: APIAuthTokens = try await api.post("/auth/login/verify", body: body, auth: false)
        TokenStore.shared.save(t)
    }

    static func resendLoginCode(_ c: APILoginChallenge) async throws {
        let _: EmptyResponse = try await api.post("/auth/login/code", body: APILoginResendCodeRequest(challengeToken: c.challengeToken), auth: false)
    }

    @discardableResult
    static func sendCode(_ id: AccountId, purpose: APISendCodeRequest.APIPurpose) async throws -> APISendCodeResponse {
        try await api.post("/auth/code/send", body: APISendCodeRequest(purpose: purpose, email: id.emailOrNil,
                                                                      phone: id.phoneOrNil, countryCode: id.ccOrNil), auth: false)
    }

    static func register(_ id: AccountId, password: String, code: String, invite: String?) async throws {
        let body = APIRegisterRequest(email: id.emailOrNil, phone: id.phoneOrNil, countryCode: id.ccOrNil,
                                      password: password, code: code, inviteCode: invite?.isEmpty == false ? invite : nil)
        let t: APIAuthTokens = try await api.post("/auth/register", body: body, auth: false)
        TokenStore.shared.save(t)
    }

    static func verifyResetCode(_ id: AccountId, code: String) async throws -> APIVerifyResetCodeResponse {
        try await api.post("/auth/password/verify-code", body: APIVerifyResetCodeRequest(email: id.emailOrNil, phone: id.phoneOrNil,
                                                                                         countryCode: id.ccOrNil, code: code), auth: false)
    }

    static func resetPassword(token: String, newPassword: String) async throws {
        let _: EmptyResponse = try await api.post("/auth/password/reset", body: APIPasswordResetRequest(resetToken: token, newPassword: newPassword), auth: false)
    }

    static func logout() async {
        let _: EmptyResponse? = try? await api.post("/auth/logout", body: Optional<Int>.none)
        TokenStore.shared.clear()
    }

    static func account() async throws -> APIAccount { try await api.get("/account", auth: true) }
    static func bindTotp() async throws -> APITotpBinding { try await api.post("/2fa/totp/bind", body: Optional<Int>.none) }
    static func verifyTotp(_ code: String) async throws {
        let _: EmptyResponse = try await api.post("/2fa/totp/verify", body: APIVerifyTotpRequest(code: code))
    }
}

enum DeviceInfo {
    static var name: String {
        #if canImport(UIKit)
        return UIDevice.current.name
        #else
        return "iPhone"
        #endif
    }
}
#if canImport(UIKit)
import UIKit
#endif
