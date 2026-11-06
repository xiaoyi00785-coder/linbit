package click.cowa.linbit.core.network

import com.jakewharton.retrofit2.converter.kotlinx.serialization.asConverterFactory
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import java.util.concurrent.TimeUnit

/** 构建 OkHttp / Retrofit（Hilt 模块与单元测试共用）。 */
object NetworkFactory {
    val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        coerceInputValues = true
    }

    fun bareClient(): OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(15, TimeUnit.SECONDS)
        .build()

    fun client(
        tokens: TokenStore,
        baseUrl: String,
        userAgent: String,
        deviceId: () -> String,
        onSessionExpired: () -> Unit = {},
    ): OkHttpClient {
        val bare = bareClient()
        return bare.newBuilder()
            .addInterceptor(AuthInterceptor(tokens, userAgent, deviceId))
            .authenticator(TokenAuthenticator(tokens, baseUrl, userAgent, bare, onSessionExpired))
            .build()
    }

    fun api(client: OkHttpClient, baseUrl: String): LinbitApi = Retrofit.Builder()
        .baseUrl(baseUrl)
        .client(client)
        .addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
        .build()
        .create(LinbitApi::class.java)

    fun paperApi(client: OkHttpClient, baseUrl: String): click.cowa.linbit.data.paper.PaperTradingApi = Retrofit.Builder()
        .baseUrl(baseUrl).client(client).addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
        .build().create(click.cowa.linbit.data.paper.PaperTradingApi::class.java)
}
