package click.cowa.linbit.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiSecurityOverview
import click.cowa.linbit.core.generated.ApiSendCodeAuthedRequest
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

// 账户安全二级页：修改登录密码 / 换绑邮箱手机 / 资金密码 / 防钓鱼码 / 会话与设备 / 通行密钥。
// 交互与 iOS SecurityCenter.swift、Web features/Security.tsx 对齐；所有敏感操作的二次验证规则以服务端为准。

private fun digits6(s: String) = s.filter(Char::isDigit).take(6)

/** 页面骨架：导航栏 + 可滚动内容 + 底部主按钮 */
@Composable
private fun SecurityFormPage(
    title: String,
    onBack: () -> Unit,
    button: String,
    enabled: Boolean,
    loading: Boolean,
    onSubmit: () -> Unit,
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = LB.p
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar(title, onBack)
        Column(
            Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(top = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            content = content,
        )
        LbButton(button, Modifier.padding(Spacing.pageH), enabled = enabled, loading = loading, onClick = onSubmit)
    }
}

/** 验证码输入框 + 右侧「获取验证码 / 倒计时」 */
@Composable
private fun CodeField(value: String, onValue: (String) -> Unit, placeholder: String, cooldown: Int, sending: Boolean, onSend: () -> Unit) {
    val p = LB.p
    Row(verticalAlignment = Alignment.CenterVertically) {
        LbTextField(value, { onValue(digits6(it)) }, placeholder, Modifier.weight(1f), keyboardType = KeyboardType.NumberPassword, height = 48.dp)
        Spacer(Modifier.width(10.dp))
        Text(
            when { cooldown > 0 -> "${cooldown}s"; sending -> "发送中"; else -> "获取验证码" },
            Modifier.pressable(scale = 1f) { if (cooldown == 0 && !sending) onSend() },
            style = lb(FontSize.small, FontWeight.Medium),
            color = if (cooldown > 0) p.textTertiary else p.brand,
        )
    }
}

/** 二次验证：已开启 TOTP 输动态码，否则获取并输入发到当前联系方式的验证码 */
@Composable
private fun SecondFactorField(useTotp: Boolean, value: String, onValue: (String) -> Unit, vm: SecuritySettingsViewModel) {
    val s by vm.state.collectAsStateWithLifecycle()
    if (useTotp) {
        LbTextField(value, { onValue(digits6(it)) }, "6 位身份验证器验证码", keyboardType = KeyboardType.NumberPassword, height = 48.dp)
    } else {
        CodeField(value, onValue, "6 位安全验证码（邮箱 / 短信）", s.securityCooldown, s.sending) { vm.sendSecurityCode() }
    }
}

@Composable
private fun FormError(text: String?) {
    if (!text.isNullOrBlank()) Text(text, style = lb(FontSize.small), color = LB.p.down)
}

private fun ApiSecurityOverview?.totpOn() = this?.twoFactor?.enabled == true

// ---- 修改登录密码 ----
@Composable
fun ChangePasswordScreen(sec: ApiSecurityOverview?, vm: SecuritySettingsViewModel, onBack: () -> Unit, onDone: () -> Unit) {
    val s by vm.state.collectAsStateWithLifecycle()
    var old by remember { mutableStateOf("") }
    var new1 by remember { mutableStateOf("") }
    var new2 by remember { mutableStateOf("") }
    var factor by remember { mutableStateOf("") }
    val useTotp = sec.totpOn()
    val hint = when {
        new1.isNotEmpty() && new1.length < 8 -> "新密码至少 8 位"
        new1.isNotEmpty() && new1 == old -> "新密码不能与原密码相同"
        new2.isNotEmpty() && new2 != new1 -> "两次输入的新密码不一致"
        else -> null
    }
    LaunchedEffect(Unit) { vm.clearError() }
    SecurityFormPage(
        "修改登录密码", onBack, "确认修改",
        enabled = old.isNotEmpty() && new1.length >= 8 && new1 == new2 && new1 != old && factor.length == 6,
        loading = s.submitting,
        onSubmit = { vm.changePassword(old, new1, SecondFactor(useTotp, factor), onDone) },
    ) {
        InfoNote("修改后其他设备将退出登录，且 ${sec?.policy?.withdrawLockHours ?: 24} 小时内禁止提现与内部转账。", warning = true)
        LbTextField(old, { old = it }, "原登录密码", secure = true, height = 48.dp)
        LbTextField(new1, { new1 = it }, "新登录密码（至少 8 位，建议包含字母、数字和符号）", secure = true, height = 48.dp)
        LbTextField(new2, { new2 = it }, "再次输入新密码", secure = true, height = 48.dp)
        FormError(hint)
        SecondFactorField(useTotp, factor, { factor = it }, vm)
        FormError(s.error)
    }
}

// ---- 资金密码：设置 / 修改 / 重置 ----
@Composable
fun FundPasswordScreen(sec: ApiSecurityOverview?, vm: SecuritySettingsViewModel, onBack: () -> Unit, onDone: () -> Unit) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    val isSet = sec?.fundPasswordSet == true
    var mode by remember { mutableStateOf(if (isSet) FundPasswordMode.CHANGE else FundPasswordMode.SET) }
    var old by remember { mutableStateOf("") }
    var pwd by remember { mutableStateOf("") }
    var pwd2 by remember { mutableStateOf("") }
    var factor by remember { mutableStateOf("") }
    val useTotp = sec.totpOn()
    val title = when (mode) { FundPasswordMode.SET -> "设置资金密码"; FundPasswordMode.CHANGE -> "修改资金密码"; FundPasswordMode.RESET -> "重置资金密码" }
    LaunchedEffect(Unit) { vm.clearError() }
    SecurityFormPage(
        title, onBack, "确认",
        enabled = pwd.length == 6 && pwd == pwd2 && factor.length == 6 && (mode != FundPasswordMode.CHANGE || old.length == 6),
        loading = s.submitting,
        onSubmit = { vm.fundPassword(mode, pwd, if (mode == FundPasswordMode.CHANGE) old else null, SecondFactor(useTotp, factor), onDone) },
    ) {
        InfoNote("资金密码为 6 位数字，用于提现与内部转账，不能与登录密码相同。设置或修改后 ${sec?.policy?.withdrawLockHours ?: 24} 小时内禁止提现。", warning = true)
        if (isSet) {
            Row(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.md)).padding(4.dp)) {
                listOf(FundPasswordMode.CHANGE to "修改（记得原密码）", FundPasswordMode.RESET to "忘记 / 重置").forEach { (m, t) ->
                    val sel = mode == m
                    Text(
                        t,
                        Modifier.weight(1f)
                            .background(if (sel) p.bg else androidx.compose.ui.graphics.Color.Transparent, RoundedCornerShape(Radius.sm))
                            .pressable(scale = 1f) { mode = m; vm.clearError() }
                            .padding(vertical = 8.dp),
                        style = lb(FontSize.label, if (sel) FontWeight.SemiBold else FontWeight.Normal),
                        color = p.textPrimary,
                        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                    )
                }
            }
            sec?.fundPasswordLockedUntil?.let { InfoNote("资金密码错误次数过多，已锁定至 $it。重置资金密码可解除锁定。", warning = true) }
        }
        if (mode == FundPasswordMode.CHANGE) {
            LbTextField(old, { old = digits6(it) }, "原资金密码（6 位数字）", secure = true, keyboardType = KeyboardType.NumberPassword, height = 48.dp)
        }
        LbTextField(pwd, { pwd = digits6(it) }, if (mode == FundPasswordMode.SET) "资金密码（6 位数字）" else "新资金密码（6 位数字）", secure = true, keyboardType = KeyboardType.NumberPassword, height = 48.dp)
        LbTextField(pwd2, { pwd2 = digits6(it) }, "再次输入资金密码", secure = true, keyboardType = KeyboardType.NumberPassword, height = 48.dp)
        FormError(if (pwd2.length == 6 && pwd2 != pwd) "两次输入的资金密码不一致" else null)
        SecondFactorField(useTotp, factor, { factor = it }, vm)
        FormError(s.error)
    }
}

// ---- 防钓鱼码 ----
@Composable
fun AntiPhishingScreen(sec: ApiSecurityOverview?, vm: SecuritySettingsViewModel, onBack: () -> Unit, onDone: () -> Unit) {
    val s by vm.state.collectAsStateWithLifecycle()
    var code by remember { mutableStateOf("") }
    var factor by remember { mutableStateOf("") }
    val useTotp = sec.totpOn()
    val valid = Regex("^[A-Za-z0-9]{4,20}$").matches(code)
    LaunchedEffect(Unit) { vm.clearError() }
    SecurityFormPage(
        "设置防钓鱼码", onBack, "确认",
        enabled = valid && factor.length == 6, loading = s.submitting,
        onSubmit = { vm.setAntiPhishing(code, SecondFactor(useTotp, factor), onDone) },
    ) {
        InfoNote("设置后，Linbit 发出的所有邮件开头都会显示该防钓鱼码。没有防钓鱼码的邮件，请勿点击其中的链接。")
        sec?.antiPhishingHint?.let { Text("当前：$it", style = lb(FontSize.small), color = LB.p.textSecondary) }
        LbTextField(code, { code = it.filter(Char::isLetterOrDigit).take(20) }, "4–20 位字母或数字", height = 48.dp)
        FormError(if (code.isNotEmpty() && !valid) "防钓鱼码为 4–20 位字母或数字" else null)
        SecondFactorField(useTotp, factor, { factor = it }, vm)
        FormError(s.error)
    }
}

// ---- 换绑邮箱 / 手机 ----
@Composable
fun ContactChangeScreen(email: Boolean, sec: ApiSecurityOverview?, vm: SecuritySettingsViewModel, onBack: () -> Unit, onDone: () -> Unit) {
    val s by vm.state.collectAsStateWithLifecycle()
    var value by remember { mutableStateOf("") }
    var cc by remember { mutableStateOf("86") }
    var newCode by remember { mutableStateOf("") }
    var oldCode by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    val useTotp = sec.totpOn()
    val current = if (email) sec?.email else sec?.phone
    // 旧联系方式验证码：更换已绑定的同类联系方式时，服务端在「被替换的那个」上校验，必须指定渠道；
    // 新绑定（原来没有这类联系方式）时发到账户现有联系方式，由服务端默认选择。
    val oldChannel = if (current.isNullOrBlank()) null else if (email) ApiSendCodeAuthedRequest.Channel.EMAIL else ApiSendCodeAuthedRequest.Channel.SMS
    val oldLabel = when {
        !current.isNullOrBlank() -> "原${if (email) "邮箱" else "手机"}验证码（$current）"
        else -> "当前联系方式验证码（${sec?.email ?: sec?.phone ?: "已绑定的邮箱 / 手机"}）"
    }
    val valueOk = if (email) Regex("^\\S+@\\S+\\.\\S+$").matches(value.trim()) else Regex("^\\d{5,15}$").matches(value.trim())
    LaunchedEffect(Unit) { vm.clearError() }
    SecurityFormPage(
        if (email) (if (current.isNullOrBlank()) "绑定邮箱" else "更换邮箱") else (if (current.isNullOrBlank()) "绑定手机" else "更换手机"),
        onBack, "确认",
        enabled = valueOk && newCode.length == 6 && oldCode.length == 6 && (!useTotp || totp.length == 6) && (email || cc.isNotBlank()),
        loading = s.submitting,
        onSubmit = { vm.changeContact(email, value.trim(), if (email) null else cc.trim(), newCode, oldCode, if (useTotp) totp else null, onDone) },
    ) {
        InfoNote("为保障资产安全，更换后 ${sec?.policy?.withdrawLockHours ?: 24} 小时内禁止提现与内部转账。", warning = true)
        if (email) {
            LbTextField(value, { value = it.trim() }, "新邮箱", keyboardType = KeyboardType.Email, height = 48.dp)
        } else {
            Row(verticalAlignment = Alignment.CenterVertically) {
                LbTextField(cc, { cc = it.filter(Char::isDigit).take(4) }, "区号", Modifier.width(88.dp), keyboardType = KeyboardType.Number, height = 48.dp,
                    leading = { Text("+", style = lb(FontSize.body), color = LB.p.textSecondary) })
                Spacer(Modifier.width(8.dp))
                LbTextField(value, { value = it.filter(Char::isDigit).take(15) }, "新手机号", Modifier.weight(1f), keyboardType = KeyboardType.Phone, height = 48.dp)
            }
        }
        CodeField(newCode, { newCode = it }, "新${if (email) "邮箱" else "手机"}验证码", s.newContactCooldown, s.sending) {
            if (!valueOk) vm.toast.show(if (email) "请输入正确的新邮箱" else "请输入正确的手机号")
            else vm.sendNewContactCode(if (email) value.trim() else null, if (email) null else value.trim(), if (email) null else cc.trim())
        }
        CodeField(oldCode, { oldCode = it }, oldLabel, s.securityCooldown, s.sending) { vm.sendSecurityCode(oldChannel) }
        if (useTotp) LbTextField(totp, { totp = digits6(it) }, "6 位身份验证器验证码", keyboardType = KeyboardType.NumberPassword, height = 48.dp)
        FormError(s.error)
    }
}

// ---- 登录会话 / 设备 ----
@Composable
fun SessionsScreen(vm: SecuritySettingsViewModel, onBack: () -> Unit) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf<Pair<String, () -> Unit>?>(null) }
    LaunchedEffect(Unit) { vm.clearError(); vm.loadSessionsAndDevices() }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("登录设备与会话", onBack)
        when {
            s.listLoading && s.sessions == null -> ProfileLoading()
            s.error != null && s.sessions == null -> ProfileError(s.error ?: "") { vm.loadSessionsAndDevices() }
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                InfoNote("如发现陌生设备，请立即退出该会话并修改登录密码。")
                val sessions = s.sessions.orEmpty()
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("当前有效会话", Modifier.weight(1f), style = lb(FontSize.label, FontWeight.SemiBold), color = p.textPrimary)
                    if (sessions.any { it.current != true }) Text("退出其他所有设备", Modifier.pressable(scale = 1f) {
                        confirm = "退出除本机外的所有会话？" to { vm.revokeOtherSessions() }
                    }, style = lb(FontSize.small, FontWeight.Medium), color = p.brand)
                }
                if (sessions.isEmpty()) Text("暂无会话", style = lb(FontSize.small), color = p.textSecondary)
                sessions.forEach { x ->
                    SessionCard(
                        title = x.name ?: x.userAgent ?: x.deviceId ?: "未知设备",
                        detail = listOfNotNull(x.ip?.let { "IP $it" }, x.createdAt?.let { "登录于 ${it.take(16).replace('T', ' ')}" }).joinToString(" · "),
                        current = x.current == true,
                        action = "退出",
                    ) { x.id?.let { id -> confirm = "退出该会话？" to { vm.revokeSession(id) } } }
                }
                Spacer(Modifier.height(8.dp))
                Text("登录过的设备", style = lb(FontSize.label, FontWeight.SemiBold), color = p.textPrimary)
                Text("移除设备后，该设备再次登录需要重新验证。", style = lb(FontSize.small), color = p.textSecondary)
                val devices = s.devices.orEmpty()
                if (devices.isEmpty()) Text("暂无设备", style = lb(FontSize.small), color = p.textSecondary)
                devices.forEach { d ->
                    SessionCard(
                        title = d.name ?: d.userAgent ?: "未知设备",
                        detail = listOfNotNull(d.ip?.let { "IP $it" }, "最近 ${d.lastSeen.take(16).replace('T', ' ')}").joinToString(" · "),
                        current = d.current == true,
                        action = "移除",
                    ) { confirm = "移除该设备？会同时退出它的登录会话。" to { vm.removeDevice(d.deviceId) } }
                }
                FormError(if (s.sessions != null) s.error else null)
                Spacer(Modifier.height(24.dp))
            }
        }
    }
    confirm?.let { (msg, run) ->
        AlertDialog(
            onDismissRequest = { confirm = null }, title = { Text("确认") }, text = { Text(msg) },
            confirmButton = { TextButton(onClick = { confirm = null; run() }) { Text("确定", color = p.down) } },
            dismissButton = { TextButton(onClick = { confirm = null }) { Text("取消") } },
        )
    }
}

@Composable
private fun SessionCard(title: String, detail: String, current: Boolean, action: String, onAction: () -> Unit) {
    val p = LB.p
    Row(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.lg)).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(title, style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary, maxLines = 1)
                if (current) { Spacer(Modifier.width(6.dp)); StatusTag("本机", p.up) }
            }
            if (detail.isNotBlank()) Text(detail, style = lb(FontSize.small), color = p.textSecondary, maxLines = 2)
        }
        if (!current) {
            Spacer(Modifier.width(8.dp))
            Text(action, Modifier.pressable(scale = 1f, onClick = onAction), style = lb(FontSize.small, FontWeight.Medium), color = p.down)
        }
    }
}

// ---- 通行密钥：查看与删除（添加需要 Credential Manager，Android 暂未开放）----
@Composable
fun PasskeysScreen(vm: SecuritySettingsViewModel, onBack: () -> Unit) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { vm.clearError(); vm.loadPasskeys() }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("通行密钥", onBack)
        when {
            s.listLoading && s.passkeys == null -> ProfileLoading()
            s.error != null && s.passkeys == null -> ProfileError(s.error ?: "") { vm.loadPasskeys() }
            !s.passkeysEnabled -> Box(Modifier.padding(Spacing.pageH)) { InfoNote("通行密钥服务暂未启用") }
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                InfoNote("可在网页端或 iOS App 添加通行密钥；Android 端目前支持查看和删除。")
                val list = s.passkeys.orEmpty()
                if (list.isEmpty()) Text("尚未添加通行密钥", style = lb(FontSize.small), color = p.textSecondary)
                list.forEach { k ->
                    SessionCard(
                        title = k.name ?: "通行密钥",
                        detail = listOfNotNull(k.createdAt?.let { "添加于 ${it.take(10)}" }, k.lastUsedAt?.let { "最近使用 ${it.take(10)}" }).joinToString(" · "),
                        current = false, action = "删除",
                    ) { confirm = k.id }
                }
                FormError(if (s.passkeys != null) s.error else null)
            }
        }
    }
    confirm?.let { id ->
        AlertDialog(
            onDismissRequest = { confirm = null }, title = { Text("删除通行密钥") }, text = { Text("删除后将无法再用它登录，确定删除？") },
            confirmButton = { TextButton(onClick = { confirm = null; vm.deletePasskey(id) }) { Text("删除", color = p.down) } },
            dismissButton = { TextButton(onClick = { confirm = null }) { Text("取消") } },
        )
    }
}
