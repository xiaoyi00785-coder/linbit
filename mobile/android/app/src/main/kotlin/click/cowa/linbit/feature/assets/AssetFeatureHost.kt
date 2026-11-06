package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import click.cowa.linbit.feature.more.TradFiScreen
import click.cowa.linbit.feature.trade.EarnSubpage
import click.cowa.linbit.feature.trade.StrategySubpage
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.feature.more.TradFiViewModel
import androidx.hilt.navigation.compose.hiltViewModel

/** One actual AppRoot host for the existing earn/strategy/TradFi OpenAPI features. */
@Composable
fun AssetFeatureHost(feature: String, onBack: () -> Unit, onLogin: () -> Unit, onTrade: (Boolean, String) -> Unit, spotVm: SpotSubpagesViewModel? = null, tradFiVm: TradFiViewModel? = null, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    require(feature in setOf("earn", "strategy", "tradfi")) { "Unsupported asset feature: $feature" }
    BackHandler(onBack = onBack)
    Box(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        when (feature) {
            "earn" -> EarnSubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onBack = onBack, onDialogWindow = onDialogWindow)
            "strategy" -> StrategySubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onBack = onBack, onDialogWindow = onDialogWindow)
            else -> TradFiScreen(onBack, onTrade, vm = tradFiVm ?: hiltViewModel(), bottomInset = 0.dp)
        }
    }
}
