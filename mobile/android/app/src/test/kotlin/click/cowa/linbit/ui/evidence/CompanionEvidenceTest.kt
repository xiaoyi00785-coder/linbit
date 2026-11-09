package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.feature.home.*
import click.cowa.linbit.feature.more.*
import click.cowa.linbit.feature.profile.UsdRateInfo
import click.cowa.linbit.feature.trade.*
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.mockito.kotlin.*
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.*
import java.io.File
import java.math.BigDecimal

/** Local in-memory/loopback fixtures only. No real account, order, message or ticket write. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class CompanionEvidenceTest {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    @Before fun start() { Dispatchers.setMain(UnconfinedTestDispatcher());rule.activity.actionBar?.hide() }
    @After fun stop() { Dispatchers.resetMain() }
    private fun capture(name: String,dialogs: Boolean=false) {
        rule.waitForIdle();System.getProperty("production.evidence.dir")?.let { path -> rule.runOnIdle {
            val v=rule.activity.window.decorView;val b=Bitmap.createBitmap(v.width,v.height,Bitmap.Config.ARGB_8888);val c=Canvas(b);v.draw(c)
            if(dialogs) org.robolectric.shadows.ShadowDialog.getShownDialogs().filter { it.isShowing }.forEach { d ->
                val view=d.window!!.decorView;c.drawColor(android.graphics.Color.argb(128,0,0,0));c.save();c.translate((b.width-view.width)/2f,(b.height-view.height).toFloat());view.draw(c);c.restore()
            }
            File(path).mkdirs();File(path,"mock-b8-$name.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG,100,it) };b.recycle()
        } }
    }
    @Test fun welfareShowsSeparateRealFieldValuesAndMissingNotZero() {
        val tokens=InMemoryTokenStore().apply { accessToken="INVALID_LOCAL_ONLY" };val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main)
        val vm=WelfareViewModel(mock(),tokens,ToastCenter(scope))
        vm.seedEvidenceWelfare(ApiHomeNewcomer(available=false),ApiFuturesBonus200Response(available=ApiFuturesBonusAvailable(trial="12.34567890",deduction=null)))
        try {
            rule.setContent { LinbitTheme(false) { WelfareScreen({}, {}, {}, {_,_->}, {}, {},vm) } }
            rule.onNodeWithText("12.3456789 USDT").assertExists();rule.onNodeWithText("-- USDT").assertExists();capture("welfare-bonus")
        } finally { vm.viewModelScope.cancel();scope.cancel() }
    }
    @Test fun homeAnnouncementListOpensExactIdAndNoFlashFabrication() {
        val item=ApiAnnouncement(id=12,title="本地测试公告：平台服务升级说明",publishAt="2026-01-01T12:00:00Z",body="本地测试正文，仅用于验证公告详情。\n\n没有真实发布公告或发送通知。")
        val items=listOf(item,item.copy(id=13,title="本地测试公告：交易规则说明"),item.copy(id=14,title="本地测试公告：系统维护安排"))
        var selected by mutableStateOf<Long?>(null)
        rule.setContent { LinbitTheme(false) { Column(Modifier.fillMaxSize().background(LB.p.bg)) {
            if(selected==null) NewsSection(emptyList(),{},items,onAnnouncement={selected=it}) else AnnouncementDetailContent(AnnouncementState(item,false),{selected=null},{})
        } } }
        rule.onNodeWithText(item.title!!).assertIsDisplayed();capture("announcements-list")
        rule.onNodeWithText(item.title!!).performClick();rule.waitForIdle();assertEquals(12L,selected)
        rule.onNodeWithText(item.body!!).assertIsDisplayed();capture("announcement-detail")
    }
    @Test fun usdInfoShowsActualSourceTimeAndUnavailableNeverPeg() {
        val rate=ApiUsdRate(available=true,rate="0.99876543",source="example.invalid",updatedAt="2026-01-01T12:00:00Z",stale=true,precision=8,rounding="half_away_from_zero",unit="USD per 1 USDT",pegAssumed=false)
        var value by mutableStateOf<ApiUsdRate?>(rate)
        rule.setContent { LinbitTheme(false) { Column(Modifier.fillMaxSize().background(LB.p.bg)) { UsdRateInfo(value) } } }
        rule.onNodeWithText("1 USDT = 0.99876543 USD").assertIsDisplayed();rule.onNodeWithText("来源：example.invalid").assertIsDisplayed();capture("usd-rate-info")
        rule.runOnIdle { value=rate.copy(available=false,rate=null,source=null,updatedAt=null) }
        rule.onNodeWithText("1 USDT = -- USD").assertIsDisplayed();rule.onNodeWithText("更新时间：--").assertIsDisplayed();capture("usd-unavailable")
    }
    @Test fun twelvePeriodMenuUsesRequestedPeriodAndUnsupportedNeverShowsOldCandles() {
        val calls=mutableListOf<String>()
        val bars=(0..30).map { i -> Candle(BigDecimal("100"),BigDecimal("102"),BigDecimal("99"),BigDecimal("101"),timeMs=1700000000000L+i*900000) }
        rule.setContent { LinbitTheme(false) { Column(Modifier.fillMaxSize().background(LB.p.bg)) {
            TradeChartPanel("BTCUSDT",2,false,{ period -> calls+=period;if(period=="3m") error("本地模拟：服务端尚不支持 3m") else bars })
        } } }
        rule.waitForIdle();assertEquals(listOf("15m"),calls)
        rule.onNodeWithTag("chart.intervals.more").performClick()
        listOf("3m","2h","6h","12h").forEach { rule.onNodeWithText(it).assertExists() };rule.onNodeWithText("1M").assertDoesNotExist();capture("kline-periods",true)
        rule.onNodeWithText("3m").performClick();rule.onNodeWithText("本地模拟：服务端尚不支持 3m").assertIsDisplayed();capture("kline-unsupported")
        rule.onNodeWithText("重试").performClick();rule.waitForIdle();assertEquals(2,calls.count { it=="3m" })
        rule.onNodeWithTag("chart.intervals.more").performClick();rule.onNodeWithText("2h").performClick();rule.waitForIdle();assertEquals("2h",calls.last());rule.onNodeWithText("本地模拟：服务端尚不支持 3m").assertDoesNotExist()
    }
    @Test fun publicAnnouncementApiReadsListDetailAndPreserves404AsFailure()=runBlocking {
        MockEvidenceApi().use { server ->
            server.route("GET","/announcements","""{"items":[{"id":12,"title":"LOCAL"}]}""")
            server.route("GET","/announcements/12","""{"id":12,"title":"LOCAL","body":"LOCAL BODY"}""")
            server.route("GET","/announcements/13","""{"code":"NOT_FOUND","message":"LOCAL 404"}""",404)
            val api=NetworkFactory.api(okhttp3.OkHttpClient(),server.baseUrl)
            assertEquals(12L,api.announcements().items?.single()?.id);assertEquals("LOCAL BODY",api.announcement(12).body)
            assertTrue(runCatching { apiCall { api.announcement(13) } }.isFailure)
            server.assertNoWrites();assertEquals(3,server.requests.size)
        }
    }
    @Test fun welfareReloadsAfterTokenVersionChangesWithoutLeavingPage() {
        val tokens=InMemoryTokenStore().apply { accessToken="INVALID_LOCAL_FIRST" }
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main);var reads=0
        val api=mock<LinbitApi> {
            onBlocking { homeNewcomer() } doReturn ApiHomeNewcomer(available=false)
            onBlocking { futuresBonus() } doSuspendableAnswer { reads++;ApiFuturesBonus200Response(available=ApiFuturesBonusAvailable(trial=reads.toString(),deduction="0")) }
        }
        val vm=WelfareViewModel(api,tokens,ToastCenter(scope))
        try {
            rule.setContent { LinbitTheme(false) { WelfareScreen({}, {}, {}, {_,_->}, {}, {},vm) } }
            rule.onNodeWithText("1 USDT").assertExists()
            rule.runOnIdle { tokens.accessToken="INVALID_LOCAL_ROTATED" }
            rule.onNodeWithText("2 USDT").assertExists();assertEquals(2,reads)
        } finally { vm.viewModelScope.cancel();scope.cancel() }
    }

}
