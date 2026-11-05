package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.ApiAccount
import click.cowa.linbit.core.network.InMemoryTokenStore
import org.junit.Assert.*
import org.junit.Test

class SavedAccountsTest {
    private class Storage : AccountSecureStorage {
        val values = mutableMapOf<String, String>()
        override fun get(key: String): String? = values[key]
        override fun set(key: String, value: String?) { if (value == null) values.remove(key) else values[key] = value }
    }

    @Test fun switchingAndFailureRestoreBothTokensAndExpiryFlag() {
        val storage = Storage()
        val tokens = InMemoryTokenStore()
        val accounts = SavedAccounts(storage, tokens)
        tokens.accessToken = "access-a"; tokens.refreshToken = "refresh-a"; tokens.sessionExpired = true
        accounts.remember(ApiAccount("a", 0, false, "2026-01-01", email = "a@example.com"))
        tokens.accessToken = "access-b"; tokens.refreshToken = "refresh-b"; tokens.sessionExpired = false
        accounts.remember(ApiAccount("b", 0, false, "2026-01-01", email = "b@example.com"))

        tokens.accessToken = "latest-a"; tokens.refreshToken = "latest-refresh-a"; tokens.sessionExpired = true
        accounts.beginSwitch()
        assertTrue(accounts.switchTo("b"))
        assertEquals("access-b", tokens.accessToken)
        assertFalse(tokens.sessionExpired)

        accounts.restore()
        assertEquals("latest-a", tokens.accessToken)
        assertEquals("latest-refresh-a", tokens.refreshToken)
        assertTrue(tokens.sessionExpired)
        assertFalse(tokens.loggingOut)
    }

    @Test fun removingAccountDeletesOnlyItsEncryptedTokens() {
        val storage = Storage(); val tokens = InMemoryTokenStore(); val accounts = SavedAccounts(storage, tokens)
        tokens.accessToken = "a"; tokens.refreshToken = "ra"
        accounts.remember(ApiAccount("a", 0, false, "2026-01-01"))
        tokens.accessToken = "b"; tokens.refreshToken = "rb"
        accounts.remember(ApiAccount("b", 0, false, "2026-01-01"))
        accounts.remove(setOf("a"))
        assertEquals(listOf("b"), accounts.list().map { it.uid })
        assertNull(storage.get("lb.acct.a.r"))
        assertEquals("rb", storage.get("lb.acct.b.r"))
    }
}
