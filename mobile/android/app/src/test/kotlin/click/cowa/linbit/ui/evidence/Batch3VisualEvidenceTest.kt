package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.feature.assets.*
import click.cowa.linbit.feature.profile.*
import click.cowa.linbit.feature.more.*
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** Native rendered production components with explicit loopback data. NOT real account or device screenshots. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class Batch3VisualEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        Dispatchers.setMain(UnconfinedTestDispatcher())
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        fixtures.tokens.accessToken = "INVALID_BATCH3_FIXTURE"
    }
    @After fun done() { if (::api.isInitialized) { api.assertNoWrites(); api.close() }; Dispatchers.resetMain() }
    private fun render(content: @Composable () -> Unit) { rule.activity.actionBar?.hide(); rule.setContent { LinbitTheme(false) { Box(Modifier.fillMaxSize().background(LB.p.bg)) { content() } } } }
    private fun capture(name: String) {
        rule.waitForIdle()
        rule.runOnIdle {
            val view = rule.activity.window.decorView
            val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            val out = File(System.getProperty("production.evidence.dir") ?: "build/reports/batch3").apply { mkdirs() }
            File(out, "mock-$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            File(out, "mock-$name.txt").writeText("Robolectric native-rendered production component; loopback fixtures; not device screenshot; no financial writes.\n" + api.dumpRequests())
            bitmap.recycle()
        }
    }
    @Test fun profileHub() {
        val vm = fixtures.profile()
        render { ProfileLandingScreen({}, {}, {}, {}, {}, vm) }
        rule.onNodeWithText("交易抽奖").assertExists(); rule.onNodeWithText("API Key").assertExists()
        capture("b3-profile")
    }
    @Test fun referral() {
        api.route("GET", "/partner/summary", """{"futuresRate":"0.3","spotRate":"0.2","directs":0,"total":"0","yesterday":"0","month":"0","payoutAsset":"USDT"}""")
        api.route("GET", "/partner/links", """{"code":"LB1234","url":"https://example.invalid/r/LB1234","registerUrl":"https://example.invalid/register?ref=LB1234"}""")
        api.route("GET", "/partner/team?type=all&page=1&pageSize=100", """{"items":[]}""")
        api.route("GET", "/partner/commissions?kind=referral&page=1&pageSize=30", """{"items":[],"total":0,"page":1,"pageSize":30}""")
        val vm = ReferralViewModel(fixtures.retrofit, fixtures.tokens)
        render { ReferralScreen({}, {}, vm) }
        rule.waitUntil(5000) { !vm.state.loading }
        rule.onNodeWithText("数据总览").assertExists()
        capture("b3-referral")
    }
    @Test fun cashbackMissing() {
        api.route("GET", "/campaigns/deposit-cashback", """{"code":"NOT_FOUND","message":"not found"}""", 404)
        val vm = CampaignViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)
        render { CashbackScreen({}, {}, vm) }
        rule.waitUntil(5000) { vm.cashback !is Load.Loading }
        rule.onNodeWithText("暂无充值返现活动").assertExists()
        capture("b3-cashback-missing")
    }
    @Test fun lotteryMissing() {
        api.route("GET", "/campaigns/trade-lottery", """{"code":"NOT_FOUND","message":"not found"}""", 404)
        api.route("GET", "/campaigns/trade-lottery/entries?status=pending", """{"code":"NOT_FOUND","message":"not found"}""", 404)
        val vm = CampaignViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)
        render { LotteryScreen({}, {}, {}, {}, vm) }
        rule.waitUntil(5000) { vm.lottery !is Load.Loading }
        rule.onNodeWithText("暂无交易抽奖活动").assertExists()
        capture("b3-lottery-missing")
    }
    @Test fun apiKeyMissing() {
        api.route("GET", "/account/api-keys", """{"code":"NOT_FOUND","message":"not found"}""", 404)
        val vm = ApiKeyViewModel(fixtures.retrofit, fixtures.toast)
        render { ApiKeyScreen({}, vm) }
        rule.waitUntil(5000) { vm.keys !is Load.Loading }
        rule.onNodeWithText("创建 API Key，安全管理 API 权限").assertExists()
        capture("b3-apikey-missing")
    }
    @Test fun apiKeyEmpty() {
        api.route("GET", "/account/api-keys", """{"items":[]}""")
        val vm = ApiKeyViewModel(fixtures.retrofit, fixtures.toast)
        render { ApiKeyScreen({}, vm) }
        rule.waitUntil(5000) { vm.keys !is Load.Loading }
        capture("b3-apikey-empty")
    }
    private fun prefs(open: String, name: String) {
        api.route("GET", "/account/preferences", """{"quoteCurrency":"USD","defaultQuoteCurrency":"USD","supportedQuoteCurrencies":["USD","CNY"]}""")
        api.route("GET", "/account/deposit-target", """{"code":"NOT_FOUND","message":"not found"}""", 404)
        val vm = AccountPreferencesViewModel(fixtures.retrofit, fixtures.tokens)
        val dvm = DepositTargetViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)
        val profile = fixtures.profile()
        render { androidx.compose.runtime.CompositionLocalProvider(LocalAccountPreferencesFactory provides { vm }, LocalDepositTargetFactory provides { dvm }) { UserCenterScreen(ThemeMode.LIGHT, {}, {}, {}, {}, {}, profile) } }
        rule.onNodeWithText("偏好设置").performClick()
        rule.waitUntil(5000) { dvm.target != null }
        if (open.isNotEmpty()) rule.onAllNodesWithText(open)[0].performClick()
        capture(name)
    }
    @Test fun preferencesPage() = prefs("", "b3-preferences")
    @Test fun themeSettingsSheet() = prefs("主题设置", "b3-theme-settings")
    @Test fun colorSheet() = prefs("颜色偏好", "b3-color")
    @Test fun depositTargetSheet() = prefs("默认充值账户", "b3-deposit-target")
}
