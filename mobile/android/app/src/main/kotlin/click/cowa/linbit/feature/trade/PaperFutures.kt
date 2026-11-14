package click.cowa.linbit.feature.trade

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.AmountBody
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.PaperOrderBody
import click.cowa.linbit.core.network.PriceAlertBody
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.KvRow
import click.cowa.linbit.ui.components.NumberBox
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import java.util.UUID
import javax.inject.Inject

@HiltViewModel
class PaperFuturesViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    val toast: ToastCenter,
) : ViewModel() {
    val loggedIn get() = tokens.loggedIn
    var balance by mutableStateOf("--"); private set
    var available by mutableStateOf("--"); private set
    var upnl by mutableStateOf("--"); private set
    var qty by mutableStateOf("0.001")
    var alertPrice by mutableStateOf("")

    fun load() {
        if (!tokens.loggedIn.value) return
        viewModelScope.launch {
            try {
                val el = apiCall { api.paperFuturesAccount() }
                val o = el as? JsonObject
                balance = o?.get("balance")?.jsonPrimitive?.contentOrNull ?: "--"
                available = o?.get("available")?.jsonPrimitive?.contentOrNull ?: "--"
                upnl = o?.get("unrealizedPnl")?.jsonPrimitive?.contentOrNull ?: "--"
            } catch (e: Throwable) { toast.error(e) }
        }
    }

    fun claim() {
        viewModelScope.launch {
            try {
                apiCall { api.paperFuturesClaim(AmountBody("1000")) }
                toast.show("已领取 1000 USDX", ToastCenter.Kind.SUCCESS)
                load()
            } catch (e: Throwable) { toast.error(e) }
        }
    }

    fun order(side: String) {
        viewModelScope.launch {
            try {
                apiCall { api.paperFuturesOrder(PaperOrderBody(symbol = "BTCUSDX", side = side, quantity = qty)) }
                toast.show("模拟单已提交", ToastCenter.Kind.SUCCESS)
                load()
            } catch (e: Throwable) { toast.error(e) }
        }
    }

    fun addAlert(symbol: String) {
        if (alertPrice.isBlank()) { toast.show("请填写价格"); return }
        viewModelScope.launch {
            try {
                apiCall {
                    api.createPriceAlert(PriceAlertBody(
                        clientAlertId = UUID.randomUUID().toString(),
                        market = "futures",
                        symbol = symbol,
                        condition = "above",
                        threshold = alertPrice,
                    ))
                }
                toast.show("价格预警已创建", ToastCenter.Kind.SUCCESS)
            } catch (e: Throwable) { toast.error(e) }
        }
    }
}

@Composable
fun PaperFuturesPane(bottomInset: Dp, onLogin: () -> Unit, vm: PaperFuturesViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    LaunchedEffect(loggedIn) { if (loggedIn) vm.load() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Spacing.pageH).padding(bottom = bottomInset), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("模拟交易", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Text("使用模拟资金 USDX，不影响真实余额。", style = lb(FontSize.small), color = p.textSecondary)
        if (!loggedIn) {
            ChipButton("登录后开始", Modifier.fillMaxWidth(), onClick = onLogin)
        } else {
            KvRow("余额", vm.balance)
            KvRow("可用", vm.available)
            KvRow("未实现盈亏", vm.upnl)
            ChipButton("领取 1000 USDX", Modifier.fillMaxWidth()) { vm.claim() }
            NumberBox("数量", "BTC", vm.qty, { vm.qty = it })
            ChipButton("市价开多 BTCUSDX", Modifier.fillMaxWidth()) { vm.order("buy") }
            ChipButton("市价开空 BTCUSDX", Modifier.fillMaxWidth()) { vm.order("sell") }
        }
    }
}

@Composable
fun PriceAlertForm(symbol: String, onClose: () -> Unit, vm: PaperFuturesViewModel = hiltViewModel()) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("$symbol 价格上穿后通知", style = lb(FontSize.body), color = p.textPrimary)
        NumberBox("价格", "USDT", vm.alertPrice, { vm.alertPrice = it })
        ChipButton("创建预警", Modifier.fillMaxWidth()) { vm.addAlert(symbol); onClose() }
    }
}
