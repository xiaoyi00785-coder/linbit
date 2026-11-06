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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.network.lbMessage
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
import kotlinx.coroutines.launch

/** 划转：现货(资金) ↔ 合约，币种固定 USDT（对照图 #46 / iOS TransferView）。 */
@Composable
fun TransferScreen(vm: AssetsViewModel, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val scope = rememberCoroutineScope()
    var toFutures by remember { mutableStateOf(true) }
    var amount by remember { mutableStateOf("") }
    var submitting by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { vm.load(silent = true) }
    val avail = if (toFutures) vm.spotAvailUsdt() else vm.futuresAvail()
    val value = Dec.parse(amount)
    val err = if (value > avail) "可用余额不足" else null

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
                    Modifier
                        .align(Alignment.Center)
                        .size(30.dp)
                        .background(p.bg, CircleShape)
                        .border(1.dp, p.border, CircleShape)
                        .pressable(scale = 1f) { toFutures = !toFutures },
                    contentAlignment = Alignment.Center,
                ) { Text("⇅", style = lb(FontSize.small, FontWeight.SemiBold), color = p.textPrimary) }
            }
            Text("币种", Modifier.padding(top = 10.dp), style = lb(FontSize.caption), color = p.textPrimary)
            Row(
                Modifier.fillMaxWidth().height(48.dp).background(p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f) { vm.toast.show("当前仅支持 USDT 划转") }.padding(horizontal = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CoinIcon("USDT", 22.dp)
                Spacer(Modifier.width(8.dp))
                Text("USDT", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Spacer(Modifier.width(6.dp))
                Text("Tether USDT", style = lb(FontSize.small), color = p.textTertiary)
            }
            Text("划转数量", Modifier.padding(top = 10.dp), style = lb(FontSize.caption), color = p.textPrimary)
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                LbTextField(
                    amount,
                    { v ->
                        val c = v.filter { it.isDigit() || it == '.' }
                        if (c.count { it == '.' } <= 1 && (c.substringAfter('.', "").length) <= 2) amount = c
                    },
                    "0",
                    Modifier.weight(1f),
                    keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal,
                    error = err,
                )
                Spacer(Modifier.width(8.dp))
                Text("USDT", style = lb(FontSize.label), color = p.textPrimary)
                Spacer(Modifier.width(8.dp))
                Text("最大", Modifier.pressable(scale = 1f) { amount = Fmt.amount(avail, 2).replace(",", "") }, style = lb(FontSize.label, FontWeight.Medium), color = p.brand)
            }
            Row(Modifier.padding(top = 8.dp)) {
                Text("可用", style = lbNum(FontSize.small), color = p.textSecondary)
                Spacer(Modifier.weight(1f))
                Text("${Fmt.amount(avail, 2)} USDT", style = lbNum(FontSize.small), color = p.textPrimary)
            }
        }
        LbButton(
            "确认划转",
            Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp),
            enabled = value.signum() > 0 && err == null,
            loading = submitting,
            onClick = {
                scope.launch {
                    submitting = true
                    vm.transferFutures(toFutures, amount).fold(
                        onSuccess = {
                            vm.toast.show("划转成功")
                            amount = ""
                            vm.load(silent = true)
                        },
                        onFailure = { vm.toast.show(it.lbMessage()) },
                    )
                    submitting = false
                }
            },
        )
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
