package click.cowa.linbit.ui.components

import androidx.annotation.DrawableRes
import click.cowa.linbit.R

/** 本地打包图标索引；新增币种须同时运行 mobile/tools/gen_coin_icons.py。 */
internal object CoinIcons {
    @DrawableRes
    fun normalize(base: String): String {
        var b = base.trim().uppercase().removeSuffix(".E")
        b = b.substringBefore('/').substringBefore('-').substringBefore('_')
        return b
    }

    @Volatile private var assetSet: Set<String>? = null

    /** spothq/cryptocurrency-icons 128/color PNG（assets/coins，CC0）。 */
    fun assetUri(ctx: android.content.Context, base: String): String? {
        val set = assetSet ?: runCatching { ctx.assets.list("coins")?.toSet() ?: emptySet() }.getOrDefault(emptySet()).also { assetSet = it }
        val f = base.lowercase() + ".png"
        return if (f in set) "file:///android_asset/coins/$f" else null
    }

    @DrawableRes
    fun drawable(base: String): Int? = when (base.trim().uppercase().removeSuffix(".E")) {
        "AAVE" -> R.drawable.coin_aave
        "AVAX" -> R.drawable.coin_avax
        "BCH" -> R.drawable.coin_bch
        "BTC" -> R.drawable.coin_btc
        "DAI" -> R.drawable.coin_dai
        "DOGE" -> R.drawable.coin_doge
        "DOT" -> R.drawable.coin_dot
        "ETC" -> R.drawable.coin_etc
        "ETH" -> R.drawable.coin_eth
        "FIL" -> R.drawable.coin_fil
        "LINK" -> R.drawable.coin_link
        "LTC" -> R.drawable.coin_ltc
        "MATIC" -> R.drawable.coin_matic
        "PAXG" -> R.drawable.coin_paxg
        "SOL" -> R.drawable.coin_sol
        "TRX" -> R.drawable.coin_trx
        "UNI" -> R.drawable.coin_uni
        "USDC" -> R.drawable.coin_usdc
        "USDT" -> R.drawable.coin_usdt
        "WBTC" -> R.drawable.coin_wbtc
        "XLM" -> R.drawable.coin_xlm
        "XRP" -> R.drawable.coin_xrp
        else -> null
    }
}
