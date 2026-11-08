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
    HubItem("邀请好友", R.drawable.ic_g_referral_light, R.drawable.ic_g_referral_dark),
    HubItem("充值返现", R.drawable.ic_g_cashback_light, R.drawable.ic_g_cashback_dark),
    HubItem("TradFi", R.drawable.ic_g_tradfi_light, R.drawable.ic_g_tradfi_dark),
    HubItem("交易抽奖", R.drawable.ic_g_lottery_light, R.drawable.ic_g_lottery_dark),
    HubItem("合约", R.drawable.ic_g_futures_light, R.drawable.ic_g_futures_dark),
    HubItem("现货", R.drawable.ic_g_spot_light, R.drawable.ic_g_spot_dark),
    HubItem("模拟交易", R.drawable.ic_g_demo_light, R.drawable.ic_g_demo_dark),
    HubItem("理财", R.drawable.ic_g_earn_light, R.drawable.ic_g_earn_dark),
    HubItem("快捷买币", R.drawable.ic_g_quickbuy_light, R.drawable.ic_g_quickbuy_dark),
    HubItem("聊天室", R.drawable.ic_g_chat_light, R.drawable.ic_g_chat_dark),
    HubItem("帮助中心", R.drawable.ic_g_help_light, R.drawable.ic_g_help_dark),
    HubItem("客服", R.drawable.ic_g_support_light, R.drawable.ic_g_support_dark),
    HubItem("API Key", R.drawable.ic_g_apikey_light, R.drawable.ic_g_apikey_dark),
)

@Composable
fun ProfileLandingScreen(onBack: () -> Unit, onAccount: () -> Unit, onSwitch: () -> Unit, onSecurity: () -> Unit, onFeature: (String) -> Unit, vm: ProfileViewModel) {
    val s by vm.state.collectAsStateWithLifecycle()
    val p = LB.p
    val clipboard = LocalClipboardManager.current
    var about by remember { mutableStateOf(false) }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val guidePrefs = remember { ctx.getSharedPreferences("profile_guide", android.content.Context.MODE_PRIVATE) }
    var guide by remember { mutableStateOf(!guidePrefs.getBoolean("prefs_moved_seen", false)) }
    Box(Modifier.fillMaxSize()) {
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        ProfileNavBar("", onBack, trailing = { ThemedIcon(R.drawable.ic_mc_transfer_square_light, R.drawable.ic_mc_transfer_square_dark, 22.dp, Modifier.pressable(onClick = onSwitch)) })
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            Row(Modifier.fillMaxWidth().padding(vertical = 16.dp).pressable(scale = 1f, onClick = onAccount), verticalAlignment = Alignment.CenterVertically) {
                ProfileAvatar(s.account?.avatarUrl, 56.dp)
                Column(Modifier.weight(1f).padding(start = 12.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    Text(ProfileLogic.displayName(s.account), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                    Row(Modifier.pressable { s.account?.uid?.let { clipboard.setText(AnnotatedString(it)); vm.toast.show("UID 已复制") } }, verticalAlignment = Alignment.CenterVertically) {
                        Text("UID: " + (s.account?.uid ?: "--"), style = lbNum(FontSize.caption), color = p.textSecondary)
                        LbIcon(R.drawable.ic_bn_copy, 12.dp, p.textSecondary, Modifier.padding(start = 4.dp))
                    }
                    Text(ProfileLogic.kycLabel(s.kyc), Modifier.background(p.warning.copy(alpha = 0.12f), RoundedCornerShape(4.dp)).padding(horizontal = 6.dp, vertical = 1.dp), style = lb(FontSize.small), color = p.warning)
                }
                LbIcon(R.drawable.ic_arrow_right, 20.dp, p.textPrimary)
            }
            Row(Modifier.fillMaxWidth().padding(vertical = 12.dp).border(1.dp, p.border, RoundedCornerShape(Radius.lg)).pressable(scale = 1f, onClick = onSecurity).padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                LbIcon(R.drawable.ic_shield, 30.dp, p.textPrimary)
                Column(Modifier.padding(start = 12.dp)) {
                    Text("您的账户安全设置弱于多数用户", style = lb(FontSize.label), color = p.textPrimary)
                    Text("设置通行密钥", Modifier.padding(top = 4.dp), style = lb(FontSize.label), color = p.brand)
                }
            }
            HubGrid("推荐", recommended, onFeature)
            HubGrid("热门", popular, onFeature)
            Row(Modifier.fillMaxWidth().padding(vertical = 12.dp).pressable(scale = 1f) { onFeature("更多服务") }, horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                Text("更多服务", style = lb(FontSize.label), color = p.textSecondary)
                LbIcon(R.drawable.ic_arrow_right, 14.dp, p.textSecondary, Modifier.padding(start = 2.dp))
            }
            s.error?.let { Text(it, style = lb(FontSize.caption), color = p.down); TextButton(onClick = { vm.load() }) { Text("重试") } }
        }
        Row(Modifier.fillMaxWidth().height(52.dp).pressable(scale = 1f) { about = true }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            LbIcon(R.drawable.ic_cv_help, 16.dp, p.textPrimary)
            Text("关于 Linbit", Modifier.padding(start = 6.dp).weight(1f), style = lb(FontSize.body), color = p.textPrimary)
            Text("V" + BuildConfig.VERSION_NAME, style = lbNum(FontSize.label), color = p.textTertiary)
            LbIcon(R.drawable.ic_arrow_right, 16.dp, p.textTertiary, Modifier.padding(start = 8.dp))
        }
    }
    // 参考 App 引导遮罩：「偏好设置已移至用户中心」+「知道了」，只出现一次
    if (guide) Box(Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.6f)).pressable(scale = 1f) {}.statusBarsPadding()) {
        Column(Modifier.padding(start = Spacing.pageH, top = 120.dp).background(p.bg, RoundedCornerShape(Radius.lg)).padding(16.dp).widthIn(max = 280.dp)) {
            Text("偏好设置已移至用户中心。", style = lb(FontSize.body), color = p.textPrimary)
            Text("知道了", Modifier.align(Alignment.End).padding(top = 12.dp).background(p.primary, RoundedCornerShape(Radius.md)).pressable { guidePrefs.edit().putBoolean("prefs_moved_seen", true).apply(); guide = false }.padding(horizontal = 16.dp, vertical = 8.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textOnPrimary)
        }
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
                Column(Modifier.weight(1f).height(66.dp).pressable(scale = 1f) { onFeature(item.title) }, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (item.light in listOf(R.drawable.ic_ref_trade, R.drawable.ic_ref_quickbuy)) LbReferenceIcon(item.light, 26.dp) else ThemedIcon(item.light, item.dark, 26.dp)
                    Text(item.title, style = lb(FontSize.small), color = LB.p.textPrimary)
                }
            }
            repeat(4 - row.size) { Spacer(Modifier.weight(1f)) }
        }
    }
}
