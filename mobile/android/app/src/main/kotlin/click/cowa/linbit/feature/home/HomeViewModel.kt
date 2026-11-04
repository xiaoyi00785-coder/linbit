package click.cowa.linbit.feature.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiHomeNewcomer
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.SortDir
import click.cowa.linbit.data.SortField
import click.cowa.linbit.data.Watchlist
import click.cowa.linbit.data.sortedByField
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import java.math.BigDecimal
import javax.inject.Inject

data class HomeBanner(val id: String, val title: String, val subtitle: String?)

data class HomeState(
    val totalUsdt: BigDecimal = BigDecimal.ZERO,
    val newcomer: ApiHomeNewcomer? = null,
    val earn: List<ApiEarnProduct> = emptyList(),
    val banners: List<HomeBanner> = LOCAL_BANNERS,
    val refreshing: Boolean = false,
)

/** 运营位未接入前的本地兜底（与 iOS HomeViewModel.localBanners 一致）。 */
val LOCAL_BANNERS = listOf(
    HomeBanner("card", "Linbit Card", "数字资产 · 全球消费"),
    HomeBanner("redpacket", "全员红包雨火爆来袭", "惊喜好礼等你拿"),
)

/** 首页数据（对应 iOS HomeViewModel）：总资产（登录后 /assets/overview）、新手任务、理财宝、榜单（由行情本地计算）。 */
@HiltViewModel
class HomeViewModel @Inject constructor(
    private val api: LinbitApi,
    private val market: MarketStore,
    private val tokens: TokenStore,
    val watchlist: Watchlist,
) : ViewModel() {
    private val _state = MutableStateFlow(HomeState())
    val state: StateFlow<HomeState> = _state.asStateFlow()
    val market_: StateFlow<MarketState> = market.state
    val loggedIn: StateFlow<Boolean> get() = tokens.loggedIn

    init {
        load()
        // 登录 / 退出后刷新总资产与新手任务
        viewModelScope.launch { tokens.loggedIn.collect { load(refreshMarket = false) } }
    }

    fun load(refreshMarket: Boolean = true) {
        viewModelScope.launch {
            _state.value = _state.value.copy(refreshing = true)
            coroutineScope {
                val earn = async { runCatching { apiCall { api.earnProducts() } }.getOrNull() }
                val newcomer = async { runCatching { apiCall { api.homeNewcomer() } }.getOrNull() }
                val overview = async { if (tokens.loggedIn.value) runCatching { apiCall { api.assetsOverview() } }.getOrNull() else null }
                val m = async { if (refreshMarket && market.state.value.spot.isEmpty()) market.load() }
                m.await()
                _state.value = _state.value.copy(
                    earn = earn.await()?.items?.filter { Dec.parse(it.apr).signum() > 0 }.orEmpty(),
                    newcomer = newcomer.await(),
                    totalUsdt = Dec.parse(overview.await()?.totalValueUsdt),
                    refreshing = false,
                )
            }
        }
    }

    fun refresh() { viewModelScope.launch { market.load() }; load(refreshMarket = false) }
}

/** 榜单：0 自选 1 热门(成交额) 2 涨幅榜 3 跌幅榜 4 成交额榜。 */
fun rankingRows(tab: Int, kind: MarketKind, m: MarketState, watch: List<String>): List<MarketRow> {
    val rows = m.rows(kind)
    return when (tab) {
        0 -> rows.filter { it.symbol in watch }
        2 -> rows.sortedByField(SortField.CHANGE, SortDir.DESC).take(5)
        3 -> rows.sortedByField(SortField.CHANGE, SortDir.ASC).take(5)
        else -> rows.sortedByField(SortField.VOLUME, SortDir.DESC).take(5)
    }
}
