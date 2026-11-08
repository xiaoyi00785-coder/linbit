package click.cowa.linbit.feature.profile

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.storage.*
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import androidx.compose.foundation.background
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import javax.inject.Inject

@HiltViewModel
class AccountPreferencesViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore) : ViewModel() {
    var preferences by mutableStateOf<ApiAccountPreferences?>(null); private set
    var error by mutableStateOf<String?>(null); private set
    var saving by mutableStateOf(false); private set
    init { viewModelScope.launch { tokens.sessionVersion.collect { preferences = null; error = null; QuoteDisplay.currency = "USD"; if (tokens.loggedIn.value) load() } } }
    fun load() {
        val session = tokens.sessionVersion.value
        viewModelScope.launch { val result = runCatching { apiCall { api.accountPreferences() } }; if (session == tokens.sessionVersion.value) result.onSuccess { preferences = it; QuoteDisplay.currency = it.quoteCurrency; error = null }.onFailure { error = it.lbMessage() } }
    }
    fun select(currency: String, onDone: () -> Unit) {
        if (saving) return
        val session = tokens.sessionVersion.value
        saving = true
        viewModelScope.launch {
            val result = runCatching { apiCall { api.updateAccountPreferences(ApiUpdateAccountPreferencesRequest(currency)) } }
            if (session != tokens.sessionVersion.value) { saving = false; return@launch }
            result.onSuccess { preferences = it; QuoteDisplay.currency = it.quoteCurrency; error = null; onDone() }.onFailure { error = it.lbMessage() }
            saving = false
        }
    }
}

/** 默认充值账户（接入点 GET/PUT account/deposit-target；字段见 docs/batch3-backend-fields.md）。 */
@Serializable data class ApiDepositTarget(val account: String)

enum class DepositTarget(val key: String, val title: String, val desc: String) {
    FUNDING("funding", "资金账户", "充值后资产将自动转入资金账户"),
    FUTURES("futures", "合约账户", "充值后资产将自动转入合约账户；不支持的资产将存入资金账户"),
    SPOT("spot", "现货账户", "充值后资产将自动转入现货账户"),
}

@HiltViewModel
class DepositTargetViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore, val toast: click.cowa.linbit.ui.components.ToastCenter) : ViewModel() {
    /** null = 尚未读到服务端设置；服务端未提供时 [unsupported] 为真，此时真实到账账户为资金账户。 */
    var target by mutableStateOf<DepositTarget?>(null); private set
    var unsupported by mutableStateOf(false); private set
    var error by mutableStateOf<String?>(null); private set
    var saving by mutableStateOf(false); private set
    init { viewModelScope.launch { tokens.sessionVersion.collect { target = null; unsupported = false; if (tokens.loggedIn.value) load() } } }
    fun load() { viewModelScope.launch { runCatching { apiCall { api.depositTarget() } }
        .onSuccess { r -> target = DepositTarget.entries.firstOrNull { it.key == r.account } ?: DepositTarget.FUNDING; unsupported = false; error = null }
        .onFailure { if (it is ApiFailure.Server && (it.status == 404 || it.status == 501)) { unsupported = true; target = DepositTarget.FUNDING; error = null } else error = it.lbMessage() } } }
    fun select(t: DepositTarget, done: () -> Unit) { if (saving) return; saving = true; viewModelScope.launch {
        runCatching { apiCall { api.updateDepositTarget(ApiDepositTarget(t.key)) } }
            .onSuccess { r -> target = DepositTarget.entries.firstOrNull { it.key == r.account } ?: t; done() }
            .onFailure { toast.show(if (it is ApiFailure.Server && (it.status == 404 || it.status == 501)) "服务端暂未开放该设置，充值仍存入资金账户" else it.lbMessage()) }
        saving = false } }
}

val LocalDepositTargetFactory = staticCompositionLocalOf<@Composable () -> DepositTargetViewModel> { { hiltViewModel() } }
val LocalAccountPreferencesFactory = staticCompositionLocalOf<@Composable () -> AccountPreferencesViewModel> { { hiltViewModel() } }

@Composable
private fun Swatch(up: Long, down: Long) = Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
    Box(Modifier.size(14.dp).background(Color(down), RoundedCornerShape(2.dp))); Box(Modifier.size(14.dp).background(Color(up), RoundedCornerShape(2.dp)))
}

@Composable
fun PreferencesContent(themeMode: ThemeMode, onTheme: (ThemeMode) -> Unit, onTrading: (Boolean) -> Unit, vm: AccountPreferencesViewModel = LocalAccountPreferencesFactory.current.invoke(), depositVm: DepositTargetViewModel = LocalDepositTargetFactory.current.invoke()) {
    val context = LocalContext.current
    val tc = TradeColors
    var sheet by remember { mutableStateOf<String?>(null) }
    Column {
        ProfileRow("语言", "简体中文") { sheet = "语言" }
        ProfileRow("计价货币", vm.preferences?.quoteCurrency ?: "USD") { sheet = "计价货币" }
        ProfileRow("主题", themeMode.title) { sheet = "主题" }
        ProfileRow("颜色偏好", DisplayPrefs.color.title) { sheet = "颜色偏好" }
        ProfileRow("主题设置", if (DisplayPrefs.classicTheme) "经典主题" else "新主题") { sheet = "主题设置" }
        Spacer(Modifier.height(12.dp))
        ProfileRow("默认充值账户", depositVm.target?.title ?: depositVm.error?.let { "读取失败" } ?: "--") { if (depositVm.error != null) depositVm.load(); sheet = "默认充值账户" }
        ProfileRow("合约交易") { onTrading(true) }
        ProfileRow("现货交易") { onTrading(false) }
    }
    sheet?.let { title -> LbSheet(title, { sheet = null }, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            when (title) {
                "语言" -> SheetOptionRow("简体中文", true) { sheet = null }
                "主题" -> ThemeMode.entries.forEach { mode -> SheetOptionRow(mode.title, mode == themeMode) { onTheme(mode); sheet = null } }
                "颜色偏好" -> UpDownColor.entries.forEach { c -> SheetOptionRow(c.title, DisplayPrefs.color == c) { DisplayPrefs.setColor(context, c); sheet = null } }
                "主题设置" -> listOf(false to "新主题", true to "经典主题").forEach { (classic, label) ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (classic) Swatch(tc.CLASSIC_UP, tc.CLASSIC_DOWN) else Swatch(tc.NEW_UP, tc.NEW_DOWN)
                        SheetOptionRow(label, DisplayPrefs.classicTheme == classic, Modifier.weight(1f).padding(start = 8.dp)) { DisplayPrefs.setClassicTheme(context, classic); sheet = null }
                    } }
                "计价货币" -> {
                    if (vm.preferences == null && vm.error == null) Text("正在读取账户偏好…", color = LB.p.textSecondary)
                    vm.preferences?.supportedQuoteCurrencies?.forEach { currency -> SheetOptionRow(currency, currency == vm.preferences?.quoteCurrency) { vm.select(currency) { sheet = null } } }
                    vm.error?.let { Text(it, color = LB.p.down, style = lb(FontSize.caption)); LbButton("重试", onClick = vm::load) }
                }
                else -> {
                    DepositTarget.entries.forEach { t -> SheetOptionRow(t.title, depositVm.target == t, Modifier.padding(vertical = 4.dp), subtitle = t.desc) { depositVm.select(t) { sheet = null } } }
                    if (depositVm.unsupported) Text("服务端暂未开放默认充值账户设置，当前充值存入资金账户。", Modifier.padding(top = 8.dp), style = lb(FontSize.caption), color = LB.p.textSecondary)
                    depositVm.error?.let { Text(it, Modifier.padding(top = 8.dp), color = LB.p.down, style = lb(FontSize.caption)) }
                }
            }
        }
    } }
}
