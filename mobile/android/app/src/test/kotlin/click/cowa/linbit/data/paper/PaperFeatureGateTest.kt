package click.cowa.linbit.data.paper

import click.cowa.linbit.core.network.*
import click.cowa.linbit.di.AppModule
import click.cowa.linbit.feature.assets.PaperAccountViewModel
import click.cowa.linbit.ui.components.ToastCenter
import com.jakewharton.retrofit2.converter.kotlinx.serialization.asConverterFactory
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.*
import org.junit.*
import org.junit.Assert.*
import retrofit2.Retrofit
import java.util.concurrent.CopyOnWriteArrayList

/** All tests reject routes other than explicitly registered loopback GET responses. No real session or URL. */
@OptIn(ExperimentalCoroutinesApi::class)
@org.junit.runner.RunWith(org.robolectric.RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
class PaperFeatureGateTest {
    @Before fun main() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun resetMain() { Dispatchers.resetMain() }
    lateinit var http: OkHttpClient
    lateinit var features: LinbitApi
    private fun fixture(defaultFlag: String) = Triple(MockWebServer().also { it.start() }, CopyOnWriteArrayList<RecordedRequest>(), defaultFlag).also { (server, requests, _) ->
        val json = NetworkFactory.json
        http = OkHttpClient.Builder().addInterceptor(AuthInterceptor(InMemoryTokenStore().apply { accessToken = "mock-only-token" }, "mock", { "mock" })).build()
        val retrofit = Retrofit.Builder().baseUrl("http://127.0.0.1:${server.port}/").client(http).addConverterFactory(json.asConverterFactory("application/json".toMediaType())).build()
        features = retrofit.create(LinbitApi::class.java)
        server.dispatcher = object : Dispatcher() { override fun dispatch(req: RecordedRequest): MockResponse {
            requests += req
            if (req.path != "/app/features") return MockResponse().setResponseCode(599).setBody("Only public configuration is allowed")
            val body = if (defaultFlag == "malformed") "not-json" else if (defaultFlag == "network-error") throw java.io.IOException("mock network failure") else if (defaultFlag == "missing") """{"features":{}}""" else """{"features":{"paperTrading":$defaultFlag}}"""
            return MockResponse().setResponseCode(if (defaultFlag == "500") 500 else 200).setHeader("Content-Type", "application/json").setBody(body)
        } }
    }
    private fun client(server: MockWebServer) = AppModule.paperClient(NetworkFactory.paperApi(http, "http://127.0.0.1:${server.port}/"), AppModule.paperFeatureGate(features))
    private fun requireDenied(block: suspend () -> Any?) = runBlocking { runCatching { block() }.exceptionOrNull() ?: error("Request was not denied locally") }
    private fun closed(defaultFlag: String) {
        val (server, requests, _) = fixture(defaultFlag)
        try {
            val client = client(server)
            assertFalse(client.enabled)
            runBlocking { assertFalse(client.refreshFeature()) }
            requireDenied { client.account() }
            requireDenied { client.reset("idempotency-mock-must-not-send") }
            requireDenied { client.placeOrder(PaperOrderRequest("BTCUSDT", "buy", quantity = "1", clientOrderId = "mock-must-not-send")) }
            assertEquals(listOf("GET /app/features"), requests.map { "${it.method} ${it.path}" })
            assertNull(requests.single().getHeader("Authorization"))
            assertNull(requests.single().getHeader(ApiHeaders.NO_AUTH))
        } finally { server.shutdown() }
    }
    @Test fun disabledMissingConfigErrorsNetworkAndBadJsonFailClosed() {
        listOf("false", "missing", "\"bad-type\"", "500", "network-error", "malformed").forEach(::closed)
    }
    @Test fun trueAllowsIndependentRetrofitPaperReadsOnlyWithGenerationAndModeChecks() = runTest {
        val (server, requests, _) = fixture("true")
        try {
            server.dispatcher = object : Dispatcher() { override fun dispatch(req: RecordedRequest): MockResponse {
                requests += req
                val body = when (req.path) {
                    "/app/features" -> """{"features":{"paperTrading":true}}"""
                    "/paper/account" -> ACCOUNT
                    "/paper/balances?page=1&pageSize=20" -> BALANCES
                    else -> return MockResponse().setResponseCode(599).setBody("Route is not permitted")
                }
                return MockResponse().setResponseCode(200).setHeader("Content-Type", "application/json").setBody(body)
            } }
            val client = client(server)
            assertFalse(client.enabled)
            assertTrue(client.refreshFeature())
            assertEquals(3L, client.account().generation)
            assertEquals(3L, client.balances().generation)
            assertEquals(listOf("/app/features", "/paper/account", "/paper/balances?page=1&pageSize=20"), requests.map { it.path })
            assertTrue(requests.all { it.method == "GET" })
        } finally { server.shutdown() }
    }
    @OptIn(ExperimentalCoroutinesApi::class)
    @Test fun disabledVmDoesNotEraseSessionEpochOrSendPaperRead() = runTest {
        val (server, requests, _) = fixture("false")
        try {
            val vm = PaperAccountViewModel(client(server), InMemoryTokenStore().apply { accessToken = "mock-session" }, ToastCenter(this))
            assertFalse(vm.available)
            vm.load(); advanceUntilIdle()
            assertFalse(vm.available); assertNull(vm.account)
            assertEquals(listOf("GET /app/features"), requests.map { "${it.method} ${it.path}" })
        } finally { server.shutdown() }
    }
    companion object {
        const val ACCOUNT = """{"accountId":"p","mode":"paper","market":"spot","generation":3,"quoteAsset":"USDT","initialBalance":"10000","resetLimitPerUtcDay":1,"resetsUsedToday":0,"orderTypes":["market"],"priceSource":"INDEX","feeModel":"none","slippageModel":"none","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
        const val BALANCES = """{"page":1,"pageSize":20,"total":1,"items":[{"asset":"USDT","free":"10000","locked":"0","total":"10000"}],"account":"paper","mode":"paper","generation":3}"""
    }
}
