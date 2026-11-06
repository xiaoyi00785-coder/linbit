package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.mockwebserver.*
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk=[34],application=android.app.Application::class)
@kotlinx.coroutines.ExperimentalCoroutinesApi
class AssetsTransferFlowTest {
    private val tokens=InMemoryTokenStore()
    private lateinit var server:MockWebServer
    private lateinit var vm:AssetsViewModel
    @Before fun setup(){
        Dispatchers.setMain(UnconfinedTestDispatcher());tokens.accessToken="INVALID_TRANSFER_TOKEN"
        server=MockWebServer();server.start()
        val api=NetworkFactory.api(okhttp3.OkHttpClient(),server.url("/").newBuilder().host("127.0.0.1").build().toString())
        val scope=CoroutineScope(Dispatchers.Main)
        val market=MarketStore(api,MarketSocket(okhttp3.OkHttpClient(),"ws://127.0.0.1/","fake",scope,accessToken={null}),scope)
        vm=AssetsViewModel(api,tokens,market,ToastCenter(scope))
    }
    private fun r(ref:String,status:String,code:Int=200)=MockResponse().setResponseCode(code).setHeader("Content-Type","application/json").setBody("""{"ref":"$ref","status":"$status"}""")
    private fun balances()=MockResponse().setResponseCode(200).setHeader("Content-Type","application/json").setBody("""{"items":[{"asset":"USDT","free":"90","locked":"0","total":"90"}]}""")
    @Test fun oneInflightAndOldSessionCannotPublish()=runBlocking{
        val arrived=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse=when(req.path){
            "/futures/transfer"->{assertEquals("POST",req.method);assertEquals("{\"direction\":\"in\",\"amount\":\"10.00\"}",req.body.readUtf8());arrived.complete(Unit);runBlocking{withTimeout(5_000){release.await()}};r("OLD-SESSION","completed")}
            else->{fail("Undeclared ${req.method} ${req.path}");MockResponse().setResponseCode(599)}
        }}
        assertTrue(vm.transferFutures(true,"10.00"));assertTrue(vm.transferBusy)
        assertFalse(vm.transferFutures(true,"10.00"));withTimeout(5_000){arrived.await()}
        tokens.accessToken="OTHER_INVALID_SESSION";release.complete(Unit)
        withTimeout(5_000){while(vm.transferBusy)delay(10)}
        assertNull(vm.transferReceipt);assertNull(vm.transferError);assertEquals(1,server.requestCount)
    }
    @After fun close(){runCatching{server.shutdown()};Dispatchers.resetMain()}
}
