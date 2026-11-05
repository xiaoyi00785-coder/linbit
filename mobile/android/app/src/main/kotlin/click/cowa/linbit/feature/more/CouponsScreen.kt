package click.cowa.linbit.feature.more

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

data class CouponState(
    val items: List<ApiCoupon> = emptyList(),
    val summary: ApiCouponSummary? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val activating: Long? = null,
    val actionError: String? = null,
)

@HiltViewModel
class CouponViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore, val toast: ToastCenter) : ViewModel() {
    private val _state = MutableStateFlow(CouponState())
    val state: StateFlow<CouponState> = _state
    val loggedIn get() = tokens.loggedIn
    var status = "issued"
        private set

    init { viewModelScope.launch { tokens.sessionVersion.collect { _state.value = CouponState() } } }

    fun load(newStatus: String = status) {
        status = newStatus
        _state.value = _state.value.copy(loading = true, error = null, actionError = null)
        viewModelScope.launch {
            val list = async { runCatching { apiCall { api.coupons(status) } } }
            val summary = async { runCatching { apiCall { api.couponSummary() } }.getOrNull() }
            list.await().onSuccess { r ->
                _state.value = _state.value.copy(items = r.items.orEmpty(), summary = summary.await(), loading = false)
            }.onFailure {
                summary.await()
                _state.value = _state.value.copy(loading = false, error = it.lbMessage())
            }
        }
    }

    fun activate(coupon: ApiCoupon) {
        if (_state.value.activating != null) return
        val id = coupon.id ?: return
        _state.value = _state.value.copy(activating = id, actionError = null)
        viewModelScope.launch {
            runCatching { apiCall { api.activateCoupon(id) } }
                .onSuccess { toast.show("激活成功", ToastCenter.Kind.SUCCESS); load() }
                .onFailure { _state.value = _state.value.copy(activating = null, actionError = it.lbMessage()) }
        }
    }
}

@Composable
fun CouponsScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: CouponViewModel = hiltViewModel()) {
    val s by vm.state.collectAsStateWithLifecycle()
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var tab by remember { mutableIntStateOf(0) }
    var confirm by remember { mutableStateOf<ApiCoupon?>(null) }
    val tabs = listOf("issued" to "可使用", "active" to "使用中", "expired" to "已失效")
    LaunchedEffect(tab, loggedIn) { if (loggedIn) vm.load(tabs[tab].first) }
    if (!loggedIn) {
        MoreFrame("卡券中心", onBack) { EmptyState("登录后查看卡券", Modifier.weight(1f)) { LbButton("登录", Modifier.width(160.dp), onClick = onLogin) } }
        return
    }
    MoreFrame("卡券中心", onBack) {
        Row(Modifier.padding(Spacing.pageH), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            StatTile("可使用", s.summary?.available?.toString() ?: "--", modifier = Modifier.weight(1f))
            StatTile("使用中", s.summary?.active?.toString() ?: "--", modifier = Modifier.weight(1f))
        }
        PrimaryTabs(tabs.map { it.second }, tab, { tab = it }, Modifier.padding(horizontal = Spacing.pageH), size = FontSize.label, underline = true)
        if (s.actionError != null) Text(s.actionError ?: "", Modifier.padding(Spacing.pageH), style = lb(FontSize.small), color = LB.p.down)
        when {
            s.loading -> MoreLoading(Modifier.weight(1f))
            s.error != null -> MoreError(s.error ?: "", { vm.load() }, Modifier.weight(1f))
            s.items.isEmpty() -> EmptyState(if (tab == 0) "暂无可用卡券，完成新手任务即可领取" else "暂无卡券", Modifier.weight(1f))
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                s.items.forEach { coupon -> CouponCard(coupon, s.activating == coupon.id) { confirm = coupon } }
            }
        }
    }
    confirm?.let { c ->
        val amount = "${Fmt.amount(Dec.parse(c.amount), 2)} ${c.asset ?: "USDT"}"
        val message = if (c.kind == ApiCoupon.Kind.TRIAL)
            "激活后将向合约账户发放 $amount 体验金，${c.validDays ?: 0} 天内有效。体验金仅可用于开仓，盈利可提取，亏损优先扣除体验金。激活要求合约账户自有资金不低于 $amount。"
        else "激活后 ${c.validDays ?: 0} 天内，交易手续费将优先由本券抵扣，最高 $amount。"
        AlertDialog(
            onDismissRequest = { confirm = null }, title = { Text("激活${c.title ?: "卡券"}") }, text = { Text(message) },
            confirmButton = { TextButton(onClick = { confirm = null; vm.activate(c) }) { Text("确认激活") } },
            dismissButton = { TextButton(onClick = { confirm = null }) { Text("取消") } },
        )
    }
}

@Composable
private fun CouponCard(c: ApiCoupon, activating: Boolean, onActivate: () -> Unit) {
    val dim = c.status == ApiCoupon.Status.EXPIRED || c.status == ApiCoupon.Status.REVOKED
    Row(Modifier.fillMaxWidth().height(112.dp).border(1.dp, LB.p.border, RoundedCornerShape(Radius.lg))) {
        Column(
            Modifier.width(94.dp).fillMaxHeight().background(if (dim) LB.p.fillStrong else LB.p.brand.copy(alpha = 0.1f)),
            horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
        ) {
            Text(Fmt.amount(Dec.parse(c.amount), 2), style = lbNum(24.sp, FontWeight.Bold), color = if (dim) LB.p.textTertiary else LB.p.brand)
            Text(c.asset ?: "USDT", style = lb(FontSize.small), color = if (dim) LB.p.textTertiary else LB.p.brand)
        }
        Column(Modifier.weight(1f).padding(12.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(c.title ?: if (c.kind == ApiCoupon.Kind.TRIAL) "合约体验金" else "手续费抵扣券", Modifier.weight(1f), style = lb(FontSize.body, FontWeight.SemiBold), color = if (dim) LB.p.textTertiary else LB.p.textPrimary, maxLines = 1)
                SmallTag(if (c.kind == ApiCoupon.Kind.TRIAL) "体验金" else "抵扣券")
            }
            when (c.status) {
                ApiCoupon.Status.ACTIVE -> {
                    Text(c.bonus?.remaining?.let { "剩余 ${Fmt.amount(Dec.parse(it), 2)} ${c.asset ?: "USDT"}" } ?: "激活于 ${formatMillis(c.activatedAt)}", style = lbNum(FontSize.small), color = LB.p.textSecondary)
                    Text("有效期至 ${formatMillis(c.bonus?.expiresAt)}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                }
                ApiCoupon.Status.EXPIRED, ApiCoupon.Status.REVOKED -> Text(if (c.status == ApiCoupon.Status.REVOKED) "已作废" else "已过期", style = lb(FontSize.small), color = LB.p.textTertiary)
                else -> {
                    Text("请于 ${formatMillis(c.activateBy)} 前激活", style = lbNum(FontSize.small), color = LB.p.textSecondary)
                    Text("激活后 ${c.validDays ?: 0} 天内有效", style = lb(FontSize.small), color = LB.p.textTertiary)
                }
            }
        }
        if (c.status == ApiCoupon.Status.ISSUED) LbButton("激活", Modifier.width(66.dp).align(Alignment.CenterVertically).padding(end = 10.dp), height = 30.dp, enabled = !activating, loading = activating, onClick = onActivate)
    }
}
