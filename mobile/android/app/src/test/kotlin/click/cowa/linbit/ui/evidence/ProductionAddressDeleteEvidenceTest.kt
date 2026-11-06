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
class ProductionAddressDelete320EvidenceTest:ProductionAddressDeleteScenario("address-delete-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionAddressDelete360EvidenceTest:ProductionAddressDeleteScenario("address-delete-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

abstract class ProductionAddressDeleteScenario(private val label:String,private val qualifier:String) {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
    private lateinit var assetVm:click.cowa.linbit.feature.assets.AssetsViewModel
    private var window:android.view.Window?=null
    private val old="""{"id":"MOCK-ADDRESS-DELETE","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-DELETE-LOCAL","label":"Mock delete label","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}"""
    private val deleted="""{"id":"MOCK-ADDRESS-OTHER","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-KEEP-LOCAL","label":"Mock keep label","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}"""
    @Before fun setup(){Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    private fun await(target:String){try{rule.waitUntil(10_000){rule.onAllNodesWithText(target,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}}catch(t:Throwable){throw AssertionError("wait label=$target requests=${api.dumpRequests()} nodes="+rule.onAllNodes(isRoot(),useUnmergedTree=true).printToString(maxDepth=15),t)}}
    private fun initial(){repeat(24){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"100","withdrawAccount":"funding"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}
        val balance="""{"items":[{"asset":"USDT","free":"100","locked":"0","total":"100"}],"page":1,"pageSize":200,"total":1}"""
        repeat(2){api.route("GET","/assets/balances?account=spot&pageSize=200",balance);api.route("GET","/assets/balances?account=funding&pageSize=200",balance);api.route("GET","/futures/account","{}")}
        api.route("GET","/account/withdraw-addresses","""{"items":[$old]}""");repeat(5){api.route("GET","/account/withdraw-whitelist","""{"enabled":false,"whitelisted":false}""")}
        api.route("DELETE","/account/withdraw-addresses/MOCK-ADDRESS-DELETE?totpCode=123456","""{"code":"MOCK_DELETE_503","message":"mock DELETE 503, retry allowed"}""",503)
        api.route("DELETE","/account/withdraw-addresses/MOCK-ADDRESS-DELETE?totpCode=123456","",204)
        api.route("GET","/account/withdraw-addresses","""{"items":[$old]}""")
        repeat(2){api.route("GET","/account/withdraw-addresses","""{"items":[$deleted]}""")}
    }
    private fun settleWindow(){rule.waitForIdle();rule.mainClock.advanceTimeBy(500);rule.waitForIdle();rule.waitUntil(5_000){window?.decorView?.width?:0>0&&window?.decorView?.height?:0>0}}
    private fun toFinal(action:()->Unit)=MockEvidenceApi().use{server->
        api=server;initial();val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_ADDRESS_DELETE_TOKEN";val root=f.root();val home=f.rootHomeMarket();assetVm=f.assets()
        runBlocking{root.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
        rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides home){click.cowa.linbit.app.AppRoot(root,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(assetVm,f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
        await("首页");rule.onNodeWithTag("main-tab-assets",useUnmergedTree=true).performClick();await("提现");rule.onNodeWithText("提现",useUnmergedTree=true).performClick();await("链上提币");rule.onNodeWithText("地址簿",useUnmergedTree=true).performClick();await("Mock delete label")
        rule.onNodeWithText("删除",substring=false,useUnmergedTree=true).performClick();await("删除提现地址");rule.onNodeWithTag("address.delete.totp",useUnmergedTree=true).performTextReplacement("123456");settleWindow()
        harness.app("approot-$label-form");harness.dialogWindow("approot-$label-review",window);api.assertNoWrites();action()
    }
    @Test fun actualAppRootCancelZeroDelete(){toFinal{rule.onNodeWithTag("address.delete.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()}}
    @Test fun actualAppRootRetryAndSameIdAbsentListReadback(){toFinal{
        api.addressDeleteFailureGate=java.util.concurrent.CountDownLatch(1)
        rule.onNodeWithTag("address.delete.confirm",useUnmergedTree=true).performClick();rule.waitUntil(5_000){api.mutationRequests.size==1}
        rule.runOnIdle{Assert.assertTrue(assetVm.addressDeleteBusy)}
        Assert.assertEquals(1,api.mutationRequests.size)
        api.addressDeleteFailureGate?.countDown();await("mock DELETE 503, retry allowed");settleWindow()
        rule.onNodeWithTag("address.delete.totp",useUnmergedTree=true).assertTextContains("123456");harness.dialogWindow("approot-$label-retry",window)
        rule.onNodeWithTag("address.delete.confirm",useUnmergedTree=true).assertIsEnabled().performClick()
        await("删除已受理，但服务端列表仍含该地址；只可重读列表，不能再次删除。")
        settleWindow();harness.dialogWindow("approot-$label-readback",window)
        Assert.assertEquals(2,api.mutationRequests.size)
        rule.onNodeWithTag("address.delete.refresh",useUnmergedTree=true).performClick()
        await("已从服务端地址列表确认删除：Mock delete label");await("Mock keep label")
        rule.onNodeWithTag("address.delete.readback",useUnmergedTree=true).assertIsDisplayed().assertTextEquals("已从服务端地址列表确认删除：Mock delete label")
        Assert.assertEquals(2,api.mutationRequests.size)
        synchronized(api.mutationRequests){api.mutationRequests.toList()}.forEach{
            Assert.assertEquals("DELETE",it.method);Assert.assertEquals("/account/withdraw-addresses/MOCK-ADDRESS-DELETE?totpCode=123456",it.path)
            Assert.assertEquals("",it.body.readUtf8())
        }
        Assert.assertTrue(api.requests.none{it.contains("[REJECTED")});Assert.assertTrue(api.requests.none{it.startsWith("POST /account/code/send")})
        window=null;harness.app("approot-$label-result")
    }}
}
