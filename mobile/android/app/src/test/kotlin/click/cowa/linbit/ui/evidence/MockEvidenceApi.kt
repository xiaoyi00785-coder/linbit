package click.cowa.linbit.ui.evidence

import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest

internal data class MutationEvidence(val method: String?, val path: String?, val body: okio.Buffer)

/** Explicit, loopback-only mock responses. No production network or credential is allowed. */
internal class MockEvidenceApi : Dispatcher(), AutoCloseable {
    val server = MockWebServer()
    private val routes = linkedMapOf<String, MutableList<MockResponse>>()
    val requests = java.util.Collections.synchronizedList(mutableListOf<String>())
    val mutationRequests = java.util.Collections.synchronizedList(mutableListOf<MutationEvidence>())
    private val mutationStatuses = mutableListOf<Int>()
    private val expectedFailureKeys = mutableSetOf<String>()
    var defaultFeatureFlags: String? = null
    /** Test-only loopback response hold; it cannot call or configure any production host. */
    var passkeyFinishGate: java.util.concurrent.CountDownLatch? = null
    var passkeyRegistrationFinishGate: java.util.concurrent.CountDownLatch? = null
    var passkeyDeleteGate: java.util.concurrent.CountDownLatch? = null

    init { server.dispatcher = this; server.start() }

    val baseUrl: String get() = server.url("/").newBuilder().host("127.0.0.1").build().toString().also {
        check(it.startsWith("http://127.0.0.1:")) { "Evidence API must be loopback, got $it" }
    }

    fun route(method: String, pathAndQuery: String, body: String, code: Int = 200, retryAfter: Int? = null) = queue(method, pathAndQuery, body, code, failure = false, retryAfter = retryAfter)
    fun explicitFailure(method: String, pathAndQuery: String, message: String) = queue(method, pathAndQuery, """{"code":"EVIDENCE_FAILURE","message":"$message"}""", 503, failure = true)

    private fun queue(method: String, pathAndQuery: String, body: String, code: Int = 200, failure: Boolean = false, retryAfter: Int? = null) {
        val key = "$method $pathAndQuery"
        check(!failure || (method != "GET" && code >= 400)) { "Only explicit mutation failures can bypass no-write evidence routes" }
        if (failure) expectedFailureKeys += key
        routes.getOrPut("$method $pathAndQuery") { mutableListOf() }.add(
            MockResponse().setResponseCode(code)
                .setHeader("Content-Type", "application/json")
                .apply { if (retryAfter != null) setHeader("Retry-After", retryAfter) }
                .setHeader("X-Evidence-Failure", failure.toString())
                .setBody(body),
        )
    }

    var futuresTransferFailureGate: java.util.concurrent.CountDownLatch? = null

    var addressEditFailureGate: java.util.concurrent.CountDownLatch? = null

    var addressCreateFailureGate: java.util.concurrent.CountDownLatch? = null

    var withdrawSubmissionFailureGate: java.util.concurrent.CountDownLatch? = null

    var withdrawCancelFailureGate: java.util.concurrent.CountDownLatch? = null

    override fun dispatch(request: RecordedRequest): MockResponse {
        val key = "${request.method} ${request.path.orEmpty()}"
        val path = request.path?.substringBefore('?').orEmpty()
        val method = request.method.orEmpty()
        if (method != "GET") mutationRequests += MutationEvidence(request.method,request.path,request.body.clone())
        val declaredFailure = expectedFailureKeys.remove(key)
        val deletePasskey = method == "DELETE" && path.startsWith("/account/passkeys/")
        val sensitive = method != "GET" && (path.startsWith("/account/") || path.startsWith("/earn/") || path.startsWith("/strategy/") || path.contains("/wallet/") || path.endsWith("/convert/accept") || path.contains("/2fa/") || path.contains("/kyc/"))
        val passkeyRegisterPath = path in setOf("/account/passkeys/register/begin", "/account/passkeys/register/finish")
        val response = routes[key]?.removeFirstOrNull() ?: defaultFeatureFlags?.takeIf { key == "GET /app/features" }?.let { flags ->
            MockResponse().setResponseCode(200).setHeader("Content-Type", "application/json").setBody(flags)
        }
        val passkeyLogin = path in setOf("/auth/passkey/login/begin", "/auth/passkey/login/finish")
        val passkeyRegister = path in setOf("/account/passkeys/register/begin", "/account/passkeys/register/finish")
        val alertWrite = path == "/price-alerts" || path.startsWith("/price-alerts/")
        val paperBuy = method == "POST" && path == "/paper/orders"
        val futuresTransfer = method == "POST" && path == "/futures/transfer" && response != null && request.body.readUtf8() == "{\"direction\":\"in\",\"amount\":\"10.00\"}"
        val withdrawSubmit = method == "POST" && path == "/wallet/withdraw" && response != null && request.body.readUtf8() == "{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER\",\"amount\":\"10.00\",\"totpCode\":\"123456\"}"
        val withdrawCancel = method == "POST" && path == "/wallet/withdraw/MOCK-WD-CANCEL/cancel" && response != null && request.body.size == 0L
        val addressCreate = method == "POST" && path == "/account/withdraw-addresses" && response != null && request.body.readUtf8() == "{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"INVALID-ADDRESS-BOOK-LOCAL\",\"label\":\"Mock local label\",\"whitelisted\":false,\"totpCode\":\"123456\"}"
        val addressEdit = method == "PATCH" && path == "/account/withdraw-addresses/MOCK-ADDRESS-EDIT" && response != null && request.body.readUtf8() == "{\"label\":\"Mock edited label\",\"memo\":\"MEMO-LOCAL\",\"totpCode\":\"123456\"}"
        if (sensitive && !withdrawCancel && !addressEdit && !addressCreate && !withdrawSubmit && !futuresTransfer && !declaredFailure && !passkeyRegisterPath && !deletePasskey) {
            return undeclared(request, "Sensitive mutation requires explicit loopback failure")
        }
        val algoCancel = method == "DELETE" && path.matches(Regex("/futures/(plan|tpsl)-orders/[0-9]+")) && response != null
        val algoCancelFailure = algoCancel && response?.httpCode() == 503
        val expected = (declaredFailure || algoCancelFailure) && response?.httpCode() == 503
        if (method != "GET" && !expected && !paperBuy && !alertWrite && !passkeyLogin && !passkeyRegister && !deletePasskey && !algoCancel && !futuresTransfer && !withdrawSubmit && !withdrawCancel && !addressCreate && !addressEdit) {
            return undeclared(request, "Only explicitly declared loopback mock alerts, paper orders, or failure routes are allowed")
        }
        synchronized(requests) { requests += "$key${if (method != "GET") " [${if (expected) "EXPECTED_LOOPBACK_FAILURE" else "DECLARED_LOOPBACK_MOCK"}]" else ""}" }
        if (method != "GET") mutationStatuses += response?.httpCode() ?: 599
        if (paperBuy && response != null) {
            val payload = response.getBody()?.clone()?.readUtf8() ?: throw AssertionError("Paper response required")
            val data = org.json.JSONObject(request.body.clone().readUtf8()).getString("clientOrderId")
            val fulfilled = payload.replace("MOCK_CLIENT_ID", data.replace("\\", "\\\\").replace("\"", "\\\""))
            return response.clone().setBody(fulfilled)
        }
        if (deletePasskey) {
            check(passkeyDeleteGate?.await(5, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback delete latch timed out" }
        }
        if (path == "/account/passkeys/register/finish") {
            check(passkeyRegistrationFinishGate?.await(5, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback register finish latch timed out" }
        }
        if (path == "/auth/passkey/login/finish") {
            check(passkeyFinishGate?.await(5, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback finish latch timed out" }
        }
        if (addressEdit && response?.httpCode() == 503) { check(addressEditFailureGate?.await(15, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback address PATCH latch timed out" } }
        if (addressCreate && response?.httpCode() == 503) { check(addressCreateFailureGate?.await(15, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback address failure latch timed out" } }
        if (withdrawCancel && response?.httpCode() == 503) { check(withdrawCancelFailureGate?.await(15,java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback withdraw cancel latch timed out" } }
        if (withdrawSubmit && response?.httpCode() == 503) { check(withdrawSubmissionFailureGate?.await(15, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback withdrawal failure latch timed out" } }
        if (futuresTransfer && response?.httpCode() == 503) {
            check(futuresTransferFailureGate?.await(5, java.util.concurrent.TimeUnit.SECONDS) != false) { "Loopback transfer failure latch timed out" }
        }
        return response ?: undeclared(request, "Evidence route must be declared explicitly")
    }

    private fun undeclared(request: RecordedRequest, reason: String): MockResponse {
        synchronized(requests) { requests += "${request.method} ${request.path} [REJECTED:$reason]" }
        return MockResponse().setResponseCode(599).setHeader("Content-Type", "application/json").setHeader("Connection", "close").setBody("""{"code":"EVIDENCE_ROUTE_REJECTED","message":"${request.method} ${request.path}"}""")
    }

    private fun MockResponse.httpCode(): Int = status.removePrefix("HTTP/1.1 ").substringBefore(' ').toInt()


    fun dumpRequests(): String = synchronized(requests) { requests.toList() }.joinToString("\n")

    fun toManifest(): Map<String, String> = mapOf("mock.baseUrl" to baseUrl, "mock.requestedRoutes" to synchronized(requests) { requests.toList() }.joinToString(";"))

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

    fun lastMutationBody(): String = synchronized(mutationRequests) { mutationRequests.last().body.clone().readUtf8() }

    fun assertNoWrites() {
        check(requests.none { it.split(" ").first() in setOf("POST", "PUT", "PATCH", "DELETE") }) {
            "Evidence scenario must stop before a state-changing operation: $requests"
        }
    }

    override fun close() { server.shutdown() }
}
