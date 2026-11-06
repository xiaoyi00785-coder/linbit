package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.core.generated.ApiInternalTransferRequest
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.generated.ApiWithdrawRequest
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.data.assets.AssetsRoute
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
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
import java.math.BigDecimal

/**
 * 提现：链上 / 内部转账。
 * 最小额 / 预计到账时间一律取后端（/wallet/withdraw/networks 与 /wallet/networks），
 * 不写死 DEFAULT_MIN_WITHDRAW。
 */
@Composable
fun WithdrawScreen(
    assetInit: String?,
    vm: AssetsViewModel,
    assetsNav: AssetsNav,
    onBack: () -> Unit,
    onChangeAsset: () -> Unit,
    onDialogWindow: (android.view.Window?) -> Unit = {},
) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val scope = rememberCoroutineScope()
    var asset by remember { mutableStateOf(assetInit ?: "USDT") }
    var tab by remember { mutableStateOf(0) } // 0 链上 1 内部
    var nets by remember { mutableStateOf<List<ApiWithdrawNetwork>>(emptyList()) }
    var walletNets by remember { mutableStateOf<List<ApiWalletNetwork>>(emptyList()) }
    var net by remember { mutableStateOf<ApiWithdrawNetwork?>(null) }
    var walletNet by remember { mutableStateOf<ApiWalletNetwork?>(null) }
    var showNets by remember { mutableStateOf(false) }
    var addr by remember { mutableStateOf("") }
    var memo by remember { mutableStateOf("") }
    var amount by remember { mutableStateOf("") }
    var to by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    var smsCode by remember { mutableStateOf("") }
    var fundPw by remember { mutableStateOf("") }
    var showConfirm by remember { mutableStateOf(false) }
    var submitting by remember { mutableStateOf(false) }
    var twoFa by remember { mutableStateOf(true) }
    var sendingCode by remember { mutableStateOf(false) }
    var pendingNetwork by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) {
        assetsNav.consumeWithdrawPrefill()?.let { pref ->
            asset = pref.asset
            addr = pref.address
            memo = pref.memo.orEmpty()
            pendingNetwork = pref.network
        }
    }
    LaunchedEffect(showConfirm) {
        if (showConfirm) twoFa = runCatching { vm.twoFactorEnabled() }.getOrDefault(true)
    }

    LaunchedEffect(asset) {
        nets = emptyList(); net = null; walletNets = emptyList(); walletNet = null
        vm.loadWithdrawNetworks(asset).onSuccess { list ->
            nets = list
            net = list.firstOrNull()
        }
        vm.loadWalletNetworksForWithdraw(asset).onSuccess { list ->
            walletNets = list
            walletNet = list.firstOrNull { it.recommended == true } ?: list.firstOrNull()
            // 若 withdraw/networks 空，用 wallet networks 兜底构造展示
            if (nets.isEmpty() && list.isNotEmpty()) {
                nets = list.map {
                    ApiWithdrawNetwork(
                        network = it.network!!,
                        withdrawEnabled = it.withdrawEnabled != false,
                        memoRequired = it.memoRequired == true,
                        minWithdraw = it.minWithdraw,
                        maxWithdraw = it.maxWithdraw,
                        gasFeeEstimate = it.withdrawFee,
                        gasFeeAsset = it.withdrawFeeAsset,
                        confirmations = it.confirmations,
                        estimatedArrivalMinutes = it.estimatedArrivalMinutes,
                    )
                }
                net = nets.firstOrNull()
            }
            pendingNetwork?.let { pn ->
                nets.firstOrNull { it.network.equals(pn, true) }?.let { n ->
                    net = n
                    walletNet = walletNets.firstOrNull { it.network.equals(pn, true) }
                }
                pendingNetwork = null
            }
        }
        vm.load(silent = true)
    }
    LaunchedEffect(assetInit) { if (!assetInit.isNullOrBlank()) asset = assetInit }

    val free = (vm.state.value.funding + vm.state.value.spot)
        .firstOrNull { it.asset.equals(asset, true) }?.free
        ?.let { Dec.parse(it) } ?: BigDecimal.ZERO
    val minS = AssetsLogic.positiveAmount(net?.minWithdraw)
    val maxS = AssetsLogic.positiveAmount(net?.maxWithdraw)
    val feeS = AssetsLogic.positiveAmount(net?.gasFeeEstimate)
    val eta = AssetsLogic.etaShort(net?.estimatedArrivalMinutes ?: walletNet?.estimatedArrivalMinutes)
    val memoReq = AssetsLogic.withdrawMemoRequired(net, walletNet)
    val value = Dec.parse(amount)
    val receive = AssetsLogic.receiveAmount(amount, net?.gasFeeEstimate)

    fun amtError(): String? = AssetsLogic.chainAmountError(
        amount = amount,
        free = free,
        minWithdraw = net?.minWithdraw,
        maxWithdraw = net?.maxWithdraw,
        gasFee = net?.gasFeeEstimate,
        asset = asset,
    )

    Column(Modifier.fillMaxSize().background(p.bg)) {
        AssetsNavBar("提现", onBack = onBack) {
            Text("地址簿", Modifier.pressable(scale = 1f) { assetsNav.openAddressBook(forWithdraw = true) }, style = lb(FontSize.small), color = p.textSecondary)
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(top = 8.dp)) {
            WarnBanner("为保障账户安全，修改密码或绑定/解绑身份验证器会触发提现保护期。")
            Spacer(Modifier.height(12.dp))
            Text("选择币种", style = lb(FontSize.caption), color = p.textSecondary)
            Row(
                Modifier.fillMaxWidth().height(48.dp).background(p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f, onClick = onChangeAsset).padding(horizontal = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CoinIcon(asset, 22.dp); Spacer(Modifier.width(8.dp))
                Text(asset, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Text("▾", style = lb(FontSize.small), color = p.textSecondary)
            }
            Spacer(Modifier.height(12.dp))
            Row(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.md)).padding(4.dp)) {
                listOf("链上提现", "内部转账").forEachIndexed { i, t ->
                    val sel = tab == i
                    Text(
                        t,
                        Modifier
                            .weight(1f)
                            .background(if (sel) p.bg else androidx.compose.ui.graphics.Color.Transparent, RoundedCornerShape(Radius.sm))
                            .pressable(scale = 1f) { tab = i }
                            .padding(vertical = 8.dp),
                        style = lb(FontSize.label, if (sel) FontWeight.SemiBold else FontWeight.Normal),
                        color = p.textPrimary,
                        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                    )
                }
            }
            Spacer(Modifier.height(12.dp))
            if (tab == 0) {
                Text("提现网络", style = lb(FontSize.caption), color = p.textSecondary)
                Row(
                    Modifier.fillMaxWidth().height(48.dp).background(p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f) { showNets = true }.padding(horizontal = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        net?.network?.let { AssetsLogic.displayNetwork(it) } ?: "请选择网络",
                        Modifier.weight(1f),
                        style = lb(FontSize.body, FontWeight.Medium),
                        color = if (net == null) p.textTertiary else p.textPrimary,
                    )
                    Text("▾", style = lb(FontSize.small), color = p.textSecondary)
                }
                if (net != null) {
                    Text(AssetsLogic.etaText(net?.estimatedArrivalMinutes), Modifier.padding(top = 4.dp), style = lb(FontSize.small), color = p.textSecondary)
                }
                Spacer(Modifier.height(12.dp))
                Row {
                    Text("提现地址", style = lb(FontSize.caption), color = p.textSecondary)
                    Spacer(Modifier.weight(1f))
                    Text("地址簿 ›", Modifier.pressable(scale = 1f) { assetsNav.openAddressBook(forWithdraw = true) }, style = lb(FontSize.small), color = p.brand)
                }
                LbTextField(addr, { addr = it.trim() }, "输入或粘贴地址", Modifier.padding(top = 4.dp), testTag = "withdraw.address")
                Spacer(Modifier.height(8.dp))
                LbTextField(memo, { memo = it.trim() }, if (memoReq) "Memo / Tag（必填）" else "Memo / Tag（选填）", testTag = "withdraw.memo")
            } else {
                WarnBanner("转给 Linbit 用户，免手续费、实时到账、不上链。需已绑定谷歌验证器。")
                Spacer(Modifier.height(8.dp))
                LbTextField(to, { to = it.trim() }, "请输入接收方 UID 或邮箱", testTag = "withdraw.recipient")
            }
            Spacer(Modifier.height(12.dp))
            Text(if (tab == 0) "提现数量" else "转账数量", style = lb(FontSize.caption), color = p.textSecondary)
            Row(verticalAlignment = Alignment.CenterVertically) {
                LbTextField(
                    amount,
                    { v ->
                        val c = v.filter { it.isDigit() || it == '.' }
                        if (c.count { it == '.' } <= 1) amount = c
                    },
                    if (tab == 0 && minS != null) "最小提现数量: $minS" else "请输入数量",
                    Modifier.weight(1f),
                    keyboardType = KeyboardType.Decimal,
                    error = amtError(), testTag = "withdraw.amount",
                )
                Spacer(Modifier.width(8.dp))
                Text(asset, style = lb(FontSize.label), color = p.textPrimary)
                Spacer(Modifier.width(8.dp))
                Text("最大", Modifier.pressable(scale = 1f) { amount = free.stripTrailingZeros().toPlainString() }, style = lb(FontSize.label, FontWeight.Medium), color = p.brand)
            }
            Spacer(Modifier.height(8.dp))
            AssetKv("可用", "${Fmt.amount(free, 8).trimEnd('0').trimEnd('.').ifEmpty { "0" }} $asset")
            if (tab == 0) {
                AssetKv("单笔最小提现", "${minS ?: "--"} $asset")
                AssetKv("单笔最大提现", "${maxS ?: "不限"} $asset")
                AssetKv("链上网络费（Gas）", "${feeS ?: "--"} ${net?.gasFeeAsset ?: asset}")
                AssetKv("到账数量", "${receive ?: "--"} $asset")
                AssetKv("预计到账时间", eta)
            } else {
                AssetKv("手续费", "免费")
                AssetKv("预计到账", "实时到账")
            }
            Spacer(Modifier.height(12.dp))
            Text("* 请谨防诈骗！Linbit 不会以任何形式索要账户密码，也不会要求私下转账或提现。", style = lb(11.sp), color = p.textTertiary)
            Spacer(Modifier.height(24.dp))
        }
        val canSubmit = if (tab == 0) {
            addr.length >= 10 && value.signum() > 0 && amtError() == null && net != null && (!memoReq || memo.isNotBlank())
        } else {
            to.isNotBlank() && value.signum() > 0 && amtError() == null
        }
        Row(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("预计到账数量", style = lb(FontSize.small), color = p.textSecondary)
                Text(
                    if (tab == 0) "${receive ?: "0"} $asset" else "${if (value.signum() > 0) amount else "0"} $asset",
                    style = lbNum(FontSize.body, FontWeight.Bold),
                    color = p.textPrimary,
                )
            }
            LbButton(if (tab == 0) "提现" else "转账", Modifier.width(140.dp), enabled = canSubmit, testTag = "withdraw.review") { showConfirm = true }
        }
    }

    if (showNets) {
        WithdrawNetworkSheet(
            asset = asset,
            nets = nets,
            wallet = walletNets,
            selected = net?.network,
            onPick = { n ->
                net = n
                walletNet = walletNets.firstOrNull { it.network == n.network }
                showNets = false
            },
            onClose = { showNets = false },
        )
    }
    if (showConfirm) {
        LbSheet("安全验证", onClose = { if (!submitting) showConfirm = false }, showClose = !submitting, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
                Text(
                    if (tab == 0) "提现 $amount $asset（${AssetsLogic.displayNetwork(net?.network)}），扣除 Gas ${feeS ?: "--"}，到账 ${receive ?: "--"}"
                    else "站内转账 $amount $asset 至 $to，免手续费，实时到账",
                    style = lb(FontSize.small),
                    color = p.textSecondary,
                )
                Spacer(Modifier.height(12.dp))
                if (twoFa) {
                    LbTextField(totp, { totp = it.filter(Char::isDigit).take(6) }, "谷歌验证器动态码", keyboardType = KeyboardType.Number)
                } else {
                    Text("未开启 2FA：发送 purpose=withdraw 验证码到绑定邮箱/手机", style = lb(FontSize.small), color = p.textSecondary)
                    Spacer(Modifier.height(8.dp))
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        LbTextField(
                            smsCode,
                            { smsCode = it.filter(Char::isDigit).take(8) },
                            "邮箱/手机验证码",
                            Modifier.weight(1f),
                            keyboardType = KeyboardType.Number,
                        )
                        Spacer(Modifier.width(8.dp))
                        LbButton("发送", Modifier.width(88.dp), loading = sendingCode, enabled = !sendingCode && !submitting) {
                            scope.launch {
                                sendingCode = true
                                vm.sendWithdrawCode().fold(
                                    onSuccess = { vm.toast.show("验证码已发送") },
                                    onFailure = { e -> vm.toast.show(e.lbMessage()) },
                                )
                                sendingCode = false
                            }
                        }
                    }
                }
                Spacer(Modifier.height(8.dp))
                LbTextField(fundPw, { fundPw = it.filter(Char::isDigit).take(6) }, "资金密码（若已设置）", secure = true, keyboardType = KeyboardType.NumberPassword)
                Spacer(Modifier.height(12.dp))
                val verifyOk = AssetsLogic.withdrawVerifyReady(twoFa, totp, smsCode)
                LbButton("确认提交", loading = submitting, enabled = verifyOk && !submitting) {
                    if (submitting) return@LbButton
                    scope.launch {
                        submitting = true
                        val auth = AssetsLogic.withdrawAuthCodes(twoFa, totp, smsCode)
                        val result = if (tab == 0) {
                            vm.submitWithdraw(
                                ApiWithdrawRequest(
                                    asset = asset,
                                    network = net!!.network,
                                    address = addr,
                                    amount = amount,
                                    memo = memo.ifBlank { null },
                                    totpCode = auth.totpCode,
                                    code = auth.code,
                                    fundPassword = fundPw.ifBlank { null },
                                ),
                            )
                        } else {
                            val isEmail = to.contains("@")
                            vm.submitInternal(
                                ApiInternalTransferRequest(
                                    clientId = AssetsLogic.newClientId(),
                                    asset = asset,
                                    amount = amount,
                                    totpCode = auth.totpCode ?: "",
                                    code = auth.code,
                                    toUid = if (isEmail) null else to,
                                    toEmail = if (isEmail) to else null,
                                    fundPassword = fundPw.ifBlank { null },
                                ),
                            )
                        }
                        result.fold(
                            onSuccess = {
                                vm.toast.show(if (tab == 0) "提现申请已提交" else "转账成功")
                                showConfirm = false
                                amount = ""; addr = ""; memo = ""; totp = ""; smsCode = ""; fundPw = ""
                                vm.load(silent = true)
                                assetsNav.open(AssetsRoute.WithdrawRecords)
                            },
                            onFailure = { e -> vm.toast.show(e.lbMessage()) },
                        )
                        submitting = false
                    }
                }
            }
        }
    }

}

/** 提现网络：显示手续费 + 预计到账；推荐来自 /wallet/networks。不标「最低」以免与充值规则混淆时可按产品再加。 */
@Composable
fun WithdrawNetworkSheet(
    asset: String,
    nets: List<ApiWithdrawNetwork>,
    wallet: List<ApiWalletNetwork>,
    selected: String?,
    onPick: (ApiWithdrawNetwork) -> Unit,
    onClose: () -> Unit,
) {
    val p = LB.p
    val recommended = wallet.filter { it.recommended == true }.mapNotNull { it.network }.toSet()
    LbSheet("选择网络", onClose = onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            Text("请确保您选择的网络与收款地址使用的网络一致，否则可能导致资产丢失。", style = lb(FontSize.small), color = p.textSecondary)
            Spacer(Modifier.height(10.dp))
            if (nets.isEmpty()) EmptyState("暂无可用网络", Modifier.height(200.dp))
            nets.forEach { n ->
                val sel = n.network == selected
                val fee = AssetsLogic.positiveAmount(n.gasFeeEstimate) ?: "0"
                Column(
                    Modifier
                        .fillMaxWidth()
                        .padding(bottom = 10.dp)
                        .border(if (sel) 1.5.dp else 1.dp, if (sel) p.textPrimary else p.border, RoundedCornerShape(Radius.xl))
                        .pressable(onClick = { onPick(n) })
                        .padding(14.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(AssetsLogic.displayNetwork(n.network), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                        if (n.network in recommended) {
                            Spacer(Modifier.width(6.dp))
                            RecommendedChip()
                        }
                    }
                    Spacer(Modifier.height(6.dp))
                    Text("手续费 $fee ${n.gasFeeAsset ?: asset}", style = lbNum(FontSize.small), color = p.textSecondary)
                    Text(AssetsLogic.etaText(n.estimatedArrivalMinutes), style = lb(FontSize.small), color = p.textSecondary)
                    AssetsLogic.positiveAmount(n.minWithdraw)?.let {
                        Text("最小提现 $it $asset", style = lb(FontSize.small), color = p.textSecondary)
                    }
                }
            }
        }
    }
}
