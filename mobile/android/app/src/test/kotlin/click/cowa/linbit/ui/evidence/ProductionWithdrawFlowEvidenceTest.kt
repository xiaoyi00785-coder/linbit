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
class ProductionWithdraw320EvidenceTest:ProductionWithdrawScenario("withdraw-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionWithdraw360EvidenceTest:ProductionWithdrawScenario("withdraw-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

abstract class ProductionWithdrawScenario(private val label:String,private val qualifier:String) {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
    private var window:android.view.Window?=null
    private lateinit var assetVm:click.cowa.linbit.feature.assets.AssetsViewModel
    private val body="""{"id":"MOCK-WD-PENDING","asset":"USDT","network":"TRC20","address":"INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER","amount":"10.00","fee":"0.10","status":"pending","createdAt":"2026-11-06T00:00:00Z"}"""
    @Before fun setup(){Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    private fun await(t:String){try{rule.waitUntil(10_000){runCatching{rule.onAllNodesWithText(t,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}.getOrDefault(false)}}catch(e:Throwable){throw AssertionError("wait=$t requests=${api.dumpRequests()} roots="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=Int.MAX_VALUE),e)}}
    private fun balance(free:String)="""{"items":[{"asset":"USDT","free":"$free","locked":"0.00","total":"$free"}],"page":1,"pageSize":200,"total":1}"""
    private fun initialReads(){repeat(3){api.route("GET","/assets/balances?account=funding&pageSize=200",balance("100.00"));api.route("GET","/assets/balances?account=spot&pageSize=200",balance("100.00"));api.route("GET","/futures/account","""{"availableBalance":"20","marginBalance":"20"}""")}
        repeat(24){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"100","withdrawAccount":"funding"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}
        api.route("GET","/wallet/withdraw/networks?asset=USDT","""{"asset":"USDT","networks":[{"network":"TRC20","withdrawEnabled":true,"memoRequired":false,"minWithdraw":"1","maxWithdraw":"200","gasFeeEstimate":"0.1","gasFeeAsset":"USDT","estimatedArrivalMinutes":3}]}""")
        api.route("GET","/wallet/networks?asset=USDT","""{"networks":[{"asset":"USDT","network":"TRC20","withdrawEnabled":true,"recommended":true,"estimatedArrivalMinutes":3,"minWithdraw":"1","maxWithdraw":"200","withdrawFee":"0.1","withdrawFeeAsset":"USDT","memoRequired":false}]}""")
        api.route("GET","/account/security","""{"twoFactor":{"enabled":true,"method":"totp"}}""")
    }
    private fun setupFlow(action:()->Unit)=MockEvidenceApi().use{server->api=server;initialReads()
        api.route("POST","/wallet/withdraw","""{"code":"MOCK_WITHDRAW_503","message":"mock withdrawal 503, retry allowed"}""",503)
        api.route("POST","/wallet/withdraw",body,202)
        api.route("GET","/assets/balances?account=funding&pageSize=200",balance("90.00"))
        api.route("GET","/wallet/withdraw/records?page=1&pageSize=20","""{"items":[$body],"page":1,"pageSize":20,"total":1}""")
        val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_WITHDRAW_TOKEN";val root=f.root();val home=f.rootHomeMarket();assetVm=f.assets()
        runBlocking{root.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
        rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides home){click.cowa.linbit.app.AppRoot(root,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(assetVm,f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
        await("首页");rule.onNodeWithTag("main-tab-assets",useUnmergedTree=true).performClick();await("提现");rule.onNodeWithText("提现",useUnmergedTree=true).performClick();await("链上提币");rule.waitForIdle();rule.onNodeWithText("链上提币").performClick();await("选择币种");rule.onAllNodesWithText("USDT",useUnmergedTree=true)[3].performClick();await("提现地址")
        rule.onNodeWithTag("withdraw.address").performTextReplacement("INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER");rule.onNodeWithTag("withdraw.amount").performTextReplacement("10.00")
        try{rule.waitUntil(5_000){runCatching{rule.onNodeWithTag("withdraw.review").assertIsEnabled();true}.getOrDefault(false)}}catch(e:Throwable){throw AssertionError("review disabled requests=${api.dumpRequests()} roots="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=4),e)};api.assertNoWrites();harness.app("approot-$label-form")
        rule.onNodeWithTag("withdraw.review").performClick();await("安全验证");rule.waitUntil(5_000){window!=null};harness.dialogWindow("approot-$label-review",window)
        rule.onNodeWithTag("withdraw.totp",useUnmergedTree=true).performTextReplacement("123456");action()
    }
    @Test fun actualAppRootCancelZeroWrite(){setupFlow{rule.onNodeWithTag("withdraw.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()}}
    @Test fun actualAppRootRetryPendingAndActualReadback(){setupFlow{api.withdrawSubmissionFailureGate=java.util.concurrent.CountDownLatch(1)
        rule.onNodeWithTag("withdraw.confirm",useUnmergedTree=true).performClick();rule.waitUntil(5_000){api.mutationRequests.size==1};rule.runOnIdle { Assert.assertTrue("Held POST must publish ViewModel busy",assetVm.withdrawBusy) };Assert.assertEquals("After held click: ${api.dumpRequests()}",1,api.mutationRequests.size);api.withdrawSubmissionFailureGate?.countDown()
        await("mock withdrawal 503, retry allowed");rule.waitUntil(5_000){window!=null};harness.dialogWindow("approot-$label-retry",window);Assert.assertEquals(1,api.mutationRequests.size)
        rule.onNodeWithTag("withdraw.confirm",useUnmergedTree=true).performClick();await("提现记录读回状态：pending");rule.onNode(hasScrollAction() and hasAnyDescendant(hasTestTag("withdraw.address")) and androidx.compose.ui.test.SemanticsMatcher.keyIsDefined(androidx.compose.ui.semantics.SemanticsProperties.VerticalScrollAxisRange)).performScrollToNode(hasTestTag("withdraw.receipt"));rule.onNodeWithTag("withdraw.receipt",useUnmergedTree=true).assertExists();rule.onNodeWithText("读回资金可用：90.00 USDT",substring=false,useUnmergedTree=true).assertExists();Assert.assertEquals("90.00",assetVm.withdrawReceipt!!.balance.toPlainString());await("服务端提交状态：pending");Assert.assertEquals(2,api.mutationRequests.size)
        api.mutationRequests.forEach{Assert.assertEquals("/wallet/withdraw",it.path);Assert.assertEquals("{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER\",\"amount\":\"10.00\",\"totpCode\":\"123456\"}",it.body.readUtf8())}
        Assert.assertFalse(api.requests.any{it.startsWith("POST /account/code/send")});window=null;harness.app("approot-$label-result")
    }}
}
