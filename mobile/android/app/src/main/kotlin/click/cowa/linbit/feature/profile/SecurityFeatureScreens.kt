package click.cowa.linbit.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import click.cowa.linbit.core.generated.ApiSecurityOverview
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

/** 本页只显示表单；任何敏感提交都必须先经过此生产安全确认 sheet。 */
@Composable
internal fun ChangePasswordScreen(overview: ApiSecurityOverview?, vm: SecurityFeatureViewModel, onBack: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    var old by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var repeat by remember { mutableStateOf("") }
    var submitted by remember { mutableStateOf(false) }
    val error = SecurityFeatureLogic.passwordError(old, next, repeat)
    var errorOverride by remember { mutableStateOf<String?>(null) }
    var confirm by remember { mutableStateOf(false) }
    SecurityFormFrame("修改登录密码", onBack) {
        InfoNote("修改成功后将注销其他设备会话，并可能触发提现保护期。", warning = true)
        FieldLabel("原密码")
        LbTextField(old, { old = it; errorOverride = null }, "请输入原密码", secure = true, testTag = "security.password.old")
        FieldLabel("新密码")
        LbTextField(next, { next = it; errorOverride = null }, "8-64 位，含字母和数字", secure = true, error = errorOverride ?: error?.takeIf { next.isNotEmpty() && old.isNotEmpty() }, testTag = "security.password.new")
        FieldLabel("确认新密码")
        LbTextField(repeat, { repeat = it; errorOverride = null }, "再次输入新密码", secure = true, testTag = "security.password.confirm")
        Text("已开启 2FA 时使用谷歌动态码；否则需要 purpose=security 的邮箱/手机验证码。", style = lb(FontSize.small), color = LB.p.textSecondary)
        LbButton("下一步", enabled = error == null, onClick = { confirm = true })
    }
    LaunchedEffect(submitted) { if (submitted) { confirm = false; onBack() } }
    if (confirm) SecurityVerificationSheet("修改登录密码", "修改登录密码会注销其他设备会话。请核对后完成安全验证。", overview, vm, onClose = { confirm = false }, onDialogWindow = onDialogWindow, submit = { totp, code, onDone, onError ->
        vm.changePassword(old, next, repeat, overview, totp, code, onError) { submitted = true; onDone() }
    })
}

@Composable
internal fun AntiPhishingScreen(overview: ApiSecurityOverview?, vm: SecurityFeatureViewModel, onBack: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    var submitted by remember { mutableStateOf(false) }
    var value by remember { mutableStateOf("") }
    val validation = SecurityFeatureLogic.antiPhishingError(value)
    var confirm by remember { mutableStateOf(false) }
    SecurityFormFrame("设置防钓鱼码", onBack) {
        InfoNote("防钓鱼码会显示在 Linbit 外发邮件开头，用于识别仿冒邮件；它不是密码，不要填写任何登录或资金密码。")
        FieldLabel("防钓鱼码")
        LbTextField(value, { value = it.trim() }, "4-20 位字母或数字", error = validation?.takeIf { value.isNotEmpty() }, testTag = "security.antiphishing.value")
        Text("仅使用英文字母和数字，例如 Lb2026。", style = lb(FontSize.small), color = LB.p.textSecondary)
        LbButton("核对并设置", enabled = validation == null, onClick = { confirm = true })
    }
    LaunchedEffect(submitted) { if (submitted) { confirm = false; onBack() } }
    if (confirm) SecurityVerificationSheet("确认防钓鱼码", "保存后请勿向他人泄露动态验证码；防钓鱼码将显示在后续平台邮件中。", overview, vm, onClose = { confirm = false }, onDialogWindow = onDialogWindow, submit = { totp, code, onDone, onError ->
        vm.setAntiPhishing(value, overview, totp, code, onError) { submitted = true; onDone() }
    })
}

@Composable
private fun SecurityFormFrame(title: String, onBack: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    Column(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar(title, onBack)
        Column(Modifier.weight(1f).imePadding().verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp), content = content)
    }
}

@Composable
private fun SecurityVerificationSheet(
    title: String,
    note: String,
    overview: ApiSecurityOverview?,
    vm: SecurityFeatureViewModel,
    onClose: () -> Unit,
    onDialogWindow: (android.view.Window?) -> Unit,
    submit: (String, String, onDone: () -> Unit, onError: (String) -> Unit) -> Unit,
) {
    val twoFa = overview?.twoFactor?.enabled == true
    var totp by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var sending by remember { mutableStateOf(false) }
    var submitting by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    LbSheet(title, onClose = { if (!submitting) onClose() }, showClose = !submitting, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            InfoNote(note, warning = true)
            if (overview == null) ProfileError("安全状态尚未加载，请返回刷新后重试") {}
            else if (twoFa) LbTextField(totp, { totp = it.filter(Char::isDigit).take(6); error = null }, "6 位身份验证器动态码", keyboardType = KeyboardType.NumberPassword, error = error)
            else Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LbTextField(code, { code = it.filter(Char::isDigit).take(8); error = null }, "邮箱/手机验证码", Modifier.weight(1f), keyboardType = KeyboardType.Number, error = error)
                LbButton("发送", Modifier.width(86.dp), enabled = !sending && !submitting, loading = sending, onClick = {
                    scope.launch {
                        sending = true
                        try { vm.sendSecurityCode(); vm.toast.show("验证码已发送") } catch (e: Exception) { error = e.lbMessage() } finally { sending = false }
                    }
                })
            }
            LbButton("确认提交", enabled = overview != null && SecurityFeatureLogic.verificationReady(overview, totp, code) && !submitting, loading = submitting, onClick = {
                if (submitting) return@LbButton
                submitting = true
                submit(totp, code, onClose, { message -> submitting = false; error = message })
            })
            LbButton("取消", kind = ButtonKind.OUTLINE, enabled = !submitting, onClick = onClose)
        }
    }
}
