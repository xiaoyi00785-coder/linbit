package click.cowa.linbit.core.storage

import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.realtime.WsTicker
import click.cowa.linbit.feature.more.bonusAmount
import click.cowa.linbit.feature.trade.CHART_INTERVALS
import kotlinx.serialization.json.Json
import org.junit.*
import org.junit.Assert.*
import java.math.BigDecimal

class CompanionContractTest {
    private val json=Json { ignoreUnknownKeys=true }
    @After fun reset() { QuoteDisplay.applyRate(null);QuoteDisplay.currency="USD";QuoteDisplay.rates=emptyMap() }
    @Test fun independentBonusDecimalAmountsAndMissingValues() {
        val b=json.decodeFromString(ApiFuturesBonus200Response.serializer(),"""{"available":{"trial":"12345678901234567890.12345678","deduction":null},"items":[]}""")
        assertEquals("12345678901234567890.12345678",bonusAmount(b.available?.trial));assertEquals("--",bonusAmount(b.available?.deduction))
        assertEquals("0",bonusAmount("0.00000000"));assertEquals("--",bonusAmount(null));assertEquals("--",bonusAmount("bad"));assertEquals("--",bonusAmount("-1"))
        assertNull(json.decodeFromString(ApiFuturesBonus200Response.serializer(),"{}").available)
    }
    @Test fun claimableRemainsStringCount() {
        val n=json.decodeFromString(ApiHomeNewcomer.serializer(),"""{"claimable":"1","completed":0}""")
        assertEquals("1",n.claimable);assertEquals(0,n.completed)
    }
    @Test fun twelvePeriodsMatchGeneratedContractAndNoMonthOrEventTenMinutes() {
        val expected=listOf("1m","3m","5m","15m","30m","1h","2h","4h","6h","12h","1d","1w")
        assertEquals(expected,ApiKlineInterval.entries.map { it.value });assertEquals(expected,CHART_INTERVALS.map { it.first })
        assertFalse(CHART_INTERVALS.any { it.first=="1M" || it.first=="10m" })
    }
    @Test fun fullUsdMetadataIdenticalAcrossRestAndWs() {
        val payload="""{"symbol":"BTCUSDT","lastPrice":"100","priceChangePercent":"0","high":"1","low":"1","volume":"0","quoteVolume":"0","usd":{"available":true,"price":"99.90000000","rate":"0.99900000","source":"example.invalid","updatedAt":"2026-01-01T12:00:00Z","stale":true,"precision":8,"rounding":"half_away_from_zero","convention":"usdPrice = lastPrice * rate","pegAssumed":false}}"""
        val r=json.decodeFromString(ApiTicker.serializer(),payload).usd!!;val w=json.decodeFromString(WsTicker.serializer(),payload).usd!!
        assertEquals(r,w);assertEquals("example.invalid",r.source);assertEquals("2026-01-01T12:00:00Z",r.updatedAt);assertEquals(8,r.precision);assertEquals("half_away_from_zero",r.rounding);assertEquals(false,r.pegAssumed);assertNotNull(r.convention)
        QuoteDisplay.applyTickerRate(w);assertEquals(r.source,QuoteDisplay.usdRate?.source);assertEquals(r.updatedAt,QuoteDisplay.usdRate?.updatedAt)
    }
    @Test fun noValidRateNeverAssumesPegAndExplicitUnavailableOverridesCachedRate() {
        QuoteDisplay.applyRate(ApiUsdRate(available=true,rate="0.999",source="example.invalid",stale=false,pegAssumed=false))
        assertEquals("$99.90",QuoteDisplay.format(BigDecimal("100")))
        assertEquals("$--",QuoteDisplay.formatQuote(BigDecimal("100"),null,2,ApiWsUsd(available=false,reason="rate_unavailable")))
        assertEquals("$--",QuoteDisplay.formatQuote(BigDecimal("100"),null,2,null,"BTCUSDC"))
        QuoteDisplay.applyRate(ApiUsdRate(available=false,rate=null));assertEquals("$--",QuoteDisplay.format(BigDecimal("100")))
        QuoteDisplay.applyRate(ApiUsdRate(available=true,rate="1",pegAssumed=true));assertNull(QuoteDisplay.usdPerUsdt)
    }
    @Test fun usdRateSchemaRetainsPrecisionUnitAndUnavailableNulls() {
        val u=json.decodeFromString(ApiUsdRate.serializer(),"""{"pair":"USDTUSD","available":false,"rate":null,"source":"","updatedAt":null,"stale":false,"precision":8,"rounding":"half_away_from_zero","unit":"USD per 1 USDT","pegAssumed":false}""")
        assertFalse(u.available);assertNull(u.rate);assertNull(u.updatedAt);assertEquals(8,u.precision);assertEquals("USD per 1 USDT",u.unit)
    }
    @Test fun fallbackUsesPublishedEightDecimalHalfAwayConventionBeforeDisplayRounding() {
        QuoteDisplay.applyRate(ApiUsdRate(available=true,rate="1",precision=8,rounding="half_away_from_zero",pegAssumed=false))
        assertEquals("$100.01",QuoteDisplay.format(BigDecimal("100.004999996")))
        assertEquals("$-100.01",QuoteDisplay.format(BigDecimal("-100.004999996")))
    }


    @Test fun notificationLinkIsSupportTicketOnlyAndNotUrl() {
        val with=json.decodeFromString(ApiNotification.serializer(),"""{"id":"1","kind":"system","title":"t","body":"b","link":{"type":"support_ticket","ticketId":"42"}}""")
        assertEquals(ApiNotificationLink.Type.SUPPORT_TICKET,with.link?.type);assertEquals("42",with.link?.ticketId)
        val legacy=json.decodeFromString(ApiNotification.serializer(),"""{"id":"2","kind":"system","title":"旧通知","body":"工单 99"}""")
        assertNull(legacy.link)
        assertNull(click.cowa.linbit.feature.more.supportTicketId(legacy))
        assertEquals("42",click.cowa.linbit.feature.more.supportTicketId(with))
    }

}
