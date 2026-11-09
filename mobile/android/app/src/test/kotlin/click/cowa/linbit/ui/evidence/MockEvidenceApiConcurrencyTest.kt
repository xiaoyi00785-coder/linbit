package click.cowa.linbit.ui.evidence

import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.concurrent.CyclicBarrier
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * Reproduces ChatAccessTest CI failure mode: Compose waitUntil polls api.requests while
 * MockWebServer dispatcher threads append. Root fix is CopyOnWriteArrayList in MockEvidenceApi
 * (synchronizedList iterators are not thread-safe). Not a production app concurrency bug.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class MockEvidenceApiConcurrencyTest {
    @Test fun requestLogIterationSurvivesConcurrentDispatcherAppends() {
        MockEvidenceApi().use { api ->
            val total = 160
            repeat(total) { i -> api.route("GET", "/probe/$i", "{}") }
            api.route("GET", "/chat/rooms", """{"rooms":[]}""")
            val writers = 8
            val barrier = CyclicBarrier(writers + 1)
            val httpErrors = AtomicInteger(0)
            val polls = AtomicInteger(0)
            val pool = Executors.newFixedThreadPool(writers)
            val client = OkHttpClient.Builder().build()
            repeat(writers) { w ->
                pool.execute {
                    barrier.await(10, TimeUnit.SECONDS)
                    val base = w * (total / writers)
                    repeat(total / writers) { i ->
                        val path = "/probe/${base + i}"
                        runCatching {
                            client.newCall(Request.Builder().url(api.baseUrl.trimEnd('/') + path).get().build()).execute().use { resp ->
                                if (!resp.isSuccessful) httpErrors.incrementAndGet()
                            }
                        }.onFailure { httpErrors.incrementAndGet() }
                    }
                }
            }
            barrier.await(10, TimeUnit.SECONDS)
            // Fire the waitUntil target while writers still append probe logs.
            runCatching {
                client.newCall(Request.Builder().url(api.baseUrl.trimEnd('/') + "/chat/rooms").get().build()).execute().close()
            }.onFailure { httpErrors.incrementAndGet() }
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(6)
            var sawChat = false
            while (System.nanoTime() < deadline) {
                polls.incrementAndGet()
                // Exact ChatAccessTest waitUntil predicate shape (plus count/dump).
                if (api.requests.any { it == "GET /chat/rooms" }) sawChat = true
                if (api.requests.toList().any { it == "GET /chat/rooms" }) sawChat = true
                api.requests.count { it.startsWith("GET ") }
                api.dumpRequests()
            }
            pool.shutdown()
            assertTrue(pool.awaitTermination(20, TimeUnit.SECONDS))
            assertEquals("http errors", 0, httpErrors.get())
            assertTrue("chat rooms logged", api.requests.any { it == "GET /chat/rooms" })
            assertTrue("poll observed chat rooms", sawChat)
            assertTrue("polled enough", polls.get() > 10)
        }
    }

    @Test fun requestLogIterationSurvivesConcurrentDispatcherAppendsRepeated() {
        repeat(5) { requestLogIterationSurvivesConcurrentDispatcherAppends() }
    }
}
