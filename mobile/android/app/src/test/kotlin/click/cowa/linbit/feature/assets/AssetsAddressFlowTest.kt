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
class AssetsAddressFlowTest {
    private val tokens=InMemoryTokenStore();private lateinit var server:MockWebServer;private lateinit var vm:AssetsViewModel
    @Before fun setup(){Dispatchers.setMain(UnconfinedTestDispatcher());tokens.accessToken="INVALID_WITHDRAW_TOKEN";server=MockWebServer();server.start();val http=okhttp3.OkHttpClient();val api=NetworkFactory.api(http,server.url("/").newBuilder().host("127.0.0.1").build().toString());val scope=CoroutineScope(Dispatchers.Main);vm=AssetsViewModel(api,tokens,MarketStore(api,MarketSocket(http,"ws://127.0.0.1/","fake",scope,accessToken={null}),scope),ToastCenter(scope))}
    @Test fun createDuplicateAndOldSessionCannotPublish()=runBlocking {
        val arrived=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse {
            assertEquals("POST",req.method);assertEquals("/account/withdraw-addresses",req.path)
            val body=org.json.JSONObject(req.body.readUtf8());assertEquals("USDT",body.getString("asset"));assertEquals("INVALID-ADDRESS-BOOK-LOCAL",body.getString("address"));assertEquals("123456",body.getString("totpCode"));assertFalse(body.getBoolean("whitelisted"))
            arrived.complete(Unit);runBlocking{withTimeout(5_000){release.await()}}
            return MockResponse().setResponseCode(201).setHeader("Content-Type","application/json").setBody("""{"id":"MOCK-ADDRESS-OLD","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-BOOK-LOCAL","label":"Mock local label","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}""")
        }}
        val request=ApiCreateWithdrawAddressRequest(asset="USDT",network="TRC20",address="INVALID-ADDRESS-BOOK-LOCAL",label="Mock local label",whitelisted=false,totpCode="123456")
        assertTrue(vm.submitCreateAddressConfirmed(request));assertTrue(vm.addressBusy);withTimeout(5_000){arrived.await()}
        tokens.accessToken="OTHER_INVALID_SESSION";release.complete(Unit);withTimeout(5_000){while(vm.addressBusy)delay(10)}
        assertNull(vm.addressReceipt);assertNull(vm.addressError);assertEquals(1,server.requestCount)
    }
    @Test fun successRequiresSameIdListReadbackAndDoesNotAcceptAnotherCreate()=runBlocking {
        val created="""{"id":"MOCK-ADDRESS-NEW","asset":"USDT","network":"TRC20","address":"INVALID-ADDRESS-BOOK-LOCAL","label":"Mock local label","whitelisted":false,"createdAt":"2026-11-06T00:00:00Z"}"""
        server.dispatcher=object:Dispatcher(){override fun dispatch(req:RecordedRequest):MockResponse = when(req.path?.substringBefore('?')){
            "/account/withdraw-addresses"->MockResponse().setResponseCode(if(req.method=="POST")201 else 200).setHeader("Content-Type","application/json").setBody(if(req.method=="POST")created else """{"items":[$created]}""")
            else->MockResponse().setResponseCode(404)
        }}
        val request=ApiCreateWithdrawAddressRequest(asset="USDT",network="TRC20",address="INVALID-ADDRESS-BOOK-LOCAL",label="Mock local label",whitelisted=false,totpCode="123456")
        assertTrue(vm.submitCreateAddressConfirmed(request));withTimeout(5_000){while(vm.addressBusy)delay(10)}
        assertEquals("MOCK-ADDRESS-NEW",vm.addressReceipt?.readback?.id);assertNull(vm.addressError);assertEquals(2,server.requestCount);assertFalse(vm.submitCreateAddressConfirmed(request))
    }
    @After fun close(){runCatching{server.shutdown()};Dispatchers.resetMain()}
}
