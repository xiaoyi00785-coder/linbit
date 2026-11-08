package click.cowa.linbit.feature.more

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 福利进度、报名、任务领取与卡券均使用 growth 服务真实接口。 */
enum class WelfareTaskRoute { Profile, Deposit, Futures }

fun welfareTaskRoute(type: ApiNewcomerTask.Type?): WelfareTaskRoute? = when (type) {
    ApiNewcomerTask.Type.BIND -> WelfareTaskRoute.Profile
    ApiNewcomerTask.Type.FIRST_DEPOSIT -> WelfareTaskRoute.Deposit
    ApiNewcomerTask.Type.VOLUME -> WelfareTaskRoute.Futures
    else -> null
}

data class WelfareState(
    val newcomer: ApiHomeNewcomer? = null,
    val bonus: ApiFuturesBonus200Response? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val enrolling: Boolean = false,
    val claiming: Set<String> = emptySet(),
    val vouchers: Int? = null,
)

@HiltViewModel
class WelfareViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    private val _state = kotlinx.coroutines.flow.MutableStateFlow(WelfareState())
    val state: kotlinx.coroutines.flow.StateFlow<WelfareState> = _state
    val loggedIn get() = tokens.loggedIn
    internal var holdEvidence: Boolean = false

    /** 离屏证据：福利中心示例。 */
    internal fun seedEvidenceWelfare(newcomer: ApiHomeNewcomer?, bonus: ApiFuturesBonus200Response? = null) {
        holdEvidence = true
        _state.value = WelfareState(newcomer = newcomer, bonus = bonus, loading = false)
    }

    init {
        viewModelScope.launch { tokens.sessionVersion.collect { _state.value = WelfareState() } }
    }

    fun load() {
        if (holdEvidence) return
        val session = tokens.sessionVersion.value
        _state.value = _state.value.copy(loading = _state.value.newcomer == null, error = null)
        viewModelScope.launch {
            val n = async { runCatching { apiCall { api.homeNewcomer() } } }
            val b = async {
                if (tokens.loggedIn.value) runCatching { apiCall { api.futuresBonus() } }.getOrNull()
                else null
            }
            val c = async { if (tokens.loggedIn.value) runCatching { apiCall { api.couponSummary() } }.getOrNull()?.available else null }
            val newcomer = n.await()
            val bonus = b.await()
            val vouchers = c.await()
            if (session != tokens.sessionVersion.value) return@launch
            _state.value = WelfareState(
                newcomer = newcomer.getOrNull(),
                bonus = bonus,
                loading = false,
                error = newcomer.exceptionOrNull()?.lbMessage(),
                vouchers = vouchers,
            )
        }
    }

    fun enroll() {
        if (_state.value.enrolling) return
        val session = tokens.sessionVersion.value
        _state.value = _state.value.copy(enrolling = true)
        viewModelScope.launch {
            runCatching { apiCall { api.enrollNewcomer() } }
                .onSuccess {
                    if (session == tokens.sessionVersion.value) {
                        _state.value = _state.value.copy(enrolling = false)
                        toast.show("报名成功，完成任务即可领取奖励", ToastCenter.Kind.SUCCESS)
                        load()
                    }
                }
                .onFailure {
                    if (session == tokens.sessionVersion.value) {
                        _state.value = _state.value.copy(enrolling = false)
                        toast.error(it)
                    }
                }
        }
    }

    fun claim(task: ApiNewcomerTask) {
        if (task.status != ApiNewcomerTask.Status.CLAIMABLE) return
        val key = task.key ?: return
        if (key in _state.value.claiming) return
        val session = tokens.sessionVersion.value
        _state.value = _state.value.copy(claiming = _state.value.claiming + key)
        viewModelScope.launch {
            runCatching { apiCall { api.claimNewcomerTask(key) } }
                .onSuccess { result ->
                    if (session == tokens.sessionVersion.value) {
                        _state.value = _state.value.copy(claiming = _state.value.claiming - key)
                        toast.show("${result.title ?: "任务奖励"}已发放至卡券中心", ToastCenter.Kind.SUCCESS)
                        load()
                    }
                }
                .onFailure {
                    if (session == tokens.sessionVersion.value) {
                        _state.value = _state.value.copy(claiming = _state.value.claiming - key)
                        toast.error(it)
                    }
                }
        }
    }
}

private val WfBg = androidx.compose.ui.graphics.Color(0xFF0A0A0A)
private val WfCard = androidx.compose.ui.graphics.Color(0xFF141414)
private val WfLine = androidx.compose.ui.graphics.Color(0xFF2A2A2A)
private val WfText = androidx.compose.ui.graphics.Color.White
private val WfSub = androidx.compose.ui.graphics.Color(0xFF9A9A9A)

private enum class WfType(val label: String, val match: (ApiNewcomerTask) -> Boolean) {
    ALL("全部类型", { true }), DEPOSIT("充值", { it.type == ApiNewcomerTask.Type.FIRST_DEPOSIT }),
    FUTURES("合约", { it.type == ApiNewcomerTask.Type.VOLUME }), EARN("理财", { false }), SPOT("现货", { false }),
}

private val wfFaq = listOf(
    "新用户注册后可以获得哪些奖励？" to "报名新人任务后，在有效期内完成绑定、首充、合约交易等任务，即可领取对应的合约体验金或手续费抵扣券，奖励以任务卡片显示为准。",
    "任务类型与领取规则" to "任务需先报名；完成后状态变为「领取」，点击即可发放至卡券中心。每个任务每位用户仅可领取一次。",
    "任务有效期多久？" to "有效期自报名起计算，过期未完成或未领取的任务将显示「已过期」。",
    "为什么交易量进度没有实时更新？" to "交易量统计存在延迟，请稍后下拉刷新查看最新进度。",
    "奖励什么时候到账？在哪里查看？" to "领取后奖励即时发放至「卡券中心」，可在卡券中心查看与使用。",
    "什么是合约体验金？如何使用？" to "合约体验金可用于合约交易的保证金抵扣，盈利部分可提取，体验金本金到期回收。",
    "什么是手续费抵扣券？如何使用？" to "手续费抵扣券在交易时自动抵扣手续费，需先在卡券中心激活。",
)

@Composable
fun WelfareScreen(
    onBack: () -> Unit,
    onLogin: () -> Unit,
    onOpen: (MorePage) -> Unit,
    onTrade: (Boolean, String) -> Unit,
    onProfile: () -> Unit,
    onDeposit: () -> Unit,
    vm: WelfareViewModel = hiltViewModel(),
) {
    val s by vm.state.collectAsStateWithLifecycle()
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    LaunchedEffect(loggedIn) { vm.load() }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val go: (ApiNewcomerTask) -> Unit = { task ->
        when (welfareTaskRoute(task.type)) {
            WelfareTaskRoute.Profile -> onProfile()
            WelfareTaskRoute.Deposit -> onDeposit()
            WelfareTaskRoute.Futures -> onTrade(true, "BTCUSDT")
            else -> Unit
        }
    }
    MoreFrame("福利中心", onBack, trailing = {
        LbIcon(R.drawable.ic_refresh, 22.dp, LB.p.textPrimary, Modifier.pressable(onClick = vm::load))
    }, centered = true) {
        if (s.loading && s.newcomer == null) MoreLoading(Modifier.weight(1f))
        else if (s.error != null && s.newcomer == null) MoreError(s.error ?: "", vm::load, Modifier.weight(1f))
        else Column(Modifier.weight(1f).background(WfBg).verticalScroll(rememberScrollState())) {
            val n = s.newcomer
            val center = androidx.compose.ui.Alignment.CenterHorizontally
            // ── Hero ──
            Column(Modifier.fillMaxWidth().padding(top = 28.dp, bottom = 8.dp), horizontalAlignment = center) {
                LbImageIcon(R.drawable.ic_gift_big, 120.dp)
                Text("福利中心", Modifier.padding(top = 20.dp), style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.brand)
                Text(if (n?.available == true && n.total != null) "解锁最高 ${n.total} ${n.asset ?: "USDT"} 奖励！" else "福利中心",
                    Modifier.padding(horizontal = 24.dp, vertical = 10.dp), style = lb(28.sp, FontWeight.Bold), color = WfText,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center)
                Row(Modifier.padding(top = 6.dp).pressable(scale = 1f) { if (loggedIn) onOpen(MorePage.COUPONS) else onLogin() }, verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    Text("可用卡券 ", style = lb(FontSize.label), color = WfText)
                    Text(s.vouchers?.toString() ?: "--", style = lbNum(FontSize.label, FontWeight.SemiBold), color = LB.p.brand)
                    LbIcon(R.drawable.ic_chevron_right, 14.dp, WfText, Modifier.padding(start = 4.dp))
                }
                Row(Modifier.padding(top = 20.dp).width(210.dp).height(44.dp).background(androidx.compose.ui.graphics.Color(0xFF1E1E1E), RoundedCornerShape(22.dp))
                    .border(1.dp, WfLine, RoundedCornerShape(22.dp)).pressable {
                        val text = "Linbit 福利中心" + (n?.link?.let { "：$it" } ?: "")
                        ctx.startActivity(android.content.Intent.createChooser(android.content.Intent(android.content.Intent.ACTION_SEND).setType("text/plain").putExtra(android.content.Intent.EXTRA_TEXT, text), "分享"))
                    }, horizontalArrangement = Arrangement.Center, verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    Text("分享", style = lb(FontSize.label, FontWeight.SemiBold), color = WfText)
                    LbIcon(R.drawable.ic_share, 16.dp, WfText, Modifier.padding(start = 8.dp))
                }
                if (n?.cta == ApiHomeNewcomer.Cta.LOGIN || n?.cta == ApiHomeNewcomer.Cta.ENROLL || n?.cta == ApiHomeNewcomer.Cta.KYC) {
                    LbButton(newcomerCta(n.cta?.value), Modifier.padding(top = 14.dp, start = 48.dp, end = 48.dp), loading = s.enrolling) {
                        when (n.cta) { ApiHomeNewcomer.Cta.LOGIN -> onLogin(); ApiHomeNewcomer.Cta.KYC -> onProfile(); else -> vm.enroll() }
                    }
                }
            }
            // ── 任务标签 ──
            var tab by remember { mutableStateOf(0) }
            Row(Modifier.fillMaxWidth().padding(top = 28.dp).padding(horizontal = Spacing.pageH), verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                Text("✦ 新人限时任务", Modifier.weight(1f).pressable(scale = 1f) { tab = 0 }, style = lb(FontSize.title, FontWeight.SemiBold), color = if (tab == 0) LB.p.brand else WfSub)
                Text("每日任务", Modifier.pressable(scale = 1f) { tab = 1 }, style = lb(FontSize.title, FontWeight.SemiBold), color = if (tab == 1) LB.p.brand else WfSub)
            }
            Box(Modifier.padding(top = 12.dp).fillMaxWidth().height(1.dp).background(WfLine))
            if (tab == 1) {
                EmptyState("暂无每日任务", Modifier.fillMaxWidth().padding(vertical = 40.dp))
            } else if (n?.available != true) {
                EmptyState("暂无进行中的新人活动", Modifier.fillMaxWidth().padding(vertical = 40.dp))
            } else {
                n.days?.let { d ->
                    Row(Modifier.padding(horizontal = Spacing.pageH).padding(top = 18.dp)) { Text("$d 天", style = lb(FontSize.body, FontWeight.SemiBold), color = WfText) }
                }
                var type by remember { mutableStateOf(WfType.ALL) }
                Row(Modifier.padding(top = 16.dp).horizontalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    WfType.entries.forEach { t ->
                        Text(t.label, Modifier.background(if (t == type) androidx.compose.ui.graphics.Color(0xFF262626) else androidx.compose.ui.graphics.Color.Transparent, RoundedCornerShape(8.dp))
                            .pressable(scale = 1f) { type = t }.padding(horizontal = 12.dp, vertical = 8.dp), style = lb(FontSize.label, if (t == type) FontWeight.SemiBold else FontWeight.Normal), color = if (t == type) WfText else WfSub)
                    }
                }
                val tasks = n.tasks.orEmpty().filter(type.match)
                if (n.degraded == true) Text("进度暂时无法读取，请稍后刷新", Modifier.padding(horizontal = Spacing.pageH, vertical = 8.dp), style = lb(FontSize.small), color = LB.p.down)
                if (tasks.isEmpty()) EmptyState("暂无任务", Modifier.fillMaxWidth().padding(vertical = 36.dp))
                tasks.forEach { task -> WfTaskCard(task, n.enrolled == true, task.key in s.claiming, onClaim = { vm.claim(task) }, onGo = { go(task) }) }
            }
            // ── FAQ ──
            Text("常见问题", Modifier.padding(horizontal = Spacing.pageH).padding(top = 32.dp, bottom = 8.dp), style = lb(22.sp, FontWeight.Bold), color = WfText)
            wfFaq.forEach { (q, a) ->
                var open by remember { mutableStateOf(false) }
                Column(Modifier.fillMaxWidth().pressable(scale = 1f) { open = !open }.padding(horizontal = Spacing.pageH, vertical = 14.dp)) {
                    Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                        Text(q, Modifier.weight(1f), style = lb(FontSize.body), color = WfText)
                        Text(if (open) "−" else "+", style = lb(FontSize.title), color = WfSub)
                    }
                    if (open) Text(a, Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = WfSub)
                }
                Box(Modifier.padding(horizontal = Spacing.pageH).fillMaxWidth().height(0.5.dp).background(WfLine))
            }
            Spacer(Modifier.height(32.dp))
        }
    }
}

@Composable
private fun WfTaskCard(task: ApiNewcomerTask, enrolled: Boolean, claiming: Boolean, onClaim: () -> Unit, onGo: () -> Unit) {
    val dec = click.cowa.linbit.core.util.Dec::parse
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 16.dp).fillMaxWidth().background(WfCard, RoundedCornerShape(14.dp)).border(1.dp, WfLine, RoundedCornerShape(14.dp))) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Text("新人专享", Modifier.background(androidx.compose.ui.graphics.Color(0xFF1F2A0C), RoundedCornerShape(topEnd = 14.dp, bottomStart = 14.dp)).padding(horizontal = 12.dp, vertical = 5.dp), style = lb(FontSize.small), color = LB.p.brand)
        }
        Column(Modifier.padding(horizontal = 16.dp).padding(bottom = 16.dp)) {
            Row(verticalAlignment = androidx.compose.ui.Alignment.Bottom) {
                Text(task.amount ?: "--", style = lbNum(30.sp, FontWeight.Bold), color = LB.p.brand)
                Text(when (task.kind) { ApiNewcomerTask.Kind.TRIAL -> "USDT 合约体验金"; ApiNewcomerTask.Kind.DEDUCTION -> "USDT 手续费抵扣券"; else -> "USDT" },
                    Modifier.padding(start = 6.dp, bottom = 6.dp), style = lb(FontSize.small), color = WfSub)
            }
            Text(task.title ?: "新手任务", Modifier.padding(top = 10.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = WfText)
            val target = dec(task.target)
            if (target.signum() > 0) {
                val prog = dec(task.progress)
                val tg = click.cowa.linbit.core.util.Fmt.amount(target, 0); val pg = click.cowa.linbit.core.util.Fmt.amount(prog, 0)
                val ratio = (prog.toDouble() / target.toDouble()).coerceIn(0.0, 1.0)
                Text(when (task.type) { ApiNewcomerTask.Type.FIRST_DEPOSIT -> "累计充值 ≥ $tg USDT"; ApiNewcomerTask.Type.VOLUME -> "累计合约交易量 ≥ $tg USDT"; else -> "目标 ${task.target}" },
                    Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = WfSub)
                Row(Modifier.padding(top = 10.dp), verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    Box(Modifier.weight(1f).height(4.dp).background(WfLine, RoundedCornerShape(2.dp))) {
                        Box(Modifier.fillMaxWidth(ratio.toFloat()).fillMaxHeight().background(LB.p.brand, RoundedCornerShape(2.dp)))
                    }
                    Text("${(ratio * 100).toInt()}%", Modifier.padding(start = 8.dp), style = lbNum(FontSize.small), color = WfSub)
                }
                Text("$pg / $tg USDT", Modifier.padding(top = 6.dp), style = lbNum(FontSize.small), color = WfSub)
            } else Text("奖励：${task.amount ?: "--"} " + when (task.kind) { ApiNewcomerTask.Kind.TRIAL -> "USDT 合约体验金"; ApiNewcomerTask.Kind.DEDUCTION -> "USDT 手续费抵扣券"; else -> "USDT" },
                Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = WfSub)
            val (label, active, act) = when (task.status) {
                ApiNewcomerTask.Status.CLAIMABLE -> Triple("领取", true, onClaim)
                ApiNewcomerTask.Status.IN_PROGRESS -> Triple("去完成", true, onGo)
                ApiNewcomerTask.Status.CLAIMED -> Triple("已领取", false, {})
                ApiNewcomerTask.Status.EXPIRED -> Triple("已过期", false, {})
                else -> Triple(if (enrolled) "完成身份认证后可参与" else "报名后可参与", false, {})
            }
            Box(Modifier.padding(top = 16.dp).fillMaxWidth().height(44.dp).background(if (active) LB.p.brand else androidx.compose.ui.graphics.Color(0xFF2A2A2A), RoundedCornerShape(10.dp))
                .then(if (active && !claiming) Modifier.pressable(onClick = act) else Modifier), contentAlignment = androidx.compose.ui.Alignment.Center) {
                Text(if (claiming) "领取中…" else label, style = lb(FontSize.label, FontWeight.SemiBold), color = if (active) androidx.compose.ui.graphics.Color.Black else WfText)
            }
        }
    }
}
