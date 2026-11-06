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
import androidx.compose.foundation.layout.imePadding
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
import click.cowa.linbit.core.generated.ApiStrategyConfig200Response
import click.cowa.linbit.core.generated.ApiStrategyCopyRequest
import click.cowa.linbit.core.generated.ApiEarnRedeemRequest
import click.cowa.linbit.core.generated.ApiConvertPairs
import click.cowa.linbit.core.generated.ApiConvertQuote
import click.cowa.linbit.core.generated.ApiConvertQuoteRequest
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiStrategyPreview200Response
import click.cowa.linbit.core.generated.ApiGridCreateRequest
import click.cowa.linbit.core.generated.ApiEarnAutoRequest
import click.cowa.linbit.core.generated.ApiEarnPositions200ResponseItemsInner
import click.cowa.linbit.core.generated.ApiEarnRecords200ResponseItemsInner
import click.cowa.linbit.core.generated.ApiEarnOrders200ResponseItemsInner
import click.cowa.linbit.core.generated.ApiEarnSummary200Response
import click.cowa.linbit.core.generated.ApiGridBot
import click.cowa.linbit.core.generated.ApiGridSquareItem
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.ApiFailure
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
    var earnPositions by mutableStateOf<List<ApiEarnPositions200ResponseItemsInner>>(emptyList()); private set
    var earnOrders by mutableStateOf<List<ApiEarnOrders200ResponseItemsInner>>(emptyList()); private set
    var earnRecords by mutableStateOf<List<ApiEarnRecords200ResponseItemsInner>>(emptyList()); private set
    var gridPreview by mutableStateOf<ApiStrategyPreview200Response?>(null); private set
    var square by mutableStateOf<List<ApiGridSquareItem>>(emptyList()); private set
    var myGrids by mutableStateOf<List<ApiGridBot>>(emptyList()); private set
    var convertPairs by mutableStateOf<ApiConvertPairs?>(null); private set
    var strategyConfig by mutableStateOf<ApiStrategyConfig200Response?>(null); private set
    var quote by mutableStateOf<ApiConvertQuote?>(null); private set
    /** 表单修订号：from/to/amount 变更递增，防旧报价异步覆盖。 */
    var convertFormRev by mutableStateOf(0L); private set
    var loading by mutableStateOf(false); private set
    var error by mutableStateOf<String?>(null); private set
    var quoting by mutableStateOf(false); private set
    var accepting by mutableStateOf(false); private set
    var acting by mutableStateOf(false); private set

    fun clearAccountData() {
        earnSummary = null
        earnPositions = emptyList()
        earnOrders = emptyList()
        earnRecords = emptyList()
        myGrids = emptyList()
        quote = null
        gridPreview = null
    }

    fun clearConvertQuote() {
        quote = null
    }

    /** 兑出/兑入/数量变更：作废本地报价并抬升 revision。 */
    fun bumpConvertForm() {
        convertFormRev += 1
        quote = null
    }

    fun loadEarn() {
        viewModelScope.launch {
            loading = true; error = null
            try {
                earnProducts = apiCall { api.earnProducts() }.items ?: emptyList()
                if (tokens.loggedIn.value) {
                    earnSummary = runCatching { apiCall { api.earnSummary() } }.getOrNull()
                    earnPositions = runCatching { apiCall { api.earnPositions() }.items ?: emptyList() }.getOrDefault(emptyList())
                    earnOrders = runCatching { apiCall { api.earnOrders() }.items ?: emptyList() }.getOrDefault(emptyList())
                    earnRecords = runCatching { apiCall { api.earnRecords() }.items ?: emptyList() }.getOrDefault(emptyList())
                } else {
                    earnSummary = null; earnPositions = emptyList(); earnOrders = emptyList(); earnRecords = emptyList()
                }
            } catch (e: Throwable) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    fun loadStrategy() {
        viewModelScope.launch {
            loading = true; error = null
            try {
                square = apiCall { api.strategySquare() }.items ?: emptyList()
                strategyConfig = runCatching { apiCall { api.strategyConfig() } }.getOrNull()
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
        if (quoting || accepting) return
        val form = ConvertQuoteLogic.Form(from, to, amount)
        if (Dec.parse(form.amount).signum() <= 0) { toast.show("请输入数量", ToastCenter.Kind.ERROR); onDone(false); return }
        val pair = ConvertQuoteLogic.findPair(convertPairs?.pairs, from, to)
        if (pair == null) { toast.show("该币对不可闪兑", ToastCenter.Kind.ERROR); onDone(false); return }
        if (ConvertQuoteLogic.isPairPaused(pair)) {
            toast.show(ConvertQuoteLogic.pauseHint(pair) ?: "该币对已暂停", ToastCenter.Kind.ERROR)
            onDone(false); return
        }
        val rev = convertFormRev
        quoting = true
        viewModelScope.launch {
            try {
                val q = apiCall { api.convertQuote(ApiConvertQuoteRequest(from = from, to = to, fromAmount = amount.trim())) }
                if (ConvertQuoteLogic.shouldApplyQuote(q, requestRev = rev, currentRev = convertFormRev, form = form)) {
                    quote = q
                    onDone(true)
                } else {
                    // 表单已变或响应与请求不一致：丢弃，不覆盖新输入
                    onDone(false)
                }
            } catch (e: Throwable) {
                val code = (e as? ApiFailure)?.code
                if (ConvertQuoteLogic.shouldClearQuoteOnError(code)) quote = null
                toast.error(e); onDone(false)
            } finally { quoting = false }
        }
    }

    fun acceptQuote(from: String, to: String, amount: String, onDone: (Boolean) -> Unit = {}) {
        if (!tokens.loggedIn.value) { toast.show("请先登录"); onDone(false); return }
        if (quoting || accepting) return
        val form = ConvertQuoteLogic.Form(from, to, amount)
        val q = quote
        if (!ConvertQuoteLogic.canAccept(q, form)) {
            if (ConvertQuoteLogic.isExpired(q)) {
                quote = null
                toast.show("报价已过期，请重新询价", ToastCenter.Kind.ERROR)
            } else {
                toast.show("报价与当前表单不一致，请重新询价", ToastCenter.Kind.ERROR)
                quote = null
            }
            onDone(false); return
        }
        val id = q!!.quoteId!!
        accepting = true
        viewModelScope.launch {
            try {
                apiCall { api.acceptConvertQuote(ApiAcceptConvertQuoteRequest(quoteId = id)) }
                toast.show("闪兑成功", ToastCenter.Kind.SUCCESS)
                quote = null
                onDone(true)
            } catch (e: Throwable) {
                val code = (e as? ApiFailure)?.code
                if (ConvertQuoteLogic.shouldClearQuoteOnError(code)) {
                    quote = null
                    toast.show(e.lbMessage(), ToastCenter.Kind.ERROR)
                } else {
                    toast.error(e)
                }
                onDone(false)
            } finally { accepting = false }
        }
    }

    /** POST /strategy/grid/square/{id}/copy */
    fun copyStrategy(id: String, investment: String, onDone: (Boolean) -> Unit = {}) {
        if (!tokens.loggedIn.value) { toast.show("请先登录"); onDone(false); return }
        if (id.isBlank()) { toast.show("无效策略", ToastCenter.Kind.ERROR); onDone(false); return }
        if (acting) return
        val err = StrategyGridLogic.validateInvestment(investment, strategyConfig?.minInvestment)
        if (err != null) { toast.show(err, ToastCenter.Kind.ERROR); onDone(false); return }
        acting = true
        viewModelScope.launch {
            try {
                apiCall { api.strategyCopy(id, ApiStrategyCopyRequest(investment = investment.trim())) }
                toast.show("策略已复制", ToastCenter.Kind.SUCCESS)
                loadStrategy(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }

    /**
     * POST /strategy/grid/{id}/stop
     * sellBase 必须由确认层显式传入（撤销挂单 + 可选市价卖底仓）。
     */
    fun stopGrid(id: String, sellBase: Boolean, onDone: (Boolean) -> Unit = {}) {
        if (!tokens.loggedIn.value) { toast.show("请先登录"); onDone(false); return }
        if (id.isBlank()) { toast.show("无效网格", ToastCenter.Kind.ERROR); onDone(false); return }
        if (acting) return
        val bot = myGrids.firstOrNull { it.id == id }
        if (bot != null && !StrategyGridLogic.canStop(bot.status)) {
            toast.show(if (StrategyGridLogic.isStopping(bot.status)) "停止处理中" else "当前状态不可停止", ToastCenter.Kind.ERROR)
            onDone(false); return
        }
        acting = true
        viewModelScope.launch {
            try {
                apiCall { api.strategyStopGrid(id, ApiStrategyStopGridRequest(sellBase = sellBase)) }
                toast.show("停止请求已提交", ToastCenter.Kind.SUCCESS)
                loadStrategy(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }

    /** POST /earn/products/{id}/auto — 仅活期 */
    fun setEarnAuto(id: String, enabled: Boolean, onDone: (Boolean) -> Unit = {}) {
        if (!tokens.loggedIn.value) { toast.show("请先登录"); onDone(false); return }
        if (id.isBlank() || acting) { onDone(false); return }
        acting = true
        viewModelScope.launch {
            try {
                apiCall { api.earnAuto(id, ApiEarnAutoRequest(enabled = enabled)) }
                toast.show(if (enabled) "已开启自动申购" else "已关闭自动申购", ToastCenter.Kind.SUCCESS)
                loadEarn(); onDone(true)
            } catch (e: Throwable) { toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { acting = false }
        }
    }

    fun previewGrid(
        symbol: String, mode: ApiGridCreateRequest.Mode, lower: String, upper: String, grids: Int, investment: String,
        onDone: (Boolean) -> Unit = {},
    ) {
        if (acting || quoting) return
        quoting = true
        viewModelScope.launch {
            try {
                gridPreview = apiCall {
                    api.strategyPreview(
                        symbol = symbol.trim().uppercase(),
                        mode = mode.value,
                        lowerPrice = lower.trim(),
                        upperPrice = upper.trim(),
                        gridCount = grids,
                        investment = investment.trim(),
                    )
                }
                onDone(true)
            } catch (e: Throwable) { gridPreview = null; toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false) }
            finally { quoting = false }
        }
    }

    /** POST /strategy/grid 自建网格（真实契约，非模拟）。 */
    fun createGrid(
        symbol: String, mode: ApiGridCreateRequest.Mode, lower: String, upper: String, grids: Int, investment: String,
        sellBaseOnStop: Boolean, onDone: (Boolean) -> Unit = {},
    ) {
        if (!tokens.loggedIn.value) { toast.show("请先登录"); onDone(false); return }
        if (acting) return
        val invErr = StrategyGridLogic.validateInvestment(investment, strategyConfig?.minInvestment)
        if (invErr != null) { toast.show(invErr, ToastCenter.Kind.ERROR); onDone(false); return }
        if (Dec.parse(lower).signum() <= 0 || Dec.parse(upper).signum() <= 0 || grids < 2) {
            toast.show("请填写有效价格区间与网格数", ToastCenter.Kind.ERROR); onDone(false); return
        }
        acting = true
        viewModelScope.launch {
            try {
                apiCall {
                    api.strategyCreateGrid(
                        ApiGridCreateRequest(
                            symbol = symbol.trim().uppercase(),
                            mode = mode,
                            lowerPrice = lower.trim(),
                            upperPrice = upper.trim(),
                            gridCount = grids,
                            investment = investment.trim(),
                            sellBaseOnStop = sellBaseOnStop,
                        ),
                    )
                }
                toast.show("网格已创建", ToastCenter.Kind.SUCCESS)
                gridPreview = null
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
    var stopTarget by remember { mutableStateOf<ApiGridBot?>(null) }
    var showCreate by remember { mutableStateOf(false) }
    LaunchedEffect(sessionVersion) { vm.clearAccountData(); vm.loadStrategy() }
    Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(bottom = bottomInset + 16.dp)) {
        Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("策略广场", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
            Box(Modifier.weight(1f))
            ChipButton("创建网格", Modifier.height(28.dp)) {
                if (!loggedIn) onLogin() else showCreate = true
            }
        }
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
                    Text("${g.symbol ?: "--"} · ${g.mode ?: ""} · ${g.status?.value ?: ""}", style = lb(FontSize.body), color = p.textPrimary)
                    KvRow("总收益", g.totalProfit ?: "--")
                    KvRow("计价币", g.quoteAsset ?: StrategyGridLogic.quoteAssetFromSymbol(g.symbol) ?: "--")
                    when {
                        StrategyGridLogic.isStopping(g.status) -> {
                            Text("停止处理中…", style = lb(FontSize.small), color = p.textTertiary)
                        }
                        StrategyGridLogic.canStop(g.status) && !vm.acting -> {
                            ChipButton("停止", Modifier.height(28.dp)) { stopTarget = g }
                        }
                        StrategyGridLogic.canStop(g.status) && vm.acting -> {
                            Text("提交中…", style = lb(FontSize.small), color = p.textTertiary)
                        }
                    }
                }
            }
        }
    }
    copyTarget?.let { item ->
        var inv by remember { mutableStateOf("") }
        val quoteLabel = StrategyGridLogic.quoteAssetFromSymbol(item.symbol)
        LbSheet("复制策略", { copyTarget = null }, showClose = true) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                KvRow("标的", item.symbol ?: "--")
                KvRow("区间", "${item.lowerPrice ?: "--"} – ${item.upperPrice ?: "--"}")
                if (quoteLabel == null) {
                    Text("无法确认该标的计价币，暂不可复制（广场项无 quoteAsset，symbol 无法推断）", style = lb(FontSize.small), color = p.down)
                } else {
                    NumberBox("投资额", quoteLabel, inv, { inv = it })
                    vm.strategyConfig?.minInvestment?.let { min ->
                        Text("最小投资额 $min $quoteLabel", style = lb(FontSize.small), color = p.textSecondary)
                    }
                }
                LbButton(
                    "确认复制",
                    kind = ButtonKind.PRIMARY,
                    loading = vm.acting,
                    enabled = quoteLabel != null && !vm.acting,
                ) {
                    val id = item.id
                    if (id == null) vm.toast.show("无效策略", ToastCenter.Kind.ERROR)
                    else vm.copyStrategy(id, inv) { ok -> if (ok) copyTarget = null }
                }
            }
        }
    }
    stopTarget?.let { g ->
        val defaultSell = g.sellBaseOnStop ?: vm.strategyConfig?.sellBaseOnStop ?: true
        var sellBase by remember(g.id) { mutableStateOf(defaultSell) }
        LbSheet("确认停止网格", { if (!vm.acting) stopTarget = null }, showClose = !vm.acting) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                KvRow("标的", g.symbol ?: "--")
                KvRow("状态", g.status?.value ?: "--")
                KvRow("底仓策略默认", if (defaultSell) "停止时市价卖出底仓" else "停止时保留底仓")
                Text(
                    "停止将撤销该网格全部挂单。若选择市价卖出底仓，会按市价卖出当前持有的基础币。",
                    style = lb(FontSize.small),
                    color = p.textSecondary,
                )
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (!vm.acting) {
                        ChipButton(
                            if (sellBase) "✓ 市价卖出底仓" else "市价卖出底仓",
                            Modifier.height(32.dp),
                            color = if (sellBase) p.textPrimary else p.textSecondary,
                        ) { sellBase = true }
                        ChipButton(
                            if (!sellBase) "✓ 保留底仓" else "保留底仓",
                            Modifier.height(32.dp),
                            color = if (!sellBase) p.textPrimary else p.textSecondary,
                        ) { sellBase = false }
                    }
                }
                if (sellBase) {
                    Text("将市价卖出底仓（sellBase=true）", style = lb(FontSize.small), color = p.down)
                } else {
                    Text("将保留底仓，仅撤单（sellBase=false）", style = lb(FontSize.small), color = p.textSecondary)
                }
                LbButton("确认停止", kind = ButtonKind.SELL, loading = vm.acting, enabled = !vm.acting) {
                    val id = g.id ?: return@LbButton
                    vm.stopGrid(id, sellBase = sellBase) { ok -> if (ok) stopTarget = null }
                }
                LbButton("取消", kind = ButtonKind.OUTLINE, enabled = !vm.acting) { stopTarget = null }
            }
        }
    }


    if (showCreate) {
        var symbol by remember { mutableStateOf("BTCUSDT") }
        var modeArith by remember { mutableStateOf(true) }
        var lower by remember { mutableStateOf("") }
        var upper by remember { mutableStateOf("") }
        var grids by remember { mutableStateOf("10") }
        var inv by remember { mutableStateOf("") }
        var sellBase by remember { mutableStateOf(vm.strategyConfig?.sellBaseOnStop ?: true) }
        val quote = StrategyGridLogic.quoteAssetFromSymbol(symbol) ?: "USDT"
        LbSheet("创建网格", { if (!vm.acting) showCreate = false }, showClose = !vm.acting) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                NumberBox("交易对", "", symbol, { symbol = it.uppercase() })
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    ChipButton(if (modeArith) "✓ 等差" else "等差", Modifier.height(28.dp)) { modeArith = true }
                    ChipButton(if (!modeArith) "✓ 等比" else "等比", Modifier.height(28.dp)) { modeArith = false }
                }
                NumberBox("下限价", quote, lower, { lower = it })
                NumberBox("上限价", quote, upper, { upper = it })
                NumberBox("网格数", "", grids, { grids = it.filter { ch -> ch.isDigit() } })
                NumberBox("投资额", quote, inv, { inv = it })
                vm.strategyConfig?.minInvestment?.let { Text("最小投资额 $it $quote", style = lb(FontSize.small), color = p.textSecondary) }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    ChipButton(if (sellBase) "✓ 停止时卖底仓" else "停止时卖底仓", Modifier.height(28.dp)) { sellBase = true }
                    ChipButton(if (!sellBase) "✓ 停止保留底仓" else "停止保留底仓", Modifier.height(28.dp)) { sellBase = false }
                }
                vm.gridPreview?.let { prev ->
                    KvRow("当前价", prev.currentPrice ?: "--")
                    KvRow("每格数量", prev.qtyPerGrid ?: "--")
                    KvRow("买/卖单", "${prev.buyOrders ?: 0} / ${prev.sellOrders ?: 0}")
                    KvRow("初始买入", prev.initialBuyQuote ?: "--")
                    KvRow("单格收益", "${prev.profitPerGridMin ?: "--"} ~ ${prev.profitPerGridMax ?: "--"}")
                }
                val mode = if (modeArith) ApiGridCreateRequest.Mode.ARITHMETIC else ApiGridCreateRequest.Mode.GEOMETRIC
                val gCount = grids.toIntOrNull() ?: 0
                LbButton("试算", kind = ButtonKind.SECONDARY, loading = vm.quoting, enabled = !vm.acting && !vm.quoting) {
                    vm.previewGrid(symbol, mode, lower, upper, gCount, inv)
                }
                LbButton("确认创建", kind = ButtonKind.PRIMARY, loading = vm.acting, enabled = !vm.acting && !vm.quoting) {
                    vm.createGrid(symbol, mode, lower, upper, gCount, inv, sellBaseOnStop = sellBase) { ok -> if (ok) showCreate = false }
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
    Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(bottom = bottomInset + 16.dp)) {
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
        if (loggedIn && vm.earnPositions.isNotEmpty()) {
            Text("我的持仓", Modifier.padding(horizontal = Spacing.pageH).padding(top = 8.dp, bottom = 6.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            vm.earnPositions.forEach { pos ->
                Column(
                    Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 8.dp)
                        .border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Text(pos.title ?: pos.asset ?: "--", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    KvRow("持仓", "${pos.amount ?: "--"} ${pos.asset ?: ""}")
                    KvRow("累计利息", pos.totalInterest ?: "--")
                    if (pos.kind?.contains("flex", ignoreCase = true) == true || pos.auto != null) {
                        val on = pos.auto == true
                        ChipButton(if (on) "自动申购：开" else "自动申购：关", Modifier.height(28.dp)) {
                            val id = pos.productId
                            if (id != null) vm.setEarnAuto(id, enabled = !on)
                        }
                    }
                }
            }
        }
        if (loggedIn && vm.earnOrders.isNotEmpty()) {
            Text("定期订单", Modifier.padding(horizontal = Spacing.pageH).padding(top = 4.dp, bottom = 6.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            vm.earnOrders.take(20).forEach { o ->
                Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text("${o.title ?: o.asset ?: "--"} · ${o.status ?: ""}", style = lb(FontSize.small), color = p.textPrimary)
                    KvRow("金额", "${o.amount ?: "--"} ${o.asset ?: ""}")
                    KvRow("利息", o.interest ?: "--")
                }
            }
        }
        if (loggedIn && vm.earnRecords.isNotEmpty()) {
            Text("理财流水", Modifier.padding(horizontal = Spacing.pageH).padding(top = 4.dp, bottom = 6.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            vm.earnRecords.take(30).forEach { r ->
                KvRow(
                    "${r.type?.value ?: "--"} · ${r.asset ?: ""}",
                    r.amount ?: "--",
                    Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 4.dp),
                )
            }
        }
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
    // 报价过期本地倒计时：到期清 quote 并禁止 accept
    LaunchedEffect(vm.quote?.quoteId, vm.quote?.expiresAt) {
        val q = vm.quote ?: return@LaunchedEffect
        val exp = ConvertQuoteLogic.normalizeExpiresAtMs(q.expiresAt) ?: return@LaunchedEffect
        val wait = (exp - System.currentTimeMillis()).coerceAtLeast(0L)
        kotlinx.coroutines.delay(wait)
        if (vm.quote?.quoteId == q.quoteId && ConvertQuoteLogic.isExpired(vm.quote)) {
            vm.clearConvertQuote()
            vm.toast.show("报价已过期，请重新询价", ToastCenter.Kind.ERROR)
        }
    }
    Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(bottom = bottomInset + 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("闪兑", Modifier.padding(vertical = 12.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        when {
            vm.loading && vm.convertPairs == null -> Box(Modifier.fillMaxWidth().height(120.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && vm.convertPairs == null -> EmptyState(vm.error ?: "") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadConvert() } }
            else -> {
                val pairs = vm.convertPairs?.pairs
                val fromAssets = ConvertQuoteLogic.fromAssets(pairs)
                LaunchedEffect(fromAssets, from, to) {
                    if (fromAssets.isEmpty()) return@LaunchedEffect
                    if (from !in fromAssets) {
                        from = fromAssets.first()
                        val tos = ConvertQuoteLogic.toAssets(pairs, from)
                        to = tos.firstOrNull { it != from } ?: tos.firstOrNull() ?: to
                        vm.bumpConvertForm()
                        return@LaunchedEffect
                    }
                    val tos = ConvertQuoteLogic.toAssets(pairs, from)
                    if (tos.isNotEmpty() && to !in tos) {
                        to = tos.first()
                        vm.bumpConvertForm()
                    }
                }
                if (fromAssets.isEmpty()) {
                    EmptyState("暂无可交易闪兑币对")
                } else {
                    val toAssets = ConvertQuoteLogic.toAssets(pairs, from)
                    val pair = ConvertQuoteLogic.findPair(pairs, from, to)
                    val paused = ConvertQuoteLogic.isPairPaused(pair)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        fromAssets.take(8).forEach { a ->
                            ChipButton(a, Modifier.height(28.dp), color = if (from == a) p.textPrimary else p.textSecondary) {
                                if (from != a) {
                                    from = a
                                    val nextTo = ConvertQuoteLogic.toAssets(pairs, a).firstOrNull { it != a } ?: to
                                    to = nextTo
                                    amount = amount // keep text but invalidate quote
                                    vm.bumpConvertForm()
                                }
                            }
                        }
                    }
                    Text("兑出 $from", style = lb(FontSize.small), color = p.textSecondary)
                    NumberBox("数量", from, amount, {
                        if (amount != it) {
                            amount = it
                            vm.bumpConvertForm()
                        }
                    })
                    Text("兑入 $to", style = lb(FontSize.small), color = p.textSecondary)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        toAssets.take(8).forEach { a ->
                            ChipButton(a, Modifier.height(28.dp), color = if (to == a) p.textPrimary else p.textSecondary) {
                                if (to != a) {
                                    to = a
                                    vm.bumpConvertForm()
                                }
                            }
                        }
                    }
                    if (pair == null) {
                        Text("所选币对不在可交易列表中", style = lb(FontSize.small), color = p.down)
                    } else if (paused) {
                        Text(ConvertQuoteLogic.pauseHint(pair) ?: "该币对已暂停", style = lb(FontSize.small), color = p.down)
                    }
                    val form = ConvertQuoteLogic.Form(from, to, amount)
                    val q = vm.quote
                    val quoteOk = ConvertQuoteLogic.canAccept(q, form)
                    q?.takeIf { ConvertQuoteLogic.matchesForm(it, form) }?.let { matched ->
                        KvRow("报价", "1 ${matched.from} ≈ ${matched.rate ?: "--"} ${matched.to}")
                        KvRow("兑出", "${matched.fromAmount ?: "--"} ${matched.from}")
                        KvRow("预计到账", "${matched.toAmount ?: "--"} ${matched.to}")
                        val expMs = ConvertQuoteLogic.normalizeExpiresAtMs(matched.expiresAt)
                        KvRow(
                            "有效至",
                            expMs?.let {
                                java.text.SimpleDateFormat("HH:mm:ss", java.util.Locale.US).format(java.util.Date(it))
                            } ?: "--",
                        )
                        if (ConvertQuoteLogic.isExpired(matched)) {
                            Text("报价已过期", style = lb(FontSize.small), color = p.down)
                        }
                    }
                    if (!loggedIn) LbButton("登录后闪兑") { onLogin() }
                    else {
                        LbButton(
                            "获取报价",
                            loading = vm.quoting,
                            enabled = Dec.parse(amount).signum() > 0 && pair != null && !paused && !vm.quoting && !vm.accepting,
                        ) { vm.requestQuote(from, to, amount) }
                        if (q != null) {
                            LbButton(
                                "确认闪兑",
                                kind = ButtonKind.BUY,
                                loading = vm.accepting,
                                enabled = quoteOk && !vm.quoting && !vm.accepting,
                            ) { vm.acceptQuote(from, to, amount) }
                        }
                    }
                }
            }
        }
    }
}
