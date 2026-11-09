package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiDeposit
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import kotlinx.coroutines.launch

/** 充值记录（GET /wallet/deposit/records）；只读，无资金操作。 */
@Composable
fun DepositRecordsScreen(vm: AssetsViewModel, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<ApiDeposit>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val session by vm.sessionVersion.collectAsStateWithLifecycle()
    fun reload() {
        scope.launch {
            error = null
            vm.depositRecords()
                .onSuccess { items = it }
                .onFailure { e -> error = e.lbMessage(); if (items == null) items = emptyList() }
        }
    }
    LaunchedEffect(session) { items = null; reload() }
    Column(Modifier.fillMaxSize().background(p.bg).testTag("deposit.records")) {
        AssetsNavBar("充值记录", onBack = onBack)
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            when {
                items == null && error == null -> Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                    CircularProgressIndicator(Modifier.padding(40.dp), color = p.brand, strokeWidth = 2.dp)
                }
                error != null && items.isNullOrEmpty() -> EmptyState(error ?: "加载失败") {
                    LbButton("重试", Modifier.padding(top = 8.dp).height(36.dp)) { reload() }
                }
                items.isNullOrEmpty() -> EmptyState("暂无充值记录")
                else -> items!!.forEach { d ->
                    AssetsCard(Modifier.padding(bottom = 10.dp)) {
                        Text("${d.amount} ${d.asset}", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                        Spacer(Modifier.height(8.dp))
                        AssetKv("状态", d.status.value)
                        AssetKv("网络", AssetsLogic.displayNetwork(d.network))
                        AssetKv("时间", AssetsLogic.displayWithdrawTime(d.createdAt))
                    }
                }
            }
        }
    }
}
