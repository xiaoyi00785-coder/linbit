import Foundation
import AuthenticationServices
import UIKit

/// 账户安全接口（openapi: /account/security、/account/profile、/account/email|phone、/account/fund-password、
/// /account/anti-phishing、/account/sessions、/account/devices、/account/passkeys、/2fa/totp/disable）
enum SecurityAPI {
    static var api: APIClient { .shared }

    static func overview() async throws -> APISecurityOverview { try await api.get("/account/security", auth: true) }
    static func updateNickname(_ n: String) async throws -> APIAccount {
        try await api.patch("/account/profile", body: APIUpdateProfileRequest(nickname: n))
    }
    static func sendSecurityCode() async throws { try await AuthAPI.sendSecurityCode() }
    static func sendContactCode(email: String?, phone: String?, cc: String?) async throws {
        let _: APISendContactCode200Response = try await api.post("/account/contact/code",
            body: APISendContactCodeRequest(email: email, phone: phone, countryCode: cc))
    }
    static func changeEmail(_ email: String, newCode: String, oldCode: String, totp: String?) async throws {
        let _: APIOkResponse = try await api.put("/account/email",
            body: APIChangeContactRequest(email: email, newCode: newCode, oldCode: oldCode, totpCode: totp))
    }
    static func changePhone(_ phone: String, cc: String, newCode: String, oldCode: String, totp: String?) async throws {
        let _: APIOkResponse = try await api.put("/account/phone",
            body: APIChangeContactRequest(phone: phone, countryCode: cc, newCode: newCode, oldCode: oldCode, totpCode: totp))
    }
    enum FundMode { case set, change, reset }
    static func fundPassword(_ mode: FundMode, pwd: String, old: String?, totp: String?, code: String?) async throws {
        let body = APIFundPasswordRequest(fundPassword: pwd, oldFundPassword: old, totpCode: totp, code: code)
        switch mode {
        case .set: let _: APIOkResponse = try await api.post("/account/fund-password", body: body)
        case .change: let _: APIOkResponse = try await api.put("/account/fund-password", body: body)
        case .reset: let _: APIOkResponse = try await api.post("/account/fund-password/reset", body: body)
        }
    }
    static func setAntiPhishing(_ c: String, totp: String?, code: String?) async throws {
        let _: APIOkResponse = try await api.put("/account/anti-phishing",
            body: APISetAntiPhishingRequest(antiPhishingCode: c, totpCode: totp, code: code))
    }
    static func sessions() async throws -> [APISession] {
        let r: APIListSessions200Response = try await api.get("/account/sessions", auth: true); return r.items ?? []
    }
    static func revokeSession(_ id: String) async throws { try await api.deleteAuth("/account/sessions/\(id.urlPath)") }
    static func revokeOthers() async throws -> Int {
        let r: APIRevokeOtherSessions200Response = try await api.post("/account/sessions/revoke-others", body: Optional<Int>.none)
        return r.revoked ?? 0
    }
    static func devices() async throws -> [APIDevice] {
        let r: APIDeviceList = try await api.get("/account/devices", auth: true); return r.items
    }
    static func removeDevice(_ id: String) async throws { try await api.deleteAuth("/account/devices/\(id.urlPath)") }
    static func passkeys() async throws -> APIListPasskeys200Response { try await api.get("/account/passkeys", auth: true) }
    static func deletePasskey(_ id: String) async throws { try await api.deleteAuth("/account/passkeys/\(id.urlPath)") }
    static func beginPasskey(totp: String?, code: String?) async throws -> APIPasskeyCeremony {
        try await api.post("/account/passkeys/register/begin", body: APIBeginPasskeyRegistrationRequest(totpCode: totp, code: code))
    }
    static func finishPasskey(session: String, name: String, credential: [String: Any]) async throws -> APIPasskey {
        try await api.post("/account/passkeys/register/finish",
            body: APIFinishPasskeyRegistrationRequest(sessionId: session, name: name, credential: AnyCodable(credential)))
    }
    static func disableTotp(_ code: String) async throws {
        let _: APITwoFactorStatus = try await api.post("/2fa/totp/disable", body: APIDisableTotpRequest(code: code))
    }
}

extension String {
    /// Path-segment percent-encoding shared by SecurityAPI / HomeAPI claim paths.
    var urlPath: String { addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/"))) ?? self }
}

/// 统一中文错误：先按错误码映射（含 401 的 INVALID_TOTP / PASSKEY_INVALID），再走通用兜底
func securityMessage(_ e: Error) -> String {
    if let f = e as? APIFailure, let c = f.code, let m = ErrorText.map[c] { return m }
    return e.lbMessage
}

// MARK: Passkey（WebAuthn）— iOS 平台验证器
enum PasskeyError: LocalizedError {
    case badOptions, cancelled
    var errorDescription: String? { self == .badOptions ? "通行密钥参数异常" : "已取消" }
}

extension Data {
    init?(base64url s: String) {
        var b = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while b.count % 4 != 0 { b += "=" }
        self.init(base64Encoded: b)
    }
    var base64url: String {
        base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }
}

@MainActor
final class PasskeyRegistrar: NSObject, ASAuthorizationControllerDelegate, ASAuthorizationControllerPresentationContextProviding {
    private var cont: CheckedContinuation<[String: Any], Error>?

    private func resumeOnce(_ result: Result<[String: Any], Error>) {
        guard let c = cont else { return }
        cont = nil
        c.resume(with: result)
    }

    /// 读取 PublicKeyCredentialCreationOptions（可能包了一层 publicKey），调起系统通行密钥，返回 PublicKeyCredential.toJSON() 等价结构
    func register(options raw: [String: AnyCodable]) async throws -> [String: Any] {
        var o: [String: Any] = raw.mapValues { $0.value }
        if let inner = o["publicKey"] as? [String: Any] { o = inner }
        guard let ch = o["challenge"] as? String, let challenge = Data(base64url: ch),
              let rp = o["rp"] as? [String: Any], let rpId = rp["id"] as? String,
              let user = o["user"] as? [String: Any], let uidS = user["id"] as? String, let uid = Data(base64url: uidS)
        else { throw PasskeyError.badOptions }
        let name = (user["name"] as? String) ?? "Linbit"
        let provider = ASAuthorizationPlatformPublicKeyCredentialProvider(relyingPartyIdentifier: rpId)
        let req = provider.createCredentialRegistrationRequest(challenge: challenge, name: name, userID: uid)
        let c = ASAuthorizationController(authorizationRequests: [req])
        c.delegate = self; c.presentationContextProvider = self
        return try await withCheckedThrowingContinuation { k in
            if let prev = cont { cont = nil; prev.resume(throwing: PasskeyError.cancelled) }
            cont = k; c.performRequests()
        }
    }

    /// 登录断言：对齐 Web `passkeyLogin` / PublicKeyCredentialRequestOptions
    /// （challenge、rpId、allowCredentials、userVerification）
    func assert(options raw: [String: AnyCodable]) async throws -> [String: Any] {
        var o: [String: Any] = raw.mapValues { $0.value }
        if let inner = o["publicKey"] as? [String: Any] { o = inner }
        guard let ch = o["challenge"] as? String, let challenge = Data(base64url: ch) else { throw PasskeyError.badOptions }
        let rpId = (o["rpId"] as? String) ?? ((o["rp"] as? [String: Any])?["id"] as? String)
        guard let rpId else { throw PasskeyError.badOptions }
        let provider = ASAuthorizationPlatformPublicKeyCredentialProvider(relyingPartyIdentifier: rpId)
        let req = provider.createCredentialAssertionRequest(challenge: challenge)
        if let list = o["allowCredentials"] as? [[String: Any]] {
            var descs: [ASAuthorizationPlatformPublicKeyCredentialDescriptor] = []
            for c in list {
                guard let idS = c["id"] as? String, let id = Data(base64url: idS) else { continue }
                descs.append(ASAuthorizationPlatformPublicKeyCredentialDescriptor(credentialID: id))
            }
            if !descs.isEmpty { req.allowedCredentials = descs }
        }
        if let uv = o["userVerification"] as? String {
            switch uv {
            case "required": req.userVerificationPreference = .required
            case "preferred": req.userVerificationPreference = .preferred
            case "discouraged": req.userVerificationPreference = .discouraged
            default: break
            }
        }
        let c = ASAuthorizationController(authorizationRequests: [req])
        c.delegate = self; c.presentationContextProvider = self
        return try await withCheckedThrowingContinuation { k in
            if let prev = cont { cont = nil; prev.resume(throwing: PasskeyError.cancelled) }
            cont = k; c.performRequests()
        }
    }

    nonisolated func authorizationController(controller: ASAuthorizationController, didCompleteWithAuthorization a: ASAuthorization) {
        Task { @MainActor in
            if let r = a.credential as? ASAuthorizationPlatformPublicKeyCredentialRegistration, let att = r.rawAttestationObject {
                let id = r.credentialID.base64url
                resumeOnce(.success([
                    "id": id, "rawId": id, "type": "public-key", "authenticatorAttachment": "platform",
                    "response": ["clientDataJSON": r.rawClientDataJSON.base64url, "attestationObject": att.base64url, "transports": ["internal", "hybrid"]],
                    "clientExtensionResults": [String: Any]()
                ]))
                return
            }
            if let r = a.credential as? ASAuthorizationPlatformPublicKeyCredentialAssertion {
                let id = r.credentialID.base64url
                var response: [String: Any] = [
                    "clientDataJSON": r.rawClientDataJSON.base64url,
                    "authenticatorData": r.rawAuthenticatorData.base64url,
                    "signature": r.signature.base64url
                ]
                // 与 WebAuthn JSON 一致：无 userHandle 时省略字段，不塞 NSNull
                if let uh = r.userID { response["userHandle"] = uh.base64url }
                resumeOnce(.success([
                    "id": id, "rawId": id, "type": "public-key", "authenticatorAttachment": "platform",
                    "response": response,
                    "clientExtensionResults": [String: Any]()
                ]))
                return
            }
            resumeOnce(.failure(PasskeyError.badOptions))
        }
    }
    nonisolated func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: Error) {
        Task { @MainActor in
            let cancelled = (error as? ASAuthorizationError)?.code == .canceled
            resumeOnce(.failure(cancelled ? PasskeyError.cancelled : error))
        }
    }
    nonisolated func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow }.first ?? ASPresentationAnchor()
        }
    }
}
