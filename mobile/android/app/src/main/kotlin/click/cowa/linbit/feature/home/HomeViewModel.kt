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

data class HomeBanner(val id: String, val title: String, val subtitle: String?, val imageUrl: String? = null, val link: String? = null)

data class HomeState(
    val totalUsdt: BigDecimal = BigDecimal.ZERO,
    val newcomer: ApiHomeNewcomer? = null,
    val earn: List<ApiEarnProduct> = emptyList(),
    val refreshing: Boolean = false,
    /** 运营位：活动弹窗、轮播、搜索提示（/home/placements）。 */
    val popups: List<click.cowa.linbit.core.generated.ApiPlacement> = emptyList(),
    val carousel: List<click.cowa.linbit.core.generated.ApiPlacement> = emptyList(),
    val searchHint: String? = null,
    val tradfiSymbols: Set<String> = emptySet(),
    val academy: List<click.cowa.linbit.feature.markets.ApiAcademyArticle>? = null,
    /** 新人奖励总额（/growth/config newcomer.total，兜底为任务奖励合计）。 */
    val newcomerTotal: String? = null,
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
        // 登录 / 退出 / 切换账号后先清掉旧账号数据，再刷新总资产与新手任务。
        viewModelScope.launch {
            tokens.sessionVersion.collect {
                _state.value = _state.value.copy(totalUsdt = java.math.BigDecimal.ZERO, newcomer = null)
                load(refreshMarket = false)
            }
        }
    }

    fun load(refreshMarket: Boolean = true) {
        viewModelScope.launch {
            _state.value = _state.value.copy(refreshing = true)
            coroutineScope {
                val earn = async { runCatching { apiCall { api.earnProducts() } }.getOrNull() }
                val newcomer = async { runCatching { apiCall { api.homeNewcomer() } }.getOrNull() }
                val overview = async { if (tokens.loggedIn.value) runCatching { apiCall { api.assetsOverview() } }.getOrNull() else null }
                val pl = async { runCatching { apiCall { api.homePlacements() } }.getOrNull() }
                val growth = async { runCatching { apiCall { api.growthConfig() } }.getOrNull() }
                val tf = async { runCatching { apiCall { api.tradFiAssets() } }.getOrNull() }
                val ac = async { runCatching { apiCall { api.academyArticles(limit = 5) } }.getOrNull() }
                val m = async { if (refreshMarket && market.state.value.spot.isEmpty()) market.load() }
                m.await()
                _state.value = _state.value.copy(
                    earn = earn.await()?.items?.filter { Dec.parse(it.apr).signum() > 0 }.orEmpty(),
                    newcomer = newcomer.await(),
                    totalUsdt = Dec.parse(overview.await()?.totalValueUsdt),
                    refreshing = false,
                    tradfiSymbols = tf.await()?.items.orEmpty().mapNotNull { it.symbol }.toSet(),
                    academy = ac.await()?.items ?: _state.value.academy,
                    popups = pl.await()?.slots?.get("popup").orEmpty().filter { !it.imageUrl.isNullOrBlank() },
                    carousel = pl.await()?.slots?.get("carousel").orEmpty().filter { !it.imageUrl.isNullOrBlank() },
                    searchHint = pl.await()?.slots?.get("search_hint")?.firstOrNull()?.title,
                    newcomerTotal = growth.await()?.newcomer?.total
                        ?: _state.value.newcomer?.tasks?.mapNotNull { Dec.parseOrNull(it.amount) }?.takeIf { it.isNotEmpty() }?.fold(java.math.BigDecimal.ZERO, java.math.BigDecimal::add)?.toPlainString(),
                )
            }
        }
    }

    private val _reserves = MutableStateFlow<Result<click.cowa.linbit.core.generated.ApiProofOfReserves>?>(null)
    val reserves: StateFlow<Result<click.cowa.linbit.core.generated.ApiProofOfReserves>?> = _reserves.asStateFlow()
    fun loadReserves() { _reserves.value = null; viewModelScope.launch { _reserves.value = runCatching { apiCall { api.proofOfReserves() } } } }

    fun refresh() { viewModelScope.launch { market.load() }; load(refreshMarket = false) }
}

/** 榜单：0 自选 1 热门(成交额) 2 涨幅榜 3 跌幅榜 4 成交额榜。 */
fun rankingRows(tab: Int, kind: MarketKind, m: MarketState, watch: List<String>): List<MarketRow> {
    val rows = m.rows(kind)
    return when (tab) {
        0 -> rows.filter { it.symbol in watch }
        2 -> rows.sortedByField(SortField.CHANGE, SortDir.DESC).take(7)
        3 -> rows.sortedByField(SortField.CHANGE, SortDir.ASC).take(7)
        else -> rows.sortedByField(SortField.VOLUME, SortDir.DESC).take(7)
    }
}
