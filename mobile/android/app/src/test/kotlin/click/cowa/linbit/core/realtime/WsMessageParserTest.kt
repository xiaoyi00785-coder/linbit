package click.cowa.linbit.core.realtime

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WsMessageParserTest {
    @Test fun parsesTickerFrameKeepingDecimalStrings() {
        val raw = """{"channel":"ticker:BTCUSDT","ts":1730000000123,"data":{"symbol":"BTCUSDT","lastPrice":"80991.90000000","priceChangePercent":"0.62","quoteVolume":"1294000000.5","extra":1}}"""
        val f = WsMessageParser.parse(raw) as WsInbound.Frame
        assertEquals("ticker:BTCUSDT", f.channel)
        assertEquals(1730000000123L, f.ts)
        val t = WsMessageParser.ticker(f)!!
        assertEquals("BTCUSDT", t.symbol)
        assertEquals("80991.90000000", t.lastPrice) // 尾部 0 保留，不经 Double
        assertEquals("0.62", t.priceChangePercent)
        assertEquals("1294000000.5", t.quoteVolume)
    }

    @Test fun parsesFuturesTickerWithMissingFields() {
        val f = WsMessageParser.parse("""{"channel":"futures-ticker:ETHUSDT","data":{"symbol":"ETHUSDT","markPrice":"2349.39"}}""") as WsInbound.Frame
        val t = WsMessageParser.futuresTicker(f)!!
        assertEquals("ETHUSDT", t.symbol)
        assertNull(t.lastPrice)
        assertEquals("2349.39", t.markPrice)
    }

    @Test fun parsesEvents() {
        val ok = WsMessageParser.parse("""{"event":"subscribed","channel":"ticker:BTCUSDT","id":"android-1"}""") as WsInbound.Event
        assertEquals("subscribed", ok.event); assertEquals("ticker:BTCUSDT", ok.channel); assertEquals("android-1", ok.id)
        val err = WsMessageParser.parse("""{"event":"error","code":"BAD_CHANNEL","message":"unknown channel"}""") as WsInbound.Event
        assertEquals("BAD_CHANNEL", err.code); assertEquals("unknown channel", err.message)
        assertTrue(WsMessageParser.parse("""{"event":"pong"}""") is WsInbound.Event)
    }

    @Test fun ignoresGarbage() {
        assertEquals(WsInbound.Ignored, WsMessageParser.parse("not json"))
        assertEquals(WsInbound.Ignored, WsMessageParser.parse("[1,2,3]"))
        assertEquals(WsInbound.Ignored, WsMessageParser.parse("""{"foo":"bar"}"""))
        assertEquals(WsInbound.Ignored, WsMessageParser.parse("""{"channel":"ticker:BTCUSDT"}""")) // 缺 data
    }

    @Test fun badTickerDataDecodesToNull() {
        val f = WsMessageParser.parse("""{"channel":"ticker:X","data":{"symbol":"X"}}""") as WsInbound.Frame
        assertNull(WsMessageParser.ticker(f)) // 缺必填 lastPrice
    }

    @Test fun encodesRequests() {
        val sub = Json.parseToJsonElement(WsMessageParser.encode("subscribe", listOf("ticker:BTCUSDT", "futures-ticker:BTCUSDT"), "android-7")).jsonObject
        assertEquals("subscribe", sub["op"]!!.jsonPrimitive.content)
        assertEquals("android-7", sub["id"]!!.jsonPrimitive.content)
        assertEquals(listOf("ticker:BTCUSDT", "futures-ticker:BTCUSDT"), sub["args"]!!.jsonArray.map { it.jsonPrimitive.content })
        val ping = Json.parseToJsonElement(WsMessageParser.encode("ping", null, "p1")).jsonObject
        assertNotNull(ping["op"]); assertNull(ping["args"])
    }
}
