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
class ProductionWithdrawCancel320EvidenceTest:ProductionWithdrawCancelScenario("withdraw-cancel-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionWithdrawCancel360EvidenceTest:ProductionWithdrawCancelScenario("withdraw-cancel-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

abstract class ProductionWithdrawCancelScenario(private val label:String,private val qualifier:String) {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
    private var window:android.view.Window?=null
    private lateinit var assetVm:click.cowa.linbit.feature.assets.AssetsViewModel
    private val pending="""{"id":"MOCK-WD-CANCEL","asset":"USDT","network":"TRC20","address":"INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER","amount":"10.00","fee":"0.10","status":"pending","createdAt":"2026-11-06T00:00:00Z"}"""
    private val canceled=pending.replace("pending","canceled")
    @Before fun setup(){Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    private fun await(t:String){try{rule.waitUntil(10_000){runCatching{rule.onAllNodesWithText(t,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}.getOrDefault(false)}}catch(e:Throwable){throw AssertionError("wait=$t requests=${api.dumpRequests()} route-state=$window roots="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=Int.MAX_VALUE),e)}}
    private fun initial(){repeat(24){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"100","withdrawAccount":"funding"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}
        val balanceJson="""{"items":[{"asset":"USDT","free":"100.00","locked":"0.00","total":"100.00"}],"page":1,"pageSize":200,"total":1}"""
        repeat(2){api.route("GET","/assets/balances?account=spot&pageSize=200",balanceJson);api.route("GET","/assets/balances?account=funding&pageSize=200",balanceJson);api.route("GET","/futures/account","{}")}
        api.route("GET","/wallet/withdraw/records?page=1&pageSize=20","""{"items":[$pending],"page":1,"pageSize":20,"total":1}""")
        api.route("POST","/wallet/withdraw/MOCK-WD-CANCEL/cancel","""{"code":"MOCK_CANCEL_503","message":"mock cancel 503, retry allowed"}""",503);api.route("POST","/wallet/withdraw/MOCK-WD-CANCEL/cancel",canceled)
        repeat(2){api.route("GET","/assets/overview","""{"totalValueUsdt":"100","withdrawAccount":"funding"}""");api.route("GET","/assets/balances?account=funding&pageSize=200","""{"items":[{"asset":"USDT","free":"100.00","locked":"0.00","total":"100.00"}],"page":1,"pageSize":200,"total":1}""");api.route("GET","/wallet/withdraw/records?page=1&pageSize=20","""{"items":[$canceled],"page":1,"pageSize":20,"total":1}""")}
    }
    private fun settleWindow(){rule.waitForIdle();rule.mainClock.advanceTimeBy(500);rule.waitForIdle();rule.waitUntil(5_000){window?.decorView?.width?:0>0&&window?.decorView?.height?:0>0}}
    private fun toFinal(action:()->Unit)=MockEvidenceApi().use{server->api=server;initial();val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_CANCEL_TOKEN";val home=f.rootHomeMarket();val root=f.root();assetVm=f.assets()
        runBlocking{root.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
        rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides home){click.cowa.linbit.app.AppRoot(root,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(assetVm,f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
        await("首页");rule.onNodeWithTag("main-tab-assets",useUnmergedTree=true).performClick();await("提现");rule.onNodeWithText("提现",useUnmergedTree=true).performClick();await("链上提币");rule.onNodeWithText("提现记录",useUnmergedTree=true).performClick();await("10.00 USDT · Tron (TRC20)");harness.app("approot-$label-records")
        rule.onNodeWithTag("withdraw.cancel.open",useUnmergedTree=true).performClick();await("确认撤销提现");settleWindow();harness.dialogWindow("approot-$label-review",window);api.assertNoWrites();action()
    }
    @Test fun actualAppRootCancelZeroWrite(){toFinal{rule.onNodeWithTag("withdraw.cancel.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()}}
    @Test fun actualAppRootRetryAndSameIdCanceledRecordBalanceReadback(){toFinal{api.withdrawCancelFailureGate=java.util.concurrent.CountDownLatch(1)
        rule.onNodeWithTag("withdraw.cancel.confirm",useUnmergedTree=true).performClick();rule.waitUntil(5_000){api.mutationRequests.size==1};rule.runOnIdle{Assert.assertTrue(assetVm.withdrawCancelBusy)};Assert.assertEquals(1,api.mutationRequests.size);api.withdrawCancelFailureGate?.countDown();await("mock cancel 503, retry allowed");settleWindow();harness.dialogWindow("approot-$label-retry",window)
        rule.onNodeWithTag("withdraw.cancel.confirm",useUnmergedTree=true).performClick();await("已从服务端记录读回撤销");await("实际记录状态：已撤销");rule.waitUntil(10_000){assetVm.withdrawCancelReceipt!=null&&runCatching{rule.onNodeWithTag("withdraw.cancel.pending.status",useUnmergedTree=true).assertDoesNotExist();true}.getOrDefault(false)};rule.onNode(hasTestTag("withdraw.cancel.readback.status") and hasAnyDescendant(hasText("已撤销"))).assertIsDisplayed();rule.onNodeWithTag("withdraw.records.scroll",useUnmergedTree=true).performScrollToNode(hasTestTag("withdraw.cancel.balance"));await("100.00");await("USDT");rule.onNodeWithTag("withdraw.cancel.balance",useUnmergedTree=true).assertIsDisplayed();rule.runOnIdle{Assert.assertEquals("100.00",assetVm.withdrawCancelReceipt?.balance?.toPlainString());Assert.assertEquals(click.cowa.linbit.core.generated.ApiWithdrawal.Status.CANCELED,assetVm.withdrawCancelReceipt?.readback?.status)};api.mutationRequests.forEach{Assert.assertEquals("POST",it.method);Assert.assertEquals("/wallet/withdraw/MOCK-WD-CANCEL/cancel",it.path);Assert.assertEquals(0L,it.body.size)};Assert.assertEquals(2,api.mutationRequests.size);Assert.assertFalse(api.requests.any{it.contains("[REJECTED")});window=null;harness.app("approot-$label-result")
    }}
}
