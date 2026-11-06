package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiAcceptConvertQuoteRequest
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.core.network.ApiEarnSubscribeRequest
import click.cowa.linbit.core.generated.ApiStrategyStopGridRequest
import click.cowa.linbit.core.generated.ApiStrategyCopyRequest
import click.cowa.linbit.core.generated.ApiEarnRedeemRequest
import click.cowa.linbit.core.generated.ApiConvertPairs
import click.cowa.linbit.core.generated.ApiConvertQuote
import click.cowa.linbit.core.generated.ApiConvertQuoteRequest
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiEarnSummary200Response
import click.cowa.linbit.core.generated.ApiGridBot
import click.cowa.linbit.core.generated.ApiGridSquareItem
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.KvRow
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.NumberBox
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import java.math.BigDecimal
import javax.inject.Inject

@HiltViewModel
class SpotSubpagesViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    val loggedIn get() = tokens.loggedIn
    val sessionVersion get() = tokens.sessionVersion

    var earnProducts by mutableStateOf<List<ApiEarnProduct>>(emptyList()); private set
    var earnSummary by mutableStateOf<ApiEarnSummary200Response?>(null); private set
    var square by mutableStateOf<List<ApiGridSquareItem>>(emptyList()); private set
    var myGrids by mutableStateOf<List<ApiGridBot>>(emptyList()); private set
    var convertPairs by mutableStateOf<ApiConvertPairs?>(null); private set
    var quote by mutableStateOf<ApiConvertQuote?>(null); private set
    var loading by mutableStateOf(false); private set
    var error by mutableStateOf<String?>(null); private set
    var quoting by mutableStateOf(false); private set
    var acting by mutableStateOf(false); private set

    fun clearAccountData() {
        earnSummary = null
        myGrids = emptyList()
        quote = null
    }

    fun loadEarn() {
        viewModelScope.launch {
            loading = true; error = null
            try {
                earnProducts = apiCall { api.earnProducts() }.items ?: emptyList()
                if (tokens.loggedIn.value) earnSummary = runCatching { apiCall { api.earnSummary() } }.getOrNull()
            } catch (e: Throwable) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    fun loadStrategy() {
        viewModelScope.launch {
            loading = true; error = null
            try {
                square = apiCall { api.strategySquare() }.items ?: emptyList()
                if (tokens.loggedIn.value) myGrids = apiCall { api.strategyGrids() }.items ?: emptyList()
            } catch (e: Throwable) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    fun loadConvert() {
        viewModelScope.launch {
            loading = true; error = null
            try { convertPairs = apiCall { api.convertPairs() } }
            catch (e: Throwable) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    fun requestQuote(from: String, to: String, amount: String, onDone: (Boolean) -> Unit = {}) {
        if (!tokens.loggedIn.value) { toast.show("请先登录"); onDone(false); return }
        quoting = true
        viewModelScope.launch {
            try {
                quote = apiCall { api.convertQuote(ApiConvertQuoteRequest(from = from, to = to, fromAmount = amount)) }
                onDone(true)
            } catch (e: Throwable) { toast.error(e); onDone(false) }
            finally { quoting = false }
        }
    }

    fun acceptQuote(onDone: (Boolean) -> Unit = {}) {
        val q = quote ?: return
        val id = q.quoteId ?: run { toast.show("报价无效"); return }
        quoting = true
        viewModelScope.launch {
            try {
                apiCall { api.acceptConvertQuote(ApiAcceptConvertQuoteRequest(quoteId = id)) }
                toast.show("闪兑成功", ToastCenter.Kind.SUCCESS); quote = null; onDone(true)
            } catch (e: Throwable) { toast.error(e); onDone(false) }
            finally { quoting = false }
        }
    }

    /** POST /strategy/grid/square/{id}/copy */
    fun copyStrategy(id: String, investment: String, onDone: (Boolean) -> Unit = {}) {
        val inv = investment.trim()
        if (Dec.parse(inv).signum() <= 0) { toast.show("请输入投资额", ToastCenter.Kind.ERROR); onDone(false); return }
        acting = true
        viewModelScope.launch {
            try {
                apiCall { api.strategyCopy(id, ApiStrategyCopyRequest(investment = inv)) }
                toast.show("策略已复制", ToastCenter.Kind.SUCCESS)
                loadStrategy(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }

    /** POST /strategy/grid/{id}/stop */
    fun stopGrid(id: String, onDone: (Boolean) -> Unit = {}) {
        acting = true
        viewModelScope.launch {
            try {
                apiCall { api.strategyStopGrid(id, ApiStrategyStopGridRequest()) }
                toast.show("已停止网格", ToastCenter.Kind.SUCCESS)
                loadStrategy(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }

    /** POST /earn/products/{id}/subscribe */
    fun subscribeEarn(id: String, amount: String, onDone: (Boolean) -> Unit = {}) {
        val a = amount.trim()
        if (Dec.parse(a).signum() <= 0) { toast.show("请输入申购金额", ToastCenter.Kind.ERROR); onDone(false); return }
        acting = true
        viewModelScope.launch {
            try {
                apiCall { api.earnSubscribe(id, ApiEarnSubscribeRequest(amount = a)) }
                toast.show("申购成功", ToastCenter.Kind.SUCCESS)
                loadEarn(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }

    /** POST /earn/products/{id}/redeem */
    fun redeemEarn(id: String, amount: String?, all: Boolean, onDone: (Boolean) -> Unit = {}) {
        if (!all && Dec.parse(amount).signum() <= 0) {
            toast.show("请输入赎回金额", ToastCenter.Kind.ERROR); onDone(false); return
        }
        acting = true
        viewModelScope.launch {
            try {
                apiCall {
                    api.earnRedeem(
                        id,
                        ApiEarnRedeemRequest(amount = if (all) null else amount?.trim(), all = if (all) true else null),
                    )
                }
                toast.show("赎回已提交", ToastCenter.Kind.SUCCESS)
                loadEarn(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }
}

@Composable
fun StrategySubpage(bottomInset: androidx.compose.ui.unit.Dp, onLogin: () -> Unit, vm: SpotSubpagesViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    var copyTarget by remember { mutableStateOf<ApiGridSquareItem?>(null) }
    LaunchedEffect(sessionVersion) { vm.clearAccountData(); vm.loadStrategy() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = bottomInset + 16.dp)) {
        Text("策略广场", Modifier.padding(horizontal = Spacing.pageH, vertical = 12.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        when {
            vm.loading && vm.square.isEmpty() -> Box(Modifier.fillMaxWidth().height(160.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && vm.square.isEmpty() -> EmptyState(vm.error ?: "") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadStrategy() } }
            vm.square.isEmpty() -> EmptyState("暂无公开策略")
            else -> vm.square.forEach { item ->
                Column(
                    Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 10.dp)
                        .border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(item.symbol ?: "--", style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                        Text("  ${item.mode ?: "网格"}", style = lb(FontSize.small), color = p.textSecondary)
                        Box(Modifier.weight(1f))
                        Text("收益率 ${(Dec.parse(item.profitRate).multiply(BigDecimal(100))).stripTrailingZeros().toPlainString()}%", style = lbNum(FontSize.small), color = p.up)
                    }
                    KvRow("价格区间", "${item.lowerPrice ?: "--"} – ${item.upperPrice ?: "--"}")
                    KvRow("网格数", "${item.gridCount ?: 0}")
                    ChipButton("复制策略", Modifier.height(32.dp)) {
                        if (!loggedIn) onLogin() else copyTarget = item
                    }
                }
            }
        }
        if (loggedIn) {
            Text("我的策略", Modifier.padding(horizontal = Spacing.pageH).padding(top = 8.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            if (vm.myGrids.isEmpty()) Text("暂无运行中的网格", Modifier.padding(Spacing.pageH), style = lb(FontSize.small), color = p.textSecondary)
            else vm.myGrids.forEach { g ->
                Column(Modifier.padding(horizontal = Spacing.pageH, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("${g.symbol ?: "--"} · ${g.mode ?: ""}", style = lb(FontSize.body), color = p.textPrimary)
                    KvRow("总收益", g.totalProfit ?: "--")
                    g.id?.let { id ->
                        ChipButton("停止", Modifier.height(28.dp)) { vm.stopGrid(id) }
                    }
                }
            }
        }
    }
    copyTarget?.let { item ->
        var inv by remember { mutableStateOf("") }
        LbSheet("复制策略", { copyTarget = null }, showClose = true) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                KvRow("标的", item.symbol ?: "--")
                KvRow("区间", "${item.lowerPrice ?: "--"} – ${item.upperPrice ?: "--"}")
                NumberBox("投资额", "USDT", inv, { inv = it })
                LbButton("确认复制", kind = ButtonKind.PRIMARY, loading = vm.acting) {
                    val id = item.id
                    if (id == null) vm.toast.show("无效策略", ToastCenter.Kind.ERROR)
                    else vm.copyStrategy(id, inv) { ok -> if (ok) copyTarget = null }
                }
            }
        }
    }
}

@Composable
fun EarnSubpage(bottomInset: androidx.compose.ui.unit.Dp, onLogin: () -> Unit, vm: SpotSubpagesViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    var earnAction by remember { mutableStateOf<EarnAction?>(null) }
    LaunchedEffect(sessionVersion) { vm.clearAccountData(); vm.loadEarn() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = bottomInset + 16.dp)) {
        Text("理财", Modifier.padding(horizontal = Spacing.pageH, vertical = 12.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        vm.earnSummary?.let { s ->
            Column(
                Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 12.dp)
                    .clip(RoundedCornerShape(Radius.md)).background(p.bgElevated).padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                KvRow("理财总资产(USDT)", Fmt.amount(Dec.parse(s.totalValueUsdt), 2))
                KvRow("累计收益", Fmt.amount(Dec.parse(s.totalInterestUsdt), 4))
            }
        } ?: if (!loggedIn) {
            EmptyState("登录后查看理财资产") { ChipButton("登录 / 注册", Modifier.height(36.dp), onClick = onLogin) }
        } else Unit
        Text("产品列表", Modifier.padding(horizontal = Spacing.pageH).padding(top = 4.dp, bottom = 8.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
        when {
            vm.loading && vm.earnProducts.isEmpty() -> Box(Modifier.fillMaxWidth().height(120.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && vm.earnProducts.isEmpty() -> EmptyState(vm.error ?: "") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadEarn() } }
            vm.earnProducts.isEmpty() -> EmptyState("暂无理财产品")
            else -> vm.earnProducts.forEach { prod ->
                Column(
                    Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 8.dp)
                        .border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(prod.title ?: prod.asset ?: "--", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                            Text(prod.kind?.value ?: "", style = lb(FontSize.small), color = p.textSecondary)
                        }
                        Column(horizontalAlignment = Alignment.End) {
                            Text("${Fmt.amount(Dec.parse(prod.apr).multiply(BigDecimal(100)), 2)}%", style = lbNum(FontSize.body, FontWeight.SemiBold), color = p.up)
                            Text("年化", style = lb(FontSize.small), color = p.textTertiary)
                        }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        ChipButton("申购", Modifier.height(28.dp)) {
                            if (!loggedIn) onLogin() else earnAction = EarnAction(prod, subscribe = true)
                        }
                        ChipButton("赎回", Modifier.height(28.dp)) {
                            if (!loggedIn) onLogin() else earnAction = EarnAction(prod, subscribe = false)
                        }
                    }
                }
            }
        }
    }
    earnAction?.let { act ->
        var amount by remember { mutableStateOf("") }
        val title = if (act.subscribe) "申购 ${act.product.title ?: act.product.asset ?: ""}" else "赎回 ${act.product.title ?: act.product.asset ?: ""}"
        LbSheet(title, { earnAction = null }, showClose = true) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                NumberBox("金额", act.product.asset ?: "USDT", amount, { amount = it })
                if (!act.subscribe) {
                    LbButton("全部赎回", kind = ButtonKind.SECONDARY, loading = vm.acting) {
                        val id = act.product.id
                        if (id == null) vm.toast.show("无效产品", ToastCenter.Kind.ERROR)
                        else vm.redeemEarn(id, null, all = true) { ok -> if (ok) earnAction = null }
                    }
                }
                LbButton(if (act.subscribe) "确认申购" else "确认赎回", kind = ButtonKind.PRIMARY, loading = vm.acting) {
                    val id = act.product.id
                    if (id == null) vm.toast.show("无效产品", ToastCenter.Kind.ERROR)
                    else if (act.subscribe) vm.subscribeEarn(id, amount) { ok -> if (ok) earnAction = null }
                    else vm.redeemEarn(id, amount, all = false) { ok -> if (ok) earnAction = null }
                }
            }
        }
    }
}

private data class EarnAction(val product: ApiEarnProduct, val subscribe: Boolean)

@Composable
fun ConvertSubpage(bottomInset: androidx.compose.ui.unit.Dp, onLogin: () -> Unit, vm: SpotSubpagesViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var from by remember { mutableStateOf("USDT") }
    var to by remember { mutableStateOf("BTC") }
    var amount by remember { mutableStateOf("") }
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    LaunchedEffect(sessionVersion) { vm.clearAccountData(); vm.loadConvert() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(bottom = bottomInset + 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("闪兑", Modifier.padding(vertical = 12.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        when {
            vm.loading && vm.convertPairs == null -> Box(Modifier.fillMaxWidth().height(120.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && vm.convertPairs == null -> EmptyState(vm.error ?: "") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadConvert() } }
            else -> {
                val assets = vm.convertPairs?.assets?.mapNotNull { it.asset } ?: listOf("USDT", "BTC", "ETH")
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    assets.take(6).forEach { a ->
                        ChipButton(a, Modifier.height(28.dp), color = if (from == a) p.textPrimary else p.textSecondary) { from = a; if (to == a) to = assets.firstOrNull { it != a } ?: to }
                    }
                }
                Text("兑出 $from", style = lb(FontSize.small), color = p.textSecondary)
                NumberBox("数量", from, amount, { amount = it })
                Text("兑入 $to", style = lb(FontSize.small), color = p.textSecondary)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    assets.filter { it != from }.take(6).forEach { a ->
                        ChipButton(a, Modifier.height(28.dp), color = if (to == a) p.textPrimary else p.textSecondary) { to = a }
                    }
                }
                vm.quote?.let { q ->
                    KvRow("报价", "1 $from ≈ ${q.rate ?: "--"} $to")
                    KvRow("预计到账", "${q.toAmount ?: "--"} $to")
                    KvRow("有效期", q.expiresAt?.let { java.text.SimpleDateFormat("HH:mm:ss", java.util.Locale.US).format(java.util.Date(if (it < 1_000_000_000_000L) it * 1000 else it)) } ?: "--")
                }
                if (!loggedIn) LbButton("登录后闪兑") { onLogin() }
                else {
                    LbButton("获取报价", loading = vm.quoting, enabled = Dec.parse(amount).signum() > 0) {
                        vm.requestQuote(from, to, amount)
                    }
                    if (vm.quote != null) LbButton("确认闪兑", kind = ButtonKind.BUY, loading = vm.quoting) { vm.acceptQuote() }
                }
            }
        }
    }
}
