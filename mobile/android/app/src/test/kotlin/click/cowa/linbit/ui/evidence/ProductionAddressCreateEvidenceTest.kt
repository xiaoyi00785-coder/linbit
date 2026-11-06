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
class ProductionAddress320EvidenceTest:ProductionAddressScenario("address-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionAddress360EvidenceTest:ProductionAddressScenario("address-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

abstract class ProductionAddressScenario(private val label:String,private val qualifier:String) {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
    private var window:android.view.Window?=null
    private lateinit var rootVm:click.cowa.linbit.app.AppViewModel
    private lateinit var assetVm:click.cowa.linbit.feature.assets.AssetsViewModel
    private val created="""{"id":"MOCK-ADDRESS-NEW","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-BOOK-LOCAL","label":"Mock local label","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}"""
    private val body="{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"INVALID-ADDRESS-BOOK-LOCAL\",\"label\":\"Mock local label\",\"whitelisted\":false,\"totpCode\":\"123456\"}"
    @Before fun setup(){Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    private fun await(t:String){try{rule.waitUntil(12_000){runCatching{rule.onAllNodesWithText(t,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}.getOrDefault(false)}}catch(e:Throwable){throw AssertionError("wait=$t requests=${api.dumpRequests()} roots="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=3),e)}}
    private fun initial(){repeat(24){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"100","withdrawAccount":"funding"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}
        val balance="""{"items":[{"asset":"USDT","free":"100","locked":"0","total":"100"}],"page":1,"pageSize":200,"total":1}"""
        repeat(2){api.route("GET","/assets/balances?account=spot&pageSize=200",balance);api.route("GET","/assets/balances?account=funding&pageSize=200",balance);api.route("GET","/futures/account","{}")}
        listRoutes(false);api.route("GET","/account/withdraw-whitelist","""{"whitelisted":false}""");api.route("GET","/wallet/networks?asset=USDT","""{"networks":[{"asset":"USDT","network":"TRC20","withdrawEnabled":true,"recommended":true,"estimatedArrivalMinutes":3,"minWithdraw":"1","maxWithdraw":"200","withdrawFee":"0.1","withdrawFeeAsset":"USDT","memoRequired":false}]}""")
        api.route("POST","/account/withdraw-addresses","""{"code":"MOCK_ADDRESS_503","message":"mock address 503, retry allowed"}""",503);api.route("POST","/account/withdraw-addresses",created,201);listRoutes(true);listRoutes(true);api.route("GET","/account/withdraw-whitelist","""{"whitelisted":false}""");api.route("GET","/account/withdraw-whitelist","""{"whitelisted":false}""")
    }
    private fun listRoutes(withCreated:Boolean)=api.route("GET","/account/withdraw-addresses","""{"items":[${if(withCreated)created else ""}]}""")
    private fun toReview(action:()->Unit)=MockEvidenceApi().use{server->api=server;initial();val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_ADDRESS_TOKEN";val root=f.root().also{rootVm=it};val home=f.rootHomeMarket();assetVm=f.assets()
        runBlocking{root.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
        rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides home){click.cowa.linbit.app.AppRoot(root,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(assetVm,f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
        await("首页");rule.onNodeWithTag("main-tab-assets",useUnmergedTree=true).performClick();await("提现");rule.onNodeWithText("提现",useUnmergedTree=true).performClick();await("链上提币");rule.onNodeWithText("地址簿",useUnmergedTree=true).performClick();await("提现白名单");rule.onAllNodesWithText("添加地址",useUnmergedTree=true)[0].performClick();await("新增地址")
        rule.onNodeWithTag("address.input").performTextReplacement("INVALID-ADDRESS-BOOK-LOCAL");rule.onNodeWithTag("address.label").performTextReplacement("Mock local label");rule.onNodeWithText("加入提现白名单",useUnmergedTree=true).performClick()
        rule.waitUntil(5_000){runCatching{rule.onNodeWithTag("address.review").assertIsEnabled();true}.getOrDefault(false)};api.assertNoWrites();harness.app("approot-$label-form");rule.onNodeWithTag("address.review").performClick();await("确认新增提现地址");rule.waitForIdle();rule.mainClock.advanceTimeBy(1_000);rule.waitForIdle();rule.waitUntil(5_000){window?.decorView?.width?:0>0&&window?.decorView?.height?:0>0};harness.dialogWindow("approot-$label-review",window)
        rule.onNodeWithTag("address.totp",useUnmergedTree=true).performTextReplacement("123456");rule.onNodeWithTag("address.confirm",useUnmergedTree=true).assertIsEnabled();Assert.assertTrue("AddAddress must remain routed",root.assetsNav.route.value is click.cowa.linbit.data.assets.AssetsRoute.AddAddress);action()
    }
    @Test fun actualAppRootCancelZeroWrite(){toReview{rule.onNodeWithTag("address.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()}}
    @Test fun actualAppRootFailureRetryThenRealListReadback(){toReview{api.addressCreateFailureGate=java.util.concurrent.CountDownLatch(1)
        rule.onNodeWithTag("address.confirm",useUnmergedTree=true).performClick();try{rule.waitUntil(5_000){api.mutationRequests.size==1}}catch(e:Throwable){throw AssertionError("requests=${api.dumpRequests()} busy=${assetVm.addressBusy} error=${assetVm.addressError} receipt=${assetVm.addressReceipt} roots="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=3),e)};rule.runOnIdle{Assert.assertTrue(assetVm.addressBusy)};Assert.assertEquals(1,api.mutationRequests.size);api.addressCreateFailureGate?.countDown();await("mock address 503, retry allowed");rule.waitForIdle();rule.waitForIdle();rule.waitUntil(5_000){window?.decorView?.width?:0>0&&window?.decorView?.height?:0>0};harness.dialogWindow("approot-$label-retry",window)
        rule.onNodeWithTag("address.confirm",useUnmergedTree=true).performClick();Thread.sleep(300);rule.waitForIdle();try{rule.waitUntil(5_000){rootVm.assetsNav.route.value is click.cowa.linbit.data.assets.AssetsRoute.AddressBook}}catch(e:Throwable){throw AssertionError("route=${rootVm.assetsNav.route.value} receipt=${assetVm.addressReceipt} error=${assetVm.addressError} busy=${assetVm.addressBusy} requests=${api.dumpRequests()}",e)};await("Mock local label");await("INVALID-ADDRESS-BOOK-LOCAL");rule.waitForIdle();Assert.assertTrue("No rejected GET/list allowed: ${api.dumpRequests()}",!api.requests.any{it.contains("[REJECTED")});Assert.assertTrue(synchronized(api.requests){api.requests.toList()}.count{it=="GET /account/withdraw-addresses"}>=3);Assert.assertEquals(2,api.mutationRequests.size);api.mutationRequests.forEach{Assert.assertEquals("/account/withdraw-addresses",it.path);Assert.assertEquals(body,it.body.readUtf8())};Assert.assertFalse(api.requests.any{it.startsWith("POST /account/code/send")});window=null;harness.app("approot-$label-result")
    }}
}
