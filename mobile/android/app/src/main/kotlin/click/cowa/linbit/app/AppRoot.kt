package click.cowa.linbit.app

import androidx.compose.runtime.Composable
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
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

/** Real production VM factory. Tests inject the same concrete VM types bound to loopback. */
@androidx.compose.runtime.Stable
class AssetRouteViewModels(val assets: click.cowa.linbit.feature.assets.AssetsViewModel, val spot: click.cowa.linbit.feature.trade.SpotSubpagesViewModel, val tradFi: click.cowa.linbit.feature.more.TradFiViewModel, val paper: click.cowa.linbit.feature.assets.PaperAccountViewModel? = null, val priceAlerts: click.cowa.linbit.feature.alerts.PriceAlertViewModel? = null)
data class RootHomeMarketFactory(val home: @Composable () -> click.cowa.linbit.feature.home.HomeViewModel, val markets: @Composable () -> click.cowa.linbit.feature.markets.MarketsViewModel)
val LocalRootHomeMarketFactory = androidx.compose.runtime.staticCompositionLocalOf<RootHomeMarketFactory?> { null }
@Composable internal fun rootHome(): click.cowa.linbit.feature.home.HomeViewModel = LocalRootHomeMarketFactory.current?.home?.invoke() ?: hiltViewModel()
@Composable internal fun rootMarkets(): click.cowa.linbit.feature.markets.MarketsViewModel = LocalRootHomeMarketFactory.current?.markets?.invoke() ?: hiltViewModel()
@Composable
fun rememberAssetRouteViewModels(): AssetRouteViewModels {
    val assets: click.cowa.linbit.feature.assets.AssetsViewModel = hiltViewModel()
    val spot: click.cowa.linbit.feature.trade.SpotSubpagesViewModel = hiltViewModel()
    val tradFi: click.cowa.linbit.feature.more.TradFiViewModel = hiltViewModel()
    val paper: click.cowa.linbit.feature.assets.PaperAccountViewModel = hiltViewModel()
    val priceAlerts: click.cowa.linbit.feature.alerts.PriceAlertViewModel = hiltViewModel()
    return remember { AssetRouteViewModels(assets, spot, tradFi, paper, priceAlerts) }
}

val LocalRootProfileFactory = androidx.compose.runtime.staticCompositionLocalOf<@Composable () -> click.cowa.linbit.feature.profile.ProfileViewModel> { { hiltViewModel() } }
val LocalTradeHistoryFactory = androidx.compose.runtime.staticCompositionLocalOf<@Composable () -> click.cowa.linbit.feature.trade.TradeHistoryViewModel> { { hiltViewModel() } }

/** Root stays the sole owner of feature routing. Optional factory is DI, not a synthetic navigation path. */
@Composable
fun AppRoot(vm: AppViewModel = hiltViewModel(), badgeVm: NotificationBadgeViewModel = hiltViewModel(), routeVms: AssetRouteViewModels = rememberAssetRouteViewModels(), showSplash: Boolean = true, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    // SecureTokenStore supplies its initial state synchronously in DI; never optimistically show Home.
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val authVm: click.cowa.linbit.feature.auth.AuthViewModel = hiltViewModel()
    AppRootContent(vm, badgeVm, routeVms, loggedIn, showSplash, onDialogWindow, authVm)
}

@Composable
internal fun AppRootContent(vm: AppViewModel, badgeVm: NotificationBadgeViewModel, routeVms: AssetRouteViewModels, loggedIn: Boolean, showSplash: Boolean, onDialogWindow: (android.view.Window?) -> Unit, authVm: click.cowa.linbit.feature.auth.AuthViewModel) {
    val activity = androidx.compose.ui.platform.LocalContext.current as? android.app.Activity
    val mode by vm.themeMode.collectAsStateWithLifecycle()
    val dark = resolveDark(mode)
    LaunchedEffect(loggedIn) { if (!loggedIn) vm.resetSessionNavigation() }
    if (!loggedIn) {
        LinbitTheme(dark) {
            AuthFlow(startRegister = false, onClose = {
                // Unauthenticated users must not dismiss the auth root to expose Home.
                if (!vm.loggedIn.value) activity?.finish()
            }, vm = authVm, onDialogWindow = onDialogWindow)
            ToastOverlay(vm.toast)
        }
        return
    }
    AuthenticatedAppContent(vm, badgeVm, routeVms, showSplash, onDialogWindow)
}
