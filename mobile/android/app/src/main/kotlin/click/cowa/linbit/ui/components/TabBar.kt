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

/** 悬浮胶囊 TabBar（对照图 ref/1.jpg 底部：高 61，左右各 21，半透明，选中项灰色胶囊，图标 20 + 11sp 文案）。 */
@Composable
fun FloatingTabBar(selected: MainTab, onSelect: (MainTab) -> Unit, modifier: Modifier = Modifier) {
    val p = LB.p
    Row(
        modifier
            .navigationBarsPadding()
            .padding(horizontal = Metrics.tabBarInset, vertical = 6.dp)
            .fillMaxWidth()
            .height(Metrics.tabBarH)
            .shadow(12.dp, CircleShape, ambientColor = Color.Black.copy(alpha = 0.08f), spotColor = Color.Black.copy(alpha = 0.08f))
            .clip(CircleShape)
            .background(p.tabBarBg)
            .border(BorderStroke(0.5.dp, p.tabBarBorder), CircleShape)
            .padding(5.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        MainTab.entries.forEach { tab ->
            val sel = tab == selected
            Column(
                Modifier
                    .weight(1f)
                    .height(Metrics.tabBarH - 10.dp)
                    .clip(CircleShape)
                    .background(if (sel) p.tabBarSelected else Color.Transparent)
                    .clickable(remember { MutableInteractionSource() }, indication = null) { onSelect(tab) },
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                androidx.compose.foundation.Image(painterResource(if (p.isDark) tab.dark else tab.light), null, Modifier.size(20.dp))
                Box(Modifier.height(3.dp))
                Text(tab.title, style = lb(FontSize.tabBar, FontWeight.Medium), color = p.textPrimary, maxLines = 1)
            }
        }
    }
}
