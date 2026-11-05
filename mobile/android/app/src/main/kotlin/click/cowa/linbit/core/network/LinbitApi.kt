package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiAuthTokens
import click.cowa.linbit.core.generated.ApiAssetsOverview
import click.cowa.linbit.core.generated.ApiEarnProducts200Response
import click.cowa.linbit.core.generated.ApiFiatRates
import click.cowa.linbit.core.generated.ApiHomeNewcomer
import click.cowa.linbit.core.generated.ApiLoginRequest
import click.cowa.linbit.core.generated.ApiLoginResendCodeRequest
import click.cowa.linbit.core.generated.ApiLoginVerifyRequest
import click.cowa.linbit.core.generated.ApiLogoutRequest
import click.cowa.linbit.core.generated.ApiMarketCategory
import click.cowa.linbit.core.generated.ApiPasswordResetRequest
import click.cowa.linbit.core.generated.ApiRegisterRequest
import click.cowa.linbit.core.generated.ApiSendCodeRequest
import click.cowa.linbit.core.generated.ApiSendCodeResponse
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.generated.ApiVerifyResetCodeRequest
import click.cowa.linbit.core.generated.ApiVerifyResetCodeResponse
import kotlinx.serialization.json.JsonElement
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.Headers
import retrofit2.http.POST
import retrofit2.http.Query

/**
 * Retrofit 接口（本期只含 Markets + 登录所需端点；其余模块按 mobile/openapi.yaml 逐步补）。
 * 路径不带前导 "/"，基地址为 https://api.cowa.click/ 。
 * oneOf 响应（login / ticker / futures ticker）以 JsonElement 返回，由 [OneOfResponses] 判别解码。
 */
interface LinbitApi {
    // ---- Auth（公开）----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/login")
    suspend fun login(@Body body: ApiLoginRequest): JsonElement

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/login/verify")
    suspend fun loginVerify(@Body body: ApiLoginVerifyRequest): ApiAuthTokens

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/login/code")
    suspend fun loginResendCode(@Body body: ApiLoginResendCodeRequest)

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/code/send")
    suspend fun sendCode(@Body body: ApiSendCodeRequest): ApiSendCodeResponse

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/register")
    suspend fun register(@Body body: ApiRegisterRequest): ApiAuthTokens

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/password/verify-code")
    suspend fun verifyResetCode(@Body body: ApiVerifyResetCodeRequest): ApiVerifyResetCodeResponse

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @POST("auth/password/reset")
    suspend fun resetPassword(@Body body: ApiPasswordResetRequest)

    /** 退出：请求体始终带 refreshToken（访问令牌过期时服务端据此识别用户）；不走 401 自动刷新。 */
    @Headers("${ApiHeaders.NO_REFRESH}: 1")
    @POST("auth/logout")
    suspend fun logout(@Body body: ApiLogoutRequest)

    // ---- Market（公开）----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("market/symbols")
    suspend fun symbols(): List<ApiSymbol>

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("market/categories")
    suspend fun categories(@Query("market") market: String? = null): List<ApiMarketCategory>

    /** 不传 symbol：返回数组；传 symbol：返回单个对象（oneOf） */
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("market/ticker")
    suspend fun ticker(@Query("symbol") symbol: String? = null): JsonElement

    /** 不传 symbol：返回 {tickers:[...]}；传 symbol：返回单个 FuturesTicker（oneOf） */
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/market/ticker")
    suspend fun futuresTicker(@Query("symbol") symbol: String? = null): JsonElement

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("market/fiat-rates")
    suspend fun fiatRates(@Query("base") base: String = "USDT"): ApiFiatRates

    // ---- Home / Assets ----
    /** 可匿名；带合法 Bearer 时返回个人进度（所以不加 NO_AUTH）。 */
    @GET("home/newcomer")
    suspend fun homeNewcomer(): ApiHomeNewcomer

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("earn/products")
    suspend fun earnProducts(): ApiEarnProducts200Response

    @GET("assets/overview")
    suspend fun assetsOverview(): ApiAssetsOverview
}
