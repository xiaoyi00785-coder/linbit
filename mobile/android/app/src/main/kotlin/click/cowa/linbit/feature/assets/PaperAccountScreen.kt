package click.cowa.linbit.feature.assets

import androidx.compose.foundation.background

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import java.util.UUID
import javax.inject.Inject

@HiltViewModel
class PaperAccountViewModel @Inject constructor(
    private val client: PaperTradingClient,
    val tokens: TokenStore,
    private val toast: ToastCenter,
) : androidx.lifecycle.ViewModel() {
    val available: Boolean get() = client.enabled
    var loading by mutableStateOf(false); private set
    var account by mutableStateOf<PaperAccount?>(null); private set
    var balances by mutableStateOf<PaperBalances?>(null); private set
    var failure by mutableStateOf<String?>(null)
    var resetting by mutableStateOf(false); private set
    var resetFailure by mutableStateOf<String?>(null)
    private var epoch = 0L
    val sessionVersion: StateFlow<Long> get() = tokens.sessionVersion

    init { observeSession() }
    private fun observeSession() {
        viewModelScope.launch {
            tokens.sessionVersion.collect {
                epoch += 1
                account = null; balances = null; failure = null; resetFailure = null; resetting = false
                load()
            }
        }
    }
    fun load() {
        if (!tokens.loggedIn.value) { loading = false; failure = null; return }
        val version = epoch
        loading = true; failure = null
        viewModelScope.launch {
            client.refreshFeature()
            if (!client.enabled || version != epoch || !tokens.loggedIn.value) {
                if (version == epoch) { account = null; balances = null; loading = false; failure = client.featureFailure }
                return@launch
            }
            runCatching {
                val act = client.account()
                require(tokens.loggedIn.value && version == epoch) { "Session changed" }
                val page = client.balances()
                require(page.generation == act.generation) { "虚拟账户余额代次不一致，请重试" }
                act to page
            }.onSuccess { (act, page) ->
                if (version == epoch && tokens.loggedIn.value) { account = act; balances = page }
            }.onFailure {
                if (version == epoch && tokens.loggedIn.value) failure = it.lbMessage()
            }
            if (version == epoch) loading = false
        }
    }
    fun reset(idempotencyKey: String, onDone: (Boolean) -> Unit) {
        if (!client.enabled || resetting || loading || account == null || !tokens.loggedIn.value) return
        val version = epoch
        resetting = true; resetFailure = null
        viewModelScope.launch {
            runCatching { client.reset(idempotencyKey) }
                .onSuccess {
                    if (version == epoch && tokens.loggedIn.value) {
                        // Clear the old generation synchronously. URI page response also requires its read-back generation.
                        account = null; balances = null
                        toast.show("虚拟账户已重置，不影响真实资产", ToastCenter.Kind.SUCCESS)
                        onDone(true); load()
                    }
                }
                .onFailure {
                    if (version == epoch && tokens.loggedIn.value) {
                        resetFailure = if (it is ApiFailure.Server && it.httpStatus == 429) "操作较频繁，请${it.retryAfterSeconds?.let { seconds -> "在${seconds}秒后重试" } ?: "稍后重试"}" else it.lbMessage()
                        onDone(false)
                    }
                }
            if (version == epoch) resetting = false
        }
    }
}

@Composable
fun PaperAccountScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: PaperAccountViewModel = hiltViewModel(), onDialogWindow: (android.view.Window?) -> Unit = {}) {
    val loggedIn by vm.tokens.loggedIn.collectAsStateWithLifecycle()
    PaperAccountContent(loggedIn, vm, onBack, onLogin, onDialogWindow)
}

@Composable
private fun PaperAccountContent(loggedIn: Boolean, vm: PaperAccountViewModel, onBack: () -> Unit, onLogin: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit) {
    val p = LB.p
    var showReset by remember { mutableStateOf(false) }
    var resetKey by remember { mutableStateOf("") }
    Column(Modifier.fillMaxSize().background(p.bg).verticalScroll(rememberScrollState()).padding(Spacing.pageH)) {
        AssetsNavBar("模拟交易", onBack = onBack)
        Text("模拟账户", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Text("独立模拟 / 虚拟账户，不影响真实资产", Modifier.padding(top = 4.dp, bottom = 16.dp), style = lb(FontSize.small), color = p.textSecondary)
        when {
            !loggedIn -> EmptyState("登录后可查看模拟账户") { LbButton("登录 / 注册", Modifier.fillMaxWidth(), onClick = onLogin) }
            !vm.available -> {
                Text("模拟功能尚未开放", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                Text("当前版本仅保留独立模拟入口，未发起虚拟或真实交易请求。", Modifier.padding(top = 12.dp), style = lb(FontSize.body), color = p.textSecondary)
            }
            vm.loading -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { CircularProgressIndicator(color = p.brand) }
            vm.failure != null && vm.account == null -> EmptyState(vm.failure ?: "模拟账户暂不可用") { LbButton("重试", Modifier.fillMaxWidth(), onClick = vm::load) }
            vm.account != null -> {
                Text("虚拟账户 · ${vm.account?.quoteAsset ?: "USDT"}", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                Text("账户标识 ${vm.account?.accountId.orEmpty()} · 代次 ${vm.account?.generation ?: 0}", style = lb(FontSize.small), color = p.textSecondary)
                vm.account?.let { KvRow("初始虚拟资金", "${it.initialBalance} ${it.quoteAsset}") }
                Text("虚拟余额", Modifier.padding(vertical = 10.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                if (vm.balances?.items?.isEmpty() == true) Text("暂无虚拟余额", style = lb(FontSize.body), color = p.textSecondary)
                vm.balances?.items?.forEach { balance ->
                    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                        KvRow("资产", balance.asset)
                        KvRow("可用", balance.free)
                        KvRow("锁定", balance.locked)
                        KvRow("合计", balance.total)
                    }
                }
                Text("本批仅提供虚拟账户、余额和重置复核；市价买卖、订单和成交记录尚未开放。此处不会调用真钱API。", Modifier.padding(top = 12.dp), style = lb(FontSize.small), color = p.textSecondary)
                LbButton("重置虚拟账户", Modifier.padding(top = 16.dp), enabled = !vm.resetting && !vm.loading) {
                    resetKey = "android-paper-${UUID.randomUUID()}"
                    vm.resetFailure = null
                    showReset = true
                }
            }
        }
    }
    if (showReset) {
        LbSheet("重置虚拟账户最终复核", { if (!vm.resetting) showReset = false }, showClose = !vm.resetting, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("只重置模拟余额和虚拟交易历史，不影响真实资产。此操作不可用于真实资金。", style = lb(FontSize.body), color = p.textSecondary)
                Text("确认语句：RESET_PAPER_ACCOUNT", style = lb(FontSize.small), color = p.textSecondary)
                vm.resetFailure?.let { Text("操作未完成：$it", style = lb(FontSize.small), color = p.down) }
                LbButton(if (vm.resetting) "提交中…" else "确认重置模拟账户", enabled = !vm.resetting) { vm.reset(resetKey) { ok -> if (ok) showReset = false } }
                LbButton("取消", kind = ButtonKind.OUTLINE, enabled = !vm.resetting) { showReset = false }
            }
        }
    }
}
