package click.cowa.linbit.ui.evidence

import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest

/** Explicit, loopback-only mock responses. No production network or credential is allowed. */
internal class MockEvidenceApi : Dispatcher(), AutoCloseable {
    val server = MockWebServer()
    private val routes = linkedMapOf<String, MutableList<MockResponse>>()
    val requests = mutableListOf<String>()

    init { server.dispatcher = this; server.start() }

    val baseUrl: String get() = server.url("/").newBuilder().host("127.0.0.1").build().toString().also {
        check(it.startsWith("http://127.0.0.1:")) { "Evidence API must be loopback, got $it" }
    }

    fun route(method: String, pathAndQuery: String, body: String, code: Int = 200) {
        routes.getOrPut("$method $pathAndQuery") { mutableListOf() }.add(
            MockResponse().setResponseCode(code)
                .setHeader("Content-Type", "application/json")
                .setBody(body),
        )
    }

    override fun dispatch(request: RecordedRequest): MockResponse {
        val key = "${request.method} ${request.path.orEmpty()}"
        val path = request.path?.substringBefore('?').orEmpty()
        val method = request.method.orEmpty()
        if (path.endsWith("/wallet/withdraw") || path.endsWith("/wallet/internal/transfer") || path.endsWith("/convert/accept") || path.endsWith("/2fa/verify") || path.endsWith("/2fa/enable") || path.endsWith("/2fa/disable") || (path.contains("/kyc/") && method != "GET")) {
            throw AssertionError("Sensitive mutation must remain unconfirmed: $key")
        }
        requests += key
        return routes[key]?.removeFirstOrNull() ?: throw AssertionError("Evidence route must be declared explicitly: $key")
    }

    fun dumpRequests(): String = requests.joinToString("\n")

    fun toManifest(): Map<String, String> = mapOf("mock.baseUrl" to baseUrl, "mock.requestedRoutes" to requests.joinToString(";"))

    fun assertNoWrites() {
        check(requests.none { it.split(" ").first() in setOf("POST", "PUT", "PATCH", "DELETE") }) {
            "Evidence scenario must stop before a state-changing operation: $requests"
        }
    }

    override fun close() { server.shutdown() }
}
