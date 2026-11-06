package click.cowa.linbit.feature.auth

import android.app.Activity
import androidx.credentials.CredentialManager
import androidx.credentials.GetCredentialRequest
import androidx.credentials.PublicKeyCredential
import androidx.credentials.GetPublicKeyCredentialOption
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.GetCredentialCustomException
import androidx.credentials.exceptions.GetCredentialInterruptedException
import androidx.credentials.exceptions.GetCredentialProviderConfigurationException
import androidx.credentials.exceptions.GetCredentialUnsupportedException
import androidx.credentials.exceptions.NoCredentialException
import click.cowa.linbit.core.generated.ApiFinishPasskeyLoginRequest
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject

/** Tests use an explicit fake; production always invokes the public Android system API. */
fun interface PasskeyCredentialBridge {
    suspend fun authenticate(activity: Activity, options: JsonObject): JsonObject
}

class AndroidPasskeyCredentialBridge : PasskeyCredentialBridge {
    override suspend fun authenticate(activity: Activity, options: JsonObject): JsonObject {
        if (activity.isFinishing || activity.isDestroyed) throw PasskeyLoginFailure.Cancelled
        val request = GetCredentialRequest.Builder()
            .addCredentialOption(GetPublicKeyCredentialOption(requestJson = options.toString()))
            .build()
        val result = try {
            CredentialManager.create(activity).getCredential(context = activity, request = request)
        } catch (e: GetCredentialCancellationException) { throw PasskeyLoginFailure.Cancelled
        } catch (e: NoCredentialException) { throw PasskeyLoginFailure.NoCredential
        } catch (e: GetCredentialProviderConfigurationException) { throw PasskeyLoginFailure.Unsupported
        } catch (e: GetCredentialUnsupportedException) { throw PasskeyLoginFailure.Unsupported
        } catch (e: GetCredentialInterruptedException) { throw PasskeyLoginFailure.Retry
        } catch (e: GetCredentialCustomException) { throw PasskeyLoginFailure.Retry }
        val publicKey = result.credential as? PublicKeyCredential ?: throw PasskeyLoginFailure.Unsupported
        return credentialResponse(publicKey.authenticationResponseJson)
    }
}

fun credentialResponse(json: String): JsonObject = runCatching {
    val credential = kotlinx.serialization.json.Json.parseToJsonElement(json).jsonObject
    val response = credential["response"] as? JsonObject ?: error("response must be an object")
    require((credential["id"] as? kotlinx.serialization.json.JsonPrimitive)?.isString == true && (credential["rawId"] as? kotlinx.serialization.json.JsonPrimitive)?.isString == true)
    require(credential["type"] == kotlinx.serialization.json.JsonPrimitive("public-key"))
    fun nonempty(key: String, holder: JsonObject = credential) =
        (holder[key] as? kotlinx.serialization.json.JsonPrimitive)?.takeIf { it.isString && it.content.isNotEmpty() } != null
    require(nonempty("id") && nonempty("rawId") && nonempty("clientDataJSON", response) && nonempty("authenticatorData", response) && nonempty("signature", response))
    credential
}.getOrElse { throw PasskeyLoginFailure.InvalidResponse }

sealed class PasskeyLoginFailure(message: String) : Exception(message) {
    data object Cancelled : PasskeyLoginFailure("已取消通行密钥登录，您可以重试")
    data object NoCredential : PasskeyLoginFailure("未找到可用通行密钥。RP/origin须与本APK的assetlinks签名关联一致；请检查部署后重试或改用密码登录")
    data object Unsupported : PasskeyLoginFailure("当前设备或凭据服务不支持Android通行密钥；请用密码登录")
    data object InvalidResponse : PasskeyLoginFailure("通行密钥响应无效，请重试")
    data object Retry : PasskeyLoginFailure("系统凭据服务暂不可用，请重试")
}

class PasskeyLoginService(private val api: LinbitApi, private val bridge: PasskeyCredentialBridge, private val deviceId: () -> String, private val deviceName: () -> String) {
    /** Does not persist a token: the owning AuthViewModel checks its session/generation first. */
    suspend fun login(activity: Activity): click.cowa.linbit.core.generated.ApiAuthTokens = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
        val ceremony = apiCall { api.passkeyLoginBegin() }
        val session = requireNotNull(ceremony.sessionId).takeIf { it.isNotBlank() } ?: throw PasskeyLoginFailure.InvalidResponse
        val options = requireNotNull(ceremony.publicKey).takeIf { it.isNotEmpty() } ?: throw PasskeyLoginFailure.InvalidResponse
        val assertion = bridge.authenticate(activity, options)
        apiCall { api.passkeyLoginFinish(ApiFinishPasskeyLoginRequest(sessionId = session, credential = assertion, deviceId = deviceId(), deviceName = deviceName())) }
    }
}
