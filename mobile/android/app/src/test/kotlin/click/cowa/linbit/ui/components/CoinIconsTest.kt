package click.cowa.linbit.ui.components

import click.cowa.linbit.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CoinIconsTest {
    @Test fun bundledCoinsResolveCaseInsensitively() {
        assertEquals(R.drawable.coin_btc, CoinIcons.drawable("btc"))
        assertEquals(R.drawable.coin_usdt, CoinIcons.drawable(" USDT "))
        assertEquals(R.drawable.coin_eth, CoinIcons.drawable("ETH.e"))
        // t82u: SOL must map to handmade web3icons mark (not missing / wrong generic)
        assertEquals(R.drawable.coin_sol, CoinIcons.drawable("sol"))
        assertEquals(R.drawable.coin_sol, CoinIcons.drawable("SOL"))
        assertEquals(R.drawable.coin_sol, CoinIcons.drawable("SOL.e"))
        assertEquals(R.drawable.coin_doge, CoinIcons.drawable("doge"))
        assertEquals(R.drawable.coin_dot, CoinIcons.drawable("DOT"))
    }

    @Test fun unbundledCoinsKeepRemoteAndLetterFallback() {
        assertNull(CoinIcons.drawable("ARB"))
        assertNull(CoinIcons.drawable("UNKNOWN"))
        assertNull(CoinIcons.drawable(""))
    }
}
