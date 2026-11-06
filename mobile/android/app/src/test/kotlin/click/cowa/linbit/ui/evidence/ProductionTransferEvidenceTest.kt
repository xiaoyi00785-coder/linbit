package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import kotlinx.coroutines.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.*

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w320dp-h844dp-xxhdpi",fontScale=1.3f)
class ProductionTransfer320EvidenceTest:ProductionTransferScenario("transfer-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionTransfer360EvidenceTest:ProductionTransferScenario("transfer-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

abstract class ProductionTransferScenario(private val label:String,private val qualifier:String) {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
        private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
        private var window:android.view.Window?=null
    @Before fun setup(){Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    private fun await(t:String){try{rule.waitUntil(12_000){runCatching{rule.onAllNodesWithText(t,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}.getOrDefault(false)}}catch(e:Throwable){throw AssertionError("wait=$t requests=${api.dumpRequests()} roots="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=Int.MAX_VALUE),e)}}
    private fun balance(free:String)="""{"items":[{"asset":"USDT","free":"$free","locked":"0.00","total":"$free"}],"page":1,"pageSize":200,"total":1}"""
    private fun reads(free:String,available:String){api.route("GET","/assets/balances?account=spot&pageSize=200",balance(free));api.route("GET","/assets/balances?account=funding&pageSize=200",balance(free));api.route("GET","/futures/account","""{"availableBalance":"$available","marginBalance":"$available"}""")}
    private fun balanceLoads(){repeat(2){reads("100","20")};api.route("POST","/futures/transfer","""{"code":"MOCK_TRANSFER_503","message":"mock transfer 503, retry allowed","ref":"MOCK-FAIL-503","status":"failed"}""",503);api.route("POST","/futures/transfer","""{"ref":"MOCK-TRANSFER-COMPLETED","status":"completed"}""");reads("90","30")}
    private fun rootBase(){repeat(24){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"100"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}}
    private fun render(f:ProductionEvidenceFixtures){
        val root=f.root();val home=f.rootHomeMarket();val assets=f.assets();rootBase()
        runBlocking{root.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
        rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides home){click.cowa.linbit.app.AppRoot(root,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(assets,f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
    }
    private fun toReview(action:()->Unit)=MockEvidenceApi().use{server->api=server;balanceLoads();val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_TRANSFER_TOKEN";render(f)
        await("首页");rule.onNodeWithTag("main-tab-assets",useUnmergedTree=true).performClick();await("划转");rule.onNodeWithText("划转",useUnmergedTree=true).performClick();rule.onNodeWithTag("transfer.amount").assertExists();api.assertNoWrites()
        rule.onNodeWithTag("transfer.amount").performTextReplacement("10.00");rule.waitUntil(5_000){runCatching{rule.onNodeWithTag("transfer.review").assertIsEnabled();true}.getOrDefault(false)};harness.app("approot-$label-form");rule.onNodeWithTag("transfer.review").performClick();await("确认划转");rule.onNodeWithTag("transfer.review.amount",useUnmergedTree=true).assertTextEquals("10.00 USDT");api.assertNoWrites()
        rule.waitForIdle();rule.waitForIdle();rule.waitUntil(5_000){window!=null};val reviewWindow=window
        harness.dialogWindow("approot-$label-review",reviewWindow);Assert.assertNotNull(reviewWindow)
        action()
    }
    @Test fun actualAppRootFormReviewCancelZeroWrite(){toReview{rule.onNodeWithTag("transfer.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()}}
    @Test fun actualAppRootFailureRetainedRetryAndBalanceResult(){toReview{api.futuresTransferFailureGate=java.util.concurrent.CountDownLatch(1);rule.onNodeWithTag("transfer.confirm",useUnmergedTree=true).performClick();rule.waitUntil(5_000){api.mutationRequests.size==1};rule.onNodeWithTag("transfer.confirm",useUnmergedTree=true).assertIsNotEnabled();api.futuresTransferFailureGate?.countDown();await("确认划转")
        Assert.assertEquals(1,api.mutationRequests.size);await("mock transfer 503, retry allowed");await("确认划转")
        rule.waitUntil(5_000){window!=null};val retryWindow=window;harness.dialogWindow("approot-$label-retry",retryWindow);Assert.assertNotNull(retryWindow)
        rule.onNodeWithTag("transfer.confirm",useUnmergedTree=true).assertIsEnabled().performClick();await("划转成功");await("读回现货可用：90.00 USDT");await("读回合约可用：30.00 USDT")
        Assert.assertEquals(2,api.mutationRequests.size);Assert.assertTrue(api.requests.contains("GET /futures/account"))
        Assert.assertTrue(api.requests.count{it=="GET /assets/balances?account=spot&pageSize=200"}>=3)
        api.mutationRequests.forEach{Assert.assertEquals("/futures/transfer",it.path);Assert.assertEquals("{\"direction\":\"in\",\"amount\":\"10.00\"}",it.body.readUtf8())}
        Assert.assertTrue("Each isolated transfer must be explicitly declared",synchronized(api.requests){api.requests.toList()}.filter{it.startsWith("POST ")}.all{it.contains("DECLARED_LOOPBACK_MOCK")});window=null;harness.app("approot-$label-result") }}
}
