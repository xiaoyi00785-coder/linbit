package click.cowa.linbit.ui.evidence

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.feature.assets.PaperAccountViewModel
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = android.app.Application::class, qualifiers = "w360dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPaperAvailabilityTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    @Before fun main() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun reset() { Dispatchers.resetMain() }
    @Test fun guestDisplaysHonestLoginWithoutRequests() {
        MockEvidenceApi().use { api ->
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            host(f.paper())
            rule.onNodeWithText("登录后可查看模拟账户", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("登录 / 注册", useUnmergedTree = true).assertIsDisplayed()
            Assert.assertTrue(api.requests.none { it.contains("/paper/") || it.contains("/spot/order") })
            api.assertNoWrites()
        }
    }
    @Test fun productionHiltDefaultDisplaysClosedStateWithoutRequests() {
        MockEvidenceApi().use { api ->
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            api.route("GET", "/app/features", """{"features":{"paperTrading":false,"priceAlerts":false}}""")
            host(PaperAccountViewModel(click.cowa.linbit.di.AppModule.paperClient(click.cowa.linbit.core.network.NetworkFactory.paperApi(okhttp3.OkHttpClient(), api.baseUrl), click.cowa.linbit.di.AppModule.paperFeatureGate(click.cowa.linbit.core.network.NetworkFactory.api(okhttp3.OkHttpClient(), api.baseUrl))), f.tokens, f.toast))
            rule.onNodeWithText("模拟功能尚未开放", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("未发起虚拟或真实交易请求", substring = true, useUnmergedTree = true).assertIsDisplayed()
            Assert.assertTrue(api.requests.none { it.contains("/paper/") || it.contains("/spot/order") || it.contains("/order/accept") })
            api.assertNoWrites()
        }
    }
    private fun host(vm: PaperAccountViewModel) {
        rule.setContent { click.cowa.linbit.ui.theme.LinbitTheme(false) { click.cowa.linbit.feature.assets.AssetFeatureHost("paper", {}, {}, { _, _ -> }, paperVm = vm) } }
    }
}
