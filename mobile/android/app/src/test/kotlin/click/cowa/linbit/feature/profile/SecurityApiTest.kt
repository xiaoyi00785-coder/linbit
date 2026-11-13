package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.*
import org.junit.Test

/** 安全中心接口路径 / 方法 / 请求体与 docs/api/openapi.yaml 一致。 */
class SecurityApiTest {
    private val server = MockWebServer()
    private val api = NetworkFactory.api(NetworkFactory.bareClient(), server.url("/").toString())
    @After fun tearDown() { server.shutdown() }

    private fun ok(body: String = """{"ok":true}""") = server.enqueue(MockResponse().setBody(body))

    @Test fun changePasswordSendsSecondFactor() = runTest {
        ok("""{"ok":true,"revokedSessions":2}""")
        val r = api.changePassword(ApiChangePasswordRequest(oldPassword = "old-Pass1", newPassword = "new-Pass1", code = "123456"))
        assertEquals(2, r.revokedSessions)
        val req = server.takeRequest()
        assertEquals("POST", req.method)
        assertEquals("/account/password/change", req.path)
        val body = req.body.readUtf8()
        assertTrue(body.contains("\"code\":\"123456\""))
        assertFalse(body.contains("totpCode"))
    }

    @Test fun fundPasswordModesUseDistinctEndpoints() = runTest {
        val body = ApiFundPasswordRequest(fundPassword = "135790", totpCode = "123456")
        ok(); api.setFundPassword(body)
        server.takeRequest().let { assertEquals("POST", it.method); assertEquals("/account/fund-password", it.path) }
        ok(); api.changeFundPassword(body.copy(oldFundPassword = "246801"))
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/account/fund-password", it.path)
            assertTrue(it.body.readUtf8().contains("\"oldFundPassword\":\"246801\""))
        }
        ok(); api.resetFundPassword(body)
        server.takeRequest().let { assertEquals("POST", it.method); assertEquals("/account/fund-password/reset", it.path) }
    }

    @Test fun antiPhishingAndContactChange() = runTest {
        ok(); api.setAntiPhishing(ApiSetAntiPhishingRequest(antiPhishingCode = "Linbit88", code = "111111"))
        server.takeRequest().let { assertEquals("PUT", it.method); assertEquals("/account/anti-phishing", it.path) }

        server.enqueue(MockResponse().setBody("""{"sent":true,"channel":"email","ttlSeconds":300}"""))
        api.sendContactCode(ApiSendContactCodeRequest(email = "new@example.com"))
        server.takeRequest().let { assertEquals("/account/contact/code", it.path) }

        ok(); api.changeEmail(ApiChangeContactRequest(newCode = "111111", oldCode = "222222", email = "new@example.com"))
        server.takeRequest().let { assertEquals("PUT", it.method); assertEquals("/account/email", it.path) }
        ok(); api.changePhone(ApiChangeContactRequest(newCode = "111111", oldCode = "222222", phone = "13800000000", countryCode = "86"))
        server.takeRequest().let { assertEquals("PUT", it.method); assertEquals("/account/phone", it.path) }
    }

    @Test fun sessionsDevicesAndPasskeys() = runTest {
        server.enqueue(MockResponse().setBody("""{"items":[{"id":"7","deviceId":"cid:a","current":true}]}"""))
        assertEquals("7", api.sessions().items?.single()?.id)
        assertEquals("/account/sessions", server.takeRequest().path)

        server.enqueue(MockResponse().setResponseCode(204))
        api.revokeSession("7")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/account/sessions/7", it.path) }

        server.enqueue(MockResponse().setBody("""{"revoked":3}"""))
        assertEquals(3, api.revokeOtherSessions().revoked)
        assertEquals("/account/sessions/revoke-others", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"items":[{"deviceId":"cid:a","firstSeen":"2026-11-01T00:00:00Z","lastSeen":"2026-11-13T00:00:00Z"}]}"""))
        assertEquals("cid:a", api.devices().items.single().deviceId)
        assertEquals("/account/devices", server.takeRequest().path)

        server.enqueue(MockResponse().setResponseCode(204))
        api.removeDevice("cid:a")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/account/devices/cid:a", it.path) }

        server.enqueue(MockResponse().setBody("""{"enabled":true,"items":[{"id":"pk1","name":"Mac"}]}"""))
        assertEquals("pk1", api.passkeys().items?.single()?.id)
        server.takeRequest()
        server.enqueue(MockResponse().setResponseCode(204))
        api.deletePasskey("pk1")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/account/passkeys/pk1", it.path) }
    }

    @Test fun secondFactorPicksTotpOrCode() {
        val totp = SecondFactor(useTotp = true, value = "123456")
        assertEquals("123456", totp.totpCode); assertNull(totp.code)
        val mail = SecondFactor(useTotp = false, value = "654321")
        assertNull(mail.totpCode); assertEquals("654321", mail.code)
    }
}
