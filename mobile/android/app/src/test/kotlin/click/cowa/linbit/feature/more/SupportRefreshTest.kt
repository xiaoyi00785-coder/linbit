package click.cowa.linbit.feature.more

import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.ui.components.ToastCenter
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.Assert.*
import org.mockito.kotlin.*

/** Pure in-process APIs only, including the mutation race test. Never a real support message. */
@OptIn(ExperimentalCoroutinesApi::class)
class SupportRefreshTest {
    private val dispatcher=StandardTestDispatcher()
    private val vms=mutableListOf<SupportViewModel>()
    private lateinit var scope: CoroutineScope
    private lateinit var tokens: InMemoryTokenStore
    private fun ticket(id: String="A",subject: String="本地测试")=ApiSupportTicket(id,ApiSupportTicketCategory.OTHER,subject,
        ApiSupportTicketStatus.REPLIED,ApiSupportTicket.LastReplyBy.SUPPORT,"2026-01-01T00:00:00Z",false,0,"2026-01-01T00:00:00Z","2026-01-01T00:00:00Z")
    @Before fun start() { Dispatchers.setMain(dispatcher);scope=CoroutineScope(SupervisorJob()+dispatcher);tokens=InMemoryTokenStore().apply { accessToken="INVALID_LOCAL_ONLY" } }
    @After fun stop() { vms.forEach { it.stopDetail(); it.viewModelScope.cancel() };scope.cancel();Dispatchers.resetMain() }
    private fun vm(api: LinbitApi)=SupportViewModel(api,tokens,ToastCenter(scope)).also { vms+=it }

    private fun runRefreshTest(block: suspend TestScope.() -> Unit) = runTest(dispatcher) {
        try { block() } finally { vms.forEach { it.stopDetail(); it.viewModelScope.cancel() } }
    }

    @Test fun deduplicatesRequestsAndStopsTimerWhileHidden()=runRefreshTest {
        val gate=CompletableDeferred<ApiSupportTicket>();var reads=0
        val api=mock<LinbitApi> { onBlocking { supportTicket(any()) } doSuspendableAnswer { reads++;gate.await() } }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent()
        vm.startDetail("A");vm.loadDetail("A");vm.loadDetail("A");runCurrent();assertEquals(1,reads)
        gate.complete(ticket());runCurrent();val unchanged=vm.state.value
        vm.loadDetail("A");runCurrent();assertSame(unchanged,vm.state.value)
        advanceTimeBy(10000);runCurrent();assertEquals(3,reads)
        vm.stopDetail("A");advanceTimeBy(60000);runCurrent();assertEquals(3,reads)
    }
    @Test fun cancellingInFlightRequestDoesNotProduceError()=runRefreshTest {
        var cancelled=false
        val api=mock<LinbitApi> { onBlocking { supportTicket(any()) } doSuspendableAnswer {
            try { CompletableDeferred<ApiSupportTicket>().await() } finally { cancelled=true }
        } }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent();vm.stopDetail("A");runCurrent()
        assertTrue(cancelled);assertNull(vm.state.value.error)
    }
    @Test fun nonCooperativeOldTicketResponseCannotReplaceNewTicket()=runRefreshTest {
        val gate=CompletableDeferred<ApiSupportTicket>()
        val api=mock<LinbitApi> { onBlocking { supportTicket(any()) } doSuspendableAnswer { call ->
            if(call.getArgument<String>(0)=="A") withContext(NonCancellable) { gate.await() } else ticket("B")
        } }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent();vm.startDetail("B");runCurrent()
        assertEquals("B",vm.state.value.detail?.id)
        gate.complete(ticket("A"));runCurrent();assertEquals("B",vm.state.value.detail?.id)
    }
    @Test fun accountChangeDiscardsLateReplyAndStopsReads()=runRefreshTest {
        val gate=CompletableDeferred<ApiSupportTicket>();var reads=0
        val api=mock<LinbitApi> { onBlocking { supportTicket(any()) } doSuspendableAnswer { reads++;withContext(NonCancellable) { gate.await() } } }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent()
        tokens.clear();runCurrent();gate.complete(ticket());runCurrent()
        assertNull(vm.state.value.detail);advanceTimeBy(60000);runCurrent();assertEquals(1,reads)
    }
    @Test fun transientRefreshFailureRetainsHistoryAndNextSuccessClearsWarning()=runRefreshTest {
        var reads=0
        val api=mock<LinbitApi> { onBlocking { supportTicket(any()) } doSuspendableAnswer {
            reads++;if(reads==2) error("LOCAL_REFRESH_FAILURE") else ticket()
        } }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent()
        advanceTimeBy(10000);runCurrent();assertEquals("A",vm.state.value.detail?.id);assertNotNull(vm.state.value.detailRefreshError)
        advanceTimeBy(10000);runCurrent();assertNull(vm.state.value.detailRefreshError)
    }
    @Test fun readStartedBeforeLocalFakeMutationCannotOverwriteMutationResult()=runRefreshTest {
        val gate=CompletableDeferred<ApiSupportTicket>();val updated=ticket(subject="仅进程内回复结果")
        val api=mock<LinbitApi> {
            onBlocking { supportTicket(any()) } doSuspendableAnswer { withContext(NonCancellable) { gate.await() } }
            onBlocking { replySupportTicket(any(),any()) } doReturn updated
        }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent()
        vm.reply("A","LOCAL_IN_PROCESS_ONLY",emptyList()) {};runCurrent()
        assertEquals(updated,vm.state.value.detail)
        gate.complete(ticket(subject="旧响应"));runCurrent();assertEquals(updated,vm.state.value.detail)
    }
    @Test fun lateFakeWriteCannotPutOldTicketBackAfterSwitch()=runRefreshTest {
        val gate=CompletableDeferred<ApiSupportTicket>()
        val api=mock<LinbitApi> {
            onBlocking { supportTicket(any()) } doSuspendableAnswer { ticket(it.getArgument(0)) }
            onBlocking { replySupportTicket(any(),any()) } doSuspendableAnswer { gate.await() }
        }
        val vm=vm(api);runCurrent();vm.startDetail("A");runCurrent()
        vm.reply("A","LOCAL_ONLY",emptyList()) {};runCurrent();vm.startDetail("B");runCurrent()
        gate.complete(ticket("A"));runCurrent();assertNull(vm.state.value.detail)
        vm.loadDetail("B");runCurrent();assertEquals("B",vm.state.value.detail?.id)
    }

}
