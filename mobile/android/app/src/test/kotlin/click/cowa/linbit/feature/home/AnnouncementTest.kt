package click.cowa.linbit.feature.home

import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiAnnouncement
import click.cowa.linbit.core.network.LinbitApi
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.Assert.*
import org.mockito.kotlin.*

@OptIn(ExperimentalCoroutinesApi::class)
class AnnouncementTest {
    private val dispatcher=StandardTestDispatcher()
    @Before fun before() { Dispatchers.setMain(dispatcher) }
    @After fun after() { Dispatchers.resetMain() }
    @Test fun oldNonCooperativeResponseCannotReplaceAnotherId()=runTest(dispatcher) {
        val gate=CompletableDeferred<ApiAnnouncement>()
        val api=mock<LinbitApi> { onBlocking { announcement(any()) } doSuspendableAnswer { call ->
            val id=call.getArgument<Long>(0);if(id==1L) withContext(NonCancellable) { gate.await() } else ApiAnnouncement(id=id,title="LOCAL B")
        } }
        val vm=AnnouncementViewModel(api)
        try { vm.load(1);runCurrent();vm.load(2);runCurrent();gate.complete(ApiAnnouncement(id=1,title="OLD LOCAL"));runCurrent();assertEquals(2L,vm.state.value.item?.id) }
        finally { vm.stop();vm.viewModelScope.cancel() }
    }
    @Test fun errorsAreNotEmptySuccessAndRetryCanRecover()=runTest(dispatcher) {
        var attempt=0
        val api=mock<LinbitApi> { onBlocking { announcement(any()) } doSuspendableAnswer { if(++attempt==1) error("LOCAL 404") else ApiAnnouncement(id=3,title="LOCAL") } }
        val vm=AnnouncementViewModel(api)
        try { vm.load(3);runCurrent();assertNotNull(vm.state.value.error);assertNull(vm.state.value.item);vm.load(3);runCurrent();assertNull(vm.state.value.error);assertEquals(3L,vm.state.value.item?.id) }
        finally { vm.stop();vm.viewModelScope.cancel() }
    }
    @Test fun disposeCancelsRead()=runTest(dispatcher) {
        var cancelled=false
        val api=mock<LinbitApi> { onBlocking { announcement(any()) } doSuspendableAnswer { try { CompletableDeferred<ApiAnnouncement>().await() } finally { cancelled=true } } }
        val vm=AnnouncementViewModel(api)
        try { vm.load(1);runCurrent();vm.stop();runCurrent();assertTrue(cancelled);assertNull(vm.state.value.item) }
        finally { vm.viewModelScope.cancel() }
    }
    @Test fun wrongResponseIdIsNotDisplayed()=runTest(dispatcher) {
        val api=mock<LinbitApi> { onBlocking { announcement(any()) } doReturn ApiAnnouncement(id=7,title="WRONG LOCAL") }
        val vm=AnnouncementViewModel(api)
        try { vm.load(8);runCurrent();assertNull(vm.state.value.item);assertNotNull(vm.state.value.error) }
        finally { vm.stop();vm.viewModelScope.cancel() }
    }

}
