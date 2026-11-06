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
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

/** Host UI for the existing safe Passkey API, neither a fake native ceremony nor a second-factor claim. */
@Composable
fun PasskeySecurityScreen(vm: SecurityViewModel, onBack: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    val state by vm.state.collectAsStateWithLifecycle()
    val activity = LocalContext.current as? android.app.Activity
    var name by remember { mutableStateOf("") }
    LaunchedEffect(Unit) { vm.loadPasskeys() }
    DisposableEffect(Unit) { onDispose { vm.cancelPasskeyRegistration() } }
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
                    LbButton(if (state.passkeyDeleteBusy && state.passkeyDeleteTarget?.id == passkey.id) "等待服务端复核结果…" else "删除",
                        kind = ButtonKind.SECONDARY, enabled = !state.passkeyBusy && !state.passkeyDeleteBusy, height = 36.dp,
                        testTag = "security.passkey.delete.${passkey.id}") { vm.showDeletePasskey(passkey) }
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
            else LbButton("验证并添加通行密钥", Modifier.fillMaxWidth().padding(Spacing.pageH), enabled = !state.passkeyDeleteBusy, testTag = "security.passkey.register") { activity?.let { vm.registerPasskey(it, name) } }
        }
    }
    state.passkeyDeleteTarget?.let { item ->
        click.cowa.linbit.feature.auth.LbSheet("删除通行密钥？", { if (!state.passkeyDeleteBusy) vm.cancelDeletePasskey() }, showClose = !state.passkeyDeleteBusy, onWindowReady = onDialogWindow) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("删除后「${item.name ?: "Passkey"}」不能再用于登录此账户。该操作不会改变密码或其他登录方式。请确认后再删除。", style = lb(FontSize.label))
                Text("本步骤是前端最终复核；当前服务端 DELETE 契约不提供二次验证码校验。", style = lb(FontSize.small), color = LB.p.textSecondary)
                state.passkeysError?.let { Text(it, color = LB.p.down, style = lb(FontSize.small)) }
                LbButton(if (state.passkeyDeleteBusy) "正在删除并读取列表…" else "确认删除", enabled = !state.passkeyDeleteBusy, testTag = "security.passkey.delete.confirm") { vm.confirmDeletePasskey() }
                LbButton("取消", kind = ButtonKind.SECONDARY, enabled = !state.passkeyDeleteBusy, testTag = "security.passkey.delete.cancel") { vm.cancelDeletePasskey() }
            }
        }
    }
}
