package click.cowa.linbit.feature.more

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
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
import click.cowa.linbit.feature.profile.InfoNote
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject
import java.math.BigDecimal

/** 合伙人：未登录只加载公开规则；申请、看板与团队全部要求登录。 */
data class PartnerState(
    val me: ApiPartnerMe? = null,
    val rules: ApiPartnerRules? = null,
    val summary: ApiPartnerSummary? = null,
    val links: ApiPartnerLinks? = null,
    val commissions: List<ApiPartnerCommissionsItemsInner> = emptyList(),
    val team: List<ApiPartnerTeamItemsInner> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
    val listError: String? = null,
    val busy: Boolean = false,
)

@HiltViewModel
class PartnerViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    private val _state = MutableStateFlow(PartnerState())
    val state: StateFlow<PartnerState> = _state
    val loggedIn get() = tokens.loggedIn
    var listTab = 0; var kind: String? = null

    init { viewModelScope.launch { tokens.sessionVersion.collect { _state.value = PartnerState() } } }

    fun load() {
        _state.value = _state.value.copy(loading = true, error = null)
        viewModelScope.launch {
            val rules = async { runCatching { apiCall { api.partnerRules() } }.getOrNull() }
            if (!tokens.loggedIn.value) {
                _state.value = _state.value.copy(rules = rules.await(), loading = false)
            } else {
                val me = async { runCatching { apiCall { api.partnerMe() } } }
                val result = me.await()
                _state.value = _state.value.copy(me = result.getOrNull(), rules = rules.await(), loading = false, error = result.exceptionOrNull()?.lbMessage())
                if (result.getOrNull()?.status == ApiPartnerMe.Status.ACTIVE) loadDashboard()
            }
        }
    }

    fun loadDashboard() {
        viewModelScope.launch {
            val summary = async { runCatching { apiCall { api.partnerSummary() } }.getOrNull() }
            val links = async { runCatching { apiCall { api.partnerLinks() } }.getOrNull() }
            loadList()
            _state.value = _state.value.copy(summary = summary.await(), links = links.await())
        }
    }

    fun loadList(tab: Int = listTab, newKind: String? = kind) {
        listTab = tab; kind = newKind
        _state.value = _state.value.copy(listError = null)
        viewModelScope.launch {
            val result = if (tab == 0) runCatching { apiCall { api.partnerCommissions(kind) }.items.orEmpty() }
                else runCatching { apiCall { api.partnerTeam() }.items.orEmpty() }
            result.onSuccess {
                _state.value = if (tab == 0) _state.value.copy(commissions = it.filterIsInstance<ApiPartnerCommissionsItemsInner>()) else _state.value.copy(team = it.filterIsInstance<ApiPartnerTeamItemsInner>())
            }.onFailure { _state.value = _state.value.copy(listError = it.lbMessage()) }
        }
    }

    fun apply(contact: String, channel: String, scale: String, onDone: () -> Unit) {
        _state.value = _state.value.copy(busy = true)
        viewModelScope.launch {
            runCatching { apiCall { api.applyPartner(ApiPartnerApplyRequest(contact.trim(), channel, scale)) } }
                .onSuccess { toast.show("申请已提交", ToastCenter.Kind.SUCCESS); _state.value = _state.value.copy(me = it, busy = false); onDone() }
                .onFailure { _state.value = _state.value.copy(busy = false); toast.error(it) }
        }
    }
}

@Composable
fun PartnerScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: PartnerViewModel = hiltViewModel()) {
    val s by vm.state.collectAsStateWithLifecycle()
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var applying by remember { mutableStateOf(false) }
    var tab by remember { mutableIntStateOf(0) }
    var kind by remember { mutableIntStateOf(0) }
    val kinds = listOf(null to "全部", "futures" to "合约", "spot" to "现货", "override" to "团队奖励", "referral" to "邀请")
    LaunchedEffect(loggedIn) { vm.load() }
    LaunchedEffect(tab, kind) { if (s.me?.status == ApiPartnerMe.Status.ACTIVE) vm.loadList(tab, kinds[kind].first) }
    MoreFrame("合伙人计划", onBack) {
        when {
            s.loading && s.me == null && loggedIn -> MoreLoading(Modifier.weight(1f))
            s.error != null -> MoreError(s.error ?: "", vm::load, Modifier.weight(1f))
            s.me?.status == ApiPartnerMe.Status.ACTIVE -> PartnerDashboard(s, tab, { tab = it }, kind, { kind = it }, vm)
            else -> PartnerLanding(s, loggedIn, onLogin) { applying = true }
        }
    }
    if (applying) PartnerApplyDialog(s.busy, { applying = false }) { c, ch, sc -> vm.apply(c, ch, sc) { applying = false } }
}

@Composable
private fun PartnerLanding(s: PartnerState, loggedIn: Boolean, onLogin: () -> Unit, onApply: () -> Unit) {
    val status = s.me?.partner?.status ?: s.me?.status?.value ?: "none"
    val tiers = s.rules?.tiers.orEmpty()
    Column(Modifier.verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text("成为 Linbit 合伙人", style = lb(22.sp, FontWeight.Bold), color = LB.p.textPrimary)
        // 规则未下发时不展示「0%」，避免离线/缺口被读成真实返佣
        val max = tiers.maxOfOrNull { Dec.parse(it.futuresRate) }
        Text(
            if (max == null) "邀请好友交易，享手续费返佣，每日结算（比例以服务端规则为准）"
            else "邀请好友交易，最高享 ${Fmt.price(max.multiply(BigDecimal(100)), 0)}% 手续费返佣，每日结算",
            style = lb(FontSize.body), color = LB.p.textSecondary,
        )
        when (status) {
            "pending" -> InfoNote("申请审核中，预计 1-3 个工作日内完成审核")
            "rejected" -> InfoNote("申请未通过：${s.me?.partner?.reviewNote ?: "请完善资料后重新申请"}", warning = true)
            "disabled" -> InfoNote("合伙人资格已停用，如有疑问请联系客服", warning = true)
        }
        if (tiers.isNotEmpty()) {
            Text("返佣等级", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
            MoreCard {
                tiers.forEach { tier ->
                    Row(Modifier.height(36.dp)) {
                        Text(tier.name ?: "--", Modifier.weight(1f), style = lb(FontSize.small, FontWeight.Medium), color = LB.p.textPrimary)
                        Text("≥ ${Fmt.amount(Dec.parse(tier.minVolume30d), 0)}", Modifier.weight(1f), style = lbNum(FontSize.small), color = LB.p.textSecondary)
                        Text("${Fmt.amount(Dec.parse(tier.futuresRate).multiply(BigDecimal(100)), 0)}% / ${Fmt.amount(Dec.parse(tier.spotRate).multiply(BigDecimal(100)), 0)}%", style = lbNum(FontSize.small), color = LB.p.up)
                    }
                }
            }
        }
        InfoNote("佣金每日 UTC 结算，以 ${s.rules?.payoutAsset ?: "USDT"} 发放至资金账户。实际比例以服务端规则为准。")
        when (status) {
            "pending", "disabled" -> LbButton(if (status == "pending") "审核中" else "资格已停用", enabled = false) {}
            else -> LbButton(if (status == "rejected") "重新申请" else "立即申请") { if (loggedIn) onApply() else onLogin() }
        }
    }
}

@Composable
private fun PartnerDashboard(s: PartnerState, tab: Int, onTab: (Int) -> Unit, kind: Int, onKind: (Int) -> Unit, vm: PartnerViewModel) {
    val summary = s.summary
    val clipboard = LocalClipboardManager.current
    Column(Modifier.verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        MoreCard {
            Row {
                SmallTag(summary?.tierName ?: "合伙人", LB.p.brand)
                Spacer(Modifier.weight(1f))
                Text("合约 ${percent(summary?.futuresRate)} · 现货 ${percent(summary?.spotRate)}", style = lbNum(FontSize.small), color = LB.p.textSecondary)
            }
            Text("累计返佣（${summary?.payoutAsset ?: "USDT"}）", style = lb(FontSize.small), color = LB.p.textSecondary)
            Text(amount(summary?.total), style = lbNum(28.sp, FontWeight.Bold), color = LB.p.textPrimary)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                StatTile("昨日", amount(summary?.yesterday), LB.p.up, Modifier.weight(1f))
                StatTile("本月", amount(summary?.month), modifier = Modifier.weight(1f))
                StatTile("待结算", amount(summary?.unsettledEstimate), modifier = Modifier.weight(1f))
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                StatTile("直属邀请", summary?.directs?.toString() ?: "--", modifier = Modifier.weight(1f))
                StatTile("下级合伙人", summary?.directPartners?.toString() ?: "--", modifier = Modifier.weight(1f))
                StatTile("团队奖励", amount(summary?.overrideTotal), modifier = Modifier.weight(1f))
            }
            summary?.nextTier?.let { next ->
                val need = Dec.parse(next.minVolume30d); val gap = Dec.parse(next.gap)
                val progress = if (need > BigDecimal.ZERO) (need - gap).divide(need).toFloat().coerceIn(0f, 1f) else 1f
                Text("距 ${next.name ?: "下一等级"} 还差 ${Fmt.amount(gap, 0)} USDT 交易量", style = lb(FontSize.small), color = LB.p.textSecondary)
                LinearProgressIndicator(progress = { progress }, color = LB.p.brand)
            }
        }
        MoreCard {
            Text("我的推广链接", style = lb(FontSize.body, FontWeight.SemiBold), color = LB.p.textPrimary)
            CopyRow("邀请码", s.links?.code, vm)
            CopyRow("邀请链接", s.links?.registerUrl ?: s.links?.url, vm)
        }
        PrimaryTabs(listOf("佣金明细", "我的团队"), tab, onTab, size = FontSize.label, underline = true)
        if (tab == 0) ChipBar(listOf("全部", "合约", "现货", "团队奖励", "邀请"), kind, onKind)
        if (s.listError != null) MoreError(s.listError, { vm.loadList() })
        else if (tab == 0) {
            if (s.commissions.isEmpty()) EmptyState("暂无佣金记录，分享邀请链接开始赚取返佣")
            s.commissions.forEach { item -> MoreCard {
                Row {
                    Column(Modifier.weight(1f)) {
                        Text(when (item.kind) { ApiPartnerCommissionsItemsInner.Kind.FUTURES -> "合约返佣"; ApiPartnerCommissionsItemsInner.Kind.SPOT -> "现货返佣"; ApiPartnerCommissionsItemsInner.Kind.OVERRIDE -> "团队奖励"; else -> "邀请奖励" }, style = lb(FontSize.body, FontWeight.Medium), color = LB.p.textPrimary)
                        Text("UID ${item.sourceUid ?: "--"} · 比例 ${percent(item.rate)}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                    }
                    Column(horizontalAlignment = androidx.compose.ui.Alignment.End) {
                        Text("+${amount(item.amount)} ${item.asset ?: "USDT"}", style = lbNum(FontSize.body, FontWeight.SemiBold), color = LB.p.up)
                        Text(item.day ?: formatMillis(item.settledAt), style = lbNum(FontSize.small), color = LB.p.textTertiary)
                    }
                }
            } }
        } else {
            if (s.team.isEmpty()) EmptyState("暂无团队成员")
            s.team.forEach { item -> MoreCard {
                Row {
                    Column(Modifier.weight(1f)) {
                        Row { Text("UID ${item.uid ?: "--"}", style = lbNum(FontSize.body, FontWeight.Medium), color = LB.p.textPrimary); if (item.isPartner == true) { Spacer(Modifier.width(6.dp)); SmallTag("合伙人", LB.p.brand) } }
                        Text("加入 ${formatMillis(item.joinedAt)} · KYC Lv.${item.kycLevel ?: 0}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                    }
                    Column(horizontalAlignment = androidx.compose.ui.Alignment.End) {
                        Text("+${amount(item.commission)} USDT", style = lbNum(FontSize.body, FontWeight.SemiBold), color = LB.p.up)
                        Text("30 日合约量 ${Fmt.amount(Dec.parse(item.futuresVolume30d), 0)}", style = lbNum(FontSize.small), color = LB.p.textTertiary)
                    }
                }
            } }
        }
    }
}

@Composable
private fun CopyRow(label: String, value: String?, vm: PartnerViewModel) {
    val clipboard = LocalClipboardManager.current
    Row {
        Text(label, style = lb(FontSize.small), color = LB.p.textSecondary)
        Spacer(Modifier.weight(1f))
        Text(value ?: "--", Modifier.weight(1f).pressable(scale = 1f) {
            if (value != null) { clipboard.setText(AnnotatedString(value)); vm.toast.show("已复制", ToastCenter.Kind.SUCCESS) }
        }, style = lbNum(FontSize.small), color = LB.p.textPrimary, maxLines = 1)
    }
}

private fun amount(v: String?) = v?.let { Fmt.amount(Dec.parse(it), 2) } ?: "--"
private fun percent(v: String?) = v?.let { Fmt.price(Dec.parse(it).multiply(BigDecimal(100)), 1).trimEnd('0').trimEnd('.') + "%" } ?: "--"

@Composable
private fun PartnerApplyDialog(busy: Boolean, onDismiss: () -> Unit, onSubmit: (String, String, String) -> Unit) {
    var contact by remember { mutableStateOf("") }
    var channel by remember { mutableIntStateOf(0) }
    var scale by remember { mutableIntStateOf(1) }
    val channels = listOf("社群", "自媒体", "KOL", "机构", "其他")
    val scales = listOf("< 1,000", "1,000 - 10,000", "10,000 - 100,000", "> 100,000")
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() }, title = { Text("申请成为合伙人") },
        text = { Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            LbTextField(contact, { contact = it }, "Telegram / 邮箱 / 微信")
            Text("推广渠道", style = lb(FontSize.small), color = LB.p.textSecondary)
            ChipBar(channels, channel, { channel = it })
            Text("用户规模", style = lb(FontSize.small), color = LB.p.textSecondary)
            ChipBar(scales, scale, { scale = it })
        } },
        confirmButton = { TextButton(enabled = contact.trim().length >= 3 && !busy, onClick = { onSubmit(contact, channels[channel], scales[scale]) }) { Text(if (busy) "提交中" else "提交申请") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}
