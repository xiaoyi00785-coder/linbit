package click.cowa.linbit.feature.more

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 首页通知未读角标：登录/切换账号先清零，再请求当前账号未读数。 */
@HiltViewModel
class NotificationBadgeViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
) : ViewModel() {
    private val _unread = MutableStateFlow(0)
    val unread: StateFlow<Int> = _unread

    init {
        viewModelScope.launch {
            tokens.sessionVersion.collect {
                _unread.value = 0
                refresh()
            }
        }
    }

    fun refresh() {
        if (!tokens.loggedIn.value) { _unread.value = 0; return }
        viewModelScope.launch {
            _unread.value = runCatching { apiCall { api.unreadCount() }.unread ?: 0 }.getOrDefault(0)
        }
    }
}
