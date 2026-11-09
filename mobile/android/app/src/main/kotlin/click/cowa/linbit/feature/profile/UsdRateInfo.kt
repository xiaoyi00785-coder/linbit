package click.cowa.linbit.feature.profile

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.storage.*
import click.cowa.linbit.ui.theme.*

@Composable
internal fun UsdRateInfo(rate: ApiUsdRate? = QuoteDisplay.usdRate) {
    Column(Modifier.fillMaxWidth().padding(vertical=12.dp),verticalArrangement=Arrangement.spacedBy(6.dp)) {
        val valid=rate?.available==true && rate.pegAssumed!=true && rate.rate?.toBigDecimalOrNull()?.signum()==1
        Text("美元折合 · ${if(valid) "可用" else "不可用"}${if(rate?.stale==true) "（缓存报价）" else ""}",style=lb(FontSize.body),color=LB.p.textPrimary)
        fun value(s: String?)=s?.takeIf { it.isNotBlank() } ?: "--"
        Text("1 USDT = ${if(valid) value(rate?.rate) else "--"} USD",style=lbNum(FontSize.label),color=LB.p.textPrimary)
        Text("来源：${value(rate?.source)}",style=lb(FontSize.small),color=LB.p.textSecondary)
        Text("更新时间：${usdSourceTime(rate?.updatedAt)}",style=lbNum(FontSize.small),color=LB.p.textSecondary)
        Text("精度：${rate?.precision ?: "--"} · 舍入：${value(rate?.rounding)}",style=lb(FontSize.small),color=LB.p.textSecondary)
        Text("单位：${value(rate?.unit)} · 假定锚定：${rate?.pegAssumed?.let { if(it) "是（不使用）" else "否" } ?: "--"}",style=lb(FontSize.small),color=LB.p.textSecondary)
        Text("无有效报价时显示 --，不假定 USDT = USD。",style=lb(FontSize.small),color=LB.p.textTertiary)
    }
}
internal fun usdSourceTime(raw: String?): String = raw?.let { runCatching {
    java.time.Instant.parse(it).atZone(java.time.ZoneId.systemDefault()).format(java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss z"))
}.getOrNull() } ?: "--"
