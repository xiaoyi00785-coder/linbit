package click.cowa.linbit.feature.profile

import android.content.Context
import android.net.Uri
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.RequestBody.Companion.toRequestBody
import javax.inject.Inject

data class ProfileUiState(
    val loading: Boolean = false,
    val error: String? = null,
    val account: ApiAccount? = null,
    val kyc: ApiKycStatus? = null,
    val security: ApiSecurityOverview? = null,
    val saved: List<SavedAccount> = emptyList(),
    val switching: Boolean = false,
)

@HiltViewModel
class ProfileViewModel @Inject constructor(
    private val api: LinbitApi,
    private val accounts: SavedAccounts,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    private val _s = MutableStateFlow(ProfileUiState())
    val state: StateFlow<ProfileUiState> = _s.asStateFlow()
    val loggedIn = tokens.loggedIn

    init { restoreInterruptedSwitch() }

    /** 离屏证据：本地示例账号资料，非真实个人/生产会话。 */
    internal var holdEvidence: Boolean = false
    internal fun seedEvidenceProfile(account: ApiAccount, kyc: ApiKycStatus? = null, security: ApiSecurityOverview? = null, saved: List<SavedAccount> = emptyList()) {
        holdEvidence = true
        _s.value = ProfileUiState(loading = false, error = null, account = account, kyc = kyc, security = security, saved = saved.ifEmpty { listOf(SavedAccount(account.uid, ProfileLogic.displayName(account), ProfileLogic.contact(account))) })
    }

    fun addAccount(onDone: () -> Unit) {
        val uid = _s.value.account?.uid ?: return
        accounts.beginAddAccount(uid)
        _s.value = ProfileUiState(saved = accounts.list())
        onDone()
    }

    fun load(silent: Boolean = false) {
        if (holdEvidence) return
        viewModelScope.launch {
            if (!silent) _s.value = _s.value.copy(loading = true, error = null)
            try {
                val account = apiCall { api.account() }
                accounts.remember(account)
                val kyc = runCatching { apiCall { api.kycStatus() } }.getOrNull()
                val security = runCatching { apiCall { api.securityOverview() } }.getOrNull()
                _s.value = _s.value.copy(loading = false, error = null, account = account, kyc = kyc, security = security, saved = accounts.list())
            } catch (e: Exception) {
                _s.value = _s.value.copy(loading = false, error = e.lbMessage(), saved = accounts.list())
            }
        }
    }

    fun switchAccount(uid: String, onDone: () -> Unit) {
        if (_s.value.switching || uid == _s.value.account?.uid) { onDone(); return }
        viewModelScope.launch {
            _s.value = _s.value.copy(switching = true, error = null)
            val currentUid = _s.value.account?.uid ?: run {
                _s.value = _s.value.copy(switching = false, error = "当前账号信息尚未加载")
                onDone()
                return@launch
            }
            accounts.beginSwitch(currentUid)
            try {
                if (!accounts.switchTo(uid)) throw IllegalStateException("该账号的登录凭证已失效，请重新登录")
                val target = apiCall { api.account() }
                if (target.uid != uid) throw IllegalStateException("账号校验失败，请重新登录")
                accounts.commit()
                accounts.remember(target)
                _s.value = _s.value.copy(account = target, kyc = null, security = null, saved = accounts.list())
                load(silent = true)
                toast.show("已切换至 ${ProfileLogic.displayName(target)}")
                onDone()
            } catch (e: Exception) {
                accounts.restore()
                load(silent = true)
                toast.show("目标账号登录已失效，请重新登录", click.cowa.linbit.ui.components.ToastCenter.Kind.ERROR)
            } finally { _s.value = _s.value.copy(switching = false) }
        }
    }

    fun removeAccounts(uids: Set<String>, onLogout: () -> Unit) {
        accounts.remove(uids)
        _s.value = _s.value.copy(saved = accounts.list())
        if (uids.contains(_s.value.account?.uid)) onLogout()
    }

    private fun restoreInterruptedSwitch() {
        accounts.restore()
        _s.value = _s.value.copy(saved = accounts.list())
    }
}

@HiltViewModel
class SecurityViewModel @Inject constructor(
    private val api: LinbitApi,
    val toast: ToastCenter,
) : ViewModel() {
    private val _s = MutableStateFlow(SecurityUiState())
    val state: StateFlow<SecurityUiState> = _s.asStateFlow()
    internal var holdEvidence: Boolean = false
    /** 离屏证据：2FA 设置页使用不可用占位，不暴露可扫密钥。 */
    internal var evidenceTotpUiPlaceholder: Boolean = false

    /** 离屏证据：安全概览示例；不写入真实 2FA 密钥。 */
    internal fun seedEvidenceSecurity(overview: ApiSecurityOverview) {
        holdEvidence = true
        _s.value = SecurityUiState(loading = false, security = overview)
    }

    /** 离屏证据：2FA 设置 UI（说明/表单/校验错误）；密钥与 QR 为明确不可用占位，不发起绑定。 */
    internal fun seedEvidenceTotpSetupUi(error: String? = null, code: String = "") {
        holdEvidence = true
        evidenceTotpUiPlaceholder = true
        _s.value = SecurityUiState(
            loading = false,
            bindingLoading = false,
            binding = ApiTotpBinding(
                secret = "PLACEHOLDER-NOT-A-TOTP-SECRET",
                otpauthUri = "linbit-test://totp-unavailable",
            ),
            code = code,
            error = error,
        )
    }

    fun load() {
        if (holdEvidence) return
        viewModelScope.launch {
            _s.value = _s.value.copy(loading = true, error = null)
            try { _s.value = _s.value.copy(loading = false, security = apiCall { api.securityOverview() }) }
            catch (e: Exception) { _s.value = _s.value.copy(loading = false, error = e.lbMessage()) }
        }
    }

    fun beginBinding() {
        if (holdEvidence) return
        viewModelScope.launch {
            _s.value = _s.value.copy(bindingLoading = true, error = null, binding = null, code = "")
            try { _s.value = _s.value.copy(bindingLoading = false, binding = apiCall { api.bindTotp() }) }
            catch (e: Exception) { _s.value = _s.value.copy(bindingLoading = false, error = e.lbMessage()) }
        }
    }

    fun code(raw: String) { _s.value = _s.value.copy(code = raw.filter(Char::isDigit).take(6), error = null) }

    fun verify(onDone: () -> Unit) {
        if (holdEvidence) return // 证据态不提交真实绑定
        viewModelScope.launch {
            _s.value = _s.value.copy(submitting = true, error = null)
            try {
                apiCall { api.verifyTotp(ApiVerifyTotpRequest(_s.value.code)) }
                _s.value = _s.value.copy(submitting = false, code = "", binding = null)
                toast.show("身份验证器已绑定")
                load(); onDone()
            } catch (e: Exception) { _s.value = _s.value.copy(submitting = false, error = e.lbMessage()) }
        }
    }

    fun disable(onDone: () -> Unit) {
        if (holdEvidence) return
        viewModelScope.launch {
            _s.value = _s.value.copy(submitting = true, error = null)
            try {
                apiCall { api.disableTotp(ApiDisableTotpRequest(_s.value.code)) }
                _s.value = _s.value.copy(submitting = false, code = "")
                toast.show("身份验证器已关闭")
                load(); onDone()
            } catch (e: Exception) { _s.value = _s.value.copy(submitting = false, error = e.lbMessage()) }
        }
    }
}

data class SecurityUiState(
    val loading: Boolean = false,
    val bindingLoading: Boolean = false,
    val submitting: Boolean = false,
    val error: String? = null,
    val security: ApiSecurityOverview? = null,
    val binding: ApiTotpBinding? = null,
    val code: String = "",
)

@HiltViewModel
class KycViewModel @Inject constructor(
    private val api: LinbitApi,
    @ApplicationContext private val context: Context,
    val toast: ToastCenter,
) : ViewModel() {
    private val _s = MutableStateFlow(KycUiState())
    val state: StateFlow<KycUiState> = _s.asStateFlow()
    internal var holdEvidence: Boolean = false

    /** 离屏证据：KYC 状态示例；默认非 APPROVED，不提交真实证件。 */
    internal fun seedEvidenceKyc(status: ApiKycStatus) {
        holdEvidence = true
        _s.value = KycUiState(loading = false, status = status)
    }

    fun load() {
        if (holdEvidence) return
        viewModelScope.launch {
            _s.value = _s.value.copy(loading = true, error = null)
            try { _s.value = _s.value.copy(loading = false, status = apiCall { api.kycStatus() }) }
            catch (e: Exception) { _s.value = _s.value.copy(loading = false, error = e.lbMessage()) }
        }
    }

    fun update(transform: (KycInput) -> KycInput) { _s.value = _s.value.copy(input = transform(_s.value.input), error = null) }

    fun upload(uri: Uri, side: ApiKycDocumentSide) {
        if (_s.value.submitting || _s.value.uploading != null) return
        viewModelScope.launch {
            _s.value = _s.value.copy(uploading = side, error = null)
            try {
                val jpeg = KycImage.jpeg(context, uri)
                val file = MultipartBody.Part.createFormData("file", "${side.wire}.jpg", jpeg.toRequestBody("image/jpeg".toMediaType()))
                val result = apiCall { api.uploadKycDocument(file, side.wire.toRequestBody("text/plain".toMediaType())) }
                val ids = _s.value.documentIds + (side to result.fileId)
                val input = _s.value.input.copy(uploaded = ids.keys)
                _s.value = _s.value.copy(uploading = null, documentIds = ids, input = input)
            } catch (e: Exception) { _s.value = _s.value.copy(uploading = null, error = e.lbMessage()) }
        }
    }

    fun submit(onDone: () -> Unit) {
        val current = _s.value
        val validation = ProfileLogic.kycError(current.input)
        if (validation != null) { _s.value = current.copy(error = validation); return }
        viewModelScope.launch {
            _s.value = _s.value.copy(submitting = true, error = null)
            try {
                val i = current.input
                val d = current.documentIds
                val body = ApiKycSubmission(
                    firstName = i.firstName.trim(), lastName = i.lastName.trim(), country = i.country,
                    idType = i.idType, idNumber = i.idNumber.trim(), dateOfBirth = i.dateOfBirth,
                    level = if (i.level == 2) ApiKycSubmission.Level._2 else ApiKycSubmission.Level._1,
                    documents = ApiKycSubmissionDocuments(
                        front = d.getValue(ApiKycDocumentSide.FRONT), selfie = d.getValue(ApiKycDocumentSide.SELFIE),
                        back = d[ApiKycDocumentSide.BACK], proofOfAddress = d[ApiKycDocumentSide.PROOF_OF_ADDRESS],
                    ),
                )
                val status = apiCall { api.submitKyc(body) }
                _s.value = _s.value.copy(submitting = false, status = status)
                toast.show("已提交，等待审核")
                onDone()
            } catch (e: Exception) { _s.value = _s.value.copy(submitting = false, error = e.lbMessage()) }
        }
    }
}

data class KycUiState(
    val loading: Boolean = false,
    val submitting: Boolean = false,
    val uploading: ApiKycDocumentSide? = null,
    val error: String? = null,
    val status: ApiKycStatus? = null,
    val input: KycInput = KycInput(),
    val documentIds: Map<ApiKycDocumentSide, String> = emptyMap(),
)

@HiltViewModel
class SubAccountsViewModel @Inject constructor(
    private val api: LinbitApi,
    val toast: ToastCenter,
) : ViewModel() {
    private val _s = MutableStateFlow(SubAccountsUiState())
    val state: StateFlow<SubAccountsUiState> = _s.asStateFlow()
    internal var holdEvidence: Boolean = false

    /** 离屏证据：子账户列表示例（非生产会话）。 */
    internal fun seedEvidenceSubAccounts(items: List<ApiSubAccount>) {
        holdEvidence = true
        _s.value = SubAccountsUiState(loading = false, items = items)
    }

    fun load() {
        if (holdEvidence) return
        viewModelScope.launch {
            _s.value = _s.value.copy(loading = true, error = null)
            try { _s.value = _s.value.copy(loading = false, items = apiCall { api.subAccounts() }.items.orEmpty()) }
            catch (e: Exception) { _s.value = _s.value.copy(loading = false, error = e.lbMessage()) }
        }
    }

    fun create(username: String, nickname: String, note: String, onDone: () -> Unit) {
        if (holdEvidence) return
        viewModelScope.launch {
            _s.value = _s.value.copy(submitting = true, error = null)
            try {
                apiCall { api.createSubAccount(ApiSubAccountCreateRequest(username, nickname.ifBlank { null }, note.ifBlank { null })) }
                _s.value = _s.value.copy(submitting = false)
                toast.show("子账户已创建")
                load(); onDone()
            } catch (e: Exception) { _s.value = _s.value.copy(submitting = false, error = e.lbMessage()) }
        }
    }
}

data class SubAccountsUiState(
    val loading: Boolean = false,
    val submitting: Boolean = false,
    val error: String? = null,
    val items: List<ApiSubAccount> = emptyList(),
)
