package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.runtime.*
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.lifecycle.*
import androidx.lifecycle.compose.LocalLifecycleOwner
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.feature.more.*
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.mockito.kotlin.*
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.*
import java.io.ByteArrayOutputStream
import java.io.File

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class SupportLifecycleEvidenceTest {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private class Owner: LifecycleOwner {
        val registry=LifecycleRegistry.createUnsafe(this)
        override val lifecycle: Lifecycle get()=registry
    }
    @Test fun foregroundRefreshPreservesDraftAttachmentAndScrollAndStopsOnPauseOrDispose() {
        val dispatcher=UnconfinedTestDispatcher();Dispatchers.setMain(dispatcher)
        val scope=CoroutineScope(SupervisorJob()+dispatcher)
        val tokens=InMemoryTokenStore().apply { accessToken="INVALID_IN_MEMORY_ONLY" }
        val messages=(1..25).map { i -> ApiSupportTicketMessage(i.toString(),ApiSupportTicketMessage.Author.SUPPORT,"本地测试历史 $i：仅用于验证详情阅读和滚动，不是真实客服消息。",emptyList(),"2026-01-01T00:00:00Z","客服（本地夹具）") }
        var response=ApiSupportTicket("LOCAL-TICKET",ApiSupportTicketCategory.OTHER,"本地测试工单（非真实账号）",ApiSupportTicketStatus.REPLIED,
            ApiSupportTicket.LastReplyBy.SUPPORT,"2026-01-01T00:00:00Z",false,messages.size,"2026-01-01T00:00:00Z","2026-01-01T00:00:00Z",messages=messages)
        var reads=0
        val api=mock<LinbitApi> { onBlocking { supportTicket(any()) } doSuspendableAnswer { reads++;response } }
        val vm=SupportViewModel(api,tokens,ToastCenter(scope)).apply { detailRefreshMillis=1000 }
        val draft=SupportDetailDraft()
        val png=ByteArrayOutputStream().also { stream -> Bitmap.createBitmap(2,2,Bitmap.Config.ARGB_8888).apply { eraseColor(android.graphics.Color.rgb(117,146,168));compress(Bitmap.CompressFormat.PNG,100,stream);recycle() } }.toByteArray()
        val attachment=SupportImage(png,"image/png","local-fixture.png")
        draft.images.value=listOf(attachment)
        val owner=Owner().apply { registry.currentState=Lifecycle.State.STARTED }
        var visible by mutableStateOf(true)
        try {
            rule.activity.actionBar?.hide()
            rule.setContent { CompositionLocalProvider(LocalLifecycleOwner provides owner) { LinbitTheme(false) {
                if(visible) SupportTicketDetail("LOCAL-TICKET",vm,{},draft)
            } } }
            rule.waitForIdle();assertEquals(0,reads)
            rule.runOnIdle { owner.registry.currentState=Lifecycle.State.RESUMED }
            rule.onNodeWithText("本地测试工单（非真实账号）").assertIsDisplayed()
            rule.onNode(hasSetTextAction()).performTextReplacement("本地未发送草稿，刷新不能清空")
            rule.runOnIdle { runBlocking { draft.scroll.scrollTo(500) } }
            val scroll=draft.scroll.value;assertTrue(scroll>0)
            val first=vm.state.value
            dispatcher.scheduler.advanceTimeBy(1001);dispatcher.scheduler.runCurrent();rule.waitForIdle()
            assertSame(first,vm.state.value);assertEquals(scroll,draft.scroll.value)
            response=response.copy(messages=messages+messages.last().copy(id="NEW-LOCAL",body="客服新回复（纯内存夹具）"),messageCount=26)
            dispatcher.scheduler.advanceTimeBy(1001);dispatcher.scheduler.runCurrent();rule.waitForIdle()
            assertEquals(26,vm.state.value.detail!!.messages!!.size)
            assertEquals("本地未发送草稿，刷新不能清空",draft.text.value);assertEquals(listOf(attachment),draft.images.value);assertEquals(scroll,draft.scroll.value)
            System.getProperty("production.evidence.dir")?.let { path -> rule.runOnIdle {
                val view=rule.activity.window.decorView
                val b=Bitmap.createBitmap(view.width,view.height,Bitmap.Config.ARGB_8888);view.draw(Canvas(b))
                File(path).mkdirs();File(path,"mock-support-detail-refresh.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG,100,it) };b.recycle()
            } }
            val beforeRotation=reads
            rule.runOnIdle { tokens.accessToken="INVALID_ROTATED_IN_MEMORY_ONLY" };rule.waitForIdle()
            assertEquals(beforeRotation+1,reads)
            assertEquals("本地未发送草稿，刷新不能清空",draft.text.value)
            assertEquals(listOf(attachment),draft.images.value);assertEquals(scroll,draft.scroll.value)
            rule.runOnIdle { owner.registry.currentState=Lifecycle.State.STARTED }
            val paused=reads;dispatcher.scheduler.advanceTimeBy(60000);dispatcher.scheduler.runCurrent();assertEquals(paused,reads)
            rule.runOnIdle { owner.registry.currentState=Lifecycle.State.RESUMED };assertEquals(paused+1,reads)
            rule.runOnIdle { visible=false };rule.waitForIdle()
            val disposed=reads;dispatcher.scheduler.advanceTimeBy(60000);dispatcher.scheduler.runCurrent();assertEquals(disposed,reads)
            runBlocking { verify(api,never()).replySupportTicket(any(),any());verify(api,never()).createSupportTicket(any());verify(api,never()).closeSupportTicket(any()) }
        } finally { vm.stopDetail();vm.viewModelScope.cancel();scope.cancel();Dispatchers.resetMain() }
    }
}
