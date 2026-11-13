package click.cowa.linbit.feature.profile

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiSecurityLevel
import click.cowa.linbit.feature.assets.QrImage
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

private enum class SecurityPage { CENTER, BIND, DISABLE, PASSWORD, EMAIL, PHONE, FUND, ANTI_PHISHING, SESSIONS, PASSKEYS }

@Composable
fun SecurityHost(
    onBack: () -> Unit,
    vm: SecurityViewModel = hiltViewModel(),
    settings: SecuritySettingsViewModel = hiltViewModel(),
) {
    var page by remember { mutableStateOf(SecurityPage.CENTER) }
    val s by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.load() }
    val back = { page = SecurityPage.CENTER }
    // 二级页提交成功：回到安全中心并刷新概览（安全等级、提现保护期等）
    val done = { page = SecurityPage.CENTER; vm.load() }
    BackHandler { if (page == SecurityPage.CENTER) onBack() else back() }
    when (page) {
        SecurityPage.CENTER -> SecurityCenterScreen(vm, onBack, onBind = { page = SecurityPage.BIND; vm.beginBinding() }, onDisable = { page = SecurityPage.DISABLE }, onOpen = { page = it })
        SecurityPage.BIND -> TotpSetupScreen(vm, onBack = back)
        SecurityPage.DISABLE -> DisableTotpScreen(vm, onBack = back)
        SecurityPage.PASSWORD -> ChangePasswordScreen(s.security, settings, onBack = back, onDone = done)
        SecurityPage.EMAIL -> ContactChangeScreen(email = true, s.security, settings, onBack = back, onDone = done)
        SecurityPage.PHONE -> ContactChangeScreen(email = false, s.security, settings, onBack = back, onDone = done)
        SecurityPage.FUND -> FundPasswordScreen(s.security, settings, onBack = back, onDone = done)
        SecurityPage.ANTI_PHISHING -> AntiPhishingScreen(s.security, settings, onBack = back, onDone = done)
        SecurityPage.SESSIONS -> SessionsScreen(settings, onBack = back)
        SecurityPage.PASSKEYS -> PasskeysScreen(settings, onBack = { back(); vm.load() })
    }
}

@Composable
private fun SecurityCenterScreen(vm: SecurityViewModel, onBack: () -> Unit, onBind: () -> Unit, onDisable: () -> Unit, onOpen: (SecurityPage) -> Unit) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    val security = s.security
    val enabled = security?.twoFactor?.enabled == true
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("账户安全", onBack)
        when {
            s.loading && security == null -> ProfileLoading()
            s.error != null && security == null -> ProfileError(s.error ?: "") { vm.load() }
            else -> Column(Modifier.verticalScroll(rememberScrollState())) {
                security?.withdrawLockedUntil?.let { until ->
                    Box(Modifier.padding(horizontal = Spacing.pageH).padding(top = 8.dp)) {
                        InfoNote("${security.withdrawLockReason ?: "安全设置变更"}，$until 前暂停提现与内部转账", warning = true)
                    }
                }
                security?.securityLevel?.let { SecurityLevelCard(it) }
                Text("账户信息", Modifier.padding(horizontal = Spacing.pageH).padding(top = 18.dp, bottom = 4.dp), style = lb(FontSize.caption), color = p.textSecondary)
                ProfileRow("邮箱", security?.email ?: "未绑定") { onOpen(SecurityPage.EMAIL) }
                ProfileRow("手机", security?.phone ?: "未绑定") { onOpen(SecurityPage.PHONE) }
                Text("登录验证", Modifier.padding(horizontal = Spacing.pageH).padding(top = 18.dp, bottom = 4.dp), style = lb(FontSize.caption), color = p.textSecondary)
                ProfileRow("身份验证器", if (enabled) "已开启" else "未开启") { if (enabled) onDisable() else onBind() }
                ProfileRow("通行密钥", if ((security?.passkeysCount ?: 0) > 0) "${security?.passkeysCount} 个" else "未添加") { onOpen(SecurityPage.PASSKEYS) }
                ProfileRow("登录密码", "修改") { onOpen(SecurityPage.PASSWORD) }
                ProfileRow("登录设备与会话", "${security?.recentDevices?.size ?: 0} 台设备") { onOpen(SecurityPage.SESSIONS) }
                Text("资金安全", Modifier.padding(horizontal = Spacing.pageH).padding(top = 18.dp, bottom = 4.dp), style = lb(FontSize.caption), color = p.textSecondary)
                ProfileRow("资金密码", if (security?.fundPasswordSet == true) "已设置" else "未设置") { onOpen(SecurityPage.FUND) }
                ProfileRow("防钓鱼码", if (security?.antiPhishingSet == true) (security.antiPhishingHint ?: "已设置") else "未设置") { onOpen(SecurityPage.ANTI_PHISHING) }
                Spacer(Modifier.height(32.dp))
            }
        }
    }
}

@Composable
private fun SecurityLevelCard(level: ApiSecurityLevel) {
    val p = LB.p
    val color = when (level.level) {
        ApiSecurityLevel.Level.HIGH -> p.up
        ApiSecurityLevel.Level.MEDIUM -> androidx.compose.ui.graphics.Color(0xFFD97706)
        else -> p.down
    }
    val text = when (level.level) {
        ApiSecurityLevel.Level.HIGH -> "高"
        ApiSecurityLevel.Level.MEDIUM -> "中"
        else -> "低"
    }
    Column(Modifier.fillMaxWidth().padding(Spacing.pageH).background(p.fill, RoundedCornerShape(Radius.xl)).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("安全等级", style = lb(FontSize.label), color = p.textSecondary)
            Spacer(Modifier.width(8.dp))
            Text(text, style = lb(22.sp, FontWeight.Bold), color = color)
            Spacer(Modifier.weight(1f))
            Text("${level.score}/${level.max}", style = lbNum(FontSize.label, FontWeight.Medium), color = p.textSecondary)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            repeat(maxOf(level.max, 1)) { i -> Box(Modifier.weight(1f).height(4.dp).background(if (i < level.score) color else p.fillStrong, RoundedCornerShape(Radius.pill))) }
        }
        val labels = mapOf("emailVerified" to "邮箱验证", "phoneVerified" to "手机验证", "twoFactor" to "二次验证", "fundPassword" to "资金密码", "antiPhishing" to "防钓鱼码")
        level.items.orEmpty().chunked(2).forEach { row ->
            Row(Modifier.fillMaxWidth()) {
                row.forEach { item ->
                    Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                        CheckCircle(item.ok)
                        Spacer(Modifier.width(5.dp))
                        Text(labels[item.key?.value] ?: item.key?.value ?: "安全项", style = lb(FontSize.small), color = if (item.ok) p.textPrimary else p.textSecondary)
                    }
                }
                if (row.size == 1) Spacer(Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun TotpSetupScreen(vm: SecurityViewModel, onBack: () -> Unit) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    val clipboard = LocalClipboardManager.current
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("设置身份验证器", onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            InfoNote("绑定身份验证器后，提现等敏感操作需要动态验证码。", warning = true)
            Spacer(Modifier.height(20.dp))
            TotpStep(1, "下载身份验证器", "从应用商店下载您可用的身份验证器。")
            Spacer(Modifier.height(16.dp))
            TotpStep(2, "扫描二维码或手动输入密语", "在验证器中添加账号，激活验证令牌。")
            when {
                s.bindingLoading -> Box(Modifier.fillMaxWidth().height(210.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = p.brand) }
                s.binding == null -> ProfileError(s.error ?: "暂时无法获取绑定信息") { vm.beginBinding() }
                else -> Column(Modifier.fillMaxWidth().padding(vertical = 16.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(16.dp)) {
                    QrImage(s.binding!!.otpauthUri, size = 150.dp)
                    Row(Modifier.border(1.dp, p.border, RoundedCornerShape(Radius.md)).pressable(scale = 1f) {
                        clipboard.setText(AnnotatedString(s.binding!!.secret)); vm.toast.show("密钥已复制")
                    }.padding(horizontal = 12.dp).height(38.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(s.binding!!.secret, style = lbNum(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                        Spacer(Modifier.width(8.dp))
                        Text("复制", style = lb(FontSize.small), color = p.brand)
                    }
                }
            }
            TotpStep(3, "输入身份验证器验证码")
            LbTextField(s.code, vm::code, "6 位身份验证器验证码", Modifier.padding(top = 10.dp), keyboardType = KeyboardType.NumberPassword, height = 48.dp)
            Spacer(Modifier.height(16.dp))
            TotpStep(4, "输入安全验证码", "验证码将发送至您已绑定的邮箱或手机。")
            Row(Modifier.padding(top = 10.dp), verticalAlignment = Alignment.Top) {
                LbTextField(s.securityCode, vm::securityCode, "6 位安全验证码", Modifier.weight(1f), keyboardType = KeyboardType.NumberPassword, error = s.error, height = 48.dp)
                TextButton(onClick = vm::sendSecurityCode, enabled = !s.sending && s.cooldown == 0, modifier = Modifier.height(48.dp)) {
                    Text(
                        when { s.cooldown > 0 -> "${s.cooldown}s"; s.sending -> "发送中"; else -> "获取验证码" },
                        style = lb(FontSize.small, FontWeight.Medium), color = if (s.cooldown > 0) p.textTertiary else p.brand,
                    )
                }
            }
            Spacer(Modifier.height(24.dp))
        }
        LbButton("确认", Modifier.padding(Spacing.pageH), enabled = s.code.length == 6 && s.securityCode.length == 6 && s.binding != null, loading = s.submitting) { vm.verify(onBack) }
    }
}

@Composable
private fun TotpStep(number: Int, title: String, subtitle: String? = null) {
    val p = LB.p
    Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(16.dp).background(p.textPrimary, RoundedCornerShape(Radius.pill)), contentAlignment = Alignment.Center) {
                Text("$number", style = lb(10.sp, FontWeight.Bold), color = p.bg)
            }
            Spacer(Modifier.width(6.dp))
            Text(title, style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
        }
        if (subtitle != null) Text(subtitle, Modifier.padding(start = 22.dp, top = 4.dp), style = lb(FontSize.small), color = p.textSecondary)
    }
}

@Composable
private fun DisableTotpScreen(vm: SecurityViewModel, onBack: () -> Unit) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("关闭身份验证器", onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            InfoNote("关闭后账户安全等级将降低，提现与安全设置可能受到临时限制。", warning = true)
            Text("身份验证器已开启", style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            Text("登录、提现及安全设置变更需要验证。", style = lb(FontSize.small), color = p.textSecondary)
            LbTextField(s.code, vm::code, "6 位身份验证器验证码", keyboardType = KeyboardType.NumberPassword, error = s.error, height = 48.dp)
        }
        LbButton("关闭身份验证器", Modifier.padding(Spacing.pageH), kind = ButtonKind.SELL, enabled = s.code.length == 6, loading = s.submitting) { confirm = true }
    }
    if (confirm) AlertDialog(
        onDismissRequest = { confirm = false }, title = { Text("确认关闭") }, text = { Text("关闭身份验证器会降低账户安全性，确定继续？") },
        confirmButton = { TextButton(onClick = { confirm = false; vm.disable(onBack) }) { Text("关闭", color = p.down) } },
        dismissButton = { TextButton(onClick = { confirm = false }) { Text("取消") } },
    )
}
