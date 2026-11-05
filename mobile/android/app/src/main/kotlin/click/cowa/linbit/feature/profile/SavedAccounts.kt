package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.ApiAccount
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.storage.SecureStore
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

interface AccountSecureStorage {
    fun get(key: String): String?
    fun set(key: String, value: String?)
}

private class SecureAccountStorage(private val store: SecureStore) : AccountSecureStorage {
    override fun get(key: String): String? = store.get(key)
    override fun set(key: String, value: String?) = store.set(key, value)
}
@Serializable
data class SavedAccount(
    val uid: String,
    val nickname: String,
    val masked: String,
)

/**
 * 本机多账号资料与令牌都放在 EncryptedSharedPreferences。只保存用户实际登录过的账号，
 * 切换后必须用 /account 验证目标会话；失败恢复原令牌，不制造已登录假象。
 */
class SavedAccounts(
    private val store: AccountSecureStorage,
    private val tokens: TokenStore,
) {
    constructor(store: SecureStore, tokens: TokenStore) : this(SecureAccountStorage(store), tokens)

    private val json = Json { ignoreUnknownKeys = true }
    private var pendingUid: String? = null
    private var pendingAccess: String? = null

    fun list(): List<SavedAccount> = store.get(ACCOUNTS)?.let {
        runCatching { json.decodeFromString(ListSerializer(SavedAccount.serializer()), it) }.getOrNull()
    }.orEmpty()

    fun remember(a: ApiAccount) {
        val item = SavedAccount(a.uid, ProfileLogic.displayName(a), ProfileLogic.contact(a))
        persist((list().filterNot { it.uid == a.uid } + item).takeLast(MAX_ACCOUNTS))
        store.set(accessKey(a.uid), tokens.accessToken)
        store.set(refreshKey(a.uid), tokens.refreshToken)
    }

    /** 添加账号只保存本机当前会话，不调用服务端 logout，保留以后切回的能力。 */
    fun beginAddAccount(uid: String) {
        beginSwitch(uid)
        tokens.clear()
        store.set(PENDING, null)
        tokens.loggingOut = false
        tokens.notifySessionChanged()
    }

    /** 切走前立即把刷新后的最新令牌写回原 uid，再保留 PENDING 供失败或进程中断恢复。 */
    fun beginSwitch(uid: String) {
        pendingUid = uid
        pendingAccess = tokens.accessToken
        persistCurrent(uid)
        store.set(PENDING, "${tokens.accessToken.orEmpty()}\n${tokens.refreshToken.orEmpty()}\n${tokens.sessionExpired}\n$uid")
        tokens.loggingOut = true
    }

    fun switchTo(uid: String): Boolean {
        val saved = list().firstOrNull { it.uid == uid } ?: return false
        val refresh = store.get(refreshKey(uid)) ?: return false
        tokens.accessToken = store.get(accessKey(saved.uid))
        tokens.refreshToken = refresh
        tokens.sessionExpired = false
        return true
    }

    fun restore() {
        val old = store.get(PENDING)?.split('\n', limit = 4) ?: return
        old.getOrNull(3)?.takeIf(String::isNotBlank)?.let { pendingUid = it }
        pendingAccess = tokens.accessToken
        tokens.accessToken = old[0].ifEmpty { null }
        tokens.refreshToken = old.getOrNull(1)?.ifEmpty { null }
        tokens.sessionExpired = old.getOrNull(2)?.toBooleanStrictOrNull() ?: false
        store.set(PENDING, null)
        persistPendingIfUnchanged()
        pendingUid = null
        pendingAccess = null
        tokens.loggingOut = false
    }

    fun commit() {
        store.set(PENDING, null)
        persistPendingIfUnchanged()
        pendingUid = null
        pendingAccess = null
        tokens.loggingOut = false
        tokens.notifySessionChanged()
    }

    fun remove(uids: Set<String>) {
        uids.forEach {
            store.set(accessKey(it), null)
            store.set(refreshKey(it), null)
        }
        persist(list().filterNot { it.uid in uids })
    }

    /** 只有当前仍持有切走前的 access token 才能回写，避免在途请求跨账号串写。 */
    private fun persistPendingIfUnchanged() {
        val uid = pendingUid ?: return
        if (tokens.accessToken == pendingAccess) persistCurrent(uid)
    }

    private fun persistCurrent(uid: String) {
        if (list().none { it.uid == uid }) return
        store.set(accessKey(uid), tokens.accessToken)
        store.set(refreshKey(uid), tokens.refreshToken)
    }

    private fun persist(items: List<SavedAccount>) {
        store.set(ACCOUNTS, json.encodeToString(ListSerializer(SavedAccount.serializer()), items))
    }

    private companion object {
        const val ACCOUNTS = "lb.accounts"
        const val PENDING = "lb.account.switch.pending"
        const val MAX_ACCOUNTS = 10
        fun accessKey(uid: String) = "lb.acct.$uid.a"
        fun refreshKey(uid: String) = "lb.acct.$uid.r"
    }
}
