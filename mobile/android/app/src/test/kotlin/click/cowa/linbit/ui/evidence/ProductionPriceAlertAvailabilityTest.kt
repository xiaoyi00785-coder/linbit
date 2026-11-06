package click.cowa.linbit.ui.evidence

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.feature.alerts.PriceAlertViewModel
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = android.app.Application::class, qualifiers = "w360dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPriceAlertAvailabilityTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    @Before fun main() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun reset() { Dispatchers.resetMain() }
    private fun host(guest: Boolean, flags: String?, empty: Boolean = false): MockEvidenceApi {
        val api = MockEvidenceApi()
        val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); if (!guest) f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
        val gate = click.cowa.linbit.di.AppModule.priceAlertFeatureGate(f.retrofit)
        val client = click.cowa.linbit.di.AppModule.priceAlertClient(NetworkFactory.paperApi(okhttp3.OkHttpClient(), api.baseUrl), gate)
        flags?.let { api.route("GET", "/app/features", it) }
        if (empty) api.route("GET", "/price-alerts?page=1&pageSize=2", """{"page":1,"pageSize":2,"total":0,"items":[]}""")
        val vm = PriceAlertViewModel(client, f.tokens)
        rule.setContent { click.cowa.linbit.ui.theme.LinbitTheme(false) { click.cowa.linbit.feature.alerts.PriceAlertScreen({}, {}, vm) } }
        rule.waitForIdle()
        return api
    }
    private fun waitText(text: String) {
        rule.waitUntil(5_000) { rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
    }
    @Test fun guestNoFeatureOrAlertRequest() {
        host(true, null).use { api -> rule.onNodeWithText("登录后可查看价格预警", useUnmergedTree = true).assertIsDisplayed(); api.assertNoWrites(); Assert.assertTrue(api.requests.isEmpty()) }
    }
    @Test fun explicitFalseClosesAndMissingFlagDoesNotGetAlertsOrAnyWrites() {
        host(false, """{"features":{}}""").use { api ->
            waitText("价格预警功能尚未开放")
            api.assertNoWrites(); Assert.assertTrue(api.requests.none { it.contains("/price-alerts") })
            Assert.assertTrue(api.requests.all { it.startsWith("GET /app/features") })
        }
    }
    @Test fun publicTrueLoadsTrueEmptyFromIndependentClient() {
        host(false, """{"features":{"priceAlerts":true}}""", empty = true).use { api ->
            waitText("暂无价格预警")
            waitText("第1页 · 0/0 · 已到末页")
            api.assertNoWrites(); Assert.assertTrue(api.requests.any { it == "GET /price-alerts?page=1&pageSize=2" })
        }
    }
    @Test fun gateReadErrorFailsClosedExposesFailureWithoutAlertQuery() {
        host(false, null).use { api ->
            waitText("价格预警状态暂无法确认，请重试")
            api.assertNoWrites(); Assert.assertTrue(api.requests.none { it.contains("/price-alerts") })
        }
    }
}
