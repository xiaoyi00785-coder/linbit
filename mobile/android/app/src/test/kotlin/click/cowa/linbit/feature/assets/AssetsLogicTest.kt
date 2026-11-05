package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiWalletNetwork
import org.junit.Assert.assertEquals
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
}
