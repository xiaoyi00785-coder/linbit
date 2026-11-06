package click.cowa.linbit.feature.profile

import androidx.test.ext.junit.rules.ActivityScenarioRule
import click.cowa.linbit.core.generated.ApiPasskey
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.*
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
class PasskeyDeletionStateTest {
    @get:Rule val activity = ActivityScenarioRule(androidx.activity.ComponentActivity::class.java)
    private val server = click.cowa.linbit.ui.evidence.MockEvidenceApi()
    private val tokens = InMemoryTokenStore()
    private lateinit var vm: SecurityViewModel
    private fun model(api: click.cowa.linbit.core.network.LinbitApi = NetworkFactory.api(okhttp3.OkHttpClient(), server.baseUrl)) {
        vm = SecurityViewModel(api, ToastCenter(CoroutineScope(Dispatchers.Unconfined)), tokens,
            PasskeyRegistrationService(api) { _, _ -> throw AssertionError("Deletion must not create a native credential") })
    }
    @Before fun setup() { Dispatchers.setMain(Dispatchers.Unconfined); tokens.accessToken = "INVALID_LOOPBACK_ONLY"; model(); activity.scenario.onActivity { } }
    private fun list(id: String = "loopback-id") = server.route("GET", "/account/passkeys", """{"enabled":true,"items":[{"id":"$id","name":"Loopback Android","createdAt":"2026-11-06T00:00:00Z"}]}""")
    private suspend fun loading() = withTimeout(5_000) { while (vm.state.value.passkeysLoading) delay(25) }

    @Test fun cancelHasZeroRequestAndSuccessfulDeleteReadbackAndBusyGuard() = runBlocking {
        list(); vm.loadPasskeys(); loading()
        val item = vm.state.value.passkeys.single()
        vm.showDeletePasskey(item); vm.cancelDeletePasskey()
        assertEquals(0, server.requests.count { it.startsWith("DELETE") })
        server.route("DELETE", "/account/passkeys/loopback-id", "", 204); list("second-from-server")
        vm.showDeletePasskey(item); vm.confirmDeletePasskey(); vm.confirmDeletePasskey()
        withTimeout(5_000) { while (vm.state.value.passkeyDeleteBusy) delay(25); delay(10) }
        assertEquals(1, server.requests.count { it.startsWith("DELETE") })
        assertEquals("second-from-server", vm.state.value.passkeys.single().id)
        assertNull(vm.state.value.passkeyDeleteTarget); assertFalse(vm.state.value.passkeyDeleteBusy)
    }
    @Test fun failureRetainsReviewForRetryAndRejectsSecondClick() = runBlocking {
        list(); vm.loadPasskeys(); loading()
        server.route("DELETE", "/account/passkeys/loopback-id", """{"code":"EVIDENCE_FAILURE","message":"loopback blocked"}""", 503)
        vm.showDeletePasskey(vm.state.value.passkeys.single()); vm.confirmDeletePasskey()
        withTimeout(5_000) { while (vm.state.value.passkeyDeleteBusy) delay(25); delay(10) }
        assertNotNull(vm.state.value.passkeysError); assertNotNull(vm.state.value.passkeyDeleteTarget)
        server.route("DELETE", "/account/passkeys/loopback-id", "", 204); server.route("GET", "/account/passkeys", """{"enabled":true,"items":[]}""")
        vm.confirmDeletePasskey()
        withTimeout(5_000) { while (vm.state.value.passkeyDeleteBusy) delay(25); delay(10) }
        assertTrue(vm.state.value.passkeys.isEmpty()); assertNull(vm.state.value.passkeyDeleteTarget)
    }
    @Test fun sameSessionWhileDeleteInFlightCannotReadbackOrOverwriteNextAccount() = runBlocking {
        list(); vm.loadPasskeys(); loading()
        val item = vm.state.value.passkeys.single(); val hold = CountDownLatch(1); val returned = CountDownLatch(1); server.passkeyDeleteGate = hold
        val client = okhttp3.OkHttpClient.Builder().addInterceptor { chain ->
            try { chain.proceed(chain.request()) } finally { if (chain.request().method == "DELETE") returned.countDown() }
        }.build()
        model(NetworkFactory.api(client, server.baseUrl))
        server.route("DELETE", "/account/passkeys/loopback-id", "", 204)
        vm.showDeletePasskey(item); vm.confirmDeletePasskey()
        withTimeout(5_000) { while (server.requests.none { it.startsWith("DELETE") }) delay(25) }
        val beforeSessionChangeReads = server.requests.count { it.startsWith("GET /account/passkeys") }
        tokens.accessToken = "INVALID_NEW_SESSION"
        withTimeout(5_000) { while (vm.state.value.passkeyDeleteBusy) delay(25) }
        hold.countDown(); assertTrue(returned.await(5, TimeUnit.SECONDS)); delay(50)
        assertEquals(beforeSessionChangeReads, server.requests.count { it.startsWith("GET /account/passkeys") })
        assertTrue(vm.state.value.passkeys.isEmpty()); assertNull(vm.state.value.passkeyDeleteTarget)
        assertFalse(vm.state.value.passkeyDeleteBusy)
    }
    @After fun close() { server.passkeyDeleteGate?.countDown(); server.shutdown(); Dispatchers.resetMain() }
}
