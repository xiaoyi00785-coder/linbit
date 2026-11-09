package click.cowa.linbit.feature.assets

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.HorizontalDivider
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import click.cowa.linbit.feature.more.TradFiScreen
import click.cowa.linbit.feature.more.TradFiViewModel
import click.cowa.linbit.feature.trade.EarnSubpage
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.feature.trade.StrategySubpage
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.LB

/** One actual AppRoot host for the existing earn/strategy/TradFi OpenAPI features. */
@Composable
fun AssetFeatureHost(feature: String, onBack: () -> Unit, onLogin: () -> Unit, onTrade: (Boolean, String) -> Unit, spotVm: SpotSubpagesViewModel? = null, tradFiVm: TradFiViewModel? = null, paperVm: PaperAccountViewModel? = null, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    require(feature in setOf("earn", "strategy", "paper", "tradfi", "pnl", "quickbuy", "convert")) { "Unsupported asset feature: $feature" }
    BackHandler(onBack = onBack)
    Box(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        when (feature) {
            "pnl" -> PnlAnalysisScreen(onBack)
            "convert" -> Column {
                Box(Modifier.padding(start = 16.dp).size(48.dp).pressable(scale = 1f, onClick = onBack), contentAlignment = Alignment.CenterStart) {
                    LbIcon(click.cowa.linbit.R.drawable.ic_arrow_left, 22.dp, LB.p.textPrimary)
                }
                click.cowa.linbit.feature.trade.ConvertSubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onDialogWindow = onDialogWindow)
            }
            "quickbuy" -> QuickBuyScreen(onBack)
            // 理财对照参考：现货顶栏内嵌，无独立返回标题栏；系统返回仍由 BackHandler 关闭 Host。
            "earn" -> Column(Modifier.fillMaxSize()) {
                Box(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = 16.dp), contentAlignment = Alignment.CenterStart) {
                    PrimaryTabs(listOf("现货", "策略", "理财", "闪兑"), 2, {}, size = 16.sp)
                }
                HorizontalDivider(color = LB.p.divider)
                EarnSubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onBack = null, onDialogWindow = onDialogWindow)
            }
            "strategy" -> StrategySubpage(0.dp, onLogin, vm = spotVm ?: hiltViewModel(), onBack = onBack, onDialogWindow = onDialogWindow)
            "paper" -> PaperAccountScreen(onBack, onLogin, vm = paperVm ?: hiltViewModel(), onDialogWindow = onDialogWindow)
            else -> TradFiScreen(onBack, onTrade, vm = tradFiVm ?: hiltViewModel(), bottomInset = 0.dp)
        }
    }
}
