package click.cowa.linbit.core.auth

import click.cowa.linbit.core.generated.ApiLoginChallenge
import click.cowa.linbit.core.generated.ApiLoginRequest
import click.cowa.linbit.core.generated.ApiLoginResendCodeRequest
import click.cowa.linbit.core.generated.ApiLoginVerifyRequest
import click.cowa.linbit.core.generated.ApiLogoutRequest
import click.cowa.linbit.core.generated.ApiPasswordResetRequest
import click.cowa.linbit.core.generated.ApiRegisterRequest
import click.cowa.linbit.core.generated.ApiSendCodeRequest
import click.cowa.linbit.core.generated.ApiVerifyResetCodeRequest
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.LoginResponse
import click.cowa.linbit.core.network.OneOfResponses
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

sealed interface LoginResult {
    data object Done : LoginResult
    data class Challenge(val challenge: ApiLoginChallenge) : LoginResult
}

/** 登录 / 退出（对应 iOS AuthAPI）。 */
class AuthRepository(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    private val deviceId: () -> String,
    private val deviceName: () -> String,
    private val scope: CoroutineScope,
) {
    suspend fun login(id: AccountId, password: String): LoginResult {
        val body = ApiLoginRequest(
            password = password, email = id.emailOrNull, phone = id.phoneOrNull, countryCode = id.ccOrNull,
            deviceId = deviceId(), deviceName = deviceName(),
        )
        return when (val r = apiCall { OneOfResponses.login(api.login(body)) }) {
            is LoginResponse.Tokens -> { tokens.save(r.tokens); LoginResult.Done }
            is LoginResponse.Challenge -> LoginResult.Challenge(r.challenge)
        }
    }

    /** 二次验证：TOTP 用 totpCode，邮箱/短信验证码用 code。 */
    suspend fun verifyLogin(c: ApiLoginChallenge, method: ApiLoginChallenge.Methods, code: String) {
        val totp = method == ApiLoginChallenge.Methods.TOTP
        val body = ApiLoginVerifyRequest(c.challengeToken, totpCode = if (totp) code else null, code = if (totp) null else code)
        tokens.save(apiCall { api.loginVerify(body) })
    }

    suspend fun resendLoginCode(c: ApiLoginChallenge) {
        apiCall { api.loginResendCode(ApiLoginResendCodeRequest(c.challengeToken)) }
    }

    /**
     * 退出：请求体始终带 refreshToken（访问令牌过期时服务端据此识别用户）；
     * 503 LOGOUT_INCOMPLETE（访问令牌吊销暂时失败）时退避重试，最多 3 次。
     * 退出后旧访问令牌会立即 401，期间置 loggingOut，避免误报「登录已过期」。
     */
    suspend fun logout() {
        tokens.loggingOut = true
        val body = ApiLogoutRequest(tokens.refreshToken)
        for (attempt in 0 until 3) {
            try {
                apiCall { api.logout(body) }
                break
            } catch (e: ApiFailure) {
                if (e.code == "LOGOUT_INCOMPLETE" || e.httpStatus == 503) {
                    if (attempt < 2) delay((attempt + 1) * 800L)
                } else break // 401 等：令牌已无效，本地清除即可
            }
        }
        tokens.clear()
        tokens.sessionExpired = false
        // 在途请求可能仍携带旧令牌，稍后再恢复「登录过期」提示
        scope.launch { delay(3_000); if (tokens.accessToken == null) tokens.loggingOut = false }
    }

    suspend fun sendCode(id: AccountId, purpose: ApiSendCodeRequest.Purpose) {
        apiCall { api.sendCode(ApiSendCodeRequest(purpose, id.emailOrNull, id.phoneOrNull, id.ccOrNull)) }
    }

    suspend fun register(id: AccountId, password: String, code: String, invite: String?) {
        val body = ApiRegisterRequest(password, code, id.emailOrNull, id.phoneOrNull, id.ccOrNull, invite?.takeIf { it.isNotEmpty() })
        tokens.save(apiCall { api.register(body) })
    }

    suspend fun verifyResetCode(id: AccountId, code: String): String =
        apiCall { api.verifyResetCode(ApiVerifyResetCodeRequest(code, id.emailOrNull, id.phoneOrNull, id.ccOrNull)) }.resetToken

    suspend fun resetPassword(token: String, newPassword: String) {
        apiCall { api.resetPassword(ApiPasswordResetRequest(token, newPassword)) }
    }
}
