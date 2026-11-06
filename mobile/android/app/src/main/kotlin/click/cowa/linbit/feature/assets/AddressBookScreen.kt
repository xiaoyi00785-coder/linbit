package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiCreateWithdrawAddressRequest
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawAddress
import click.cowa.linbit.core.generated.ApiWithdrawal
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.ButtonKind
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
    onDialogWindow: (android.view.Window?) -> Unit = {},
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
    var editTarget by remember { mutableStateOf<ApiWithdrawAddress?>(null) }
    var editPhase by remember { mutableStateOf(0) }
    var editLabel by remember { mutableStateOf("") }
    var editMemo by remember { mutableStateOf("") }
    var editTotp by remember { mutableStateOf("") }
    val session by vm.sessionVersion.collectAsStateWithLifecycle()
    LaunchedEffect(session) { editTarget = null }
    LaunchedEffect(vm.addressEditReceipt?.readback?.id) {
        if (vm.addressEditReceipt != null) {
            editTarget = null; list = null
            scope.launch { vm.addresses().fold(onSuccess={list=it},onFailure={err=it.lbMessage();list=emptyList()}) }
        }
    }

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
                    list!!.firstOrNull { it.id == vm.addressEditReceipt?.readback?.id }?.let { Text("已从服务端列表读回修改", Modifier.testTag("address.edit.readback"), style = lb(FontSize.small), color = p.brand) }
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
                            LbButton("编辑", kind = ButtonKind.OUTLINE, height = 32.dp, testTag = "address.edit.open") {
                                editTarget = a; editPhase = 0; editLabel = a.label.orEmpty(); editMemo = a.memo.orEmpty(); editTotp = ""
                            }
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

    editTarget?.let { target ->
        val busy = vm.addressEditBusy
        LbSheet(if (editPhase == 0) "编辑提现地址" else "确认修改提现地址", onClose = { if (!busy) editTarget = null }, showClose = !busy, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
                AssetKv("币种 / 网络", "${target.asset} / ${AssetsLogic.displayNetwork(target.network)}")
                AssetKv("地址", target.address)
                Spacer(Modifier.height(10.dp))
                if (editPhase == 0) {
                    LbTextField(editLabel, { if (!busy) editLabel = it }, "标签", testTag = "address.edit.label")
                    Spacer(Modifier.height(10.dp))
                    LbTextField(editMemo, { if (!busy) editMemo = it }, "Memo", testTag = "address.edit.memo")
                    Spacer(Modifier.height(12.dp))
                    LbButton("复核修改", enabled = !busy, testTag = "address.edit.review") { if (editPhase == 0) editPhase = 1 }
                } else {
                    AssetKv("修改后标签", editLabel); AssetKv("修改后 Memo", editMemo)
                    Text("仅修改标签/Memo；服务端列表必须读回同 id 才算列表更新。", Modifier.padding(top=8.dp), style = lb(FontSize.small), color = p.textSecondary)
                    Spacer(Modifier.height(10.dp))
                    LbTextField(editTotp, { if (!busy) editTotp = it.filter(Char::isDigit).take(6) }, "请输入 6 位谷歌验证码", keyboardType = KeyboardType.Number, testTag = "address.edit.totp")
                    Spacer(Modifier.height(12.dp))
                    vm.addressEditError?.let { Text(it, Modifier.padding(bottom = 8.dp), style = lb(FontSize.small), color = p.down) }
                    LbButton(if (busy) "等待服务端结果…" else "确认修改", enabled = !busy && editTotp.length == 6, loading = busy, testTag = "address.edit.confirm") {
                        vm.submitEditAddressConfirmed(target.id, editLabel, editMemo, editTotp)
                    }
                }
                Spacer(Modifier.height(8.dp))
                LbButton("取消", kind = ButtonKind.SECONDARY, enabled = !busy, testTag = "address.edit.dismiss") { editTarget = null }
            }
        }
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
fun AddAddressScreen(assetInit: String?, vm: AssetsViewModel, onBack: () -> Unit, onDone: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
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
    var showConfirm by remember { mutableStateOf(false) }
    val session by vm.sessionVersion.collectAsStateWithLifecycle()

    LaunchedEffect(session) {
        showConfirm = false
        vm.clearAddressFeedback()
    }
    LaunchedEffect(vm.addressReceipt) {
        if (vm.addressReceipt != null) {
            showConfirm = false
            onDone()
        }
    }

    LaunchedEffect(asset) {
        net = null
        vm.loadWalletNetworksForWithdraw(asset).onSuccess {
            nets = it
            net = AssetsLogic.defaultDepositNetwork(it) // 推荐优先；无推荐则单条
                ?: it.firstOrNull()
        }
    }

    val valid = net != null && address.trim().length >= 10 &&
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
            LbTextField(address, { if(!vm.addressBusy) address = it }, "长按粘贴地址", Modifier.padding(top = 4.dp), testTag = "address.input")
            if (net?.memoRequired == true) {
                Spacer(Modifier.height(10.dp))
                LbTextField(memo, { if(!vm.addressBusy) memo = it }, "该网络必须填写 Memo")
            }
            Spacer(Modifier.height(10.dp))
            LbTextField(label, { if(!vm.addressBusy) label = it }, "如：我的USDT钱包", testTag = "address.label")
            Spacer(Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.pressable(scale = 1f) { whitelisted = !whitelisted }) {
                Text(if (whitelisted) "☑" else "☐", style = lb(FontSize.body), color = p.textPrimary)
                Spacer(Modifier.width(8.dp))
                Text("加入提现白名单", style = lb(FontSize.label), color = p.textPrimary)
            }
            Spacer(Modifier.height(12.dp))

            Spacer(Modifier.height(24.dp))
        }
        LbButton("复核并保存", Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), enabled = valid && !vm.addressBusy && vm.addressReceipt == null, loading = vm.addressBusy, testTag = "address.review") {
            showConfirm = true
        }
    }

    if (showConfirm) {
        LbSheet("确认新增提现地址", onClose = { if (!vm.addressBusy) showConfirm = false }, showClose = !vm.addressBusy, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
                Text("提交后将向服务端新增地址；仅服务端列表读回同一条记录才算保存更新。", style = lb(FontSize.small), color = p.textSecondary)
                AssetKv("币种", asset); AssetKv("网络", AssetsLogic.displayNetwork(net?.network)); AssetKv("地址", address.trim())
                if (label.isNotBlank()) AssetKv("标签", label.trim()); if (memo.isNotBlank()) AssetKv("Memo", memo.trim())
                AssetKv("加入白名单", if (whitelisted) "是" else "否")
                Spacer(Modifier.height(12.dp))
                val receipt = vm.addressReceipt
                if (receipt != null) {
                    Text("列表已读回记录 ${receipt.readback.id}", style = lb(FontSize.small), color = p.textPrimary, modifier = Modifier.testTag("address.receipt"))
                } else {
                    LbTextField(totp, { if (!vm.addressBusy) totp = it.filter(Char::isDigit).take(6) }, "请输入 6 位谷歌验证码", keyboardType = KeyboardType.Number, testTag = "address.totp")
                }
                Spacer(Modifier.height(12.dp))
                vm.addressError?.let { Text(it, Modifier.padding(bottom = 8.dp), style = lb(FontSize.small), color = p.down) }
                LbButton(if (vm.addressBusy) "等待服务端结果…" else "确认新增", loading = vm.addressBusy, enabled = receipt != null || (valid && totp.length == 6 && !vm.addressBusy), testTag = "address.confirm") {
                    if (receipt != null) { showConfirm = false; onDone(); return@LbButton }
                    vm.submitCreateAddressConfirmed(ApiCreateWithdrawAddressRequest(asset = asset, network = net!!.network!!, address = address.trim(), memo = memo.ifBlank { null }, label = label.ifBlank { null }, whitelisted = whitelisted, totpCode = totp))
                }
                LbButton("取消", kind = ButtonKind.SECONDARY, enabled = !vm.addressBusy, testTag = "address.dismiss") { showConfirm = false }
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
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<ApiWithdrawal>?>(null) }
    var cancelTarget by remember { mutableStateOf<ApiWithdrawal?>(null) }
    var cancellingId by remember { mutableStateOf<String?>(null) }

    fun reload() {
        scope.launch {
            vm.withdrawRecords().fold(
                onSuccess = { items = it },
                onFailure = { e -> vm.toast.show(e.lbMessage()); if (items == null) items = emptyList() },
            )
        }
    }
    LaunchedEffect(Unit) { reload() }

    fun statusLabel(s: ApiWithdrawal.Status): String = when (s) {
        ApiWithdrawal.Status.PENDING -> "待审核"
        ApiWithdrawal.Status.PROCESSING -> "处理中"
        ApiWithdrawal.Status.COMPLETED -> "已完成"
        ApiWithdrawal.Status.FAILED -> "失败"
        ApiWithdrawal.Status.CANCELED -> "已撤销"
    }

    Column(Modifier.fillMaxSize().background(p.bg)) {
        AssetsNavBar("提现记录", onBack = onBack)
        when {
            items == null -> Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                CircularProgressIndicator(Modifier.padding(40.dp), color = p.brand, strokeWidth = 2.dp)
            }
            items!!.isEmpty() -> EmptyState("暂无提现记录")
            else -> Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
                items!!.forEach { w ->
                    val canCancel = AssetsLogic.canCancelWithdrawal(w.status) && cancellingId != w.id
                    AssetsCard(Modifier.padding(bottom = 10.dp)) {
                        Text(
                            "${w.amount} ${w.asset} · ${AssetsLogic.displayNetwork(w.network)}",
                            style = lb(FontSize.label, FontWeight.Medium),
                            color = p.textPrimary,
                        )
                        Spacer(Modifier.height(4.dp))
                        AssetKv("状态", statusLabel(w.status))
                        AssetKv("时间", AssetsLogic.displayWithdrawTime(w.createdAt))
                        if (AssetsLogic.canCancelWithdrawal(w.status) || cancellingId == w.id) {
                            Spacer(Modifier.height(8.dp))
                            LbButton(
                                "撤销申请",
                                kind = ButtonKind.OUTLINE,
                                height = 36.dp,
                                loading = cancellingId == w.id,
                                enabled = canCancel,
                            ) { cancelTarget = w }
                        }
                    }
                }
            }
        }
    }

    val target = cancelTarget
    if (target != null) {
        LbSheet("确认撤销提现", onClose = { if (cancellingId == null) cancelTarget = null }, showClose = cancellingId == null) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    "仅待审核（pending）可撤销。撤销后冻结金额将解冻退回可用余额。",
                    style = lb(FontSize.small),
                    color = p.textSecondary,
                )
                AssetKv("数量", "${target.amount} ${target.asset}")
                AssetKv("网络", AssetsLogic.displayNetwork(target.network))
                AssetKv("地址", target.address)
                LbButton(
                    "确认撤销",
                    kind = ButtonKind.SELL,
                    loading = cancellingId == target.id,
                    enabled = cancellingId == null && AssetsLogic.canCancelWithdrawal(target.status),
                ) {
                    if (cancellingId != null) return@LbButton
                    // 竞态：确认瞬间再判一次；非 pending 则提示并刷新
                    if (!AssetsLogic.canCancelWithdrawal(target.status)) {
                        vm.toast.show("该提现已无法撤销", click.cowa.linbit.ui.components.ToastCenter.Kind.ERROR)
                        cancelTarget = null
                        reload()
                        return@LbButton
                    }
                    scope.launch {
                        cancellingId = target.id
                        vm.cancelWithdraw(target.id).fold(
                            onSuccess = { updated ->
                                items = items?.let { AssetsLogic.applyWithdrawalUpdate(it, updated) }
                                vm.toast.show("已撤销提现申请")
                                cancelTarget = null
                                vm.load(silent = true) // 余额解冻刷新
                                reload()
                            },
                            onFailure = { e ->
                                vm.toast.show(e.lbMessage())
                                // 409 / WITHDRAW_NOT_CANCELABLE：刷新列表对齐真实状态
                                reload()
                                cancelTarget = null
                            },
                        )
                        cancellingId = null
                    }
                }
                LbButton("取消", kind = ButtonKind.OUTLINE, enabled = cancellingId == null) { cancelTarget = null }
            }
        }
    }
}
