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

internal class ProductionEvidenceFixtures(private val api: MockEvidenceApi, private val scope: CoroutineScope) {
    val tokens = InMemoryTokenStore()
    val toast = ToastCenter(scope)
    private val http = okhttp3.OkHttpClient()
    val retrofit = NetworkFactory.api(http, api.baseUrl)

    fun auth(): AuthViewModel {
        val ctx = org.robolectric.RuntimeEnvironment.getApplication()
        val repo = AuthRepository(retrofit, tokens, { "evidence-invalid-device" }, { "Robolectric Content Capture" }, scope)
        return AuthViewModel(repo, tokens, AppPrefs(ctx), toast)
    }

    fun security(): SecurityViewModel = SecurityViewModel(retrofit, toast)
    fun securityFeatures(): SecurityFeatureViewModel = SecurityFeatureViewModel(retrofit, toast)

    fun assets(): AssetsViewModel {
        tokens.accessToken = "INVALID_LAYOUT_TOKEN"
        val socket = MarketSocket(http, "ws://127.0.0.1/", "evidence", scope, accessToken = { null })
        return AssetsViewModel(retrofit, tokens, MarketStore(retrofit, socket, scope), toast)
    }
}
