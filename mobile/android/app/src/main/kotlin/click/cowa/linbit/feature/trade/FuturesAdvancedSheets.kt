package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
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
import click.cowa.linbit.core.generated.ApiFuturesOrder
import click.cowa.linbit.core.generated.ApiFuturesPosition
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.KvRow
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.NumberBox
import click.cowa.linbit.ui.components.PillSegment
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal

/** 改单：PUT /futures/orders/{id}，改价 / 改量（至少一项）。 */
@Composable
fun AmendOrderSheet(vm: FuturesTradeViewModel, order: ApiFuturesOrder, onClose: () -> Unit) {
    val p = LB.p
    var price by remember { mutableStateOf(order.price ?: "") }
    var qty by remember { mutableStateOf(order.qty ?: "") }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val buy = order.side == "buy"
    LbSheet("改单", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("${order.symbol ?: vm.symbol} · ${if (order.type == "market") "市价" else "限价"}", style = lb(FontSize.small), color = p.textSecondary)
            Row {
                KVCell("原委托价", order.price ?: "市价", Modifier.weight(1f))
                KVCell("原数量", order.qty ?: "--", Modifier.weight(1f), TextAlign.Center)
                KVCell("已成交", order.filledQty ?: "0", Modifier.weight(1f), TextAlign.End)
            }
            NumberBox("新价格", vm.quote, price, { price = it; error = null })
            NumberBox("新数量（总量）", vm.base, qty, { qty = it; error = null })
            KvRow("已改单次数", "${order.amendCount ?: 0} 次")
            if (order.timeInForce == "POST_ONLY" || order.timeInForce == "GTX") {
                Text("只做 Maker 委托：若新价格会立即成交，改单将被拒绝", style = lb(FontSize.small), color = p.textTertiary)
            }
            error?.let { Text(it, Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(10.dp), style = lb(FontSize.small), color = p.down) }
            LbButton("确认修改", kind = if (buy) ButtonKind.BUY else ButtonKind.SELL, loading = saving) {
                val np = Dec.parse(price); val nq = Dec.parse(qty); val op = Dec.parse(order.price); val oq = Dec.parse(order.qty)
                val filled = Dec.parse(order.filledQty)
                when {
                    price.isBlank() && qty.isBlank() -> error = "请至少修改价格或数量"
                    price.isNotBlank() && np.signum() <= 0 -> error = "请输入有效价格"
                    qty.isNotBlank() && nq.signum() <= 0 -> error = "请输入有效数量"
                    qty.isNotBlank() && nq < filled -> error = "新数量不能小于已成交数量"
                    np.compareTo(op) == 0 && nq.compareTo(oq) == 0 -> error = "价格与数量均未变化"
                    else -> {
                        saving = true
                        vm.amend(order, if (price.isNotBlank() && np.compareTo(op) != 0) vm.fmt(np, vm.pricePrec) else null,
                            if (qty.isNotBlank() && nq.compareTo(oq) != 0) vm.fmt(nq, vm.qtyPrec) else null) { ok, msg ->
                            saving = false
                            if (ok) onClose() else error = msg
                        }
                    }
                }
            }
        }
    }
}

/** 仓位止盈止损：整仓走 /positions/{symbol}/tpsl，部分走 /futures/tpsl-orders。 */
@Composable
fun PositionTpslSheet(vm: FuturesTradeViewModel, position: ApiFuturesPosition, onClose: () -> Unit) {
    val p = LB.p
    val long = position.side != ApiFuturesPosition.Side.SHORT
    val entry = Dec.parse(position.entryPrice)
    val maxQty = Dec.parse(position.qty).abs()
    var tp by remember { mutableStateOf(position.tpPrice.orEmpty()) }
    var sl by remember { mutableStateOf(position.slPrice.orEmpty()) }
    var partial by remember { mutableStateOf(false) }
    var partialQty by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    val hasExisting = !position.tpPrice.isNullOrBlank() || !position.slPrice.isNullOrBlank()
    LbSheet("止盈止损", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(position.symbol ?: vm.symbol, style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                Text(if (long) "多" else "空", Modifier.clip(RoundedCornerShape(3.dp)).background(if (long) p.upTint else p.downTint).padding(horizontal = 4.dp), style = lb(10.sp), color = if (long) p.up else p.down)
            }
            KvRow("开仓价格", "${Fmt.priceAuto(entry)} ${vm.quote}")
            KvRow("持仓数量", "${Fmt.amount(maxQty, vm.qtyPrec)} ${vm.base}")
            KvRow("标记价格", "${Fmt.priceAuto(vm.markPriceFor(position.symbol, position.markPrice))} ${vm.quote}")
            NumberBox("止盈触发价", vm.quote, tp, { tp = it })
            NumberBox("止损触发价", vm.quote, sl, { sl = it })
            Row(Modifier.pressable(scale = 1f) { partial = !partial }, verticalAlignment = Alignment.CenterVertically) {
                Text(if (partial) "部分止盈止损" else "全部止盈止损", style = lb(FontSize.small, FontWeight.Medium), color = p.textPrimary)
                Text("  ⇄", style = lb(FontSize.small), color = p.textSecondary)
            }
            if (partial) {
                NumberBox("平仓数量", vm.base, partialQty, { partialQty = it })
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    listOf(25, 50, 75, 100).forEach { pct ->
                        Box(
                            Modifier.weight(1f).height(26.dp).clip(RoundedCornerShape(Radius.xs)).background(p.fill)
                                .pressable(scale = 1f) { partialQty = vm.fmt(maxQty.multiply(BigDecimal(pct)).divide(BigDecimal(100), vm.qtyPrec, java.math.RoundingMode.DOWN), vm.qtyPrec) },
                            contentAlignment = Alignment.Center,
                        ) { Text("$pct%", style = lb(FontSize.small), color = p.textPrimary) }
                    }
                }
                Text("可平 ${Fmt.amount(maxQty, vm.qtyPrec)} ${vm.base}，触发后以市价平掉所填数量", style = lb(FontSize.small), color = p.textTertiary)
            } else {
                Text("触发后将以市价平掉该仓位全部数量，行情剧烈波动时成交价可能偏离触发价", style = lb(FontSize.small), color = p.textTertiary)
            }
            if (hasExisting && !partial) {
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    LbButton("撤销止盈止损", Modifier.weight(1f), kind = ButtonKind.SECONDARY, loading = saving) {
                        saving = true
                        vm.clearTpsl(position) { ok -> saving = false; if (ok) onClose() }
                    }
                    LbButton("修改", Modifier.weight(1f), loading = saving) {
                        if (!validateTpsl(long, entry, tp, sl, vm)) return@LbButton
                        saving = true
                        vm.setTpsl(position, tp, sl) { ok -> saving = false; if (ok) onClose() }
                    }
                }
            } else {
                LbButton(if (partial) "提交" else "确定", loading = saving) {
                    if (!validateTpsl(long, entry, tp, sl, vm)) return@LbButton
                    if (tp.isBlank() && sl.isBlank()) { vm.toast.show("请至少填写止盈或止损触发价"); return@LbButton }
                    saving = true
                    if (partial) {
                        val q = Dec.parse(partialQty)
                        if (q.signum() <= 0) { vm.toast.show("请输入平仓数量"); saving = false; return@LbButton }
                        if (q > maxQty) { vm.toast.show("平仓数量不能超过持仓数量"); saving = false; return@LbButton }
                        vm.createTpslOrder(position, vm.fmt(q, vm.qtyPrec), tp.ifBlank { null }, sl.ifBlank { null }) { ok -> saving = false; if (ok) onClose() }
                    } else {
                        vm.setTpsl(position, tp, sl) { ok -> saving = false; if (ok) onClose() }
                    }
                }
            }
        }
    }
}

private fun validateTpsl(long: Boolean, entry: BigDecimal, tp: String, sl: String, vm: FuturesTradeViewModel): Boolean {
    val t = Dec.parse(tp); val s = Dec.parse(sl)
    if (tp.isNotBlank() && (if (long) t <= entry else t >= entry)) {
        vm.toast.show("止盈价需${if (long) "高于" else "低于"}开仓价"); return false
    }
    if (sl.isNotBlank() && (if (long) s >= entry else s <= entry)) {
        vm.toast.show("止损价需${if (long) "低于" else "高于"}开仓价"); return false
    }
    return true
}

/** 逐仓调整保证金：正数追加、负数减少，POST /futures/positions/{symbol}/margin。 */
@Composable
fun AdjustMarginSheet(vm: FuturesTradeViewModel, position: ApiFuturesPosition, onClose: () -> Unit) {
    val p = LB.p
    val isolated = position.marginMode == ApiFuturesPosition.MarginMode.ISOLATED
    var add by remember { mutableStateOf(0) }
    var amount by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    LbSheet("调整保证金", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("${position.symbol ?: ""} · ${if (isolated) "逐仓" else "全仓"} ${position.leverage ?: 0}x", style = lb(FontSize.small), color = p.textSecondary)
            if (!isolated) {
                Text("全仓仓位共享账户余额作为保证金，无需单独调整。切换为逐仓后可调整。",
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(10.dp),
                    style = lb(FontSize.small), color = p.textSecondary)
                LbButton("知道了") { onClose() }
            } else {
                PillSegment(listOf("增加保证金", "减少保证金"), add, { add = it })
                NumberBox("数量", "USDT", amount, { amount = it })
                KvRow(if (add == 0) "可增加" else "可减少", "${Fmt.amount(if (add == 0) vm.available else Dec.parse(position.margin), 2)} USDT")
                KvRow("当前保证金", "${Fmt.amount(Dec.parse(position.margin), 2)} USDT")
                val liq = Dec.parse(position.liquidationPrice)
                KvRow("当前强平价", if (liq.signum() > 0) Fmt.priceAuto(liq) else "--")
                LbButton("确认", enabled = Dec.parse(amount).signum() > 0, loading = saving) {
                    val v = Dec.parse(amount)
                    saving = true
                    vm.adjustMargin(position, if (add == 0) v else v.negate()) { ok -> saving = false; if (ok) onClose() }
                }
            }
        }
    }
}

/** 本地计算器（开仓成本 / 强平价 / 目标价），不调用资金接口。 */
@Composable
fun CalculatorSheet(vm: FuturesTradeViewModel, onClose: () -> Unit) {
    val p = LB.p
    var tab by remember { mutableStateOf(0) }
    var long by remember { mutableStateOf(true) }
    var entry by remember { mutableStateOf(if (vm.last.signum() > 0) vm.fmt(vm.last, vm.pricePrec) else "") }
    var qty by remember { mutableStateOf("") }
    var lev by remember { mutableStateOf(vm.leverage.toString()) }
    var exit by remember { mutableStateOf("") }
    var targetPnl by remember { mutableStateOf("") }
    val e = Dec.parse(entry); val q = Dec.parse(qty); val l = lev.toIntOrNull() ?: vm.leverage
    val cost = TradeCalculator.cost(q, e, l)
    val liq = TradeCalculator.liqPrice(e, l, long, vm.mmr)
    val pnl = if (exit.isNotBlank()) TradeCalculator.pnl(e, Dec.parse(exit), q, long) else null
    val target = if (targetPnl.isNotBlank()) TradeCalculator.targetPrice(e, q, Dec.parse(targetPnl), long) else null
    LbSheet("计算器", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            PillSegment(listOf("开仓", "盈亏", "目标价"), tab, { tab = it })
            PillSegment(listOf("做多", "做空"), if (long) 0 else 1, { long = it == 0 }, height = 32.dp)
            NumberBox("开仓价格", vm.quote, entry, { entry = it })
            NumberBox("数量", vm.base, qty, { qty = it })
            NumberBox("杠杆", "x", lev, { lev = it.filter { c -> c.isDigit() } })
            when (tab) {
                0 -> {
                    KvRow("保证金（成本）", "${Fmt.amount(cost, 4)} ${vm.quote}")
                    KvRow("预估强平价", if (liq.signum() > 0) Fmt.price(liq, vm.pricePrec) else "--")
                }
                1 -> {
                    NumberBox("平仓价格", vm.quote, exit, { exit = it })
                    KvRow("预估盈亏", pnl?.let { (if (it.signum() > 0) "+" else "") + Fmt.amount(it, 4) } ?: "--", trailing = null)
                }
                else -> {
                    NumberBox("目标盈亏", vm.quote, targetPnl, { targetPnl = it })
                    KvRow("目标价格", target?.let { Fmt.price(it, vm.pricePrec) } ?: "--")
                }
            }
            Text("计算结果仅供参考，实际成交与强平以盘口与风控结果为准", style = lb(FontSize.small), color = p.textTertiary)
            LbButton("关闭", kind = ButtonKind.SECONDARY) { onClose() }
        }
    }
}
