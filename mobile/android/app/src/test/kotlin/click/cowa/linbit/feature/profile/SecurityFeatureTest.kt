package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.ApiSecurityOverview
import click.cowa.linbit.core.generated.ApiSecurityOverviewTwoFactor
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.coroutines.test.advanceUntilIdle
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.junit.Test

@RunWith(RobolectricTestRunner::class)
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@Config(sdk = [34])
class SecurityFeatureTest {
    private lateinit var server: MockWebServer
    private lateinit var vm: SecurityFeatureViewModel

    @Before fun setup() {
        server = MockWebServer().also { it.start() }
        vm = SecurityFeatureViewModel(NetworkFactory.api(OkHttpClient(), server.url("/openapi/").toString()), ToastCenter(CoroutineScope(Dispatchers.Default)))
    }
    @After fun close() { server.shutdown() }

    private fun overview(enabled: Boolean) = ApiSecurityOverview(twoFactor = ApiSecurityOverviewTwoFactor(enabled = enabled))

    @Test fun validationRejectsWeakAndInvalidUnpublishedValues() {
        assertTrue(SecurityFeatureLogic.antiPhishingError("") != null)
        assertTrue(SecurityFeatureLogic.antiPhishingError("密码") != null)
        assertTrue(SecurityFeatureLogic.passwordError("", "validTest1", "validTest1") != null)
        assertTrue(SecurityFeatureLogic.passwordError("oldTest1", "oldTest1", "oldTest1") != null)
        assertTrue(SecurityFeatureLogic.passwordError("oldTest1", "validTest1", "mismatch") != null)
        assertTrue(SecurityFeatureLogic.passwordError("oldTest1", "validTest1", "validTest1") == null)
        assertTrue(SecurityFeatureLogic.verificationReady(overview(true), "123456", ""))
        assertTrue(SecurityFeatureLogic.verificationReady(overview(false), "", "123456"))
        assertFalse(SecurityFeatureLogic.verificationReady(overview(true), "12345", "000000"))
        assertFalse(SecurityFeatureLogic.verificationReady(null, "123456", ""))
    }

    @Test fun passwordPostsExactContractUsingInvalidFixtureAndRevokesOtherSessions() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"ok":true,"revokedSessions":2}"""))
        val result = vm.runPasswordChange("oldTest1", "newTest2", "newTest2", overview(true), "000000", "invalid")
        advanceUntilIdle()
        val r = server.takeRequest(5, java.util.concurrent.TimeUnit.SECONDS).let { requireNotNull(it) }
        advanceUntilIdle()
        assertEquals("path=${r.path}", 2, result)
        assertEquals("POST", r.method)
        assertEquals("/openapi/account/password/change", r.path)
        val body = r.body.readUtf8()
        val requestBody = NetworkFactory.json.parseToJsonElement(body).jsonObject
        assertEquals("oldTest1", requestBody["oldPassword"]?.jsonPrimitive?.content)
        assertEquals("newTest2", requestBody["newPassword"]?.jsonPrimitive?.content)
        assertEquals("000000", requestBody["totpCode"]?.jsonPrimitive?.content)
        assertFalse("code" in requestBody)
        assertEquals(true, requestBody["revokeOtherSessions"]?.jsonPrimitive?.boolean)
    }

    @Test fun nonTwoFactorPasswordUsesSecurityCodeNotTotp() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"ok":true,"revokedSessions":1}"""))
        vm.runPasswordChange("oldTest1", "newTest2", "newTest2", overview(false), "000000", "000001")
        advanceUntilIdle()
        val b = server.takeRequest(5, java.util.concurrent.TimeUnit.SECONDS).let { requireNotNull(it) }.body.readUtf8()
        assertTrue(b.contains("\"code\":\"000001\""))
        assertTrue(!b.contains("totpCode"))
    }

    @Test fun invalidPasswordFailsWithoutAnyNetworkRequest() = runTest {
        var error: String? = null
        kotlin.runCatching { vm.runPasswordChange("oldTest1", "newTest2", "different", overview(true), "000000", "") }.exceptionOrNull()?.let { error = it.message }
        advanceUntilIdle()
        assertTrue(error != null)
        val missingOverview = runCatching {
            vm.runPasswordChange("oldTest1", "validTest1", "validTest1", null, "", "123456")
        }.exceptionOrNull()
        assertTrue(missingOverview != null)
        assertEquals("请先完成安全验证", missingOverview?.message)
        assertEquals(0, server.requestCount)
    }

    @Test fun antiPhishingRejectsTooLongBeforeOpeningSecurityReview() = runTest {
        assertTrue(SecurityFeatureLogic.antiPhishingError("a".repeat(21)) != null)
        assertTrue(SecurityFeatureLogic.antiPhishingError("a".repeat(20)) == null)
        val err = runCatching { vm.runAntiPhishing("a".repeat(21), overview(false), "", "123456") }.exceptionOrNull()
        assertTrue(err != null)
        assertFalse(SecurityFeatureLogic.verificationReady(overview(false), "", ""))
        assertEquals(0, server.requestCount)
    }

    @Test fun antiPhishingAndSecuritySendFollowExactContract() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody("{}")) // Generated response permits absent ok, independent of a later documented {ok:true}.
        vm.runAntiPhishing("TEST2026", overview(true), "000000", "")
        advanceUntilIdle()

        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"ok":true}"""))
        vm.runAntiPhishing("TEST2026", overview(true), "000000", "")
        advanceUntilIdle()
        server.takeRequest() // first
        val req = server.takeRequest(5, java.util.concurrent.TimeUnit.SECONDS).let { requireNotNull(it) }
        assertEquals("PUT", req.method)
        assertEquals("/openapi/account/anti-phishing", req.path)
        assertTrue(req.body.readUtf8().contains("\"antiPhishingCode\":\"TEST2026\""))
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"sent":true,"channel":"email"}"""))
        // All inputs are .invalid / documented-unused codes, never actual challenge/TOTP state.
        vm.sendSecurityCode()
        val send = server.takeRequest()
        assertEquals("POST", send.method)
        assertTrue(send.body.readUtf8().contains("\"purpose\":\"security\""))
        // Fixture challenge is invalid and deliberately never submitted to a real user/API.
    }
}
