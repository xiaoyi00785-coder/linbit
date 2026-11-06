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
class ProductionAddressEdit320EvidenceTest:ProductionAddressEditScenario("address-edit-320-font130","w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionAddressEdit360EvidenceTest:ProductionAddressEditScenario("address-edit-360-dark","w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

abstract class ProductionAddressEditScenario(private val label:String,private val qualifier:String) {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy{ProductionEvidenceHarness(rule)}
    private lateinit var api:MockEvidenceApi
    private var window:android.view.Window?=null
    private lateinit var assetVm:click.cowa.linbit.feature.assets.AssetsViewModel
    private val old="""{"id":"MOCK-ADDRESS-EDIT","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-BOOK-LOCAL","label":"Mock original label","memo":"OLD-MEMO","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}"""
    private val edited="""{"id":"MOCK-ADDRESS-EDIT","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-BOOK-LOCAL","label":"Mock edited label","memo":"MEMO-LOCAL","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}"""
    private val patch="{\"label\":\"Mock edited label\",\"memo\":\"MEMO-LOCAL\",\"totpCode\":\"123456\"}"
    @Before fun setup(){Assume.assumeTrue(System.getProperty("production.evidence")=="1");System.setProperty("production.evidence.qualifier",qualifier)}
    private fun await(t:String){try{rule.waitUntil(10_000){runCatching{rule.onAllNodesWithText(t,substring=true,useUnmergedTree=true).fetchSemanticsNodes().isNotEmpty()}.getOrDefault(false)}}catch(e:Throwable){throw AssertionError("wait=$t requests=${api.dumpRequests()} route-state=$window",e)}}
    private fun initial(){repeat(24){api.route("GET","/market/symbols","[]");api.route("GET","/market/ticker","[]");api.route("GET","/futures/market/ticker","""{"tickers":[]}""");api.route("GET","/market/categories","[]");api.route("GET","/market/fiat-rates?base=USDT","""{"base":"USDT","rates":{}}""");api.route("GET","/assets/overview","""{"totalValueUsdt":"100","withdrawAccount":"funding"}""");api.route("GET","/home/newcomer","{}");api.route("GET","/notifications/unread-count","""{"unread":0}""");earnRoutes(api,false)}
        val balance="""{"items":[{"asset":"USDT","free":"100","locked":"0","total":"100"}],"page":1,"pageSize":200,"total":1}"""
        repeat(2){api.route("GET","/assets/balances?account=spot&pageSize=200",balance);api.route("GET","/assets/balances?account=funding&pageSize=200",balance);api.route("GET","/futures/account","{}")}
        api.route("GET","/account/withdraw-addresses","""{"items":[$old]}""");repeat(4){api.route("GET","/account/withdraw-whitelist","""{"whitelisted":false}""")};api.route("GET","/wallet/networks?asset=USDT","""{"networks":[{"asset":"USDT","network":"TRC20","recommended":true,"memoRequired":false}]}""")
        api.route("PATCH","/account/withdraw-addresses/MOCK-ADDRESS-EDIT","""{"code":"MOCK_PATCH_503","message":"mock PATCH 503, retry allowed"}""",503);api.route("PATCH","/account/withdraw-addresses/MOCK-ADDRESS-EDIT",edited);repeat(2){api.route("GET","/account/withdraw-addresses","""{"items":[$edited]}""")}
    }
    private fun settleWindow(){rule.waitForIdle();rule.mainClock.advanceTimeBy(500);rule.waitForIdle();rule.waitUntil(5_000){window?.decorView?.width?:0>0&&window?.decorView?.height?:0>0}}
    private fun toFinal(action:()->Unit)=MockEvidenceApi().use{server->api=server;initial();val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));f.tokens.accessToken="INVALID_ADDRESS_EDIT_TOKEN";val root=f.root();val home=f.rootHomeMarket();assetVm=f.assets()
        runBlocking{root.setTheme(if(label.endsWith("dark"))click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT)}
        rule.setContent{androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides home){click.cowa.linbit.app.AppRoot(root,f.badge(),click.cowa.linbit.app.AssetRouteViewModels(assetVm,f.spot(),f.tradFi()),showSplash=false,onDialogWindow={window=it})}}
        await("首页");rule.onNodeWithTag("main-tab-assets",useUnmergedTree=true).performClick();await("提现");rule.onNodeWithText("提现",useUnmergedTree=true).performClick();await("链上提币");rule.onNodeWithText("地址簿",useUnmergedTree=true).performClick();await("Mock original label");rule.onNodeWithText("编辑",useUnmergedTree=true).performClick();await("编辑提现地址");settleWindow();harness.dialogWindow("approot-$label-form",window)
        rule.onNodeWithTag("address.edit.label",useUnmergedTree=true).performTextReplacement("Mock edited label");rule.onNodeWithTag("address.edit.memo",useUnmergedTree=true).performTextReplacement("MEMO-LOCAL");rule.onNodeWithTag("address.edit.review",useUnmergedTree=true).performClick();await("确认修改提现地址");settleWindow();harness.dialogWindow("approot-$label-review",window)
        rule.onNodeWithTag("address.edit.totp",useUnmergedTree=true).performTextReplacement("123456");api.assertNoWrites();action()
    }
    @Test fun actualAppRootCancelZeroWrite(){toFinal{rule.onNodeWithTag("address.edit.dismiss",useUnmergedTree=true).performClick();api.assertNoWrites()}}
    @Test fun actualAppRootRetryAndSameIdValuesListReadback(){toFinal{api.addressEditFailureGate=java.util.concurrent.CountDownLatch(1)
        rule.onNodeWithTag("address.edit.confirm",useUnmergedTree=true).performClick();rule.waitUntil(5_000){api.mutationRequests.size==1};rule.runOnIdle{Assert.assertTrue(assetVm.addressEditBusy)};Assert.assertEquals(1,api.mutationRequests.size);api.addressEditFailureGate?.countDown();await("mock PATCH 503, retry allowed");settleWindow();harness.dialogWindow("approot-$label-retry",window)
        rule.onNodeWithTag("address.edit.confirm",useUnmergedTree=true).performClick();Thread.sleep(300);rule.waitForIdle();await("已从服务端列表读回修改");await("Mock edited label");await("MEMO-LOCAL");api.mutationRequests.forEach{Assert.assertEquals("PATCH",it.method);Assert.assertEquals("/account/withdraw-addresses/MOCK-ADDRESS-EDIT",it.path);Assert.assertEquals(patch,it.body.readUtf8())};Assert.assertEquals(2,api.mutationRequests.size);Assert.assertFalse(api.requests.any{it.contains("[REJECTED")});window=null;harness.app("approot-$label-result")
    }}
}
