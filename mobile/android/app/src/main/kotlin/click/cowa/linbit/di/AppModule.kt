package click.cowa.linbit.di

import android.content.Context
import android.os.Build
import click.cowa.linbit.BuildConfig
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.network.SecureTokenStore
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.core.storage.SecureStore
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.Watchlist
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import okhttp3.OkHttpClient
import java.util.concurrent.TimeUnit
import javax.inject.Qualifier
import javax.inject.Singleton

@Qualifier @Retention(AnnotationRetention.BINARY) annotation class ApplicationScope

/** 刷新令牌失败（登录过期）事件：UI 据此弹出登录。 */
class SessionEvents {
    private val _expired = MutableSharedFlow<Unit>(extraBufferCapacity = 1)
    val expired: SharedFlow<Unit> = _expired.asSharedFlow()
    fun notifyExpired() { _expired.tryEmit(Unit) }
}

@Module
@InstallIn(SingletonComponent::class)
object AppModule {
    private fun baseUrl() = BuildConfig.API_BASE.trimEnd('/') + "/"

    @Provides @Singleton @ApplicationScope
    fun appScope(): CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    @Provides @Singleton
    fun secureStore(@ApplicationContext c: Context) = SecureStore(c)

    @Provides @Singleton
    fun tokenStore(store: SecureStore): TokenStore = SecureTokenStore(store)

    @Provides @Singleton
    fun appPrefs(@ApplicationContext c: Context) = AppPrefs(c)

    @Provides @Singleton
    fun watchlist(@ApplicationContext c: Context) = Watchlist(c)

    @Provides @Singleton
    fun sessionEvents() = SessionEvents()

    @Provides @Singleton
    fun toastCenter(@ApplicationScope scope: CoroutineScope) = click.cowa.linbit.ui.components.ToastCenter(scope)

    @Provides @Singleton
    fun okHttp(tokens: TokenStore, store: SecureStore, events: SessionEvents): OkHttpClient =
        NetworkFactory.client(tokens, baseUrl(), BuildConfig.USER_AGENT, { store.deviceId }, events::notifyExpired)

    @Provides @Singleton
    fun api(client: OkHttpClient): LinbitApi = NetworkFactory.api(client, baseUrl())

    @Provides @Singleton
    fun paperClient(): click.cowa.linbit.data.paper.PaperTradingClient =
        click.cowa.linbit.data.paper.PaperTradingClient(UnavailableProductionPaperApi, enabled = false)

    @Provides @Singleton
    fun socket(client: OkHttpClient, tokens: TokenStore, @ApplicationScope scope: CoroutineScope) = MarketSocket(
        client = client.newBuilder().readTimeout(0, TimeUnit.MILLISECONDS).build(),
        url = BuildConfig.WS_URL,
        userAgent = BuildConfig.USER_AGENT,
        scope = scope,
        accessToken = { tokens.accessToken },
    )

    @Provides @Singleton
    fun marketStore(api: LinbitApi, socket: MarketSocket, @ApplicationScope scope: CoroutineScope) = MarketStore(api, socket, scope)

    @Provides @Singleton
    fun tradeNav() = click.cowa.linbit.data.trade.TradeNav()

    @Provides @Singleton
    fun assetsNav() = click.cowa.linbit.data.assets.AssetsNav()

    @Provides @Singleton
    fun savedAccounts(store: SecureStore, tokens: TokenStore) = click.cowa.linbit.feature.profile.SavedAccounts(store, tokens)

    @Provides @Singleton
    fun auth(api: LinbitApi, tokens: TokenStore, store: SecureStore, @ApplicationScope scope: CoroutineScope) =
        AuthRepository(api, tokens, { store.deviceId }, { Build.MODEL ?: "Android" }, scope)
}


// Fail closed until the production feature is explicitly certified and wired by a later change.
// No real-money API is exposed through this interface; methods are never called by the disabled host.
private object UnavailableProductionPaperApi : click.cowa.linbit.data.paper.PaperTradingApi {
    private fun <T> unavailable(): retrofit2.Response<T> = retrofit2.Response.error(403, okhttp3.ResponseBody.create(null, """{"code":"FEATURE_DISABLED","message":"模拟功能尚未开放"}"""))
    override suspend fun account(): retrofit2.Response<click.cowa.linbit.data.paper.PaperAccount> = unavailable()
    override suspend fun reset(key: String, request: click.cowa.linbit.data.paper.PaperResetRequest): retrofit2.Response<click.cowa.linbit.data.paper.PaperReset> = unavailable()
    override suspend fun balances(page: Int, pageSize: Int): retrofit2.Response<click.cowa.linbit.data.paper.PaperBalances> = unavailable()
    override suspend fun placeOrder(request: click.cowa.linbit.data.paper.PaperOrderRequest): retrofit2.Response<click.cowa.linbit.data.paper.PaperOrder> = unavailable()
    override suspend fun orders(symbol: String?, status: String?, generation: Long?, page: Int, pageSize: Int): retrofit2.Response<click.cowa.linbit.data.paper.PaperOrders> = unavailable()
    override suspend fun order(id: String): retrofit2.Response<click.cowa.linbit.data.paper.PaperOrder> = unavailable()
    override suspend fun trades(symbol: String?, generation: Long?, page: Int, pageSize: Int): retrofit2.Response<click.cowa.linbit.data.paper.PaperTrades> = unavailable()
    override suspend fun alerts(symbol: String?, status: String?, page: Int, pageSize: Int): retrofit2.Response<click.cowa.linbit.data.paper.PriceAlerts> = unavailable()
    override suspend fun createAlert(request: click.cowa.linbit.data.paper.CreatePriceAlertRequest): retrofit2.Response<click.cowa.linbit.data.paper.PriceAlert> = unavailable()
    override suspend fun updateAlert(id: String, request: click.cowa.linbit.data.paper.UpdatePriceAlertRequest): retrofit2.Response<click.cowa.linbit.data.paper.PriceAlert> = unavailable()
    override suspend fun deleteAlert(id: String, revision: Int?): retrofit2.Response<Unit> = unavailable()
}
