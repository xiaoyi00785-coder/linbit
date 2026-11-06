package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.OkHttpClient
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.*

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w320dp-h844dp-xxhdpi",fontScale=1.3f)
class ProductionAlgoHistory320EvidenceTest: ProductionAlgoHistoryScenario("algo-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionAlgoHistory360EvidenceTest: ProductionAlgoHistoryScenario("algo-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

@OptIn(ExperimentalCoroutinesApi::class)
abstract class ProductionAlgoHistoryScenario(private val label:String,private val qualifier:String) {
    private val dispatcher=UnconfinedTestDispatcher()
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
    private lateinit var vm:click.cowa.linbit.feature.trade.TradeHistoryViewModel
    private var window:android.view.Window?=null
    @Before fun main(){Dispatchers.setMain(dispatcher);Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    @After fun reset(){Dispatchers.resetMain()}
    private fun await(t:String){try{rule.waitUntil(12_000){runCatching{rule.onAllNodesWithText(t,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}.getOrDefault(false)}}catch(e:Throwable){java.io.File(System.getProperty("production.evidence.dir")+"/${label}-failure-tree.txt").apply{parentFile.mkdirs()}.writeText(rule.onAllNodes(isRoot()).fetchSemanticsNodes().joinToString("\n"));throw AssertionError("wait=$t requests=${api.dumpRequests()} state=${vm.futAlgos.map{it.symbol}} busy=${vm.actionBusy} error=${vm.actionError}",e)}}
    private fun base(){repeat(6){api.route("GET","/assets/balances?account=spot&pageSize=200","""{"items":[]}""");api.route("GET","/assets/balances?account=funding&pageSize=200","""{"items":[]}""");api.route("GET","/futures/account","{}")};repeat(3){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"0"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}}
    private fun tradingReads(){
        repeat(3){
            api.route("GET","/futures/contracts","""{"contracts":[]}""");api.route("GET","/futures/account","{}");api.route("GET","/futures/positions","""{"positions":[]}""");api.route("GET","/futures/orders?status=open","""{"orders":[]}""");api.route("GET","/futures/plan-orders?status=active&limit=50","""{"orders":[]}""");api.route("GET","/futures/tpsl-orders?status=active&limit=50","""{"orders":[]}""")
        }
    }
    private fun orders(s:String="BTCUSDT",id:Long=9001)= """{"orders":[{"algoId":$id,"symbol":"$s","algoType":"tp","status":"active","triggerPrice":"70000","quantity":"0.001","side":"sell","positionSide":"long","createdAt":"2026-01-01T00:00:00Z"}],"nextCursor":null}"""
    private fun render(cancel:Boolean=false){
        // Keep root ViewModels stable; do not recreate a new owner on every composition snapshot.

        MockEvidenceApi().use{server->api=server;base();tradingReads()
            val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_LAYOUT_TOKEN"
            val rootVm=f.root();runBlocking{rootVm.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
            val scope=CoroutineScope(Dispatchers.Main);val market=click.cowa.linbit.data.MarketStore(f.retrofit,click.cowa.linbit.core.realtime.MarketSocket(OkHttpClient(),"ws://127.0.0.1/","fake",scope,accessToken={null}),scope)
            vm=click.cowa.linbit.feature.trade.TradeHistoryViewModel(f.retrofit,f.tokens,market)
            api.route("GET","/futures/plan-orders?status=active&limit=50",orders("STALE",7));api.route("GET","/futures/tpsl-orders?status=active&limit=50",orders())
            if(cancel){api.route("DELETE","/futures/tpsl-orders/9001","""{"code":"EVIDENCE_FAILURE","message":"测试：TPSL撤单失败，可重试"}""",503);api.route("DELETE","/futures/tpsl-orders/9001","""{"acknowledged":true}""");api.route("GET","/futures/tpsl-orders?status=active&limit=50",orders("AFTERREADBACK",9002))}
            rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket(),click.cowa.linbit.app.LocalTradeHistoryFactory provides {vm}){click.cowa.linbit.app.AppRoot(rootVm,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(f.assets(),f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
            await("首页");rule.onNodeWithTag("main-tab-futures",useUnmergedTree=true).performClick();await("合约历史");rule.onNodeWithContentDescription("合约历史",useUnmergedTree=true).performClick();await("历史记录");rule.onNodeWithText("条件委托",useUnmergedTree=true).performScrollTo().performClick();await("止盈止损");rule.onNodeWithText("止盈止损",useUnmergedTree=true).performClick();await("BTCUSDT")
            rule.waitUntil(5_000){api.requests.contains("GET /futures/tpsl-orders?status=active")}
            harness.app("approot-$label-tpsl")
            rule.onNodeWithText("撤销",useUnmergedTree=true).performScrollTo().performClick();await("撤销止盈止损？")
            rule.waitUntil(5_000){window!=null};harness.dialogWindow("approot-$label-tpsl-review",window)
            rule.onNodeWithTag("history.algo.cancel.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()
            if(cancel){rule.onNodeWithText("撤销",useUnmergedTree=true).performScrollTo().performClick();rule.onNodeWithTag("history.algo.cancel.confirm",useUnmergedTree=true).performClick();await("测试：TPSL撤单失败，可重试")
                Assert.assertEquals(1,api.mutationRequests.size);Assert.assertNotNull(vm.actionError)
                rule.onNodeWithTag("history.algo.cancel.confirm",useUnmergedTree=true).assertIsEnabled().performClick();await("AFTERREADBACK")
                Assert.assertEquals(2,api.mutationRequests.size);Assert.assertEquals(2,api.requests.count{it=="GET /futures/tpsl-orders?status=active&limit=50"});api.mutationRequests.forEach{Assert.assertEquals("DELETE",it.method)};harness.app("approot-$label-tpsl-after-cancel")
            }else api.assertNoWrites()
        }
    }
    @Test fun actualProductionHistoryHostBigTextAndDarkTpslZeroWriteReview(){
        MockEvidenceApi().use { server -> api=server
            val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_LAYOUT_TOKEN"
            val market=click.cowa.linbit.data.MarketStore(f.retrofit,click.cowa.linbit.core.realtime.MarketSocket(OkHttpClient(),"ws://127.0.0.1/","fake",CoroutineScope(Dispatchers.Main),accessToken={null}),CoroutineScope(Dispatchers.Main))
            vm=click.cowa.linbit.feature.trade.TradeHistoryViewModel(f.retrofit,f.tokens,market)
            api.route("GET","/futures/orders?status=open&limit=100","""{"orders":[]}""");api.route("GET","/futures/plan-orders?status=active&limit=50",orders("STALE",7));api.route("GET","/futures/tpsl-orders?status=active&limit=50",orders())
            rule.setContent { click.cowa.linbit.ui.theme.LinbitTheme(label.endsWith("dark")){click.cowa.linbit.feature.trade.FuturesHistoryScreen(onClose={},onLogin={},vm=vm,onDialogWindow={window=it})} }
            await("条件委托");rule.onNodeWithText("条件委托",useUnmergedTree=true).performScrollTo().performClick();await("止盈止损");rule.onNodeWithText("止盈止损",useUnmergedTree=true).performClick();await("BTCUSDT")
            harness.app("history-$label-tpsl")
            rule.onNodeWithText("撤销",useUnmergedTree=true).performClick();await("撤销止盈止损？")
            rule.waitUntil(5_000){window!=null};harness.dialogWindow("history-$label-tpsl-review",window)
            rule.onNodeWithTag("history.algo.cancel.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()
        }
    }
    @Test fun actualProductionHistoryHostFailureRetryAndReadback(){
        MockEvidenceApi().use{server->api=server
            val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_LAYOUT_TOKEN"
            val market=click.cowa.linbit.data.MarketStore(f.retrofit,click.cowa.linbit.core.realtime.MarketSocket(OkHttpClient(),"ws://127.0.0.1/","fake",CoroutineScope(Dispatchers.Main),accessToken={null}),CoroutineScope(Dispatchers.Main))
            vm=click.cowa.linbit.feature.trade.TradeHistoryViewModel(f.retrofit,f.tokens,market)
            api.route("GET","/futures/plan-orders?status=active&limit=50",orders("STALE",7));api.route("GET","/futures/tpsl-orders?status=active&limit=50",orders())
            api.route("DELETE","/futures/tpsl-orders/9001","""{"code":"EVIDENCE_FAILURE","message":"失败复核保留，可重试"}""",503);api.route("DELETE","/futures/tpsl-orders/9001","""{"acknowledged":true}""");api.route("GET","/futures/tpsl-orders?status=active&limit=50",orders("AFTERREADBACK",9002))
            rule.setContent{click.cowa.linbit.ui.theme.LinbitTheme(label.endsWith("dark")){click.cowa.linbit.feature.trade.FuturesHistoryScreen(onClose={},onLogin={},vm=vm,onDialogWindow={window=it})}}
            await("条件委托");rule.onNodeWithText("条件委托",useUnmergedTree=true).performScrollTo().performClick();rule.onNodeWithText("止盈止损",useUnmergedTree=true).performClick();await("BTCUSDT")
            rule.onNodeWithText("撤销",useUnmergedTree=true).performClick();await("撤销止盈止损？");rule.onNodeWithTag("history.algo.cancel.confirm",useUnmergedTree=true).performClick();await("失败复核保留，可重试")
            Assert.assertFalse(vm.actionBusy);Assert.assertEquals(1,api.mutationRequests.count{it.method=="DELETE"})
            harness.dialogWindow("history-$label-tpsl-failure",window)
            rule.onNodeWithTag("history.algo.cancel.confirm",useUnmergedTree=true).assertIsEnabled().performClick();await("AFTERREADBACK")
            Assert.assertEquals(2,api.mutationRequests.count{it.method=="DELETE"});Assert.assertEquals(2,api.requests.count{it=="GET /futures/tpsl-orders?status=active&limit=50"});Assert.assertEquals("AFTERREADBACK",vm.futAlgos.single().symbol)
        }
    }
    @Test fun actualAppRootTpslIndependentReviewAndDismissZeroWrite(){render(false)}
    @Test fun actualAppRootTpslFailureRetryAndCurrentFilterReadback(){render(true)}
}
