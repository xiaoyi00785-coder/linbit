package click.cowa.linbit.ui.components

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.theme.*

/**
 * 参考 App 的确认 / 表单弹层：底部弹出，左对齐粗体标题 + 说明 + 并排两个按钮
 * （左「取消」灰底，右操作按钮：破坏性操作红底，其余主色黑底）。替代各处 Material AlertDialog。
 */
@Composable
fun LbDialog(
    title: String,
    onDismiss: () -> Unit,
    confirmText: String,
    onConfirm: () -> Unit,
    message: String? = null,
    dismissText: String? = "取消",
    onDismissClick: () -> Unit = onDismiss,
    destructive: Boolean = false,
    confirmEnabled: Boolean = true,
    loading: Boolean = false,
    content: (@Composable ColumnScope.() -> Unit)? = null,
) {
    val p = LB.p
    LbSheet(null, onClose = onDismiss) {
        Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 4.dp, bottom = 16.dp)) {
            Text(title, style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
            if (message != null) Text(message, Modifier.padding(top = 12.dp), style = lb(FontSize.label), color = p.textSecondary)
            if (content != null) Column(Modifier.padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp), content = content)
            Row(Modifier.fillMaxWidth().padding(top = 24.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (dismissText != null) LbButton(dismissText, Modifier.weight(1f), kind = ButtonKind.SECONDARY, onClick = onDismissClick)
                LbButton(confirmText, Modifier.weight(1f), kind = if (destructive) ButtonKind.SELL else ButtonKind.PRIMARY, enabled = confirmEnabled, loading = loading, onClick = onConfirm)
            }
        }
    }
}

/** 弹层单选列表的选中标记：参考为实心圆底白勾。 */
@Composable
fun SheetCheck(modifier: Modifier = Modifier) = ThemedIcon(click.cowa.linbit.R.drawable.ic_mc_check_circle_light, click.cowa.linbit.R.drawable.ic_mc_check_circle_dark, 18.dp, modifier)
