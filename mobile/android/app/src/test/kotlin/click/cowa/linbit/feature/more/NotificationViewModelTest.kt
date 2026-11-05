package click.cowa.linbit.feature.more

import click.cowa.linbit.core.generated.ApiNewcomerTask
import click.cowa.linbit.core.generated.ApiNotification
import click.cowa.linbit.core.generated.ApiNotificationList
import click.cowa.linbit.core.generated.ApiReadNotificationsRequest
import click.cowa.linbit.core.generated.ApiUpdatedCount
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.anyOrNull
import org.mockito.kotlin.doSuspendableAnswer
import org.mockito.kotlin.doReturn
import org.mockito.kotlin.mock

@OptIn(ExperimentalCoroutinesApi::class)
class NotificationViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private lateinit var toastScope: CoroutineScope

    @Before fun setUp() {
        Dispatchers.setMain(dispatcher)
        toastScope = CoroutineScope(SupervisorJob() + dispatcher)
    }

    @After fun tearDown() {
        toastScope.cancel()
        Dispatchers.resetMain()
    }

    private fun unread(id: String, kind: ApiNotification.Kind = ApiNotification.Kind.SYSTEM) =
        ApiNotification(id = id, kind = kind, read = false)

    @Test fun `第一条已读失败不会回滚第二条成功`() = runTest {
        val first = CompletableDeferred<ApiUpdatedCount>()
        val second = CompletableDeferred<ApiUpdatedCount>()
        val tokens = InMemoryTokenStore().apply { accessToken = "account-a" }
        val api = mock<LinbitApi> {
            this.onBlocking { notifications(anyOrNull(), any(), any()) } doReturn ApiNotificationList(
                items = listOf(unread("1"), unread("2", ApiNotification.Kind.TRADE)),
                total = 2, unread = 2, unreadByKind = mapOf("system" to 1, "trade" to 1),
            )
            this.onBlocking { readNotifications(any()) } doSuspendableAnswer { call ->
                val ids = call.getArgument<ApiReadNotificationsRequest>(0).ids.orEmpty()
                if (ids == listOf("1")) first.await() else second.await()
            }
        }
        val vm = NotificationViewModel(api, tokens, ToastCenter(toastScope))
        vm.load()
        advanceUntilIdle()
        vm.markRead(unread("1"))
        vm.markRead(unread("2", ApiNotification.Kind.TRADE))
        second.complete(ApiUpdatedCount(updated = 1))
        advanceUntilIdle()
        assertTrue(vm.state.value.items.single { it.id == "2" }.read == true)

        first.completeExceptionally(IllegalStateException("network"))
        advanceUntilIdle()
        assertFalse(vm.state.value.items.single { it.id == "1" }.read == true)
        assertTrue(vm.state.value.items.single { it.id == "2" }.read == true)
        assertEquals(1, vm.state.value.unread)
        assertEquals(1, vm.state.value.byKind["system"])
        assertEquals(0, vm.state.value.byKind["trade"])
    }

    @Test fun `分页刷新保留本条已读且未读计数不重复增加`() = runTest {
        val tokens = InMemoryTokenStore().apply { accessToken = "account-a" }
        val api = mock<LinbitApi> {
            this.onBlocking { notifications(anyOrNull(), any(), any()) } doReturn ApiNotificationList(
                items = listOf(unread("1")), total = 2, unread = 2, unreadByKind = mapOf("system" to 2),
            )
            this.onBlocking { readNotifications(any()) } doSuspendableAnswer { CompletableDeferred<ApiUpdatedCount>().await() }
        }
        val vm = NotificationViewModel(api, tokens, ToastCenter(toastScope))
        vm.load()
        advanceUntilIdle()
        vm.markRead(unread("1"))
        assertEquals(1, vm.state.value.unread)

        vm.load(append = true)
        advanceUntilIdle()
        assertTrue(vm.state.value.items.single { it.id == "1" }.read == true)
        assertEquals(1, vm.state.value.unread)
        assertEquals(1, vm.state.value.byKind["system"])
    }

    @Test fun `切账号后旧已读失败不能写回旧通知`() = runTest {
        val oldRequest = CompletableDeferred<ApiUpdatedCount>()
        val tokens = InMemoryTokenStore().apply { accessToken = "account-a" }
        val api = mock<LinbitApi> {
            this.onBlocking { notifications(anyOrNull(), any(), any()) } doReturn ApiNotificationList(
                items = listOf(unread("old")), total = 1, unread = 1, unreadByKind = mapOf("system" to 1),
            )
            this.onBlocking { readNotifications(any()) } doSuspendableAnswer { oldRequest.await() }
        }
        val vm = NotificationViewModel(api, tokens, ToastCenter(toastScope))
        vm.load()
        advanceUntilIdle()
        vm.markRead(unread("old"))

        tokens.accessToken = "account-b"
        oldRequest.completeExceptionally(IllegalStateException("network"))
        advanceUntilIdle()
        assertTrue(vm.state.value.items.isEmpty())
        assertEquals(0, vm.state.value.unread)
        assertTrue(vm.state.value.byKind.isEmpty())
    }

    @Test fun `切账号后旧未读角标不能覆盖新账号`() = runTest {
        val oldCount = CompletableDeferred<click.cowa.linbit.core.generated.ApiUnreadCount>()
        val newCount = CompletableDeferred<click.cowa.linbit.core.generated.ApiUnreadCount>()
        val tokens = InMemoryTokenStore().apply { accessToken = "account-a" }
        val api = mock<LinbitApi> {
            this.onBlocking { unreadCount() } doSuspendableAnswer {
                if (tokens.accessToken == "account-a") oldCount.await() else newCount.await()
            }
        }
        val vm = NotificationBadgeViewModel(api, tokens)
        advanceUntilIdle()
        tokens.accessToken = "account-b"
        oldCount.complete(click.cowa.linbit.core.generated.ApiUnreadCount(unread = 99))
        advanceUntilIdle()
        assertEquals(0, vm.unread.value)
    }

    @Test fun `新人任务类型映射到实际业务入口`() {
        assertEquals(WelfareTaskRoute.Profile, welfareTaskRoute(ApiNewcomerTask.Type.BIND))
        assertEquals(WelfareTaskRoute.Deposit, welfareTaskRoute(ApiNewcomerTask.Type.FIRST_DEPOSIT))
        assertEquals(WelfareTaskRoute.Futures, welfareTaskRoute(ApiNewcomerTask.Type.VOLUME))
        assertNull(welfareTaskRoute(null))
    }
}
