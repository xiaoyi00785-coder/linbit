package click.cowa.linbit.core.storage

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/** Only device appearance preferences; no credentials or security settings. */
object DisplayPrefs {
    var redUp by mutableStateOf(false); private set
    private var loaded = false
    fun load(context: Context) { if (!loaded) { loaded = true; redUp = context.getSharedPreferences("display_prefs", Context.MODE_PRIVATE).getBoolean("red_up", false) } }
    fun setRedUp(context: Context, value: Boolean) { redUp = value; context.getSharedPreferences("display_prefs", Context.MODE_PRIVATE).edit().putBoolean("red_up", value).apply() }
}
