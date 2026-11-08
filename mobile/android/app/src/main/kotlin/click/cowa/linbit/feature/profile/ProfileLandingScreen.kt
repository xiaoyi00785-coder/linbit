package click.cowa.linbit.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.BuildConfig
import click.cowa.linbit.R
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

private data class HubItem(val title: String, val light: Int, val dark: Int = light)
private val recommended = listOf(
    HubItem("聊天室", R.drawable.ic_g_chat_light, R.drawable.ic_g_chat_dark),
    HubItem("福利中心", R.drawable.ic_g_gift_light, R.drawable.ic_g_gift_dark),
    HubItem("理财", R.drawable.ic_g_earn_light, R.drawable.ic_g_earn_dark),
    HubItem("TradFi", R.drawable.ic_g_tradfi_light, R.drawable.ic_g_tradfi_dark),
    HubItem("跟单", R.drawable.ic_g_copy_light, R.drawable.ic_g_copy_dark),
    HubItem("策略交易", R.drawable.ic_g_bot_light, R.drawable.ic_g_bot_dark),
)
private val popular = listOf(
    HubItem("合伙人计划", R.drawable.ic_avatar), HubItem("卡券中心", R.drawable.ic_g_gift_light, R.drawable.ic_g_gift_dark),
    HubItem("TradFi", R.drawable.ic_g_tradfi_light, R.drawable.ic_g_tradfi_dark), HubItem("福利中心", R.drawable.ic_g_gift_light, R.drawable.ic_g_gift_dark),
    HubItem("合约", R.drawable.ic_candles), HubItem("现货", R.drawable.ic_ref_trade),
    HubItem("模拟交易", R.drawable.ic_doc_text), HubItem("理财", R.drawable.ic_g_earn_light, R.drawable.ic_g_earn_dark),
    HubItem("快捷买币", R.drawable.ic_ref_quickbuy), HubItem("聊天室", R.drawable.ic_g_chat_light, R.drawable.ic_g_chat_dark),
    HubItem("帮助中心", R.drawable.ic_doc_text), HubItem("客服", R.drawable.ic_headset),
)

@Composable
fun ProfileLandingScreen(onBack: () -> Unit, onAccount: () -> Unit, onSwitch: () -> Unit, onSecurity: () -> Unit, onFeature: (String) -> Unit, vm: ProfileViewModel) {
    val s by vm.state.collectAsStateWithLifecycle()
    val p = LB.p
    val clipboard = LocalClipboardManager.current
    var about by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("", onBack, trailing = { ThemedIcon(R.drawable.ic_mc_transfer_square_light, R.drawable.ic_mc_transfer_square_dark, 22.dp, Modifier.pressable(onClick = onSwitch)) })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            Row(Modifier.fillMaxWidth().padding(vertical = 16.dp).pressable(scale = 1f, onClick = onAccount), verticalAlignment = Alignment.CenterVertically) {
                ProfileAvatar(s.account?.avatarUrl, 48.dp)
                Column(Modifier.weight(1f).padding(start = 12.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    Text(ProfileLogic.displayName(s.account), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                    Text("UID: " + (s.account?.uid ?: "--"), Modifier.pressable { s.account?.uid?.let { clipboard.setText(AnnotatedString(it)); vm.toast.show("UID 已复制") } }, style = lbNum(FontSize.caption), color = p.textSecondary)
                    Text(ProfileLogic.kycLabel(s.kyc), style = lb(FontSize.small), color = p.warning)
                }
                LbIcon(R.drawable.ic_arrow_right, 20.dp, p.textPrimary)
            }
            Row(Modifier.fillMaxWidth().padding(vertical = 12.dp).border(1.dp, p.border, RoundedCornerShape(Radius.lg)).pressable(scale = 1f, onClick = onSecurity).padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                LbIcon(R.drawable.ic_passkey, 30.dp, p.brand)
                Column(Modifier.padding(start = 12.dp)) {
                    Text("守护您的账户安全", style = lb(FontSize.label), color = p.textPrimary)
                    Text("账户安全设置 ›", style = lb(FontSize.caption), color = p.brand)
                }
            }
            HubGrid("推荐", recommended, onFeature)
            HubGrid("热门", popular, onFeature)
            ProfileRow("关于 Linbit", "V" + BuildConfig.VERSION_NAME) { about = true }
            s.error?.let { Text(it, style = lb(FontSize.caption), color = p.down); TextButton(onClick = { vm.load() }) { Text("重试") } }
        }
    }
    if (about) AlertDialog(onDismissRequest = { about = false }, title = { Text("Linbit") }, text = { Text("版本 " + BuildConfig.VERSION_NAME) }, confirmButton = { TextButton(onClick = { about = false }) { Text("知道了") } })
}

@Composable
private fun HubGrid(title: String, items: List<HubItem>, onFeature: (String) -> Unit) {
    Text(title, Modifier.padding(top = 20.dp, bottom = 16.dp), style = lb(FontSize.title), color = LB.p.textPrimary)
    items.chunked(4).forEach { row ->
        Row(Modifier.fillMaxWidth().padding(bottom = 20.dp)) {
            row.forEach { item ->
                Column(Modifier.weight(1f).height(60.dp).pressable(scale = 1f) { onFeature(item.title) }, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (item.light in listOf(R.drawable.ic_ref_trade, R.drawable.ic_ref_quickbuy)) LbReferenceIcon(item.light, 26.dp) else ThemedIcon(item.light, item.dark, 26.dp)
                    Text(item.title, style = lb(FontSize.small), color = LB.p.textPrimary)
                }
            }
            repeat(4 - row.size) { Spacer(Modifier.weight(1f)) }
        }
    }
}
