package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.PercentSlider
import click.cowa.linbit.ui.components.PillSegment
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal

/** 盘口显示模式（对照图 ref/12：默认 / 买 / 卖，选中项右侧黑色对勾）。 */
@Composable
fun BookModeSheet(mode: Int, onPick: (Int) -> Unit, onClose: () -> Unit) {
    val p = LB.p
    LbSheet(null, onClose) {
        Column(Modifier.padding(bottom = 20.dp)) {
            listOf("默认", "买", "卖").forEachIndexed { i, t ->
                Row(
                    Modifier.fillMaxWidth().height(56.dp).pressable(scale = 1f) { onPick(i); onClose() }.padding(horizontal = Spacing.pageH),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    BookModeGlyph(i)
                    Text(t, Modifier.weight(1f).padding(start = 12.dp), style = lb(FontSize.body), color = p.textPrimary)
                    if (mode == i) LbIcon(R.drawable.ic_check, 20.dp, p.textPrimary)
                }
            }
        }
    }
}

@Composable
private fun BookModeGlyph(mode: Int) {
    val p = LB.p
    Column(Modifier.size(width = 14.dp, height = 12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        repeat(2) { i ->
            val c = when (mode) { 0 -> if (i == 0) p.down else p.up; 1 -> p.up; else -> p.down }
            Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(2.dp), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(5.dp).clip(RoundedCornerShape(1.dp)).background(c))
                Box(Modifier.weight(1f).height(1.5.dp).background(p.textTertiary))
            }
        }
    }
}

/** 常用功能（对照图 ref/13：4 列 × 3 行图标网格）。 */
@Composable
fun ToolsSheet(toast: (String) -> Unit, onClose: () -> Unit, onTool: (String) -> Unit = {}) {
    val p = LB.p
    val items = listOf(
        "交易偏好", "计算器", "价格预警", "模拟交易", "仓位档位介绍", "历史资金费率", "历史标记价格", "风险准备金", "添加自选", "新手教学",
    )
    LbSheet("常用功能", onClose) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 4.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
            items.chunked(4).forEach { row ->
                Row(Modifier.fillMaxWidth()) {
                    row.forEach { t ->
                        Column(Modifier.weight(1f).pressable(scale = 1f) {
                            onClose()
                            when (t) {
                                "添加自选" -> toast("已添加到自选")
                                "计算器", "价格预警", "模拟交易" -> onTool(t)
                                else -> toast("${t}即将开放")
                            }
                        }, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            Box(Modifier.size(44.dp).border(1.dp, p.border, RoundedCornerShape(Radius.lg)), contentAlignment = Alignment.Center) {
                                LbIcon(R.drawable.ic_doc_text, 20.dp, p.textPrimary)
                            }
                            Text(t, style = lb(FontSize.small), color = p.textPrimary, maxLines = 1, textAlign = TextAlign.Center)
                        }
                    }
                    repeat(4 - row.size) { Box(Modifier.weight(1f)) }
                }
            }
        }
    }
}

/** 调整交易模式（对照图 ref/17）：保证金模式 全仓/逐仓 + 仓位模式 合仓/分仓。 */
@Composable
fun MarginModeSheet(vm: FuturesTradeViewModel, onClose: () -> Unit) {
    val p = LB.p
    var mode by remember { mutableStateOf(if (vm.cross) 0 else 1) }
    var posMode by remember { mutableStateOf(if (vm.hedge) 1 else 0) }
    var saving by remember { mutableStateOf(false) }
    val modeChanged = (posMode == 1) != vm.hedge
    val block = if (modeChanged) vm.positionModeBlockReason else null
    LbSheet("调整交易模式", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("保证金模式", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            PillSegment(listOf("全仓", "逐仓"), mode, { mode = it }, height = 40.dp, activeColors = listOf(p.bg, p.bg), activeText = p.textPrimary)
            Text(
                if (mode == 0) "全仓模式：所有全仓仓位共享账户可用余额作为保证金，盈亏可相互抵消。" else "逐仓模式：每个仓位独立分配保证金，最大亏损为该仓位保证金。",
                style = lb(FontSize.small), color = p.textSecondary,
            )
            Text("仓位模式", Modifier.padding(top = 4.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            PillSegment(listOf("合仓", "分仓"), posMode, { posMode = it }, height = 40.dp, activeColors = listOf(p.bg, p.bg), activeText = p.textPrimary)
            Text(
                if (posMode == 0) "合仓模式：同一合约（开多与开空）仅持有一个仓位，反向开仓会先减少已有仓位。"
                else "分仓模式：同一合约可同时持有多个独立仓位，每笔开仓单独计算保证金与盈亏，可分别止盈止损、平仓。",
                style = lb(FontSize.small), color = p.textSecondary,
            )
            block?.let { Text(it, Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(10.dp), style = lb(FontSize.small), color = p.down) }
            LbButton("确认", Modifier.padding(top = 6.dp), enabled = block == null, loading = saving) {
                saving = true
                fun finish(ok: Boolean) { saving = false; if (ok) onClose() }
                fun margin() {
                    if ((mode == 0) != vm.cross) vm.saveLeverage(vm.leverage, mode == 0) { finish(it) } else finish(true)
                }
                if (modeChanged) vm.setPositionMode(posMode == 1) { ok -> if (ok) margin() else finish(false) } else margin()
            }
        }
    }
}

/** 调整杠杆（对照图 ref/18）：标题行 + 「− 100x +」+ 刻度滑杆 + 说明 + 保存。 */
@Composable
fun LeverageSheet(vm: FuturesTradeViewModel, onClose: () -> Unit) {
    val p = LB.p
    val maxL = maxOf(vm.maxLeverage, 1)
    var lev by remember { mutableStateOf(vm.leverage.coerceIn(1, maxL)) }
    var saving by remember { mutableStateOf(false) }
    val ticks = listOf(1) + (1..4).map { maxL * it / 5 } + listOf(maxL)
    LbSheet("调整杠杆", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(vm.symbol, style = lb(FontSize.label, FontWeight.SemiBold), color = p.textPrimary)
                Text("多", Modifier.clip(RoundedCornerShape(3.dp)).background(p.upTint).padding(horizontal = 4.dp), style = lb(10.sp), color = p.up)
                Text("${lev}x", Modifier.clip(RoundedCornerShape(3.dp)).background(p.upTint).padding(horizontal = 4.dp), style = lb(10.sp), color = p.up)
                Text(if (vm.cross) "全仓" else "逐仓", Modifier.clip(RoundedCornerShape(3.dp)).background(p.fill).padding(horizontal = 4.dp), style = lb(10.sp), color = p.textSecondary)
            }
            Row(Modifier.fillMaxWidth().height(44.dp).clip(RoundedCornerShape(Radius.md)).background(p.fill), verticalAlignment = Alignment.CenterVertically) {
                Text("−", Modifier.size(44.dp).pressable(scale = 1f) { lev = maxOf(1, lev - 1) }, style = lb(FontSize.title), color = p.textPrimary, textAlign = TextAlign.Center)
                Text("${lev}x", Modifier.weight(1f), style = lbNum(FontSize.title, FontWeight.SemiBold), color = p.textPrimary, textAlign = TextAlign.Center)
                Text("+", Modifier.size(44.dp).pressable(scale = 1f) { lev = minOf(maxL, lev + 1) }, style = lb(FontSize.title), color = p.textPrimary, textAlign = TextAlign.Center)
            }
            PercentSlider(
                if (maxL > 1) (lev - 1f) / (maxL - 1f) else 0f,
                { f -> lev = (1 + f * (maxL - 1)).let { Math.round(it) }.coerceIn(1, maxL) }, tint = p.textPrimary,
            )
            Row(Modifier.fillMaxWidth()) {
                ticks.forEachIndexed { i, t ->
                    Text("${t}x", Modifier.pressable(scale = 1f) { lev = t }, style = lbNum(FontSize.small), color = p.textSecondary)
                    if (i < ticks.size - 1) Box(Modifier.weight(1f))
                }
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("当前杠杆倍数最大可开 ${Fmt.amount(vm.maxNotional(lev) ?: BigDecimal.ZERO, 0)} ${vm.quote}", style = lb(FontSize.small), color = p.textSecondary)
                Text("调整杠杆对当前合约的全部仓位与委托生效", style = lb(FontSize.small), color = p.textSecondary)
                if (lev >= 50) Text("选择超过 50x 的杠杆交易会增加强行平仓的风险，请注意相关风险", style = lb(FontSize.small), color = p.down)
            }
            LbButton("保存", loading = saving) {
                saving = true
                vm.saveLeverage(lev, vm.cross) { ok -> saving = false; if (ok) onClose() }
            }
        }
    }
}

/** 二次确认弹窗（撤单 / 平仓等破坏性操作）。 */
@Composable
fun ConfirmDialog(title: String, message: String, confirm: String, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    val p = LB.p
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = p.bgElevated,
        title = { Text(title, style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary) },
        text = { Text(message, style = lb(FontSize.label), color = p.textSecondary) },
        confirmButton = { TextButton(onConfirm) { Text(confirm, style = lb(FontSize.label, FontWeight.Medium), color = p.down) } },
        dismissButton = { TextButton(onDismiss) { Text("取消", style = lb(FontSize.label), color = p.textPrimary) } },
    )
}
