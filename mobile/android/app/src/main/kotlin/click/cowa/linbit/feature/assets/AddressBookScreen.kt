package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiCreateWithdrawAddressRequest
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawAddress
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.components.SheetOptionRow
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import kotlinx.coroutines.launch

@Composable
fun AddressBookScreen(
    vm: AssetsViewModel,
    onBack: () -> Unit,
    onAdd: (String?) -> Unit,
    onPickForWithdraw: ((ApiWithdrawAddress) -> Unit)? = null,
) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var list by remember { mutableStateOf<List<ApiWithdrawAddress>?>(null) }
    var err by remember { mutableStateOf<String?>(null) }
    var whitelist by remember { mutableStateOf<Boolean?>(null) }
    var filter by remember { mutableStateOf(0) }
    var verify by remember { mutableStateOf<Pair<String, (String) -> Unit>?>(null) }

    fun reload() {
        scope.launch {
            err = null
            vm.addresses().fold(onSuccess = { list = it }, onFailure = { err = it.lbMessage(); list = emptyList() })
            vm.whitelist().onSuccess { whitelist = it }
        }
    }
    LaunchedEffect(Unit) { reload() }

    Column(Modifier.fillMaxSize().background(p.bg)) {
        AssetsNavBar("地址簿", onBack = onBack) {
            LbIcon(R.drawable.ic_mc_plus_square_light, Metrics.navIcon, p.textPrimary, Modifier.pressable(scale = 1f) { onAdd(null) })
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(vertical = 12.dp)) {
            AssetsCard {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("提现白名单", style = lb(FontSize.label, FontWeight.SemiBold), color = p.textPrimary)
                        Text("开启后仅能向白名单地址提现", style = lb(FontSize.small), color = p.textSecondary)
                    }
                    val on = whitelist == true
                    BoxToggle(on, enabled = whitelist != null) {
                        verify = (if (on) "关闭提现白名单" else "开启提现白名单") to { totp ->
                            scope.launch {
                                vm.setWhitelist(!on, totp).fold(
                                    onSuccess = { whitelist = it; vm.toast.show(if (it) "已开启提现白名单" else "已关闭提现白名单") },
                                    onFailure = { e -> vm.toast.show(e.lbMessage()) },
                                )
                            }
                        }
                    }
                }
            }
            Spacer(Modifier.height(12.dp))
            Row(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.md)).padding(4.dp)) {
                listOf("全部地址", "白名单地址").forEachIndexed { i, t ->
                    val sel = filter == i
                    Text(
                        t,
                        Modifier.weight(1f).background(if (sel) p.bg else androidx.compose.ui.graphics.Color.Transparent, RoundedCornerShape(Radius.sm)).pressable(scale = 1f) { filter = i }.padding(vertical = 8.dp),
                        style = lb(FontSize.small, if (sel) FontWeight.SemiBold else FontWeight.Normal),
                        color = p.textPrimary,
                        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                    )
                }
            }
            Spacer(Modifier.height(12.dp))
            when {
                list == null -> Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) { CircularProgressIndicator(Modifier.padding(40.dp), color = p.brand, strokeWidth = 2.dp) }
                err != null && list!!.isEmpty() -> EmptyState(err ?: "加载失败")
                else -> {
                    val shown = if (filter == 1) list!!.filter { it.whitelisted } else list!!
                    if (shown.isEmpty()) {
                        EmptyState(if (filter == 1) "暂无白名单地址" else "暂无地址")
                        Spacer(Modifier.height(12.dp))
                        Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) { LbButton("添加地址", Modifier.width(140.dp), onClick = { onAdd(null) }) }
                    }
                    shown.forEach { a ->
                        Spacer(Modifier.height(10.dp))
                        AssetsCard {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                CoinIcon(a.asset, 20.dp)
                                Spacer(Modifier.width(8.dp))
                                Text(a.label?.takeIf { it.isNotBlank() } ?: a.asset, style = lb(FontSize.label, FontWeight.SemiBold), color = p.textPrimary)
                                Spacer(Modifier.width(6.dp))
                                Text(a.network, Modifier.background(p.fill, RoundedCornerShape(Radius.xs)).padding(horizontal = 6.dp, vertical = 2.dp), style = lb(11.sp), color = p.textSecondary)
                                if (a.whitelisted) {
                                    Spacer(Modifier.width(4.dp))
                                    Text("白名单", Modifier.background(p.up.copy(alpha = 0.12f), RoundedCornerShape(Radius.xs)).padding(horizontal = 6.dp, vertical = 2.dp), style = lb(11.sp), color = p.up)
                                }
                            }
                            Spacer(Modifier.height(8.dp))
                            Text(a.address, style = lbNum(FontSize.small), color = p.textPrimary)
                            if (!a.memo.isNullOrBlank()) {
                                Spacer(Modifier.height(4.dp)); AssetKv("Memo", a.memo!!)
                            }
                            Spacer(Modifier.height(8.dp))
                            Row {
                                Spacer(Modifier.weight(1f))
                                if (onPickForWithdraw != null) {
                                    Text(
                                        "使用",
                                        Modifier.pressable(scale = 1f) { onPickForWithdraw(a) }
                                            .padding(horizontal = 10.dp, vertical = 4.dp)
                                            .background(p.brand.copy(alpha = 0.15f), RoundedCornerShape(Radius.sm)),
                                        style = lb(FontSize.small),
                                        color = p.brand,
                                    )
                                    Spacer(Modifier.width(8.dp))
                                }
                                Text("复制", Modifier.pressable(scale = 1f) { copyText(ctx, a.address); vm.toast.show("已复制地址") }.padding(horizontal = 10.dp, vertical = 4.dp).background(p.fill, RoundedCornerShape(Radius.sm)), style = lb(FontSize.small), color = p.textPrimary)
                                Spacer(Modifier.width(8.dp))
                                Text(if (a.whitelisted) "移出白名单" else "加入白名单", Modifier.pressable(scale = 1f) {
                                    verify = (if (a.whitelisted) "移出白名单" else "加入白名单") to { totp ->
                                        scope.launch {
                                            vm.updateAddressWhitelist(a.id, !a.whitelisted, totp).fold(
                                                onSuccess = { vm.toast.show("已更新"); reload() },
                                                onFailure = { e -> vm.toast.show(e.lbMessage()) },
                                            )
                                        }
                                    }
                                }.padding(horizontal = 10.dp, vertical = 4.dp).background(p.fill, RoundedCornerShape(Radius.sm)), style = lb(FontSize.small), color = p.textPrimary)
                            }
                        }
                    }
                }
            }
        }
        LbButton("添加地址", Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), onClick = { onAdd(null) })
    }

    verify?.let { (title, act) ->
        var totp by remember { mutableStateOf("") }
        LbSheet(title, onClose = { verify = null }, showClose = true) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
                LbTextField(totp, { totp = it.filter(Char::isDigit).take(6) }, "请输入 6 位谷歌验证码", keyboardType = KeyboardType.Number)
                Spacer(Modifier.height(12.dp))
                LbButton("确认", enabled = totp.length == 6) { act(totp); verify = null }
            }
        }
    }
}

@Composable
private fun BoxToggle(on: Boolean, enabled: Boolean, onToggle: () -> Unit) {
    val p = LB.p
    androidx.compose.foundation.layout.Box(
        Modifier
            .width(48.dp).height(28.dp)
            .background(if (on) p.up else p.fillStrong, RoundedCornerShape(999.dp))
            .pressable(enabled = enabled, scale = 1f, onClick = onToggle)
            .padding(2.dp),
        contentAlignment = if (on) Alignment.CenterEnd else Alignment.CenterStart,
    ) {
        androidx.compose.foundation.layout.Box(Modifier.width(24.dp).height(24.dp).background(p.bg, RoundedCornerShape(999.dp)))
    }
}

@Composable
fun AddAddressScreen(assetInit: String?, vm: AssetsViewModel, onBack: () -> Unit, onDone: () -> Unit) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val scope = rememberCoroutineScope()
    var asset by remember { mutableStateOf(assetInit ?: "USDT") }
    var nets by remember { mutableStateOf<List<ApiWalletNetwork>>(emptyList()) }
    var net by remember { mutableStateOf<ApiWalletNetwork?>(null) }
    var address by remember { mutableStateOf("") }
    var memo by remember { mutableStateOf("") }
    var label by remember { mutableStateOf("") }
    var whitelisted by remember { mutableStateOf(true) }
    var totp by remember { mutableStateOf("") }
    var showNet by remember { mutableStateOf(false) }
    var showAsset by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }

    LaunchedEffect(asset) {
        net = null
        vm.loadWalletNetworksForWithdraw(asset).onSuccess {
            nets = it
            net = AssetsLogic.defaultDepositNetwork(it) // 推荐优先；无推荐则单条
                ?: it.firstOrNull()
        }
    }

    val valid = net != null && address.trim().length >= 10 && totp.length == 6 &&
        (net?.memoRequired != true || memo.isNotBlank())

    Column(Modifier.fillMaxSize().background(p.bg)) {
        AssetsNavBar("新增地址", onBack = onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(top = 8.dp)) {
            WarnBanner("为保障账户安全，修改密码或绑定/解绑身份验证器后，短期内可能无法保存提现地址。")
            Spacer(Modifier.height(12.dp))
            PickerField("选择币种", asset) { showAsset = true }
            Spacer(Modifier.height(10.dp))
            PickerField("选择网络", net?.network?.let { AssetsLogic.displayNetwork(it) } ?: "", "选择网络") { showNet = true }
            Spacer(Modifier.height(10.dp))
            Text("地址", style = lb(FontSize.caption), color = p.textSecondary)
            LbTextField(address, { address = it }, "长按粘贴地址", Modifier.padding(top = 4.dp))
            if (net?.memoRequired == true) {
                Spacer(Modifier.height(10.dp))
                LbTextField(memo, { memo = it }, "该网络必须填写 Memo")
            }
            Spacer(Modifier.height(10.dp))
            LbTextField(label, { label = it }, "如：我的USDT钱包")
            Spacer(Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.pressable(scale = 1f) { whitelisted = !whitelisted }) {
                Text(if (whitelisted) "☑" else "☐", style = lb(FontSize.body), color = p.textPrimary)
                Spacer(Modifier.width(8.dp))
                Text("加入提现白名单", style = lb(FontSize.label), color = p.textPrimary)
            }
            Spacer(Modifier.height(12.dp))
            LbTextField(totp, { totp = it.filter(Char::isDigit).take(6) }, "请输入 6 位谷歌验证码", keyboardType = KeyboardType.Number)
            Spacer(Modifier.height(24.dp))
        }
        LbButton("保存", Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), enabled = valid, loading = saving) {
            scope.launch {
                saving = true
                vm.createAddress(
                    ApiCreateWithdrawAddressRequest(
                        asset = asset,
                        network = net!!.network!!,
                        address = address.trim(),
                        memo = memo.ifBlank { null },
                        label = label.ifBlank { null },
                        whitelisted = whitelisted,
                        totpCode = totp,
                    ),
                ).fold(
                    onSuccess = { vm.toast.show("地址已保存"); onDone() },
                    onFailure = { vm.toast.show(it.lbMessage()) },
                )
                saving = false
            }
        }
    }
    if (showNet) {
        LbSheet("选择网络", onClose = { showNet = false }, showClose = true) {
            if (nets.isEmpty()) EmptyState("暂无可用网络", Modifier.height(200.dp))
            nets.forEach { n ->
                SheetOptionRow(AssetsLogic.displayNetwork(n.network), n.network == net?.network, subtitle = AssetsLogic.etaText(n.estimatedArrivalMinutes)) {
                    net = n; showNet = false
                }
            }
            Spacer(Modifier.height(20.dp))
        }
    }
    if (showAsset) {
        LbSheet("选择币种", onClose = { showAsset = false }, showClose = true) {
            vm.coinUniverse().take(30).forEach { a ->
                SheetOptionRow(a, a == asset) { asset = a; showAsset = false }
            }
            Spacer(Modifier.height(20.dp))
        }
    }
}

@Composable
fun WithdrawRecordsScreen(vm: AssetsViewModel, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    val p = LB.p
    var items by remember { mutableStateOf<List<click.cowa.linbit.core.generated.ApiWithdrawal>?>(null) }
    LaunchedEffect(Unit) {
        vm.withdrawRecords().onSuccess { items = it }.onFailure { vm.toast.show(it.lbMessage()); items = emptyList() }
    }
    Column(Modifier.fillMaxSize().background(p.bg)) {
        AssetsNavBar("提现记录", onBack = onBack)
        when {
            items == null -> Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) { CircularProgressIndicator(Modifier.padding(40.dp), color = p.brand, strokeWidth = 2.dp) }
            items!!.isEmpty() -> EmptyState("暂无提现记录")
            else -> Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
                items!!.forEach { w ->
                    AssetsCard(Modifier.padding(bottom = 10.dp)) {
                        Text("${w.amount} ${w.asset} · ${AssetsLogic.displayNetwork(w.network)}", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                        Spacer(Modifier.height(4.dp))
                        AssetKv("状态", w.status.value)
                        AssetKv("时间", w.createdAt)
                    }
                }
            }
        }
    }
}
