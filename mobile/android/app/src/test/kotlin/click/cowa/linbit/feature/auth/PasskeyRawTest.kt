package click.cowa.linbit.feature.auth
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test
import org.junit.Assert.*
@org.robolectric.annotation.LooperMode(org.robolectric.annotation.LooperMode.Mode.LEGACY)
class PasskeyRawTest {
 @Test fun rawLoopbackRetrofitBegin() = runBlocking {
  val server = MockWebServer(); server.start()
  server.enqueue(MockResponse().setResponseCode(200).setHeader("Content-Type", "application/json").setBody("""{"sessionId":"raw","publicKey":{"challenge":"YQ","rpId":"cowa.click"}}"""))
  val api = NetworkFactory.api(okhttp3.OkHttpClient(), server.url("/").newBuilder().host("127.0.0.1").build().toString())
  try { val result = click.cowa.linbit.core.network.apiCall { api.passkeyLoginBegin() }; assertEquals("raw",result.sessionId); assertEquals(1,server.requestCount) } finally { server.shutdown() }
 }
}
