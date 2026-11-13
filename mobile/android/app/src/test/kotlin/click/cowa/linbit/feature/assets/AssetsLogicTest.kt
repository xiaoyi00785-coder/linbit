package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.ApiAssetsOverview
import click.cowa.linbit.core.generated.ApiAssetsOverviewAccount
import click.cowa.linbit.core.generated.ApiAssetsOverviewAccountBalancesInner
import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigDecimal
import java.time.Instant

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

    @Test fun receiveAmountOnlyDeductsGasInSameAsset() {
        // Gas 以提现币种计价（或未标明）：从数量中扣除
        assertEquals("9", AssetsLogic.receiveAmount("10", "1", "USDT", "USDT"))
        assertEquals("9", AssetsLogic.receiveAmount("10", "1", "USDT", null))
        assertEquals("9", AssetsLogic.receiveAmount("10", "1", "usdt", "USDT"))
        // Gas 以其他币种计价（如 ERC20 代币付 ETH Gas）：到账数量等于提现数量
        assertEquals("10", AssetsLogic.receiveAmount("10", "0.002", "USDT", "ETH"))
        assertEquals("0.5", AssetsLogic.receiveAmount("0.5", "2", "USDT", "TRX"))
    }

    @Test fun withdrawAmountErrorRules() {
        val usdtTrc = ApiWithdrawNetwork(
            network = "TRC20", withdrawEnabled = true, memoRequired = false,
            minWithdraw = "10", maxWithdraw = "20000", gasFeeEstimate = "2", gasFeeAsset = "USDT",
        )
        val avail = BigDecimal("100")
        assertNull(AssetsLogic.withdrawAmountError("", "USDT", avail, usdtTrc))
        assertEquals("请输入提现数量", AssetsLogic.withdrawAmountError("0", "USDT", avail, usdtTrc))
        assertEquals("可用余额不足", AssetsLogic.withdrawAmountError("101", "USDT", avail, usdtTrc))
        assertEquals("最小提现数量 10 USDT", AssetsLogic.withdrawAmountError("5", "USDT", avail, usdtTrc))
        assertEquals("单笔最大提现 20000 USDT", AssetsLogic.withdrawAmountError("30000", "USDT", BigDecimal("50000"), usdtTrc))
        assertNull(AssetsLogic.withdrawAmountError("50", "USDT", avail, usdtTrc))
        // 同币种 Gas：数量必须大于 Gas
        val noMin = usdtTrc.copy(minWithdraw = null)
        assertEquals("提现数量需大于链上网络费（Gas） 2 USDT", AssetsLogic.withdrawAmountError("2", "USDT", avail, noMin))
        // 其他币种 Gas：不应因 Gas 拦截
        val gasInTrx = noMin.copy(gasFeeEstimate = "5", gasFeeAsset = "TRX")
        assertNull(AssetsLogic.withdrawAmountError("2", "USDT", avail, gasInTrx))
        // 余额未知时只按网络规则校验
        assertNull(AssetsLogic.withdrawAmountError("50", "USDT", null, usdtTrc))
    }

    @Test fun withdrawAvailableUsesBackendWithdrawAccount() {
        fun acct(a: ApiAssetsOverviewAccount.Account, free: String) = ApiAssetsOverviewAccount(
            account = a, balances = listOf(ApiAssetsOverviewAccountBalancesInner(asset = "USDT", free = free)),
        )
        val accounts = listOf(acct(ApiAssetsOverviewAccount.Account.FUNDING, "5"), acct(ApiAssetsOverviewAccount.Account.SPOT, "80"))
        val spotWithdraw = ApiAssetsOverview(accounts = accounts, withdrawAccount = ApiAssetsOverview.WithdrawAccount.SPOT)
        assertEquals(BigDecimal("80"), AssetsLogic.withdrawAvailable(spotWithdraw, "USDT"))
        // 未指定时默认资金账户
        assertEquals(BigDecimal("5"), AssetsLogic.withdrawAvailable(ApiAssetsOverview(accounts = accounts), "usdt"))
        // 扣款账户里没有该币种 → 0；总览不可用 → null（由调用方回退）
        assertEquals(0, AssetsLogic.withdrawAvailable(spotWithdraw, "BTC")!!.signum())
        assertNull(AssetsLogic.withdrawAvailable(null, "USDT"))
    }

    @Test fun activeWithdrawLockIgnoresPastAndInvalid() {
        val now = Instant.parse("2026-11-13T10:00:00Z")
        assertEquals(Instant.parse("2026-11-14T10:00:00Z"), AssetsLogic.activeWithdrawLock("2026-11-14T10:00:00Z", now))
        assertNull(AssetsLogic.activeWithdrawLock("2026-11-12T10:00:00Z", now))
        assertNull(AssetsLogic.activeWithdrawLock("not-a-date", now))
        assertNull(AssetsLogic.activeWithdrawLock(null, now))
        assertNull(AssetsLogic.activeWithdrawLock("", now))
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
}
