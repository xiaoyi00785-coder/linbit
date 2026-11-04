package click.cowa.linbit.core.storage

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import java.util.UUID

/**
 * 令牌 / 设备 ID 的加密存储：EncryptedSharedPreferences（AES-256，主密钥在 Android Keystore）。
 * 对应 iOS 的 Keychain。
 */
class SecureStore(private val context: Context) {
    private val prefs: SharedPreferences = create()

    /**
     * Keystore / EncryptedSharedPreferences 初始化失败（备份还原、系统升级或密钥失效后常见的 AEADBadTagException /
     * KeyStoreException）时不能让应用崩溃：删除损坏的存储文件与主密钥后重建（代价是需要重新登录）。
     */
    private fun create(): SharedPreferences = try {
        open()
    } catch (e: Exception) {
        reset()
        open()
    }

    private fun open(): SharedPreferences {
        val key = MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build()
        return EncryptedSharedPreferences.create(
            context, FILE, key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    private fun reset() {
        runCatching { context.deleteSharedPreferences(FILE) }
        runCatching {
            java.io.File(context.applicationInfo.dataDir, "shared_prefs/$FILE.xml").delete()
        }
        runCatching {
            val ks = java.security.KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            ks.deleteEntry(MasterKey.DEFAULT_MASTER_KEY_ALIAS)
        }
    }

    fun get(key: String): String? = prefs.getString(key, null)

    fun set(key: String, value: String?) {
        prefs.edit().apply { if (value == null) remove(key) else putString(key, value) }.apply()
    }

    /** 稳定的设备标识（X-Device-Id / 登录 deviceId）。 */
    val deviceId: String
        get() = get(KEY_DEVICE) ?: UUID.randomUUID().toString().also { set(KEY_DEVICE, it) }

    private companion object {
        const val FILE = "linbit_secure"
        const val KEY_DEVICE = "lb.deviceId"
    }
}
