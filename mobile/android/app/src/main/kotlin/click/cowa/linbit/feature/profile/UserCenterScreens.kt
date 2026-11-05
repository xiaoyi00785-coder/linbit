package click.cowa.linbit.feature.profile

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.launch
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.R
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.feature.assets.AssetsNavBar
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

enum class ProfilePage { HUB, SWITCH, SECURITY, KYC, SUBACCOUNTS }

@Composable
fun UserCenterHost(
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogout: () -> Unit,
    onClose: () -> Unit,
    onLoginOther: () -> Unit,
    vm: ProfileViewModel = hiltViewModel(),
) {
    var page by remember { mutableStateOf(ProfilePage.HUB) }
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.load() }
    LaunchedEffect(loggedIn) { if (!loggedIn) page = ProfilePage.HUB }
    BackHandler { if (page == ProfilePage.HUB) onClose() else page = ProfilePage.HUB }
    when (page) {
        ProfilePage.HUB -> UserCenterScreen(themeMode, onTheme, onLogout, onClose, onOpen = { page = it }, vm = vm)
        ProfilePage.SWITCH -> AccountSwitchScreen(onBack = { page = ProfilePage.HUB }, onAdd = onLoginOther, vm = vm, onLogout = onLogout)
        ProfilePage.SECURITY -> SecurityHost(onBack = { page = ProfilePage.HUB; vm.load(silent = true) })
        ProfilePage.KYC -> KycHost(onBack = { page = ProfilePage.HUB; vm.load(silent = true) })
        ProfilePage.SUBACCOUNTS -> SubAccountsScreen(onBack = { page = ProfilePage.HUB }, onSwitch = { page = ProfilePage.SWITCH })
    }
}

@Composable
fun UserCenterScreen(
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogout: () -> Unit,
    onClose: () -> Unit,
    onOpen: (ProfilePage) -> Unit,
    vm: ProfileViewModel,
) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    val a = s.account
    var tab by remember { mutableIntStateOf(0) }
    var logoutConfirm by remember { mutableStateOf(false) }
    var editingNick by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("用户中心", onClose, trailing = {
            LbIcon(R.drawable.ic_mc_transfer_square_light, 22.dp, p.textPrimary, Modifier.pressable(scale = 1f) { onOpen(ProfilePage.SWITCH) })
        })
        when {
            s.loading && a == null -> ProfileLoading()
            s.error != null && a == null -> ProfileError(s.error ?: "") { vm.load() }
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
                Column(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    Box { ProfileAvatar(a?.avatarUrl); Box(Modifier.align(Alignment.BottomEnd).size(22.dp).background(p.bg, RoundedCornerShape(Radius.pill)).padding(4.dp)) { Text("•", color = p.textPrimary) } }
                    Text(ProfileLogic.displayName(a), Modifier.padding(top = 10.dp), style = lb(18.sp, FontWeight.SemiBold), color = p.textPrimary)
                    Text(ProfileLogic.contact(a), style = lb(FontSize.caption), color = p.textSecondary)
                }
                PrimaryTabs(listOf("个人资料", "偏好设置", "通知设置"), tab, { tab = it }, Modifier.padding(horizontal = Spacing.pageH).padding(top = 24.dp), size = FontSize.label, underline = true)
                Spacer(Modifier.height(8.dp))
                when (tab) {
                    0 -> Column {
                        Row(Modifier.fillMaxWidth().height(50.dp).pressable(scale = 1f) {
                            a?.uid?.let { clipboard.setText(AnnotatedString(it)); vm.toast.show("UID 已复制") }
                        }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                            Text("UID", style = lb(FontSize.label), color = p.textPrimary)
                            Spacer(Modifier.weight(1f))
                            Text(a?.uid ?: "—", style = lbNum(FontSize.label), color = p.textSecondary)
                            Spacer(Modifier.width(8.dp))
                            LbIcon(R.drawable.ic_doc_text, 14.dp, p.textSecondary)
                        }
                        ProfileRow("昵称", a?.nickname ?: "未设置") { editingNick = true }
                        ProfileRow("身份认证", ProfileLogic.kycLabel(s.kyc)) { onOpen(ProfilePage.KYC) }
                        ProfileRow("账户安全") { onOpen(ProfilePage.SECURITY) }
                        ProfileRow("子账户") { onOpen(ProfilePage.SUBACCOUNTS) }
                        ProfileRow("切换账号") { onOpen(ProfilePage.SWITCH) }
                        ProfileRow("退出当前账户", destructive = true) { logoutConfirm = true }
                    }
                    1 -> Column {
                        Text("外观", Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp, bottom = 10.dp), style = lb(FontSize.caption), color = p.textSecondary)
                        Row(Modifier.padding(horizontal = Spacing.pageH), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            ThemeMode.entries.forEach { mode ->
                                LbButton(mode.title, Modifier.weight(1f), kind = if (mode == themeMode) ButtonKind.PRIMARY else ButtonKind.SECONDARY, height = 40.dp) { onTheme(mode) }
                            }
                        }
                        Text("计价货币", Modifier.padding(horizontal = Spacing.pageH).padding(top = 28.dp), style = lb(FontSize.caption), color = p.textSecondary)
                        ProfileRow("默认计价货币", a?.quoteCurrency ?: "系统默认") { vm.toast.show("计价货币设置将在后续批次接入") }
                    }
                    else -> Box(Modifier.padding(Spacing.pageH).padding(top = 12.dp)) {
                        InfoNote("推送通知设置将在 Firebase / FCM 接入后开放。")
                    }
                }
            }
        }
    }
    if (editingNick && a != null) NicknameDialog(a, onClose = { editingNick = false }, onSaved = { editingNick = false; vm.load(silent = true) })
    if (logoutConfirm) AlertDialog(
        onDismissRequest = { logoutConfirm = false },
        title = { Text("退出当前账户") }, text = { Text("退出后需重新登录才能交易与查看资产。") },
        confirmButton = { TextButton(onClick = { logoutConfirm = false; onLogout() }) { Text("退出", color = p.down) } },
        dismissButton = { TextButton(onClick = { logoutConfirm = false }) { Text("取消") } },
    )
}

@Composable
private fun NicknameDialog(account: click.cowa.linbit.core.generated.ApiAccount, onClose: () -> Unit, onSaved: () -> Unit, vm: ProfileEditViewModel = hiltViewModel()) {
    var nickname by remember { mutableStateOf(account.nickname.orEmpty()) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = { if (!busy) onClose() }, title = { Text("修改昵称") },
        text = { Column {
            LbTextField(nickname, { nickname = it.take(32); error = null }, "1-32 个字符", error = error)
            Text("昵称将展示在跟单、聊天室等公开场景。", Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = LB.p.textSecondary)
        } },
        confirmButton = { TextButton(enabled = nickname.isNotBlank() && !busy, onClick = {
            busy = true
            vm.save(nickname.trim()) { result -> busy = false; result.onSuccess { onSaved() }.onFailure { error = it.lbMessage() } }
        }) { Text(if (busy) "保存中" else "保存") } },
        dismissButton = { TextButton(onClick = onClose) { Text("取消") } },
    )
}

@HiltViewModel
class ProfileEditViewModel @Inject constructor(private val api: click.cowa.linbit.core.network.LinbitApi) : androidx.lifecycle.ViewModel() {
    fun save(nickname: String, onDone: (Result<Unit>) -> Unit) {
        viewModelScope.launch {
            onDone(runCatching { click.cowa.linbit.core.network.apiCall { api.updateProfile(click.cowa.linbit.core.generated.ApiUpdateProfileRequest(nickname = nickname)) }; Unit })
        }
    }
}

@Composable
fun AccountSwitchScreen(
    onBack: () -> Unit,
    onAdd: () -> Unit,
    onLogout: () -> Unit,
    vm: ProfileViewModel,
) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    var editing by remember { mutableStateOf(false) }
    var picked by remember { mutableStateOf(setOf<String>()) }
    var confirmRemove by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        AssetsNavBar("账号切换", onBack, trailing = {
            Text(if (editing) "完成" else "编辑", Modifier.pressable(scale = 1f) { editing = !editing; picked = emptySet() }, style = lb(FontSize.label), color = p.textPrimary)
        })
        Text("本机登录账号", Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp, bottom = 8.dp), style = lb(FontSize.caption), color = p.textSecondary)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            if (s.saved.isEmpty() && s.account != null) AccountCard(SavedAccount(s.account!!.uid, ProfileLogic.displayName(s.account), ProfileLogic.contact(s.account)), true, false, false) {}
            s.saved.forEach { account ->
                val current = account.uid == s.account?.uid
                AccountCard(account, current, editing, account.uid in picked) {
                    if (editing) picked = if (account.uid in picked) picked - account.uid else picked + account.uid
                    else vm.switchAccount(account.uid, onBack)
                }
            }
            if (s.switching) LinearProgressIndicator(Modifier.fillMaxWidth(), color = p.brand)
            Text("切换账号使用本机加密保存的登录凭证；失效时需要重新登录。", style = lb(FontSize.small), color = p.textTertiary)
        }
        if (editing) {
            LbButton("删除选中账号", Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp), enabled = picked.isNotEmpty(), kind = ButtonKind.SELL) { confirmRemove = true }
            LbButton("完成", Modifier.padding(Spacing.pageH), kind = ButtonKind.SECONDARY) { editing = false; picked = emptySet() }
        } else LbButton("+ 添加账号", Modifier.padding(Spacing.pageH), enabled = !s.switching, onClick = onAdd)
    }
    if (confirmRemove) AlertDialog(
        onDismissRequest = { confirmRemove = false }, title = { Text("删除本机登录记录") },
        text = { Text("仅从本机移除账号与令牌，不会注销账户。删除当前账号将退出登录。") },
        confirmButton = { TextButton(onClick = { confirmRemove = false; vm.removeAccounts(picked, onLogout); picked = emptySet(); editing = false }) { Text("删除", color = p.down) } },
        dismissButton = { TextButton(onClick = { confirmRemove = false }) { Text("取消") } },
    )
}

@Composable
private fun AccountCard(account: SavedAccount, current: Boolean, editing: Boolean, selected: Boolean, onClick: () -> Unit) {
    val p = LB.p
    Box(Modifier.fillMaxWidth().border(if (current) 1.5.dp else 1.dp, if (current) p.brand else p.border, RoundedCornerShape(Radius.lg)).pressable(scale = 1f, onClick = onClick)) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            ProfileAvatar(null, 36.dp)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(account.nickname, style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Text(account.masked, style = lb(FontSize.small), color = p.textSecondary)
            }
            if (editing) CheckCircle(selected)
        }
        if (current && !editing) Box(Modifier.align(Alignment.TopEnd)) { CurrentBadge() }
    }
}
