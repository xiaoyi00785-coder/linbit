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
import click.cowa.linbit.core.generated.ApiSendCodeAuthedRequest
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
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.Query
import retrofit2.http.Multipart
import retrofit2.http.Part
import okhttp3.MultipartBody

/**
 * Retrofit 接口：认证、行情、首页、理财、资产总览/划转/充提/地址簿、现货与合约交易、通知、卡券、跟单、合伙人、TradFi 与客服工单等端点（按 mobile/openapi.yaml 逐步补齐）。
 * 路径不带前导 "/"，基地址为 https://api.cowa.click/ 。
 * oneOf 响应（login / ticker / futures ticker）以 JsonElement 返回，由 [OneOfResponses] 判别解码。
 */
interface LinbitApi {

    /** Exact existing /account/contact/code and /account/email|phone contract. */
    @POST("account/contact/code")
    suspend fun sendContactCode(@Body body: ApiSendContactCodeRequest): ApiSendContactCode200Response

    @PUT("account/email")
    suspend fun changeEmail(@Body body: ApiChangeContactRequest): ApiOkResponse

    @PUT("account/phone")
    suspend fun changePhone(@Body body: ApiChangeContactRequest): ApiOkResponse

    @POST("account/fund-password")
    suspend fun setFundPassword(@Body body: ApiFundPasswordRequest): ApiOkResponse

    @PUT("account/fund-password")
    suspend fun changeFundPassword(@Body body: ApiFundPasswordRequest): ApiOkResponse
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

    @GET("rewards/newcomer")
    suspend fun newcomerState(): ApiNewcomerState

    @POST("rewards/newcomer/enroll")
    suspend fun enrollNewcomer(): ApiNewcomerState

    @POST("rewards/newcomer/tasks/{key}/claim")
    suspend fun claimNewcomerTask(@Path("key") key: String): ApiNewcomerClaimResult

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
    suspend fun futuresPlanOrders(
        @Query("symbol") symbol: String? = null,
        @Query("status") status: String = "active",
        @Query("cursor") cursor: Long? = null,
        @Query("limit") limit: Int = 50,
    ): ApiFuturesListTpslOrders200Response

    @GET("futures/tpsl-orders")
    suspend fun futuresTpslOrders(
        @Query("symbol") symbol: String? = null,
        @Query("status") status: String = "active",
        @Query("cursor") cursor: Long? = null,
        @Query("limit") limit: Int = 50,
    ): ApiFuturesListTpslOrders200Response

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
    suspend fun futuresPositionHistory(
        @Query("symbol") symbol: String? = null,
        @Query("startTime") startTime: Long? = null,
        @Query("endTime") endTime: Long? = null,
        @Query("cursor") cursor: Long? = null,
        @Query("limit") limit: Int = 50,
    ): ApiFuturesPositionHistory200Response

    @GET("futures/trades")
    suspend fun futuresMyTrades(@Query("symbol") symbol: String? = null, @Query("limit") limit: Int = 100): ApiFuturesMyTrades200Response

    @GET("futures/bills")
    suspend fun futuresBills(@Query("kind") kind: String? = null, @Query("limit") limit: Int = 100): ApiFuturesBills200Response

    @POST("futures/transfer")
    suspend fun futuresTransfer(@Body body: ApiFuturesTransferRequest): ApiFuturesTransfer200Response

    @GET("futures/transfers")
    suspend fun futuresTransfers(): ApiFuturesTransfers200Response

    // ---- K 线 / 成交 ----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("market/klines")
    suspend fun spotKlines(
        @Query("symbol") symbol: String,
        @Query("interval") interval: String,
        @Query("limit") limit: Int = 160,
    ): List<ApiKline>

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/market/klines")
    suspend fun futuresKlines(
        @Query("symbol") symbol: String,
        @Query("interval") interval: String,
        @Query("limit") limit: Int = 120,
    ): ApiFuturesKlines

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/market/trades")
    suspend fun futuresMarketTrades(@Query("symbol") symbol: String, @Query("limit") limit: Int = 50): ApiFuturesMarketTrades200Response

    // ---- 合约改单 / 仓位止盈止损 / 调整保证金 ----
    @PUT("futures/orders/{id}")
    suspend fun amendFuturesOrder(@Path("id") id: Long, @Body body: ApiFuturesAmendOrderRequest): ApiFuturesPlaceOrder200Response

    @POST("futures/tpsl-orders")
    suspend fun createFuturesTpsl(@Body body: ApiFuturesTpslOrderRequest): ApiFuturesCreateTpslOrders200Response

    @PUT("futures/tpsl-orders/{id}")
    suspend fun amendFuturesTpsl(@Path("id") id: Long, @Body body: ApiFuturesAlgoAmendRequest): ApiFuturesAmendTpslOrder200Response

    @PUT("futures/plan-orders/{id}")
    suspend fun amendFuturesPlan(@Path("id") id: Long, @Body body: ApiFuturesAlgoAmendRequest): ApiFuturesAmendTpslOrder200Response

    @POST("futures/positions/{symbol}/tpsl")
    suspend fun setFuturesPositionTpsl(
        @Path("symbol") symbol: String,
        @Query("positionSide") positionSide: String? = null,
        @Body body: ApiFuturesSetTpslRequest,
    ): ApiFuturesPositions200Response

    @POST("futures/positions/{symbol}/margin")
    suspend fun adjustFuturesMargin(
        @Path("symbol") symbol: String,
        @Query("positionSide") positionSide: String? = null,
        @Body body: ApiFuturesAdjustMarginRequest,
    ): ApiFuturesPositions200Response

    // ---- 闪兑 / 策略 / 理财扩展 ----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("convert/pairs")
    suspend fun convertPairs(): ApiConvertPairs

    @POST("convert/quote")
    suspend fun convertQuote(@Body body: ApiConvertQuoteRequest): ApiConvertQuote

    @POST("convert/accept")
    suspend fun acceptConvertQuote(@Body body: ApiAcceptConvertQuoteRequest): ApiAcceptConvertQuote200Response

    @GET("convert/orders")
    suspend fun convertOrders(@Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiListConvertOrders200Response

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("strategy/config")
    suspend fun strategyConfig(): ApiStrategyConfig200Response

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("strategy/grid/square")
    suspend fun strategySquare(@Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiStrategySquare200Response

    @GET("strategy/grid")
    suspend fun strategyGrids(@Query("status") status: String? = null): ApiStrategyListGrids200Response

    @GET("earn/summary")
    suspend fun earnSummary(): ApiEarnSummary200Response

    @GET("earn/positions")
    suspend fun earnPositions(): ApiEarnPositions200Response

    @POST("earn/products/{id}/subscribe")
    suspend fun earnSubscribe(@Path("id") id: String, @Body body: ApiEarnSubscribeRequest): ApiEarnSubscribe200Response

    @POST("earn/products/{id}/redeem")
    suspend fun earnRedeem(@Path("id") id: String, @Body body: ApiEarnRedeemRequest): ApiEarnRedeem200Response

    @POST("strategy/grid/square/{id}/copy")
    suspend fun strategyCopy(@Path("id") id: String, @Body body: ApiStrategyCopyRequest): ApiGridBot

    @POST("strategy/grid/{id}/stop")
    suspend fun strategyStopGrid(
        @Path("id") id: String,
        @Body body: ApiStrategyStopGridRequest = ApiStrategyStopGridRequest(),
    ): ApiGridBot

    @GET("earn/orders")
    suspend fun earnOrders(@Query("status") status: String? = null): ApiEarnOrders200Response

    @GET("earn/records")
    suspend fun earnRecords(@Query("type") type: String? = null): ApiEarnRecords200Response

    @POST("earn/products/{id}/auto")
    suspend fun earnAuto(@Path("id") id: String, @Body body: ApiEarnAutoRequest): ApiEarnAuto200Response

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("strategy/grid/preview")
    suspend fun strategyPreview(
        @Query("symbol") symbol: String,
        @Query("mode") mode: String,
        @Query("lowerPrice") lowerPrice: String,
        @Query("upperPrice") upperPrice: String,
        @Query("gridCount") gridCount: Int,
        @Query("investment") investment: String,
        @Query("stopLossPrice") stopLossPrice: String? = null,
        @Query("takeProfitPrice") takeProfitPrice: String? = null,
    ): ApiStrategyPreview200Response

    @POST("strategy/grid")
    suspend fun strategyCreateGrid(@Body body: ApiGridCreateRequest): ApiGridBot

    @GET("strategy/grid/{id}/orders")
    suspend fun strategyGridOrders(
        @Path("id") id: String,
        @Query("status") status: String? = null,
    ): ApiStrategyGridOrders200Response

    // ---- 自选 ----
    @GET("account/favorites")
    suspend fun favorites(@Query("market") market: String = "spot"): JsonElement

    // ---- Assets / Wallet（充提、划转、地址簿）----
    @GET("wallet/networks")
    suspend fun walletNetworks(@Query("asset") asset: String? = null): ApiListWalletNetworks200Response

    @GET("wallet/withdraw/networks")
    suspend fun withdrawNetworks(@Query("asset") asset: String): ApiWithdrawNetworkOptions

    @GET("wallet/deposit/address")
    suspend fun depositAddress(@Query("asset") asset: String, @Query("network") network: String): ApiDepositAddress

    @GET("wallet/deposit/records")
    suspend fun depositRecords(@Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiPaginatedDeposits

    @POST("wallet/withdraw")
    suspend fun withdraw(@Body body: ApiWithdrawRequest): ApiWithdrawal

    @GET("wallet/withdraw/records")
    suspend fun withdrawRecords(@Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiPaginatedWithdrawals

    @POST("wallet/withdraw/{id}/cancel")
    suspend fun cancelWithdraw(@Path("id") id: String): ApiWithdrawal

    @POST("wallet/internal-transfer")
    suspend fun internalTransfer(@Body body: ApiInternalTransferRequest): ApiInternalTransfer

    @GET("wallet/internal-transfers")
    suspend fun internalTransfers(@Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiListInternalTransfers200Response

    @POST("assets/transfer")
    suspend fun assetsTransfer(@Body body: ApiTransferRequest): ApiTransfer

    @GET("assets/transfers")
    suspend fun assetsTransfers(@Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): ApiListAssetTransfers200Response

    @GET("account/withdraw-addresses")
    suspend fun withdrawAddresses(): ApiWithdrawAddressList

    @POST("account/withdraw-addresses")
    suspend fun createWithdrawAddress(@Body body: ApiCreateWithdrawAddressRequest): ApiWithdrawAddress

    @PATCH("account/withdraw-addresses/{id}")
    suspend fun updateWithdrawAddress(@Path("id") id: String, @Body body: ApiUpdateWithdrawAddressRequest): ApiWithdrawAddress

    @GET("account/withdraw-whitelist")
    suspend fun withdrawWhitelist(): ApiWhitelistStatus

    @PUT("account/withdraw-whitelist")
    suspend fun setWithdrawWhitelist(@Body body: ApiSetWhitelistRequest): ApiWhitelistStatus

    // ---- 个人中心 / 安全 / KYC / 子账户 ----
    @POST("account/code/send")
    suspend fun sendAccountCode(@Body body: ApiSendCodeAuthedRequest): ApiSendCodeResponse

    @GET("account")
    suspend fun account(): ApiAccount

    @PATCH("account/profile")
    suspend fun updateProfile(@Body body: ApiUpdateProfileRequest): ApiAccount

    @GET("account/security")
    suspend fun securityOverview(): ApiSecurityOverview

    @POST("account/password/change")
    suspend fun changePassword(@Body body: click.cowa.linbit.feature.profile.SecurityChangePasswordRequest): ApiChangePassword200Response

    @PUT("account/anti-phishing")
    suspend fun setAntiPhishing(@Body body: ApiSetAntiPhishingRequest): ApiOkResponse

    @POST("2fa/totp/bind")
    suspend fun bindTotp(): ApiTotpBinding

    @POST("2fa/totp/verify")
    suspend fun verifyTotp(@Body body: ApiVerifyTotpRequest): ApiTwoFactorStatus

    @POST("2fa/totp/disable")
    suspend fun disableTotp(@Body body: ApiDisableTotpRequest): ApiTwoFactorStatus

    @GET("kyc/status")
    suspend fun kycStatus(): ApiKycStatus

    @Multipart
    @POST("kyc/documents")
    suspend fun uploadKycDocument(
        @Part file: MultipartBody.Part,
        @Part("side") side: okhttp3.RequestBody,
    ): ApiKycDocument

    @POST("kyc")
    suspend fun submitKyc(@Body body: ApiKycSubmission): ApiKycStatus

    @GET("sub-accounts")
    suspend fun subAccounts(
        @Query("page") page: Int = 1,
        @Query("size") size: Int = 100,
    ): ApiSubAccountList200Response

    @POST("sub-accounts")
    suspend fun createSubAccount(@Body body: ApiSubAccountCreateRequest): ApiSubAccount


    // ---- 福利 / 通知 / 卡券 ----
    @GET("notifications")
    suspend fun notifications(
        @Query("kind") kind: String? = null,
        @Query("page") page: Int = 1,
        @Query("pageSize") pageSize: Int = 20,
    ): ApiNotificationList

    @GET("notifications/unread-count")
    suspend fun unreadCount(): ApiUnreadCount

    @POST("notifications/read")
    suspend fun readNotifications(@Body body: ApiReadNotificationsRequest): ApiUpdatedCount

    @GET("notifications/settings")
    suspend fun notificationSettings(): ApiNotifyPrefs

    @PUT("notifications/settings")
    suspend fun updateNotificationSettings(@Body body: ApiNotifyPrefsPatch): ApiNotifyPrefs

    @GET("coupons")
    suspend fun coupons(@Query("status") status: String): ApiCouponList

    @GET("coupons/summary")
    suspend fun couponSummary(): ApiCouponSummary

    @POST("coupons/{id}/activate")
    suspend fun activateCoupon(@Path("id") id: Long): ApiCouponActivateResult

    // ---- 跟单（公开榜单 + 登录后操作）----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/copy/summary")
    suspend fun copySummary(): JsonElement

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/copy/traders")
    suspend fun copyTraders(
        @Query("sort") sort: String = "composite",
        @Query("days") days: Int = 30,
    ): ApiCopyTraderList

    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("futures/copy/traders/{id}")
    suspend fun copyTrader(@Path("id") id: String, @Query("days") days: Int = 30): ApiCopyTraderDetail

    @GET("futures/copy/follows")
    suspend fun copyFollows(@Query("status") status: String = "all"): JsonElement

    @POST("futures/copy/follows")
    suspend fun followTrader(@Body body: ApiCopyFollowRequest): ApiCopyFollow

    @DELETE("futures/copy/follows/{id}")
    suspend fun stopCopyFollow(@Path("id") id: Long): ApiCopyFollow

    @POST("futures/copy/trader")
    suspend fun becomeCopyTrader(@Body body: ApiCopyTraderRequest): ApiCopyTrader

    // ---- 合伙人 ----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("partner/rules")
    suspend fun partnerRules(): ApiPartnerRules

    @GET("partner/me")
    suspend fun partnerMe(): ApiPartnerMe

    @POST("partner/apply")
    suspend fun applyPartner(@Body body: ApiPartnerApplyRequest): ApiPartnerMe

    @GET("partner/summary")
    suspend fun partnerSummary(): ApiPartnerSummary

    @GET("partner/links")
    suspend fun partnerLinks(): ApiPartnerLinks

    @GET("partner/commissions")
    suspend fun partnerCommissions(
        @Query("kind") kind: String? = null,
        @Query("page") page: Int = 1,
        @Query("pageSize") pageSize: Int = 30,
    ): ApiPartnerCommissions

    @GET("partner/team")
    suspend fun partnerTeam(
        @Query("type") type: String = "all",
        @Query("page") page: Int = 1,
        @Query("pageSize") pageSize: Int = 100,
    ): ApiPartnerTeam

    // ---- 事件合约 ----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("event/products")
    suspend fun eventProducts(): ApiEventProducts

    @POST("event/orders")
    suspend fun placeEventOrder(@Body body: ApiPlaceEventOrderRequest): ApiEventOrder

    @GET("event/orders")
    suspend fun eventOrders(
        @Query("status") status: String? = null,
        @Query("symbol") symbol: String? = null,
        @Query("before") before: String? = null,
        @Query("limit") limit: Int = 20,
    ): ApiListEventOrders200Response

    // ---- TradFi（公开）----
    @Headers("${ApiHeaders.NO_AUTH}: 1")
    @GET("tradfi/assets")
    suspend fun tradFiAssets(@Query("kind") kind: String? = null): ApiListTradFiAssets200Response

    // ---- 客服工单 ----
    @GET("support/tickets")
    suspend fun supportTickets(
        @Query("status") status: String? = null,
        @Query("page") page: Int = 1,
        @Query("pageSize") pageSize: Int = 20,
    ): ApiSupportTicketList

    @POST("support/tickets")
    suspend fun createSupportTicket(@Body body: ApiSupportCreateTicketRequest): ApiSupportTicket

    @GET("support/tickets/{id}")
    suspend fun supportTicket(@Path("id") id: String): ApiSupportTicket

    @POST("support/tickets/{id}/messages")
    suspend fun replySupportTicket(@Path("id") id: String, @Body body: ApiSupportReplyTicketRequest): ApiSupportTicket

    @POST("support/tickets/{id}/close")
    suspend fun closeSupportTicket(@Path("id") id: String): ApiSupportTicket

    @retrofit2.http.Streaming
    @GET("support/attachments/{id}")
    suspend fun supportAttachment(@Path("id") id: String): okhttp3.ResponseBody

    @Multipart
    @POST("support/attachments")
    suspend fun uploadSupportAttachment(@Part file: MultipartBody.Part): ApiSupportAttachment

}
