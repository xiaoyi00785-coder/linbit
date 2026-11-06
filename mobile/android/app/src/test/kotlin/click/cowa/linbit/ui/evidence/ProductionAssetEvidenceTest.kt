package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.printToString
import androidx.compose.ui.unit.dp
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.data.assets.AssetsRoute
import click.cowa.linbit.feature.assets.AssetsHost
import click.cowa.linbit.feature.assets.AssetsViewModel
import click.cowa.linbit.feature.trade.ConvertSubpage
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assume
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Small scoped high-risk Host/NavHost cases. Writes remain stopped before confirm with invalid data. */
@RunWith(RobolectricTestRunner::class)
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionAssetEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private val scope = CoroutineScope(Dispatchers.Main)
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    private var dialog: android.view.Window? = null
    @get:org.junit.Rule val testName = org.junit.rules.TestName()

    @Before fun setup() {
        Assume.assumeTrue(System.getProperty("offscreen.shots") == "1" && System.getProperty("production.evidence") == "1")
        Assume.assumeTrue(System.getProperty("production.evidence.dir") != null)
        kotlinx.coroutines.Dispatchers.setMain(kotlinx.coroutines.test.UnconfinedTestDispatcher())
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, scope)
        fixtures.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
    }
    @After fun teardown() {
        if (::api.isInitialized) {
            FileLog.save(api, harness.out, "mock-routes-${testName.methodName}")
            api.close()
        }
        kotlinx.coroutines.Dispatchers.resetMain()
    }

    private fun waitText(text: String, substring: Boolean = false) {
        try { rule.waitUntil(timeoutMillis = 15_000) { runCatching { rule.onAllNodesWithText(text, substring = substring, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
        catch (e: Throwable) { throw AssertionError("wait $text; routes=${api.requests}; tree=${runCatching { rule.onRoot(useUnmergedTree = true).printToString() }.getOrDefault("unavailable")}", e) }
    }
    private fun assets(route: AssetsRoute): AssetsViewModel {
        val vm = fixtures.assets()
        val nav = AssetsNav().also { it.open(route) }
        rule.setContent { LinbitTheme(darkTheme = false) { AssetsHost(nav, vm, onDialogWindow = { dialog = it }) } }
        rule.waitForIdle()
        return vm
    }

    private fun validBalances() {
        val body = """{"items":[{"asset":"USDT","free":"100.00","locked":"0.00","total":"100.00"}],"page":1,"pageSize":200,"total":1}"""
        api.route("GET", "/assets/balances?account=spot&pageSize=200", body)
        api.route("GET", "/assets/balances?account=funding&pageSize=200", body)
        api.route("GET", "/futures/account", "{}")
    }

    @Test fun depositNormalMockAddressScreenshotIsNotUsableForTransfers() {
        api.route("GET", "/wallet/networks?asset=USDT", """{"networks":[{"asset":"USDT","network":"TRC20","depositEnabled":true,"withdrawEnabled":true,"minDeposit":"1.0","confirmations":1,"recommended":true,"estimatedArrivalMinutes":3}]}""")
        api.route("GET", "/wallet/deposit/address?asset=USDT&network=TRC20", """{"asset":"USDT","network":"TRC20","address":"INVALID-EVIDENCE-NOT-A-DEPOSIT-ADDRESS","memo":null,"memoRequired":false}""")
        assets(AssetsRoute.Deposit("USDT"))
        waitText("INVALID-EVIDENCE-NOT-A-DEPOSIT-ADDRESS")
        harness.app("deposit-normal", 1170, 2364)
        api.assertNoWrites()
    }

    @Test fun depositExplicitErrorMockIsAProductionFailure() {
        api.route("GET", "/wallet/networks?asset=USDT", """{"code":"WALLET_ASSET_UNAVAILABLE","message":"测试：该币种暂不可用"}""", 422)
        assets(AssetsRoute.Deposit("USDT"))
        rule.onNodeWithText("测试：该币种暂不可用", substring = true).assertIsDisplayed()
        harness.app("deposit-error", 1170, 2364)
        api.assertNoWrites()
    }

    @Test fun withdrawNormalMockStopsBeforeFinalSecuritySubmit() {
        api.route("GET", "/wallet/withdraw/networks?asset=USDT", """{"asset":"USDT","networks":[{"network":"TRC20","withdrawEnabled":true,"memoRequired":false,"minWithdraw":"1","maxWithdraw":"200","gasFeeEstimate":"0.1","gasFeeAsset":"USDT","estimatedArrivalMinutes":3}]}""")
        api.route("GET", "/wallet/networks?asset=USDT", """{"networks":[{"asset":"USDT","network":"TRC20","depositEnabled":true,"withdrawEnabled":true,"minWithdraw":"1","maxWithdraw":"200","withdrawFee":"0.1","withdrawFeeAsset":"USDT","estimatedArrivalMinutes":3}]}""")
        validBalances()
        validBalances() // Host/root read-only reload: no synthetic ViewModel balance seed.
        api.route("GET", "/account/security", """{"twoFactor":{"enabled":true,"method":"totp"}}""")
        val vm = assets(AssetsRoute.Withdraw("USDT"))
        waitText("TRC20", substring = true)
        waitText("100", substring = true); rule.waitForIdle()
        waitText("预计到账数量")
        rule.onNodeWithTag("withdraw.address").performTextReplacement("INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER")
        rule.onNodeWithTag("withdraw.amount").performTextReplacement("1")
        try { rule.waitUntil(timeoutMillis = 10_000) { runCatching { rule.onNodeWithTag("withdraw.review").assertIsEnabled(); true }.getOrDefault(false) } } catch (e: Throwable) { throw AssertionError("review not enabled: routes=${api.requests}; tree=${rule.onRoot(useUnmergedTree = true).printToString()}",e) }
        rule.onNodeWithTag("withdraw.review").performClick()
        rule.waitUntil(timeoutMillis = 5_000) { runCatching { rule.onNodeWithText("安全验证").assertIsDisplayed(); true }.getOrDefault(false) }
        org.junit.Assert.assertFalse(api.requests.any { it.startsWith("POST /wallet/withdraw") })
        org.junit.Assert.assertFalse(api.requests.any { it.startsWith("POST /account/code/send") })
        if (dialog == null) { java.io.File(harness.out, "UNVERIFIED_WITHDRAW_DIALOG.txt").writeText("Production semantic review present; independent Window not capturable by offscreen harness. Do not infer pixels; use ProductionDialogPixelCopyTest.\n")
            Assume.assumeTrue("Production withdraw review present, no capturable independent Window offscreen", false) }
        harness.dialogWindow("withdraw-before-confirm", dialog)
    }

    @Test fun withdrawNormalNetworkFetchEmptyAndErrorRemainExplicit() {
        api.route("GET", "/wallet/withdraw/networks?asset=USDT", """{"asset":"USDT","networks":[]}""")
        api.route("GET", "/wallet/networks?asset=USDT", """{"code":"TEST_NETWORK_ERROR","message":"测试：网络获取失败"}""", 503)
        validBalances()
        assets(AssetsRoute.Withdraw("USDT"))
        rule.onNodeWithText("请选择网络").assertIsDisplayed()
        harness.app("withdraw-empty-error", 1170, 2364)
        api.assertNoWrites()
    }

    @Test fun depositEmptyNetworks() {
        api.route("GET", "/wallet/networks?asset=USDT", """{"networks":[]}""")
        assets(AssetsRoute.Deposit("USDT")); waitText("该币种暂不支持充值")
        harness.app("deposit-empty", 1170, 2364); api.assertNoWrites()
    }

    @Test fun convertPausedProductionMock() {
        fixtures.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
        api.route("GET", "/convert/pairs", """{"assets":[{"asset":"USDT"},{"asset":"BTC"}],"pairs":[{"from":"USDT","to":"BTC","paused":true,"pauseReason":"price_unavailable","route":"direct"}]}""")
        rule.setContent { LinbitTheme(darkTheme = false) { ConvertSubpage(bottomInset = 0.dp, onLogin = {}, vm = SpotSubpagesViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)) } }
        waitText("该币对暂无可用价格"); rule.onNodeWithText("获取报价").assertIsNotEnabled(); harness.app("convert-paused", 1170, 2364); api.assertNoWrites()
    }

    @Test fun convertNormalProductionMock() {
        api.route("GET", "/convert/pairs", """{"assets":[{"asset":"USDT"},{"asset":"BTC"}],"pairs":[{"from":"USDT","to":"BTC","enabled":true,"paused":false,"route":"direct"}],"minUsdt":"0.01"}""")
        rule.setContent { LinbitTheme(darkTheme = false) { ConvertSubpage(bottomInset = 0.dp, onLogin = {}, vm = SpotSubpagesViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)) } }
        waitText("兑出 USDT")
        harness.app("convert-normal", 1170, 2364)
        api.assertNoWrites()
    }

    @Test fun convertEmptyProductionMock() {
        fixtures.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
        api.route("GET", "/convert/pairs", """{"assets":[{"asset":"USDT"}],"pairs":[],"minUsdt":"0.01"}""") // Direct valid generated model, no synthetic state seeding.
        rule.setContent { LinbitTheme(darkTheme = false) { ConvertSubpage(bottomInset = 0.dp, onLogin = {}, vm = SpotSubpagesViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)) } }
        waitText("暂无可交易闪兑币对")
        harness.app("convert-empty", 1170, 2364)
        api.assertNoWrites()
    }
}

private object FileLog {
    fun save(api: MockEvidenceApi, dir: java.io.File, prefix: String) {
        java.io.File(dir, "$prefix.txt").writeText(api.requests.joinToString("\n"))
    }
}
