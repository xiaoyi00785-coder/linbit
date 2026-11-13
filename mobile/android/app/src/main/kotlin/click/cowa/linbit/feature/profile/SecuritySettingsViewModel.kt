package click.cowa.linbit.feature.profile

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 安全设置二级页（修改密码、换绑联系方式、资金密码、防钓鱼码、会话 / 设备、通行密钥）的状态。 */
data class SecuritySettingsUiState(
    val submitting: Boolean = false,
    val error: String? = null,
    /** 发往当前邮箱 / 手机的安全验证码（purpose=security）倒计时 */
    val securityCooldown: Int = 0,
    /** 发往新邮箱 / 手机的绑定验证码倒计时 */
    val newContactCooldown: Int = 0,
    val sending: Boolean = false,
    val listLoading: Boolean = false,
    val sessions: List<ApiSession>? = null,
    val devices: List<ApiDevice>? = null,
    val passkeysEnabled: Boolean = false,
    val passkeys: List<ApiPasskey>? = null,
)

enum class FundPasswordMode { SET, CHANGE, RESET }

/**
 * 二次验证：已开启 TOTP 时提交动态码，否则提交发到当前联系方式的验证码（purpose=security）。
 * 与服务端 requireSensitiveAuth 一致。
 */
data class SecondFactor(val useTotp: Boolean, val value: String) {
    val totpCode: String? get() = if (useTotp) value else null
    val code: String? get() = if (useTotp) null else value
}

@HiltViewModel
class SecuritySettingsViewModel @Inject constructor(
    private val api: LinbitApi,
    val toast: ToastCenter,
) : ViewModel() {
    private val _s = MutableStateFlow(SecuritySettingsUiState())
    val state: StateFlow<SecuritySettingsUiState> = _s.asStateFlow()
    private var securityTimer: Job? = null
    private var newContactTimer: Job? = null

    fun clearError() { _s.value = _s.value.copy(error = null) }

    // ---- 验证码 ----
    /**
     * 向当前联系方式发送安全验证码。channel 为空时由服务端选（有手机优先短信）；
     * 换绑邮箱 / 手机时必须指定，服务端会在「被替换的那个联系方式」上校验 oldCode。
     */
    fun sendSecurityCode(channel: ApiSendCodeAuthedRequest.Channel? = null) {
        if (_s.value.sending || _s.value.securityCooldown > 0) return
        viewModelScope.launch {
            _s.value = _s.value.copy(sending = true, error = null)
            try {
                apiCall { api.sendAccountCode(ApiSendCodeAuthedRequest(purpose = ApiSendCodeAuthedRequest.Purpose.SECURITY, channel = channel)) }
                toast.show("验证码已发送")
                _s.value = _s.value.copy(sending = false)
                securityTimer?.cancel()
                securityTimer = countdown { n -> _s.value = _s.value.copy(securityCooldown = n) }
            } catch (e: Exception) { _s.value = _s.value.copy(sending = false, error = e.lbMessage()) }
        }
    }

    /** 向新的邮箱（email 非空）或手机（phone + countryCode）发送绑定验证码 */
    fun sendNewContactCode(email: String?, phone: String?, countryCode: String?) {
        if (_s.value.sending || _s.value.newContactCooldown > 0) return
        viewModelScope.launch {
            _s.value = _s.value.copy(sending = true, error = null)
            try {
                apiCall { api.sendContactCode(ApiSendContactCodeRequest(email = email, phone = phone, countryCode = countryCode)) }
                toast.show("验证码已发送")
                _s.value = _s.value.copy(sending = false)
                newContactTimer?.cancel()
                newContactTimer = countdown { n -> _s.value = _s.value.copy(newContactCooldown = n) }
            } catch (e: Exception) { _s.value = _s.value.copy(sending = false, error = e.lbMessage()) }
        }
    }

    private fun countdown(from: Int = 60, set: (Int) -> Unit): Job = viewModelScope.launch {
        var n = from
        while (n > 0) { set(n); delay(1_000); n -= 1 }
        set(0)
    }

    // ---- 提交类操作：成功后 toast + onDone，失败把错误显示在页面上 ----
    private fun submit(success: String, onDone: () -> Unit, block: suspend () -> Unit) {
        if (_s.value.submitting) return
        viewModelScope.launch {
            _s.value = _s.value.copy(submitting = true, error = null)
            try {
                block()
                _s.value = _s.value.copy(submitting = false)
                toast.show(success)
                onDone()
            } catch (e: Exception) { _s.value = _s.value.copy(submitting = false, error = e.lbMessage()) }
        }
    }

    fun changePassword(old: String, new: String, factor: SecondFactor, onDone: () -> Unit) =
        submit("登录密码已修改，其他设备已退出登录", onDone) {
            apiCall { api.changePassword(ApiChangePasswordRequest(oldPassword = old, newPassword = new, totpCode = factor.totpCode, code = factor.code, revokeOtherSessions = true)) }
        }

    fun fundPassword(mode: FundPasswordMode, pwd: String, old: String?, factor: SecondFactor, onDone: () -> Unit) {
        val body = ApiFundPasswordRequest(fundPassword = pwd, oldFundPassword = old, totpCode = factor.totpCode, code = factor.code)
        val msg = when (mode) { FundPasswordMode.SET -> "资金密码已设置"; FundPasswordMode.CHANGE -> "资金密码已修改"; FundPasswordMode.RESET -> "资金密码已重置" }
        submit(msg, onDone) {
            when (mode) {
                FundPasswordMode.SET -> apiCall { api.setFundPassword(body) }
                FundPasswordMode.CHANGE -> apiCall { api.changeFundPassword(body) }
                FundPasswordMode.RESET -> apiCall { api.resetFundPassword(body) }
            }
        }
    }

    fun setAntiPhishing(code: String, factor: SecondFactor, onDone: () -> Unit) =
        submit("防钓鱼码已设置", onDone) {
            apiCall { api.setAntiPhishing(ApiSetAntiPhishingRequest(antiPhishingCode = code, totpCode = factor.totpCode, code = factor.code)) }
        }

    /**
     * 换绑邮箱 / 手机：newCode 发到新联系方式，oldCode 发到当前联系方式（purpose=security），
     * 已开启 TOTP 时还需动态码。
     */
    fun changeContact(email: Boolean, value: String, countryCode: String?, newCode: String, oldCode: String, totp: String?, onDone: () -> Unit) =
        submit(if (email) "邮箱已更换" else "手机号已更换", onDone) {
            val body = ApiChangeContactRequest(
                newCode = newCode, oldCode = oldCode, totpCode = totp,
                email = if (email) value else null,
                phone = if (email) null else value,
                countryCode = if (email) null else countryCode,
            )
            if (email) apiCall { api.changeEmail(body) } else apiCall { api.changePhone(body) }
        }

    // ---- 会话 / 设备 ----
    fun loadSessionsAndDevices() {
        viewModelScope.launch {
            _s.value = _s.value.copy(listLoading = true, error = null)
            try {
                val sessions = apiCall { api.sessions() }.items.orEmpty()
                val devices = runCatching { apiCall { api.devices() }.items }.getOrDefault(emptyList())
                _s.value = _s.value.copy(listLoading = false, sessions = sessions, devices = devices)
            } catch (e: Exception) { _s.value = _s.value.copy(listLoading = false, error = e.lbMessage()) }
        }
    }

    fun revokeSession(id: String) = submit("已退出该会话", ::loadSessionsAndDevices) { apiCall { api.revokeSession(id) } }

    fun revokeOtherSessions() {
        if (_s.value.submitting) return
        viewModelScope.launch {
            _s.value = _s.value.copy(submitting = true, error = null)
            try {
                val n = apiCall { api.revokeOtherSessions() }.revoked ?: 0
                _s.value = _s.value.copy(submitting = false)
                toast.show("已退出 $n 个其他会话")
                loadSessionsAndDevices()
            } catch (e: Exception) { _s.value = _s.value.copy(submitting = false, error = e.lbMessage()) }
        }
    }

    fun removeDevice(deviceId: String) = submit("已移除该设备", ::loadSessionsAndDevices) { apiCall { api.removeDevice(deviceId) } }

    // ---- 通行密钥（列表 / 删除；在 Android 上添加需接入 Credential Manager，暂未开放）----
    fun loadPasskeys() {
        viewModelScope.launch {
            _s.value = _s.value.copy(listLoading = true, error = null)
            try {
                val r = apiCall { api.passkeys() }
                _s.value = _s.value.copy(listLoading = false, passkeysEnabled = r.enabled == true, passkeys = r.items.orEmpty())
            } catch (e: Exception) { _s.value = _s.value.copy(listLoading = false, error = e.lbMessage()) }
        }
    }

    fun deletePasskey(id: String) = submit("通行密钥已删除", ::loadPasskeys) { apiCall { api.deletePasskey(id) } }
}
