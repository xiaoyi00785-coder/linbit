package click.cowa.linbit.feature.profile

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.auth.AccountId
import click.cowa.linbit.core.auth.PasswordRule
import click.cowa.linbit.core.generated.ApiSecurityOverview
import click.cowa.linbit.core.generated.ApiSendCodeAuthedRequest
import click.cowa.linbit.core.generated.ApiSetAntiPhishingRequest
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import javax.inject.Inject

/** Wire shape is mandatory so encodeDefaults=false cannot omit revokeOtherSessions=true. */
@Serializable
data class SecurityChangePasswordRequest(
    @SerialName("oldPassword") val oldPassword: String,
    @SerialName("newPassword") val newPassword: String,
    @SerialName("totpCode") val totpCode: String? = null,
    @SerialName("code") val code: String? = null,
    @SerialName("revokeOtherSessions") val revokeOtherSessions: Boolean,
)

/** 安全表单的共享校验；测试使用验证用字符串，不使用真实凭据。 */
object SecurityFeatureLogic {
    fun antiPhishingError(raw: String): String? = when {
        raw.length !in 4..20 -> "防钓鱼码需为 4-20 位字母或数字"
        !raw.all { it.code < 128 && it.isLetterOrDigit() } -> "仅支持字母和数字"
        else -> null
    }

    fun contactError(raw: String, phone: Boolean): String? = when {
        phone -> AccountId.fromRaw(raw.filter(Char::isDigit), "86").let {
            if (it.kind == AccountId.Kind.PHONE && it.isValid) null else "请输入正确的手机号码"
        }
        !raw.contains("@") || !AccountId.fromRaw(raw, "86").isValid -> "请输入正确的邮箱地址"
        else -> null
    }

    fun verificationReady(overview: ApiSecurityOverview?, totp: String, code: String): Boolean = when {
        overview == null -> false // Never treat missing overview as non-2FA.
        overview.twoFactor?.enabled == true -> totp.length == 6
        else -> code.length >= 6
    }

    fun passwordError(oldPassword: String, newPassword: String, confirmation: String): String? = when {
        oldPassword.isEmpty() -> "请输入原密码"
        !PasswordRule.ok(newPassword) -> PasswordRule.hint(newPassword) ?: "新密码需 8-64 位且包含字母和数字"
        newPassword == oldPassword -> "新密码不能与原密码相同"
        confirmation != newPassword -> "两次输入的密码不一致"
        else -> null
    }
}

@HiltViewModel
class SecurityFeatureViewModel @Inject constructor(
    private val api: LinbitApi,
    val toast: ToastCenter,
) : ViewModel() {
    /** Suspend core allows exact request/result tests and a single completion/error path in the UI. */
    suspend fun runPasswordChange(oldPassword: String, newPassword: String, confirmation: String, overview: ApiSecurityOverview?, totp: String, code: String): Int? {
        val localError = SecurityFeatureLogic.passwordError(oldPassword, newPassword, confirmation)
        require(localError == null) { localError ?: "密码校验失败" }
        require(SecurityFeatureLogic.verificationReady(overview, totp, code)) { "请先完成安全验证" }
        return apiCall { api.changePassword(SecurityChangePasswordRequest(
            oldPassword = oldPassword, newPassword = newPassword,
            totpCode = totp.takeIf { overview?.twoFactor?.enabled == true },
            code = code.takeUnless { overview?.twoFactor?.enabled == true },
            revokeOtherSessions = true,
        )) }.revokedSessions
    }

    fun changePassword(oldPassword: String, newPassword: String, confirmation: String = newPassword, overview: ApiSecurityOverview?, totp: String, code: String, error: (String) -> Unit, done: (Int?) -> Unit) {
        viewModelScope.launch { runCatchingFeature(error) {
            val revoked = runPasswordChange(oldPassword, newPassword, confirmation, overview, totp, code)
            toast.show("登录密码已修改；其他会话已注销")
            done(revoked)
        } }
    }

    suspend fun runAntiPhishing(value: String, overview: ApiSecurityOverview?, totp: String, code: String) {
        val localError = SecurityFeatureLogic.antiPhishingError(value)
        require(localError == null) { localError ?: "防钓鱼码校验失败" }
        require(SecurityFeatureLogic.verificationReady(overview, totp, code)) { "请先完成安全验证" }
        apiCall { api.setAntiPhishing(ApiSetAntiPhishingRequest(value,
            totpCode = totp.takeIf { overview?.twoFactor?.enabled == true },
            code = code.takeUnless { overview?.twoFactor?.enabled == true },
        )) }
    }

    fun setAntiPhishing(value: String, overview: ApiSecurityOverview?, totp: String, code: String, error: (String) -> Unit, done: () -> Unit) {
        viewModelScope.launch { runCatchingFeature(error) {
            runAntiPhishing(value, overview, totp, code)
            toast.show("防钓鱼码已保存"); done()
        } }
    }

    private suspend inline fun runCatchingFeature(error: (String) -> Unit, block: () -> Unit) {
        try { block() } catch (e: kotlinx.coroutines.CancellationException) { throw e } catch (e: Exception) { error(e.lbMessage()) }
    }

    suspend fun sendSecurityCode() { apiCall { api.sendAccountCode(ApiSendCodeAuthedRequest(purpose = ApiSendCodeAuthedRequest.Purpose.SECURITY)) } }
}
