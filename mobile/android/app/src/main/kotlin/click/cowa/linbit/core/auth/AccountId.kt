package click.cowa.linbit.core.auth

/** 账户标识：邮箱 或 手机（国家码 + 去掉前导 0 的号码），与 iOS AccountId / 网页 Auth.tsx 一致。 */
data class AccountId(
    val kind: Kind = Kind.EMAIL,
    val email: String = "",
    val countryCode: String = "86",
    val phone: String = "",
) {
    enum class Kind { EMAIL, PHONE }

    val phoneDigits: String get() = phone.filter { it.isDigit() }.trimStart('0')
    val e164: String get() = "+$countryCode$phoneDigits"

    val isValid: Boolean
        get() = when (kind) {
            Kind.EMAIL -> EMAIL_RE.matches(email)
            Kind.PHONE -> phoneDigits.length in 5..15
        }

    /** 「验证码已发送至 X」用的脱敏展示：Ins***@outlook.com / +86 138****8000 */
    val masked: String
        get() = when (kind) {
            Kind.EMAIL -> {
                val parts = email.split("@")
                if (parts.size == 2) "${parts[0].take(2)}***@${parts[1]}" else email
            }
            Kind.PHONE -> "+$countryCode ${phoneDigits.take(3)}****${phoneDigits.takeLast(4)}"
        }

    val emailOrNull: String? get() = if (kind == Kind.EMAIL) email.trim() else null
    val phoneOrNull: String? get() = if (kind == Kind.PHONE) e164 else null
    val ccOrNull: String? get() = if (kind == Kind.PHONE) countryCode else null

    companion object {
        private val EMAIL_RE = Regex("^[^\\s@]+@[^\\s@]+\\.[^\\s@]+$")

        /** 输入框原文 → 账户：纯数字按手机号，否则按邮箱（与 iOS AccountField 一致）。 */
        fun fromRaw(raw: String, countryCode: String): AccountId {
            val t = raw.trim()
            return if (t.isNotEmpty() && t.all { it.isDigit() }) AccountId(Kind.PHONE, countryCode = countryCode, phone = t)
            else AccountId(Kind.EMAIL, email = t, countryCode = countryCode)
        }
    }
}

object PasswordRule {
    /** 8–64 位，至少包含一个英文字母和一个数字（与服务端规则一致） */
    const val MIN_LEN = 8
    const val MAX_LEN = 64
    fun hasLetter(s: String) = s.any { it.code < 128 && it.isLetter() }
    fun hasDigit(s: String) = s.any { it.code < 128 && it.isDigit() }
    fun lengthOk(s: String) = s.length in MIN_LEN..MAX_LEN
    fun ok(s: String) = lengthOk(s) && hasLetter(s) && hasDigit(s)

    /** 逐条规则（界面勾选提示） */
    fun rules(s: String): List<Pair<String, Boolean>> =
        listOf("8-64 位" to lengthOk(s), "包含字母" to hasLetter(s), "包含数字" to hasDigit(s))

    /** 不合规时的中文提示；空串不提示 */
    fun hint(s: String): String? = when {
        s.isEmpty() -> null
        s.length < MIN_LEN -> "密码至少 8 位"
        s.length > MAX_LEN -> "密码不能超过 64 位"
        !hasLetter(s) -> "密码需至少包含一个英文字母"
        !hasDigit(s) -> "密码需至少包含一个数字"
        else -> null
    }
}

/** 国家 / 地区区号（与 iOS CountryCode 一致）。 */
object CountryCodes {
    data class Entry(val code: String, val name: String, val flag: String)

    val list = listOf(
        Entry("86", "中国大陆", "🇨🇳"), Entry("852", "中国香港", "🇭🇰"), Entry("853", "中国澳门", "🇲🇴"), Entry("886", "中国台湾", "🇹🇼"),
        Entry("1", "美国 / 加拿大", "🇺🇸"), Entry("44", "英国", "🇬🇧"), Entry("81", "日本", "🇯🇵"), Entry("82", "韩国", "🇰🇷"),
        Entry("65", "新加坡", "🇸🇬"), Entry("60", "马来西亚", "🇲🇾"), Entry("66", "泰国", "🇹🇭"), Entry("84", "越南", "🇻🇳"),
        Entry("63", "菲律宾", "🇵🇭"), Entry("62", "印度尼西亚", "🇮🇩"), Entry("91", "印度", "🇮🇳"), Entry("61", "澳大利亚", "🇦🇺"),
        Entry("49", "德国", "🇩🇪"), Entry("33", "法国", "🇫🇷"), Entry("7", "俄罗斯", "🇷🇺"), Entry("971", "阿联酋", "🇦🇪"),
    )

    fun flag(code: String): String = list.firstOrNull { it.code == code }?.flag ?: "🌐"
}
