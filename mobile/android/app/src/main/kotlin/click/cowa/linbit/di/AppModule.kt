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
    fun paperFeatureGate(api: LinbitApi) = click.cowa.linbit.data.paper.PaperFeatureGate(click.cowa.linbit.data.paper.PublicFeatureFlags { api.appFeatures() })

    @Provides @Singleton
    fun paperApi(client: OkHttpClient): click.cowa.linbit.data.paper.PaperTradingApi = NetworkFactory.paperApi(client, baseUrl())

    @Provides @Singleton
    fun paperClient(api: click.cowa.linbit.data.paper.PaperTradingApi, gate: click.cowa.linbit.data.paper.PaperFeatureGate) =
        click.cowa.linbit.data.paper.PaperTradingClient(api, gate)

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
