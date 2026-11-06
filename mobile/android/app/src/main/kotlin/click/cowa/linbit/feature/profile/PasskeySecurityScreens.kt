package click.cowa.linbit.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.components.FieldLabel
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.theme.*

/** Application host only; Credential Manager displays the actual system sheet. */
@Composable
fun PasskeySecurityScreen(vm: SecurityViewModel, onBack: () -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    val activity = LocalContext.current as? android.app.Activity
    var name by remember { mutableStateOf("") }
    LaunchedEffect(Unit) { vm.loadPasskeys() }
    Column(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("通行密钥", onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Spacer(Modifier.height(4.dp))
            Text("使用设备屏幕锁或支持的凭据提供程序添加通行密钥。添加时需要服务端安全验证。", style = lb(FontSize.label), color = LB.p.textSecondary)
            if (!state.passkeysEnabled) Text("服务端尚未启用通行密钥；可保留密码登录，启用后再重试。", style = lb(FontSize.label), color = LB.p.down)
            when {
                state.passkeysLoading -> Text("正在读取已绑定的通行密钥…", style = lb(FontSize.label))
                state.passkeys.isEmpty() -> Text("暂无已绑定通行密钥", style = lb(FontSize.label), color = LB.p.textSecondary)
            }
            state.passkeys.forEach { passkey ->
                Column(Modifier.fillMaxWidth().background(LB.p.fill, RoundedCornerShape(Radius.md)).padding(14.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                    Text(passkey.name ?: "Passkey", style = lb(FontSize.body))
                    Text("添加于 ${passkey.createdAt?.take(10) ?: "未知时间"}", style = lb(FontSize.small), color = LB.p.textSecondary)
                }
            }
            if (state.passkeysEnabled) {
                FieldLabel(if (state.security?.twoFactor?.enabled == true) "身份验证器动态码" else "安全验证码")
                LbTextField(state.passkeyCode, vm::passkeyCode, if (state.security?.twoFactor?.enabled == true) "6 位动态码" else "邮箱/手机安全验证码", keyboardType = androidx.compose.ui.text.input.KeyboardType.NumberPassword, testTag = "security.passkey.code")
                FieldLabel("设备名称（可选）")
                LbTextField(name, { if (!state.passkeyBusy) name = it.take(64) }, "例如：我的 Android", testTag = "security.passkey.name")
            }
            state.passkeysError?.let { Text(it, style = lb(FontSize.small), color = LB.p.down, modifier = Modifier.testTag("security.passkey.error")) }
        }
        if (state.passkeysEnabled) {
            if (state.passkeyBusy) LbButton("取消本次注册", Modifier.fillMaxWidth().padding(Spacing.pageH), kind = ButtonKind.SECONDARY, testTag = "security.passkey.cancel") { vm.cancelPasskeyRegistration() }
            else LbButton("验证并添加通行密钥", Modifier.fillMaxWidth().padding(Spacing.pageH), testTag = "security.passkey.register") { activity?.let { vm.registerPasskey(it, name) } }
        }
    }
}
