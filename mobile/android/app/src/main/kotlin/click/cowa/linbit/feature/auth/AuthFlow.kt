package click.cowa.linbit.feature.auth

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import click.cowa.linbit.R
import click.cowa.linbit.core.auth.CountryCodes
import click.cowa.linbit.core.auth.PasswordRule
import click.cowa.linbit.core.generated.ApiLoginChallenge
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.DashedLink
import click.cowa.linbit.ui.components.FieldLabel
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbImageIcon
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb

private object Route {
    const val LOGIN = "login"
    const val PASSWORD = "login/password"
    const val VERIFY_LOGIN = "login/verify"
    const val REGISTER = "register"
    const val VERIFY_REGISTER = "register/verify"
    const val SET_PASSWORD = "register/password"
    const val FORGOT = "forgot"
    const val VERIFY_RESET = "forgot/verify"
    const val RESET = "forgot/new"
}

/** 登录 / 注册 / 找回密码 全流程（全屏，自带返回栈）。onClose：关闭（取消或登录成功）。 */
@Composable
fun AuthFlow(startRegister: Boolean, onClose: () -> Unit, vm: AuthViewModel = hiltViewModel()) {
    val p = LB.p
    val nav = rememberNavController()
    var resetToken by remember { mutableStateOf("") }
    val soon: (String) -> Unit = { vm.toast.show(it) }
    Box(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().imePadding()) {
        NavHost(nav, startDestination = if (startRegister) Route.REGISTER else Route.LOGIN) {
            composable(Route.LOGIN) {
                LoginAccountScreen(vm, onClose = onClose, onNext = { nav.navigate(Route.PASSWORD) }, onRegister = {
                    nav.navigate(Route.REGISTER) { popUpTo(Route.LOGIN) { inclusive = true } }
                }, soon = soon)
            }
            composable(Route.PASSWORD) {
                PasswordScreen(vm, onBack = { nav.popBackStack() }, onForgot = { nav.navigate(Route.FORGOT) },
                    onChallenge = { nav.navigate(Route.VERIFY_LOGIN) }, onDone = onClose, soon = soon)
            }
            composable(Route.VERIFY_LOGIN) {
                VerifyScreen(vm, VerifyMode.LOGIN, onBack = { nav.popBackStack() }, onDone = onClose, onSetPassword = {}, onResetToken = {}, soon = soon)
            }
            composable(Route.REGISTER) {
                RegisterScreen(vm, onClose = onClose, onNext = { nav.navigate(Route.VERIFY_REGISTER) }, onLogin = {
                    nav.navigate(Route.LOGIN) { popUpTo(Route.REGISTER) { inclusive = true } }
                }, soon = soon)
            }
            composable(Route.VERIFY_REGISTER) {
                VerifyScreen(vm, VerifyMode.REGISTER, onBack = { nav.popBackStack() }, onDone = {}, onSetPassword = { nav.navigate(Route.SET_PASSWORD) }, onResetToken = {}, soon = soon)
            }
            composable(Route.SET_PASSWORD) {
                SetPasswordScreen(vm, resetToken = null, onBack = { nav.popBackStack() }, onRegistered = onClose, onReset = {},
                    onTokenExpired = {}, onCodeInvalid = { nav.popBackStack() })
            }
            composable(Route.FORGOT) {
                ForgotScreen(vm, onBack = { nav.popBackStack() }, onNext = { nav.navigate(Route.VERIFY_RESET) })
            }
            composable(Route.VERIFY_RESET) {
                VerifyScreen(vm, VerifyMode.RESET, onBack = { nav.popBackStack() }, onDone = {}, onSetPassword = {},
                    onResetToken = { resetToken = it; nav.navigate(Route.RESET) }, soon = soon)
            }
            composable(Route.RESET) {
                SetPasswordScreen(vm, resetToken = resetToken, onBack = { nav.popBackStack() }, onRegistered = {},
                    onReset = { nav.popBackStack(Route.LOGIN, inclusive = false).also { if (!it) nav.navigate(Route.PASSWORD) { popUpTo(Route.FORGOT) { inclusive = true } } } },
                    onTokenExpired = { nav.popBackStack(Route.FORGOT, inclusive = false) }, onCodeInvalid = { nav.popBackStack() })
            }
        }
    }
}

private fun lightSubtitle(text: String) = AnnotatedString(text)

// ───────────── 登录 · 账户（ref2 #3 / #9）─────────────
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun LoginAccountScreen(vm: AuthViewModel, onClose: () -> Unit, onNext: () -> Unit, onRegister: () -> Unit, soon: (String) -> Unit) {
    val p = LB.p
    var showCountry by remember { mutableStateOf(false) }
    BackHandler(onBack = onClose)
    Column(Modifier.fillMaxSize()) {
        AuthTopBar(isRoot = true, onLeft = onClose, onSupport = { soon("在线客服即将开放") })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            AuthHeader("登录", lightSubtitle("立即登录，每日登录领红包 🧧"))
            LabeledAccountField(vm) { showCountry = true }
            AuthButton("登录", Modifier.padding(top = 32.dp)) { vm.submitAccount(onNext) }
            OrDivider(Modifier.padding(vertical = 32.dp))
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                SocialButton(R.drawable.ic_passkey, "使用通行密钥继续") { soon("通行密钥登录即将开放") }
                SocialButton(R.drawable.ic_apple, "通过 Apple 继续") { soon("Apple 登录即将开放") }
                SocialButton(R.drawable.ic_google, "通过 Google 继续", multicolor = true) { soon("Google 登录即将开放") }
            }
            Text(
                "创建账户", Modifier.align(Alignment.CenterHorizontally).padding(top = 24.dp, bottom = 24.dp).pressable(onClick = onRegister),
                style = lb(FontSize.label, FontWeight.Medium), color = p.brand,
            )
        }
    }
    if (showCountry) CountrySheet(vm.countryCode, onPick = { vm.countryCode = it; showCountry = false }, onClose = { showCountry = false })
}

// ───────────── 登录 · 密码（ref2 #10）─────────────
@Composable
internal fun PasswordScreen(vm: AuthViewModel, onBack: () -> Unit, onForgot: () -> Unit, onChallenge: () -> Unit, onDone: () -> Unit, soon: (String) -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxSize()) {
        AuthTopBar(isRoot = false, onLeft = onBack, onSupport = { soon("在线客服即将开放") })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            AuthHeader("请输入密码", AnnotatedString(vm.id.masked), LB.p.textSecondary)
            FieldLabel("密码")
            Box(Modifier.height(8.dp))
            LbTextField(vm.password, vm::onPasswordChange, "输入您的密码", secure = true, error = vm.passwordError)
            if (vm.cooldown > 0) {
                Box(Modifier.padding(top = 12.dp).fillMaxWidth().background(p.downTint, RoundedCornerShape(Radius.md)).padding(12.dp)) {
                    Text("验证码发送过于频繁，请 ${vm.cooldown} 秒后重试", style = lb(FontSize.small), color = p.down)
                }
            }
            AuthButton(
                if (vm.cooldown > 0) "${vm.cooldown}s 后重试" else "下一步", Modifier.padding(top = 32.dp), loading = vm.loading,
            ) { if (vm.cooldown == 0) vm.submitPassword(onChallenge, onDone) }
            Box(Modifier.padding(top = 20.dp)) { DashedLink("忘记密码?", onClick = onForgot) }
        }
    }
}

// ───────────── 验证码（ref2 #11）：登录二次验证 / 注册 / 找回密码 ─────────────
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun VerifyScreen(
    vm: AuthViewModel, mode: VerifyMode, onBack: () -> Unit, onDone: () -> Unit,
    onSetPassword: () -> Unit, onResetToken: (String) -> Unit, soon: (String) -> Unit,
) {
    val p = LB.p
    var methodIndex by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf(false) }
    var showHelp by remember { mutableStateOf(false) }
    val methods = vm.challenge?.methods.orEmpty()
    val method = if (mode == VerifyMode.LOGIN && methods.isNotEmpty()) methods[methodIndex.coerceAtMost(methods.size - 1)]
    else if (vm.id.kind == click.cowa.linbit.core.auth.AccountId.Kind.PHONE) ApiLoginChallenge.Methods.SMS else ApiLoginChallenge.Methods.EMAIL
    val totp = method == ApiLoginChallenge.Methods.TOTP
    val title = if (totp) "谷歌验证" else if (method == ApiLoginChallenge.Methods.SMS) "验证手机" else "验证邮箱"
    val subtitle: AnnotatedString = if (totp) AnnotatedString("请输入谷歌验证器（Authenticator）中显示的 6 位动态码") else buildAnnotatedString {
        append("请输入${if (method == ApiLoginChallenge.Methods.SMS) "手机" else "邮箱"} ")
        withStyle(SpanStyle(color = p.textPrimary)) { append(vm.id.masked) }
        append(" 收到的6位验证码")
    }
    fun submit() { vm.submitCode(mode, method, onDone, onSetPassword, onResetToken) }
    Column(Modifier.fillMaxSize()) {
        AuthTopBar(isRoot = false, onLeft = onBack, onSupport = { soon("在线客服即将开放") })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            AuthHeader(title, subtitle)
            FieldLabel("验证码")
            Box(Modifier.height(8.dp))
            OtpBoxes(
                code = vm.code,
                onCode = { v -> vm.code = v; error = false; if (v.length == 6 && !vm.loading) submit() },
                error = error, resendable = !totp, onResend = { vm.resend(mode) },
                onPaste = { vm.toast.show("剪贴板中没有验证码") },
            )
            AuthButton("确认", Modifier.padding(top = 32.dp), loading = vm.loading) { submit() }
            Row(Modifier.fillMaxWidth().padding(top = 20.dp), verticalAlignment = Alignment.CenterVertically) {
                DashedLink(if (totp) "无法使用验证器?" else "收不到验证码?") { showHelp = true }
                Box(Modifier.weight(1f))
                if (mode == VerifyMode.LOGIN && methods.size > 1) {
                    Text(
                        "切换验证方式", style = lb(FontSize.label), color = p.brand,
                        modifier = Modifier.pressable { methodIndex = (methodIndex + 1) % methods.size; vm.code = "" },
                    )
                }
            }
        }
    }
    if (showHelp) {
        val lines = if (totp) listOf("请确认手机系统时间为自动设置", "如更换手机或丢失验证器，请联系在线客服重置", "重置 2FA 后 24 小时内禁止提现")
        else listOf("请检查垃圾邮件 / 短信拦截", "确认${if (vm.id.kind == click.cowa.linbit.core.auth.AccountId.Kind.PHONE) "手机号" else "邮箱"}填写正确：${vm.id.masked}", "验证码 10 分钟内有效，60 秒后可重新发送", "仍未收到请联系在线客服")
        LbSheet(title = if (totp) "无法使用验证器?" else "收不到验证码?", onClose = { showHelp = false }) {
            Column(Modifier.padding(horizontal = Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                lines.forEach { Text("• $it", style = lb(FontSize.caption), color = p.textSecondary) }
            }
            LbButton("我知道了", Modifier.padding(Spacing.pageH)) { showHelp = false }
        }
    }
}

// ───────────── 注册（ref2 #4 / #5 / #6 / #8）─────────────
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun RegisterScreen(vm: AuthViewModel, onClose: () -> Unit, onNext: () -> Unit, onLogin: () -> Unit, soon: (String) -> Unit) {
    val p = LB.p
    var showInvite by remember { mutableStateOf(false) }
    var agree by remember { mutableStateOf(true) }
    var showRetain by remember { mutableStateOf(false) }
    var showCountry by remember { mutableStateOf(false) }
    BackHandler { showRetain = true }
    Column(Modifier.fillMaxSize()) {
        AuthTopBar(isRoot = true, onLeft = { showRetain = true }, onSupport = { soon("在线客服即将开放") })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            AuthHeader("创建帐户", buildAnnotatedString {
                append("完成注册可获得最高 ")
                withStyle(SpanStyle(color = p.textPrimary)) { append("15,200 USDT") }
            })
            LabeledAccountField(vm) { showCountry = true }
            Row(Modifier.padding(top = 24.dp).pressable(scale = 1f) { showInvite = !showInvite }, verticalAlignment = Alignment.CenterVertically) {
                Text("邀请码", style = lb(FontSize.label), color = p.textPrimary)
                Text(if (showInvite) " ▴" else " ▾", style = lb(FontSize.small), color = p.textPrimary)
            }
            if (showInvite) LbTextField(vm.invite, { vm.invite = it }, "邀请码（选填）", Modifier.padding(top = 8.dp))
            Row(Modifier.padding(top = 18.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.Top) {
                Checkbox(agree) { agree = it }
                Text(
                    buildAnnotatedString {
                        append("我已阅读并同意 Linbit 的")
                        withStyle(SpanStyle(color = p.brand)) { append("服务条款") }
                        append("与")
                        withStyle(SpanStyle(color = p.brand)) { append("隐私政策") }
                    },
                    style = lb(FontSize.small), color = p.textPrimary,
                )
            }
            AuthButton("注册", Modifier.padding(top = 16.dp), loading = vm.loading) { vm.submitRegister(agree, onNext) }
            OrDivider(Modifier.padding(vertical = 32.dp))
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                SocialButton(R.drawable.ic_apple, "通过 Apple 继续") { soon("Apple 注册即将开放") }
                SocialButton(R.drawable.ic_google, "通过 Google 继续", multicolor = true) { soon("Google 注册即将开放") }
            }
            Text(
                "已有账户，立即登录", Modifier.align(Alignment.CenterHorizontally).padding(top = 24.dp, bottom = 24.dp).pressable(onClick = onLogin),
                style = lb(FontSize.label, FontWeight.Medium), color = p.brand,
            )
        }
    }
    if (showCountry) CountrySheet(vm.countryCode, onPick = { vm.countryCode = it; showCountry = false }, onClose = { showCountry = false })
    // ref2 #8 挽留弹窗
    if (showRetain) {
        LbSheet(title = null, onClose = { showRetain = false }) {
            Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                LbImageIcon(R.drawable.ic_gift_big, 56.dp, Modifier.padding(top = 16.dp, bottom = 8.dp))
                Text(
                    buildAnnotatedString {
                        append("完成注册最高领 ")
                        withStyle(SpanStyle(color = p.brand)) { append("15,200 USDT") }
                    },
                    style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary,
                )
                Text("1分钟即可完成注册", style = lb(FontSize.caption), color = p.textSecondary)
                Row(Modifier.padding(top = 20.dp, bottom = 16.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    LbButton("退出", Modifier.weight(1f), kind = ButtonKind.SECONDARY) { showRetain = false; onClose() }
                    LbButton("继续", Modifier.weight(1f)) { showRetain = false }
                }
            }
        }
    }
}

// ───────────── 设置密码（注册第 3 步 / 重置密码第 3 步）─────────────
@Composable
internal fun SetPasswordScreen(
    vm: AuthViewModel, resetToken: String?, onBack: () -> Unit, onRegistered: () -> Unit, onReset: () -> Unit,
    onTokenExpired: () -> Unit, onCodeInvalid: () -> Unit,
) {
    val p = LB.p
    var pwd by remember { mutableStateOf("") }
    var pwd2 by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    Column(Modifier.fillMaxSize()) {
        AuthTopBar(isRoot = false, onLeft = onBack, onSupport = { vm.toast.show("在线客服即将开放") })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            AuthHeader(
                if (resetToken == null) "设置密码" else "重置密码",
                AnnotatedString(if (resetToken == null) vm.id.masked else "为保障资金安全，重置密码后 24 小时内禁止提现。"),
            )
            FieldLabel("密码")
            Box(Modifier.height(8.dp))
            LbTextField(pwd, { pwd = it; error = null }, "8-64 位，含字母和数字", secure = true, error = PasswordRule.hint(pwd))
            Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                PasswordRule.rules(pwd).forEach { (name, ok) ->
                    Text((if (ok) "✓ " else "○ ") + name, style = lb(FontSize.small), color = if (ok) p.up else p.textTertiary)
                }
            }
            if (resetToken != null) {
                Box(Modifier.height(20.dp))
                FieldLabel("确认密码")
                Box(Modifier.height(8.dp))
                LbTextField(pwd2, { pwd2 = it; error = null }, "再次输入密码", secure = true, error = error)
            }
            AuthButton("确认", Modifier.padding(top = 32.dp), loading = vm.loading) {
                if (!PasswordRule.ok(pwd)) { error = PasswordRule.hint(pwd) ?: "请输入密码"; return@AuthButton }
                vm.submitNewPassword(pwd, pwd2, resetToken, { error = it }, onRegistered, onReset, onTokenExpired, onCodeInvalid)
            }
        }
    }
}

// ───────────── 忘记密码 · 账户 ─────────────
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ForgotScreen(vm: AuthViewModel, onBack: () -> Unit, onNext: () -> Unit) {
    var showCountry by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize()) {
        AuthTopBar(isRoot = false, onLeft = onBack, onSupport = { vm.toast.show("在线客服即将开放") })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            AuthHeader("重置密码", AnnotatedString("为保障资金安全，重置密码后 24 小时内禁止提现。"))
            LabeledAccountField(vm) { showCountry = true }
            AuthButton("下一步", Modifier.padding(top = 32.dp), loading = vm.loading) { vm.submitForgot(onNext) }
        }
    }
    if (showCountry) CountrySheet(vm.countryCode, onPick = { vm.countryCode = it; showCountry = false }, onClose = { showCountry = false })
}

// ───────────── 底部弹层 ─────────────
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LbSheet(title: String?, onClose: () -> Unit, content: @Composable () -> Unit) {
    val p = LB.p
    ModalBottomSheet(
        onDismissRequest = onClose,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = p.bgElevated,
        shape = RoundedCornerShape(topStart = Radius.sheet, topEnd = Radius.sheet),
    ) {
        Column(Modifier.navigationBarsPadding()) {
            if (title != null) Text(title, Modifier.padding(horizontal = Spacing.pageH, vertical = 8.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
            content()
        }
    }
}

@Composable
private fun CountrySheet(selected: String, onPick: (String) -> Unit, onClose: () -> Unit) {
    val p = LB.p
    var q by remember { mutableStateOf("") }
    LbSheet("选择国家/地区", onClose) {
        LbTextField(q, { q = it }, "搜索", Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp))
        LazyColumn(Modifier.height(420.dp)) {
            items(CountryCodes.list.filter { q.isEmpty() || it.name.contains(q) || it.code.startsWith(q) }, key = { it.code }) { c ->
                Row(Modifier.fillMaxWidth().height(48.dp).pressable(scale = 1f) { onPick(c.code) }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                    Text("${c.flag}  ${c.name}", Modifier.weight(1f), style = lb(FontSize.body), color = p.textPrimary)
                    Text("+${c.code}", style = click.cowa.linbit.ui.theme.lbNum(FontSize.label), color = if (c.code == selected) p.brand else p.textSecondary)
                }
            }
        }
    }
}
