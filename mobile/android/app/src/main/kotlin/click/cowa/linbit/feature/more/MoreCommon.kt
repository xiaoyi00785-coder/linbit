package click.cowa.linbit.feature.more

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.TextButton
import androidx.compose.foundation.layout.RowScope
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import androidx.hilt.navigation.compose.hiltViewModel
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.assets.AssetsNavBar
import click.cowa.linbit.feature.profile.InfoNote
import click.cowa.linbit.feature.profile.ProfileNavBar
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import kotlinx.serialization.json.*
import java.math.BigDecimal
import java.math.RoundingMode
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** 首页宫格 / 个人中心可打开的功能页。 */
enum class MorePage(val title: String) {
    WELFARE("福利中心"), COPY("跟单交易"), NOTIFICATIONS("消息通知"),
    PRICE_ALERTS("价格预警"),
    COUPONS("卡券中心"), PARTNER("合伙人计划"), TRADFI("TradFi"), SUPPORT("我的工单"),
}

@Composable
fun MoreHost(
    initial: MorePage,
    onClose: () -> Unit,
    onLogin: () -> Unit,
    onOpen: (MorePage) -> Unit,
    onTrade: (Boolean, String) -> Unit,
    onProfile: () -> Unit,
    onDeposit: () -> Unit,
    modifier: Modifier = Modifier,
    alertVm: click.cowa.linbit.feature.alerts.PriceAlertViewModel = hiltViewModel(),
    onDialogWindow: (android.view.Window?) -> Unit = {},
) {
    BackHandler(onBack = onClose)
    Box(modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        when (initial) {
            MorePage.PRICE_ALERTS -> click.cowa.linbit.feature.alerts.PriceAlertScreen(onClose, onLogin, alertVm, onDialogWindow)
            MorePage.WELFARE -> WelfareScreen(onClose, onLogin, onOpen, onTrade, onProfile, onDeposit)
            MorePage.COPY -> CopyTradingScreen(onClose, onLogin)
            MorePage.NOTIFICATIONS -> NotificationsScreen(onClose, onLogin)
            MorePage.COUPONS -> CouponsScreen(onClose, onLogin)
            MorePage.PARTNER -> PartnerScreen(onClose, onLogin)
            MorePage.TRADFI -> TradFiScreen(onClose, onTrade)
            MorePage.SUPPORT -> SupportTicketsScreen(onClose, onLogin)
        }
    }
}


@Composable
fun MoreMenuScreen(onBack: () -> Unit, onOpen: (MorePage) -> Unit) {
    MoreFrame("全部功能", onBack) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            MorePage.entries.forEach { page ->
                Row(Modifier.fillMaxWidth().background(LB.p.fill, RoundedCornerShape(Radius.lg)).pressable(scale = 1f) { onOpen(page) }.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                    LbIcon(when (page) {
                        MorePage.PRICE_ALERTS -> R.drawable.ic_bell
                        MorePage.WELFARE, MorePage.COUPONS -> R.drawable.ic_g_gift_light
                        MorePage.COPY -> R.drawable.ic_g_copy_light
                        MorePage.NOTIFICATIONS -> R.drawable.ic_bell
                        MorePage.PARTNER -> R.drawable.ic_avatar
                        MorePage.TRADFI -> R.drawable.ic_g_tradfi_light
                        MorePage.SUPPORT -> R.drawable.ic_headset
                    }, 24.dp)
                    Spacer(Modifier.width(12.dp))
                    Text(page.title, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = LB.p.textPrimary)
                    LbIcon(R.drawable.ic_chevron_right, 14.dp, LB.p.textTertiary)
                }
            }
        }
    }
}

@Composable
fun MoreTopTabs(labels: List<String>, selected: Int, onSelect: (Int) -> Unit) {
    PrimaryTabs(labels, selected, onSelect, Modifier.padding(horizontal = Spacing.pageH), size = FontSize.label, underline = true)
}

@Composable
fun MoreFrame(
    title: String,
    onBack: () -> Unit,
    trailing: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(Modifier.fillMaxSize().background(LB.p.bg)) {
        ProfileNavBar(title, onBack, trailing)
        content()
    }
}

@Composable
fun MoreLoading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().height(240.dp), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(Modifier.size(28.dp), color = LB.p.brand, strokeWidth = 2.dp)
    }
}

@Composable
fun MoreError(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(message, style = lb(FontSize.body), color = LB.p.textSecondary)
        Spacer(Modifier.height(14.dp))
        LbButton("重试", Modifier.width(120.dp), kind = ButtonKind.OUTLINE, height = 36.dp, onClick = onRetry)
    }
}

@Composable
fun MoreCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Column(modifier.fillMaxWidth().border(1.dp, LB.p.border, RoundedCornerShape(Radius.lg)).padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp), content = content)
}

@Composable
fun StatTile(title: String, value: String, color: Color = LB.p.textPrimary, modifier: Modifier = Modifier) {
    Column(modifier.background(LB.p.fill, RoundedCornerShape(Radius.md)).padding(12.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
        Text(title, style = lb(FontSize.small), color = LB.p.textSecondary, maxLines = 1)
        Text(value, style = lbNum(16.sp, FontWeight.SemiBold), color = color, maxLines = 1)
    }
}

@Composable
fun SmallTag(text: String, color: Color = LB.p.textSecondary) {
    Text(text, Modifier.background(color.copy(alpha = 0.1f), RoundedCornerShape(3.dp)).padding(horizontal = 5.dp, vertical = 2.dp), style = lb(10.sp), color = color)
}

fun JsonObject.str(key: String): String? = this[key]?.jsonPrimitive?.contentOrNull
fun JsonObject.dec(key: String): BigDecimal = Dec.parse(str(key))
fun JsonObject.integer(key: String): Int? = this[key]?.jsonPrimitive?.intOrNull
fun JsonObject.long(key: String): Long? = this[key]?.jsonPrimitive?.longOrNull
fun JsonObject.boolean(key: String): Boolean? = this[key]?.jsonPrimitive?.booleanOrNull
fun JsonObject.obj(key: String): JsonObject = this[key] as? JsonObject ?: JsonObject(emptyMap())
fun JsonObject.array(key: String): List<JsonObject> = (this[key] as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }
fun JsonObject.amount(key: String, scale: Int = 2): String = this[key]?.let { Fmt.amount(dec(key), scale) } ?: "--"
fun JsonObject.ratio(key: String, scale: Int = 1): String = this[key]?.let {
    Fmt.price(dec(key).multiply(BigDecimal(100)), scale).trimEnd('0').trimEnd('.') + "%"
} ?: "--"
fun JsonObject.signedAmount(key: String, scale: Int = 2): String = this[key]?.let {
    val d = dec(key); (if (d.signum() > 0) "+" else "") + Fmt.amount(d, scale)
} ?: "--"

private val dateTimeFmt: DateTimeFormatter = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm")
fun formatMillis(ms: Long?): String = ms?.let {
    dateTimeFmt.format(Instant.ofEpochMilli(it).atZone(ZoneId.systemDefault()))
} ?: "--"
fun formatTime(value: String?): String = value?.let {
    runCatching { dateTimeFmt.format(Instant.parse(it).atZone(ZoneId.systemDefault())) }.getOrDefault(it)
} ?: "--"

fun newcomerCta(value: String?): String = when (value) {
    "login" -> "登录领取"; "kyc" -> "去认证"; "enroll" -> "立即参与"; "claim" -> "领取奖励"
    "continue" -> "继续任务"; "done" -> "已完成"; "none" -> "暂无可领取"; "open" -> "查看详情"
    else -> "立即参与"
}
