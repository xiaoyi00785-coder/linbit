package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.brand.BrandCopy
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BrandCopyTest {
    @Test fun earnAgreementUsesLinbitNotCompetitor() {
        assertEquals("我已阅读并同意 Linbit 的理财服务协议", BrandCopy.EARN_AGREEMENT)
        assertFalse(BrandCopy.containsCompetitor(BrandCopy.EARN_AGREEMENT))
        assertTrue(BrandCopy.EARN_AGREEMENT.contains("Linbit"))
    }

    @Test fun sanitizeReplacesEasiCoinVariants() {
        assertEquals("我已阅读并同意 Linbit的理财服务协议", BrandCopy.sanitize("我已阅读并同意 EasiCoin的理财服务协议"))
        assertEquals("欢迎来到 Linbit", BrandCopy.sanitize("欢迎来到 easicoin"))
        assertEquals("Linbit 理财", BrandCopy.sanitize("易币 理财"))
        assertEquals("Linbit App", BrandCopy.sanitize("Easi Coin App"))
    }

    @Test fun sanitizeLeavesLinbitUntouched() {
        assertEquals("Linbit 理财服务协议", BrandCopy.sanitize("Linbit 理财服务协议"))
    }
}
