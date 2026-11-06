package click.cowa.linbit.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiSubAccount
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

@Composable
fun SubAccountsScreen(onBack: () -> Unit, onSwitch: () -> Unit, vm: SubAccountsViewModel = hiltViewModel()) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    var create by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { vm.load() }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("子账户", onBack, trailing = {
            Text("切换账号", Modifier.pressable(scale = 1f, onClick = onSwitch), style = lb(FontSize.caption), color = p.textPrimary)
        })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            InfoNote("子账户用于隔离策略与资金。创建子账户需要母账户已开启身份验证器。")
            when {
                s.loading && s.items.isEmpty() -> ProfileLoading()
                s.error != null && s.items.isEmpty() -> ProfileError(s.error.orEmpty()) { vm.load() }
                s.items.isEmpty() -> EmptyState("暂无子账户，可用于隔离策略与资金")
                else -> s.items.forEach { SubAccountCard(it) }
            }
            Text("子账户没有免密切换会话接口；需要切换时，请退出后使用该子账户的登录邮箱重新登录。", style = lb(FontSize.small), color = p.textTertiary)
        }
        LbButton("创建子账户", Modifier.padding(Spacing.pageH), onClick = { create = true })
    }
    if (create) CreateSubAccountSheet(s, vm, onClose = { create = false })
}

@Composable
private fun SubAccountCard(account: ApiSubAccount) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().border(1.dp, p.border, RoundedCornerShape(Radius.lg)).padding(14.dp), verticalArrangement = Arrangement.spacedBy(7.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(ProfileLogic.subDisplayName(account), Modifier.weight(1f), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            StatusTag(if (account.status == ApiSubAccount.Status.FROZEN) "已冻结" else "正常", if (account.status == ApiSubAccount.Status.FROZEN) p.down else p.up)
        }
        Text("UID ${account.uid ?: "—"}", style = lbNum(FontSize.small), color = p.textSecondary)
        account.loginEmail?.let { Text(it, style = lb(FontSize.small), color = p.textSecondary) }
        Text(if (account.loginEnabled == true) "允许账号密码登录" else "尚未开启账号密码登录", style = lb(FontSize.small), color = if (account.loginEnabled == true) p.up else p.textTertiary)
        account.note?.takeIf { it.isNotBlank() }?.let { Text(it, style = lb(FontSize.small), color = p.textTertiary) }
    }
}

@Composable
private fun CreateSubAccountSheet(s: SubAccountsUiState, vm: SubAccountsViewModel, onClose: () -> Unit) {
    var username by remember { mutableStateOf("") }
    var nickname by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    val nameError = ProfileLogic.subAccountNameError(username)
    LbSheet("创建子账户", onClose, showClose = true) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(bottom = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Column {
                FieldLabel("用户名")
                Spacer(Modifier.height(6.dp))
                LbTextField(username, { username = it.lowercase().filter { c -> c.isLowerCase() || c.isDigit() || c == '_' }.take(20) }, "3-20 位小写字母、数字或下划线", error = nameError)
            }
            Column {
                FieldLabel("昵称（选填）")
                Spacer(Modifier.height(6.dp))
                LbTextField(nickname, { nickname = it.take(32) }, "便于识别")
            }
            Column {
                FieldLabel("备注（选填）")
                Spacer(Modifier.height(6.dp))
                LbTextField(note, { note = it.take(200) }, "如：网格策略专用")
            }
            s.error?.let { Text(it, style = lb(FontSize.small), color = LB.p.down) }
            LbButton("创建", enabled = username.isNotEmpty() && nameError == null, loading = s.submitting) { vm.create(username, nickname, note, onClose) }
        }
    }
}
