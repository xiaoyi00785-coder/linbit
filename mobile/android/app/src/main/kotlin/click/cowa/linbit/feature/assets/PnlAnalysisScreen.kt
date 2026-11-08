package click.cowa.linbit.feature.assets

import androidx.compose.foundation.layout.*
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiFuturesTodayPnl200Response
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class PnlAnalysisViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore) : ViewModel() {
    var data by mutableStateOf<ApiFuturesTodayPnl200Response?>(null); private set
    var error by mutableStateOf<String?>(null); private set
    var loading by mutableStateOf(false); private set
    init { viewModelScope.launch { tokens.sessionVersion.collect { data = null; error = null; load() } } }
    fun load() {
        val session = tokens.sessionVersion.value
        if (!tokens.loggedIn.value) { data = null; error = "请登录后查看盈亏"; loading = false; return }
        loading = true; error = null
        viewModelScope.launch {
            val response = runCatching { apiCall { api.futuresTodayPnl() } }
            if (session != tokens.sessionVersion.value) return@launch
            response.onSuccess { data = it }.onFailure { error = it.lbMessage() }
            loading = false
        }
    }
}

@Composable
fun PnlAnalysisScreen(onBack: () -> Unit, vm: PnlAnalysisViewModel = hiltViewModel()) {
    Column(Modifier.fillMaxSize()) {
        AssetsNavBar("盈亏分析", onBack)
        Column(Modifier.fillMaxWidth().padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            Text("合约 · 今日", style = lb(FontSize.title), color = LB.p.textPrimary)
            Text("按交易服务端 UTC 00:00 起统计", style = lb(FontSize.caption), color = LB.p.textSecondary)
            when {
                vm.loading && vm.data == null -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                vm.error != null -> EmptyState(vm.error!!) { ChipButton("重试", onClick = vm::load) }
                vm.data != null -> {
                    val data = vm.data!!
                    listOf("总盈亏" to data.pnl, "已实现盈亏" to data.realizedPnl, "资金费用" to data.funding, "手续费" to data.fee, "强平费用" to data.liquidationFee).forEach { (label, value) ->
                        val amount = Dec.parse(value)
                        KvRow(label, (if (amount.signum() > 0) "+" else "") + Fmt.amount(amount, 4) + " USDT")
                    }
                    LbButton("刷新", kind = ButtonKind.SECONDARY, onClick = vm::load)
                }
            }
            Text("服务端当前仅提供今日合约汇总；未提供按日期查询的盈亏曲线及现货盈亏分析。", style = lb(FontSize.caption), color = LB.p.textSecondary)
        }
    }
}

/** No fiat endpoint exists in the current contract. Do not fabricate a quote or redirect a purchase to a deposit. */
@Composable
fun QuickBuyScreen(onBack: () -> Unit) {
    Column(Modifier.fillMaxSize()) {
        AssetsNavBar("快捷买币", onBack)
        EmptyState("暂无可用的支付渠道", Modifier.padding(top = 80.dp)) {
            Text("服务端未配置法币报价、支付渠道和买币订单接口。", Modifier.padding(horizontal = Spacing.pageH), style = lb(FontSize.caption), color = LB.p.textSecondary)
        }
    }
}
