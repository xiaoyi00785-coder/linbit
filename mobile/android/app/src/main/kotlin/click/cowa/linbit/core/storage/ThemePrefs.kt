package click.cowa.linbit.core.storage

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.dataStore by preferencesDataStore(name = "linbit_prefs")

enum class ThemeMode(val title: String) {
    SYSTEM("跟随系统"), LIGHT("日间模式"), DARK("夜间模式");

    companion object {
        fun from(raw: String?): ThemeMode = entries.firstOrNull { it.name == raw } ?: LIGHT
    }
}

/** 非敏感偏好（DataStore）：主题模式、最近登录账户。默认日间（与 iOS 一致）。 */
class AppPrefs(private val context: Context) {
    private val themeKey = stringPreferencesKey("theme")
    private val lastLoginKey = stringPreferencesKey("lastLogin")

    val themeMode: Flow<ThemeMode> = context.dataStore.data.map { ThemeMode.from(it[themeKey]) }
    val lastLogin: Flow<String> = context.dataStore.data.map { it[lastLoginKey].orEmpty() }

    suspend fun setThemeMode(mode: ThemeMode) { context.dataStore.edit { it[themeKey] = mode.name } }
    suspend fun setLastLogin(account: String) { context.dataStore.edit { it[lastLoginKey] = account } }
}
