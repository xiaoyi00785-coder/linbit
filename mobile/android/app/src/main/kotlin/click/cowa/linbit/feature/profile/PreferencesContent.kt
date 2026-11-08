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
import androidx.compose.ui.draw.alpha
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

/** 默认充值账户：GET/PUT/DELETE /assets/deposit-account（契约 android-companion.md §8）。 */
@Serializable data class ApiDepositTarget(val account: String, val source: String? = null, val platformDefault: String? = null, val supported: List<String> = listOf("funding", "spot"))
@Serializable data class ApiDepositTargetUpdate(val account: String)

enum class DepositTarget(val key: String, val title: String, val desc: String) {
    FUNDING("funding", "资金账户", "充值后资产将自动转入资金账户"),
    FUTURES("futures", "合约账户", "充值后资产将自动转入合约账户；不支持的资产将存入资金账户"),
    SPOT("spot", "现货账户", "充值后资产将自动转入现货账户"),
}

@HiltViewModel
class DepositTargetViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore, val toast: click.cowa.linbit.ui.components.ToastCenter) : ViewModel() {
    var data by mutableStateOf<ApiDepositTarget?>(null); private set
    val target: DepositTarget? get() = data?.let { d -> DepositTarget.entries.firstOrNull { it.key == d.account } }
    var error by mutableStateOf<String?>(null); private set
    var saving by mutableStateOf(false); private set
    init { viewModelScope.launch { tokens.sessionVersion.collect { data = null; error = null; if (tokens.loggedIn.value) load() } } }
    fun load() { viewModelScope.launch { runCatching { apiCall { api.depositTarget() } }.onSuccess { data = it; error = null }.onFailure { error = it.lbMessage() } } }
    fun select(t: DepositTarget, done: () -> Unit) = write(done) { api.updateDepositTarget(ApiDepositTargetUpdate(t.key)) }
    fun followPlatform(done: () -> Unit) = write(done) { api.resetDepositTarget() }
    private fun write(done: () -> Unit, call: suspend () -> ApiDepositTarget) { if (saving) return; saving = true; viewModelScope.launch {
        runCatching { apiCall { call() } }.onSuccess { data = it; done() }.onFailure { toast.show(if ((it as? ApiFailure.Server)?.error?.code == "DEPOSIT_ACCOUNT_UNSUPPORTED") "暂不支持该入账账户" else it.lbMessage()) }; saving = false } }
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
                    val supported = depositVm.data?.supported.orEmpty()
                    DepositTarget.entries.forEach { t -> val ok = t.key in supported
                        SheetOptionRow(t.title, depositVm.target == t, Modifier.padding(vertical = 4.dp).alpha(if (ok) 1f else 0.4f), subtitle = if (ok) t.desc else "暂不支持：充值无法直接入账合约账户，请充值后划转") { if (ok) depositVm.select(t) { sheet = null } } }
                    depositVm.data?.let { d -> if (d.source == "user") SheetOptionRow("跟随平台默认（" + (DepositTarget.entries.firstOrNull { it.key == d.platformDefault }?.title ?: "资金账户") + "）", false, Modifier.padding(vertical = 4.dp), subtitle = "恢复平台默认入账账户") { depositVm.followPlatform { sheet = null } } }
                    Text("仅影响之后的链上充值入账账户，不会划转已有余额。", Modifier.padding(top = 8.dp), style = lb(FontSize.caption), color = LB.p.textSecondary)
                    depositVm.error?.let { Text(it, Modifier.padding(top = 8.dp), color = LB.p.down, style = lb(FontSize.caption)) }
                }
            }
        }
    } }
}
