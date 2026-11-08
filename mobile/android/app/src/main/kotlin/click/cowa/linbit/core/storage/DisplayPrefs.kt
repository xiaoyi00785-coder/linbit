package click.cowa.linbit.core.storage

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/** 涨跌配色：参考 App「颜色偏好」三选一。 */
enum class UpDownColor(val title: String) { GREEN_UP("绿涨红跌"), RED_UP_GREEN_DOWN("红涨绿跌"), RED_UP_BLUE_DOWN("红涨蓝跌") }

/** Only device appearance preferences; no credentials or security settings. */
object DisplayPrefs {
    var color by mutableStateOf(UpDownColor.GREEN_UP); private set
    /** 参考「主题设置」：新主题 / 经典主题（经典为参考默认）。 */
    var classicTheme by mutableStateOf(true); private set
    val redUp: Boolean get() = color != UpDownColor.GREEN_UP
    private var loaded = false
    private fun sp(c: Context) = c.getSharedPreferences("display_prefs", Context.MODE_PRIVATE)
    fun load(context: Context) {
        if (loaded) return; loaded = true
        val s = sp(context)
        color = s.getString("updown", null)?.let { n -> UpDownColor.entries.firstOrNull { it.name == n } }
            ?: if (s.getBoolean("red_up", false)) UpDownColor.RED_UP_GREEN_DOWN else UpDownColor.GREEN_UP
        classicTheme = s.getBoolean("classic_theme", true)
    }
    fun setColor(context: Context, value: UpDownColor) { color = value; sp(context).edit().putString("updown", value.name).putBoolean("red_up", value != UpDownColor.GREEN_UP).apply() }
    fun setRedUp(context: Context, value: Boolean) = setColor(context, if (value) UpDownColor.RED_UP_GREEN_DOWN else UpDownColor.GREEN_UP)
    fun setClassicTheme(context: Context, value: Boolean) { classicTheme = value; sp(context).edit().putBoolean("classic_theme", value).apply() }
}

/** 主题设置对应的涨/跌基色（取样自参考截图）。 */
object TradeColors {
    const val NEW_UP = 0xFF72CC29; const val NEW_DOWN = 0xFFFA465B
    const val CLASSIC_UP = 0xFF31BD65; const val CLASSIC_DOWN = 0xFFED4C52
    const val BLUE_DOWN = 0xFF348FEA
}
