package click.cowa.linbit.feature.more

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.feature.profile.InfoNote
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import javax.inject.Inject
import java.math.BigDecimal

/** OpenAPI 对跟单扩展字段使用 additionalProperties；用 JsonObject 保留 minAmount / followView / 收益统计。 */
data class CopyState(
    val summary: JsonObject = JsonObject(emptyMap()),
    val traders: List<JsonObject> = emptyList(),
    val follows: List<JsonObject> = emptyList(),
    val detail: ApiCopyTraderDetail? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val busy: Boolean = false,
)

@HiltViewModel
class CopyViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    private val _state = MutableStateFlow(CopyState())
    val state: StateFlow<CopyState> = _state
    val loggedIn get() = tokens.loggedIn
    var sort = "composite"; var days = 30
    internal var holdEvidence: Boolean = false

    init { viewModelScope.launch { tokens.sessionVersion.collect { if (!holdEvidence) _state.value = CopyState() } } }

    /** 离屏证据：带单员榜示例（JsonObject 合成，非生产）。 */
    internal fun seedEvidenceCopy(traders: List<kotlinx.serialization.json.JsonObject>, summary: kotlinx.serialization.json.JsonObject = kotlinx.serialization.json.JsonObject(emptyMap())) {
        holdEvidence = true
        _state.value = CopyState(summary = summary, traders = traders, loading = false)
    }

    fun loadTraders(newSort: String = sort, newDays: Int = days) {
        if (holdEvidence) return
        sort = newSort; days = newDays
        _state.value = _state.value.copy(loading = true, error = null)
        viewModelScope.launch {
            val summary = async { runCatching { apiCall { api.copySummary() }.jsonObject }.getOrNull() }
            runCatching { apiCall { api.copyTraders(sort, days) } }
                .onSuccess { _state.value = _state.value.copy(summary = summary.await() ?: _state.value.summary, traders = it.traders.orEmpty(), loading = false) }
                .onFailure { summary.await(); _state.value = _state.value.copy(loading = false, error = it.lbMessage()) }
        }
    }

    fun loadFollows() {
        if (!tokens.loggedIn.value) { _state.value = _state.value.copy(follows = emptyList(), loading = false); return }
        _state.value = _state.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { apiCall { api.copyFollows() }.jsonObject.array("follows") }
                .onSuccess { _state.value = _state.value.copy(follows = it, loading = false) }
                .onFailure { _state.value = _state.value.copy(loading = false, error = it.lbMessage()) }
        }
    }

    fun loadDetail(id: String) {
        _state.value = _state.value.copy(loading = true, error = null, detail = null)
        viewModelScope.launch {
            runCatching { apiCall { api.copyTrader(id, days) } }
                .onSuccess { _state.value = _state.value.copy(detail = it, loading = false) }
                .onFailure { _state.value = _state.value.copy(loading = false, error = it.lbMessage()) }
        }
    }

    fun follow(traderId: String, input: CopyLogic.Input, onDone: () -> Unit) {
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true)
        viewModelScope.launch {
            runCatching { apiCall { api.followTrader(CopyLogic.request(traderId, input)) } }
                .onSuccess { toast.show("跟单成功", ToastCenter.Kind.SUCCESS); _state.value = _state.value.copy(busy = false); onDone() }
                .onFailure { _state.value = _state.value.copy(busy = false); toast.error(it) }
        }
    }

    fun stop(id: Long) {
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true)
        viewModelScope.launch {
            runCatching { apiCall { api.stopCopyFollow(id) } }
                .onSuccess { toast.show("已停止跟单", ToastCenter.Kind.SUCCESS); _state.value = _state.value.copy(busy = false); loadFollows() }
                .onFailure { _state.value = _state.value.copy(busy = false); toast.error(it) }
        }
    }

    fun become(nickname: String, bio: String, profitShare: String, onDone: () -> Unit) {
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true)
        viewModelScope.launch {
            runCatching { apiCall { api.becomeCopyTrader(ApiCopyTraderRequest(nickname = nickname, bio = bio, profitShare = profitShare)) } }
                .onSuccess { toast.show("带单员申请已提交", ToastCenter.Kind.SUCCESS); _state.value = _state.value.copy(busy = false); onDone() }
                .onFailure { _state.value = _state.value.copy(busy = false); toast.error(it) }
        }
    }
}

@Composable
fun CopyTradingScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: CopyViewModel = hiltViewModel()) {
    val s by vm.state.collectAsStateWithLifecycle()
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var top by remember { mutableIntStateOf(0) }
    var sort by remember { mutableIntStateOf(0) }
    var daysIndex by remember { mutableIntStateOf(1) }
    var detailId by remember { mutableStateOf<String?>(null) }
    var followTrader by remember { mutableStateOf<JsonObject?>(null) }
    var become by remember { mutableStateOf(false) }
    var stopId by remember { mutableStateOf<Long?>(null) }
    val sorts = listOf("composite" to "综合", "roi" to "收益率", "followers" to "跟随人数", "winrate" to "胜率", "aum" to "管理规模")
    LaunchedEffect(sort, daysIndex) { vm.loadTraders(sorts[sort].first, listOf(7, 30, 90)[daysIndex]) }
    LaunchedEffect(top, loggedIn) { if (top == 1) vm.loadFollows() }
    if (detailId != null) {
        CopyTraderDetailScreen(detailId!!, s, vm, onBack = { detailId = null }, onFollow = { trader ->
            if (loggedIn) followTrader = trader else onLogin()
        })
    } else MoreFrame("跟单交易", onBack, trailing = {
        Text("成为带单员", Modifier.pressable(scale = 1f) { if (loggedIn) become = true else onLogin() }, style = lb(FontSize.small, FontWeight.Medium), color = LB.p.brand)
    }) {
        PrimaryTabs(listOf("带单员", "我的跟单"), top, { top = it }, Modifier.padding(horizontal = Spacing.pageH), size = FontSize.label, underline = true)
        if (top == 1) {
            if (!loggedIn) EmptyState("登录后查看我的跟单", Modifier.weight(1f)) { LbButton("登录", Modifier.width(160.dp)) { onLogin() } }
            else CopyFollowList(s, vm::loadFollows) { stopId = it }
        } else Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                StatTile("带单员", s.summary.integer("traders")?.toString() ?: "--", modifier = Modifier.weight(1f))
                StatTile("跟单人数", s.summary.integer("totalFollowers")?.toString() ?: "--", modifier = Modifier.weight(1f))
                StatTile("跟单总收益", s.summary.amount("followerProfit", 0), LB.p.up, Modifier.weight(1f))
            }
            ChipBar(sorts.map { it.second }, sort, { sort = it })
            ChipBar(listOf("7 天", "30 天", "90 天"), daysIndex, { daysIndex = it })
            when {
                s.loading -> MoreLoading()
                s.error != null -> MoreError(s.error ?: "", { vm.loadTraders() })
                s.traders.isEmpty() -> EmptyState("暂无带单员")
                else -> s.traders.forEach { trader -> CopyTraderCard(trader) { detailId = trader.str("traderId") } }
            }
        }
    }
    followTrader?.let { trader ->
        FollowDialog(trader, s.summary.dec("minAmount").takeIf { it > BigDecimal.ZERO } ?: BigDecimal(50), s.busy,
            onDismiss = { followTrader = null },
            onSubmit = { input -> vm.follow(trader.str("traderId") ?: return@FollowDialog, input) { followTrader = null; vm.loadFollows() } })
    }
    if (become) BecomeTraderDialog(s.busy, { become = false }) { n, b, share -> vm.become(n, b, share) { become = false } }
    stopId?.let { id -> AlertDialog(
        onDismissRequest = { stopId = null }, title = { Text("停止跟单") },
        text = { Text("停止后将按市价平掉该带单员的全部跟单仓位，剩余资金返还至合约账户。") },
        confirmButton = { TextButton(enabled = !s.busy, onClick = { stopId = null; vm.stop(id) }) { Text("确认停止", color = LB.p.down) } },
        dismissButton = { TextButton(onClick = { stopId = null }) { Text("取消") } },
    ) }
}

@Composable
private fun CopyTraderCard(t: JsonObject, onClick: () -> Unit) {
    val roi = t.dec("roi")
    val full = (t.integer("followers") ?: 0) >= (t.integer("maxFollowers") ?: Int.MAX_VALUE)
    MoreCard(Modifier.pressable(scale = 1f, onClick = onClick)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TraderAvatar(t.str("nickname") ?: "?")
            Spacer(Modifier.width(10.dp))
            Column(Modifier.weight(1f)) {
                Text(t.str("nickname") ?: "--", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
                Text("跟随 ${t.integer("followers") ?: 0}/${t.integer("maxFollowers") ?: "--"} · 带单 ${t.integer("days") ?: 0} 天", style = lbNum(FontSize.small), color = LB.p.textTertiary)
            }
            LbButton(if (full) "已满员" else "跟单", Modifier.width(72.dp), height = 30.dp, enabled = !full, onClick = onClick)
        }
        Row(verticalAlignment = Alignment.Bottom) {
            Column {
                Text("收益率", style = lb(FontSize.small), color = LB.p.textSecondary)
                Text((if (roi.signum() >= 0) "+" else "") + t.ratio("roi"), style = lbNum(22.sp, FontWeight.Bold), color = if (roi.signum() >= 0) LB.p.up else LB.p.down)
            }
            Spacer(Modifier.weight(1f))
            CopyCurve(t.array("curve").map { it.dec("cumPnl").toFloat() }, Modifier.size(120.dp, 40.dp), roi.signum() >= 0)
        }
        Row {
            Text("收益 ${t.signedAmount("pnl")}", style = lbNum(FontSize.small), color = LB.p.textSecondary)
            Spacer(Modifier.weight(1f)); Text("胜率 ${t.ratio("winRate")}", style = lbNum(FontSize.small), color = LB.p.textSecondary)
            Spacer(Modifier.weight(1f)); Text("回撤 ${t.ratio("maxDrawdown")}", style = lbNum(FontSize.small), color = LB.p.textSecondary)
        }
    }
}

@Composable
private fun TraderAvatar(name: String) {
    Box(Modifier.size(40.dp).background(LB.p.brand.copy(alpha = 0.12f), CircleShape), contentAlignment = Alignment.Center) {
        Text(name.take(1), style = lb(16.sp, FontWeight.Bold), color = LB.p.brand)
    }
}

@Composable
private fun CopyCurve(points: List<Float>, modifier: Modifier, up: Boolean) {
    val curveColor = if (up) LB.p.up else LB.p.down
    Canvas(modifier) {
        if (points.size > 1) {
            val lo = points.min(); val span = (points.max() - lo).coerceAtLeast(1f)
            val path = Path()
            points.forEachIndexed { i, v ->
                val x = size.width * i / (points.size - 1); val y = size.height * (1 - (v - lo) / span)
                if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
            }
            drawPath(path, curveColor, style = Stroke(1.5.dp.toPx()))
        }
    }
}

@Composable
private fun CopyTraderDetailScreen(id: String, s: CopyState, vm: CopyViewModel, onBack: () -> Unit, onFollow: (JsonObject) -> Unit) {
    LaunchedEffect(id) { vm.loadDetail(id) }
    MoreFrame("带单员详情", onBack) {
        when {
            s.loading && s.detail == null -> MoreLoading(Modifier.weight(1f))
            s.error != null && s.detail == null -> MoreError(s.error ?: "", { vm.loadDetail(id) }, Modifier.weight(1f))
            s.detail != null -> {
                val t = s.detail!!.trader ?: JsonObject(emptyMap())
                Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        TraderAvatar(t.str("nickname") ?: "?"); Spacer(Modifier.width(12.dp))
                        Column {
                            Text(t.str("nickname") ?: "--", style = lb(18.sp, FontWeight.Bold), color = LB.p.textPrimary)
                            Text("分润比例 ${t.ratio("profitShare", 0)} · 带单 ${t.integer("days") ?: 0} 天", style = lbNum(FontSize.small), color = LB.p.textSecondary)
                        }
                    }
                    t.str("bio")?.takeIf { it.isNotBlank() }?.let { Text(it, style = lb(FontSize.small), color = LB.p.textSecondary) }
                    MoreCard { Text("收益曲线", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary); CopyCurve(t.array("curve").map { it.dec("cumPnl").toFloat() }, Modifier.fillMaxWidth().height(120.dp), t.dec("roi").signum() >= 0) }
                    val stats = listOf("收益率" to t.ratio("roi"), "累计收益" to t.signedAmount("pnl"), "胜率" to t.ratio("winRate"), "最大回撤" to t.ratio("maxDrawdown"), "跟单收益" to t.amount("followerProfit", 0), "管理规模" to t.amount("aum", 0))
                    stats.chunked(3).forEach { row -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { row.forEach { StatTile(it.first, it.second, modifier = Modifier.weight(1f)) } } }
                    Text("当前持仓（${s.detail!!.positions.orEmpty().size}）", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
                    if (s.detail!!.positions.isNullOrEmpty()) EmptyState("暂无持仓")
                    s.detail!!.positions.orEmpty().forEach { pos -> MoreCard {
                        Row {
                            Column(Modifier.weight(1f)) {
                                Text(pos.str("symbol") ?: "--", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
                                Text("开仓 ${formatTime(pos.str("openedAt"))}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                            }
                            Column(horizontalAlignment = Alignment.End) { Text("开仓价", style = lb(FontSize.small), color = LB.p.textTertiary); Text(pos.amount("entryPrice"), style = lbNum(FontSize.body), color = LB.p.textPrimary) }
                        }
                    } }
                    val full = (t.integer("followers") ?: 0) >= (t.integer("maxFollowers") ?: Int.MAX_VALUE)
                    LbButton(if (t.str("status") != null && t.str("status") != "active") "带单已关闭" else if (full) "跟单人数已满" else "立即跟单", enabled = !full && t.str("status") in listOf(null, "active")) { onFollow(t) }
                }
            }
        }
    }
}

@Composable
private fun CopyFollowList(s: CopyState, onRetry: () -> Unit, onStop: (Long) -> Unit) {
    when {
        s.loading -> MoreLoading()
        s.error != null -> MoreError(s.error ?: "", onRetry)
        s.follows.isEmpty() -> EmptyState("暂无跟单，去带单员榜单挑选吧")
        else -> Column(Modifier.verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            s.follows.forEach { view ->
                val f = view.obj("follow"); val active = f.str("status") == "active"
                MoreCard {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(view.str("traderNickname") ?: f.str("traderId") ?: "--", Modifier.weight(1f), style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
                        SmallTag(if (active) "跟单中" else "已停止", if (active) LB.p.up else LB.p.textTertiary)
                        Spacer(Modifier.width(8.dp))
                        Text(if (f.str("mode") == "fixed") "固定 ${f.amount("fixedMargin")} U" else "比例 ${f.str("ratio") ?: "1"}x", style = lbNum(FontSize.small), color = LB.p.textSecondary)
                    }
                    Text("跟单本金 ${f.amount("principal")} USDT", style = lbNum(FontSize.small), color = LB.p.textSecondary)
                    Text("跟单收益 ${view.signedAmount("pnl")} USDT", style = lbNum(FontSize.small), color = if (view.dec("pnl").signum() < 0) LB.p.down else LB.p.up)
                    Text("已付分润 ${f.amount("sharePaid")} USDT", style = lbNum(FontSize.small), color = LB.p.textSecondary)
                    Text("${if (active) "开始" else "停止"}时间 ${formatTime(if (active) f.str("createdAt") else f.str("stoppedAt"))}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                    if (active) f.long("id")?.let { LbButton("停止跟单", kind = ButtonKind.OUTLINE, height = 34.dp) { onStop(it) } }
                }
            }
        }
    }
}

@Composable
private fun FollowDialog(trader: JsonObject, minimum: BigDecimal, busy: Boolean, onDismiss: () -> Unit, onSubmit: (CopyLogic.Input) -> Unit) {
    var fixed by remember { mutableStateOf(false) }
    var amount by remember { mutableStateOf("") }
    var ratio by remember { mutableStateOf("1") }
    var margin by remember { mutableStateOf(minimum.stripTrailingZeros().toPlainString()) }
    var maxPerTrade by remember { mutableStateOf("") }
    var tp by remember { mutableStateOf("") }
    var sl by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }
    val input = CopyLogic.Input(amount, fixed, ratio, margin, maxPerTrade, tp, sl)
    val error = if (amount.isBlank()) null else CopyLogic.error(input, minimum)
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() }, title = { Text("跟单 ${trader.str("nickname") ?: ""}") },
        text = { Column(Modifier.heightIn(max = 500.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("分润比例 ${trader.ratio("profitShare", 0)}，仅对盈利部分收取", style = lb(FontSize.small), color = LB.p.textSecondary)
            ChipBar(listOf("按比例跟单", "固定保证金"), if (fixed) 1 else 0, { fixed = it == 1 })
            FieldLabel("跟单金额 (USDT)")
            LbTextField(amount, { amount = it }, "最低 ${minimum.stripTrailingZeros().toPlainString()} USDT", keyboardType = KeyboardType.Decimal, error = error)
            FieldLabel(if (fixed) "单笔保证金 (USDT)" else "跟单比例（倍）")
            if (fixed) LbTextField(margin, { margin = it }, "每笔跟单使用的保证金", keyboardType = KeyboardType.Decimal) else LbTextField(ratio, { ratio = it }, "带单员仓位的倍数，如 1", keyboardType = KeyboardType.Decimal)
            LbTextField(maxPerTrade, { maxPerTrade = it }, "单笔最大保证金（选填）", keyboardType = KeyboardType.Decimal)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LbTextField(tp, { tp = it }, "止盈 %（选填）", Modifier.weight(1f), keyboardType = KeyboardType.Decimal)
                LbTextField(sl, { sl = it }, "止损 %（选填）", Modifier.weight(1f), keyboardType = KeyboardType.Decimal)
            }
            InfoNote("跟单资金将从合约账户划入独立跟单仓位，停止跟单将按市价平仓并返还剩余资金。")
        } },
        confirmButton = { TextButton(enabled = !busy && amount.isNotBlank() && error == null, onClick = { confirm = true }) { Text(if (busy) "提交中" else "确认跟单") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
    if (confirm) AlertDialog(
        onDismissRequest = { confirm = false }, title = { Text("确认跟单 ${trader.str("nickname") ?: ""}") },
        text = { Text("将从合约账户划转 $amount USDT 到独立的跟单账户（${if (fixed) "固定保证金 $margin USDT / 笔" else "按比例 $ratio 倍跟单"}），带单员开平仓时自动跟随，盈利部分按 ${trader.ratio("profitShare", 0)} 分润。") },
        confirmButton = { TextButton(onClick = { confirm = false; onSubmit(input) }) { Text("确认跟单") } },
        dismissButton = { TextButton(onClick = { confirm = false }) { Text("返回修改") } },
    )
}

@Composable
private fun BecomeTraderDialog(busy: Boolean, onDismiss: () -> Unit, onSubmit: (String, String, String) -> Unit) {
    var nickname by remember { mutableStateOf("") }
    var bio by remember { mutableStateOf("") }
    var share by remember { mutableIntStateOf(1) }
    val shares = listOf("0.05", "0.1", "0.15", "0.2")
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("成为带单员") },
        text = { Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("申请需人工审核；合约账户权益需满足最低要求。", style = lb(FontSize.small), color = LB.p.textSecondary)
            LbTextField(nickname, { nickname = it.take(20) }, "昵称（2-20 个字符）")
            LbTextField(bio, { bio = it.take(200) }, "介绍您的交易风格")
            ChipBar(listOf("5%", "10%", "15%", "20%"), share, { share = it })
        } },
        confirmButton = { TextButton(enabled = nickname.length in 2..20 && !busy, onClick = { onSubmit(nickname, bio, shares[share]) }) { Text("提交") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}
