package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiFuturesContract
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.trade.BookLevel
import click.cowa.linbit.data.trade.OrderBook
import click.cowa.linbit.data.trade.TradeNav
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.math.BigDecimal

class TradeLogicTest {
    private fun bd(s: String) = BigDecimal(s)

    @Test fun parseSortsBidsDescAndAsksAsc() {
        val b = OrderBook.parse(listOf(listOf("10", "1"), listOf("12", "2")), listOf(listOf("15", "1"), listOf("13", "3")))
        assertEquals(bd("12"), b.bestBid)
        assertEquals(bd("13"), b.bestAsk)
    }

    @Test fun groupedFloorsBidsAndCeilsAsks() {
        val b = OrderBook(
            bids = listOf(BookLevel(bd("100.16"), bd("1")), BookLevel(bd("100.11"), bd("2")), BookLevel(bd("99.94"), bd("4"))),
            asks = listOf(BookLevel(bd("100.21"), bd("1")), BookLevel(bd("100.29"), bd("2")), BookLevel(bd("100.31"), bd("4"))),
        ).grouped(bd("0.1"))
        assertEquals(listOf(bd("100.1"), bd("99.9")), b.bids.map { it.price.stripTrailingZeros() })
        assertEquals(bd("3"), b.bids.first().qty)
        assertEquals(listOf(bd("100.3"), bd("100.4")), b.asks.map { it.price.stripTrailingZeros() })
        assertEquals(bd("3"), b.asks.first().qty)
    }

    @Test fun buyRatioDefaultsToHalfWithoutData() {
        assertEquals(0.5, OrderBook().buyRatio, 1e-9)
        val b = OrderBook(listOf(BookLevel(bd("1"), bd("3"))), listOf(BookLevel(bd("2"), bd("1"))))
        assertEquals(0.75, b.buyRatio, 1e-6)
    }

    @Test fun plainRoundsDown() {
        assertEquals("0.12", Dec.plain(bd("0.129"), 2))
        assertEquals("5", Dec.plain(bd("5.99"), 0))
        assertEquals(3, Dec.precision("0.001"))
        assertEquals(0, Dec.precision("1"))
    }

    @Test fun compactUsesKAndM() {
        assertEquals("41.35K", Fmt.compact(bd("41350")))
        assertEquals("2.73M", Fmt.compact(bd("2730000")))
        assertEquals("1.79K", Fmt.compact(bd("1790")))
    }

    private fun futuresVm(): FuturesTradeViewModel {
        val client = OkHttpClient()
        val api = NetworkFactory.api(client, "https://example.invalid/")
        val sock = MarketSocket(client, "wss://example.invalid/ws", "x", CoroutineScope(Dispatchers.Unconfined), { null })
        val vm = FuturesTradeViewModel(
            api, MarketStore(api, sock, CoroutineScope(Dispatchers.Unconfined)), sock, InMemoryTokenStore(), TradeNav(),
            ToastCenter(CoroutineScope(Dispatchers.Unconfined)),
        )
        vm.seedForShot(
            ApiFuturesContract("BTCUSDT", 100, "0.1", "0.001", "1000", "BTC", "USDT"),
            ApiFuturesTicker(symbol = "BTCUSDT", lastPrice = "80000", markPrice = "80010"), OrderBook(), null,
        )
        return vm
    }

    @Test fun futuresQtyFromCostAndLeverageRoundsDown() {
        val vm = futuresVm()
        vm.costText = "100"
        // 100 * 100x / 80000 = 0.125
        assertEquals(bd("0.125"), vm.qty)
        vm.costText = "100.5"
        assertEquals(bd("0.125"), vm.qty) // 0.1256 → 步长 0.001 向下取整
    }

    @Test fun futuresLiqPriceUsesLeverageAndMaintenanceRate() {
        val vm = futuresVm()
        // 价格 80000，100x，维持保证金率默认 0.004：多 80000*(1-0.01+0.004)=79520
        assertEquals(0, bd("79520").compareTo(vm.liqPrice(true)))
        assertEquals(0, bd("80480").compareTo(vm.liqPrice(false)))
    }

    @Test fun futuresValidationRequiresCostAndMinQty() {
        val vm = futuresVm()
        vm.kind = FuturesKind.MARKET
        assertEquals("请输入成本", vm.validation)
        vm.costText = "0.01"
        assertEquals("数量低于最小下单量 0.001 BTC", vm.validation)
        vm.costText = "100"
        assertEquals("可用保证金不足", vm.validation)
    }

    @Test fun futuresPositionModeBlockedWithoutOpenState() {
        assertNull(futuresVm().positionModeBlockReason)
    }
    @Test fun calculatorCostAndLiqAndPnl() {
        val cost = TradeCalculator.cost(bd("0.1"), bd("80000"), 20)
        assertEquals(0, cost.compareTo(bd("400")))
        val liqLong = TradeCalculator.liqPrice(bd("80000"), 20, true, bd("0.004"))
        // 80000 * (1 - 0.05 + 0.004) = 80000 * 0.954 = 76320
        assertEquals(0, liqLong.compareTo(bd("76320")))
        val pnl = TradeCalculator.pnl(bd("80000"), bd("82000"), bd("0.1"), true)
        assertEquals(0, pnl.compareTo(bd("200")))
        val target = TradeCalculator.targetPrice(bd("80000"), bd("0.1"), bd("200"), true)
        assertEquals(0, target.compareTo(bd("82000")))
    }

    @Test fun candleFromSpotParsesOhlc() {
        val c = Candle.fromSpot(click.cowa.linbit.core.generated.ApiKline(time = 1, open = "1", high = "3", low = "0.5", close = "2", volume = "10"))
        assertEquals(bd("1"), c.open)
        assertEquals(bd("3"), c.high)
        assertEquals(bd("0.5"), c.low)
        assertEquals(bd("2"), c.close)
    }

}

