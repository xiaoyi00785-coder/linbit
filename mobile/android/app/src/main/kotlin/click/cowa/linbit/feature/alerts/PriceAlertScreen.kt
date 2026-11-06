package click.cowa.linbit.feature.alerts

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import dagger.hilt.android.lifecycle.HiltViewModel
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.feature.assets.AssetsNavBar
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.drop
import javax.inject.Inject

@HiltViewModel
class PriceAlertViewModel @Inject constructor(private val client: PriceAlertApiClient, val tokens: TokenStore) : androidx.lifecycle.ViewModel() {
    var items by mutableStateOf<List<PriceAlert>>(emptyList()); private set
    var page by mutableIntStateOf(0); private set
    var total by mutableIntStateOf(0); private set
    var loading by mutableStateOf(false); private set
    var failure by mutableStateOf<String?>(null); private set
    var retryAfter by mutableStateOf<Int?>(null); private set
    var selected by mutableStateOf<PriceAlert?>(null); private set
    private var epoch = 0L
    private var request = 0L
    val featureState: StateFlow<PriceAlertFeatureSnapshot> get() = client.featureState
    private var lastEnabled = false
    init {
        viewModelScope.launch {
            tokens.sessionVersion.drop(1).collect { epoch++; invalidate(); refresh() }
        }
        viewModelScope.launch {
            featureState.collect { state ->
                if (!state.enabled) { lastEnabled = false; invalidate() }
                else if (!lastEnabled && tokens.loggedIn.value) { lastEnabled = true; load() }
            }
        }
    }
    fun refresh() {
        invalidate()
        if (!tokens.loggedIn.value) return
        viewModelScope.launch { client.refreshFeature() }
    }
    private fun invalidate() {
        request++; items = emptyList(); page = 0; total = 0; loading = false; failure = null; retryAfter = null; selected = null
    }
    fun next() { if (page > 0 && items.size < total) load(page + 1) }
    fun retry() { load(if (failure != null && items.isNotEmpty()) page + 1 else 1) }
    fun details(item: PriceAlert) { if (item in items && !loading) selected = item }
    fun closeDetails() { selected = null }
    fun load(requestedPage: Int = 1) {
        if (!tokens.loggedIn.value || loading || !client.enabled) return
        if (requestedPage < 1 || (page > 0 && requestedPage !in setOf(1, page + 1))) return
        if (page > 0 && requestedPage == page + 1 && items.size >= total) return
        val token = ++request
        val session = epoch
        loading = true; failure = null; retryAfter = null
        viewModelScope.launch {
            runCatching { client.list(requestedPage) }.onSuccess { result ->
                if (session == epoch && token == request && client.enabled && tokens.loggedIn.value) {
                    items = if (requestedPage == 1) result.items else items + result.items
                    page = result.page; total = result.total; selected = if (requestedPage == 1) null else selected
                }
            }.onFailure { error ->
                if (session == epoch && token == request && tokens.loggedIn.value) {
                    if (!client.enabled) invalidate() else {
                        failure = error.lbMessage()
                        retryAfter = (error as? ApiFailure)?.retryAfterSeconds
                    }
                }
            }
            if (session == epoch && token == request) loading = false
        }
    }
}

@Composable
fun PriceAlertScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: PriceAlertViewModel = hiltViewModel(), onDialogWindow: (android.view.Window?) -> Unit = {}) {
    val loggedIn by vm.tokens.loggedIn.collectAsStateWithLifecycle()
    val gate by vm.featureState.collectAsStateWithLifecycle()
    LaunchedEffect(loggedIn) { vm.refresh() }
    PriceAlertContent(onBack, onLogin, vm, loggedIn, gate, onDialogWindow)
}

@Composable
private fun PriceAlertContent(onBack: () -> Unit, onLogin: () -> Unit, vm: PriceAlertViewModel, loggedIn: Boolean, gate: PriceAlertFeatureSnapshot, onDialogWindow: (android.view.Window?) -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxSize().background(p.bg).verticalScroll(rememberScrollState()).padding(Spacing.pageH)) {
        AssetsNavBar("价格预警", onBack)
        Text("价格预警 · 只读", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Text("现货（spot）· 外部指数（INDEX）· 仅触发一次（once）· 应用内通知（in_app）。不代表真实交易所或下单成交。", Modifier.padding(vertical = 12.dp), style = lb(FontSize.small), color = p.textSecondary)
        when {
            !loggedIn -> EmptyState("登录后可查看价格预警") { LbButton("登录 / 注册", Modifier.fillMaxWidth(), onClick = onLogin) }
            !gate.checked && gate.failure == null -> CircularProgressIndicator(color = p.brand)
            !gate.enabled -> EmptyState(gate.failure ?: "价格预警功能尚未开放") {
                Text("仅当公开功能状态明确为 true 时读取列表；错误、缺失或 false 均不访问预警接口。", Modifier.padding(vertical = 8.dp), style = lb(FontSize.body), color = p.textSecondary)
                LbButton("重试", Modifier.fillMaxWidth(), testTag = "alert-gate-retry") { vm.refresh() }
            }
            vm.loading && vm.page == 0 -> CircularProgressIndicator(color = p.brand)
            vm.failure != null && vm.page == 0 -> EmptyState("价格预警暂不可用") {
                Text("操作未完成：${vm.failure}", style = lb(FontSize.body), color = p.down)
                RetryRow(vm)
            }
            else -> {
                vm.items.forEachIndexed { index, item ->
                    Column(Modifier.fillMaxWidth().padding(vertical = 7.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                        val chinese = if (item.condition == "gte") "指数价格 ≥ ${item.threshold}" else "指数价格 ≤ ${item.threshold}"
                        Text(item.symbol, style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                        AlertKvRow("阈值 / 状态", "$chinese / ${item.status}")
                        AlertKvRow("价格来源", "spot · INDEX · once · in_app")
                        AlertKvRow("通知与上限", "${item.deliveryStatus} · activePerUser ${item.limit.activePerUser}")
                        LbButton("查看详情", kind = ButtonKind.OUTLINE, enabled = !vm.loading, testTag = "alert-detail-$index") { vm.details(item) }
                    }
                }
                if (vm.items.isEmpty() && !vm.loading) Text("暂无价格预警", Modifier.padding(vertical = 18.dp), style = lb(FontSize.body), color = p.textSecondary)
                vm.failure?.let { Text("操作未完成：$it", style = lb(FontSize.body), color = p.down); RetryRow(vm) }
                Text("第${vm.page}页 · ${vm.items.size}/${vm.total}${if (vm.items.size >= vm.total) " · 已到末页" else ""}", style = lb(FontSize.small), color = p.textSecondary)
                if (vm.items.size < vm.total) LbButton(if (vm.loading) "加载中…" else "下一页", enabled = !vm.loading, testTag = "alert-next") { vm.next() }
            }
        }
    }
    if (vm.selected != null) alertDetail(vm, onDialogWindow)
}

@Composable private fun RetryRow(vm: PriceAlertViewModel) {
    vm.retryAfter?.let { Text("${it} 秒后可重试", style = lb(FontSize.small), color = LB.p.down) }
    LbButton("重试", enabled = !vm.loading, testTag = "alert-retry") { vm.retry() }
}

@Composable private fun alertDetail(vm: PriceAlertViewModel, onDialogWindow: (android.view.Window?) -> Unit) {
    val p = LB.p
    val item = vm.selected ?: return
    click.cowa.linbit.feature.auth.LbSheet("价格预警详情 · 只读", { vm.closeDetails() }, showClose = true, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(9.dp)) {
            AlertKvRow("预警ID", item.alertId)
            AlertKvRow("市场 / 价格源", "${item.market} / ${item.priceSource}")
            AlertKvRow("条件 / 阈值", "${item.condition} / ${item.threshold}")
            AlertKvRow("触发方式", "${item.triggerMode} · ${item.channels.joinToString()}")
            AlertKvRow("状态 / 通知", "${item.status} / ${item.deliveryStatus}")
            AlertKvRow("修订版 / active上限", "${item.revision} / ${item.limit.activePerUser}")
            AlertKvRow("最后指数价", item.lastObservedPrice ?: "未观察")
            AlertKvRow("触发价", item.triggeredPrice ?: "未触发")
            AlertKvRow("备注", item.note ?: "无")
            Text("只读预警信息，不创建、修改、删除提醒，也不执行交易。", style = lb(FontSize.small), color = p.textSecondary)
        }
    }
}

@Composable private fun AlertKvRow(label: String, value: String) {
    val p = LB.p
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, Modifier.weight(0.85f), style = lb(FontSize.small), color = p.textSecondary)
        Text(value, Modifier.weight(1.15f), style = lb(FontSize.small), color = p.textPrimary)
    }
}
