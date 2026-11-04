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
class SecureStore(context: Context) {
    private val prefs: SharedPreferences = run {
        val key = MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build()
        EncryptedSharedPreferences.create(
            context, "linbit_secure", key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    fun get(key: String): String? = prefs.getString(key, null)

    fun set(key: String, value: String?) {
        prefs.edit().apply { if (value == null) remove(key) else putString(key, value) }.apply()
    }

    /** 稳定的设备标识（X-Device-Id / 登录 deviceId）。 */
    val deviceId: String
        get() = get(KEY_DEVICE) ?: UUID.randomUUID().toString().also { set(KEY_DEVICE, it) }

    private companion object {
        const val KEY_DEVICE = "lb.deviceId"
    }
}
