package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.mockwebserver.*
import org.junit.*
import org.junit.Assert.*
import org.robolectric.RobolectricTestRunner

@org.junit.runner.RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk=[34],application=android.app.Application::class)
@kotlinx.coroutines.ExperimentalCoroutinesApi
class AssetsWithdrawFlowTest {
    private val tokens=InMemoryTokenStore();private lateinit var server:MockWebServer;private lateinit var vm:AssetsViewModel
    @Before fun setup(){Dispatchers.setMain(UnconfinedTestDispatcher());tokens.accessToken="INVALID_WITHDRAW_TOKEN";server=MockWebServer();server.start();val http=okhttp3.OkHttpClient();val api=NetworkFactory.api(http,server.url("/").newBuilder().host("127.0.0.1").build().toString());val scope=CoroutineScope(Dispatchers.Main);vm=AssetsViewModel(api,tokens,MarketStore(api,MarketSocket(http,"ws://127.0.0.1/","fake",scope,accessToken={null}),scope),ToastCenter(scope))}
    @Test fun duplicateIsRejectedAndOldSessionCannotPublish()=runBlocking{
        val arrived=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse {
            assertEquals("/wallet/withdraw",req.path);assertEquals("POST",req.method)
            val body=org.json.JSONObject(req.body.readUtf8())
            assertEquals("USDT",body.getString("asset"));assertEquals("TRC20",body.getString("network"))
            assertEquals("INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER",body.getString("address"));assertEquals("10.00",body.getString("amount"))
            assertEquals("123456",body.getString("totpCode"));assertEquals(setOf("asset","network","address","amount","totpCode"),body.keys().asSequence().toSet())
            arrived.complete(Unit);runBlocking{withTimeout(5_000){release.await()}}
            return MockResponse().setResponseCode(202).setHeader("Content-Type","application/json").setBody("""{"id":"MOCK-WD-OLD","asset":"USDT","network":"TRC20","address":"INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER","amount":"10.00","fee":"0.10","status":"pending","createdAt":"2026-11-06T00:00:00Z"}""")
        }}
        val request=ApiWithdrawRequest("USDT","TRC20","INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER","10.00",null,"123456",null,null)
        assertTrue(vm.submitWithdrawConfirmed(request));assertTrue(vm.withdrawBusy);assertFalse(vm.submitWithdrawConfirmed(request));withTimeout(5_000){arrived.await()}
        tokens.accessToken="OTHER_INVALID_SESSION";release.complete(Unit);withTimeout(5_000){while(vm.withdrawBusy)delay(10)}
        assertNull(vm.withdrawReceipt);assertNull(vm.withdrawError);assertEquals(1,server.requestCount)
    }
    @After fun close(){runCatching{server.shutdown()};Dispatchers.resetMain()}
}
