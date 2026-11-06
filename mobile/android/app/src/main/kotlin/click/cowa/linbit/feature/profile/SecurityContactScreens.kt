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
import click.cowa.linbit.core.generated.ApiSecurityOverview
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.launch

@Composable
internal fun ContactSecurityScreen(phone: Boolean, overview: ApiSecurityOverview?, vm: SecurityFeatureViewModel, onBack: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    var value by remember { mutableStateOf("") }
    var country by remember { mutableStateOf("+86") }
    var review by remember { mutableStateOf(false) }
    val label = if (phone) "手机" else "邮箱"
    val validation = SecurityFeatureLogic.contactError(value, phone)
    Column(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("更换$label", onBack)
        Column(Modifier.weight(1f).imePadding().verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            InfoNote("变更${label}需要验证新旧联系方式。已开启身份验证器时还需输入动态码。", warning = true)
            if (phone) {
                FieldLabel("国家或地区代码")
                LbTextField(country, { country = it.filter { c -> c.isDigit() || c == '+' }.take(5) }, "+86", keyboardType = KeyboardType.Phone, testTag = "security.contact.country")
            }
            FieldLabel("新$label")
            LbTextField(value, { value = it.trim() }, if (phone) "新手机号码" else "name@example.com", keyboardType = if (phone) KeyboardType.Phone else KeyboardType.Email, error = validation?.takeIf { value.isNotEmpty() }, testTag = "security.contact.value")
            Text("下一步将核对新旧验证码与身份验证器信息。", style = lb(FontSize.small), color = LB.p.textSecondary)
            LbButton("核对并继续", enabled = validation == null && overview != null, onClick = { review = true })
        }
    }
    if (review) ContactVerificationSheet(phone, value, country, overview, vm, onDialogWindow, onBack, { review = false })
}

@Composable
private fun ContactVerificationSheet(phone: Boolean, value: String, country: String, overview: ApiSecurityOverview?, vm: SecurityFeatureViewModel, onDialogWindow: (android.view.Window?) -> Unit, onBack: () -> Unit, onClose: () -> Unit) {
    val scope = rememberCoroutineScope()
    var newCode by remember { mutableStateOf("") }; var oldCode by remember { mutableStateOf("") }; var totp by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }; var busy by remember { mutableStateOf(false) }; var done by remember { mutableStateOf(false) }
    val twoFa = overview?.twoFactor?.enabled == true
    fun sendContact() {
        if (busy) return
        scope.launch {
            busy = true; error = null
            try { vm.sendContactCode(phone, value, country) } catch (e: Exception) { error = e.lbMessage() }
            finally { busy = false }
        }
    }
    LbSheet(if (phone) "复核更换手机" else "复核更换邮箱", { if (!busy) onClose() }, showClose = !busy, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            KvRow("新联系方式", if (phone) "$country $value" else value)
            InfoNote("请分别输入新联系方式与原联系方式收到的验证码。", warning = true)
            LbTextField(newCode, { newCode = it.filter(Char::isDigit).take(8); error = null }, "新联系方式验证码", keyboardType = KeyboardType.NumberPassword, error = error, testTag = "security.contact.newCode")
            LbButton("发送新验证码", Modifier.fillMaxWidth(), enabled = !busy, loading = busy, onClick = ::sendContact)
            LbTextField(oldCode, { oldCode = it.filter(Char::isDigit).take(8); error = null }, "现有联系方式验证码", keyboardType = KeyboardType.NumberPassword, testTag = "security.contact.oldCode")
            LbButton("发送现有验证码", Modifier.fillMaxWidth(), enabled = !busy, onClick = { scope.launch { busy = true; error = null; try { vm.sendSecurityCode() } catch (e: Exception) { error = e.lbMessage() } finally { busy = false } } })
            if (twoFa) LbTextField(totp, { totp = it.filter(Char::isDigit).take(6); error = null }, "6 位身份验证器动态码", keyboardType = KeyboardType.NumberPassword)
            if (overview == null) Text("安全状态尚未加载，请返回重试", color = LB.p.down, style = lb(FontSize.body))
            LbButton("确认变更", enabled = overview != null && newCode.length >= 6 && oldCode.length >= 6 && (!twoFa || totp.length == 6) && !busy, loading = busy, onClick = {
                if (busy) return@LbButton
                busy = true
                scope.launch {
                    try {
                        vm.runContactChange(SecurityContactPayload(phone, value, country, newCode, oldCode, totp.takeIf { twoFa }), overview)
                        done = true
                    } catch (e: Exception) { error = e.lbMessage() } finally { busy = false }
                }
            })
        }
    }
    LaunchedEffect(done) { if (done) { onClose(); onBack() } }
}

@Composable
internal fun FundPasswordScreen(overview: ApiSecurityOverview?, vm: SecurityFeatureViewModel, onBack: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit = {}) {
    val alreadySet = overview?.fundPasswordSet == true
    var old by remember { mutableStateOf("") }; var value by remember { mutableStateOf("") }; var repeat by remember { mutableStateOf("") }; var review by remember { mutableStateOf(false) }
    val valid = value.length == 6 && value.all(Char::isDigit) && value == repeat && (!alreadySet || (old.length == 6 && old.all(Char::isDigit))) && overview != null
    Column(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar(if (alreadySet) "修改资金密码" else "设置资金密码", onBack)
        Column(Modifier.weight(1f).imePadding().verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            InfoNote("请设置 6 位数字资金密码。不要与登录密码相同。", warning = true)
            if (alreadySet) { FieldLabel("原资金密码"); LbTextField(old, { old = it.filter(Char::isDigit).take(6) }, "原 6 位资金密码", secure = true, keyboardType = KeyboardType.NumberPassword, testTag = "security.fund.old") }
            FieldLabel("新资金密码"); LbTextField(value, { value = it.filter(Char::isDigit).take(6) }, "6 位数字", secure = true, keyboardType = KeyboardType.NumberPassword, error = if (value.isNotEmpty() && value.length != 6) "需为 6 位数字" else null, testTag = "security.fund.value")
            FieldLabel("确认新资金密码"); LbTextField(repeat, { repeat = it.filter(Char::isDigit).take(6) }, "再次输入", secure = true, keyboardType = KeyboardType.NumberPassword, error = if (repeat.isNotEmpty() && repeat != value) "两次输入不一致" else null, testTag = "security.fund.confirm")
            LbButton("核对并继续", enabled = valid, onClick = { review = true })
        }
    }
    if (review) SecurityFundVerification(overview, vm, onDialogWindow, { review = false }) { totp, code, onError ->
        vm.runFundPassword(value, repeat, old, alreadySet, overview, totp, code); review = false; onBack()
    }
}

@Composable
private fun SecurityFundVerification(overview: ApiSecurityOverview?, vm: SecurityFeatureViewModel, onDialogWindow: (android.view.Window?) -> Unit, onClose: () -> Unit, submit: suspend (String, String, (String) -> Unit) -> Unit) {
    var totp by remember { mutableStateOf("") }; var code by remember { mutableStateOf("") }; var error by remember { mutableStateOf<String?>(null) }; var submitting by remember { mutableStateOf(false) }; var sending by remember { mutableStateOf(false) }
    val twoFa = overview?.twoFactor?.enabled == true; val scope = rememberCoroutineScope()
    LbSheet("确认资金密码", { if (!submitting && !sending) onClose() }, showClose = !submitting && !sending, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            InfoNote(if (twoFa) "输入身份验证器动态码后确认。" else "请输入发送至现有联系方式的安全验证码，完成后确认。", warning = true)
            if (twoFa) LbTextField(totp, { totp = it.filter(Char::isDigit).take(6) }, "6 位身份验证器动态码", keyboardType = KeyboardType.NumberPassword)
            else Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { LbTextField(code, { code = it.filter(Char::isDigit).take(8) }, "现有联系方式验证码", Modifier.weight(1f), keyboardType = KeyboardType.NumberPassword); LbButton("发送", Modifier.width(86.dp), enabled = !submitting && !sending, loading = sending, onClick = { if (!submitting && !sending) scope.launch { sending = true; error = null; try { vm.sendSecurityCode() } catch (e: Exception) { error = e.lbMessage() } finally { sending = false } } }) }
            error?.let { Text(it, color = LB.p.down, style = lb(FontSize.body)) }
            LbButton("确认设置", enabled = SecurityFeatureLogic.verificationReady(overview, totp, code) && !submitting && !sending, loading = submitting, onClick = { if (!submitting) { submitting = true; scope.launch { try { submit(totp, code) { message -> error = message; submitting = false } } catch (e: Exception) { error = e.lbMessage(); submitting = false } } } })
        }
    }
}
