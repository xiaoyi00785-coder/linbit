package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.ApiAccount
import click.cowa.linbit.core.generated.ApiKycStatus
import click.cowa.linbit.core.generated.ApiKycSubmission
import click.cowa.linbit.core.generated.ApiSubAccount
import java.time.LocalDate
import java.time.Period
import java.time.format.DateTimeParseException

/** 个人中心的纯逻辑：展示、表单与账号切换校验均在这里单元测试。 */
object ProfileLogic {
    fun displayName(a: ApiAccount?): String = a?.nickname?.takeIf { it.isNotBlank() } ?: a?.uid?.let { "Linbit_$it" } ?: "—"

    fun contact(a: ApiAccount?): String = when {
        a?.email != null -> maskEmail(a.email)
        a?.phone != null -> maskPhone(a.phone)
        else -> "—"
    }

    fun maskEmail(raw: String): String {
        val parts = raw.split('@')
        if (parts.size != 2) return raw
        val local = parts[0]
        return local.take(3) + "****@" + parts[1]
    }

    fun maskPhone(raw: String): String = when {
        raw.length > 7 -> raw.take(3) + "****" + raw.takeLast(4)
        else -> raw
    }

    fun kycLabel(s: ApiKycStatus?): String = when (s?.status) {
        ApiKycStatus.Status.APPROVED -> "已认证 Lv.${s.level}"
        ApiKycStatus.Status.PENDING -> "审核中"
        ApiKycStatus.Status.REJECTED -> "认证未通过"
        else -> "去认证"
    }

    fun kycStatusText(s: ApiKycStatus): String = when (s.status) {
        ApiKycStatus.Status.APPROVED -> "已通过 Lv.${s.level} 认证"
        ApiKycStatus.Status.PENDING -> "审核中"
        ApiKycStatus.Status.REJECTED -> "认证未通过"
        ApiKycStatus.Status.NONE -> "未认证"
    }

    fun ageAt(dob: String, today: LocalDate = LocalDate.now()): Int? = try {
        Period.between(LocalDate.parse(dob), today).years.takeIf { it >= 0 }
    } catch (_: DateTimeParseException) { null }

    fun requiredSides(level: Int, type: ApiKycSubmission.IdType): List<ApiKycDocumentSide> = buildList {
        add(ApiKycDocumentSide.FRONT)
        if (type != ApiKycSubmission.IdType.PASSPORT) add(ApiKycDocumentSide.BACK)
        add(ApiKycDocumentSide.SELFIE)
        if (level == 2) add(ApiKycDocumentSide.PROOF_OF_ADDRESS)
    }

    fun kycError(input: KycInput, today: LocalDate = LocalDate.now()): String? {
        if (input.firstName.isBlank() || input.lastName.isBlank()) return "请填写与证件一致的姓名"
        if (!Regex("^[A-Z]{2}$").matches(input.country)) return "请选择国家或地区"
        if (input.idNumber.trim().length < 5) return "证件号码格式不正确"
        val age = ageAt(input.dateOfBirth, today) ?: return "请选择正确的出生日期"
        if (age < 18) return "未满 18 周岁无法完成认证"
        val missing = requiredSides(input.level, input.idType).firstOrNull { it !in input.uploaded }
        if (missing != null) return "请上传${missing.title}"
        return null
    }

    fun subAccountNameError(raw: String): String? = when {
        raw.isEmpty() -> null
        !Regex("^[a-z0-9][a-z0-9_]{2,19}$").matches(raw) -> "3-20 位小写字母、数字或下划线，须以字母或数字开头"
        else -> null
    }

    fun subDisplayName(s: ApiSubAccount): String = s.nickname?.takeIf { it.isNotBlank() } ?: s.username ?: s.uid ?: "子账户"
}

enum class ApiKycDocumentSide(val wire: String, val title: String) {
    FRONT("front", "证件正面"),
    BACK("back", "证件背面"),
    SELFIE("selfie", "手持证件自拍"),
    PROOF_OF_ADDRESS("proof_of_address", "地址证明"),
}

data class KycInput(
    val firstName: String = "",
    val lastName: String = "",
    val country: String = "SG",
    val idType: ApiKycSubmission.IdType = ApiKycSubmission.IdType.ID_CARD,
    val idNumber: String = "",
    val dateOfBirth: String = "",
    val level: Int = 1,
    val uploaded: Set<ApiKycDocumentSide> = emptySet(),
)
