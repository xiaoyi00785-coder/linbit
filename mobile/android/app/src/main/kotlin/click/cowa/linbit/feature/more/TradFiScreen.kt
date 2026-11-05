package click.cowa.linbit.feature.more

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiListTradFiAssets200Response
import click.cowa.linbit.core.generated.ApiTradFiAsset
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.profile.InfoNote
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject
import java.math.BigDecimal

data class TradFiState(val data: ApiListTradFiAssets200Response? = null, val loading: Boolean = true, val error: String? = null)

@HiltViewModel
class TradFiViewModel @Inject constructor(private val api: LinbitApi) : ViewModel() {
    private val _state = MutableStateFlow(TradFiState())
    val state: StateFlow<TradFiState> = _state
    var kind: String? = null

    fun load(newKind: String? = kind) {
        kind = newKind
        _state.value = _state.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { apiCall { api.tradFiAssets(kind) } }
                .onSuccess { _state.value = TradFiState(it, false) }
                .onFailure { _state.value = TradFiState(null, false, it.lbMessage()) }
        }
    }
}

@Composable
fun TradFiScreen(onBack: () -> Unit, onTrade: (Boolean, String) -> Unit, vm: TradFiViewModel = hiltViewModel()) {
    val s by vm.state.collectAsStateWithLifecycle()
    var tab by remember { mutableIntStateOf(0) }
    val kinds = listOf(null to "全部", "stock" to "股票", "metal" to "贵金属", "commodity" to "商品")
    LaunchedEffect(tab) { vm.load(kinds[tab].first) }
    MoreFrame("TradFi", onBack) {
        ChipBar(kinds.map { it.second }, tab, { tab = it })
        when {
            s.loading -> MoreLoading(Modifier.weight(1f))
            s.error != null -> MoreError(s.error ?: "", { vm.load() }, Modifier.weight(1f))
            s.data?.items.isNullOrEmpty() -> EmptyState("暂无可交易资产", Modifier.weight(1f))
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
                Row(Modifier.padding(horizontal = Spacing.pageH, vertical = 8.dp)) {
                    Text("资产", Modifier.weight(1f), style = lb(FontSize.small), color = LB.p.textTertiary)
                    Text("最新价 / 参考价", Modifier.width(118.dp), style = lb(FontSize.small), color = LB.p.textTertiary)
                    Text("偏离", Modifier.width(52.dp), style = lb(FontSize.small), color = LB.p.textTertiary)
                }
                s.data!!.items.forEach { asset ->
                    val active = asset.status != ApiTradFiAsset.Status.SUSPENDED
                    Row(Modifier.fillMaxWidth().alpha(if (active) 1f else 0.5f).pressable(enabled = active, scale = 1f) {
                        asset.symbol?.let { onTrade(asset.market == ApiTradFiAsset.Market.FUTURES, it) }
                    }.padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        CoinIcon(asset.asset ?: "?", 32.dp)
                        Spacer(Modifier.width(10.dp))
                        Column(Modifier.weight(1f)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(asset.asset ?: "--", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
                                Spacer(Modifier.width(5.dp))
                                SmallTag(if (asset.market == ApiTradFiAsset.Market.FUTURES) "永续" else "现货")
                                if (!active) { Spacer(Modifier.width(4.dp)); SmallTag("暂停", LB.p.down) }
                            }
                            Text(asset.underlying ?: asset.name ?: "", style = lb(FontSize.small), color = LB.p.textTertiary, maxLines = 1)
                        }
                        Column(Modifier.width(118.dp), horizontalAlignment = Alignment.End) {
                            Text(asset.lastPrice?.let { Fmt.priceAuto(Dec.parse(it)) } ?: "--", style = lbNum(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
                            Text(asset.referencePrice?.let { Fmt.priceAuto(Dec.parse(it)) } ?: "--", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                        }
                        Text(asset.currentDeviationBps?.let { Fmt.price(Dec.parse(it).divide(BigDecimal(100)), 2) + "%" } ?: "--", Modifier.width(52.dp), style = lbNum(FontSize.small, FontWeight.SemiBold), color = if (asset.widened == true) LB.p.down else LB.p.textSecondary)
                    }
                }
                Box(Modifier.padding(Spacing.pageH)) { InfoNote("代币化资产由发行方 1:1 托管底层资产；参考价来自永续合约市场，偏离过大时做市价差会自动放宽。部分国家或地区不提供此服务。") }
            }
        }
    }
}
