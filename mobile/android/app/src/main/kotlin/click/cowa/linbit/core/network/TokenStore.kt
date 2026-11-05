package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiAuthTokens
import click.cowa.linbit.core.storage.SecureStore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/** 令牌存储抽象（对应 iOS TokenStore）。生产实现 [SecureTokenStore]，测试用 [InMemoryTokenStore]。 */
interface TokenStore {
    var accessToken: String?
    var refreshToken: String?

    /** 是否已登录（有 access token）。UI 通过它切换登录态。 */
    val loggedIn: StateFlow<Boolean>

    /** 登录、退出、令牌失效与账号切换均递增；即使 loggedIn 不变，也要清理并重载账号数据。 */
    val sessionVersion: StateFlow<Long>

    /** 明确通知账号上下文已变化（用于目标账号令牌恰好未变的切换）。 */
    fun notifySessionChanged()

    /** 仅当 refresh token 刷新失败时置为 true（区分“未登录”与“登录过期”） */
    var sessionExpired: Boolean

    /** 退出登录进行中 / 刚退出：旧访问令牌返回 401 属预期，不提示「登录已过期」 */
    var loggingOut: Boolean

    fun save(t: ApiAuthTokens) {
        accessToken = t.accessToken
        refreshToken = t.refreshToken
        sessionExpired = false
        loggingOut = false
    }

    fun clear() {
        accessToken = null
        refreshToken = null
    }
}

class InMemoryTokenStore : TokenStore {
    private val state = MutableStateFlow(false)
    private val version = MutableStateFlow(0L)
    override var accessToken: String? = null
        set(value) { field = value; state.value = value != null; version.value++ }
    override var refreshToken: String? = null
    override val loggedIn: StateFlow<Boolean> get() = state
    override val sessionVersion: StateFlow<Long> get() = version
    override fun notifySessionChanged() { version.value++ }
    @Volatile override var sessionExpired: Boolean = false
    @Volatile override var loggingOut: Boolean = false
}

class SecureTokenStore(private val store: SecureStore) : TokenStore {
    private val state = MutableStateFlow(store.get(ACCESS) != null)
    private val version = MutableStateFlow(0L)
    override val loggedIn: StateFlow<Boolean> get() = state
    override val sessionVersion: StateFlow<Long> get() = version
    override var accessToken: String?
        get() = store.get(ACCESS)
        set(value) { store.set(ACCESS, value); state.value = value != null; version.value++ }
    override var refreshToken: String?
        get() = store.get(REFRESH)
        set(value) = store.set(REFRESH, value)
    override fun notifySessionChanged() { version.value++ }
    @Volatile override var sessionExpired: Boolean = false
    @Volatile override var loggingOut: Boolean = false

    private companion object {
        const val ACCESS = "lb.access"
        const val REFRESH = "lb.refresh"
    }
}
