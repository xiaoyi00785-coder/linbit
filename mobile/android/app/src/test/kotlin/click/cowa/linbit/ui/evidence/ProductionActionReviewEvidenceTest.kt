package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.unit.dp
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import click.cowa.linbit.feature.trade.*
import click.cowa.linbit.feature.assets.AssetFeatureHost
import click.cowa.linbit.ui.theme.LinbitTheme
import org.junit.*
import org.junit.runner.RunWith
import kotlinx.coroutines.test.*
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Production host + real Dialog Window + explicitly failed loopback mutation. Never a success mock. */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionActionReviewEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var earnVm: click.cowa.linbit.feature.trade.SpotSubpagesViewModel
    private lateinit var strategyVm: click.cowa.linbit.feature.trade.SpotSubpagesViewModel
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private var dialog: android.view.Window? = null
    private fun wait(text: String) { try { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } } catch (e: Throwable) { throw AssertionError("Wait=$text tree=" + rule.onRoot(useUnmergedTree=false).printToString(10000).toString() + if (::strategyVm.isInitialized) " strategy error=${strategyVm.error}, preview=${strategyVm.gridPreview}, loading=${strategyVm.loading}, actionFailure=${strategyVm.actionFailure}" else "", e) } }
    private fun capture(name: String) { rule.waitUntil(5_000) { dialog != null }; harness.dialogWindow(name, dialog) }
    @Before fun installMain() { kotlinx.coroutines.Dispatchers.setMain(kotlinx.coroutines.test.UnconfinedTestDispatcher()) }
    @After fun resetMain() { kotlinx.coroutines.Dispatchers.resetMain() }
    private fun earn(api: MockEvidenceApi) {
        val vm = evidenceVm(api); earnVm = vm
        rule.setContent { LinbitTheme(false) { AssetFeatureHost("earn", {}, {}, {_, _->}, vm, onDialogWindow = { dialog = it }) } }
        wait("产品列表"); wait("USDT 活期理财"); wait("我的持仓")
    }
    private fun strategy(api: MockEvidenceApi) {
        val vm = evidenceVm(api); strategyVm = vm
        rule.setContent { LinbitTheme(false) { AssetFeatureHost("strategy", {}, {}, {_, _->}, vm, onDialogWindow = { dialog = it }) } }
        try { rule.waitUntil(10_000) { vm.myGrids.isNotEmpty() } }
        catch (e: Throwable) { throw AssertionError("Strategy load failed: error=${vm.error}; loading=${vm.loading}; requests=${api.dumpRequests()}", e) }
    }
    @Test fun normalProductsAndHolding() { MockEvidenceApi().use { api ->
        earnRoutes(api); earn(api); wait("我的持仓"); harness.app("asset-earn-normal-holding", 1170, 2364); api.assertNoWrites()
    } }
    @Test fun subscribeReviewAndStableFailure() { MockEvidenceApi().use { api ->
        earnRoutes(api); earn(api); wait("产品列表")
        harness.app("asset-earn-normal-products", 1170, 2364)
        rule.onNodeWithText("申购", useUnmergedTree = true).performScrollTo().performClick()
        rule.onAllNodesWithText("金额", substring = true).filterToOne(hasSetTextAction()).performTextInput("100")
        rule.onNodeWithText("下一步 · 申购复核", substring = true).performClick(); wait("申购最终复核"); capture("asset-earn-subscribe-review")
        api.explicitFailure("POST", "/earn/products/evidence-earn/subscribe", "测试：申购失败")
        rule.onNodeWithText("确认申购", useUnmergedTree = true).performClick(); wait("操作未完成：测试：申购失败")
        rule.onNodeWithText("确认申购", useUnmergedTree = true).assertIsEnabled(); capture("asset-earn-subscribe-failure"); api.assertOnlyExpectedFailures()
    } }
    @Test fun allRedeemReviewUsesNullAmountAndStableFailure() { MockEvidenceApi().use { api ->
        earnRoutes(api); earn(api); wait("产品列表")
        rule.waitUntil(10_000) { !earnVm.loading && earnVm.error == null && earnVm.earnPositions.none { it.productId == "evidence-earn" }.not() }
        rule.onNode(hasText("赎回") and hasClickAction()).performScrollTo()
        rule.waitForIdle()
        rule.onNode(hasText("赎回") and hasClickAction()).performClick()
        try { wait("当前持仓") } catch (e: Throwable) {
            throw AssertionError("Redeem did not open: loading=${earnVm.loading}, error=${earnVm.error}, positions=${earnVm.earnPositions}, requests=${api.dumpRequests()}", e)
        }
        rule.onNodeWithText("选择全部持仓", useUnmergedTree = true).performScrollTo().performClick(); rule.onNodeWithText("下一步 · 全部赎回复核", substring = true).performClick()
        wait("全部赎回最终复核"); rule.onNodeWithText("全部持仓 · 1250 USDT", useUnmergedTree = true).assertExists()
        Assert.assertEquals("全部持仓 · 1250 USDT", "全部持仓 · ${earnVm.earnPositions.single().amount} ${earnVm.earnPositions.single().asset}")
        capture("asset-earn-redeem-all-review")
        api.explicitFailure("POST", "/earn/products/evidence-earn/redeem", "测试：赎回失败")
        rule.onNodeWithText("确认全部赎回", useUnmergedTree = true).performClick(); wait("操作未完成：测试：赎回失败")
        rule.onNodeWithText("全部赎回最终复核", useUnmergedTree = true).assertExists()
        rule.onNodeWithText("确认全部赎回", useUnmergedTree = true).assertExists()
        check(!earnVm.acting) { "Redeem must leave the acting state after an explicit loopback failure" }
        Assert.assertEquals("""{"all":true}""", api.lastMutationBody())
        api.assertOnlyExpectedFailures(); capture("asset-earn-redeem-all-failure")
    } }
    @Test fun copyReviewFailureAndStopFailureStayVisible() { MockEvidenceApi().use { api ->
        strategyRoutes(api); strategy(api); wait("我的策略")
        rule.onNodeWithText("复制策略", useUnmergedTree = true).performScrollTo().performClick(); rule.onAllNodesWithText("投资额", substring = true).filterToOne(hasSetTextAction()).performTextInput("100")
        rule.onNodeWithText("下一步 · 复制复核", substring = true).performClick(); wait("实际投资额"); capture("asset-grid-copy-review")
        api.explicitFailure("POST", "/strategy/grid/square/evidence-copy/copy", "测试：复制失败")
        rule.onNodeWithText("确认复制", useUnmergedTree = true).performClick(); wait("测试：复制失败"); capture("asset-grid-copy-failure")
        Assert.assertTrue(api.requests.none { it.contains("/stop") })
    } }
    @Test fun stopReviewFailureDoesNotDismiss() { MockEvidenceApi().use { api ->
        strategyRoutes(api); strategy(api); wait("我的策略")
        rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText("停止", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) }
        rule.onNodeWithText("停止", useUnmergedTree = true).performScrollTo().performClick(); wait("将保留底仓，仅撤单"); capture("asset-grid-stop-review")
        api.explicitFailure("POST", "/strategy/grid/evidence-grid/stop", "测试：停止失败")
        rule.onNodeWithText("确认停止", useUnmergedTree = true).performClick(); wait("测试：停止失败"); rule.onNodeWithText("确认停止", useUnmergedTree = true).assertIsEnabled(); capture("asset-grid-stop-failure"); api.assertOnlyExpectedFailures()
    } }
    @Test fun createPreviewBoundToFullFormAndFailure() { MockEvidenceApi().use { api ->
        strategyRoutes(api); strategy(api); wait("我的策略"); rule.onNodeWithText("创建网格", useUnmergedTree = true).performScrollTo().performClick()
        adjustable("下限价").performTextInput("60000"); adjustable("上限价").performTextInput("70000"); adjustable("网格数").performTextReplacement("10"); adjustable("投资额").performTextInput("100")
        api.route("GET", "/strategy/grid/preview?symbol=BTCUSDT&mode=arithmetic&lowerPrice=60000&upperPrice=70000&gridCount=10&investment=100", """{"symbol":"BTCUSDT","mode":"arithmetic","currentPrice":"65000","levels":["60000","70000"],"qtyPerGrid":"0.001","buyOrders":5,"sellOrders":5,"initialBuyQuote":"65","profitPerGridMin":"0.1","profitPerGridMax":"0.2"}""")
        rule.onNodeWithText("试算", useUnmergedTree = true).performClick()
        try { wait("创建网格最终复核") } catch (e: Throwable) { throw AssertionError("Preview failed; requests=${api.dumpRequests()}", e) }
        capture("asset-grid-create-review")
        api.explicitFailure("POST", "/strategy/grid", "测试：创建失败")
        rule.onNodeWithText("确认创建", useUnmergedTree = true).performClick()
        try {
            wait("操作未完成：测试：创建失败")
        } catch (e: Throwable) {
            throw AssertionError("Grid create failure did not appear. requests=${api.dumpRequests()} dialog=" + rule.onAllNodes(isDialog()).fetchSemanticsNodes().joinToString("\\n"), e)
        }
        capture("asset-grid-create-failure"); api.assertOnlyExpectedFailures()
    } }
    private fun adjustable(label: String) = rule.onNodeWithTag("grid.create." + when (label) { "下限价" -> "lower"; "上限价" -> "upper"; "网格数" -> "grids"; else -> "investment" })
}
