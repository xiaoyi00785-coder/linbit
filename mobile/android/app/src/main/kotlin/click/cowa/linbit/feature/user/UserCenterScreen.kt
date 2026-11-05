package click.cowa.linbit.feature.user

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb

/** 个人中心（最小版，里程碑 1）：返回、主题切换、退出登录。完整页（ref2 #1/#2/#7）见路线图。 */
@Composable
fun UserCenterScreen(themeMode: ThemeMode, onTheme: (ThemeMode) -> Unit, onLogout: () -> Unit, onClose: () -> Unit) {
    val p = LB.p
    BackHandler(onBack = onClose)
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        Row(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            LbIcon(R.drawable.ic_arrow_left, modifier = Modifier.pressable(onClick = onClose))
        }
        Column(Modifier.weight(1f).padding(horizontal = Spacing.pageH), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("个人中心", style = lb(24.sp, FontWeight.Bold), color = p.textPrimary, modifier = Modifier.padding(top = 8.dp, bottom = 16.dp))
            Text("外观", style = lb(FontSize.caption), color = p.textSecondary)
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                ThemeMode.entries.forEach { m ->
                    LbButton(m.title, Modifier.weight(1f), kind = if (m == themeMode) ButtonKind.PRIMARY else ButtonKind.SECONDARY, height = 40.dp) { onTheme(m) }
                }
            }
        }
        LbButton("退出登录", Modifier.padding(Spacing.pageH), kind = ButtonKind.SECONDARY, onClick = onLogout)
    }
}
