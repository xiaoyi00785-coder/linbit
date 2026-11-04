package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiAuthTokens
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/** 刷新令牌失败的分流：只有 401 / 400+AUTH_* 才登出；5xx、429、网络错误保持登录态。 */
class TokenAuthenticatorTest {
    private lateinit var server: MockWebServer
    private lateinit var tokens: InMemoryTokenStore
    private var expired = 0

    @Before fun setUp() {
        server = MockWebServer().also { it.start() }
        tokens = InMemoryTokenStore().also { it.accessToken = "old"; it.refreshToken = "rt" }
        expired = 0
    }

    @After fun tearDown() { runCatching { server.shutdown() } }

    private fun auth() = TokenAuthenticator(tokens, server.url("/").toString(), "test", OkHttpClient()) { expired++ }

    private fun unauthorized(): Response {
        val req = Request.Builder().url("http://localhost/x").header("Authorization", "Bearer old").build()
        return Response.Builder().request(req).protocol(Protocol.HTTP_1_1).code(401).message("Unauthorized").build()
    }

    private fun assertKept() {
        assertEquals("old", tokens.accessToken); assertEquals("rt", tokens.refreshToken)
        assertFalse(tokens.sessionExpired); assertEquals(0, expired)
    }

    @Test fun refreshSuccessRetriesWithNewToken() {
        server.enqueue(MockResponse().setBody("""{"accessToken":"new","refreshToken":"rt2","tokenType":"Bearer","expiresIn":900}"""))
        val retry = auth().authenticate(null, unauthorized())
        assertNotNull(retry)
        assertEquals("Bearer new", retry!!.header("Authorization"))
        assertEquals("rt2", tokens.refreshToken)
    }

    @Test fun status401ClearsSession() {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"error":{"code":"AUTH_INVALID_TOKEN","message":"x"}}"""))
        assertNull(auth().authenticate(null, unauthorized()))
        assertNull(tokens.accessToken); assertNull(tokens.refreshToken)
        assertTrue(tokens.sessionExpired); assertEquals(1, expired)
    }

    @Test fun status400WithAuthCodeClearsSession() {
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"code":"AUTH_REFRESH_EXPIRED","message":"x"}"""))
        assertNull(auth().authenticate(null, unauthorized()))
        assertNull(tokens.accessToken); assertEquals(1, expired)
    }

    @Test fun status400WithOtherCodeKeepsSession() {
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"code":"VALIDATION_ERROR","message":"x"}"""))
        assertNull(auth().authenticate(null, unauthorized())); assertKept()
    }

    @Test fun serverErrorKeepsSession() {
        server.enqueue(MockResponse().setResponseCode(503).setBody("<html>bad gateway</html>"))
        assertNull(auth().authenticate(null, unauthorized())); assertKept()
    }

    @Test fun rateLimitKeepsSession() {
        server.enqueue(MockResponse().setResponseCode(429))
        assertNull(auth().authenticate(null, unauthorized())); assertKept()
    }

    @Test fun networkErrorKeepsSession() {
        server.shutdown()
        assertNull(auth().authenticate(null, unauthorized())); assertKept()
    }

    @Test fun classify() {
        assertEquals(RefreshOutcome.Rejected, classifyRefresh(401, null))
        assertEquals(RefreshOutcome.Transient, classifyRefresh(500, null))
        assertEquals(RefreshOutcome.Transient, classifyRefresh(400, "not json"))
    }
}
