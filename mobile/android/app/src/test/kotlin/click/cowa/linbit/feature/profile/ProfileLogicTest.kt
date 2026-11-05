package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.ApiAccount
import click.cowa.linbit.core.generated.ApiKycStatus
import click.cowa.linbit.core.generated.ApiKycSubmission
import org.junit.Assert.*
import org.junit.Test
import java.time.LocalDate

class ProfileLogicTest {
    private val today = LocalDate.of(2026, 11, 5)
    private fun input() = KycInput(
        firstName = "San", lastName = "Zhang", country = "SG", idNumber = "E12345678",
        dateOfBirth = "1992-04-18", uploaded = setOf(ApiKycDocumentSide.FRONT, ApiKycDocumentSide.BACK, ApiKycDocumentSide.SELFIE),
    )

    @Test fun accountContactIsMasked() {
        val a = ApiAccount("80000001", 1, true, "2026-01-01", email = "trader@example.test")
        assertEquals("tra****@example.test", ProfileLogic.contact(a))
        assertEquals("Linbit_80000001", ProfileLogic.displayName(a.copy(nickname = null)))
        assertEquals("138****8000", ProfileLogic.maskPhone("13800138000"))
    }

    @Test fun kycStatusesMapToRealServerValues() {
        fun status(value: ApiKycStatus.Status) = ApiKycStatus(1, value)
        assertEquals("去认证", ProfileLogic.kycLabel(status(ApiKycStatus.Status.NONE)))
        assertEquals("审核中", ProfileLogic.kycLabel(status(ApiKycStatus.Status.PENDING)))
        assertEquals("认证未通过", ProfileLogic.kycLabel(status(ApiKycStatus.Status.REJECTED)))
        assertEquals("已认证 Lv.1", ProfileLogic.kycLabel(status(ApiKycStatus.Status.APPROVED)))
    }

    @Test fun completeIdCardSubmissionPasses() {
        assertNull(ProfileLogic.kycError(input(), today))
    }

    @Test fun underageAndMissingDocumentsAreRejected() {
        assertEquals("未满 18 周岁无法完成认证", ProfileLogic.kycError(input().copy(dateOfBirth = "2010-01-01"), today))
        assertEquals("请上传手持证件自拍", ProfileLogic.kycError(input().copy(uploaded = setOf(ApiKycDocumentSide.FRONT, ApiKycDocumentSide.BACK)), today))
        assertEquals("证件号码格式不正确", ProfileLogic.kycError(input().copy(idNumber = "1234"), today))
    }

    @Test fun passportDoesNotRequireBack() {
        val i = input().copy(idType = ApiKycSubmission.IdType.PASSPORT, uploaded = setOf(ApiKycDocumentSide.FRONT, ApiKycDocumentSide.SELFIE))
        assertNull(ProfileLogic.kycError(i, today))
    }

    @Test fun advancedRequiresProofOfAddress() {
        val i = input().copy(level = 2)
        assertEquals("请上传地址证明", ProfileLogic.kycError(i, today))
        assertNull(ProfileLogic.kycError(i.copy(uploaded = i.uploaded + ApiKycDocumentSide.PROOF_OF_ADDRESS), today))
    }

    @Test fun ageUsesBirthdayNotOnlyYear() {
        assertEquals(17, ProfileLogic.ageAt("2008-11-06", today))
        assertEquals(18, ProfileLogic.ageAt("2008-11-05", today))
        assertNull(ProfileLogic.ageAt("not-a-date", today))
    }

    @Test fun subAccountUsernameMatchesOpenApi() {
        assertNull(ProfileLogic.subAccountNameError("abc_01"))
        assertNotNull(ProfileLogic.subAccountNameError("_abc"))
        assertNotNull(ProfileLogic.subAccountNameError("ab"))
        assertNotNull(ProfileLogic.subAccountNameError("ABC"))
    }
}
