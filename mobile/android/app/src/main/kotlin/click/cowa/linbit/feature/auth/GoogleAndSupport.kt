package click.cowa.linbit.feature.auth

import android.app.Activity
import android.content.Intent
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.credentials.CredentialManager
import androidx.credentials.CustomCredential
import androidx.credentials.GetCredentialRequest
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.BuildConfig
import click.cowa.linbit.core.generated.ApiAuthTokens
import click.cowa.linbit.core.network.*
import com.google.android.libraries.identity.googleid.GetGoogleIdOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import javax.inject.Inject

/** POST auth/google 请求体：Google ID token + 设备信息；新用户由服务端注册并可能要求补充邀请码/条款确认。 */
@Serializable data class ApiGoogleLoginRequest(val idToken: String, val deviceId: String? = null, val deviceName: String? = null)
@Serializable data class ApiAuthProvider(val id: String, val configured: Boolean = false, val clientIds: List<String> = emptyList())
@Serializable data class ApiAuthProviders(val providers: List<ApiAuthProvider> = emptyList())
/** 游客在线客服公开配置：GET support/public-config。 */
@Serializable data class ApiSupportPublicConfig(val configured: Boolean = false, val available: Boolean = false, val mode: String = "unconfigured", val entryUrl: String? = null, val loginRequiredForTickets: Boolean = true)

@HiltViewModel
class GoogleSupportViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore) : ViewModel() {
    var googleLoading by mutableStateOf(false); private set
    var googleError by mutableStateOf<String?>(null); private set
    var supportLoading by mutableStateOf(false); private set
    var supportError by mutableStateOf<String?>(null); private set

    fun googleLogin(activity: Activity, onDone: () -> Unit) {
        if (googleLoading || tokens.loggedIn.value) return
        googleLoading = true; googleError = null
        viewModelScope.launch {
            try {
                // 优先服务端 /auth/providers 下发的 clientIds，其次构建期 GOOGLE_WEB_CLIENT_ID
                val provider = runCatching { apiCall { api.authProviders() } }.getOrNull()?.providers?.firstOrNull { it.id == "google" }
                val clientId = provider?.clientIds?.firstOrNull()?.takeIf { provider.configured } ?: BuildConfig.GOOGLE_WEB_CLIENT_ID.takeIf { it.isNotBlank() }
                if (clientId == null) { googleError = "Google 登录未配置，请使用邮箱、手机号或通行密钥登录"; return@launch }
                val option = GetGoogleIdOption.Builder().setServerClientId(clientId).setFilterByAuthorizedAccounts(false).setAutoSelectEnabled(false).build()
                val cred = CredentialManager.create(activity).getCredential(activity, GetCredentialRequest.Builder().addCredentialOption(option).build()).credential
                if (cred !is CustomCredential || cred.type != GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_CREDENTIAL) throw IllegalStateException("未取得 Google 凭据")
                val idToken = GoogleIdTokenCredential.createFrom(cred.data).idToken
                val t: ApiAuthTokens = apiCall { api.googleLogin(ApiGoogleLoginRequest(idToken, android.provider.Settings.Secure.getString(activity.contentResolver, android.provider.Settings.Secure.ANDROID_ID), android.os.Build.MODEL)) }
                tokens.save(t); onDone()
            } catch (e: CancellationException) { throw e
            } catch (_: GetCredentialCancellationException) { googleError = null
            } catch (e: ApiFailure.Server) { googleError = when (e.error?.code) { "GOOGLE_UNCONFIGURED" -> "Google 登录未配置，请使用邮箱、手机号或通行密钥登录"; "GOOGLE_UNAVAILABLE" -> "Google 验证服务暂时不可用，请稍后重试"; else -> e.lbMessage() }
            } catch (e: Throwable) { googleError = (e as? ApiFailure)?.lbMessage() ?: e.message ?: "Google 登录失败" }
            finally { googleLoading = false }
        }
    }

    fun openSupport(activity: Activity) {
        if (supportLoading) return
        supportLoading = true; supportError = null
        viewModelScope.launch {
            runCatching { apiCall { api.supportPublicConfig() } }
                .onSuccess { c -> val url = c.entryUrl.takeIf { c.available && c.mode == "external" }
                    if (url.isNullOrBlank()) supportError = "在线客服暂未开通，登录后可提交客服工单"
                    else runCatching { activity.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) }.onFailure { supportError = "无法打开客服页面" } }
                .onFailure { supportError = if (it is ApiFailure.Server && it.status in setOf(401, 403, 404, 501)) "在线客服暂未对游客开放，请登录后提交客服工单" else it.lbMessage() }
            supportLoading = false
        }
    }
    fun clearSupportError() { supportError = null }
    fun clearGoogleError() { googleError = null }
}
