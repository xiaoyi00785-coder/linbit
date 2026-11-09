package click.cowa.linbit.feature.trade

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * 理财页「质押借币」「我的持仓」弹层闪退回归（真机三星 Android 16 反馈）。
 * 夹具：2026-11-09 只读请求生产 https://api.cowa.click 的真实返回
 *  - /loans/market、/earn/products 为公开接口真实 JSON；
 *  - 未登录访问 /earn/summary|positions|orders|records 的真实 401 JSON；
 *  - 登录态的空持仓 {"items":[]} 为按 OpenAPI 构造（未登录生产账号，不是真实账户数据）。
 * 走生产 ModalBottomSheet 路径（不开内联证据模式）。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class EarnSheetsCrashTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(Dispatchers.Main)

    private fun fixture(name: String) = javaClass.classLoader!!.getResource("fixtures/earn-20261109/$name")!!.readText()
    private val unauthorized by lazy { fixture("earn-positions-401.json") }

    /** mode: "prod" 真实返回；"loan401" / "loan404" / "loanEmpty" / "loanBad" 借币市场异常。 */
    private fun vm(loggedIn: Boolean, mode: String = "prod", accountCode: Int = 200): SpotSubpagesViewModel {
        val client = OkHttpClient.Builder().addInterceptor { chain ->
            val path = chain.request().url.encodedPath
            val (code, body) = when {
                path.endsWith("/earn/products") -> 200 to fixture("earn-products.json")
                path.endsWith("/loans/market") -> when (mode) {
                    "loan401" -> 401 to unauthorized
                    "loan404" -> 404 to """{"code":"NOT_FOUND","message":"not found"}"""
                    "loanEmpty" -> 200 to "{}"
                    "loanBad" -> 200 to """{"asset":"USDT","terms":[{"term":"7d","days":"seven","annualRate":7}],"collaterals":null}"""
                    else -> 200 to fixture("loans-market.json")
                }
                path.contains("/earn/") && !loggedIn -> 401 to unauthorized
                path.contains("/earn/") && accountCode != 200 -> accountCode to """{"code":"X","message":"err"}"""
                path.endsWith("/earn/summary") -> 200 to "{}"
                path.contains("/earn/") -> 200 to """{"items":[]}"""
                else -> 404 to """{"code":"NOT_FOUND","message":"not found"}"""
            }
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(code).message("fixture")
                .body(body.toResponseBody()).build()
        }.build()
        val tokens = InMemoryTokenStore().also { if (loggedIn) it.accessToken = "fixture-token" }
        return SpotSubpagesViewModel(NetworkFactory.api(client, "https://example.invalid/"), tokens, ToastCenter(scope))
    }

    private var logins = 0
    private fun render(vm: SpotSubpagesViewModel) {
        rule.setContent { LinbitTheme(false) { EarnSubpage(0.dp, onLogin = { logins++ }, vm = vm) } }
        rule.waitUntil(10_000) { rule.onAllNodesWithText("质押借币").fetchSemanticsNodes().isNotEmpty() }
    }

    private fun waitText(t: String) = rule.waitUntil(10_000) {
        rule.onAllNodesWithText(t, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
    }

    @Test fun loansSheetLoggedOutWithRealMarket() {
        render(vm(false))
        rule.onNodeWithText("质押借币").performClick()
        waitText("借币市场")
        waitText("单用户借款上限")
        waitText("0.0003847% / 3.37%")
        waitText("BTC")
    }

    @Test fun loansSheetLoggedInWithRealMarket() {
        render(vm(true))
        rule.onNodeWithText("质押借币").performClick()
        waitText("0.00068493% / 6%")
    }

    private fun loanError(mode: String) {
        render(vm(false, mode))
        rule.onNodeWithText("质押借币").performClick()
        waitText("借币市场加载失败")
        waitText("重试")
    }
    @Test fun loansSheet401ShowsRetry() = loanError("loan401")
    @Test fun loansSheet404ShowsRetry() = loanError("loan404")

    @Test fun loansSheetEmptyOrMalformedShowsEmptyState() {
        render(vm(false, "loanEmpty"))
        rule.onNodeWithText("质押借币").performClick()
        waitText("暂无借币市场")
    }

    @Test fun loansSheetMalformedTypesDoNotCrash() {
        render(vm(false, "loanBad"))
        rule.onNodeWithText("质押借币").performClick()
        waitText("重试")
    }

    @Test fun loanFormatMatchesReferenceCells() {
        assertEquals("0.0003847% / 3.37%", LoanMarketFormat.rateCell("0.000003847", "0.0337"))
        assertEquals("0.00068493% / 6%", LoanMarketFormat.rateCell("0.0000068493", "0.06"))
        assertEquals("-- / --", LoanMarketFormat.rateCell("abc", null))
        assertEquals("活期", LoanMarketFormat.termLabel("flexible", 0))
        assertEquals("30 天", LoanMarketFormat.termLabel("30d", 30))
        assertEquals("1,200,000", LoanMarketFormat.amount("1200000"))
        assertEquals("80%", LoanMarketFormat.pct("0.8"))
    }

    @Test fun positionsLoggedOutRoutesToLogin() {
        render(vm(false))
        rule.onNodeWithText("我的持仓", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        assertEquals(1, logins)
    }

    @Test fun positionsLoggedInEmptyShowsEmptyState() {
        render(vm(true))
        rule.onNodeWithText("我的持仓", useUnmergedTree = true).performClick()
        waitText("暂无持仓与流水")
    }

    private fun positionsError(code: Int) {
        render(vm(true, accountCode = code))
        rule.onNodeWithText("我的持仓", useUnmergedTree = true).performClick()
        waitText("持仓加载失败")
        waitText("重试")
    }
    @Test fun positionsLoggedIn401ShowsRetry() = positionsError(401)
    @Test fun positionsLoggedIn404ShowsRetry() = positionsError(404)

    /** -PoffscreenShots=1：同窗内联渲染弹层并截图到 build/reports/earn-shots/（不入库）。 */
    @Test fun offscreenShotsOfFixedSheets() {
        if (System.getProperty("offscreen.shots") != "1") return
        val out = java.io.File("build/reports/earn-shots").absoluteFile.also { it.mkdirs() }
        fun cap(name: String) = rule.runOnIdle {
            val v = rule.activity.findViewById<android.view.View>(android.R.id.content)
            val b = android.graphics.Bitmap.createBitmap(v.width, v.height, android.graphics.Bitmap.Config.ARGB_8888)
            v.draw(android.graphics.Canvas(b))
            java.io.File(out, "$name.png").outputStream().use { b.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
        }
        val model = vm(true)
        rule.setContent {
            androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.feature.auth.LocalLbSheetInlineEvidence provides true) {
                LinbitTheme(false) { EarnSubpage(0.dp, onLogin = {}, vm = model) }
            }
        }
        rule.waitUntil(10_000) { rule.onAllNodesWithText("质押借币").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("质押借币").performClick()
        waitText("单用户借款上限")
        cap("earn-loans-sheet")
    }

    @Test fun earnTabsWithRealProductsDoNotCrash() {
        render(vm(false))
        listOf("简单赚币", "链上赚币").forEach { tab ->
            rule.onNodeWithText(tab).performClick(); rule.waitForIdle()
            rule.onNodeWithText(tab).performClick(); rule.waitForIdle()
        }
        assertTrue(rule.onAllNodesWithText("全部产品").fetchSemanticsNodes().isNotEmpty())
    }
}
