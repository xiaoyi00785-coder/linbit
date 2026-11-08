package click.cowa.linbit.feature.home

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiPlacement
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.LbImageIcon
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import coil.compose.AsyncImage

/** 游客首页主视觉：大标题「立即注册，最高可获得 N USDT」+ 黑色「登录/注册」按钮（参考 guest/002）。 */
@Composable
internal fun GuestHero(total: String?, asset: String, onLogin: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 262.dp, bottom = 12.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        val amount = total?.let { runCatching { java.text.DecimalFormat("#,##0.##").format(java.math.BigDecimal(it)) }.getOrNull() }
        Text(
            if (amount != null) "立即注册，最高可获得 $amount $asset" else "立即注册，开启数字资产之旅",
            style = lb(27.sp, FontWeight.Medium), color = p.textPrimary, textAlign = TextAlign.Center, lineHeight = 38.sp,
        )
        Spacer(Modifier.height(32.dp))
        Box(
            Modifier.fillMaxWidth().height(42.dp).clip(RoundedCornerShape(Radius.md)).background(p.primary).pressable(onClick = onLogin),
            contentAlignment = Alignment.Center,
        ) { Text("登录/注册", style = lb(FontSize.body, FontWeight.Medium), color = p.textOnPrimary) }
    }
}

/** 游客首页底部「责任与保障」：资产安全 → 储备金证明；帮助中心与 7×24 客服 → 工单/客服。 */
@Composable
internal fun ProtectionSection(onReserves: () -> Unit, onSupport: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 20.dp, bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(24.dp)) {
        Text("责任与保障", style = lb(19.sp, FontWeight.SemiBold), color = p.textPrimary)
        ProtectionRow("资产安全是我们的首要任务", "了解更多", R.drawable.ic_shield, onReserves)
        ProtectionRow("帮助中心与 7×24 小时客服", "立即前往", R.drawable.ic_headset, onSupport)
    }
}

@Composable
private fun ProtectionRow(title: String, link: String, icon: Int, onClick: () -> Unit) {
    val p = LB.p
    Row(Modifier.fillMaxWidth().pressable(scale = 1f, onClick = onClick), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text(title, style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(link, style = lb(FontSize.label, FontWeight.Medium), color = p.up)
        }
        LbIcon(icon, 40.dp, p.textSecondary)
    }
}

/** 启动活动弹窗（/home/placements popup 槽位）：横向分页卡片 + 圆点 + 底部关闭。 */
@Composable
internal fun ActivityPopup(items: List<ApiPlacement>, dark: Boolean, onOpen: (ApiPlacement) -> Unit, onDismiss: () -> Unit) {
    if (items.isEmpty()) return
    val pager = rememberPagerState { items.size }
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
            HorizontalPager(pager, contentPadding = PaddingValues(horizontal = 28.dp), pageSpacing = 12.dp, modifier = Modifier.fillMaxWidth().height(460.dp)) { i ->
                val it = items[i]
                AsyncImage(
                    model = (if (dark) it.imageDarkUrl else null) ?: it.imageUrl, contentDescription = it.title,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize().clip(RoundedCornerShape(Radius.sheet)).background(Color(0xFF1E1E20)).pressable(scale = 1f) { onOpen(it) },
                )
            }
            if (items.size > 1) Row(Modifier.padding(top = 14.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                repeat(items.size) { i -> Box(Modifier.size(6.dp).clip(CircleShape).background(if (i == pager.currentPage) Color.White else Color.White.copy(alpha = 0.45f))) }
            }
            Box(Modifier.padding(top = 18.dp).size(32.dp).clip(CircleShape).background(Color.Black.copy(alpha = 0.35f)).pressable(onClick = onDismiss), contentAlignment = Alignment.Center) {
                LbIcon(R.drawable.ic_close, 18.dp, Color.White)
            }
        }
    }
}

/** 弹窗频控：frequency=once 永久一次，其余按自然日一次。 */
internal object PopupFrequency {
    private fun prefs(ctx: android.content.Context) = ctx.getSharedPreferences("linbit_popup", android.content.Context.MODE_PRIVATE)
    fun filter(ctx: android.content.Context, items: List<ApiPlacement>): List<ApiPlacement> {
        val today = java.time.LocalDate.now().toString()
        return items.filter {
            val v = prefs(ctx).getString("p_${it.id}", null)
            if (it.frequency == "once") v == null else v != today
        }
    }
    fun markShown(ctx: android.content.Context, items: List<ApiPlacement>) {
        val today = java.time.LocalDate.now().toString()
        prefs(ctx).edit().apply { items.forEach { putString("p_${it.id}", today) } }.apply()
    }
}

/** 储备金证明弹层（/market/proof-of-reserves）：未发布快照时如实显示。 */
@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
internal fun ReservesSheet(r: Result<click.cowa.linbit.core.generated.ApiProofOfReserves>?, onRetry: () -> Unit, onDismiss: () -> Unit) {
    val p = LB.p
    androidx.compose.material3.ModalBottomSheet(onDismissRequest = onDismiss, containerColor = p.bg) {
        Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("储备金证明", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
            Text("平台按 1:1 以上比例持有用户资产，储备率 = 平台储备 / 用户负债。", style = lb(FontSize.caption), color = p.textSecondary)
            val v = r?.getOrNull()
            when {
                r == null -> androidx.compose.material3.CircularProgressIndicator(Modifier.size(24.dp).align(Alignment.CenterHorizontally), color = p.textTertiary, strokeWidth = 2.dp)
                r.isFailure -> Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text("加载失败", style = lb(FontSize.label), color = p.textSecondary)
                    Text("重试", Modifier.padding(top = 8.dp).pressable(onClick = onRetry), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                }
                v == null || !v.available || v.items.isEmpty() -> click.cowa.linbit.ui.components.EmptyState("当前暂无已发布的储备金快照")
                else -> {
                    v.snapshotAt?.let { Text("快照时间 ${it.take(19).replace('T', ' ')}", style = lb(FontSize.small), color = p.textTertiary) }
                    Row { listOf("币种", "储备", "负债", "储备率").forEachIndexed { i, t -> Text(t, Modifier.weight(1f), style = lb(FontSize.small), color = p.textSecondary, textAlign = if (i == 0) TextAlign.Start else TextAlign.End) } }
                    v.items.forEach { it2 ->
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                click.cowa.linbit.ui.components.CoinIcon(it2.asset, 20.dp)
                                Text(it2.asset, style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                            }
                            Text(it2.reserves, Modifier.weight(1f), style = lb(FontSize.caption), color = p.textPrimary, textAlign = TextAlign.End)
                            Text(it2.liabilities, Modifier.weight(1f), style = lb(FontSize.caption), color = p.textPrimary, textAlign = TextAlign.End)
                            val pct = runCatching { java.math.BigDecimal(it2.ratio).multiply(java.math.BigDecimal(100)).setScale(2, java.math.RoundingMode.HALF_UP).toPlainString() + "%" }.getOrDefault(it2.ratio)
                            Text(pct, Modifier.weight(1f), style = lb(FontSize.caption, FontWeight.Medium), color = p.up, textAlign = TextAlign.End)
                        }
                    }
                }
            }
        }
    }
}
