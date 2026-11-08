package click.cowa.linbit.feature.trade

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.border
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.Alignment
import click.cowa.linbit.R
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

/** Entry-time TP/SL edits are only a local draft; submitted with the existing reviewed order. */
@Composable
fun EntryTpslSheet(vm: FuturesTradeViewModel, onClose: () -> Unit) {
    var tp by remember { mutableStateOf(vm.tpText) }
    var sl by remember { mutableStateOf(vm.slText) }
    var tpMark by remember { mutableStateOf(vm.tpMark) }
    var slMark by remember { mutableStateOf(vm.slMark) }
    var error by remember { mutableStateOf<String?>(null) }
    LbSheet("高级止盈止损", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            KvRow("交易对", vm.symbol)
            KvRow("参考委托价", Fmt.priceAuto(vm.orderPrice))
            Text("止盈", style = lb(FontSize.label), color = LB.p.textPrimary)
            PillSegment(listOf("标记价格", "最新价格"), if (tpMark) 0 else 1, { tpMark = it == 0 })
            NumberBox("止盈触发价", vm.quote, tp, { tp = it; error = null })
            Text("止损", style = lb(FontSize.label), color = LB.p.textPrimary)
            PillSegment(listOf("标记价格", "最新价格"), if (slMark) 0 else 1, { slMark = it == 0 })
            NumberBox("止损触发价", vm.quote, sl, { sl = it; error = null })
            Text("开仓成交后生效，触发后市价平仓。这里只保存表单，不会提交订单。", style = lb(FontSize.caption), color = LB.p.textSecondary)
            error?.let { Text(it, color = LB.p.down, style = lb(FontSize.caption)) }
            LbButton("确认") {
                if (listOf(tp, sl).any { it.isNotBlank() && Dec.parse(it).signum() <= 0 }) error = "触发价必须大于零"
                else { vm.tpText = tp.trim(); vm.slText = sl.trim(); vm.tpMark = tpMark; vm.slMark = slMark; onClose() }
            }
        }
    }
}

@Composable
fun TradePreferencesSheet(mode: ThemeMode, onTheme: (ThemeMode) -> Unit, skipConfirm: Boolean, onSkip: (Boolean) -> Unit, onClose: () -> Unit) {
    LbSheet("交易偏好", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Text("主题", style = lb(FontSize.label), color = LB.p.textPrimary)
            ThemeMode.entries.forEach { item -> SheetOptionRow(item.title, item == mode) { onTheme(item) } }
            CheckRow(!skipConfirm, "下单前二次确认") { onSkip(!it) }
            Text("仅影响当前交易页面的确认方式；不改变账户安全设置。", style = lb(FontSize.caption), color = LB.p.textSecondary)
        }
    }
}

@Composable
fun TradeInfoSheet(title: String, vm: FuturesTradeViewModel, onClose: () -> Unit) {
    LbSheet(title, onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            when (title) {
                "仓位档位介绍" -> {
                    Text("持仓名义价值越大，可用最大杠杆越低。", style = lb(FontSize.caption), color = LB.p.textSecondary)
                    if (vm.tiers.isEmpty()) EmptyState("暂无仓位档位数据")
                    vm.tiers.forEach { tier ->
                        KvRow("档位 ${tier.tier ?: "--"}", "${tier.minNotional ?: "--"} – ${tier.maxNotional ?: "--"} USDT")
                        KvRow("最大杠杆", "${tier.maxLeverage ?: "--"}x")
                        KvRow("维持保证金率", pct(Dec.parse(tier.maintenanceMarginRate)))
                    }
                }
                "历史资金费率" -> { KvRow("当前资金费率", Fmt.amount(vm.fundingRate, 4) + "%"); EmptyState("服务端未提供资金费率历史接口") }
                "历史标记价格" -> { KvRow("当前标记价格", Fmt.priceAuto(vm.mark)); EmptyState("服务端未提供标记价格历史接口") }
                "风险准备金" -> EmptyState("服务端未提供风险准备金接口")
                else -> {
                    Text("合约交易入门", style = lb(FontSize.title), color = LB.p.textPrimary)
                    listOf("选择交易对、保证金模式和杠杆。杠杆会放大收益，也会放大亏损。", "限价单按指定价格或更优价格成交；市价单按当前可成交价格执行，可能产生滑点。", "开多表示预期价格上涨，开空表示预期价格下跌。", "下单前核对价格、数量及保证金，可在仓位中设置止盈止损。", "保证金不足时仓位可能被强制平仓。请充分了解风险。").forEach { Text(it, style = lb(FontSize.body), color = LB.p.textSecondary) }
                }
            }
            LbButton("知道了", onClick = onClose)
        }
    }
}

@Composable
fun SpotToolsSheet(onClose: () -> Unit, onTool: (String) -> Unit) {
    LbSheet("常用功能", onClose) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            val items = listOf("交易偏好" to R.drawable.ic_ref_settings, "充值" to R.drawable.ic_ref_deposit, "划转" to R.drawable.ic_ref_transfer, "价格预警" to R.drawable.ic_ref_price_alert, "添加自选" to R.drawable.ic_ref_star, "新手教学" to R.drawable.ic_ref_guide)
            items.chunked(4).forEach { row ->
                Row(Modifier.fillMaxWidth().padding(bottom = 20.dp)) {
                    row.forEach { (title, icon) ->
                        Column(Modifier.weight(1f).pressable(scale = 1f) { onClose(); onTool(title) }, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            Box(Modifier.size(44.dp).border(1.dp, LB.p.border, RoundedCornerShape(Radius.md)), contentAlignment = Alignment.Center) { if (title in listOf("充值", "划转")) LbReferenceIcon(icon, 22.dp) else LbIcon(icon, 22.dp, LB.p.textPrimary) }
                            Text(title, style = lb(FontSize.small), color = LB.p.textPrimary)
                        }
                    }
                    repeat(4 - row.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }
    }
}

@Composable
fun SpotGuideSheet(onClose: () -> Unit) {
    LbSheet("新手教学", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            listOf("将资金划转至现货账户，再选择交易对。", "限价委托按指定价格或更优价格成交；市价委托按当前盘口成交。", "填写数量或交易额，核对后确认买入或卖出。", "未成交的委托可在委托列表撤销；已成交订单可在历史记录中查看。").forEach { Text(it, style = lb(FontSize.body), color = LB.p.textSecondary) }
            LbButton("知道了", onClick = onClose)
        }
    }
}
