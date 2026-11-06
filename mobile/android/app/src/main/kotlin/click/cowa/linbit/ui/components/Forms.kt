package click.cowa.linbit.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import click.cowa.linbit.R
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.lbNum

/**
 * 输入框（对应 iOS LBTextField）：灰底圆角 8、高 48；聚焦黑描边，错误红描边 + 下方红字；
 * 有内容且聚焦时显示清除按钮；secure 时右侧眼睛切换显隐。
 */
@Composable
fun LbTextField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    secure: Boolean = false,
    keyboardType: KeyboardType = KeyboardType.Text,
    error: String? = null,
    height: Dp = 44.dp,
    leading: (@Composable () -> Unit)? = null,
    onFocusChange: (Boolean) -> Unit = {},
    testTag: String? = null,
    enabled: Boolean = true,
) {
    val p = LB.p
    var focused by remember { mutableStateOf(false) }
    var reveal by remember { mutableStateOf(false) }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(
            Modifier
                .fillMaxWidth()
                .heightIn(min = height)
                .clip(RoundedCornerShape(Radius.md))
                .background(if (error != null) p.bg else p.fill)
                .border(
                    BorderStroke(1.dp, if (error != null) p.down else if (focused) p.textPrimary else Color.Transparent),
                    RoundedCornerShape(Radius.md),
                )
                .padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            leading?.invoke()
            Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                if (value.isEmpty()) Text(placeholder, style = lbNum(FontSize.body), color = p.textTertiary, maxLines = 1)
                BasicTextField(
                    value = value,
                    onValueChange = if (enabled) onValueChange else { _ -> },
                    enabled = enabled,
                    singleLine = true,
                    textStyle = lbNum(FontSize.body).copy(color = p.textPrimary),
                    cursorBrush = SolidColor(p.textPrimary),
                    visualTransformation = if (secure && !reveal) PasswordVisualTransformation() else VisualTransformation.None,
                    keyboardOptions = KeyboardOptions(
                        keyboardType = if (secure) KeyboardType.Password else keyboardType,
                        autoCorrectEnabled = false,
                    ),
                    modifier = Modifier.then(if (testTag == null) Modifier else Modifier.testTag(testTag)).fillMaxWidth().onFocusChanged { focused = it.isFocused; onFocusChange(it.isFocused) },
                )
            }
            if (value.isNotEmpty() && focused) {
                LbImageIcon(if (p.isDark) R.drawable.ic_duo_clear_circle_dark else R.drawable.ic_duo_clear_circle_light, 18.dp, Modifier.pressable { onValueChange("") })
            }
            if (secure) {
                LbIcon(if (reveal) R.drawable.ic_eye else R.drawable.ic_eye_off, 20.dp, p.textSecondary, Modifier.pressable { reveal = !reveal })
            }
        }
        if (error != null) Text(error, style = click.cowa.linbit.ui.theme.lb(FontSize.small), color = p.down)
    }
}

@Composable
fun FieldLabel(text: String, modifier: Modifier = Modifier) {
    Text(text, style = click.cowa.linbit.ui.theme.lb(FontSize.caption, androidx.compose.ui.text.font.FontWeight.Medium), color = LB.p.textPrimary, modifier = modifier.fillMaxWidth())
}
