package click.cowa.linbit.feature.markets

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import click.cowa.linbit.R
import click.cowa.linbit.app.MainActivity
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

/** Public quote snapshots only; no tokens or private balances. */
class MarketQuoteWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
        val prefs = context.getSharedPreferences("market_widget", Context.MODE_PRIVATE)
        ids.forEach { id ->
            val views = RemoteViews(context.packageName, R.layout.market_quote_widget)
            views.setTextViewText(R.id.widget_symbol, prefs.getString("symbol", "BTCUSDT"))
            views.setTextViewText(R.id.widget_price, prefs.getString("price", "--"))
            val updated = prefs.getLong("updated", 0)
            views.setTextViewText(R.id.widget_updated, if (updated == 0L) "打开 Linbit 同步行情" else "行情快照 · " + java.text.SimpleDateFormat("HH:mm", java.util.Locale.getDefault()).format(java.util.Date(updated)))
            views.setOnClickPendingIntent(R.id.widget_root, PendingIntent.getActivity(context, id, Intent(context, MainActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE))
            manager.updateAppWidget(id, views)
        }
    }
    companion object {
        fun cache(context: Context, row: MarketRow) {
            context.getSharedPreferences("market_widget", Context.MODE_PRIVATE).edit()
                .putString("symbol", row.symbol).putString("price", "$" + priceText(row))
                .putLong("updated", System.currentTimeMillis()).apply()
            val manager = AppWidgetManager.getInstance(context)
            MarketQuoteWidget().onUpdate(context, manager, manager.getAppWidgetIds(ComponentName(context, MarketQuoteWidget::class.java)))
        }
    }
}

@Composable
fun MarketWidgetsSheet(rows: List<MarketRow>, onClose: () -> Unit) {
    val context = LocalContext.current
    var selected by remember { mutableStateOf(rows.firstOrNull()?.symbol ?: "BTCUSDT") }
    var message by remember { mutableStateOf<String?>(null) }
    LbSheet("Widgets", onClose, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("桌面行情小组件", style = lb(FontSize.title), color = LB.p.textPrimary)
            Text("添加行情快照到桌面；打开应用时同步最新价格。", style = lb(FontSize.caption), color = LB.p.textSecondary)
            ChipBar(rows.take(6).map { it.base }, rows.take(6).indexOfFirst { it.symbol == selected }.coerceAtLeast(0), { selected = rows[it].symbol })
            rows.firstOrNull { it.symbol == selected }?.let { MarketRowView(it, null) }
            message?.let { Text(it, style = lb(FontSize.caption), color = LB.p.textSecondary) }
            LbButton("添加到桌面", enabled = rows.isNotEmpty()) {
                val manager = AppWidgetManager.getInstance(context)
                if (manager.isRequestPinAppWidgetSupported) {
                    rows.firstOrNull { it.symbol == selected }?.let { MarketQuoteWidget.cache(context, it) }
                    if (!manager.requestPinAppWidget(ComponentName(context, MarketQuoteWidget::class.java), null, null)) message = "桌面未接受添加请求"
                } else message = "当前桌面不支持直接添加，请长按桌面，在小组件中选择 Linbit。"
            }
        }
    }
}
