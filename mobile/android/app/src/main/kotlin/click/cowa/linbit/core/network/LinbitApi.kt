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
import click.cowa.linbit.core.generated.*
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.PUT
import retrofit2.http.Path
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

    // ---- 现货交易 ----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("market/depth")
    suspend fun depth(@Query("symbol") symbol: String, @Query("limit") limit: Int = 20): ApiDepth

    @GET("assets/balances")
    suspend fun balances(@Query("account") account: String? = null, @Query("pageSize") pageSize: Int = 200): ApiPaginatedBalances

    @POST("spot/orders")
    suspend fun placeSpotOrder(@Body body: ApiPlaceOrderRequest): ApiOrder

    @DELETE("spot/orders/{orderId}")
    suspend fun cancelSpotOrder(@Path("orderId") orderId: String): ApiOrder

    @GET("spot/orders/open")
    suspend fun spotOpenOrders(@Query("symbol") symbol: String? = null, @Query("pageSize") pageSize: Int = 100): ApiPaginatedOrders

    @GET("spot/orders/history")
    suspend fun spotOrderHistory(
        @Query("symbol") symbol: String? = null, @Query("status") status: String? = null,
        @Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20,
    ): ApiPaginatedOrders

    @GET("spot/trades")
    suspend fun spotTrades(@Query("symbol") symbol: String? = null, @Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiPaginatedTrades

    @POST("spot/orders/conditional")
    suspend fun createConditional(@Body body: ApiConditionalOrderRequest): ApiConditionalOrder

    @GET("spot/orders/conditional")
    suspend fun conditionalOrders(@Query("status") status: String? = null, @Query("pageSize") pageSize: Int = 100): ApiListConditionalOrders200Response

    @DELETE("spot/orders/conditional/{id}")
    suspend fun cancelConditional(@Path("id") id: String): ApiConditionalOrder

    @GET("account/fee-rates")
    suspend fun feeRates(@Query("symbol") symbol: String? = null): ApiFeeRates

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("spot/fee-schedule")
    suspend fun feeSchedule(): ApiGetFeeSchedule200Response

    // ---- U 本位合约 ----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/market/contracts")
    suspend fun futuresContracts(): ApiFuturesContractList

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/market/depth")
    suspend fun futuresDepth(@Query("symbol") symbol: String, @Query("limit") limit: Int = 20): ApiFuturesDepth

    @GET("futures/account")
    suspend fun futuresAccount(): ApiFuturesAccount

    @GET("futures/leverage")
    suspend fun futuresLeverage(@Query("symbol") symbol: String): ApiFuturesGetLeverage200Response

    @POST("futures/leverage")
    suspend fun setFuturesLeverage(@Body body: ApiFuturesSetLeverageRequest): ApiFuturesGetLeverage200Response

    @GET("futures/position-mode")
    suspend fun futuresPositionMode(): ApiFuturesPositionMode

    @POST("futures/position-mode")
    suspend fun setFuturesPositionMode(@Body body: ApiFuturesPositionMode): ApiFuturesPositionMode

    @POST("futures/orders")
    suspend fun placeFuturesOrder(@Body body: ApiFuturesPlaceOrderRequest): ApiFuturesPlaceOrder200Response

    @GET("futures/orders")
    suspend fun futuresOrders(
        @Query("symbol") symbol: String? = null, @Query("status") status: String? = null, @Query("limit") limit: Int = 100,
    ): ApiFuturesOrders200Response

    @DELETE("futures/orders/{id}")
    suspend fun cancelFuturesOrder(@Path("id") id: Long): ApiFuturesPlaceOrder200Response

    @GET("futures/positions")
    suspend fun futuresPositions(): ApiFuturesPositions200Response

    @POST("futures/positions/{symbol}/close")
    suspend fun closeFuturesPosition(@Path("symbol") symbol: String): ApiFuturesPlaceOrder200Response

    /** 分仓（HEDGE）模式平仓需在请求体带 positionSide */
    @POST("futures/positions/{symbol}/close")
    suspend fun closeFuturesPositionSide(@Path("symbol") symbol: String, @Body body: JsonElement): ApiFuturesPlaceOrder200Response

    @POST("futures/plan-orders")
    suspend fun createFuturesPlan(@Body body: ApiFuturesPlaceOrderRequest): ApiFuturesCreateTpslOrders200Response

    @GET("futures/plan-orders")
    suspend fun futuresPlanOrders(@Query("status") status: String = "active", @Query("limit") limit: Int = 50): ApiFuturesListTpslOrders200Response

    @GET("futures/tpsl-orders")
    suspend fun futuresTpslOrders(@Query("status") status: String = "active", @Query("limit") limit: Int = 50): ApiFuturesListTpslOrders200Response

    @DELETE("futures/plan-orders/{id}")
    suspend fun cancelFuturesPlan(@Path("id") id: Long): ApiFuturesAmendTpslOrder200Response

    @DELETE("futures/tpsl-orders/{id}")
    suspend fun cancelFuturesTpsl(@Path("id") id: Long): ApiFuturesAmendTpslOrder200Response

    @GET("futures/market/risk-tiers")
    suspend fun futuresRiskTiers(@Query("symbol") symbol: String? = null): ApiFuturesRiskTiers200Response

    @GET("futures/pnl/today")
    suspend fun futuresTodayPnl(): ApiFuturesTodayPnl200Response

    @GET("futures/bonus")
    suspend fun futuresBonus(): ApiFuturesBonus200Response

    @GET("futures/positions/history")
    suspend fun futuresPositionHistory(@Query("symbol") symbol: String? = null, @Query("limit") limit: Int = 50): ApiFuturesPositionHistory200Response

    @GET("futures/trades")
    suspend fun futuresMyTrades(@Query("symbol") symbol: String? = null, @Query("limit") limit: Int = 100): ApiFuturesMyTrades200Response

    @GET("futures/bills")
    suspend fun futuresBills(@Query("kind") kind: String? = null, @Query("limit") limit: Int = 100): ApiFuturesBills200Response

    @POST("futures/transfer")
    suspend fun futuresTransfer(@Body body: ApiFuturesTransferRequest): ApiFuturesTransfer200Response

    @GET("futures/transfers")
    suspend fun futuresTransfers(): ApiFuturesTransfers200Response

    // ---- 自选 ----
    @GET("account/favorites")
    suspend fun favorites(@Query("market") market: String = "spot"): JsonElement
}
