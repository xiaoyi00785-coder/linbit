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

val LocalAccountPreferencesFactory = staticCompositionLocalOf<@Composable () -> AccountPreferencesViewModel> { { hiltViewModel() } }

@Composable
fun PreferencesContent(themeMode: ThemeMode, onTheme: (ThemeMode) -> Unit, onTrading: (Boolean) -> Unit, vm: AccountPreferencesViewModel = LocalAccountPreferencesFactory.current.invoke()) {
    val context = LocalContext.current
    var sheet by remember { mutableStateOf<String?>(null) }
    Column {
        ProfileRow("语言", "简体中文") { sheet = "语言" }
        ProfileRow("计价货币", vm.preferences?.quoteCurrency ?: "USD") { sheet = "计价货币" }
        ProfileRow("主题", themeMode.title) { sheet = "主题" }
        ProfileRow("涨跌颜色", if (DisplayPrefs.redUp) "红涨绿跌" else "绿涨红跌") { sheet = "涨跌颜色" }
        Spacer(Modifier.height(12.dp))
        ProfileRow("默认充值账户", "资金账户") { sheet = "默认充值账户" }
        ProfileRow("合约交易") { onTrading(true) }
        ProfileRow("现货交易") { onTrading(false) }
    }
    sheet?.let { title -> LbSheet(title, { sheet = null }, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            when (title) {
                "语言" -> SheetOptionRow("简体中文", true) { sheet = null }
                "主题" -> ThemeMode.entries.forEach { mode -> SheetOptionRow(mode.title, mode == themeMode) { onTheme(mode); sheet = null } }
                "涨跌颜色" -> listOf(false to "绿涨红跌", true to "红涨绿跌").forEach { (redUp, label) -> SheetOptionRow(label, DisplayPrefs.redUp == redUp) { DisplayPrefs.setRedUp(context, redUp); sheet = null } }
                "计价货币" -> {
                    if (vm.preferences == null && vm.error == null) Text("正在读取账户偏好…", color = LB.p.textSecondary)
                    vm.preferences?.supportedQuoteCurrencies?.forEach { currency -> SheetOptionRow(currency, currency == vm.preferences?.quoteCurrency) { vm.select(currency) { sheet = null } } }
                    vm.error?.let { Text(it, color = LB.p.down, style = lb(FontSize.caption)); LbButton("重试", onClick = vm::load) }
                }
                else -> Text("充值进入资金账户，可在划转页面转至现货或合约账户。服务端未提供默认充值账户设置接口。", style = lb(FontSize.body), color = LB.p.textSecondary)
            }
        }
    } }
}
