package click.cowa.linbit.ui.components

import androidx.annotation.DrawableRes
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.R
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.lb

/** 底部 Tab：首页 / 行情 / 合约 / 现货 / 资产（与 iOS MainTab 一致）。 */
enum class MainTab(val title: String, @DrawableRes val light: Int, @DrawableRes val dark: Int) {
    HOME("首页", R.drawable.ic_tab_home_light, R.drawable.ic_tab_home_dark),
    MARKETS("行情", R.drawable.ic_tab_market_light, R.drawable.ic_tab_market_dark),
    FUTURES("合约", R.drawable.ic_tab_futures_light, R.drawable.ic_tab_futures_dark),
    SPOT("现货", R.drawable.ic_tab_spot_light, R.drawable.ic_tab_spot_dark),
    ASSETS("资产", R.drawable.ic_tab_assets_light, R.drawable.ic_tab_assets_dark),
}

/** 底部 Tab（参考 EasiCoin：通栏白底 + 顶部发丝线；图标 22 + 11sp 文案；选中黑色原色图标，未选灰色）。 */
@Composable
fun FloatingTabBar(selected: MainTab, onSelect: (MainTab) -> Unit, modifier: Modifier = Modifier) {
    val p = LB.p
    Column(modifier.fillMaxWidth().background(p.bg)) {
        Box(Modifier.fillMaxWidth().height(0.5.dp).background(p.border))
        Row(
            Modifier.fillMaxWidth().navigationBarsPadding().height(Metrics.tabBarH - 3.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            MainTab.entries.forEach { tab ->
                val sel = tab == selected
                Column(
                    Modifier
                        .weight(1f)
                        .fillMaxHeight()
                        .testTag("main-tab-${tab.name.lowercase()}")
                        .clickable(remember { MutableInteractionSource() }, indication = null) { onSelect(tab) },
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Center,
                ) {
                    androidx.compose.foundation.Image(
                        painterResource(if (p.isDark) tab.dark else tab.light), null, Modifier.size(22.dp),
                        colorFilter = if (sel) null else androidx.compose.ui.graphics.ColorFilter.tint(p.textTertiary),
                    )
                    Box(Modifier.height(4.dp))
                    Text(tab.title, style = lb(FontSize.tabBar, if (sel) FontWeight.Medium else FontWeight.Normal), color = if (sel) p.textPrimary else p.textTertiary, maxLines = 1)
                }
            }
        }
    }
}
