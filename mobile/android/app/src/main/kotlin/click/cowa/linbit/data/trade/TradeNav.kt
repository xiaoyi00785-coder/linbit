package click.cowa.linbit.data.trade

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** 交易页的全局当前交易对（对应 iOS Router.spotSymbol / futuresSymbol）：行情页点击某行后切到对应 Tab 并带上交易对。 */
class TradeNav {
    private val _spot = MutableStateFlow("BTCUSDT")
    private val _futures = MutableStateFlow("BTCUSDT")
    val spotSymbol: StateFlow<String> = _spot.asStateFlow()
    val futuresSymbol: StateFlow<String> = _futures.asStateFlow()
    fun setSpot(symbol: String) { _spot.value = symbol }
    fun setFutures(symbol: String) { _futures.value = symbol }
}
