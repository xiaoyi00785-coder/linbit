package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.KvRow
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.NumberBox
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal

/**
 * 理财产品入口页（对照 ref-capture/auth/028_earn_list、029_earn_btc_detail）。
 * 主入口为底部「现货」顶栏「理财」（对照 028）；首页/个人中心「理财」深链到该顶栏。
 * 资产页「理财」tab 仍是账户视图。仅活期/定期（API kind）；链上/财富/借贷无契约不造入口。
 * 分类图标紧凑横排，有几种显示几种，绝不把单图标拉成全宽大卡。不写假 APR/余额。
 */
@Composable
fun EarnSubpage(
    bottomInset: androidx.compose.ui.unit.Dp,
    onLogin: () -> Unit,
    vm: SpotSubpagesViewModel = hiltViewModel(),
    onBack: (() -> Unit)? = null,
    onDialogWindow: (android.view.Window?) -> Unit = {},
) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    var earnAction by remember { mutableStateOf<EarnAction?>(null) }
    var hideBal by remember { mutableStateOf(false) }
    var hubFilter by remember { mutableStateOf<EarnHubFilter?>(null) }
    var query by remember { mutableStateOf("") }
    var expanded by remember { mutableStateOf<Set<String>>(emptySet()) }
    var showPositions by remember { mutableStateOf(false) }
    LaunchedEffect(sessionVersion) { vm.clearAccountData(); vm.loadEarn() }

    val products = vm.earnProducts
    val filtered = remember(products, hubFilter, query) {
        products.asSequence()
            .filter { earnMatchesHub(it, hubFilter) }
            .filter {
                val q = query.trim()
                q.isEmpty() || (it.asset?.contains(q, true) == true) || (it.title?.contains(q, true) == true)
            }
            .toList()
    }
    val byAsset = remember(filtered) {
        filtered.groupBy { it.asset ?: "--" }.toList().sortedBy { it.first }
    }
    // 对照 028：资产行默认折叠，点开再看产品；不自动展开拉稀密度。
    // 精选：仅 hot/newcomer；不得因 bonusApr 把整表活期塞进推荐并伪标「热销」。
    val featured = remember(products) {
        products.filter { it.hot == true || it.newcomer == true }
            .sortedBy { it.sort ?: Int.MAX_VALUE }
            .ifEmpty { products.sortedByDescending { Dec.parse(it.aprMax ?: it.apr) }.take(6) }
            .take(8)
    }
    val hasSimple = products.any { it.kind == ApiEarnProduct.Kind.SIMPLE || it.kind == ApiEarnProduct.Kind.FLEXIBLE }
    val hasFixed = products.any { it.kind == ApiEarnProduct.Kind.FIXED }
    val hasOnchain = products.any { it.kind == ApiEarnProduct.Kind.ONCHAIN }

    Column(Modifier.fillMaxSize().testTag("earn.hub")) {
        // 参考为现货顶栏内嵌，无独立「理财」导航栏；仅深链残留 onBack 时给返回，不加标题栏占位。
        onBack?.let { back ->
            Row(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(40.dp).pressable(scale = 1f, onClick = back), contentAlignment = Alignment.CenterStart) {
                    LbIcon(R.drawable.ic_arrow_left, 22.dp, p.textPrimary)
                }
            }
        }
        Column(
            Modifier.weight(1f).imePadding().verticalScroll(rememberScrollState())
                .padding(bottom = bottomInset + 16.dp),
        ) {
            EarnAssetHead(
                summary = vm.earnSummary,
                hide = hideBal,
                loggedIn = loggedIn,
                onToggle = { hideBal = !hideBal },
                onPositions = {
                    if (!loggedIn) onLogin() else showPositions = true
                },
                onLogin = onLogin,
            )

            // 对照 028：Simple Earn / On-chain；定期在列表内展示。Wealth Hub 无契约；Loans 有 /loans/* 但 Android 尚未接客户端 → 不造无效入口。
            if (hasSimple || hasOnchain || hasFixed) {
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 16.dp),
                    horizontalArrangement = Arrangement.Start,
                    verticalAlignment = Alignment.Top,
                ) {
                    if (hasSimple) EarnKindChip(
                        title = "简单赚币",
                        icon = R.drawable.ic_g_earn_light,
                        selected = hubFilter == EarnHubFilter.SIMPLE,
                    ) { hubFilter = if (hubFilter == EarnHubFilter.SIMPLE) null else EarnHubFilter.SIMPLE }
                    if (hasOnchain) EarnKindChip(
                        title = "链上赚币",
                        icon = R.drawable.ic_g_earn_dark,
                        selected = hubFilter == EarnHubFilter.ONCHAIN,
                    ) { hubFilter = if (hubFilter == EarnHubFilter.ONCHAIN) null else EarnHubFilter.ONCHAIN }
                    if (hasFixed) EarnKindChip(
                        title = "理财宝",
                        icon = R.drawable.ic_g_earn_light,
                        selected = hubFilter == EarnHubFilter.FIXED,
                    ) { hubFilter = if (hubFilter == EarnHubFilter.FIXED) null else EarnHubFilter.FIXED }
                }
            }

            if (featured.isNotEmpty()) {
                Text(
                    "精选推荐",
                    Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp, bottom = 10.dp),
                    style = lb(FontSize.label, FontWeight.SemiBold),
                    color = p.textPrimary,
                )
                Row(
                    Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    featured.forEach { prod ->
                        EarnFeatureCard(prod) {
                            if (!loggedIn) onLogin() else earnAction = EarnAction(prod, subscribe = true)
                        }
                    }
                }
            }

            Text(
                "全部产品",
                Modifier.padding(horizontal = Spacing.pageH).padding(top = 18.dp, bottom = 8.dp)
                    .testTag("earn.products"),
                style = lb(FontSize.label, FontWeight.SemiBold),
                color = p.textPrimary,
            )
            Row(
                Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 4.dp)
                    .height(40.dp).background(p.fill, RoundedCornerShape(Radius.lg))
                    .padding(horizontal = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                LbIcon(R.drawable.ic_search, 16.dp, p.textTertiary)
                Spacer(Modifier.width(8.dp))
                BasicTextField(
                    value = query,
                    onValueChange = { query = it },
                    singleLine = true,
                    textStyle = lb(FontSize.body).copy(color = p.textPrimary),
                    modifier = Modifier.weight(1f).testTag("earn.search"),
                    decorationBox = { inner ->
                        if (query.isEmpty()) Text("搜索币种", style = lb(FontSize.body), color = p.textTertiary)
                        inner()
                    },
                )
            }
            when {
                vm.loading && products.isEmpty() -> Box(
                    Modifier.fillMaxWidth().height(120.dp),
                    contentAlignment = Alignment.Center,
                ) { CircularProgressIndicator() }
                vm.error != null && products.isEmpty() -> EmptyState(vm.error ?: "") {
                    ChipButton("重试", Modifier.height(32.dp)) { vm.loadEarn() }
                }
                filtered.isEmpty() -> EmptyState(if (products.isEmpty()) "暂无理财产品" else "无匹配产品")
                else -> byAsset.forEach { (asset, list) ->
                    val open = asset in expanded
                    val aprLo = list.minOf { Dec.parse(it.apr) }
                    val aprHi = list.maxOf { Dec.parse(it.aprMax ?: it.bonusApr ?: it.apr) }
                    Column(Modifier.fillMaxWidth()) {
                        Row(
                            Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH)
                                .pressable(scale = 1f) {
                                    expanded = if (open) expanded - asset else expanded + asset
                                }.testTag("earn.asset.$asset"),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            CoinIcon(asset, 28.dp)
                            Text(
                                asset,
                                Modifier.padding(start = 12.dp).weight(1f),
                                style = lb(FontSize.body, FontWeight.Medium),
                                color = p.textPrimary,
                            )
                            Text(
                                if (aprLo.compareTo(aprHi) == 0) "${earnAprPct(aprHi)}%"
                                else "${earnAprPct(aprLo)}% ~ ${earnAprPct(aprHi)}%",
                                style = lbNum(FontSize.body, FontWeight.SemiBold),
                                color = p.up,
                            )
                            LbIcon(
                                if (open) R.drawable.ic_chevron_down else R.drawable.ic_chevron_right,
                                14.dp,
                                p.textSecondary,
                                Modifier.padding(start = 6.dp),
                            )
                        }
                        if (open) list.forEach { prod ->
                            EarnProductRow(
                                prod = prod,
                                loading = vm.loading,
                                loggedIn = loggedIn,
                                onLogin = onLogin,
                                onSubscribe = { earnAction = EarnAction(prod, subscribe = true) },
                                onRedeem = { earnAction = EarnAction(prod, subscribe = false) },
                            )
                        }
                    }
                }
            }
        }
    }

    if (showPositions) {
        LbSheet("我的持仓", { showPositions = false }, showClose = true, onWindowReady = onDialogWindow) {
            Column(
                Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (vm.earnPositions.isEmpty() && vm.earnOrders.isEmpty() && vm.earnRecords.isEmpty()) {
                    EmptyState("暂无持仓与流水", verticalPadding = 40.dp)
                }
                vm.earnPositions.forEach { pos ->
                    Column(
                        Modifier.fillMaxWidth().border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Text(pos.title ?: pos.asset ?: "--", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                        KvRow("持仓", "${pos.amount ?: "--"} ${pos.asset ?: ""}")
                        KvRow("累计利息", pos.totalInterest ?: "--")
                        if (pos.kind?.contains("flex", ignoreCase = true) == true || pos.auto != null) {
                            val on = pos.auto == true
                            ChipButton(if (on) "自动申购：开" else "自动申购：关", Modifier.height(28.dp)) {
                                pos.productId?.let { vm.setEarnAuto(it, enabled = !on) }
                            }
                        }
                    }
                }
                if (vm.earnOrders.isNotEmpty()) {
                    Text("定期订单", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                    vm.earnOrders.take(20).forEach { o ->
                        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Text("${o.title ?: o.asset ?: "--"} · ${o.status ?: ""}", style = lb(FontSize.small), color = p.textPrimary)
                            KvRow("金额", "${o.amount ?: "--"} ${o.asset ?: ""}")
                            KvRow("利息", o.interest ?: "--")
                        }
                    }
                }
                if (vm.earnRecords.isNotEmpty()) {
                    Text("理财流水", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                    vm.earnRecords.take(30).forEach { r ->
                        KvRow("${r.type?.value ?: "--"} · ${r.asset ?: ""}", r.amount ?: "--")
                    }
                }
            }
        }
    }

    earnAction?.let { act ->
        EarnActionSheet(
            act = act,
            vm = vm,
            onDialogWindow = onDialogWindow,
            onDismiss = { if (!vm.acting) earnAction = null },
            onDone = { earnAction = null },
        )
    }
}

@Composable
private fun EarnAssetHead(
    summary: click.cowa.linbit.core.generated.ApiEarnSummary200Response?,
    hide: Boolean,
    loggedIn: Boolean,
    onToggle: () -> Unit,
    onPositions: () -> Unit,
    onLogin: () -> Unit,
) {
    val p = LB.p
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("理财资产(USDT)", style = lb(FontSize.caption), color = p.textSecondary)
            Spacer(Modifier.width(6.dp))
            LbIcon(
                if (hide) R.drawable.ic_eye_off else R.drawable.ic_eye,
                14.dp,
                p.textSecondary,
                Modifier.pressable(scale = 1f, onClick = onToggle).testTag("earn.hide"),
            )
            Spacer(Modifier.weight(1f))
            Text(
                "我的持仓",
                Modifier.pressable(scale = 1f, onClick = onPositions).testTag("earn.positions"),
                style = lb(FontSize.caption),
                color = p.textSecondary,
            )
            LbIcon(R.drawable.ic_chevron_right, 12.dp, p.textTertiary)
        }
        Spacer(Modifier.height(6.dp))
        when {
            !loggedIn -> EmptyState("登录后查看理财资产") {
                ChipButton("登录 / 注册", Modifier.height(36.dp), onClick = onLogin)
            }
            else -> {
                val total = Dec.parse(summary?.totalValueUsdt)
                val recent = Dec.parse(summary?.recentInterestUsdt)
                Row(verticalAlignment = Alignment.Bottom) {
                    Text(
                        if (hide) "****" else Fmt.amount(total, 2),
                        style = lbNum(32.sp, FontWeight.Bold),
                        color = p.textPrimary,
                    )
                    Spacer(Modifier.width(6.dp))
                    Text("USDT", Modifier.padding(bottom = 6.dp), style = lb(FontSize.small), color = p.textPrimary)
                }
                val recentTxt = if (hide) "****" else {
                    val sign = if (recent.signum() >= 0) "+" else ""
                    "$sign${Fmt.amount(recent, 2)} USDT"
                }
                Text(
                    "昨日收益 $recentTxt",
                    Modifier.padding(top = 6.dp),
                    style = lbNum(FontSize.small),
                    color = if (!hide && recent.signum() < 0) p.down else p.up,
                )
            }
        }
    }
}

@Composable
private fun EarnKindChip(title: String, icon: Int, selected: Boolean, onClick: () -> Unit) {
    val p = LB.p
    // 对照 028 四宫格单格宽约屏宽/4；仅渲染已支持种类，绝不把单图标拉成全宽大卡。
    Column(
        Modifier.width(78.dp).pressable(scale = 1f, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            Modifier.size(48.dp).clip(CircleShape)
                .background(if (selected) p.brand.copy(alpha = 0.16f) else p.fill)
                .border(1.dp, if (selected) p.brand else p.border.copy(alpha = 0f), CircleShape),
            contentAlignment = Alignment.Center,
        ) { LbIcon(icon, 22.dp, p.textPrimary) }
        Spacer(Modifier.height(6.dp))
        Text(title, style = lb(11.sp), color = p.textPrimary, maxLines = 1)
    }
}

@Composable
private fun EarnFeatureCard(prod: ApiEarnProduct, onClick: () -> Unit) {
    val p = LB.p
    val asset = prod.asset ?: "--"
    val tag = earnFeatureTag(prod)
    Column(
        Modifier.width(126.dp).height(100.dp)
            .background(p.fill, RoundedCornerShape(Radius.md))
            .pressable(scale = 1f, onClick = onClick)
            .padding(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            CoinIcon(asset, 18.dp)
            Text(asset, Modifier.padding(start = 6.dp).weight(1f), style = lb(FontSize.small, FontWeight.Medium), color = p.textPrimary)
            Text(
                tag,
                Modifier.background(
                    if (tag == "热销") p.down.copy(alpha = 0.12f) else p.up.copy(alpha = 0.12f),
                    RoundedCornerShape(4.dp),
                ).padding(horizontal = 5.dp, vertical = 1.dp),
                style = lb(10.sp),
                color = if (tag == "热销") p.down else p.up,
            )
        }
        Spacer(Modifier.weight(1f))
        Text("${earnAprPct(Dec.parse(prod.aprMax ?: prod.bonusApr ?: prod.apr))}%", style = lbNum(FontSize.title, FontWeight.SemiBold), color = p.up)
        Text(
            earnKindSubtitle(prod),
            style = lb(FontSize.small),
            color = p.textSecondary,
        )
    }
}

@Composable
private fun EarnProductRow(
    prod: ApiEarnProduct,
    loading: Boolean,
    loggedIn: Boolean,
    onLogin: () -> Unit,
    onSubscribe: () -> Unit,
    onRedeem: () -> Unit,
) {
    val p = LB.p
    Column(
        Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 8.dp)
            .background(p.fill, RoundedCornerShape(Radius.md)).padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(prod.title ?: prod.asset ?: "--", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Text(
                    earnKindSubtitle(prod),
                    style = lb(FontSize.small),
                    color = p.textSecondary,
                )
            }
            Column(horizontalAlignment = Alignment.End) {
                Text("${earnAprPct(Dec.parse(prod.aprMax ?: prod.bonusApr ?: prod.apr))}%", style = lbNum(FontSize.body, FontWeight.SemiBold), color = p.up)
                Text("年化", style = lb(FontSize.small), color = p.textTertiary)
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ChipButton("申购", Modifier.height(28.dp).testTag("earn.subscribe"), enabled = true) {
                if (!loggedIn) onLogin() else onSubscribe()
            }
            ChipButton("赎回", Modifier.height(28.dp).testTag("earn.redeem"), enabled = true) {
                if (!loggedIn) onLogin() else onRedeem()
            }
        }
    }
}

@Composable
private fun EarnActionSheet(
    act: EarnAction,
    vm: SpotSubpagesViewModel,
    onDialogWindow: (android.view.Window?) -> Unit,
    onDismiss: () -> Unit,
    onDone: () -> Unit,
) {
    val p = LB.p
    var amount by remember(act.product.id) { mutableStateOf("") }
    var review by remember(act.product.id, act.subscribe) { mutableStateOf(false) }
    var all by remember(act.product.id) { mutableStateOf(false) }
    var validation by remember(act.product.id) { mutableStateOf<String?>(null) }
    var agreed by remember(act.product.id) { mutableStateOf(true) }
    val holding = remember(vm.earnPositions, act.product.id) { vm.earnPositions.firstOrNull { it.productId == act.product.id } }
    val asset = act.product.asset ?: "USDT"
    val title = "${if (act.subscribe) "申购" else "赎回"} ${act.product.title ?: asset}"
    fun validate(): String? {
        validation = if (act.subscribe) {
            val value = Dec.parse(amount); val min = Dec.parse(act.product.minAmount); val cap = Dec.parse(act.product.perUserMax)
            when {
                value.signum() <= 0 -> "请输入申购金额"
                min.signum() > 0 && value < min -> "不低于最小申购额 ${act.product.minAmount}"
                cap.signum() > 0 && value > cap -> "不超过单人上限 ${act.product.perUserMax}"
                !agreed -> "请先同意理财服务协议"
                else -> null
            }
        } else if (all) null else when {
            Dec.parse(amount).signum() <= 0 -> "请输入赎回金额"
            holding != null && Dec.parse(amount) > Dec.parse(holding.amount) -> "赎回金额不得超过当前持仓"
            else -> null
        }
        return validation
    }
    LbSheet(title, onDismiss, showClose = !vm.acting, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (!review) {
                if (act.subscribe && act.product.kind == ApiEarnProduct.Kind.FIXED) {
                    Text("期限", style = lb(FontSize.caption), color = p.textSecondary)
                    Row(
                        Modifier.fillMaxWidth().border(1.5.dp, p.brand, RoundedCornerShape(Radius.md)).padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text("${earnAprPct(Dec.parse(act.product.apr))}%", style = lbNum(FontSize.title, FontWeight.SemiBold), color = p.up)
                        Spacer(Modifier.weight(1f))
                        Text("${act.product.termDays ?: "--"} 天", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    }
                }
                NumberBox("金额", asset, amount, { amount = it; validation = null; review = false }, testTag = "earn.amount")
                validation?.let { EarnFailureText(it) }
                if (act.subscribe) {
                    KvRow("最低申购", "${act.product.minAmount ?: "--"} $asset")
                    KvRow("当前持仓", "${holding?.amount ?: "0"} $asset")
                    KvRow("预估年化", "${earnAprPct(Dec.parse(act.product.apr))}%")
                    Row(
                        Modifier.fillMaxWidth().pressable(scale = 1f) { agreed = !agreed }.testTag("earn.agree"),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            Modifier.size(18.dp).border(1.dp, if (agreed) p.brand else p.border, RoundedCornerShape(4.dp))
                                .background(if (agreed) p.brand else p.bg, RoundedCornerShape(4.dp)),
                            contentAlignment = Alignment.Center,
                        ) { if (agreed) Text("✓", style = lb(10.sp, FontWeight.Bold), color = androidx.compose.ui.graphics.Color.Black) }
                        Text(
                            "我已阅读并同意 Linbit 理财服务协议",
                            Modifier.padding(start = 8.dp),
                            style = lb(FontSize.small),
                            color = p.textSecondary,
                        )
                    }
                } else {
                    KvRow("当前持仓", "${holding?.amount ?: "0"} $asset")
                    LbButton(if (all) "✓ 已选择全部持仓" else "选择全部持仓", kind = ButtonKind.SECONDARY, enabled = !vm.acting) {
                        all = !all; amount = ""; validation = null; review = false
                    }
                }
                LbButton(
                    if (all) "下一步 · 全部赎回复核" else if (act.subscribe) "下一步 · 申购复核" else "下一步 · 赎回复核",
                    kind = ButtonKind.PRIMARY,
                    enabled = !vm.acting,
                ) { if (validate() == null) review = true }
            } else {
                Text(
                    if (act.subscribe) "申购最终复核" else if (all) "全部赎回最终复核" else "赎回最终复核",
                    style = lb(FontSize.title, FontWeight.SemiBold),
                    color = p.textPrimary,
                )
                KvRow("产品", act.product.title ?: asset)
                KvRow("资产", asset)
                if (act.subscribe) KvRow("年化收益", "${earnAprPct(Dec.parse(act.product.apr))}%")
                if (!act.subscribe) KvRow("当前持仓", "${holding?.amount ?: "0"} $asset")
                KvRow(
                    if (all) "赎回方式" else "金额",
                    if (all) "全部持仓 · ${holding?.amount ?: "0"} $asset"
                    else "${Dec.parse(amount).stripTrailingZeros().toPlainString()} $asset",
                )
                Text(
                    if (all) "将按服务端确认时的实际持仓全部赎回，请确认产品后继续。"
                    else "请核对产品与金额。请求成功后才会更新持仓。",
                    style = lb(FontSize.small),
                    color = p.textSecondary,
                )
                EarnFailureText(vm.actionFailure)
                LbButton(
                    if (vm.acting) "提交中…" else if (all) "确认全部赎回" else if (act.subscribe) "确认申购" else "确认赎回",
                    kind = ButtonKind.PRIMARY,
                    loading = false,
                    enabled = !vm.acting,
                ) {
                    if (!act.subscribe && vm.earnPositions.none { it.productId == act.product.id }) {
                        validation = "当前产品无可赎回持仓，请刷新后重试"; return@LbButton
                    }
                    val id = act.product.id
                    if (id == null) validation = "无效产品"
                    else if (act.subscribe) vm.subscribeEarn(id, Dec.parse(amount).stripTrailingZeros().toPlainString()) { ok -> if (ok) onDone() }
                    else vm.redeemEarn(id, if (all) null else Dec.parse(amount).stripTrailingZeros().toPlainString(), all) { ok -> if (ok) onDone() }
                }
                LbButton("返回修改", kind = ButtonKind.OUTLINE, enabled = !vm.acting) { review = false }
            }
        }
    }
}

@Composable
private fun EarnFailureText(value: String?) {
    value?.let { Text("操作未完成：$it", style = lb(FontSize.small), color = LB.p.down) }
}


private enum class EarnHubFilter { SIMPLE, FIXED, ONCHAIN }

private fun earnMatchesHub(p: ApiEarnProduct, filter: EarnHubFilter?): Boolean = when (filter) {
    null -> true
    EarnHubFilter.SIMPLE -> p.kind == ApiEarnProduct.Kind.SIMPLE || p.kind == ApiEarnProduct.Kind.FLEXIBLE
    EarnHubFilter.FIXED -> p.kind == ApiEarnProduct.Kind.FIXED
    EarnHubFilter.ONCHAIN -> p.kind == ApiEarnProduct.Kind.ONCHAIN
}

/** 标签只反映真实字段：hot→热销，newcomer→新手，bonusApr→加息；无活动契约不伪标 Flash Sale。 */
private fun earnFeatureTag(prod: ApiEarnProduct): String = when {
    prod.hot == true -> "热销"
    prod.newcomer == true -> "新手"
    !prod.bonusApr.isNullOrBlank() -> "加息"
    prod.kind == ApiEarnProduct.Kind.FIXED -> "定期"
    prod.kind == ApiEarnProduct.Kind.ONCHAIN -> "质押"
    else -> "活期"
}

private fun earnKindSubtitle(prod: ApiEarnProduct): String = when (prod.kind) {
    ApiEarnProduct.Kind.FIXED -> "定期 · ${prod.termDays ?: "--"}天"
    ApiEarnProduct.Kind.ONCHAIN -> if ((prod.unlockDays ?: 0) > 0) "链上质押 · ${prod.unlockDays}天解锁" else "链上质押"
    ApiEarnProduct.Kind.SIMPLE, ApiEarnProduct.Kind.FLEXIBLE -> "活期"
    null -> prod.kind?.value ?: ""
}

private data class EarnAction(val product: ApiEarnProduct, val subscribe: Boolean)

/** API apr 为十进制（0.052 → 5.20），与现有列表一致；不臆造。 */
private fun earnAprPct(raw: BigDecimal): String = Fmt.amount(raw.multiply(BigDecimal(100)), 2)
