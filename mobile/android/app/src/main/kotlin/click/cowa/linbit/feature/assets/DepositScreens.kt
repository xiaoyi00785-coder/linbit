package click.cowa.linbit.feature.assets

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
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
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiDepositAddress
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.core.util.Fmt
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

private val CoinNames = mapOf(
    "BTC" to "Bitcoin", "ETH" to "Ether", "USDT" to "Tether USDT", "BNB" to "BNB",
    "SOL" to "SOL", "TRX" to "TRON", "DOGE" to "DOGE", "USDC" to "USDC",
)

@Composable
fun CoinPickerScreen(
    mode: AssetsRoute.CoinPicker.Mode,
    vm: AssetsViewModel,
    onBack: () -> Unit,
    onPick: (String) -> Unit,
) {
    BackHandler(onBack = onBack)
    val p = LB.p
    var q by remember { mutableStateOf("") }
    val state by vm.state.collectAsStateWithLifecycle()
    val marketState by vm.marketState.collectAsStateWithLifecycle()
    val all = remember(state, marketState) { vm.coinUniverse() }
    val filtered = all.filter { q.isBlank() || it.contains(q, true) || (CoinNames[it] ?: "").contains(q, true) }
    val hot = listOf("BTC", "ETH", "USDT").filter { it in all }
    val groups = filtered.groupBy { it.first().uppercaseChar().toString() }.toSortedMap()

    Column(Modifier.fillMaxSize().background(p.bg)) {
        AssetsNavBar("选择币种", onBack = onBack)
        LbTextField(q, { q = it }, "搜索币种", Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), height = 36.dp)
        if (mode == AssetsRoute.CoinPicker.Mode.Withdraw) {
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 4.dp)) {
                Text("可提现资产", style = lb(FontSize.caption), color = p.textSecondary)
                Spacer(Modifier.weight(1f))
                Text("余额", style = lb(FontSize.caption), color = p.textSecondary)
            }
        }
        if (filtered.isEmpty()) EmptyState("暂无相关币种", Modifier.weight(1f))
        else {
            Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
                if (q.isBlank() && mode == AssetsRoute.CoinPicker.Mode.Deposit && hot.isNotEmpty()) {
                    Text("热门币种", Modifier.padding(horizontal = Spacing.pageH).padding(top = 14.dp, bottom = 4.dp), style = lb(FontSize.caption, FontWeight.SemiBold), color = p.textPrimary)
                    hot.forEach { CoinRow(it, mode, state, vm, onPick) }
                }
                groups.forEach { (k, list) ->
                    Text(k, Modifier.padding(horizontal = Spacing.pageH).padding(top = 14.dp, bottom = 4.dp), style = lb(FontSize.caption), color = p.textTertiary)
                    list.forEach { CoinRow(it, mode, state, vm, onPick) }
                }
            }
        }
    }
}

@Composable
private fun CoinRow(asset: String, mode: AssetsRoute.CoinPicker.Mode, state: AssetsUiState, vm: AssetsViewModel, onPick: (String) -> Unit) {
    val p = LB.p
    val bal = (state.spot + state.funding).firstOrNull { it.asset.equals(asset, true) }
    Row(
        Modifier.fillMaxWidth().height(54.dp).pressable(scale = 1f) { onPick(asset) }.padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CoinIcon(asset, 28.dp)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(asset, style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            Text(CoinNames[asset] ?: asset, style = lb(11.sp), color = p.textTertiary)
        }
        if (mode == AssetsRoute.CoinPicker.Mode.Withdraw || bal != null) {
            Column(horizontalAlignment = Alignment.End) {
                Text(bal?.total ?: "0.00", style = lbNum(FontSize.label), color = p.textPrimary)
                val rate = state.cnyRate
                if (rate != null && bal != null) {
                    Text("≈ " + Fmt.fiat(vm.usd(bal), rate, 2), style = lbNum(11.sp), color = p.textTertiary)
                }
            }
        }
    }
}

@Composable
fun DepositScreen(asset: String, vm: AssetsViewModel, onBack: () -> Unit, onChangeAsset: () -> Unit) {
    BackHandler(onBack = onBack)
    val p = LB.p
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var nets by remember { mutableStateOf<List<ApiWalletNetwork>?>(null) }
    var net by remember { mutableStateOf<ApiWalletNetwork?>(null) }
    var addr by remember { mutableStateOf<ApiDepositAddress?>(null) }
    var addrErr by remember { mutableStateOf<String?>(null) }
    var loadingAddr by remember { mutableStateOf(false) }
    var showNets by remember { mutableStateOf(false) }
    var showAccount by remember { mutableStateOf(false) }
    var account by remember { mutableStateOf("资金账户") }
    var loadingNets by remember { mutableStateOf(true) }

    fun loadAddr(n: ApiWalletNetwork) {
        scope.launch {
            loadingAddr = true; addrErr = null; addr = null
            vm.loadDepositAddress(asset, n.network!!).fold(
                onSuccess = { addr = it },
                onFailure = { addrErr = it.lbMessage() },
            )
            loadingAddr = false
        }
    }

    LaunchedEffect(asset) {
        loadingNets = true; nets = null; net = null; addr = null; addrErr = null
        vm.loadDepositNetworks(asset).fold(
            onSuccess = { list ->
                nets = list
                val def = AssetsLogic.defaultDepositNetwork(list)
                if (def != null) { net = def; loadAddr(def) }
                else if (list.isNotEmpty()) showNets = true
                else addrErr = "该币种暂不支持充值"
            },
            onFailure = { addrErr = it.lbMessage() },
        )
        loadingNets = false
    }

    Column(Modifier.fillMaxSize().background(p.bg).navigationBarsPadding().imePadding()) {
        AssetsNavBar("充值", onBack = onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(top = 8.dp).padding(bottom = 16.dp)) {
            Row(
                Modifier.fillMaxWidth().height(44.dp).background(p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f, onClick = onChangeAsset).padding(horizontal = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CoinIcon(asset, 22.dp)
                Spacer(Modifier.width(8.dp))
                Text(asset, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Text("▾", style = lb(FontSize.small), color = p.textSecondary)
            }
            Spacer(Modifier.height(16.dp))
            Box(
                Modifier.fillMaxWidth().height(190.dp),
                contentAlignment = Alignment.Center,
            ) {
                Box(
                    Modifier.size(190.dp).background(androidx.compose.ui.graphics.Color.White, RoundedCornerShape(Radius.md)),
                    contentAlignment = Alignment.Center,
                ) {
                    when {
                        loadingAddr || loadingNets -> CircularProgressIndicator(Modifier.size(28.dp), color = p.brand, strokeWidth = 2.dp)
                        addr != null -> {
                            QrImage(addr!!.address, 168.dp)
                            Box(Modifier.size(38.dp).background(androidx.compose.ui.graphics.Color.White, CircleShape), contentAlignment = Alignment.Center) {
                                CoinIcon(asset, 32.dp)
                            }
                        }
                        addrErr != null -> Text(addrErr ?: "", style = lb(FontSize.caption), color = p.down)
                        else -> Text("请选择网络", style = lb(FontSize.caption), color = p.textTertiary)
                    }
                }
            }
            Spacer(Modifier.height(16.dp))
            Column(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.xl)).padding(14.dp)) {
                Row(
                    Modifier.fillMaxWidth().pressable(scale = 1f) {
                        if (nets != null) showNets = true else scope.launch {
                            vm.loadDepositNetworks(asset).onSuccess { nets = it; showNets = true }
                        }
                    },
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text("网络", style = lb(11.sp), color = p.textSecondary)
                        Text(
                            net?.network?.let { AssetsLogic.displayNetwork(it) }
                                ?: if (loadingNets) "加载中…" else "请选择网络",
                            style = lb(FontSize.label, FontWeight.Medium),
                            color = p.textPrimary,
                        )
                    }
                    Text("⇄", style = lb(FontSize.body), color = p.textSecondary)
                }
                Spacer(Modifier.height(14.dp))
                Box(Modifier.fillMaxWidth().height(1.dp).background(p.divider))
                Spacer(Modifier.height(14.dp))
                Text("充值地址", style = lb(11.sp), color = p.textSecondary)
                Row(Modifier.padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(addr?.address ?: "--", Modifier.weight(1f), style = lbNum(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                    Spacer(Modifier.width(12.dp))
                    Box(
                        Modifier
                            .height(28.dp)
                            .background(p.primary, RoundedCornerShape(Radius.sm))
                            .pressable(enabled = addr != null, scale = 1f) {
                                copyText(ctx, addr?.address); vm.toast.show("已复制")
                            }
                            .padding(horizontal = 14.dp),
                        contentAlignment = Alignment.Center,
                    ) { Text("复制", style = lb(FontSize.small, FontWeight.Medium), color = p.textOnPrimary) }
                }
                val memo = addr?.memo
                if (!memo.isNullOrBlank()) {
                    Spacer(Modifier.height(8.dp))
                    Row {
                        Text("Memo  $memo", Modifier.weight(1f), style = lbNum(FontSize.caption), color = p.textPrimary)
                        Text("复制", Modifier.pressable(scale = 1f) { copyText(ctx, memo); vm.toast.show("已复制") }, style = lb(FontSize.small), color = p.brand)
                    }
                    Spacer(Modifier.height(6.dp))
                    WarnBanner("充值 $asset 时必须同时填写 Memo，否则资产将无法到账")
                }
            }
            Spacer(Modifier.height(16.dp))
            AssetKv("充值后自动划转至", account, Modifier.pressable(scale = 1f) { showAccount = true })
            Spacer(Modifier.height(12.dp))
            val minDep = AssetsLogic.positiveAmount(addr?.minDeposit ?: net?.minDeposit) ?: "--"
            AssetKv("最小充值", "$minDep $asset")
            Spacer(Modifier.height(12.dp))
            AssetKv("充值确认", net?.confirmations?.let { "${it}次网络确认" } ?: "--")
            Spacer(Modifier.height(12.dp))
            Text("* 请勿向上述地址充值 $asset 以外的资产，否则资产将不可找回", style = lb(11.sp), color = p.textTertiary)
            Spacer(Modifier.height(24.dp))
        }
        LbButton(
            "保存并分享地址",
            Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp),
            enabled = addr != null,
            onClick = {
                val a = addr ?: return@LbButton
                val send = Intent(Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(Intent.EXTRA_TEXT, "Linbit · $asset 充值地址（${AssetsLogic.displayNetwork(a.network)}）\n${a.address}")
                }
                ctx.startActivity(Intent.createChooser(send, "分享地址"))
            },
        )
    }

    if (showNets) {
        DepositNetworkSheet(
            nets = nets.orEmpty(),
            selected = net?.network,
            onPick = { n -> showNets = false; net = n; loadAddr(n) },
            onClose = { showNets = false },
        )
    }
    if (showAccount) {
        LbSheet("充值后自动划转至", onClose = { showAccount = false }) {
            listOf("资金账户", "现货账户").forEach { a ->
                click.cowa.linbit.ui.components.SheetOptionRow(a, a == account) { account = a; showAccount = false }
            }
            Spacer(Modifier.height(20.dp))
        }
    }
}

/** 充值网络：只显示推荐角标 + 预计到账时间；不显示手续费和「最低」。 */
@Composable
fun DepositNetworkSheet(
    nets: List<ApiWalletNetwork>,
    selected: String?,
    onPick: (ApiWalletNetwork) -> Unit,
    onClose: () -> Unit,
) {
    val p = LB.p
    LbSheet("选择网络", onClose = onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            Text("请确保您选择的网络与提币平台使用的网络一致，否则可能导致资产丢失。", style = lb(FontSize.small), color = p.textSecondary)
            Spacer(Modifier.height(10.dp))
            if (nets.isEmpty()) EmptyState("暂无可用网络", Modifier.height(200.dp))
            nets.forEach { n ->
                val code = n.network.orEmpty()
                val sel = code == selected
                Column(
                    Modifier
                        .fillMaxWidth()
                        .padding(bottom = 10.dp)
                        .border(if (sel) 1.5.dp else 1.dp, if (sel) p.textPrimary else p.border, RoundedCornerShape(Radius.xl))
                        .pressable(onClick = { onPick(n) })
                        .padding(14.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(AssetsLogic.displayNetwork(code), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                        if (n.recommended == true) {
                            Spacer(Modifier.width(6.dp))
                            RecommendedChip()
                        }
                    }
                    Spacer(Modifier.height(6.dp))
                    Text(AssetsLogic.etaText(n.estimatedArrivalMinutes), style = lb(FontSize.small), color = p.textSecondary)
                }
            }
        }
    }
}

fun copyText(ctx: Context, text: String?) {
    if (text.isNullOrBlank()) return
    val cm = ctx.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    cm.setPrimaryClip(ClipData.newPlainText("linbit", text))
}
