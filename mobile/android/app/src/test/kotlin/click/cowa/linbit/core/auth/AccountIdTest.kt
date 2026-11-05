package click.cowa.linbit.core.auth

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AccountIdTest {
    @Test fun digitsAreTreatedAsPhoneOthersAsEmail() {
        val p = AccountId.fromRaw("013800138000", "86")
        assertEquals(AccountId.Kind.PHONE, p.kind)
        assertEquals("+8613800138000", p.e164) // 去掉前导 0
        assertEquals("+86 138****8000", p.masked)
        val e = AccountId.fromRaw(" Insight@outlook.com ", "86")
        assertEquals(AccountId.Kind.EMAIL, e.kind)
        assertTrue(e.isValid)
        assertEquals("In***@outlook.com", e.masked)
    }

    @Test fun validation() {
        assertFalse(AccountId.fromRaw("abc", "86").isValid)
        assertFalse(AccountId.fromRaw("1234", "86").isValid)
        assertTrue(AccountId.fromRaw("13800138000", "86").isValid)
    }

    @Test fun passwordRules() {
        assertTrue(PasswordRule.ok("abcdefg1"))
        assertFalse(PasswordRule.ok("abcdefgh"))
        assertFalse(PasswordRule.ok("12345678"))
        assertFalse(PasswordRule.ok("a1"))
        assertEquals("密码至少 8 位", PasswordRule.hint("a1"))
        assertEquals(null, PasswordRule.hint(""))
    }
}
