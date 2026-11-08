package click.cowa.linbit.feature.assets

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiFuturesTodayPnl200Response
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class PnlAnalysisViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore) : ViewModel() {
    var data by mutableStateOf<ApiFuturesTodayPnl200Response?>(null); private set
    var error by mutableStateOf<String?>(null); private set
    var loading by mutableStateOf(false); private set
    init { viewModelScope.launch { tokens.sessionVersion.collect { data = null; error = null; load() } } }
    fun load() {
        val session = tokens.sessionVersion.value
        if (!tokens.loggedIn.value) { data = null; error = "请登录后查看盈亏"; loading = false; return }
        loading = true; error = null
        viewModelScope.launch {
            val response = runCatching { apiCall { api.futuresTodayPnl() } }
            if (session != tokens.sessionVersion.value) return@launch
            response.onSuccess { data = it }.onFailure { error = it.lbMessage() }
            loading = false
        }
    }
}


@kotlinx.serialization.Serializable data class ApiPnlPoint(val date: String, val pnl: String = "0", val realizedPnl: String? = null, val funding: String = "0", val fee: String = "0", val liquidationFee: String = "0", val insuranceCover: String = "0", val tradeCount: Int = 0)
@kotlinx.serialization.Serializable data class ApiPnlHistory(val asset: String = "USDT", val realizedPnlAvailable: Boolean = true, val points: List<ApiPnlPoint> = emptyList(), val totals: ApiPnlPoint? = null)
@kotlinx.serialization.Serializable data class ApiSpotPnlPoint(val date: String, val quoteVolume: String? = null, val tradeCount: Int = 0, val nonUsdtTradeCount: Int = 0, val fee: String? = null, val realizedPnl: String? = null)
@kotlinx.serialization.Serializable data class ApiSpotPnlHistory(val realizedPnlAvailable: Boolean = false, val points: List<ApiSpotPnlPoint> = emptyList(), val totals: ApiSpotPnlPoint? = null)
@kotlinx.serialization.Serializable data class ApiFiatBuyStatus(val configured: Boolean = false, val available: Boolean = false, val reason: String? = null, val quotes: Boolean = false, val orders: Boolean = false)

object PnlStats {
    fun cumulative(points: List<ApiPnlPoint>): List<java.math.BigDecimal> { var acc = java.math.BigDecimal.ZERO; return points.map { acc += Dec.parse(it.pnl); acc } }
    fun winDays(points: List<ApiPnlPoint>) = points.count { Dec.parse(it.pnl).signum() > 0 }
    fun lossDays(points: List<ApiPnlPoint>) = points.count { Dec.parse(it.pnl).signum() < 0 }
    fun trades(points: List<ApiPnlPoint>) = points.sumOf { it.tradeCount }
}

@HiltViewModel
class PnlHistoryViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore) : ViewModel() {
    var days by mutableStateOf(30); private set
    var futures by mutableStateOf<ApiPnlHistory?>(null); private set
    var spot by mutableStateOf<ApiSpotPnlHistory?>(null); private set
    var error by mutableStateOf<String?>(null); private set
    var loading by mutableStateOf(false); private set
    fun load(d: Int = days) {
        days = d
        if (!tokens.loggedIn.value) { error = "请登录后查看盈亏"; return }
        val to = java.time.LocalDate.now(java.time.ZoneOffset.UTC); val from = to.minusDays((d - 1).toLong())
        loading = true; error = null
        viewModelScope.launch {
            val f = runCatching { apiCall { api.futuresPnlHistory(from.toString(), to.toString()) } }
            val sp = runCatching { apiCall { api.spotPnlHistory(from.toString(), to.toString()) } }
            futures = f.getOrNull(); spot = sp.getOrNull(); error = f.exceptionOrNull()?.lbMessage(); loading = false
        }
    }
}

@Composable
private fun PnlCurve(values: List<java.math.BigDecimal>, modifier: Modifier) {
    val up = LB.p.up; val down = LB.p.down; val grid = LB.p.border
    androidx.compose.foundation.Canvas(modifier) {
        drawLine(grid, androidx.compose.ui.geometry.Offset(0f, size.height / 2), androidx.compose.ui.geometry.Offset(size.width, size.height / 2))
        if (values.size < 2) return@Canvas
        val f = values.map { it.toFloat() }; val max = maxOf(f.max(), 0f); val min = minOf(f.min(), 0f); val span = (max - min).takeIf { it > 0f } ?: 1f
        val path = androidx.compose.ui.graphics.Path()
        f.forEachIndexed { i, v -> val x = size.width * i / (f.size - 1); val y = size.height * (max - v) / span; if (i == 0) path.moveTo(x, y) else path.lineTo(x, y) }
        drawPath(path, if (f.last() >= 0f) up else down, style = androidx.compose.ui.graphics.drawscope.Stroke(width = 2.dp.toPx()))
    }
}

@Composable
fun PnlHistorySection(vm: PnlHistoryViewModel = hiltViewModel()) {
    LaunchedEffect(Unit) { vm.load() }
    val p = LB.p
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("历史盈亏", style = lb(FontSize.title), color = p.textPrimary)
        PrimaryTabs(listOf("7天", "30天", "90天"), listOf(7, 30, 90).indexOf(vm.days).coerceAtLeast(0), { vm.load(listOf(7, 30, 90)[it]) }, size = FontSize.label)
        when {
            vm.loading && vm.futures == null -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null -> EmptyState(vm.error!!) { ChipButton("重试") { vm.load() } }
            vm.futures != null -> {
                val h = vm.futures!!
                val cum = PnlStats.cumulative(h.points)
                Text("合约累计盈亏 " + Fmt.amount(cum.lastOrNull() ?: java.math.BigDecimal.ZERO, 4) + " " + h.asset, style = lbNum(FontSize.body, androidx.compose.ui.text.font.FontWeight.SemiBold), color = if ((cum.lastOrNull()?.signum() ?: 0) < 0) p.down else p.up)
                PnlCurve(cum, Modifier.fillMaxWidth().height(140.dp))
                Text("交易统计", style = lb(FontSize.title), color = p.textPrimary)
                KvRow("成交笔数", PnlStats.trades(h.points).toString())
                KvRow("盈利天数 / 亏损天数", "${PnlStats.winDays(h.points)} / ${PnlStats.lossDays(h.points)}")
                h.totals?.let { t -> KvRow("已实现盈亏", t.realizedPnl ?: "--"); KvRow("资金费用", t.funding); KvRow("手续费", t.fee) }
                vm.spot?.let { s -> val t = s.totals
                    KvRow("现货成交额（USDT 计价）", t?.quoteVolume ?: "--"); KvRow("现货成交笔数", ((t?.tradeCount ?: 0)).toString() + if ((t?.nonUsdtTradeCount ?: 0) > 0) "（另有 ${t?.nonUsdtTradeCount} 笔非 USDT 计价未折合）" else "")
                    if (!s.realizedPnlAvailable) Text("现货已实现盈亏暂不提供（服务端未计算成本）", style = lb(FontSize.caption), color = p.textSecondary) }
                Text("按 UTC 自然日统计；无流水的日期记为 0。", style = lb(FontSize.caption), color = p.textSecondary)
            }
        }
    }
}

@Composable
fun PnlAnalysisScreen(onBack: () -> Unit, vm: PnlAnalysisViewModel = hiltViewModel()) {
    Column(Modifier.fillMaxSize()) {
        AssetsNavBar("盈亏分析", onBack)
        Column(Modifier.fillMaxWidth().verticalScroll(androidx.compose.foundation.rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            Text("合约 · 今日", style = lb(FontSize.title), color = LB.p.textPrimary)
            Text("按交易服务端 UTC 00:00 起统计", style = lb(FontSize.caption), color = LB.p.textSecondary)
            when {
                vm.loading && vm.data == null -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                vm.error != null -> EmptyState(vm.error!!) { ChipButton("重试", onClick = vm::load) }
                vm.data != null -> {
                    val data = vm.data!!
                    listOf("总盈亏" to data.pnl, "已实现盈亏" to data.realizedPnl, "资金费用" to data.funding, "手续费" to data.fee, "强平费用" to data.liquidationFee, "风险准备金补偿" to data.insuranceCover).forEach { (label, value) ->
                        val amount = Dec.parse(value)
                        KvRow(label, (if (amount.signum() > 0) "+" else "") + Fmt.amount(amount, 4) + " " + data.asset)
                    }
                    LbButton("刷新", kind = ButtonKind.SECONDARY, onClick = vm::load)
                }
            }
            PnlHistorySection()
        }
    }
}

@HiltViewModel
class FiatBuyViewModel @Inject constructor(private val api: LinbitApi) : ViewModel() {
    var status by mutableStateOf<ApiFiatBuyStatus?>(null); private set
    var error by mutableStateOf<String?>(null); private set
    fun load() { error = null; viewModelScope.launch { runCatching { apiCall { api.fiatBuyStatus() } }.onSuccess { status = it }.onFailure { error = it.lbMessage() } } }
}

/** GET /fiat/buy 返回不可用时如实显示原因；不制造报价、支付商或订单（报价/下单接口当前固定 503 FIAT_UNAVAILABLE）。 */
@Composable
fun QuickBuyScreen(onBack: () -> Unit, vm: FiatBuyViewModel = hiltViewModel()) {
    LaunchedEffect(Unit) { vm.load() }
    Column(Modifier.fillMaxSize()) {
        AssetsNavBar("快捷买币", onBack)
        val s = vm.status
        when {
            vm.error != null -> EmptyState(vm.error!!, Modifier.padding(top = 80.dp)) { ChipButton("重试", onClick = vm::load) }
            s == null -> Box(Modifier.fillMaxWidth().padding(top = 80.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            !s.available -> EmptyState("暂无可用的支付渠道", Modifier.padding(top = 80.dp)) {
                Text(when (s.reason) { "provider_not_integrated" -> "法币支付服务商尚未接入，暂不能报价或下单。"; else -> "法币买币服务尚未开通。" }, Modifier.padding(horizontal = Spacing.pageH), style = lb(FontSize.caption), color = LB.p.textSecondary)
            }
            else -> EmptyState("暂无可用报价，请稍后重试", Modifier.padding(top = 80.dp)) { ChipButton("刷新", onClick = vm::load) }
        }
    }
}
