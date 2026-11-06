package click.cowa.linbit.ui.evidence

import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.feature.assets.AssetsViewModel
import click.cowa.linbit.feature.auth.AuthViewModel
import click.cowa.linbit.feature.profile.SecurityFeatureViewModel
import click.cowa.linbit.feature.profile.SecurityViewModel
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CoroutineScope
import com.jakewharton.retrofit2.converter.kotlinx.serialization.asConverterFactory
import okhttp3.MediaType.Companion.toMediaType
import retrofit2.Retrofit

internal class ProductionEvidenceFixtures(private val api: MockEvidenceApi, private val scope: CoroutineScope) {
    val tokens = InMemoryTokenStore()
    val toast = ToastCenter(scope)
    private val http = okhttp3.OkHttpClient()
    val retrofit = NetworkFactory.api(http, api.baseUrl)

    fun auth(): AuthViewModel {
        val ctx = org.robolectric.RuntimeEnvironment.getApplication()
        val repo = AuthRepository(retrofit, tokens, { "evidence-invalid-device" }, { "Robolectric Content Capture" }, scope)
        val fakeBridge = click.cowa.linbit.feature.auth.PasskeyCredentialBridge { _, _ -> error("Native bridge must be explicitly mocked per passkey scenario") }
        return AuthViewModel(repo, tokens, AppPrefs(ctx), toast, click.cowa.linbit.feature.auth.PasskeyLoginService(retrofit, fakeBridge, { "evidence-invalid-device" }, { "Robolectric" }))
    }

    fun security(): SecurityViewModel = SecurityViewModel(retrofit, toast)
    fun securityFeatures(): SecurityFeatureViewModel = SecurityFeatureViewModel(retrofit, toast)

    fun rootHomeMarket(): click.cowa.linbit.app.RootHomeMarketFactory {
        val socket = MarketSocket(http, "ws://127.0.0.1/", "evidence", scope, accessToken = { null })
        val store = MarketStore(retrofit, socket, scope); val watch = click.cowa.linbit.data.Watchlist(org.robolectric.RuntimeEnvironment.getApplication())
        val home = click.cowa.linbit.feature.home.HomeViewModel(retrofit, store, tokens, watch)
        val markets = click.cowa.linbit.feature.markets.MarketsViewModel(store, watch)
        return click.cowa.linbit.app.RootHomeMarketFactory({ home }, { markets })
    }

    fun spot(): click.cowa.linbit.feature.trade.SpotSubpagesViewModel = click.cowa.linbit.feature.trade.SpotSubpagesViewModel(retrofit, tokens, toast)

    fun tradFi(): click.cowa.linbit.feature.more.TradFiViewModel = click.cowa.linbit.feature.more.TradFiViewModel(retrofit)

    fun root(): click.cowa.linbit.app.AppViewModel {
        val ctx = org.robolectric.RuntimeEnvironment.getApplication()
        val repo = AuthRepository(retrofit, tokens, { "evidence-invalid-device" }, { "Robolectric AppRoot" }, scope)
        return click.cowa.linbit.app.AppViewModel(AppPrefs(ctx), tokens, repo, click.cowa.linbit.di.SessionEvents(), toast, click.cowa.linbit.data.trade.TradeNav(), click.cowa.linbit.data.assets.AssetsNav())
    }

    fun badge(): click.cowa.linbit.feature.more.NotificationBadgeViewModel = click.cowa.linbit.feature.more.NotificationBadgeViewModel(retrofit, tokens)

    /** Same production ViewModel, with test-only loopback adapter. Never a LinbitApi/paper hybrid. */
    fun paper(client: click.cowa.linbit.data.paper.PaperTradingClient = paperClient(true)): click.cowa.linbit.feature.assets.PaperAccountViewModel {
        return click.cowa.linbit.feature.assets.PaperAccountViewModel(
            client, tokens, toast)
    }

    fun paperClient(enabled: Boolean): click.cowa.linbit.data.paper.PaperTradingClient {
        val paper = NetworkFactory.paperApi(http, api.baseUrl)
        val gate = click.cowa.linbit.di.AppModule.paperFeatureGate(NetworkFactory.api(http, api.baseUrl))
        return click.cowa.linbit.di.AppModule.paperClient(paper, gate).also {
            api.defaultFeatureFlags = """{"features":{"paperTrading":$enabled,"priceAlerts":false}}"""
            kotlinx.coroutines.runBlocking { it.refreshFeature() }
        }
    }

    fun assets(): AssetsViewModel {
        tokens.accessToken = "INVALID_LAYOUT_TOKEN"
        val socket = MarketSocket(http, "ws://127.0.0.1/", "evidence", scope, accessToken = { null })
        return AssetsViewModel(retrofit, tokens, MarketStore(retrofit, socket, scope), toast)
    }
}
