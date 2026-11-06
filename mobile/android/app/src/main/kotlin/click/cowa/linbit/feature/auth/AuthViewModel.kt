package click.cowa.linbit.feature.auth

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.auth.AccountId
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.auth.LoginResult
import click.cowa.linbit.core.generated.ApiLoginChallenge
import click.cowa.linbit.core.generated.ApiSendCodeRequest
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.ErrorText
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * 登录 / 注册 / 找回密码 全流程的共享状态（对应 iOS AuthModel + 各步骤视图里的提交逻辑）。
 *  登录：账户(ref2 #3/#9) → 密码(#10) → [新设备] 验证码(#11，totp/sms/email) → 完成
 *  注册：账户+邀请码+协议(#4/#5/#6/#8) → 发送验证码 → 验证码(#11) → 设置密码 → /auth/register → 完成
 *  找回：账户 → 验证码 → 新密码×2 → 返回登录
 */
@HiltViewModel
class AuthViewModel @Inject constructor(
    private val auth: AuthRepository,
    private val tokens: TokenStore,
    private val prefs: AppPrefs,
    val toast: ToastCenter,
    private val passkey: PasskeyLoginService,
) : ViewModel() {
    var raw by mutableStateOf("")
    var countryCode by mutableStateOf("86")
    var password by mutableStateOf("")
    var invite by mutableStateOf("")
    var code by mutableStateOf("")
    var challenge by mutableStateOf<ApiLoginChallenge?>(null)
    var loading by mutableStateOf(false)
    /** 账户输入框下方的错误提示 */
    var accountError by mutableStateOf<String?>(null)
    /** 密码页错误提示 */
    var passwordError by mutableStateOf<String?>(null)
    /** 429 RATE_LIMITED：按 Retry-After 倒计时，期间禁止再次提交 */
    var cooldown by mutableStateOf(0)
    var lastLogin by mutableStateOf("")
    var passkeyLoading by mutableStateOf(false); private set
    var passkeyError by mutableStateOf<String?>(null); private set
    private var passkeyInFlight = false
    private var passkeyGeneration = 0L
    private var passkeyEpoch = tokens.sessionVersion.value
    private var passkeyJob: kotlinx.coroutines.Job? = null

    val id: AccountId get() = AccountId.fromRaw(raw, countryCode)

    init {
        viewModelScope.launch { lastLogin = prefs.lastLogin.first() }
        viewModelScope.launch { tokens.sessionVersion.collect { version ->
            if (passkeyEpoch != version) {
                passkeyEpoch = version; passkeyGeneration++; passkeyJob?.cancel()
                passkeyInFlight = false; passkeyLoading = false; passkeyError = null
            }
        } }
    }
    fun loginWithPasskey(activity: android.app.Activity, onDone: () -> Unit) {
        if (passkeyInFlight || loading || tokens.loggedIn.value || activity.isFinishing || activity.isDestroyed) return
        passkeyInFlight = true; passkeyLoading = true; passkeyError = null
        val generation = ++passkeyGeneration; val epoch = passkeyEpoch
        passkeyJob = viewModelScope.launch {
            try {
                val token = passkey.login(activity)
                val current = generation == passkeyGeneration && epoch == passkeyEpoch && tokens.sessionVersion.value == epoch && !tokens.loggedIn.value
                if (current) {
                    tokens.save(token)
                    if (generation == passkeyGeneration && epoch == passkeyEpoch) { password = ""; onDone() }
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                if (generation == passkeyGeneration && epoch == passkeyEpoch) passkeyError = (e as? ApiFailure)?.userMessage() ?: e.message ?: "通行密钥登录失败，请稍后重试"
            } finally {
                if (generation == passkeyGeneration && epoch == passkeyEpoch) { passkeyInFlight = false; passkeyLoading = false }
            }
        }
    }
    fun cancelPasskeyLogin() {
        if (!passkeyInFlight) return
        passkeyGeneration++; passkeyJob?.cancel(); passkeyInFlight = false; passkeyLoading = false
        passkeyError = PasskeyLoginFailure.Cancelled.message
    }
    override fun onCleared() { passkeyGeneration++; super.onCleared() }

    fun onRawChange(v: String) { raw = v; accountError = null }
    fun onPasswordChange(v: String) { password = v; passwordError = null }

    /** 登录第一步：校验账户格式后进入密码页。 */
    fun submitAccount(onNext: () -> Unit) {
        if (raw.isBlank()) { accountError = "请输入邮箱或手机号码"; return }
        if (!id.isValid) { accountError = if (id.kind == AccountId.Kind.PHONE) "手机号格式不正确" else "邮箱格式不正确"; return }
        accountError = null
        onNext()
    }

    fun submitPassword(onChallenge: () -> Unit, onDone: () -> Unit) {
        if (password.isEmpty()) { passwordError = "请输入密码"; return }
        doSubmitPassword(onChallenge, onDone)
    }

    private fun doSubmitPassword(onChallenge: () -> Unit, onDone: () -> Unit) = launch(onError = { toast.error(it) }) {
        try {
            when (val r = auth.login(id, password)) {
                is LoginResult.Done -> finishLogin(onDone)
                is LoginResult.Challenge -> { challenge = r.challenge; code = ""; onChallenge() }
            }
        } catch (e: ApiFailure) {
            when {
                e.code == "AUTH_INVALID_CREDENTIALS" -> passwordError = "请输入正确的账户和密码"
                e.code == "RATE_LIMITED" || e.httpStatus == 429 -> startCooldown(e.retryAfterSeconds ?: 60) // 不进入验证码页
                e.code in setOf("EMAIL_UNAVAILABLE", "SMS_UNAVAILABLE", "INVALID_EMAIL") -> // 验证码未能发出：留在本页显示错误
                    passwordError = ErrorText.map[e.code] ?: e.userMessage()
                else -> toast.error(e, tokens.sessionExpired)
            }
        }
    }

    private fun startCooldown(seconds: Int) {
        cooldown = seconds.coerceAtLeast(1)
        viewModelScope.launch {
            while (cooldown > 0) { kotlinx.coroutines.delay(1_000); cooldown -= 1 }
        }
    }

    /** 登录二次验证 / 注册验证码 / 找回验证码 的「确认」。 */
    fun submitCode(mode: VerifyMode, method: ApiLoginChallenge.Methods, onDone: () -> Unit, onSetPassword: () -> Unit, onResetToken: (String) -> Unit) {
        if (code.length < 6) { toast.show("请输入6位验证码", ToastCenter.Kind.ERROR); return }
        doSubmitCode(mode, method, onDone, onSetPassword, onResetToken)
    }

    private fun doSubmitCode(mode: VerifyMode, method: ApiLoginChallenge.Methods, onDone: () -> Unit, onSetPassword: () -> Unit, onResetToken: (String) -> Unit) = launch(onError = {
        code = ""
        val limited = (it as? ApiFailure)?.code in setOf("RATE_LIMITED", "CODE_TOO_MANY_ATTEMPTS")
        toast.show(if (limited) "尝试次数过多，请稍后再试" else "验证码错误或已过期", ToastCenter.Kind.ERROR)
    }) {
        when (mode) {
            VerifyMode.LOGIN -> { auth.verifyLogin(challenge ?: return@launch, method, code); finishLogin(onDone) }
            VerifyMode.REGISTER -> onSetPassword() // 注册接口需要 code + password，一并在设置密码页提交
            VerifyMode.RESET -> onResetToken(auth.verifyResetCode(id, code))
        }
    }

    suspend fun resend(mode: VerifyMode): Boolean = try {
        when (mode) {
            VerifyMode.LOGIN -> challenge?.let { auth.resendLoginCode(it) }
            VerifyMode.REGISTER -> auth.sendCode(id, ApiSendCodeRequest.Purpose.REGISTER)
            VerifyMode.RESET -> auth.sendCode(id, ApiSendCodeRequest.Purpose.RESET_PASSWORD)
        }
        toast.show("验证码已发送", ToastCenter.Kind.SUCCESS); true
    } catch (e: ApiFailure) { toast.error(e); false }

    /** 注册第一步：发送验证码。 */
    fun submitRegister(agree: Boolean, onNext: () -> Unit) {
        if (raw.isBlank()) { accountError = "请输入邮箱或手机号码"; return }
        if (!id.isValid) { accountError = if (id.kind == AccountId.Kind.PHONE) "手机号格式不正确" else "邮箱格式不正确"; return }
        if (!agree) { toast.show("请先同意服务条款", ToastCenter.Kind.ERROR); return }
        launch(onError = {
            if ((it as? ApiFailure)?.code in setOf("AUTH_EMAIL_EXISTS", "AUTH_PHONE_EXISTS")) accountError = "该账户已注册，请直接登录"
            else toast.error(it)
        }) { auth.sendCode(id, ApiSendCodeRequest.Purpose.REGISTER); code = ""; onNext() }
    }

    fun submitForgot(onNext: () -> Unit) {
        if (!id.isValid || raw.isBlank()) { accountError = "请输入正确的邮箱/手机号"; return }
        launch { auth.sendCode(id, ApiSendCodeRequest.Purpose.RESET_PASSWORD); code = ""; onNext() }
    }

    /** 设置密码：注册 → /auth/register；重置 → /auth/password/reset。 */
    fun submitNewPassword(pwd: String, pwd2: String, resetToken: String?, setError: (String?) -> Unit, onRegistered: () -> Unit, onReset: () -> Unit, onTokenExpired: () -> Unit, onCodeInvalid: () -> Unit) {
        launch(onError = {
            when ((it as? ApiFailure)?.code) {
                "RESET_TOKEN_INVALID" -> { toast.show("重置令牌已过期，请重新获取验证码", ToastCenter.Kind.ERROR); onTokenExpired() }
                "CODE_INVALID" -> { toast.show("验证码错误或已过期", ToastCenter.Kind.ERROR); onCodeInvalid() }
                else -> toast.error(it)
            }
        }) {
            if (resetToken == null) {
                auth.register(id, pwd, code, invite)
                finishLogin(onRegistered)
                toast.show("注册成功", ToastCenter.Kind.SUCCESS)
            } else {
                if (pwd != pwd2) { setError("两次输入的密码不一致"); return@launch }
                auth.resetPassword(resetToken, pwd)
                toast.show("密码已重置，请使用新密码登录", ToastCenter.Kind.SUCCESS)
                password = ""
                onReset()
            }
        }
    }

    private suspend fun finishLogin(onDone: () -> Unit) {
        prefs.setLastLogin(if (id.kind == AccountId.Kind.EMAIL) id.email else id.phone)
        password = ""
        onDone()
    }

    private fun launch(onError: (Throwable) -> Unit = { toast.error(it) }, block: suspend () -> Unit) {
        viewModelScope.launch {
            loading = true
            try { block() } catch (e: kotlinx.coroutines.CancellationException) { throw e } catch (e: Throwable) { onError(e) } finally { loading = false }
        }
    }
}

enum class VerifyMode { LOGIN, REGISTER, RESET }
