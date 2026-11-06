package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind

/** 划转：现货(资金) ↔ 合约，币种固定 USDT（对照图 #46 / iOS TransferView）。 */
@Composable
fun TransferScreen(vm: AssetsViewModel, onBack: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val session by vm.sessionVersion.collectAsStateWithLifecycle()
    var toFutures by remember { mutableStateOf(true) }
    var amount by remember { mutableStateOf("") }
    var review by remember(session) { mutableStateOf(false) }
    var submitting by remember(session) { mutableStateOf(false) }
    LaunchedEffect(Unit) { vm.load(silent = true) }
    LaunchedEffect(vm.transferError, vm.transferReceipt) {
        if (vm.transferReceipt != null && vm.transferError == null) { review = false;submitting = false }
        if (vm.transferError != null) submitting = false
    }
    val avail = if (toFutures) vm.spotAvailUsdt() else vm.futuresAvail()
    val raw = amount.trim()
    val value = runCatching { Dec.parse(raw) }.getOrDefault(java.math.BigDecimal.ZERO)
    fun rejectAmount():String? = when {
        raw.isEmpty() || !Regex("""[0-9]+(?:\.[0-9]{1,2})?""").matches(raw) -> "请输入有效划转数量"
        value.signum() <= 0 -> "请输入划转数量"
        value > avail -> "可用余额不足"
        else -> null
    }

    Column(Modifier.fillMaxSize().background(p.bg).navigationBarsPadding().imePadding()) {
        AssetsNavBar("划转", onBack = onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(top = 8.dp).padding(bottom = 16.dp)) {
            Box {
                Column {
                    AccountCard("从", if (toFutures) "资金账户" else "合约账户")
                    Spacer(Modifier.height(8.dp))
                    AccountCard("到", if (toFutures) "合约账户" else "资金账户")
                }
                Box(
                    Modifier.align(Alignment.Center).size(30.dp).background(p.bg, CircleShape).border(1.dp, p.border, CircleShape)
                        .pressable(enabled = !(vm.transferBusy || submitting), scale = 1f) { toFutures = !toFutures; vm.clearTransferFeedback() },
                    contentAlignment = Alignment.Center,
                ) { Text("⇅", style = lb(FontSize.small, FontWeight.SemiBold), color = p.textPrimary) }
            }
            Text("币种", Modifier.padding(top = 10.dp), style = lb(FontSize.caption), color = p.textPrimary)
            Row(
                Modifier.fillMaxWidth().height(48.dp).background(p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f) { vm.toast.show("当前仅支持 USDT 划转") }.padding(horizontal = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CoinIcon("USDT", 22.dp); Spacer(Modifier.width(8.dp))
                Text("USDT", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Spacer(Modifier.width(6.dp)); Text("Tether USDT", style = lb(FontSize.small), color = p.textTertiary)
            }
            Text("划转数量", Modifier.padding(top = 10.dp), style = lb(FontSize.caption), color = p.textPrimary)
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                LbTextField(
                    amount,
                    { v ->
                        if (!(vm.transferBusy || submitting)) {
                            val c = v.filter { it.isDigit() || it == '.' }
                            if (c.count { it == '.' } <= 1 && c.substringAfter('.', "").length <= 2) { amount=c;vm.clearTransferFeedback() }
                        }
                    },
                    "0", Modifier.weight(1f), keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal,
                    error = if (value > avail) "可用余额不足" else null, enabled = !(vm.transferBusy || submitting), testTag = "transfer.amount",
                )
                Spacer(Modifier.width(8.dp)); Text("USDT", style = lb(FontSize.label), color = p.textPrimary)
                Spacer(Modifier.width(8.dp))
                Text("最大", Modifier.pressable(enabled = !(vm.transferBusy || submitting), scale = 1f) { amount = Fmt.amount(avail, 2).replace(",", "");vm.clearTransferFeedback() }, style = lb(FontSize.label, FontWeight.Medium), color = p.brand)
            }
            Row(Modifier.padding(top = 8.dp)) {
                Text("可用", style = lbNum(FontSize.small), color = p.textSecondary)
                Spacer(Modifier.weight(1f)); Text("${Fmt.amount(avail, 2)} USDT", style = lbNum(FontSize.small), color = p.textPrimary)
            }
            vm.transferReceipt?.let { r ->
                Column(Modifier.fillMaxWidth().padding(top=16.dp).background(p.bgElevated,RoundedCornerShape(Radius.md)).padding(12.dp)) {
                    Text(if(r.status=="completed")"划转成功" else "划转已受理",style=lb(FontSize.body,FontWeight.SemiBold),color=p.textPrimary)
                    Text("状态：${r.status}",Modifier.padding(top=8.dp),style=lb(FontSize.label),color=p.textSecondary)
                    r.ref?.let { Text("编号：$it",Modifier.padding(top=4.dp),style=lbNum(FontSize.label),color=p.textSecondary) }
                    Text(if(r.balanceRefreshed)"余额已按服务端读回更新" else "余额暂未读回成功，请稍后刷新",Modifier.padding(top=4.dp).testTag("transfer.receipt"),style=lb(FontSize.small),color=p.textSecondary)
                    if (r.balanceRefreshed) {
                        Text("读回现货可用：${Fmt.amount(Dec.parse(r.spotFree), 2)} USDT",Modifier.padding(top=4.dp),style=lbNum(FontSize.small),color=p.textPrimary)
                        Text("读回合约可用：${Fmt.amount(Dec.parse(r.futuresAvailable), 2)} USDT",Modifier.padding(top=4.dp),style=lbNum(FontSize.small),color=p.textPrimary)
                    }
                }
            }
        }
        LbButton(rejectAmount() ?: if(toFutures)"复核现货→合约划转" else "复核合约→现货划转",
            Modifier.padding(horizontal=Spacing.pageH).padding(bottom=8.dp),
            enabled=rejectAmount()==null&&!(vm.transferBusy || submitting),testTag="transfer.review"){review=true;vm.clearTransferFeedback()}
    }
    if(review) {
        LbSheet("确认划转",onClose={if(!(vm.transferBusy || submitting))review=false},showClose=!(vm.transferBusy || submitting),onWindowReady=onDialogWindow) {
            Column(Modifier.fillMaxWidth().padding(horizontal=Spacing.pageH).padding(bottom=24.dp)) {
                Text("从 ${if(toFutures)"资金账户" else "合约账户"} 到 ${if(toFutures)"合约账户" else "资金账户"}",style=lb(FontSize.label,FontWeight.Medium),color=p.textPrimary)
                Text("$amount USDT",Modifier.padding(top=8.dp).testTag("transfer.review.amount"),style=lbNum(FontSize.title),color=p.textPrimary,)
                Text("提交后将按服务端最终状态更新余额；重复确认不会创建并发请求。",Modifier.padding(top=8.dp),style=lb(FontSize.small),color=p.textSecondary)
                vm.transferError?.let { Text(it,Modifier.padding(top=10.dp),style=lb(FontSize.label),color=p.down) }
                LbButton(if(vm.transferBusy || submitting)"等待服务端结果…"else "确认划转",Modifier.padding(top=18.dp),enabled=!(vm.transferBusy || submitting),loading=vm.transferBusy || submitting,testTag="transfer.confirm") {
                    if(vm.transferFutures(toFutures,amount)) submitting=true
                }
                LbButton("取消",kind=ButtonKind.SECONDARY,enabled=!(vm.transferBusy || submitting),testTag="transfer.dismiss"){review=false}
            }
        }
    }
}

@Composable
private fun AccountCard(label: String, name: String) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.md)).padding(12.dp)) {
        Text(label, style = lb(FontSize.small), color = p.textSecondary)
        Spacer(Modifier.height(4.dp))
        Text(name, style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
    }
}
