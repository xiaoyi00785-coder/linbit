package click.cowa.linbit.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
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
import click.cowa.linbit.feature.placeholder.PlaceholderScreen
import click.cowa.linbit.feature.user.UserCenterScreen
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
    val dark = resolveDark(mode)
    var tab by rememberSaveable { mutableStateOf(MainTab.HOME) }
    var auth by rememberSaveable { mutableStateOf<String?>(null) } // null | "login" | "register"
    var userCenter by rememberSaveable { mutableStateOf(false) }
    var splash by rememberSaveable { mutableStateOf(true) }
    var historyKind by rememberSaveable { mutableStateOf<String?>(null) } // null | "spot" | "futures"

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
        val bottomInset = Metrics.tabBarH + 12.dp
        Box(Modifier.fillMaxSize().background(p.bg)) {
            Box(Modifier.fillMaxSize().statusBarsPadding()) {
                when (tab) {
                    MainTab.HOME -> HomeScreen(
                        bottomInset, onOpenAvatar = { if (loggedIn) userCenter = true else auth = "login" },
                        onOpenMarkets = { tab = MainTab.MARKETS }, onOpenMore = { vm.toast.show("全部功能即将开放") },
                        onSoon = { vm.toast.show(it) }, onDeposit = { if (loggedIn) vm.toast.show("充值即将开放") else auth = "login" },
                    )
                    MainTab.MARKETS -> MarketsScreen(bottomInset, onOpenTrade = { kind, sym ->
                        if (kind == click.cowa.linbit.data.MarketKind.SPOT) { vm.tradeNav.setSpot(sym); tab = MainTab.SPOT } else { vm.tradeNav.setFutures(sym); tab = MainTab.FUTURES }
                    })
                    MainTab.FUTURES -> click.cowa.linbit.feature.trade.FuturesRootScreen(
                        bottomInset, mode, vm::setTheme, onLogin = { auth = "login" },
                        onDeposit = { vm.toast.show("充值即将开放") }, onTransfer = { vm.toast.show("划转即将开放") }, onHistory = { historyKind = "futures" },
                    )
                    MainTab.SPOT -> click.cowa.linbit.feature.trade.SpotTradeScreen(
                        bottomInset, mode, vm::setTheme, onLogin = { auth = "login" },
                        onDeposit = { vm.toast.show("充值即将开放") }, onTransfer = { vm.toast.show("划转即将开放") }, onHistory = { historyKind = "spot" },
                    )
                    MainTab.ASSETS -> PlaceholderScreen("资产", bottomInset, loginHint = !loggedIn, onLogin = { auth = "login" })
                }
            }
            FloatingTabBar(tab, { tab = it }, Modifier.align(Alignment.BottomCenter))

            if (userCenter && loggedIn) UserCenterScreen(mode, vm::setTheme, onLogout = { vm.logout { userCenter = false } }, onClose = { userCenter = false })
            when (historyKind) {
                "spot" -> click.cowa.linbit.feature.trade.SpotHistoryScreen(onClose = { historyKind = null }, onLogin = { historyKind = null; auth = "login" }, symbol = vm.tradeNav.spotSymbol.value)
                "futures" -> click.cowa.linbit.feature.trade.FuturesHistoryScreen(onClose = { historyKind = null }, onLogin = { historyKind = null; auth = "login" }, symbol = vm.tradeNav.futuresSymbol.value)
            }
            auth?.let { start -> AuthFlow(startRegister = start == "register", onClose = { auth = null }) }
            ToastOverlay(vm.toast)
            if (splash) SplashOverlay(reduceMotion = false, onFinish = { splash = false })
        }
    }
}
