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
    val mutationRequests = mutableListOf<RecordedRequest>()
    private val mutationStatuses = mutableListOf<Int>()
    private val expectedFailureKeys = mutableSetOf<String>()
    var defaultFeatureFlags: String? = null

    init { server.dispatcher = this; server.start() }

    val baseUrl: String get() = server.url("/").newBuilder().host("127.0.0.1").build().toString().also {
        check(it.startsWith("http://127.0.0.1:")) { "Evidence API must be loopback, got $it" }
    }

    fun route(method: String, pathAndQuery: String, body: String, code: Int = 200) = queue(method, pathAndQuery, body, code, failure = false)
    fun explicitFailure(method: String, pathAndQuery: String, message: String) = queue(method, pathAndQuery, """{"code":"EVIDENCE_FAILURE","message":"$message"}""", 503, failure = true)

    private fun queue(method: String, pathAndQuery: String, body: String, code: Int = 200, failure: Boolean = false) {
        val key = "$method $pathAndQuery"
        check(!failure || (method != "GET" && code >= 400)) { "Only explicit mutation failures can bypass no-write evidence routes" }
        if (failure) expectedFailureKeys += key
        routes.getOrPut("$method $pathAndQuery") { mutableListOf() }.add(
            MockResponse().setResponseCode(code)
                .setHeader("Content-Type", "application/json")
                .setHeader("X-Evidence-Failure", failure.toString())
                .setBody(body),
        )
    }

    override fun dispatch(request: RecordedRequest): MockResponse {
        val key = "${request.method} ${request.path.orEmpty()}"
        val path = request.path?.substringBefore('?').orEmpty()
        val method = request.method.orEmpty()
        if (method != "GET") mutationRequests += request
        val declaredFailure = expectedFailureKeys.remove(key)
        val sensitive = method != "GET" && (path.startsWith("/account/") || path.startsWith("/earn/") || path.startsWith("/strategy/") || path.contains("/wallet/") || path.endsWith("/convert/accept") || path.contains("/2fa/") || path.contains("/kyc/"))
        if (sensitive && !declaredFailure) {
            return undeclared(request, "Sensitive mutation requires explicit loopback failure")
        }
        val response = routes[key]?.removeFirstOrNull() ?: defaultFeatureFlags?.takeIf { key == "GET /app/features" }?.let { flags ->
            MockResponse().setResponseCode(200).setHeader("Content-Type", "application/json").setBody(flags)
        }
        val paperBuy = method == "POST" && path == "/paper/orders"
        val expected = declaredFailure && response?.httpCode() == 503
        if (method != "GET" && !expected && !paperBuy) {
            return undeclared(request, "Only a declared Paper order or explicit loopback failure is allowed")
        }
        requests += "$key${if (method != "GET") " [${if (declaredFailure) "EXPECTED_LOOPBACK_FAILURE" else "DECLARED_PAPER_ORDER"}]" else ""}"
        if (method != "GET") mutationStatuses += response?.httpCode() ?: 599
        if (paperBuy && response != null) {
            val payload = response.getBody()?.clone()?.readUtf8() ?: throw AssertionError("Paper response required")
            val data = org.json.JSONObject(request.body.clone().readUtf8()).getString("clientOrderId")
            val fulfilled = payload.replace("MOCK_CLIENT_ID", data.replace("\\", "\\\\").replace("\"", "\\\""))
            return response.clone().setBody(fulfilled)
        }
        return response ?: undeclared(request, "Evidence route must be declared explicitly")
    }

    private fun undeclared(request: RecordedRequest, reason: String): MockResponse {
        synchronized(requests) { requests += "${request.method} ${request.path} [REJECTED:$reason]" }
        return MockResponse().setResponseCode(599).setHeader("Content-Type", "application/json").setHeader("Connection", "close").setBody("""{"code":"EVIDENCE_ROUTE_REJECTED","message":"${request.method} ${request.path}"}""")
    }

    private fun MockResponse.httpCode(): Int = status.removePrefix("HTTP/1.1 ").substringBefore(' ').toInt()


    fun dumpRequests(): String = requests.joinToString("\n")

    fun toManifest(): Map<String, String> = mapOf("mock.baseUrl" to baseUrl, "mock.requestedRoutes" to requests.joinToString(";"))

    fun assertOnlyExpectedFailures() {
        check(mutationStatuses.isNotEmpty()) { "Expected an explicitly failed loopback mutation" }
        val accepted = mutationStatuses.all { it == 503 }
        check(accepted && requests.none { it.contains("[REJECTED]") || it.contains("[REJECTED:") }) { "Only declared failures allowed: requests=$requests statuses=$mutationStatuses" }
        check(expectedFailureKeys.isEmpty()) { "Declared failure routes not consumed: $expectedFailureKeys" }
    }

    fun assertOnlyDeclaredWrites() {
        check(mutationStatuses.isNotEmpty()) { "Expected a declared loopback Paper order" }
        check(mutationStatuses.all { it == 200 } && requests.none { it.contains("[REJECTED:") }) { "Only declared successful Paper writes allowed: $requests statuses=$mutationStatuses" }
        check(expectedFailureKeys.isEmpty()) { "Unexpected declared failure routes: $expectedFailureKeys" }
    }

    fun lastMutationBody(): String = mutationRequests.last().body.clone().readUtf8()

    fun assertNoWrites() {
        check(requests.none { it.split(" ").first() in setOf("POST", "PUT", "PATCH", "DELETE") }) {
            "Evidence scenario must stop before a state-changing operation: $requests"
        }
    }

    override fun close() { server.shutdown() }
}
