package click.cowa.linbit.feature.auth

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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupProperties
import click.cowa.linbit.R
import click.cowa.linbit.core.auth.AccountId
import click.cowa.linbit.core.auth.CountryCodes
import click.cowa.linbit.ui.components.FieldLabel
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.LbImageIcon
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** 顶栏：左 ⌄ 关闭（首屏）或 ← 返回（二级），右 客服耳机（ref2 #3/#10）。 */
@Composable
fun AuthTopBar(isRoot: Boolean, onLeft: () -> Unit, onSupport: () -> Unit) {
    val p = LB.p
    Row(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(44.dp).pressable(onClick = onLeft), contentAlignment = Alignment.CenterStart) {
            LbIcon(if (isRoot) R.drawable.ic_chevron_down else R.drawable.ic_arrow_left, 22.dp, p.textPrimary)
        }
        Box(Modifier.weight(1f))
        Box(Modifier.size(44.dp).pressable(onClick = onSupport), contentAlignment = Alignment.CenterEnd) {
            LbIcon(R.drawable.ic_headset, 22.dp, p.textPrimary)
        }
    }
}

/** 大标题 + 灰色副标题（28sp medium，副标题 14sp，ref2 #3/#10/#11）。 */
@Composable
fun AuthHeader(title: String, subtitle: AnnotatedString? = null, subtitleColor: Color = LB.p.textSecondary) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(top = 18.dp, bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(title, style = lb(androidx.compose.ui.unit.TextUnit(28f, androidx.compose.ui.unit.TextUnitType.Sp), FontWeight.Medium), color = p.textPrimary)
        if (subtitle != null) Text(subtitle, style = lb(FontSize.label), color = subtitleColor)
    }
}

/** 「—— 或者 ——」。 */
@Composable
fun OrDivider(modifier: Modifier = Modifier) {
    val p = LB.p
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        HorizontalDivider(Modifier.weight(1f), color = p.divider)
        Text("或者", style = lb(FontSize.small), color = p.textSecondary)
        HorizontalDivider(Modifier.weight(1f), color = p.divider)
    }
}

/** 第三方登录灰按钮：图标靠左，文案居中（ref2 #3）。 */
@Composable
fun SocialButton(iconRes: Int, title: String, multicolor: Boolean = false, onClick: () -> Unit) {
    val p = LB.p
    Box(
        Modifier.fillMaxWidth().height(48.dp).clip(RoundedCornerShape(Radius.xl)).background(p.fill).pressable(onClick = onClick).padding(horizontal = 24.dp),
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.align(Alignment.CenterStart)) {
            if (multicolor) LbImageIcon(iconRes, 16.dp) else LbIcon(iconRes, 16.dp, p.textPrimary)
        }
        Text(title, style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
    }
}

/** 复选框（15×15，圆角 3）。 */
@Composable
fun Checkbox(on: Boolean, onChange: (Boolean) -> Unit) {
    val p = LB.p
    Box(
        Modifier.size(15.dp).clip(RoundedCornerShape(3.dp)).background(if (on) p.textPrimary else Color.Transparent)
            .border(BorderStroke(1.2.dp, if (on) p.textPrimary else p.textTertiary), RoundedCornerShape(3.dp))
            .clickable(remember { MutableInteractionSource() }, indication = null) { onChange(!on) },
        contentAlignment = Alignment.Center,
    ) { if (on) Text("✓", style = lb(androidx.compose.ui.unit.TextUnit(9f, androidx.compose.ui.unit.TextUnitType.Sp), FontWeight.Bold), color = p.bg) }
}

private val EMAIL_DOMAINS = listOf("@hotmail.com", "@msn.com", "@yahoo.com", "@gmail.com", "@outlook.com")

/**
 * 「邮箱/手机号码」智能输入（ref2 #3/#5/#6/#9）：纯数字时出现 🇨🇳 +86 ▾ 前缀；
 * 输入字母时下拉邮箱后缀（第一行为「上一次登录」脱敏账户）。
 */
@Composable
fun AccountField(vm: AuthViewModel, onPickCountry: () -> Unit) {
    val p = LB.p
    val raw = vm.raw
    val isPhone = raw.isNotEmpty() && raw.all { it.isDigit() }
    var focused by remember { mutableStateOf(false) }
    var widthPx by remember { mutableIntStateOf(0) }
    var heightPx by remember { mutableIntStateOf(0) }
    val suggestions: List<Pair<String, Boolean>> = remember(raw, focused, vm.lastLogin) {
        if (!focused || raw.isEmpty() || isPhone) emptyList() else {
            val local = raw.substringBefore("@")
            val typedDomain = if (raw.contains("@")) "@" + raw.substringAfter("@") else ""
            val out = ArrayList<Pair<String, Boolean>>()
            if (vm.lastLogin.isNotEmpty() && vm.lastLogin.lowercase().startsWith(raw.lowercase().take(1))) out += vm.lastLogin to true
            out += EMAIL_DOMAINS.filter { typedDomain.isEmpty() || it.startsWith(typedDomain) }.map { (local + it) to false }
            out.filter { it.first != raw }
        }
    }
    Box(Modifier.fillMaxWidth().onGloballyPositioned { widthPx = it.size.width; heightPx = it.size.height }) {
        LbTextField(
            value = raw, onValueChange = vm::onRawChange, placeholder = "邮箱/手机号码 (不含国际区号)",
            keyboardType = KeyboardType.Email, error = vm.accountError, onFocusChange = { focused = it },
            leading = if (isPhone) ({
                Row(Modifier.pressable(onClick = onPickCountry), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(CountryCodes.flag(vm.countryCode), style = lb(FontSize.price))
                    Text("+${vm.countryCode}", style = lbNum(FontSize.body), color = p.textPrimary)
                    Text("▾", style = lb(FontSize.small), color = p.textTertiary)
                    Box(Modifier.padding(start = 6.dp).width(1.dp).height(18.dp).background(p.border))
                }
            }) else null,
        )
        if (suggestions.isNotEmpty()) {
            val density = LocalDensity.current
            Popup(
                alignment = Alignment.TopStart,
                offset = IntOffset(0, (if (vm.accountError != null) heightPx else heightPx) + with(density) { 6.dp.roundToPx() }),
                properties = PropertyDefaults,
            ) {
                Column(
                    Modifier
                        .width(with(density) { widthPx.toDp() })
                        .shadow(10.dp, RoundedCornerShape(Radius.md))
                        .background(p.bgElevated, RoundedCornerShape(Radius.md))
                        .padding(vertical = 4.dp),
                ) {
                    suggestions.forEach { (s, last) ->
                        Row(
                            Modifier.fillMaxWidth().height(34.dp).pressable(scale = 1f) { vm.onRawChange(s); focused = false }.padding(horizontal = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(if (last) AccountId.fromRaw(s, vm.countryCode).masked else s, Modifier.weight(1f), style = lb(FontSize.label), color = p.textPrimary, maxLines = 1)
                            if (last) Text("上一次登录", style = lb(FontSize.small), color = p.textTertiary)
                        }
                    }
                }
            }
        }
    }
}

private val PropertyDefaults = PopupProperties(focusable = false)

/** 6 格验证码（ref2 #11）：当前格 lime 描边 + 光标；左下「粘贴」，右下「重新发送(58s)」。 */
@Composable
fun OtpBoxes(
    code: String,
    onCode: (String) -> Unit,
    error: Boolean,
    resendable: Boolean,
    onResend: suspend () -> Boolean,
    onPaste: () -> Unit = {},
) {
    val p = LB.p
    val focus = remember { FocusRequester() }
    var focused by remember { mutableStateOf(false) }
    var left by remember { mutableIntStateOf(60) }
    var tick by remember { mutableIntStateOf(0) }
    var blink by remember { mutableStateOf(true) }
    val scope = rememberCoroutineScope()
    val clipboard = LocalClipboardManager.current
    LaunchedEffect(tick) { left = 60; while (left > 0) { delay(1_000); left -= 1 } }
    LaunchedEffect(Unit) { focus.requestFocus(); while (true) { delay(500); blink = !blink } }

    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Box {
            BasicTextField(
                value = code,
                onValueChange = { onCode(it.filter(Char::isDigit).take(6)) },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                cursorBrush = SolidColor(Color.Transparent),
                textStyle = lb(FontSize.body).copy(color = Color.Transparent),
                modifier = Modifier.focusRequester(focus).onFocusChanged { focused = it.isFocused }.size(1.dp),
            )
            Row(Modifier.fillMaxWidth().clickable(remember { MutableInteractionSource() }, indication = null) { focus.requestFocus() }, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                repeat(6) { i ->
                    val ch = code.getOrNull(i)?.toString().orEmpty()
                    val active = focused && i == minOf(code.length, 5)
                    Box(
                        Modifier
                            .weight(1f).height(48.dp)
                            .clip(RoundedCornerShape(Radius.md))
                            .background(if (active) p.bg else p.fill)
                            .border(BorderStroke(1.dp, if (error) p.down else if (active) p.brand else Color.Transparent), RoundedCornerShape(Radius.md)),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(ch, style = lbNum(androidx.compose.ui.unit.TextUnit(22f, androidx.compose.ui.unit.TextUnitType.Sp), FontWeight.SemiBold), color = p.textPrimary)
                        if (active && ch.isEmpty() && blink) Box(Modifier.size(1.5.dp, 20.dp).background(p.brand))
                    }
                }
            }
        }
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                "粘贴", style = lb(FontSize.caption, FontWeight.Medium), color = p.brand,
                modifier = Modifier.pressable {
                    val s = clipboard.getText()?.text.orEmpty().filter(Char::isDigit)
                    if (s.length >= 6) onCode(s.take(6)) else onPaste()
                },
            )
            Box(Modifier.weight(1f))
            if (resendable) {
                Text(
                    if (left > 0) "重新发送(${left}s)" else "重新发送", style = lb(FontSize.caption),
                    color = if (left > 0) p.textTertiary else p.brand,
                    modifier = Modifier.pressable(enabled = left == 0) { scope.launch { if (onResend()) tick += 1 } },
                )
            }
        }
    }
}

@Composable
fun LabeledAccountField(vm: AuthViewModel, onPickCountry: () -> Unit) {
    Column {
        FieldLabel("邮箱/手机号码")
        Box(Modifier.height(8.dp))
        AccountField(vm, onPickCountry)
    }
}


/** 登录 / 注册流程的主按钮：黑底白字，高 48、圆角 12，始终可点（空输入时由校验给出错误提示，ref2 #3/#10/#11）。 */
@Composable
fun AuthButton(title: String, modifier: Modifier = Modifier, loading: Boolean = false, onClick: () -> Unit) {
    click.cowa.linbit.ui.components.LbButton(title, modifier, height = 48.dp, loading = loading, radius = Radius.xl, onClick = onClick)
}
