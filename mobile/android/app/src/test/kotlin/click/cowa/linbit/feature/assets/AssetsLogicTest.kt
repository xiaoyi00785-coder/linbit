package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.generated.ApiWithdrawal
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigDecimal

class AssetsLogicTest {
    @Test fun displayNetworkMapsKnownCodes() {
        assertEquals("Tron (TRC20)", AssetsLogic.displayNetwork("TRC20"))
        assertEquals("Ethereum (ERC20)", AssetsLogic.displayNetwork("ERC20"))
        assertEquals("Bitcoin", AssetsLogic.displayNetwork("BTC"))
        assertEquals("FOO", AssetsLogic.displayNetwork("FOO"))
    }

    @Test fun positiveAmountHidesZeroAndBlank() {
        assertNull(AssetsLogic.positiveAmount(null))
        assertNull(AssetsLogic.positiveAmount(""))
        assertNull(AssetsLogic.positiveAmount("0"))
        assertNull(AssetsLogic.positiveAmount("0.000"))
        assertEquals("10", AssetsLogic.positiveAmount("10.00000000"))
        assertEquals("0.001", AssetsLogic.positiveAmount("0.00100000"))
    }

    @Test fun etaTextUsesBackendMinutes() {
        assertEquals("预计到账时间 ≈ 3 分钟", AssetsLogic.etaText(3))
        assertEquals("预计到账时间 --", AssetsLogic.etaText(null))
        assertEquals("预计到账时间 --", AssetsLogic.etaText(0))
    }

    @Test fun defaultDepositNetworkPrefersRecommended() {
        val nets = listOf(
            ApiWalletNetwork(network = "ERC20", depositEnabled = true, recommended = false, sortOrder = 1),
            ApiWalletNetwork(network = "TRC20", depositEnabled = true, recommended = true, sortOrder = 0),
            ApiWalletNetwork(network = "BEP20", depositEnabled = true, recommended = false, sortOrder = 2),
        )
        assertEquals("TRC20", AssetsLogic.defaultDepositNetwork(nets)?.network)
    }

    @Test fun defaultDepositNetworkSingleWhenNoRecommended() {
        val nets = listOf(ApiWalletNetwork(network = "BTC", depositEnabled = true, recommended = false))
        assertEquals("BTC", AssetsLogic.defaultDepositNetwork(nets)?.network)
    }

    @Test fun defaultDepositNetworkNullWhenMultipleWithoutRecommended() {
        val nets = listOf(
            ApiWalletNetwork(network = "ERC20", depositEnabled = true),
            ApiWalletNetwork(network = "TRC20", depositEnabled = true),
        )
        assertNull(AssetsLogic.defaultDepositNetwork(nets))
    }

    @Test fun depositNetworksFiltersDisabledAndOtherAssets() {
        val raw = listOf(
            ApiWalletNetwork(asset = "USDT", network = "TRC20", depositEnabled = true),
            ApiWalletNetwork(asset = "USDT", network = "ERC20", depositEnabled = false),
            ApiWalletNetwork(asset = "BTC", network = "BTC", depositEnabled = true),
            ApiWalletNetwork(network = "SOL", depositEnabled = true),
        )
        val out = AssetsLogic.depositNetworks("USDT", raw).map { it.network }
        assertEquals(listOf("TRC20", "SOL"), out)
    }

    @Test fun withdrawNetworksFiltersDisabled() {
        val raw = listOf(
            ApiWithdrawNetwork(network = "TRC20", withdrawEnabled = true, memoRequired = false),
            ApiWithdrawNetwork(network = "ERC20", withdrawEnabled = false, memoRequired = false),
        )
        assertEquals(listOf("TRC20"), AssetsLogic.withdrawEnabledNetworks(raw).map { it.network })
    }

    @Test fun withdrawMemoRequiredUsesEitherNetworkSource() {
        val primary = ApiWithdrawNetwork(network = "EOS", withdrawEnabled = true, memoRequired = true)
        val wallet = ApiWalletNetwork(network = "EOS", withdrawEnabled = true, memoRequired = true)
        assertTrue(AssetsLogic.withdrawMemoRequired(primary, null))
        assertTrue(AssetsLogic.withdrawMemoRequired(null, wallet))
        assertTrue(AssetsLogic.withdrawMemoRequired(primary.copy(memoRequired = false), wallet))
        assertFalse(AssetsLogic.withdrawMemoRequired(primary.copy(memoRequired = false), wallet.copy(memoRequired = false)))
        assertFalse(AssetsLogic.withdrawMemoRequired(null, null))
    }

    @Test fun receiveAmountSubtractsGas() {
        assertEquals("9", AssetsLogic.receiveAmount("10", "1"))
        assertNull(AssetsLogic.receiveAmount("1", "2"))
        assertNull(AssetsLogic.receiveAmount(null, "1"))
    }

    @Test fun filterBalancesHideSmallAndQuery() {
        val list = listOf(
            ApiBalance("USDT", "2", "0", "2", "2"),
            ApiBalance("BTC", "0.00001", "0", "0.00001", "0.5"),
            ApiBalance("ETH", "1", "0", "1", "3000"),
        )
        val usd = { b: ApiBalance -> BigDecimal(b.valuationUsd) }
        val rows = AssetsLogic.filterBalances(list, "", hideSmall = true, usdOf = usd)
        assertEquals(listOf("ETH", "USDT"), rows.map { it.asset })
        val q = AssetsLogic.filterBalances(list, "bt", hideSmall = false, usdOf = usd)
        assertEquals(listOf("BTC"), q.map { it.asset })
    }

    @Test fun newClientIdHasPrefix() {
        assertTrue(AssetsLogic.newClientId().startsWith("and-"))
    }

    @Test fun chainAmountErrorCoversMinMaxGasAndBalance() {
        val free = BigDecimal("100")
        assertNull(AssetsLogic.chainAmountError("", free, "10", "200", "1", "USDT"))
        assertEquals("请输入提现数量", AssetsLogic.chainAmountError("0", free, "10", null, null, "USDT"))
        assertEquals("可用余额不足", AssetsLogic.chainAmountError("150", free, null, null, null, "USDT"))
        assertTrue(AssetsLogic.chainAmountError("5", free, "10", null, null, "USDT")!!.contains("最小"))
        // free 须足够，否则会先命中「可用余额不足」
        assertTrue(AssetsLogic.chainAmountError("250", BigDecimal("1000"), null, "200", null, "USDT")!!.contains("最大"))
        assertTrue(AssetsLogic.chainAmountError("1", free, null, null, "1", "USDT")!!.contains("Gas"))
        assertNull(AssetsLogic.chainAmountError("50", free, "10", "200", "1", "USDT"))
    }

    @Test fun withdrawAuthCodesTotpVersusSms() {
        val totp = AssetsLogic.withdrawAuthCodes(true, "123456", "999999")
        assertEquals("123456", totp.totpCode)
        assertNull(totp.code)
        assertTrue(AssetsLogic.withdrawVerifyReady(true, "123456", ""))
        assertFalse(AssetsLogic.withdrawVerifyReady(true, "12", ""))
        val sms = AssetsLogic.withdrawAuthCodes(false, "123456", "654321")
        assertNull(sms.totpCode)
        assertEquals("654321", sms.code)
        assertTrue(AssetsLogic.withdrawVerifyReady(false, "", "6543"))
        assertFalse(AssetsLogic.withdrawVerifyReady(false, "", "12"))
    }


    @Test fun canCancelWithdrawalOnlyPending() {
        assertTrue(AssetsLogic.canCancelWithdrawal(ApiWithdrawal.Status.PENDING))
        assertFalse(AssetsLogic.canCancelWithdrawal(ApiWithdrawal.Status.PROCESSING))
        assertFalse(AssetsLogic.canCancelWithdrawal(ApiWithdrawal.Status.COMPLETED))
        assertFalse(AssetsLogic.canCancelWithdrawal(ApiWithdrawal.Status.FAILED))
        assertFalse(AssetsLogic.canCancelWithdrawal(ApiWithdrawal.Status.CANCELED))
        assertFalse(AssetsLogic.canCancelWithdrawal(null))
    }

    @Test fun applyWithdrawalUpdateReplacesMatchingId() {
        fun w(id: String, st: ApiWithdrawal.Status) = ApiWithdrawal(
            id = id, asset = "USDT", network = "TRC20", address = "Txxx", amount = "10",
            fee = "1", status = st, createdAt = "2026-11-05T00:00:00Z",
        )
        val list = listOf(w("a", ApiWithdrawal.Status.PENDING), w("b", ApiWithdrawal.Status.PENDING))
        val updated = w("a", ApiWithdrawal.Status.CANCELED)
        val next = AssetsLogic.applyWithdrawalUpdate(list, updated)
        assertEquals(ApiWithdrawal.Status.CANCELED, next[0].status)
        assertEquals(ApiWithdrawal.Status.PENDING, next[1].status)
    }

    @Test fun displayWithdrawTimeFormatsIso() {
        val s = AssetsLogic.displayWithdrawTime("2026-11-05T12:00:00Z")
        assertTrue(s.contains("2026-11-05"))
        assertFalse(s.contains("T"))
        assertEquals("--", AssetsLogic.displayWithdrawTime(null))
        assertEquals("--", AssetsLogic.displayWithdrawTime(""))
    }

}
