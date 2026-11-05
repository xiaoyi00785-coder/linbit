package click.cowa.linbit.feature.markets

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.Watchlist
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class MarketsViewModel @Inject constructor(
    private val store: MarketStore,
    private val watchlist: Watchlist,
) : ViewModel() {
    val state: StateFlow<MarketState> = store.state
    val watch: StateFlow<List<String>> = watchlist.symbols

    init { refreshIfEmpty() }

    private fun refreshIfEmpty() {
        if (store.state.value.spot.isEmpty()) viewModelScope.launch { store.load() }
    }

    fun refresh() { viewModelScope.launch { store.load() } }

    fun addToWatch(symbols: Collection<String>) = watchlist.add(symbols)

    fun precision(symbol: String): Int? = store.meta(symbol)?.pricePrecision
}
