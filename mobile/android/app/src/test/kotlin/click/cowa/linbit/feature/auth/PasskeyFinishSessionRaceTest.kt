package click.cowa.linbit.feature.auth

import androidx.test.ext.junit.rules.ActivityScenarioRule
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Loopback HTTP finish is truly received late; this is NOT a system/native credential test. */
@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
@kotlinx.coroutines.ExperimentalCoroutinesApi
class PasskeyFinishSessionRaceTest {
    @get:Rule val activity = ActivityScenarioRule(androidx.activity.ComponentActivity::class.java)
    private val server = click.cowa.linbit.ui.evidence.MockEvidenceApi()
    @Test fun lateFinishCannotOverwriteNewSession() = runBlocking {
        Dispatchers.setMain(Dispatchers.Unconfined)
        lateinit var screen: android.app.Activity
        activity.scenario.onActivity { screen = it }
        try {
            server.route("POST", "/auth/passkey/login/begin", JSONObject().put("sessionId", "INVALID_LOOPBACK_SESSION").put("publicKey", JSONObject().put("challenge", "YQ")).toString())
            server.route("POST", "/auth/passkey/login/finish", """{"accessToken":"INVALID_STALE_ACCESS","refreshToken":"INVALID_STALE_REFRESH","tokenType":"Bearer","expiresIn":1}""")
            val hold = CountDownLatch(1); val returned = CountDownLatch(1)
            server.passkeyFinishGate = hold
            val client = okhttp3.OkHttpClient.Builder().addInterceptor { chain ->
                try { chain.proceed(chain.request()) } finally { if (chain.request().url.encodedPath.endsWith("/finish")) returned.countDown() }
            }.build()
            val api = NetworkFactory.api(client, server.baseUrl)
            val tokens = InMemoryTokenStore(); val scope = CoroutineScope(Dispatchers.Unconfined)
            val bridge = PasskeyCredentialBridge { _, _ -> credentialResponse("""{"id":"mock","rawId":"mock","type":"public-key","response":{"clientDataJSON":"mock","authenticatorData":"mock","signature":"mock"}}""") }
            val vm = AuthViewModel(AuthRepository(api, tokens, { "loopback" }, { "loopback" }, scope), tokens, AppPrefs(org.robolectric.RuntimeEnvironment.getApplication()), ToastCenter(scope), PasskeyLoginService(api, bridge, { "loopback" }, { "loopback" }))
            vm.loginWithPasskey(screen) { fail("Late token must not finish the old login") }
            withTimeout(5_000) { while (server.requests.none { it.contains("/finish") }) kotlinx.coroutines.delay(25) }
            tokens.accessToken = "FAKE_INTERVENING_SESSION"
            withTimeout(5_000) { while (vm.passkeyLoading) kotlinx.coroutines.delay(25) }
            hold.countDown()
            assertTrue(returned.await(5, TimeUnit.SECONDS)); kotlinx.coroutines.delay(50)
            assertEquals("FAKE_INTERVENING_SESSION", tokens.accessToken)
            assertFalse(vm.passkeyLoading); assertNull(vm.passkeyError)
            assertEquals(1, server.requests.count { it.contains("/finish") })
        } finally { server.passkeyFinishGate = null; server.shutdown(); Dispatchers.resetMain() }
    }
}
