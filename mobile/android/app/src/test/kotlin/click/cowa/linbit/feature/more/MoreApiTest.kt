package click.cowa.linbit.feature.more

import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonObject
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class MoreApiTest {
    @Test fun `跟单广场与我的跟单保留扩展字段`() = runTest {
        val server = MockWebServer()
        server.enqueue(MockResponse().setBody("""{"traders":2,"minAmount":"75"}"""))
        server.enqueue(MockResponse().setBody("""{"traders":[{"traderId":"T1","nickname":"真实交易员","roi":"0.25","curve":[]}]}"""))
        server.enqueue(MockResponse().setBody("""{"follows":[{"follow":{"id":9,"status":"active"},"traderNickname":"真实交易员","pnl":"3.25"}]}"""))
        server.start()
        try {
            val api = NetworkFactory.api(NetworkFactory.bareClient(), server.url("/").toString())
            assertEquals("75", api.copySummary().jsonObject.str("minAmount"))
            assertEquals("真实交易员", api.copyTraders().traders?.first()?.str("nickname"))
            val follow = api.copyFollows().jsonObject.array("follows").first()
            assertEquals("真实交易员", follow.str("traderNickname"))
            assertEquals(9L, follow.obj("follow").long("id"))
        } finally { server.shutdown() }
    }

    @Test fun `通知卡券TradFi工单请求使用契约路径`() = runTest {
        val server = MockWebServer()
        server.enqueue(MockResponse().setBody("""{"items":[],"total":0,"unread":0}"""))
        server.enqueue(MockResponse().setBody("""{"items":[]}"""))
        server.enqueue(MockResponse().setBody("""{"items":[]}"""))
        server.enqueue(MockResponse().setBody("""{"items":[],"total":0,"page":1,"pageSize":20}"""))
        server.start()
        try {
            val api = NetworkFactory.api(NetworkFactory.bareClient(), server.url("/").toString())
            api.notifications("security", 2)
            assertEquals("/notifications?kind=security&page=2&pageSize=20", server.takeRequest().path)
            api.coupons("issued")
            assertEquals("/coupons?status=issued", server.takeRequest().path)
            api.tradFiAssets("metal")
            assertEquals("/tradfi/assets?kind=metal", server.takeRequest().path)
            api.supportTickets("open")
            assertEquals("/support/tickets?status=open&page=1&pageSize=20", server.takeRequest().path)
        } finally { server.shutdown() }
    }
}
