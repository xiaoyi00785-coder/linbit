package click.cowa.linbit.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.feature.auth.AuthFlow
import click.cowa.linbit.feature.home.HomeScreen
import click.cowa.linbit.feature.markets.MarketsScreen
import click.cowa.linbit.feature.assets.AssetsHost
import click.cowa.linbit.feature.assets.AssetsScreen
import click.cowa.linbit.feature.profile.UserCenterHost
import click.cowa.linbit.feature.more.MoreHost
import click.cowa.linbit.feature.more.MorePage
import click.cowa.linbit.feature.more.NotificationBadgeViewModel
import click.cowa.linbit.feature.assets.AssetFeatureHost
import click.cowa.linbit.feature.more.MoreMenuScreen
import click.cowa.linbit.ui.components.FloatingTabBar
import click.cowa.linbit.ui.components.MainTab
import click.cowa.linbit.ui.components.ToastOverlay
import click.cowa.linbit.ui.splash.SplashOverlay
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LinbitTheme
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.resolveDark

/** 根：主题 → 5 个 Tab（悬浮胶囊 TabBar）→ 全屏覆盖层（登录/注册、个人中心）→ Toast → 启动动画。 */
@Composable
fun AppRoot(vm: AppViewModel = hiltViewModel()) {
    val mode by vm.themeMode.collectAsStateWithLifecycle()
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val spotSymbol by vm.tradeNav.spotSymbol.collectAsStateWithLifecycle()
    val futuresSymbol by vm.tradeNav.futuresSymbol.collectAsStateWithLifecycle()
    val dark = resolveDark(mode)
    var tab by rememberSaveable { mutableStateOf(MainTab.HOME) }
    var auth by rememberSaveable { mutableStateOf<String?>(null) } // null | "login" | "register"
    var userCenter by rememberSaveable { mutableStateOf(false) }
    var splash by rememberSaveable { mutableStateOf(true) }
    var historyKind by rememberSaveable { mutableStateOf<String?>(null) } // null | "spot" | "futures"
    var morePage by rememberSaveable { mutableStateOf<MorePage?>(null) }
    var assetFeature by rememberSaveable { mutableStateOf<String?>(null) }
    var moreMenu by rememberSaveable { mutableStateOf(false) }
    val badgeVm: NotificationBadgeViewModel = hiltViewModel()
    val unread by badgeVm.unread.collectAsStateWithLifecycle()
    fun openFeature(title: String) {
        val page = when (title) {
            "福利中心" -> MorePage.WELFARE
            "跟单" -> MorePage.COPY
            "TradFi" -> MorePage.TRADFI
            "卡券中心" -> MorePage.COUPONS
            "合伙人计划" -> MorePage.PARTNER
            else -> null
        }
        if (page != null) morePage = page else vm.toast.show("$title 即将开放")
    }

    LinbitTheme(dark) {
        val view = LocalView.current
        LaunchedEffect(dark) {
            WindowCompat.getInsetsController((view.context as android.app.Activity).window, view).isAppearanceLightStatusBars = !dark
        }
        LaunchedEffect(Unit) {
            vm.expired.collect { vm.toast.show("登录已过期，请重新登录"); userCenter = false; auth = "login" }
        }
        // 登录成功后自动收起
        LaunchedEffect(loggedIn) { if (loggedIn) auth = null }

        val p = LB.p
        // 内容区底部留白 = TabBar 高 + 间距，滚动内容可滑到胶囊下方
        val bottomInset = Metrics.tabBarH + 24.dp // 空态操作与悬浮栏安全间距（对照 ref）
        Box(Modifier.fillMaxSize().background(p.bg)) {
            Box(Modifier.fillMaxSize().statusBarsPadding()) {
                when (tab) {
                    MainTab.HOME -> HomeScreen(
                        bottomInset, onOpenAvatar = { if (loggedIn) userCenter = true else auth = "login" },
                        onOpenMarkets = { tab = MainTab.MARKETS }, onOpenMore = { moreMenu = true },
                        onSoon = { vm.toast.show(it) }, onDeposit = { if (loggedIn) vm.assetsNav.openDeposit(switchTab = true) else auth = "login" },
                        onFeature = ::openFeature,
                        onNotifications = { morePage = MorePage.NOTIFICATIONS },
                        onSupport = { if (loggedIn) morePage = MorePage.SUPPORT else auth = "login" },
                        unread = unread,
                    )
                    MainTab.MARKETS -> MarketsScreen(bottomInset, onOpenTrade = { kind, sym ->
                        if (kind == click.cowa.linbit.data.MarketKind.SPOT) { vm.tradeNav.setSpot(sym); tab = MainTab.SPOT } else { vm.tradeNav.setFutures(sym); tab = MainTab.FUTURES }
                    })
                    MainTab.FUTURES -> click.cowa.linbit.feature.trade.FuturesRootScreen(
                        bottomInset, mode, vm::setTheme, onLogin = { auth = "login" },
                        onDeposit = { vm.assetsNav.openDeposit(switchTab = true) }, onTransfer = { vm.assetsNav.openTransfer(switchTab = true) }, onHistory = { historyKind = "futures" },
                        onTrade = { futures, symbol ->
                            if (futures) { vm.tradeNav.setFutures(symbol); /* 留在合约 Tab，由 FuturesRoot 切回「合约」 */ }
                            else { vm.tradeNav.setSpot(symbol); tab = MainTab.SPOT }
                        },
                        onCopy = { if (loggedIn) morePage = MorePage.COPY else auth = "login" },
                    )
                    MainTab.SPOT -> click.cowa.linbit.feature.trade.SpotTradeScreen(
                        bottomInset, mode, vm::setTheme, onLogin = { auth = "login" },
                        onDeposit = { vm.assetsNav.openDeposit(switchTab = true) }, onTransfer = { vm.assetsNav.openTransfer(switchTab = true) }, onHistory = { historyKind = "spot" }
                    )
                    MainTab.ASSETS -> AssetsScreen(
                        bottomInset,
                        onLogin = { auth = "login" },
                        assetsNav = vm.assetsNav,
                        onOpenFutures = { tab = MainTab.FUTURES },
                        onOpenSpot = { tab = MainTab.SPOT },
                        onOpenProfile = { if (loggedIn) userCenter = true else auth = "login" },
                        onOpenAssetFeature = { feature -> assetFeature = feature; vm.assetsNav.clear() },
                    )
                }
            }
            FloatingTabBar(tab, { tab = it }, Modifier.align(Alignment.BottomCenter))

            // 首页/交易页打开充值或划转时切到资产 Tab
            val assetsRoute by vm.assetsNav.route.collectAsStateWithLifecycle()
            LaunchedEffect(assetsRoute) {
                if (assetsRoute != null && vm.assetsNav.consumeTab()) tab = MainTab.ASSETS
            }
            if (assetsRoute != null) AssetsHost(vm.assetsNav)

            assetFeature?.let { feature -> AssetFeatureHost(feature,
                onBack = { assetFeature = null },
                onLogin = { assetFeature = null; auth = "login" },
                onTrade = { futures, symbol ->
                    if (futures) { vm.tradeNav.setFutures(symbol); tab = MainTab.FUTURES } else { vm.tradeNav.setSpot(symbol); tab = MainTab.SPOT }
                    assetFeature = null
                },
            ) }

            if (userCenter && loggedIn) UserCenterHost(
                mode, vm::setTheme,
                onLogout = { vm.logout { userCenter = false } },
                onClose = { userCenter = false },
                onLoginOther = { userCenter = false; auth = "login" },
                onFeature = { page -> userCenter = false; morePage = page },
            )
            if (moreMenu) MoreMenuScreen(onBack = { moreMenu = false }, onOpen = { moreMenu = false; morePage = it })
            morePage?.let { page -> MoreHost(
                page,
                onClose = { morePage = null; badgeVm.refresh() },
                onLogin = { morePage = null; auth = "login" },
                onOpen = { morePage = it },
                onTrade = { futures, symbol ->
                    if (futures) { vm.tradeNav.setFutures(symbol); tab = MainTab.FUTURES }
                    else { vm.tradeNav.setSpot(symbol); tab = MainTab.SPOT }
                    morePage = null
                },
                onProfile = { morePage = null; userCenter = true },
                onDeposit = { morePage = null; vm.assetsNav.openDeposit(switchTab = true) },
            ) }
            when (historyKind) {
                "spot" -> click.cowa.linbit.feature.trade.SpotHistoryScreen(onClose = { historyKind = null }, onLogin = { historyKind = null; auth = "login" }, symbol = spotSymbol)
                "futures" -> click.cowa.linbit.feature.trade.FuturesHistoryScreen(onClose = { historyKind = null }, onLogin = { historyKind = null; auth = "login" }, symbol = futuresSymbol)
            }
            auth?.let { start -> AuthFlow(startRegister = start == "register", onClose = { auth = null }) }
            ToastOverlay(vm.toast)
            if (splash) SplashOverlay(reduceMotion = false, onFinish = { splash = false })
        }
    }
}
