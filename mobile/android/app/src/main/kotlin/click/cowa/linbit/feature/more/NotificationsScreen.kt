package click.cowa.linbit.feature.more

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.feature.profile.InfoNote
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

data class NotificationState(
    val items: List<ApiNotification> = emptyList(),
    val total: Int = 0,
    val unread: Int = 0,
    val byKind: Map<String, Int> = emptyMap(),
    val loading: Boolean = true,
    val error: String? = null,
    val page: Int = 1,
    val prefs: ApiNotifyPrefs? = null,
    val saving: Boolean = false,
)

@HiltViewModel
class NotificationViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    private val _state = MutableStateFlow(NotificationState())
    val state: StateFlow<NotificationState> = _state
    val loggedIn get() = tokens.loggedIn
    var kind: String? = null
        private set
    internal var holdEvidence: Boolean = false

    /** 离屏证据：消息列表示例。 */
    internal fun seedEvidenceNotifications(items: List<ApiNotification>, unread: Int = 0) {
        holdEvidence = true
        _state.value = NotificationState(items = items, total = items.size, unread = unread, loading = false)
    }

    private fun isCurrent(session: Long) = session == tokens.sessionVersion.value
    private var loadSequence = 0L
    private val optimisticReadVersions = mutableMapOf<String, Long>()
    private var markAllVersion = 0L

    init {
        viewModelScope.launch {
            tokens.sessionVersion.collect {
                kind = null
                optimisticReadVersions.clear()
                markAllVersion++
                loadSequence++
                _state.value = NotificationState()
            }
        }
    }

    fun load(tab: String? = kind, append: Boolean = false) {
        if (holdEvidence) return
        kind = tab
        val session = tokens.sessionVersion.value
        val readAll = markAllVersion
        val sequence = ++loadSequence
        val nextPage = if (append) _state.value.page + 1 else 1
        _state.value = _state.value.copy(loading = !append, error = null)
        viewModelScope.launch {
            val result = runCatching { apiCall { api.notifications(tab, nextPage) } }
            if (!isCurrent(session) || sequence != loadSequence || readAll != markAllVersion) return@launch
            val pending = optimisticReadVersions.toMap()
            result.onSuccess { r ->
                val seen = _state.value.items.mapNotNull { it.id }.toSet()
                val merged = (if (append) _state.value.items + r.items.orEmpty().filter { it.id !in seen } else r.items.orEmpty())
                    .map { if (it.id in pending) it.copy(read = true) else it }
                val optimisticUnread = r.items.orEmpty().count { it.id in pending && it.read == false }
                val kindCounts = r.items.orEmpty().filter { it.id in pending && it.read == false }
                    .groupingBy { it.kind?.value }.eachCount()
                _state.value = _state.value.copy(
                    items = merged,
                    total = r.total ?: 0,
                    unread = (r.unread ?: 0).minus(optimisticUnread).coerceIn(0, r.total ?: Int.MAX_VALUE),
                    byKind = r.unreadByKind.orEmpty().mapValues { (k, count) ->
                        (count - (kindCounts[k] ?: 0)).coerceAtLeast(0)
                    },
                    page = nextPage, loading = false,
                )
            }.onFailure {
                if (append) toast.error(it)
                else _state.value = _state.value.copy(loading = false, error = it.lbMessage())
            }
        }
    }

    fun markRead(item: ApiNotification) {
        val id = item.id ?: return
        val current = _state.value.items.firstOrNull { it.id == id } ?: return
        if (current.read != false) return
        val session = tokens.sessionVersion.value
        val readAll = markAllVersion
        optimisticReadVersions[id] = readAll
        _state.value = _state.value.copy(
            items = _state.value.items.map { if (it.id == id) it.copy(read = true) else it },
            unread = (_state.value.unread - 1).coerceAtLeast(0),
            byKind = current.kind?.let { k ->
                _state.value.byKind + (k.value to (_state.value.byKind[k.value] ?: 0).minus(1).coerceAtLeast(0))
            } ?: _state.value.byKind,
        )
        viewModelScope.launch {
            try {
                runCatching { apiCall { api.readNotifications(ApiReadNotificationsRequest(ids = listOf(id))) } }
                .onFailure {
                    // 只撤销本条，且仍被标为已读时才回滚；不得覆盖其他条目、分页或新账号状态。
                    if (isCurrent(session) && markAllVersion == readAll) {
                        val rollback = _state.value.items.any { it.id == id && it.read == true } && _state.value.unread < _state.value.total
                        if (rollback) {
                            _state.value = _state.value.copy(
                                items = _state.value.items.map { if (it.id == id) it.copy(read = false) else it },
                                unread = _state.value.unread + 1,
                                byKind = current.kind?.let { k ->
                                    _state.value.byKind + (k.value to (_state.value.byKind[k.value] ?: 0) + 1)
                                } ?: _state.value.byKind,
                            )
                        }
                        toast.error(it)
                    }
                }
            } finally {
                if (isCurrent(session) && optimisticReadVersions[id] == readAll) optimisticReadVersions.remove(id)
            }
        }
    }

    fun readAll() {
        val session = tokens.sessionVersion.value
        val tab = kind
        viewModelScope.launch {
            runCatching { apiCall { api.readNotifications(ApiReadNotificationsRequest(all = true, kind = tab)) } }
                .onSuccess {
                    if (!isCurrent(session)) return@onSuccess
                    markAllVersion++
                    optimisticReadVersions.clear()
                    toast.show("已全部标为已读", ToastCenter.Kind.SUCCESS)
                    _state.value = _state.value.copy(
                        items = _state.value.items.map { it.copy(read = true) },
                        unread = if (tab == null) 0 else _state.value.unread,
                        byKind = if (tab == null) emptyMap() else _state.value.byKind + (tab to 0),
                    )
                    load()
                }.onFailure { if (isCurrent(session)) toast.error(it) }
        }
    }

    fun loadPrefs() {
        if (_state.value.prefs != null) return
        val session = tokens.sessionVersion.value
        viewModelScope.launch {
            runCatching { apiCall { api.notificationSettings() } }
                .onSuccess { if (isCurrent(session)) _state.value = _state.value.copy(prefs = it) }
                .onFailure { if (isCurrent(session)) toast.error(it) }
        }
    }

    fun setPref(key: String, value: Boolean) {
        if (_state.value.saving) return
        val session = tokens.sessionVersion.value
        _state.value = _state.value.copy(saving = true)
        viewModelScope.launch {
            val patch = ApiNotifyPrefsPatch(
                emailSystem = if (key == "system") value else null,
                emailTrade = if (key == "trade") value else null,
                emailActivity = if (key == "activity") value else null,
            )
            runCatching { apiCall { api.updateNotificationSettings(patch) } }
                .onSuccess { if (isCurrent(session)) _state.value = _state.value.copy(prefs = it, saving = false) }
                .onFailure {
                    if (isCurrent(session)) {
                        _state.value = _state.value.copy(saving = false)
                        toast.error(it)
                    }
                }
        }
    }
}

@Composable
fun NotificationsScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: NotificationViewModel = hiltViewModel()) {
    var tab by remember { mutableIntStateOf(0) }
    var settings by remember { mutableStateOf(false) }
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val s by vm.state.collectAsStateWithLifecycle()
    val kinds = listOf(null to "全部", "system" to "系统", "trade" to "交易", "activity" to "活动", "security" to "安全")
    LaunchedEffect(tab, settings, loggedIn) { if (loggedIn) { if (settings) vm.loadPrefs() else vm.load(kinds[tab].first) } }
    if (!loggedIn) {
        MoreFrame("消息通知", onBack) { EmptyState("登录后查看消息通知", Modifier.weight(1f)) { LbButton("登录", Modifier.width(160.dp), onClick = onLogin) } }
        return
    }
    if (settings) {
        NotificationSettings(s, vm, onBack = { settings = false })
        return
    }
    MoreFrame("消息通知", onBack, trailing = {
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            LbIcon(R.drawable.ic_check, 22.dp, if (s.unread > 0) LB.p.textPrimary else LB.p.textTertiary,
                Modifier.pressable(enabled = s.unread > 0, scale = 1f) { vm.readAll() })
            LbIcon(R.drawable.ic_more, 22.dp, LB.p.textPrimary, Modifier.pressable(scale = 1f) { settings = true })
        }
    }) {
        ChipBar(kinds.map { it.second }, tab, { tab = it })
        when {
            s.loading && s.items.isEmpty() -> MoreLoading(Modifier.weight(1f))
            s.error != null && s.items.isEmpty() -> MoreError(s.error ?: "", { vm.load() }, Modifier.weight(1f))
            s.items.isEmpty() -> EmptyState("暂无消息", Modifier.weight(1f))
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
                s.items.forEach { item ->
                    var expanded by remember(item.id) { mutableStateOf(false) }
                    val unread = item.read == false
                    Row(Modifier.fillMaxWidth().pressable(scale = 1f) {
                        expanded = !expanded
                        vm.markRead(item)
                    }.padding(horizontal = Spacing.pageH, vertical = 14.dp), verticalAlignment = Alignment.Top) {
                        Box(Modifier.size(36.dp).background(LB.p.fill, CircleShape), contentAlignment = Alignment.Center) {
                            LbIcon(if (item.kind == ApiNotification.Kind.TRADE) R.drawable.ic_candles else R.drawable.ic_bell, 17.dp)
                            if (unread) Box(Modifier.align(Alignment.TopEnd).size(8.dp).background(LB.p.down, CircleShape))
                        }
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Row {
                                Text(item.title ?: "", Modifier.weight(1f), style = lb(FontSize.body, if (unread) FontWeight.SemiBold else FontWeight.Normal), color = LB.p.textPrimary, maxLines = 1)
                                Spacer(Modifier.width(8.dp))
                                Text(formatMillis(item.time), style = lbNum(FontSize.small), color = LB.p.textTertiary)
                            }
                            Text(item.body ?: "", style = lb(FontSize.small), color = LB.p.textSecondary, maxLines = if (expanded) Int.MAX_VALUE else 2)
                            if (expanded) when (item.emailStatus) {
                                "sent" -> Text("已同步发送至邮箱", style = lb(FontSize.small), color = LB.p.textTertiary)
                                "pending" -> Text("邮件发送中", style = lb(FontSize.small), color = LB.p.textTertiary)
                                "failed" -> Text("邮件发送失败", style = lb(FontSize.small), color = LB.p.textTertiary)
                                "no_address" -> Text("未绑定邮箱，仅站内通知", style = lb(FontSize.small), color = LB.p.textTertiary)
                            }
                        }
                    }
                }
                if (s.items.size < s.total) LbButton("加载更多", Modifier.padding(Spacing.pageH), kind = ButtonKind.SECONDARY, height = 38.dp) { vm.load(append = true) }
            }
        }
    }
}

@Composable
private fun NotificationSettings(s: NotificationState, vm: NotificationViewModel, onBack: () -> Unit) {
    MoreFrame("通知设置", onBack) {
        val prefs = s.prefs
        if (prefs == null) MoreLoading(Modifier.weight(1f))
        else Column(Modifier.verticalScroll(rememberScrollState()).padding(Spacing.pageH)) {
            Text("邮件通知", Modifier.padding(vertical = 12.dp), style = lb(FontSize.small), color = LB.p.textTertiary)
            PrefToggle("系统公告", "平台维护、规则变更等重要公告", prefs.emailSystem != false, !s.saving) { vm.setPref("system", it) }
            PrefToggle("交易通知", "委托成交、强平预警、充提到账", prefs.emailTrade != false, !s.saving) { vm.setPref("trade", it) }
            PrefToggle("活动推送", "福利活动、新币上线等营销信息", prefs.emailActivity == true, !s.saving) { vm.setPref("activity", it) }
            PrefToggle("安全通知", "登录提醒、密码与安全设置变更", true, false) {}
            Box(Modifier.padding(top = 16.dp)) { InfoNote("为保障账户安全，安全类通知将始终通过邮件发送，无法关闭。站内信不受此设置影响。") }
        }
    }
}

@Composable
private fun PrefToggle(title: String, subtitle: String, checked: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 68.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = lb(FontSize.body), color = LB.p.textPrimary)
            Text(subtitle, style = lb(FontSize.small), color = LB.p.textTertiary)
        }
        Switch(checked, onChange, enabled = enabled, colors = SwitchDefaults.colors(checkedTrackColor = LB.p.brand))
    }
}
