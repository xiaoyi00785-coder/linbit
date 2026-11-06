package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.data.MarketStore
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.mockwebserver.*
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.concurrent.TimeUnit

/** Real production VM through real Retrofit into an explicit fake-token loopback server. */
@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk=[34],application=android.app.Application::class)
@kotlinx.coroutines.ExperimentalCoroutinesApi
class TradeHistoryAlgoFlowTest {
    private val tokens=InMemoryTokenStore()
    private lateinit var server:MockWebServer
    private lateinit var vm:TradeHistoryViewModel
    private fun response(body:String,code:Int=200)=MockResponse().setResponseCode(code).setHeader("Content-Type","application/json").setBody(body)
    private fun list(symbol:String,id:Long,next:Long?=null)="""{"orders":[{"algoId":$id,"symbol":"$symbol","algoType":"tp","status":"active"}],"nextCursor":$next}"""
    @Before fun setup(){
        Dispatchers.setMain(UnconfinedTestDispatcher());tokens.accessToken="INVALID_LOOPBACK_TOKEN"
        server=MockWebServer();server.start()
        val api=NetworkFactory.api(okhttp3.OkHttpClient(),server.url("/").newBuilder().host("127.0.0.1").build().toString())
        val scope=CoroutineScope(Dispatchers.Main)
        val socket=MarketSocket(okhttp3.OkHttpClient(),"ws://127.0.0.1/","fake",scope,accessToken={null})
        vm=TradeHistoryViewModel(api,tokens,MarketStore(api,socket,scope))
    }
    private suspend fun idle()=withTimeout(5_000){while(vm.loading)delay(10)}
    private suspend fun action()=withTimeout(5_000){while(vm.actionBusy)delay(10)}
    private class Held(private val response:MockResponse){
        val arrived=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        fun dispatch():MockResponse {arrived.complete(Unit);return runBlocking{withTimeout(5_000){release.await()};response}}
    }
    private fun request():RecordedRequest { val r=server.takeRequest(5,TimeUnit.SECONDS);assertNotNull("Expected explicit loopback request",r);return r!! }

    @Test fun obsoleteFilterCannotPublishAfterLatestHistoryFilterCompletes(){runBlocking {
        val old=Held(response(list("OLD",7)));val fresh=Held(response(list("NEW",8)))
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse=when(req.path){
            "/futures/plan-orders?status=active&limit=50"->old.dispatch()
            "/futures/tpsl-orders?status=history&limit=50"->fresh.dispatch()
            else->{fail("Undeclared read ${req.method} ${req.path}");MockResponse().setResponseCode(599)}
        }}
        vm.loadFutures(5,null,algoPlan=true);withTimeout(5_000){old.arrived.await()}
        vm.loadFutures(5,null,algoPlan=false,algoHistory=true);withTimeout(5_000){fresh.arrived.await()}
        fresh.release.complete(Unit);old.release.complete(Unit)
        withTimeout(5_000){while(vm.loading||vm.futAlgos.singleOrNull()?.symbol!="NEW")delay(10)}
        assertEquals("NEW",vm.futAlgos.single().symbol)
    }}
    @Test fun duplicateCancelIsOneWriteAndOneEffectiveSuccessCallback(){runBlocking {
        server.enqueue(response(list("CURRENT-TPSL",9)))
        vm.loadFutures(5,null,algoPlan=false);idle();request()
        val pending=ArrayDeque<Held>()
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse{
            assertEquals("DELETE",req.method);assertEquals("/futures/tpsl-orders/9",req.path)
            return pending.removeFirst().dispatch()
        }}
        val failure=Held(response("""{"code":"EVIDENCE_FAILURE","message":"mock failure"}""",503));pending+=failure
        var done=0;vm.cancelAlgo(9,false){done++};assertTrue(vm.actionBusy)
        vm.cancelAlgo(9,false){done++};withTimeout(5_000){failure.arrived.await()}
        failure.release.complete(Unit);action()
        assertEquals("Failure must not invoke success readback",0,done);assertNotNull(vm.actionError)
        val success=Held(response("{}"));pending+=success
        vm.cancelAlgo(9,false){done++};assertTrue(vm.actionBusy)
        vm.cancelAlgo(9,false){done++};withTimeout(5_000){success.arrived.await()}
        assertEquals("No success callback before response",0,done)
        success.release.complete(Unit);action()
        assertEquals(1,done);assertNull(vm.actionError)
        assertEquals(3,server.requestCount)
        repeat(2){val req=request();assertEquals("DELETE",req.method);assertEquals("/futures/tpsl-orders/9",req.path)}
    }}
    @Test fun paginationCannotAppendIntoNewFilterOrSession(){runBlocking {
        val nextPage=Held(response(list("STALE-PAGE",8,78)))
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse=when(req.path){
            "/futures/tpsl-orders?status=active&limit=50"->response(list("BASE",9,77))
            "/futures/tpsl-orders?status=active&cursor=77&limit=50"->nextPage.dispatch()
            "/futures/tpsl-orders?status=history&limit=50"->response(list("NEW",10))
            else->{fail("Undeclared read ${req.method} ${req.path}");MockResponse().setResponseCode(599)}
        }}
        vm.loadFutures(5,null,algoPlan=false);idle();assertEquals(77L,vm.algoCursor)
        vm.loadMoreAlgos(null,algoPlan=false,algoHistory=false);withTimeout(5_000){nextPage.arrived.await()}
        vm.loadFutures(5,null,algoPlan=false,algoHistory=true)
        nextPage.release.complete(Unit)
        withTimeout(5_000){while(vm.loading||vm.futAlgos.singleOrNull()?.symbol!="NEW")delay(10)}
        assertEquals("NEW",vm.futAlgos.single().symbol);assertFalse(vm.loadingMore)
    }}
    @Test fun oldSessionCannotPublishIntoNewSession(){runBlocking {
        val old=Held(response(list("OLD-SESSION",7)))
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse{
            assertEquals("/futures/tpsl-orders?status=active&limit=50",req.path)
            if(server.requestCount==1)return old.dispatch()
            return response(list("NEW-SESSION",8))
        }}
        vm.loadFutures(5,null,algoPlan=false);withTimeout(5_000){old.arrived.await()}
        tokens.accessToken="ANOTHER_INVALID_LOOPBACK_SESSION";vm.loadFutures(5,null,algoPlan=false)
        old.release.complete(Unit)
        withTimeout(5_000){while(vm.loading||vm.futAlgos.singleOrNull()?.symbol!="NEW-SESSION")delay(10)}
        assertEquals("NEW-SESSION",vm.futAlgos.single().symbol)
    }}
    @After fun close(){server.shutdown();Dispatchers.resetMain()}
}
