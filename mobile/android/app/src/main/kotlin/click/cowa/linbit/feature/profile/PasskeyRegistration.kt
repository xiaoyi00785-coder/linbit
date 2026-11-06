package click.cowa.linbit.feature.profile

import android.app.Activity
import androidx.credentials.CredentialManager
import androidx.credentials.CreatePublicKeyCredentialRequest
import androidx.credentials.CreatePublicKeyCredentialResponse
import androidx.credentials.exceptions.CreateCredentialCancellationException
import androidx.credentials.exceptions.CreateCredentialCustomException
import androidx.credentials.exceptions.CreateCredentialInterruptedException
import androidx.credentials.exceptions.CreateCredentialProviderConfigurationException
import androidx.credentials.exceptions.CreateCredentialUnsupportedException
import click.cowa.linbit.core.generated.ApiBeginPasskeyRegistrationRequest
import click.cowa.linbit.core.generated.ApiFinishPasskeyRegistrationRequest
import click.cowa.linbit.core.generated.ApiPasskey
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.feature.auth.PasskeyLoginFailure
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject

private fun PasskeyLoginFailure.registrationText() = when (this) {
    PasskeyLoginFailure.Cancelled -> Exception("已取消通行密钥注册，可重新验证后重试")
    PasskeyLoginFailure.Unsupported -> Exception("当前设备或凭据服务不支持 Android 通行密钥；请检查系统支持后重试")
    PasskeyLoginFailure.NoCredential -> Exception("当前设备没有可用于注册的凭据提供程序，请稍后重试")
    PasskeyLoginFailure.InvalidResponse -> Exception("通行密钥注册响应无效，请重试")
    PasskeyLoginFailure.Retry -> Exception("系统凭据服务暂不可用，请重试")
}.message

internal fun PasskeyLoginFailure.registrationError() = PasskeyRegistrationFailure(registrationText())
internal class PasskeyRegistrationFailure(message: String?) : Exception(message)

/** Tests explicitly replace this bridge; production always calls the Android system public API. */
fun interface PasskeyRegistrationBridge {
    suspend fun create(activity: Activity, options: JsonObject): JsonObject
}

class AndroidPasskeyRegistrationBridge : PasskeyRegistrationBridge {
    override suspend fun create(activity: Activity, options: JsonObject): JsonObject {
        if (activity.isFinishing || activity.isDestroyed) throw PasskeyLoginFailure.Cancelled
        val result = try {
            CredentialManager.create(activity).createCredential(activity, CreatePublicKeyCredentialRequest(options.toString()))
        } catch (e: CreateCredentialCancellationException) { throw PasskeyLoginFailure.Cancelled
        } catch (e: CreateCredentialProviderConfigurationException) { throw PasskeyLoginFailure.Unsupported
        } catch (e: CreateCredentialUnsupportedException) { throw PasskeyLoginFailure.Unsupported
        } catch (e: CreateCredentialInterruptedException) { throw PasskeyLoginFailure.Retry
        } catch (e: CreateCredentialCustomException) { throw PasskeyLoginFailure.Retry }
        val response = (result as? CreatePublicKeyCredentialResponse)?.registrationResponseJson ?: throw PasskeyLoginFailure.InvalidResponse
        return registrationResponse(response)
    }
}

fun registrationResponse(json: String): JsonObject = runCatching {
    val credential = kotlinx.serialization.json.Json.parseToJsonElement(json).jsonObject
    val response = credential["response"] as? JsonObject ?: error("response must be an object")
    fun present(key: String, holder: JsonObject = credential) =
        (holder[key] as? JsonPrimitive)?.takeIf { it.isString && it.content.isNotEmpty() } != null
    require(present("id") && present("rawId") && credential["type"] == JsonPrimitive("public-key"))
    require(present("clientDataJSON", response) && present("attestationObject", response))
    credential
}.getOrElse { throw PasskeyLoginFailure.InvalidResponse }

class PasskeyRegistrationService(private val api: LinbitApi, private val bridge: PasskeyRegistrationBridge) {
    suspend fun register(activity: Activity, code: String, enabledTotp: Boolean, name: String): ApiPasskey = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
        val ceremony = apiCall { api.passkeyRegisterBegin(ApiBeginPasskeyRegistrationRequest(totpCode = code.takeIf { enabledTotp }, code = code.takeUnless { enabledTotp })) }
        val sessionId = ceremony.sessionId?.takeIf { it.isNotBlank() } ?: throw PasskeyLoginFailure.InvalidResponse
        val options = ceremony.publicKey?.takeIf { it.isNotEmpty() } ?: throw PasskeyLoginFailure.InvalidResponse
        val credential = bridge.create(activity, options)
        if (activity.isDestroyed || activity.isFinishing) throw PasskeyLoginFailure.Cancelled
        apiCall { api.passkeyRegisterFinish(ApiFinishPasskeyRegistrationRequest(sessionId, credential, name)) }
    }
}
