package click.cowa.linbit.feature.auth

import androidx.test.core.app.ActivityScenario
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonObject
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = android.app.Application::class)
@kotlinx.coroutines.ExperimentalCoroutinesApi
class PasskeyLoginStateTest {
    @get:org.junit.Rule val activityRule = androidx.test.ext.junit.rules.ActivityScenarioRule(androidx.activity.ComponentActivity::class.java)
    private val tokens = InMemoryTokenStore()
    private val server = click.cowa.linbit.ui.evidence.MockEvidenceApi()
    private lateinit var activity: android.app.Activity
    private lateinit var prefs: AppPrefs
    private val dispatcher = Dispatchers.Unconfined
    private val fixtures by lazy { click.cowa.linbit.ui.evidence.ProductionEvidenceFixtures(server, CoroutineScope(Dispatchers.Main)) }
    private val api get() = fixtures.retrofit
    private val gateway = MutableGateway()
    private val paths = mutableListOf<String>()
    private val scope get() = CoroutineScope(dispatcher)
    private lateinit var vm: AuthViewModel

    private fun model(bridge: PasskeyCredentialBridge = gateway, passkeyApi: click.cowa.linbit.core.network.LinbitApi = api) {
        if (tokens.accessToken != null) tokens.clear()
        prefs = AppPrefs(org.robolectric.RuntimeEnvironment.getApplication())
        val toast = ToastCenter(scope)
        vm = AuthViewModel(AuthRepository(api, tokens, { "fake-device" }, { "loopback" }, scope), tokens, prefs, toast,
            PasskeyLoginService(passkeyApi, bridge, { "fake-device" }, { "loopback" }))
    }
    private suspend fun awaitBusy() = kotlinx.coroutines.withTimeout(5_000) {
        while (vm.passkeyLoading) kotlinx.coroutines.delay(25)
        kotlinx.coroutines.delay(5)
    }
    @Before fun setup() {
        Dispatchers.setMain(dispatcher)
        org.robolectric.shadows.ShadowLooper.idleMainLooper()
        activityRule.scenario.onActivity {
            activity = it

        }
        model()
        org.robolectric.shadows.ShadowLooper.idleMainLooper()
    }
    @After fun reset() { if (::vm.isInitialized) vm.cancelPasskeyLogin(); server.shutdown(); Dispatchers.resetMain() }
    private fun begin() = server.route("POST", "/auth/passkey/login/begin", JSONObject().put("sessionId", "INVALID_LOOPBACK_SESSION").put("publicKey", JSONObject().put("challenge", "bW9jay1jaGFsbGVuZ2U").put("rpId", "cowa.click").put("userVerification", "required")).toString())
    private fun token() = server.route("POST", "/auth/passkey/login/finish", """{"accessToken":"INVALID_LOOPBACK_ACCESS","refreshToken":"INVALID_LOOPBACK_REFRESH","tokenType":"Bearer","expiresIn":3600}""")
    class MutableGateway : PasskeyCredentialBridge {
        val body = """{"id":"mock_id","rawId":"mock_raw_id","type":"public-key","response":{"clientDataJSON":"mock_client_data","authenticatorData":"mock_auth_data","signature":"mock_signature","userHandle":"mock_user"}}"""
        var calls = 0
        var await: CompletableDeferred<JsonObject>? = null
        var error: Throwable? = null
        override suspend fun authenticate(activity: android.app.Activity, options: JsonObject): JsonObject {
            calls++
            check(options["challenge"]?.toString() == "\"bW9jay1jaGFsbGVuZ2U\"") { "System bridge must receive exact server options" }
            await?.await()
            error?.let { throw it }
            return credentialResponse(body)
        }
    }
    @Test fun realApiBeginSystemBridgeFinishAndOfficialTokenSave() = runBlocking {
        begin(); token()

        var done = 0
        vm.loginWithPasskey(activity) { done++ }; vm.loginWithPasskey(activity) { done++ }
        org.robolectric.shadows.ShadowLooper.idleMainLooper()
        awaitBusy()
        org.robolectric.shadows.ShadowLooper.idleMainLooper(); awaitBusy()

        assertEquals("routes=${server.requests}, error=${vm.passkeyError}, gateway=${gateway.calls}, done=$done", 2, server.requests.size); assertEquals(1, gateway.calls); assertEquals(1, done)
        assertEquals("INVALID_LOOPBACK_ACCESS", tokens.accessToken); assertEquals("INVALID_LOOPBACK_REFRESH", tokens.refreshToken)
        assertFalse(vm.loading); assertFalse(vm.passkeyLoading)
        assertTrue(server.requests.contains("POST /auth/passkey/login/begin [DECLARED_LOOPBACK_MOCK]"))
        val finishRequest = server.mutationRequests.single { it.path?.contains("/finish") == true }
        assertEquals("/auth/passkey/login/finish", finishRequest.path)
        val wire = JSONObject(finishRequest.body.clone().readUtf8())
        assertEquals("INVALID_LOOPBACK_SESSION", wire.getString("sessionId")); assertEquals("fake-device", wire.getString("deviceId")); assertEquals("public-key", wire.getJSONObject("credential").getString("type"))
        assertTrue(tokens.loggedIn.value)
    }
    @Test fun cancellationUnsupportedNoCredentialFailureStayRecoverableWithoutFinish() = runBlocking {
        val recoverable = listOf(PasskeyLoginFailure.Cancelled, PasskeyLoginFailure.Unsupported, PasskeyLoginFailure.NoCredential, PasskeyLoginFailure.Retry)
        val gateway = MutableGateway(); model(gateway)
        for (error in recoverable) {
            gateway.error = error; begin()
            vm.loginWithPasskey(activity) { fail("Error cannot finish login") }; awaitBusy()
            assertEquals("routes=${server.requests}, gateway=${gateway.calls}", error.message, vm.passkeyError); assertFalse(vm.passkeyLoading); assertNull(tokens.accessToken)
        }
        gateway.error = PasskeyLoginFailure.InvalidResponse; begin(); vm.loginWithPasskey(activity) { fail("Invalid JSON cannot finish login") }; awaitBusy()
        assertNotNull(vm.passkeyError); assertTrue(paths.none { it.contains("/finish") })
        assertNull(tokens.accessToken)
    }
    @Test fun lateNativeResultCannotFinishAfterCancellationOrSessionChange() = runBlocking {
        val deferred = CompletableDeferred<JsonObject>(); gateway.await = deferred
        begin(); vm.loginWithPasskey(activity) { fail("old cancelled native result") }; org.robolectric.shadows.ShadowLooper.idleMainLooper()
        kotlinx.coroutines.withTimeout(5_000) { while (gateway.calls == 0) kotlinx.coroutines.delay(25) }
        assertEquals(1, gateway.calls); assertNull(vm.passkeyError)
        vm.cancelPasskeyLogin(); deferred.complete(JsonObject(emptyMap())); awaitBusy()
        assertFalse(vm.passkeyLoading); assertNull(tokens.accessToken); assertTrue(paths.none { it.contains("/finish") })
        gateway.await = CompletableDeferred(); begin(); vm.loginWithPasskey(activity) { fail("old session native result") }; org.robolectric.shadows.ShadowLooper.idleMainLooper()
        kotlinx.coroutines.withTimeout(5_000) { while (gateway.calls < 1) kotlinx.coroutines.delay(25) }
        gateway.await!!.complete(JsonObject(emptyMap())); tokens.accessToken = "FAKE_INTERVENING_SESSION"; awaitBusy()
        assertNull(vm.passkeyError); assertFalse(vm.passkeyLoading); assertTrue(server.requests.none { it.contains("/finish") })
        assertEquals("FAKE_INTERVENING_SESSION", tokens.accessToken)
    }
}
