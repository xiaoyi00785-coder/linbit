package click.cowa.linbit.data.trade

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** 交易页的全局当前交易对（对应 iOS Router.spotSymbol / futuresSymbol）：行情页点击某行后切到对应 Tab 并带上交易对。 */
class TradeNav {
    private val _spot = MutableStateFlow("BTCUSDT")
    private val _futures = MutableStateFlow("BTCUSDT")
    /** 现货顶栏：0现货 1策略 2理财 3闪兑；消费后清空。 */
    private val _spotTop = MutableStateFlow<Int?>(null)
    val spotSymbol: StateFlow<String> = _spot.asStateFlow()
    val futuresSymbol: StateFlow<String> = _futures.asStateFlow()
    val pendingSpotTop: StateFlow<Int?> = _spotTop.asStateFlow()
    fun setSpot(symbol: String) { _spot.value = symbol }
    fun setFutures(symbol: String) { _futures.value = symbol }
    fun openSpotTop(index: Int) { _spotTop.value = index.coerceIn(0, 3) }
    fun consumeSpotTop(): Int? {
        val v = _spotTop.value
        _spotTop.value = null
        return v
    }
}
