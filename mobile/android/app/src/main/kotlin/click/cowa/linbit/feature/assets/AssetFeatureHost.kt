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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.padding
import click.cowa.linbit.ui.components.pressable

/** One actual AppRoot host for the existing earn/strategy/TradFi OpenAPI features. */
@Composable
fun AssetFeatureHost(feature: String, onBack: () -> Unit, onLogin: () -> Unit, onTrade: (Boolean, String) -> Unit, spotVm: SpotSubpagesViewModel? = null, tradFiVm: TradFiViewModel? = null, paperVm: PaperAccountViewModel? = null, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    require(feature in setOf("earn", "strategy", "paper", "tradfi", "pnl", "quickbuy", "convert")) { "Unsupported asset feature: $feature" }
    BackHandler(onBack = onBack)
    Box(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        when (feature) {
            "pnl" -> PnlAnalysisScreen(onBack)
            "convert" -> androidx.compose.foundation.layout.Column {
                androidx.compose.foundation.layout.Box(androidx.compose.ui.Modifier.padding(start = 16.dp).size(48.dp).pressable(scale = 1f, onClick = onBack), contentAlignment = androidx.compose.ui.Alignment.CenterStart) {
                    click.cowa.linbit.ui.components.LbIcon(click.cowa.linbit.R.drawable.ic_arrow_left, 22.dp, LB.p.textPrimary)
                }
                click.cowa.linbit.feature.trade.ConvertSubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onDialogWindow = onDialogWindow)
            }
            "quickbuy" -> QuickBuyScreen(onBack)
            "earn" -> EarnSubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onBack = onBack, onDialogWindow = onDialogWindow)
            "strategy" -> StrategySubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onBack = onBack, onDialogWindow = onDialogWindow)
            "paper" -> PaperAccountScreen(onBack, onLogin, vm = paperVm ?: hiltViewModel(), onDialogWindow = onDialogWindow)
            else -> TradFiScreen(onBack, onTrade, vm = tradFiVm ?: hiltViewModel(), bottomInset = 0.dp)
        }
    }
}
