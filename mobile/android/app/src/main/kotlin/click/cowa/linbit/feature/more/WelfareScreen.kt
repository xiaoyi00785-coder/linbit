package click.cowa.linbit.feature.more

import androidx.compose.foundation.background
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

    init {
        viewModelScope.launch { tokens.sessionVersion.collect { _state.value = WelfareState() } }
    }

    fun load() {
        val session = tokens.sessionVersion.value
        _state.value = _state.value.copy(loading = _state.value.newcomer == null, error = null)
        viewModelScope.launch {
            val n = async { runCatching { apiCall { api.homeNewcomer() } } }
            val b = async {
                if (tokens.loggedIn.value) runCatching { apiCall { api.futuresBonus() } }.getOrNull()
                else null
            }
            val newcomer = n.await()
            val bonus = b.await()
            if (session != tokens.sessionVersion.value) return@launch
            _state.value = WelfareState(
                newcomer = newcomer.getOrNull(),
                bonus = bonus,
                loading = false,
                error = newcomer.exceptionOrNull()?.lbMessage(),
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
    MoreFrame("福利中心", onBack) {
        if (s.loading && s.newcomer == null) MoreLoading(Modifier.weight(1f))
        else if (s.error != null && s.newcomer == null) MoreError(s.error ?: "", vm::load, Modifier.weight(1f))
        else Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            val n = s.newcomer
            if (n?.available == true) {
                Column(Modifier.fillMaxWidth().background(LB.p.fill, RoundedCornerShape(Radius.xl)).padding(16.dp)) {
                    Text("新人专享", style = lb(FontSize.caption), color = LB.p.textSecondary)
                    Row(verticalAlignment = androidx.compose.ui.Alignment.Bottom) {
                        Text(n.total ?: "--", style = lbNum(30.sp, FontWeight.Bold), color = LB.p.brand)
                        Spacer(Modifier.width(5.dp))
                        Text(n.asset ?: "USDT", Modifier.padding(bottom = 4.dp), style = lb(FontSize.label, FontWeight.Medium), color = LB.p.textPrimary)
                    }
                    val completed = n.completed?.let { "已完成 $it/${n.tasks?.size ?: 0} · " } ?: ""
                    val expiry = n.deadline?.let { "截止 ${formatTime(it)}" } ?: n.days?.let { "报名后 $it 天内有效" } ?: "完成任务领取奖励"
                    Text(completed + expiry, Modifier.padding(top = 6.dp), style = lb(FontSize.small), color = LB.p.textSecondary)
                    if (n.degraded == true) Text("进度暂时无法读取，请稍后刷新", Modifier.padding(top = 6.dp), style = lb(FontSize.small), color = LB.p.down)
                    LbButton(newcomerCta(n.cta?.value), Modifier.padding(top = 14.dp), enabled = n.cta != ApiHomeNewcomer.Cta.DONE && n.cta != ApiHomeNewcomer.Cta.NONE,
                        loading = s.enrolling) {
                        when (n.cta) {
                            ApiHomeNewcomer.Cta.LOGIN -> onLogin()
                            ApiHomeNewcomer.Cta.KYC -> onProfile()
                            ApiHomeNewcomer.Cta.ENROLL -> vm.enroll()
                            ApiHomeNewcomer.Cta.CLAIM, ApiHomeNewcomer.Cta.CONTINUE, ApiHomeNewcomer.Cta.OPEN -> {
                                n.tasks.orEmpty().firstOrNull { it.status == ApiNewcomerTask.Status.CLAIMABLE }?.let(vm::claim)
                                    ?: n.tasks.orEmpty().firstOrNull { it.status == ApiNewcomerTask.Status.IN_PROGRESS }?.let { task ->
                                        when (welfareTaskRoute(task.type)) {
                                            WelfareTaskRoute.Profile -> onProfile()
                                            WelfareTaskRoute.Deposit -> onDeposit()
                                            WelfareTaskRoute.Futures -> onTrade(true, "BTCUSDT")
                                            else -> Unit
                                        }
                                    }
                            }
                            else -> Unit
                        }
                    }
                }
                Text("任务", style = lb(FontSize.title, FontWeight.SemiBold), color = LB.p.textPrimary)
                if (n.tasks.isNullOrEmpty()) EmptyState("暂无任务")
                n.tasks.orEmpty().forEach { task ->
                    val done = task.status in setOf(ApiNewcomerTask.Status.CLAIMED, ApiNewcomerTask.Status.CLAIMABLE)
                    MoreCard {
                        Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                            LbImageIcon(if (done) R.drawable.ic_mc_check_circle_light else R.drawable.ic_g_gift_light, 26.dp)
                            Spacer(Modifier.width(10.dp))
                            Column(Modifier.weight(1f)) {
                                Text(task.title ?: "新手任务", style = lb(FontSize.label, FontWeight.Medium), color = LB.p.textPrimary)
                                val target = click.cowa.linbit.core.util.Dec.parse(task.target)
                                if (target.signum() > 0) Text("${task.progress ?: "0"} / ${task.target}", Modifier.padding(top = 4.dp), style = lbNum(FontSize.small), color = LB.p.textSecondary)
                                Text(when (task.status) {
                                    ApiNewcomerTask.Status.LOCKED -> if (n.enrolled == true) "完成身份认证后可参与" else "报名后可参与"
                                    ApiNewcomerTask.Status.IN_PROGRESS -> "进行中"
                                    ApiNewcomerTask.Status.CLAIMABLE -> "已完成，可领取"
                                    ApiNewcomerTask.Status.CLAIMED -> "已领取"
                                    ApiNewcomerTask.Status.EXPIRED -> "已过期"
                                    else -> ""
                                }, Modifier.padding(top = 4.dp), style = lb(FontSize.small), color = LB.p.textSecondary)
                            }
                            Spacer(Modifier.width(8.dp))
                            Column(horizontalAlignment = androidx.compose.ui.Alignment.End) {
                                Text("+${task.amount ?: "--"}", style = lbNum(FontSize.label, FontWeight.SemiBold), color = LB.p.brand)
                                when (task.status) {
                                    ApiNewcomerTask.Status.CLAIMABLE -> LbButton(
                                        "领取", Modifier.padding(top = 8.dp).width(76.dp), height = 32.dp,
                                        loading = task.key in s.claiming,
                                    ) { vm.claim(task) }
                                    ApiNewcomerTask.Status.IN_PROGRESS -> LbButton(
                                        when (task.type) {
                                            ApiNewcomerTask.Type.BIND -> "去绑定"
                                            ApiNewcomerTask.Type.FIRST_DEPOSIT -> "去充值划转"
                                            ApiNewcomerTask.Type.VOLUME -> "去合约交易"
                                            else -> "继续任务"
                                        }, Modifier.padding(top = 8.dp).width(108.dp), height = 32.dp,
                                    ) {
                                        when (welfareTaskRoute(task.type)) {
                                            WelfareTaskRoute.Profile -> onProfile()
                                            WelfareTaskRoute.Deposit -> onDeposit()
                                            WelfareTaskRoute.Futures -> onTrade(true, "BTCUSDT")
                                            else -> Unit
                                        }
                                    }
                                    else -> Unit
                                }
                            }
                        }
                    }
                }
            } else {
                EmptyState("暂无进行中的新人活动，敬请期待", Modifier.fillMaxWidth().padding(vertical = 36.dp))
            }
            if (loggedIn && s.bonus != null) {
                Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    Text("合约体验金", Modifier.weight(1f), style = lb(FontSize.title, FontWeight.SemiBold), color = LB.p.textPrimary)
                    Text("可用 ${s.bonus?.available ?: "--"} USDT", style = lbNum(FontSize.small), color = LB.p.brand)
                }
                if (s.bonus?.items.isNullOrEmpty()) EmptyState("暂无体验金")
                s.bonus?.items.orEmpty().forEach { bonus ->
                    MoreCard {
                        Row {
                            Column(Modifier.weight(1f)) {
                                Text(bonus.ref ?: bonus.kind ?: "合约体验金", style = lb(FontSize.label, FontWeight.Medium), color = LB.p.textPrimary)
                                Text("有效期至 ${formatMillis(bonus.expiresAt)}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                            }
                            Column(horizontalAlignment = androidx.compose.ui.Alignment.End) {
                                Text("${bonus.remaining ?: bonus.amount ?: "--"} USDT", style = lbNum(FontSize.label, FontWeight.SemiBold), color = LB.p.up)
                                Text(bonus.status ?: "", style = lb(FontSize.small), color = LB.p.textTertiary)
                            }
                        }
                    }
                }
            }
            LbButton("我的卡券", kind = ButtonKind.SECONDARY) { onOpen(MorePage.COUPONS) }
            Spacer(Modifier.height(16.dp))
        }
    }
}
