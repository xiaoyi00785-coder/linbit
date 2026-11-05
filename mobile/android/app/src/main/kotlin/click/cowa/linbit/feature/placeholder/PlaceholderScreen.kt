package click.cowa.linbit.feature.placeholder

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LinbitWordmark
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb

/** 尚未移植的 Tab（合约 / 现货 / 资产）占位页：品牌字标 + 说明；资产页未登录时给出登录入口。路线图见 mobile/android/README.md。 */
@Composable
fun PlaceholderScreen(title: String, bottomInset: Dp, loginHint: Boolean = false, onLogin: () -> Unit = {}, modifier: Modifier = Modifier) {
    val p = LB.p
    Column(
        modifier.fillMaxSize().background(p.bg).padding(horizontal = Spacing.pageH * 2).padding(bottom = bottomInset),
        horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
    ) {
        LinbitWordmark(132.dp)
        Text(title, Modifier.padding(top = 28.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Text(if (loginHint) "登录后查看资产" else "即将开放", Modifier.padding(top = 6.dp), style = lb(FontSize.caption), color = p.textSecondary)
        if (loginHint) LbButton("登录 / 注册", Modifier.padding(top = 24.dp), onClick = onLogin)
    }
}
