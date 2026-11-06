package click.cowa.linbit.feature.profile

import androidx.test.ext.junit.rules.ActivityScenarioRule
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.feature.auth.PasskeyLoginFailure
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.*
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import kotlinx.serialization.json.JsonObject
import org.json.JSONObject
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
@kotlinx.coroutines.ExperimentalCoroutinesApi
class PasskeyRegistrationStateTest {
    @get:Rule val activity = ActivityScenarioRule(androidx.activity.ComponentActivity::class.java)
    private val tokens = InMemoryTokenStore()
    private val server = click.cowa.linbit.ui.evidence.MockEvidenceApi()
    private val bridge = DeferredBridge()
    private lateinit var vm: SecurityViewModel
    private lateinit var screen: android.app.Activity
    private val api get() = NetworkFactory.api(okhttp3.OkHttpClient(), server.baseUrl)
    private val scope = CoroutineScope(Dispatchers.Unconfined)

    class DeferredBridge : PasskeyRegistrationBridge {
        var calls = 0
        var error: PasskeyLoginFailure? = null
        var deferred: CompletableDeferred<JsonObject>? = null
        override suspend fun create(activity: android.app.Activity, options: JsonObject): JsonObject {
            calls++
            assertEquals("YQ", (options["challenge"] as kotlinx.serialization.json.JsonPrimitive).content)
            deferred?.await()
            error?.let { throw it }
            return registrationResponse("""{"id":"mock","rawId":"mock","type":"public-key","response":{"clientDataJSON":"mock","attestationObject":"mock"}}""")
        }
    }

    @Before fun setup() {
        Dispatchers.setMain(Dispatchers.Unconfined)
        activity.scenario.onActivity { screen = it }
        tokens.accessToken = "INVALID_LOOPBACK_ONLY"
        model()
        org.robolectric.shadows.ShadowLooper.idleMainLooper()
    }
    private fun model() { vm = SecurityViewModel(api, ToastCenter(scope), tokens, PasskeyRegistrationService(api, bridge)) }
    private fun begin() = server.route("POST", "/account/passkeys/register/begin", """{"sessionId":"INVALID_LOOPBACK_SESSION","publicKey":{"challenge":"YQ"}}""")
    private fun list(id: String = "loopback-id") = server.route("GET", "/account/passkeys", """{"enabled":true,"items":[{"id":"$id","name":"Test Android","createdAt":"2026-11-06T00:00:00Z"}]}""")
    private fun finish() = server.route("POST", "/account/passkeys/register/finish", """{"id":"loopback-id","name":"Test Android","createdAt":"2026-11-06T00:00:00Z"}""", 201)
    private fun reloadVmPlusSeed() {
        model(); org.robolectric.shadows.ShadowLooper.idleMainLooper()
        vm.seedEvidenceSecurity(click.cowa.linbit.core.generated.ApiSecurityOverview(twoFactor = click.cowa.linbit.core.generated.ApiSecurityOverviewTwoFactor(enabled = true, method = click.cowa.linbit.core.generated.ApiSecurityOverviewTwoFactor.Method.TOTP)))
    }
    private suspend fun busy() = withTimeout(5_000) { while (vm.state.value.passkeyBusy) delay(25); delay(10) }
    private suspend fun started() = withTimeout(5_000) { while (bridge.calls == 0) delay(25) }

    @Test fun totpExactWireFinishAndReadbackWithDoubleClickGuard() = runBlocking {
        begin(); finish(); list()
        vm.seedEvidenceSecurity(click.cowa.linbit.core.generated.ApiSecurityOverview(twoFactor = click.cowa.linbit.core.generated.ApiSecurityOverviewTwoFactor(enabled = true, method = click.cowa.linbit.core.generated.ApiSecurityOverviewTwoFactor.Method.TOTP)))
        reloadVmPlusSeed(); vm.passkeyCode("123456")
        vm.registerPasskey(screen, "Test Android"); vm.registerPasskey(screen, "Test Android")
        busy()
        assertEquals(1, bridge.calls)
        assertTrue(server.requests.contains("POST /account/passkeys/register/finish [DECLARED_LOOPBACK_MOCK]"))
        val begin = server.mutationRequests.single { it.path?.endsWith("/register/begin") == true }
        val body = JSONObject(begin.body.clone().readUtf8())
        assertEquals("123456", body.getString("totpCode")); assertFalse(body.has("code"))
        val finish = server.mutationRequests.single { it.path?.endsWith("/register/finish") == true }
        assertEquals("Test Android", JSONObject(finish.body.clone().readUtf8()).getString("name"))
        assertEquals("Test Android", vm.state.value.passkeys.single().name)
        assertEquals("loopback-id", vm.state.value.passkeys.single().id)
        assertEquals("", vm.state.value.passkeyCode); assertNull(vm.state.value.passkeysError)
        assertEquals(1, server.requests.count { it.startsWith("GET /account/passkeys") })
    }
    @Test fun cancelUnsupportedInvalidAndErrorsRetainCodeAndAllowRetryWithoutFinish() = runBlocking {
        vm.passkeyCode("654321")
        for (error in listOf(PasskeyLoginFailure.Cancelled, PasskeyLoginFailure.Unsupported, PasskeyLoginFailure.InvalidResponse, PasskeyLoginFailure.Retry)) {
            bridge.error = error; begin(); vm.registerPasskey(screen, "unused"); busy()
            assertEquals(error.message, vm.state.value.passkeysError); assertEquals("654321", vm.state.value.passkeyCode)
            vm.loadPasskeys(); withTimeout(5_000) { while (vm.state.value.passkeysLoading) delay(25) }
        }
        assertEquals(0, server.requests.count { it.contains("/register/finish") })
        begin(); server.explicitFailure("POST", "/account/passkeys/register/begin", "loopback rejected")
        vm.registerPasskey(screen, "unused"); busy(); assertNotNull(vm.state.value.passkeysError); assertEquals("654321", vm.state.value.passkeyCode)
        assertTrue(server.mutationRequests.filter { it.path?.endsWith("/register/begin") == true }.all { JSONObject(it.body.clone().readUtf8()).has("code") && !JSONObject(it.body.clone().readUtf8()).has("totpCode") })
    }
    @Test fun deferredNativeResultCannotFinishAfterCancelOrSessionChange() = runBlocking {
        bridge.deferred = CompletableDeferred(); begin(); vm.passkeyCode("123456"); vm.registerPasskey(screen, "unused"); started()
        vm.cancelPasskeyRegistration(); bridge.deferred!!.complete(JsonObject(emptyMap())); delay(50)
        assertEquals(0, server.requests.count { it.contains("/register/finish") }); assertFalse(vm.state.value.passkeyBusy)
        bridge.deferred = CompletableDeferred(); begin(); vm.registerPasskey(screen, "unused"); started()
        tokens.clear(); bridge.deferred!!.complete(JsonObject(emptyMap())); delay(50)
        assertEquals(0, server.requests.count { it.contains("/register/finish") }); assertFalse(vm.state.value.passkeyBusy)
        assertTrue(vm.state.value.passkeys.isEmpty()); assertNull(vm.state.value.passkeysError)
    }
    @Test fun sessionChangeDuringFinishCannotPublishReadbackForOldAccount() = runBlocking {
        begin(); finish(); list(); vm.passkeyCode("123456")
        val hold = CountDownLatch(1); val returned = CountDownLatch(1); server.passkeyRegistrationFinishGate = hold
        val client = okhttp3.OkHttpClient.Builder().addInterceptor { chain ->
            try { chain.proceed(chain.request()) } finally { if (chain.request().url.encodedPath.endsWith("/finish")) returned.countDown() }
        }.build()
        val api = NetworkFactory.api(client, server.baseUrl)
        vm = SecurityViewModel(api, ToastCenter(scope), tokens, PasskeyRegistrationService(api, bridge))
        vm.passkeyCode("123456"); vm.registerPasskey(screen, "unused")
        withTimeout(5_000) { while (server.requests.none { it.contains("/register/finish") }) delay(25) }
        tokens.accessToken = "INVALID_NEW_SESSION"; awaitBusy()
        hold.countDown(); assertTrue(returned.await(5, TimeUnit.SECONDS)); delay(50)
        assertEquals(0, server.requests.count { it.startsWith("GET /account/passkeys") })
        assertTrue(vm.state.value.passkeys.isEmpty()); assertNull(vm.state.value.passkeysError); assertFalse(vm.state.value.passkeyBusy)
    }
    private suspend fun awaitBusy() = withTimeout(5_000) { while (vm.state.value.passkeyBusy) delay(25) }
    @After fun close() { server.passkeyRegistrationFinishGate?.countDown(); server.shutdown(); Dispatchers.resetMain() }
}
