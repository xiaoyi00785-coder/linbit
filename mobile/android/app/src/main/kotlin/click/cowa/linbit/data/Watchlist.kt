package click.cowa.linbit.data

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** 自选：本地持久化（SharedPreferences，保持顺序）。与 /account/favorites 的登录同步属后续里程碑（见 README 路线图）。 */
class Watchlist(context: Context) {
    private val prefs = context.getSharedPreferences("linbit_watchlist", Context.MODE_PRIVATE)
    private val _symbols = MutableStateFlow(prefs.getString(KEY, "").orEmpty().split(',').filter { it.isNotBlank() })
    val symbols: StateFlow<List<String>> = _symbols.asStateFlow()

    fun contains(s: String) = s in _symbols.value

    fun toggle(s: String) = set(if (contains(s)) _symbols.value - s else _symbols.value + s)

    fun add(list: Collection<String>) = set(_symbols.value + list.filter { !contains(it) })

    private fun set(v: List<String>) {
        _symbols.value = v
        prefs.edit().putString(KEY, v.joinToString(",")).apply()
    }

    private companion object { const val KEY = "symbols" }
}
