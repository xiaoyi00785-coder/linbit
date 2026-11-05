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
    }

    @Test fun unbundledCoinsKeepRemoteAndLetterFallback() {
        assertNull(CoinIcons.drawable("ARB"))
        assertNull(CoinIcons.drawable("UNKNOWN"))
        assertNull(CoinIcons.drawable(""))
    }
}
