package click.cowa.linbit.app

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.di.SessionEvents
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 应用级状态：主题、登录态、会话过期事件（对应 iOS Session / ThemeStore）。 */
@HiltViewModel
class AppViewModel @Inject constructor(
    private val prefs: AppPrefs,
    private val tokens: TokenStore,
    private val auth: AuthRepository,
    events: SessionEvents,
    val toast: ToastCenter,
    val tradeNav: click.cowa.linbit.data.trade.TradeNav,
    val assetsNav: click.cowa.linbit.data.assets.AssetsNav,
) : ViewModel() {
    val themeMode: StateFlow<ThemeMode> = prefs.themeMode.stateIn(viewModelScope, SharingStarted.Eagerly, ThemeMode.LIGHT)
    val loggedIn: StateFlow<Boolean> get() = tokens.loggedIn

    /** 刷新令牌失败：提示并（由 UI）拉起登录。 */
    val expired = events.expired

    fun setTheme(m: ThemeMode) { viewModelScope.launch { prefs.setThemeMode(m) } }

    fun logout(onDone: () -> Unit) {
        viewModelScope.launch {
            try { auth.logout() } finally { onDone() }
            toast.show("已退出登录")
        }
    }
}
